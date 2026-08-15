package fbhttp

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	gopath "path"
	"path/filepath"
	"strings"

	"github.com/rforced/filebrowser/v2/files"
	"github.com/rforced/filebrowser/v2/fileutils"
	"github.com/rforced/filebrowser/v2/users"
)

func parseQueryFiles(r *http.Request, f *files.FileInfo, _ *users.User) ([]string, error) {
	var fileSlice []string
	names := strings.Split(r.URL.Query().Get("files"), ",")

	if len(names) == 0 {
		fileSlice = append(fileSlice, f.Path)
	} else {
		for _, name := range names {
			name, err := url.QueryUnescape(strings.ReplaceAll(name, "+", "%2B"))
			if err != nil {
				return nil, err
			}

			name = slashClean(name)
			fileSlice = append(fileSlice, filepath.Join(f.Path, name))
		}
	}

	return fileSlice, nil
}

// parseQueryAlgorithm maps the ?algo= value to the archive format to produce.
// The set is deliberately small: every additional format is another encoder
// compiled into the binary and another decoder a downloader has to run.
func parseQueryAlgorithm(r *http.Request) (ext string, cont container, comp compression, err error) {
	switch r.URL.Query().Get("algo") {
	case "zip", "true", "":
		return ".zip", containerZip, compressNone, nil
	case "targz":
		return ".tar.gz", containerTar, compressGzip, nil
	case "tarlz4":
		return ".tar.lz4", containerTar, compressLz4, nil
	case "tarzst":
		return ".tar.zst", containerTar, compressZstd, nil
	default:
		return "", 0, 0, errors.New("format not implemented")
	}
}

func setContentDisposition(w http.ResponseWriter, r *http.Request, file *files.FileInfo) {
	if r.URL.Query().Get("inline") == "true" {
		// As per RFC6266 section 4.3
		w.Header().Set("Content-Disposition", "inline; filename*=utf-8''"+url.PathEscape(file.Name))
	} else {
		// As per RFC6266 section 4.3
		w.Header().Set("Content-Disposition", "attachment; filename*=utf-8''"+url.PathEscape(file.Name))
		// Force a non-renderable type so the browser always downloads the file
		// instead of sniffing it and potentially executing it inline.
		w.Header().Set("Content-Type", "application/octet-stream")
	}
}

var rawHandler = withUser(func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	if !d.user.Perm.Download {
		return http.StatusAccepted, nil
	}

	file, err := files.NewFileInfo(&files.FileOptions{
		Fs:         d.user.Fs,
		Path:       r.URL.Path,
		Modify:     d.user.Perm.Modify,
		Expand:     false,
		ReadHeader: d.server.TypeDetectionByHeader,
		Checker:    d,
	})
	if err != nil {
		return errToStatus(err), err
	}

	if files.IsNamedPipe(file.Mode) {
		setContentDisposition(w, r, file)
		return 0, nil
	}

	if !file.IsDir {
		return rawFileHandler(w, r, file)
	}

	return rawDirHandler(w, r, d, file)
})

func getFiles(d *data, path, commonPath string) ([]archiveEntry, error) {
	if !d.Check(path) {
		return nil, nil
	}

	// The scoped filesystem refuses to dereference a symlink whose target
	// escapes the user's scope: Stat (and the Open calls below) return a
	// permission error, so an escaping entry is skipped by the recursive caller
	// rather than being added to the archive.
	info, err := d.user.Fs.Stat(path)
	if err != nil {
		return nil, err
	}

	var archiveFiles []archiveEntry

	if path != commonPath {
		nameInArchive := strings.TrimPrefix(path, commonPath)
		nameInArchive = strings.TrimPrefix(nameInArchive, string(filepath.Separator))
		nameInArchive = filepath.ToSlash(nameInArchive)
		// A backslash is a legal filename character on POSIX hosts, so it can
		// reach here verbatim. Rewriting it to the path separator "/" would
		// manufacture a traversal sequence (e.g. "..\..\x" -> "../../x") that
		// escapes the extraction directory on the victim's machine, while
		// leaving it as "\" lets Windows extractors treat it as a separator.
		// Neutralize it to an inert character instead of turning it into one.
		nameInArchive = strings.ReplaceAll(nameInArchive, "\\", "_")

		// Defense in depth: never emit an archive entry whose path escapes the
		// archive root, regardless of how the name was produced.
		if cleaned := gopath.Clean("/" + nameInArchive); cleaned != "/"+nameInArchive {
			return nil, fmt.Errorf("refusing unsafe archive entry name: %q", nameInArchive)
		}

		archiveFiles = append(archiveFiles, archiveEntry{
			info:          info,
			nameInArchive: nameInArchive,
			open: func() (io.ReadCloser, error) {
				return d.user.Fs.Open(path)
			},
		})
	}

	if info.IsDir() {
		f, err := d.user.Fs.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()

		names, err := f.Readdirnames(0)
		if err != nil {
			return nil, err
		}

		for _, name := range names {
			fPath := filepath.Join(path, name)
			subFiles, err := getFiles(d, fPath, commonPath)
			if err != nil {
				log.Printf("Failed to get files from %s: %v", fPath, err)
				continue
			}
			archiveFiles = append(archiveFiles, subFiles...)
		}
	}

	return archiveFiles, nil
}

func rawDirHandler(w http.ResponseWriter, r *http.Request, d *data, file *files.FileInfo) (int, error) {
	filenames, err := parseQueryFiles(r, file, d.user)
	if err != nil {
		return http.StatusInternalServerError, err
	}

	// An unknown ?algo= is the caller naming a format we do not produce, which
	// is a bad request rather than a server fault.
	extension, cont, comp, err := parseQueryAlgorithm(r)
	if err != nil {
		return http.StatusBadRequest, err
	}

	commonDir := fileutils.CommonPrefix(filepath.Separator, filenames...)

	var allFiles []archiveEntry
	for _, fname := range filenames {
		archiveFiles, err := getFiles(d, fname, commonDir)
		if err != nil {
			log.Printf("Failed to get files from %s: %v", fname, err)
			continue
		}
		allFiles = append(allFiles, archiveFiles...)
	}

	name := filepath.Base(commonDir)
	if name == "." || name == "" || name == string(filepath.Separator) {
		if file.Name != "" {
			name = file.Name
		} else {
			actual, statErr := file.Fs.Stat(".")
			if statErr != nil {
				return http.StatusInternalServerError, statErr
			}
			name = actual.Name()
		}
	}
	if len(filenames) > 1 {
		name = "_" + name
	}
	name += extension
	w.Header().Set("Content-Disposition", "attachment; filename*=utf-8''"+url.PathEscape(name))

	if err := writeArchive(r.Context(), w, cont, comp, allFiles); err != nil {
		return http.StatusInternalServerError, err
	}

	return 0, nil
}

func rawFileHandler(w http.ResponseWriter, r *http.Request, file *files.FileInfo) (int, error) {
	fd, err := file.Fs.Open(file.Path)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	defer fd.Close()

	setContentDisposition(w, r, file)
	w.Header().Add("Content-Security-Policy", `script-src 'none';`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private")
	http.ServeContent(w, r, file.Name, file.ModTime, fd)
	return 0, nil
}
