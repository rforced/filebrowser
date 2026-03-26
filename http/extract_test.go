package fbhttp

import (
	"path"
	"strings"
	"testing"
)

func TestIsArchiveFile(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"zip file", "archive.zip", true},
		{"tar file", "archive.tar", true},
		{"tar.gz file", "archive.tar.gz", true},
		{"tgz file", "archive.tgz", true},
		{"tar.zst file", "archive.tar.zst", true},
		{"tzst file", "archive.tzst", true},
		{"tar.lz4 file", "archive.tar.lz4", true},
		{"tlz4 file", "archive.tlz4", true},
		{"zst file", "data.zst", true},
		{"lz4 file", "data.lz4", true},
		{"uppercase ZIP", "ARCHIVE.ZIP", true},
		{"mixed case", "Archive.Tar.Gz", true},
		{"not archive txt", "readme.txt", false},
		{"not archive pdf", "document.pdf", false},
		{"not archive exe", "program.exe", false},
		{"empty string", "", false},
		{"just a dot", ".", false},
		{"partial match", "file.zi", false},
		{"tar in name but not ext", "tarfile.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isArchiveFile(tt.filename)
			if got != tt.want {
				t.Errorf("isArchiveFile(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestArchiveBaseName(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{"zip", "archive.zip", "archive"},
		{"tar", "archive.tar", "archive"},
		{"tar.gz", "archive.tar.gz", "archive"},
		{"tgz", "archive.tgz", "archive"},
		{"tar.zst", "archive.tar.zst", "archive"},
		{"tzst", "archive.tzst", "archive"},
		{"tar.lz4", "backup.tar.lz4", "backup"},
		{"tlz4", "backup.tlz4", "backup"},
		{"zst", "data.zst", "data"},
		{"lz4", "data.lz4", "data"},
		{"uppercase", "ARCHIVE.ZIP", "ARCHIVE"},
		{"mixed case tar.gz", "Archive.Tar.Gz", "Archive"},
		{"no extension", "noext", "noext"},
		{"dots in name", "my.archive.file.tar.gz", "my.archive.file"},
		{"multiple dots zip", "a.b.c.zip", "a.b.c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := archiveBaseName(tt.filename)
			if got != tt.want {
				t.Errorf("archiveBaseName(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

// testSanitizeArchivePath mirrors the path validation logic in extractFileHandler
// to verify that malicious paths are properly rejected.
func testSanitizeArchivePath(name string) string {
	cleanName := path.Clean(name)
	if cleanName == "." || cleanName == "" {
		return ""
	}
	if strings.HasPrefix(cleanName, "..") || strings.HasPrefix(cleanName, "/") {
		return ""
	}
	for _, part := range strings.Split(cleanName, "/") {
		if part == ".." {
			return ""
		}
	}
	return cleanName
}

func TestPathTraversalPrevention(t *testing.T) {
	tests := []struct {
		name        string
		archivePath string
		wantEmpty   bool
	}{
		{"parent traversal", "../../../etc/passwd", true},
		{"mid traversal", "foo/../../bar", true},
		{"absolute path", "/absolute/path", true},
		{"simple parent", "../relative", true},
		{"dot only", ".", true},
		{"empty", "", true},
		{"valid simple", "file.txt", false},
		{"valid nested", "dir/subdir/file.txt", false},
		{"valid deep", "a/b/c/d.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := testSanitizeArchivePath(tt.archivePath)
			if tt.wantEmpty && result != "" {
				t.Errorf("expected path %q to be rejected, got %q", tt.archivePath, result)
			}
			if !tt.wantEmpty && result == "" {
				t.Errorf("expected path %q to be accepted, got empty", tt.archivePath)
			}
		})
	}
}
