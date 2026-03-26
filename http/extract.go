package fbhttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mholt/archives"
	"github.com/spf13/afero"
)

const (
	extractMaxFiles      = 100000
	extractMaxTotalBytes = 100 * 1024 * 1024 * 1024 // 100 GB
)

// extractRequest is the JSON body for the extract endpoint.
type extractRequest struct {
	Destination string `json:"destination"`
	Overwrite   bool   `json:"overwrite"`
	DeleteAfter bool   `json:"deleteAfter"`
}

// extractProgress is sent as an SSE event during extraction.
type extractProgress struct {
	// Total is the number of entries found so far.
	Total int `json:"total"`
	// Current is the number of entries extracted so far.
	Current int `json:"current"`
	// CurrentFile is the name of the file being extracted.
	CurrentFile string `json:"currentFile"`
	// Done indicates extraction is complete.
	Done bool `json:"done"`
	// Error, if non-empty, indicates an extraction failure.
	Error string `json:"error,omitzero"`
}

// supportedArchiveExts lists the extensions we allow extraction for.
var supportedArchiveExts = []string{
	".zip",
	".tar",
	".tar.gz", ".tgz",
	".tar.zst", ".tzst",
	".tar.lz4", ".tlz4",
	".zst",
	".lz4",
}

// isArchiveFile checks whether a filename has a supported archive extension.
func isArchiveFile(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range supportedArchiveExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// archiveBaseName strips the archive extension from a filename to produce
// the default extraction folder name.
func archiveBaseName(name string) string {
	lower := strings.ToLower(name)
	// Try multi-part extensions first (longest match).
	for _, ext := range []string{".tar.gz", ".tar.zst", ".tar.lz4"} {
		if strings.HasSuffix(lower, ext) {
			return name[:len(name)-len(ext)]
		}
	}
	// Single extensions.
	for _, ext := range []string{".tgz", ".tzst", ".tlz4", ".zip", ".tar", ".zst", ".lz4"} {
		if strings.HasSuffix(lower, ext) {
			return name[:len(name)-len(ext)]
		}
	}
	return name
}

// extractCheckHandler is a GET handler that returns whether a file is an
// extractable archive. The file path comes from the URL path.
var extractCheckHandler = withUser(func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	if !d.user.Perm.Create {
		return http.StatusForbidden, nil
	}

	filePath := r.URL.Path
	if filePath == "" || filePath == "/" {
		return renderJSON(w, r, map[string]bool{"archive": false})
	}

	info, err := d.user.Fs.Stat(filePath)
	if err != nil {
		return errToStatus(err), err
	}

	if info.IsDir() {
		return renderJSON(w, r, map[string]bool{"archive": false})
	}

	isArchive := isArchiveFile(info.Name())
	resp := map[string]any{
		"archive": isArchive,
	}
	if isArchive {
		resp["destination"] = archiveBaseName(info.Name())
	}
	return renderJSON(w, r, resp)
})

// extractHandler is a POST handler that extracts an archive file.
// It streams progress via Server-Sent Events.
var extractHandler = withUser(func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	if !d.user.Perm.Create {
		return http.StatusForbidden, nil
	}

	filePath := r.URL.Path
	if filePath == "" || filePath == "/" {
		return http.StatusBadRequest, errors.New("no file specified")
	}

	// Parse request body.
	var req extractRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err)
		}
	}

	// Validate the source file exists and is not a directory.
	srcInfo, err := d.user.Fs.Stat(filePath)
	if err != nil {
		return errToStatus(err), err
	}
	if srcInfo.IsDir() {
		return http.StatusBadRequest, errors.New("cannot extract a directory")
	}
	if !isArchiveFile(srcInfo.Name()) {
		return http.StatusBadRequest, errors.New("file is not a supported archive type")
	}

	// Determine the extraction destination directory.
	srcDir := path.Dir(filePath)
	destName := req.Destination
	if destName == "" {
		destName = archiveBaseName(srcInfo.Name())
	}

	// Sanitize destination name - prevent path traversal.
	destName = filepath.Base(filepath.Clean(destName))
	if destName == "." || destName == ".." || destName == "/" {
		return http.StatusBadRequest, errors.New("invalid destination name")
	}

	destDir := path.Join(srcDir, destName)

	// Check if destination exists.
	if _, err := d.user.Fs.Stat(destDir); err == nil {
		if !req.Overwrite {
			return http.StatusConflict, errors.New("destination already exists")
		}
	}

	// Set up SSE for progress reporting.
	flusher, ok := w.(http.Flusher)
	if !ok {
		return http.StatusInternalServerError, errors.New("streaming not supported")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	sendProgress := func(p extractProgress) {
		data, _ := json.Marshal(p)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	// Perform the extraction.
	extractErr := performExtraction(r.Context(), d.user.Fs, filePath, destDir, req.Overwrite, sendProgress)
	if extractErr != nil {
		sendProgress(extractProgress{Error: extractErr.Error(), Done: true})
		return 0, nil
	}

	// Delete the archive after successful extraction if requested.
	if req.DeleteAfter {
		if d.user.Perm.Delete {
			if err := d.user.Fs.Remove(filePath); err != nil {
				sendProgress(extractProgress{
					Done:  true,
					Error: fmt.Sprintf("extraction succeeded but failed to delete archive: %v", err),
				})
				return 0, nil
			}
		}
	}

	sendProgress(extractProgress{Done: true})
	return 0, nil
})

