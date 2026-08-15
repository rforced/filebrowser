package fbhttp

import (
	"archive/tar"
	"archive/zip"
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

// archiveKind describes how one supported extension is read: which container
// holds the members, and which stream codec wraps it.
type archiveKind struct {
	ext  string
	cont container
	comp compression
}

// supportedArchiveKinds is the whole set of formats we extract, longest
// extension first so that ".tar.gz" is matched before ".gz". Each entry is a
// decoder compiled into the binary and pointed at untrusted input, so the list
// is kept to what is actually used.
var supportedArchiveKinds = []archiveKind{
	{".tar.gz", containerTar, compressGzip},
	{".tar.zst", containerTar, compressZstd},
	{".tar.lz4", containerTar, compressLz4},
	{".tgz", containerTar, compressGzip},
	{".tzst", containerTar, compressZstd},
	{".tlz4", containerTar, compressLz4},
	{".tar", containerTar, compressNone},
	{".zip", containerZip, compressNone},
	{".gz", containerNone, compressGzip},
	{".zst", containerNone, compressZstd},
	{".lz4", containerNone, compressLz4},
}

// archiveKindFor returns the format for a filename, matching the longest
// extension.
func archiveKindFor(name string) (archiveKind, bool) {
	lower := strings.ToLower(name)
	for _, kind := range supportedArchiveKinds {
		if strings.HasSuffix(lower, kind.ext) {
			return kind, true
		}
	}
	return archiveKind{}, false
}

// isArchiveFile checks whether a filename has a supported archive extension.
func isArchiveFile(name string) bool {
	_, ok := archiveKindFor(name)
	return ok
}

// archiveBaseName strips the archive extension from a filename to produce
// the default extraction folder name.
func archiveBaseName(name string) string {
	if kind, ok := archiveKindFor(name); ok {
		return name[:len(name)-len(kind.ext)]
	}
	return name
}

// resolveDestDir returns the absolute (within the virtual FS) destination
// directory for an extraction request.
// An empty destination means extract directly into srcDir (in-place).
// A non-empty destination is treated as a single folder name appended to srcDir;
// path traversal attempts are rejected with an error.
func resolveDestDir(srcDir, destination string) (string, error) {
	if destination == "" {
		return srcDir, nil
	}
	destName := filepath.Base(filepath.Clean(destination))
	if destName == "." || destName == ".." || destName == "/" {
		return "", errors.New("invalid destination name")
	}

	dest, err := resolveInside(srcDir, destName)
	if err != nil {
		return "", errors.New("invalid destination name")
	}
	return dest, nil
}

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
	// An empty destination means extract directly into the source directory.
	srcDir := path.Dir(filePath)
	destDir, err := resolveDestDir(srcDir, req.Destination)
	if err != nil {
		return http.StatusBadRequest, err
	}

	// Check if destination sub-directory exists (only when a name was given).
	if req.Destination != "" {
		if _, statErr := d.user.Fs.Stat(destDir); statErr == nil {
			if !req.Overwrite {
				return http.StatusConflict, errors.New("destination already exists")
			}
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

// extractState carries the running limits across one extraction, so that a
// bomb is caught on the aggregate rather than per entry.
type extractState struct {
	fileCount  int
	totalBytes int64
}

// performExtraction opens the archive and dispatches on its declared format.
func performExtraction(
	ctx context.Context,
	afs afero.Fs,
	srcPath string,
	destDir string,
	overwrite bool,
	progress func(extractProgress),
) error {
	kind, ok := archiveKindFor(path.Base(srcPath))
	if !ok {
		return errors.New("unsupported archive format")
	}

	srcFile, err := afs.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer srcFile.Close()

	// The format was chosen from the filename, so confirm the bytes agree
	// before handing the stream to that decoder.
	if magic := kind.comp.magic(); magic != nil {
		if err := checkMagic(srcFile, magic, kind.ext); err != nil {
			return err
		}
	} else if kind.cont == containerZip {
		if err := checkMagic(srcFile, []byte("PK"), ".zip"); err != nil {
			return err
		}
	} else if kind.cont == containerTar {
		if err := checkTarMagic(srcFile); err != nil {
			return err
		}
	}

	if err := afs.MkdirAll(destDir, 0750); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	state := &extractState{}

	switch kind.cont {
	case containerZip:
		info, statErr := srcFile.Stat()
		if statErr != nil {
			return fmt.Errorf("failed to stat archive: %w", statErr)
		}
		return extractZip(ctx, afs, srcFile, info.Size(), destDir, overwrite, state, progress)

	case containerTar:
		cr, crErr := kind.comp.newReader(srcFile)
		if crErr != nil {
			return fmt.Errorf("failed to open decompressor: %w", crErr)
		}
		defer cr.Close()
		return extractTar(ctx, afs, cr, destDir, overwrite, state, progress)

	case containerNone:
		return extractSingle(ctx, afs, srcFile, kind, srcPath, destDir, overwrite, progress)

	default:
		return errors.New("unsupported archive format")
	}
}

// extractZip walks a zip archive. Members are read through the central
// directory, so a name is known before any of its content is decompressed.
func extractZip(
	ctx context.Context,
	afs afero.Fs,
	r io.ReaderAt,
	size int64,
	destDir string,
	overwrite bool,
	state *extractState,
	progress func(extractProgress),
) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return fmt.Errorf("failed to read zip archive: %w", err)
	}

	for _, member := range zr.File {
		if err := ctx.Err(); err != nil {
			return err
		}

		name, err := safeArchiveName(member.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}

		mode := member.Mode()
		// Skip anything that is not a plain file or directory. A zip can carry
		// a symlink as a member whose body is the link target; honouring one
		// would let an archive plant a link pointing anywhere on the host.
		if !mode.IsRegular() && !mode.IsDir() {
			continue
		}

		if err := state.count(); err != nil {
			return err
		}
		progress(extractProgress{Current: state.fileCount, CurrentFile: name})

		target, err := resolveInside(destDir, name)
		if err != nil {
			return err
		}

		if mode.IsDir() || strings.HasSuffix(member.Name, "/") {
			if err := afs.MkdirAll(target, 0750); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", name, err)
			}
			continue
		}

		rc, err := member.Open()
		if err != nil {
			return fmt.Errorf("failed to open archived file %s: %w", name, err)
		}
		err = writeExtractedFile(afs, target, name, rc, overwrite, state)
		rc.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

// extractTar walks a tar stream.
func extractTar(
	ctx context.Context,
	afs afero.Fs,
	r io.Reader,
	destDir string,
	overwrite bool,
	state *extractState,
	progress func(extractProgress),
) error {
	tr := tar.NewReader(r)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to read tar archive: %w", err)
		}

		name, err := safeArchiveName(header.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}

		// Only plain files and directories are materialized. Symlinks and hard
		// links are the classic way an archive escapes its extraction
		// directory, and device nodes have no business here at all.
		switch header.Typeflag {
		case tar.TypeDir:
			if err := state.count(); err != nil {
				return err
			}
			progress(extractProgress{Current: state.fileCount, CurrentFile: name})
			target, err := resolveInside(destDir, name)
			if err != nil {
				return err
			}
			if err := afs.MkdirAll(target, 0750); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", name, err)
			}
		case tar.TypeReg:
			if err := state.count(); err != nil {
				return err
			}
			progress(extractProgress{Current: state.fileCount, CurrentFile: name})
			target, err := resolveInside(destDir, name)
			if err != nil {
				return err
			}
			if err := writeExtractedFile(afs, target, name, tr, overwrite, state); err != nil {
				return err
			}
		default:
			continue
		}
	}
}

