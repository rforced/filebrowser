package files

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
)

func TestDirSize_EmptyDirectory(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	if err := fs.MkdirAll("/testdir", 0755); err != nil {
		t.Fatal(err)
	}

	fi := &FileInfo{
		Fs:    fs,
		Path:  "/testdir",
		IsDir: true,
	}

	info, err := fi.DirSize()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Size != 0 {
		t.Errorf("expected size 0, got %d", info.Size)
	}
	if info.NumFiles != 0 {
		t.Errorf("expected 0 files, got %d", info.NumFiles)
	}
	if info.NumDirs != 0 {
		t.Errorf("expected 0 dirs, got %d", info.NumDirs)
	}
}

func TestDirSize_WithFilesAndSubdirs(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	if err := fs.MkdirAll("/testdir/sub1", 0755); err != nil {
		t.Fatal(err)
	}
	if err := fs.MkdirAll("/testdir/sub2/nested", 0755); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(fs, "/testdir/file1.txt", []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(fs, "/testdir/sub1/file2.txt", []byte("world!"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(fs, "/testdir/sub2/nested/file3.txt", []byte("foo bar baz"), 0644); err != nil {
		t.Fatal(err)
	}

	fi := &FileInfo{
		Fs:    fs,
		Path:  "/testdir",
		IsDir: true,
	}

	info, err := fi.DirSize()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Size should include file content (22 bytes) plus directory sizes
	fileContentSize := int64(5 + 6 + 11) // "hello" + "world!" + "foo bar baz"
	if info.Size < fileContentSize {
		t.Errorf("expected size >= %d (file content), got %d", fileContentSize, info.Size)
	}
	if info.NumFiles != 3 {
		t.Errorf("expected 3 files, got %d", info.NumFiles)
	}
	// sub1, sub2, sub2/nested = 3 dirs
	if info.NumDirs != 3 {
		t.Errorf("expected 3 dirs, got %d", info.NumDirs)
	}
}

func TestDirSize_RegularFile(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "/file.txt", []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	fi := &FileInfo{
		Fs:    fs,
		Path:  "/file.txt",
		IsDir: false,
		Size:  7,
	}

	info, err := fi.DirSize()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Size != 7 {
		t.Errorf("expected size 7, got %d", info.Size)
	}
	if info.NumFiles != 1 {
		t.Errorf("expected 1 file, got %d", info.NumFiles)
	}
	if info.NumDirs != 0 {
		t.Errorf("expected 0 dirs, got %d", info.NumDirs)
	}
}

func TestDirSize_OnlySubdirectories(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	if err := fs.MkdirAll("/testdir/a/b/c", 0755); err != nil {
		t.Fatal(err)
	}

	fi := &FileInfo{
		Fs:    fs,
		Path:  "/testdir",
		IsDir: true,
	}

	info, err := fi.DirSize()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Size includes directory entry sizes
	if info.Size < 0 {
		t.Errorf("expected size >= 0, got %d", info.Size)
	}
	if info.NumFiles != 0 {
		t.Errorf("expected 0 files, got %d", info.NumFiles)
	}
	// a, a/b, a/b/c = 3 dirs
	if info.NumDirs != 3 {
		t.Errorf("expected 3 dirs, got %d", info.NumDirs)
	}
}

func TestWithinScope(t *testing.T) {
	t.Run("non-scoped filesystem is a no-op", func(t *testing.T) {
		ok, err := WithinScope(afero.NewOsFs(), "/anything")
		if err != nil || !ok {
			t.Fatalf("expected (true, nil), got (%v, %v)", ok, err)
		}
	})

	t.Run("path inside a nested scope is allowed", func(t *testing.T) {
		scope := t.TempDir()
		if err := os.WriteFile(filepath.Join(scope, "file.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		bfs := afero.NewBasePathFs(afero.NewOsFs(), scope)

		ok, err := WithinScope(bfs, "/file.txt")
		if err != nil || !ok {
			t.Fatalf("expected (true, nil), got (%v, %v)", ok, err)
		}
	})

	t.Run("new file inside scope is allowed", func(t *testing.T) {
		scope := t.TempDir()
		bfs := afero.NewBasePathFs(afero.NewOsFs(), scope)

		ok, err := WithinScope(bfs, "/does-not-exist-yet.txt")
		if err != nil || !ok {
			t.Fatalf("expected (true, nil), got (%v, %v)", ok, err)
		}
	})

	// Regression for #5975: when the scope resolves to the filesystem root,
	// root+separator used to be "//", which no path matched, so every write
	// was rejected with os.ErrPermission (HTTP 403).
	t.Run("filesystem root scope allows writes", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "file.txt")
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		bfs := afero.NewBasePathFs(afero.NewOsFs(), "/")

		ok, err := WithinScope(bfs, f)
		if err != nil || !ok {
			t.Fatalf("expected (true, nil) for a path under root scope, got (%v, %v)", ok, err)
		}
	})

	t.Run("sibling of a nested scope is rejected", func(t *testing.T) {
		base := t.TempDir()
		scope := filepath.Join(base, "srv")
		sibling := filepath.Join(base, "srvother")
		for _, d := range []string{scope, sibling} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		// A symlink lexically inside the scope pointing at a sibling directory
		// must not be followed.
		link := filepath.Join(scope, "escape")
		if err := os.Symlink(sibling, link); err != nil {
			t.Fatal(err)
		}
		bfs := afero.NewBasePathFs(afero.NewOsFs(), scope)

		ok, err := WithinScope(bfs, "/escape")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Fatal("expected escaping symlink to a sibling directory to be rejected")
		}
	})
}