// performExtraction identifies the archive format and extracts its contents.
func performExtraction(
	ctx context.Context,
	afs afero.Fs,
	srcPath string,
	destDir string,
	overwrite bool,
	progress func(extractProgress),
) error {
	// Open the source file for format identification.
	srcFile, err := afs.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer srcFile.Close()

	// Identify the archive format.
	format, reader, err := archives.Identify(ctx, path.Base(srcPath), srcFile)
	if err != nil {
		return fmt.Errorf("failed to identify archive format: %w", err)
	}

	// Ensure the destination directory exists.
	if err := afs.MkdirAll(destDir, 0750); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	var totalBytes int64
	var fileCount int

	// Handle the different format types.
	switch f := format.(type) {
	case archives.Extractor:
		// Handles: zip, tar, CompressedArchive (tar.gz, tar.zst, tar.lz4)
		err = f.Extract(ctx, reader, func(_ context.Context, info archives.FileInfo) error {
			return extractFileHandler(ctx, afs, destDir, info, overwrite, &fileCount, &totalBytes, progress)
		})
		if err != nil {
			return fmt.Errorf("extraction failed: %w", err)
		}

	case archives.Decompressor:
		// Handles standalone compressed files: .zst, .lz4
		// Decompress to a single file named after the archive without the compression extension.
		decompReader, err := f.OpenReader(reader)
		if err != nil {
			return fmt.Errorf("failed to open decompressor: %w", err)
		}
		defer decompReader.Close()

		outputName := archiveBaseName(path.Base(srcPath))
		outputPath := path.Join(destDir, outputName)

		if !overwrite {
			if _, err := afs.Stat(outputPath); err == nil {
				return errors.New("destination file already exists")
			}
		}

		outFile, err := afs.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0640)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer outFile.Close()

		fileCount = 1
		progress(extractProgress{Total: 1, Current: 0, CurrentFile: outputName})

		written, err := io.Copy(outFile, io.LimitReader(decompReader, extractMaxTotalBytes+1))
		if err != nil {
			return fmt.Errorf("decompression failed: %w", err)
		}
		if written > extractMaxTotalBytes {
			return errors.New("decompressed size exceeds maximum allowed size")
		}

		progress(extractProgress{Total: 1, Current: 1, CurrentFile: outputName})

	default:
		return errors.New("unsupported archive format")
	}

	return nil
}

// extractFileHandler handles a single file entry during archive extraction.
// It enforces security limits and writes the file to the destination.
func extractFileHandler(
	ctx context.Context,
	afs afero.Fs,
	destDir string,
	info archives.FileInfo,
	overwrite bool,
	fileCount *int,
	totalBytes *int64,
	progress func(extractProgress),
) error {
	// Check context cancellation.
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Enforce file count limit.
	*fileCount++
	if *fileCount > extractMaxFiles {
		return fmt.Errorf("archive exceeds maximum file count of %d", extractMaxFiles)
	}

	// Sanitize the file path to prevent zip-slip attacks.
	cleanName := path.Clean(info.NameInArchive)
	if cleanName == "." || cleanName == "" {
		return nil
	}

	// Reject any path that attempts to escape the destination directory.
	if strings.HasPrefix(cleanName, "..") || strings.HasPrefix(cleanName, "/") {
		return fmt.Errorf("illegal file path in archive: %s", info.NameInArchive)
	}
	// Extra check: ensure no path component is "..".
	for _, part := range strings.Split(cleanName, "/") {
		if part == ".." {
			return fmt.Errorf("illegal file path in archive: %s", info.NameInArchive)
		}
	}

	targetPath := path.Join(destDir, cleanName)

	// Reject symbolic links to prevent symlink attacks.
	if info.LinkTarget != "" {
		return nil // Silently skip symlinks for security.
	}

	// Send progress update.
	progress(extractProgress{
		Total:       0, // Unknown total for archives.
		Current:     *fileCount,
		CurrentFile: cleanName,
	})

	if info.IsDir() {
		return afs.MkdirAll(targetPath, 0750)
	}

	// Ensure parent directory exists.
	parentDir := path.Dir(targetPath)
	if err := afs.MkdirAll(parentDir, 0750); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", parentDir, err)
	}

	// Check if file exists when not overwriting.
	if !overwrite {
		if _, err := afs.Stat(targetPath); err == nil {
			return fmt.Errorf("file already exists: %s", cleanName)
		}
	}

	// Open the archived file for reading.
	if info.Open == nil {
		return nil // No content to extract (e.g., empty file entry).
	}

	srcFile, err := info.Open()
	if err != nil {
		return fmt.Errorf("failed to open archived file %s: %w", cleanName, err)
	}
	defer srcFile.Close()

	// Create the output file.
	outFile, err := afs.OpenFile(targetPath, writeFileFlags(), 0640)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", cleanName, err)
	}
	defer outFile.Close()

	// Copy with size limit enforcement.
	remaining := extractMaxTotalBytes - *totalBytes
	if remaining <= 0 {
		return errors.New("archive exceeds maximum total decompressed size")
	}

	written, err := io.Copy(outFile, io.LimitReader(srcFile, remaining+1))
	if err != nil {
		return fmt.Errorf("failed to write file %s: %w", cleanName, err)
	}

	*totalBytes += written
	if *totalBytes > extractMaxTotalBytes {
		return errors.New("archive exceeds maximum total decompressed size")
	}

	return nil
}

// writeFileFlags returns the flags for creating/writing a file.
func writeFileFlags() int {
	return os.O_WRONLY | os.O_CREATE | os.O_TRUNC
}
