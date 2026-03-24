package files

import (
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
