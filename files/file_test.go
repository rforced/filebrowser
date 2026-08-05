package files

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"mime"
	"os"
	"path/filepath"
	"strings"
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

func TestScopedFsWithin(t *testing.T) {
	t.Run("path inside a nested scope is allowed", func(t *testing.T) {
		scope := t.TempDir()
		if err := os.WriteFile(filepath.Join(scope, "file.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		sfs := NewScopedFs(afero.NewOsFs(), scope)

		ok, err := sfs.within("/file.txt")
		if err != nil || !ok {
			t.Fatalf("expected (true, nil), got (%v, %v)", ok, err)
		}
	})

	t.Run("new file inside scope is allowed", func(t *testing.T) {
		scope := t.TempDir()
		sfs := NewScopedFs(afero.NewOsFs(), scope)

		ok, err := sfs.within("/does-not-exist-yet.txt")
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
		sfs := NewScopedFs(afero.NewOsFs(), "/")

		ok, err := sfs.within(f)
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
		sfs := NewScopedFs(afero.NewOsFs(), scope)

		ok, err := sfs.within("/escape")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Fatal("expected escaping symlink to a sibling directory to be rejected")
		}
	})

	t.Run("symlink whose target stays within scope is allowed", func(t *testing.T) {
		scope := t.TempDir()
		if err := os.MkdirAll(filepath.Join(scope, "real"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(scope, "real", "f.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(scope, "real"), filepath.Join(scope, "link")); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}
		sfs := NewScopedFs(afero.NewOsFs(), scope)

		ok, err := sfs.within("/link/f.txt")
		if err != nil || !ok {
			t.Fatalf("expected (true, nil) for an in-scope symlink target, got (%v, %v)", ok, err)
		}
	})

	// The escaping symlink must also be rejected at the operation layer, not
	// only by the internal within() check: a guarded call (Stat) returns a
	// permission error so callers that don't pre-check are still protected.
	t.Run("guarded operation rejects escaping symlink", func(t *testing.T) {
		base := t.TempDir()
		scope := filepath.Join(base, "srv")
		secret := filepath.Join(base, "secret.txt")
		if err := os.MkdirAll(scope, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(secret, []byte("OUT-OF-SCOPE"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(secret, filepath.Join(scope, "escape")); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}
		sfs := NewScopedFs(afero.NewOsFs(), scope)

		if _, err := sfs.Stat("/escape"); !errors.Is(err, os.ErrPermission) {
			t.Fatalf("expected os.ErrPermission stating an escaping symlink, got %v", err)
		}
	})

	// Regression for the dangling-symlink write escape (GHSA-8wc8-hf36-mjh9 /
	// GHSA-fh54-6rfh-r8f3): a symlink whose target does not exist yet must not be
	// followed for writes. Previously within() validated the link's in-scope
	// parent directory, so OpenFile(O_CREATE) dereferenced the link and created
	// the file at its out-of-scope target.
	t.Run("write through a dangling escaping symlink is rejected", func(t *testing.T) {
		base := t.TempDir()
		scope := filepath.Join(base, "scope")
		outside := filepath.Join(base, "outside")
		for _, d := range []string{scope, outside} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		outsideTarget := filepath.Join(outside, "created.txt") // does not exist yet
		if err := os.Symlink(outsideTarget, filepath.Join(scope, "evil")); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}
		fs := NewScopedFs(afero.NewOsFs(), scope)

		f, err := fs.OpenFile("/evil", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
		if err == nil {
			_ = f.Close()
			t.Fatal("VULNERABLE: write through a dangling escaping symlink was allowed")
		}
		if !os.IsPermission(err) {
			t.Fatalf("expected permission error, got %v", err)
		}
		if _, statErr := os.Stat(outsideTarget); statErr == nil {
			t.Fatal("VULNERABLE: file was created outside the scope")
		}
	})

	// A dangling *relative* symlink that lives under an escaping directory
	// symlink must be resolved against the link's real directory, not its lexical
	// parent. Otherwise the symlinked ancestor can shift the computed target back
	// into scope while the real OS write lands outside it.
	t.Run("write through a dangling relative symlink under a symlinked dir is rejected", func(t *testing.T) {
		base := t.TempDir()
		scope := filepath.Join(base, "scope")
		outside := filepath.Join(base, "outside")
		for _, d := range []string{scope, outside} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		// An escaping directory symlink inside the scope: /scope/m -> /base/outside.
		if err := os.Symlink(outside, filepath.Join(scope, "m")); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}
		// A relative dangling symlink inside the escaping dir whose target,
		// resolved against the real directory (/base/outside), is /base/escaped —
		// outside the scope.
		if err := os.Symlink("../escaped", filepath.Join(outside, "evil")); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}
		fs := NewScopedFs(afero.NewOsFs(), scope)

		f, err := fs.OpenFile("/m/evil", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
		if err == nil {
			_ = f.Close()
			t.Fatal("VULNERABLE: write through a dangling relative symlink under a symlinked dir was allowed")
		}
		if !os.IsPermission(err) {
			t.Fatalf("expected permission error, got %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(base, "escaped")); statErr == nil {
			t.Fatal("VULNERABLE: file was created outside the scope")
		}
	})

	// Regression for the symlink-following delete escape (GHSA-hq4g-mpch-f9vp /
	// GHSA-fmm7-x4gx-8jhr): Remove/RemoveAll used to skip guard(), so RemoveAll
	// followed a symlinked ancestor escaping the scope and deleted an
	// out-of-scope file.
	t.Run("RemoveAll through an escaping symlink is rejected", func(t *testing.T) {
		base := t.TempDir()
		scope := filepath.Join(base, "scope")
		outside := filepath.Join(base, "outside")
		for _, d := range []string{scope, outside} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		victim := filepath.Join(outside, "victim.txt")
		if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(scope, "link")); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}
		fs := NewScopedFs(afero.NewOsFs(), scope)

		if err := fs.RemoveAll("/link/victim.txt"); !os.IsPermission(err) {
			t.Fatalf("expected RemoveAll through escaping symlink to be rejected, got %v", err)
		}
		if _, statErr := os.Stat(victim); statErr != nil {
			t.Fatalf("VULNERABLE: out-of-scope victim file was deleted: %v", statErr)
		}
	})

	// The guard added for the delete escape must not break legitimate deletes of
	// in-scope files.
	t.Run("RemoveAll of an in-scope file is allowed", func(t *testing.T) {
		scope := t.TempDir()
		target := filepath.Join(scope, "deleteme.txt")
		if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		fs := NewScopedFs(afero.NewOsFs(), scope)

		if err := fs.RemoveAll("/deleteme.txt"); err != nil {
			t.Fatalf("expected in-scope RemoveAll to succeed, got %v", err)
		}
		if _, statErr := os.Stat(target); statErr == nil {
			t.Fatal("expected in-scope file to be deleted")
		}
	})
}

// stat must reject a regular file reached through a symlinked ancestor that
// escapes the scope (GHSA-hf77-9m7w-fq8q), while still serving in-scope files.
func TestStatRejectsLinkedAncestorEscape(t *testing.T) {
	scope := t.TempDir()
	if err := os.MkdirAll(filepath.Join(scope, "shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(scope, "private"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scope, "private", "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scope, "shared", "ok.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(scope, "private"), filepath.Join(scope, "shared", "link")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	// Filesystem scoped to the shared directory, as a public share would be.
	sfs := NewScopedFs(afero.NewOsFs(), filepath.Join(scope, "shared"))

	if _, err := stat(&FileOptions{Fs: sfs, Path: "/link/secret.txt"}); !os.IsPermission(err) {
		t.Fatalf("expected permission error for linked-ancestor escape, got %v", err)
	}
	if _, err := stat(&FileOptions{Fs: sfs, Path: "/ok.txt"}); err != nil {
		t.Fatalf("expected in-scope file to be served, got %v", err)
	}
}

func TestDetectType_Model(t *testing.T) {
	t.Parallel()

	// ASCII and binary variants of the same format must resolve to the same
	// type: the ASCII ones are valid UTF-8 and would otherwise be typed "text",
	// which routes them to the editor instead of the 3D previewer.
	cases := []struct {
		name     string
		contents []byte
		want     string
	}{
		{"ascii.stl", []byte("solid cube\nfacet normal 0 0 1\nendsolid cube\n"), "model"},
		{"binary.stl", append([]byte("STL binary header"), 0x00, 0x01, 0x02, 0xff), "model"},
		{"mesh.obj", []byte("v 0.0 0.0 0.0\nv 1.0 0.0 0.0\nf 1 2 3\n"), "model"},
		{"scene.gltf", []byte(`{"asset":{"version":"2.0"}}`), "model"},
		{"scene.glb", append([]byte("glTF"), 0x02, 0x00, 0x00, 0x00), "model"},
		{"cloud.ply", []byte("ply\nformat ascii 1.0\nend_header\n"), "model"},
		{"print.3mf", []byte("PK\x03\x04binary-ish"), "model"},
		{"UPPER.STL", []byte("solid cube\nendsolid cube\n"), "model"},
		{"notes.txt", []byte("just text\n"), "text"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			if err := afero.WriteFile(fs, "/"+tc.name, tc.contents, 0o644); err != nil {
				t.Fatal(err)
			}

			fi := &FileInfo{
				Fs:        fs,
				Path:      "/" + tc.name,
				Name:      tc.name,
				Extension: filepath.Ext(tc.name),
				Size:      int64(len(tc.contents)),
			}

			if err := fi.detectType(true, false, true, false); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fi.Type != tc.want {
				t.Errorf("expected type %q, got %q", tc.want, fi.Type)
			}
		})
	}
}

func TestDetectType_Image(t *testing.T) {
	t.Parallel()

	// Only formats a browser can draw — or that img.Service can re-encode into
	// one, i.e. TIFF — should be typed "image". Everything else the mime tables
	// call an image has no decoder on either side and would render as a broken
	// <img>, so it must fall back to "blob" and get the download panel.
	//
	// The raw camera extensions resolve through the system /etc/mime.types,
	// which the Docker image ships via mailcap; skipped where absent so this
	// does not fail on a bare host.
	cases := []struct {
		name       string
		contents   []byte
		want       string
		needsMime  bool
		readHeader bool
	}{
		{name: "photo.png", contents: pngBytes(t), want: "image", readHeader: true},
		{name: "photo.jpg", contents: []byte("\xff\xd8\xff\xe0stub"), want: "image", readHeader: true},
		{name: "anim.gif", contents: []byte("GIF89astub"), want: "image", readHeader: true},
		{name: "photo.webp", contents: []byte("RIFF\x00\x00\x00\x00WEBPstub"), want: "image", readHeader: true},
		{name: "icon.svg", contents: []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"/>"), want: "image", readHeader: true},
		{name: "scan.tif", contents: []byte("II*\x00stub"), want: "image", readHeader: true},
		{name: "scan.tiff", contents: []byte("II*\x00stub"), want: "image", readHeader: true},
		{name: "UPPER.TIFF", contents: []byte("II*\x00stub"), want: "image", readHeader: true},

		// Raw camera files: TIFF containers, but nothing in the stack decodes them.
		{name: "shot.dng", contents: []byte("II*\x00raw"), want: "blob", needsMime: true, readHeader: true},
		{name: "shot.cr2", contents: []byte("II*\x00raw"), want: "blob", needsMime: true, readHeader: true},
		{name: "shot.nef", contents: []byte("II*\x00raw"), want: "blob", needsMime: true, readHeader: true},
		{name: "shot.arw", contents: []byte("II*\x00raw"), want: "blob", needsMime: true, readHeader: true},

		// Legacy image/* entries from mime.go that no browser renders.
		{name: "old.pcx", contents: []byte("\x0a\x05\x01\x08stub"), want: "blob", readHeader: true},
		{name: "old.xwd", contents: []byte("\x00\x00\x00\x07stub"), want: "blob", readHeader: true},
		{name: "old.ppm", contents: []byte("P6 1 1 255 \x00\x00\x00"), want: "blob", readHeader: true},

		// Regression guard: with readHeader off the sniff buffer is empty and
		// isBinary reports false for it, so a non-previewable image must not
		// fall through to the text case and get its contents inlined.
		{name: "noheader.dng", contents: []byte("II*\x00raw"), want: "blob", needsMime: true},
		{name: "noheader.pcx", contents: []byte("\x0a\x05\x01\x08stub"), want: "blob"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.needsMime && !strings.HasPrefix(mime.TypeByExtension(filepath.Ext(tc.name)), "image/") {
				t.Skipf("no system mime entry for %s", filepath.Ext(tc.name))
			}

			fs := afero.NewMemMapFs()
			if err := afero.WriteFile(fs, "/"+tc.name, tc.contents, 0o644); err != nil {
				t.Fatal(err)
			}

			fi := &FileInfo{
				Fs:        fs,
				Path:      "/" + tc.name,
				Name:      tc.name,
				Extension: filepath.Ext(tc.name),
				Size:      int64(len(tc.contents)),
			}

			if err := fi.detectType(true, true, tc.readHeader, false); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fi.Type != tc.want {
				t.Errorf("expected type %q, got %q", tc.want, fi.Type)
			}
			if fi.Type == "blob" && fi.Content != "" {
				t.Errorf("blob must not carry inlined content, got %d bytes", len(fi.Content))
			}
		})
	}
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	buf := &bytes.Buffer{}
	if err := png.Encode(buf, image.NewGray(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
