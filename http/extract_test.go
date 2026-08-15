package fbhttp

import (
	"path"
	"strings"
	"testing"
)

func TestResolveDestDir(t *testing.T) {
	tests := []struct {
		name        string
		srcDir      string
		destination string
		wantDir     string
		wantErr     bool
	}{
		// Empty destination → extract in-place into srcDir.
		{"empty destination", "/files/docs", "", "/files/docs", false},
		{"empty destination root", "/", "", "/", false},

		// Valid destination name → sub-directory of srcDir.
		{"simple name", "/files/docs", "output", "/files/docs/output", false},
		{"name with spaces", "/files/docs", "my folder", "/files/docs/my folder", false},

		// filepath.Base strips leading path components, so ../evil → "evil" (accepted).
		{"traversal stripped to name", "/files/docs", "../evil", "/files/docs/evil", false},
		{"deep traversal stripped", "/files/docs", "../../etc/passwd", "/files/docs/passwd", false},

		// Inputs whose Base collapses to "." or "/" → rejected.
		{"dot destination", "/files/docs", ".", "", true},
		{"slash destination", "/files/docs", "/", "", true},
		{"double dot destination", "/files/docs", "..", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveDestDir(tt.srcDir, tt.destination)
			if tt.wantErr {
				if err == nil {
					t.Errorf("resolveDestDir(%q, %q) expected error, got %q", tt.srcDir, tt.destination, got)
				}
				return
			}
			if err != nil {
				t.Errorf("resolveDestDir(%q, %q) unexpected error: %v", tt.srcDir, tt.destination, err)
				return
			}
			if got != tt.wantDir {
				t.Errorf("resolveDestDir(%q, %q) = %q, want %q", tt.srcDir, tt.destination, got, tt.wantDir)
			}
		})
	}
}

func TestIsArchiveFile(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"zip file", "archive.zip", true},
		{"tar.gz file", "archive.tar.gz", true},
		{"gz file", "notes.txt.gz", true},
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
		// Bare .tar is deliberately unsupported: the formats in use all carry a
		// compression extension.
		{"bare tar not supported", "archive.tar", false},
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
		{"tar.gz", "archive.tar.gz", "archive"},
		{"gz", "notes.txt.gz", "notes.txt"},
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
		{"bare tar left alone", "archive.tar", "archive.tar"},
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

// TestPathTraversalPrevention exercises the real guard used by both extractors,
// rather than a copy of it in the test file. A mirrored implementation can drift
// from production and go on passing while the code it stands for stops
// protecting anything.
func TestPathTraversalPrevention(t *testing.T) {
	tests := []struct {
		name        string
		archivePath string
		// wantErr means the name must be rejected outright; wantSkip means it
		// carries no member (empty or "."), so it is silently ignored.
		wantErr  bool
		wantSkip bool
		want     string
	}{
		{name: "parent traversal", archivePath: "../../../etc/passwd", wantErr: true},
		{name: "mid traversal", archivePath: "foo/../../bar", wantErr: true},
		{name: "absolute path", archivePath: "/absolute/path", wantErr: true},
		{name: "simple parent", archivePath: "../relative", wantErr: true},
		{name: "bare parent", archivePath: "..", wantErr: true},
		{name: "dot only", archivePath: ".", wantSkip: true},
		{name: "empty", archivePath: "", wantSkip: true},
		{name: "valid simple", archivePath: "file.txt", want: "file.txt"},
		{name: "valid nested", archivePath: "dir/subdir/file.txt", want: "dir/subdir/file.txt"},
		{name: "valid deep", archivePath: "a/b/c/d.txt", want: "a/b/c/d.txt"},
		{name: "inner traversal normalized away", archivePath: "a/b/../c.txt", want: "a/c.txt"},
		// A backslash is a legal POSIX filename byte. It must be neutralized,
		// never translated into a separator, or "..\..\x" becomes a traversal.
		{name: "backslash neutralized", archivePath: `..\..\evil.sh`, want: ".._.._evil.sh"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := safeArchiveName(tt.archivePath)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected %q to be rejected, got %q", tt.archivePath, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.archivePath, err)
			}
			if tt.wantSkip {
				if got != "" {
					t.Errorf("expected %q to yield no member, got %q", tt.archivePath, got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("safeArchiveName(%q) = %q, want %q", tt.archivePath, got, tt.want)
			}
			// Whatever comes back must never escape when joined to a destination.
			if joined := path.Join("/dest", got); !strings.HasPrefix(joined, "/dest/") {
				t.Errorf("VULNERABLE: %q joined to /dest escaped: %q", tt.archivePath, joined)
			}
		})
	}
}