// extractSingle decompresses a bare compressed file (.gz, .zst, .lz4) into one
// output file named after the archive with its extension removed.
func extractSingle(
	ctx context.Context,
	afs afero.Fs,
	src io.Reader,
	kind archiveKind,
	srcPath string,
	destDir string,
	overwrite bool,
	progress func(extractProgress),
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	cr, err := kind.comp.newReader(src)
	if err != nil {
		return fmt.Errorf("failed to open decompressor: %w", err)
	}
	defer cr.Close()

	outputName := archiveBaseName(path.Base(srcPath))
	if outputName == "" {
		return errors.New("cannot determine output file name")
	}
	outputPath, err := resolveInside(destDir, outputName)
	if err != nil {
		return err
	}

	progress(extractProgress{Total: 1, Current: 0, CurrentFile: outputName})

	state := &extractState{}
	if err := writeExtractedFile(afs, outputPath, outputName, cr, overwrite, state); err != nil {
		return err
	}

	progress(extractProgress{Total: 1, Current: 1, CurrentFile: outputName})
	return nil
}

// count records one more member and enforces the entry-count ceiling.
func (s *extractState) count() error {
	s.fileCount++
	if s.fileCount > extractMaxFiles {
		return fmt.Errorf("archive exceeds maximum file count of %d", extractMaxFiles)
	}
	return nil
}

// writeExtractedFile creates target and copies src into it, charging the bytes
// against the archive-wide budget. The copy is bounded rather than trusting any
// size the archive declares, so a member that claims to be small but decodes
// forever is cut off.
func writeExtractedFile(
	afs afero.Fs,
	target string,
	name string,
	src io.Reader,
	overwrite bool,
	state *extractState,
) error {
	if err := afs.MkdirAll(path.Dir(target), 0750); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", path.Dir(target), err)
	}

	if !overwrite {
		if _, err := afs.Stat(target); err == nil {
			return fmt.Errorf("file already exists: %s", name)
		}
	}

	remaining := extractMaxTotalBytes - state.totalBytes
	if remaining <= 0 {
		return errors.New("archive exceeds maximum total decompressed size")
	}

	outFile, err := afs.OpenFile(target, writeFileFlags(), 0640)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", name, err)
	}
	defer outFile.Close()

	written, err := io.Copy(outFile, io.LimitReader(src, remaining+1))
	if err != nil {
		return fmt.Errorf("failed to write file %s: %w", name, err)
	}

	state.totalBytes += written
	if state.totalBytes > extractMaxTotalBytes {
		return errors.New("archive exceeds maximum total decompressed size")
	}

	return nil
}

// writeFileFlags returns the flags for creating/writing a file.
func writeFileFlags() int {
	return os.O_WRONLY | os.O_CREATE | os.O_TRUNC
}
