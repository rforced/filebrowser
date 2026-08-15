package fbhttp

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
	"github.com/spf13/afero"
)

// --- helpers -----------------------------------------------------------------

// compress wraps body in the named codec, producing the bytes a real archive of
// that type would have.
func compressBytes(t *testing.T, comp compression, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := comp.newWriter(&buf)
	if err != nil {
		t.Fatalf("newWriter: %v", err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return buf.Bytes()
}

type tarEntry struct {
	name     string
	body     string
	typeflag byte
	linkname string
	mode     int64
}

// makeTar builds an uncompressed tar from entries, bypassing any sanitization so
// that hostile names can be planted.
func makeTar(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		typeflag := e.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     mode,
			Size:     int64(len(e.body)),
			Typeflag: typeflag,
			Linkname: e.linkname,
			ModTime:  time.Unix(1700000000, 0),
		}
		if typeflag != tar.TypeReg {
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header %q: %v", e.name, err)
		}
		if typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatalf("tar body %q: %v", e.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	return buf.Bytes()
}

type zipEntry struct {
	name string
	body string
	mode os.FileMode
}

func makeZip(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			hdr.SetMode(e.mode)
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("zip header %q: %v", e.name, err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatalf("zip body %q: %v", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// extractInto writes archive to a fresh in-memory FS under name and extracts it
// to /out, returning the FS and any extraction error.
func extractInto(t *testing.T, name string, archive []byte) (afero.Fs, error) {
	t.Helper()
	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "/"+name, archive, 0o644); err != nil {
		t.Fatalf("seed archive: %v", err)
	}
	err := performExtraction(context.Background(), fs, "/"+name, "/out", false, func(extractProgress) {})
	return fs, err
}

func readFile(t *testing.T, fs afero.Fs, path string) string {
	t.Helper()
	b, err := afero.ReadFile(fs, path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// --- extraction round-trips ---------------------------------------------------

// Every supported extension must decode to the same contents. This is the whole
// point of the mholt/archives replacement: the formats we advertise still work.
func TestExtractRoundTripAllFormats(t *testing.T) {
	members := []tarEntry{
		{name: "dir/", typeflag: tar.TypeDir},
		{name: "dir/hello.txt", body: "hello"},
		{name: "top.txt", body: "top"},
	}
	rawTar := makeTar(t, members)

	tarCases := []struct {
		filename string
		comp     compression
	}{
		{"a.tar.gz", compressGzip},
		{"a.tgz", compressGzip},
		{"a.tar.zst", compressZstd},
		{"a.tzst", compressZstd},
		{"a.tar.lz4", compressLz4},
		{"a.tlz4", compressLz4},
	}

	for _, tc := range tarCases {
		t.Run(tc.filename, func(t *testing.T) {
			fs, err := extractInto(t, tc.filename, compressBytes(t, tc.comp, rawTar))
			if err != nil {
				t.Fatalf("extract: %v", err)
			}
			if got := readFile(t, fs, "/out/dir/hello.txt"); got != "hello" {
				t.Errorf("dir/hello.txt = %q, want %q", got, "hello")
			}
			if got := readFile(t, fs, "/out/top.txt"); got != "top" {
				t.Errorf("top.txt = %q, want %q", got, "top")
			}
		})
	}

	t.Run("a.zip", func(t *testing.T) {
		archive := makeZip(t, []zipEntry{
			{name: "dir/hello.txt", body: "hello"},
			{name: "top.txt", body: "top"},
		})
		fs, err := extractInto(t, "a.zip", archive)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}
		if got := readFile(t, fs, "/out/dir/hello.txt"); got != "hello" {
			t.Errorf("dir/hello.txt = %q, want %q", got, "hello")
		}
		if got := readFile(t, fs, "/out/top.txt"); got != "top" {
			t.Errorf("top.txt = %q, want %q", got, "top")
		}
	})

	// Bare compressed files decode to a single output named after the archive
	// with its extension stripped.
	singles := []struct {
		filename string
		want     string
		comp     compression
	}{
		{"notes.txt.gz", "/out/notes.txt", compressGzip},
		{"notes.txt.zst", "/out/notes.txt", compressZstd},
		{"notes.txt.lz4", "/out/notes.txt", compressLz4},
	}
	for _, tc := range singles {
		t.Run(tc.filename, func(t *testing.T) {
			fs, err := extractInto(t, tc.filename, compressBytes(t, tc.comp, []byte("plain body")))
			if err != nil {
				t.Fatalf("extract: %v", err)
			}
			if got := readFile(t, fs, tc.want); got != "plain body" {
				t.Errorf("%s = %q, want %q", tc.want, got, "plain body")
			}
		})
	}
}

// --- extraction security ------------------------------------------------------

// A member name that climbs out of the destination must abort the extraction,
// and must not have written the escaping file first.
func TestExtractRejectsTraversal(t *testing.T) {
	cases := []struct {
		name    string
		archive func() []byte
		file    string
	}{
		{
			name: "tar parent traversal",
			archive: func() []byte {
				return compressBytes(t, compressGzip, makeTar(t, []tarEntry{
					{name: "../escaped.txt", body: "pwned"},
				}))
			},
			file: "evil.tar.gz",
		},
		{
			name: "tar absolute path",
			archive: func() []byte {
				return compressBytes(t, compressGzip, makeTar(t, []tarEntry{
					{name: "/etc/passwd", body: "pwned"},
				}))
			},
			file: "evil.tar.gz",
		},
		{
			name: "tar deep traversal",
			archive: func() []byte {
				return compressBytes(t, compressGzip, makeTar(t, []tarEntry{
					{name: "a/../../../escaped.txt", body: "pwned"},
				}))
			},
			file: "evil.tar.gz",
		},
		{
			name: "zip parent traversal",
			archive: func() []byte {
				return makeZip(t, []zipEntry{{name: "../escaped.txt", body: "pwned"}})
			},
			file: "evil.zip",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs, err := extractInto(t, tc.file, tc.archive())
			if err == nil {
				t.Fatal("VULNERABLE: traversal entry was accepted")
			}
			if !strings.Contains(err.Error(), "illegal file path") {
				t.Errorf("expected an illegal-path error, got %v", err)
			}
			// Nothing may have landed outside the destination.
			for _, p := range []string{"/escaped.txt", "/etc/passwd", "/out/../escaped.txt"} {
				if ok, _ := afero.Exists(fs, p); ok {
					t.Errorf("VULNERABLE: %s was written", p)
				}
			}
		})
	}
}

// Symlinks and hard links are the classic route out of an extraction directory.
// They must be skipped, while the regular members around them still extract.
func TestExtractSkipsLinksAndSpecialFiles(t *testing.T) {
	archive := compressBytes(t, compressGzip, makeTar(t, []tarEntry{
		{name: "good.txt", body: "fine"},
		{name: "link", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"},
		{name: "hard", typeflag: tar.TypeLink, linkname: "good.txt"},
		{name: "dev", typeflag: tar.TypeChar},
		{name: "pipe", typeflag: tar.TypeFifo},
	}))

	fs, err := extractInto(t, "links.tar.gz", archive)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if got := readFile(t, fs, "/out/good.txt"); got != "fine" {
		t.Errorf("good.txt = %q, want %q", got, "fine")
	}
	for _, p := range []string{"/out/link", "/out/hard", "/out/dev", "/out/pipe"} {
		if ok, _ := afero.Exists(fs, p); ok {
			t.Errorf("VULNERABLE: non-regular member %s was materialized", p)
		}
	}
}

// A zip symlink is a member whose body is the link target. Honouring one would
// let an archive plant a link pointing anywhere on the host.
func TestExtractSkipsZipSymlink(t *testing.T) {
	archive := makeZip(t, []zipEntry{
		{name: "good.txt", body: "fine"},
		{name: "link", body: "/etc/passwd", mode: os.ModeSymlink | 0o777},
	})

	fs, err := extractInto(t, "links.zip", archive)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if got := readFile(t, fs, "/out/good.txt"); got != "fine" {
		t.Errorf("good.txt = %q, want %q", got, "fine")
	}
	if ok, _ := afero.Exists(fs, "/out/link"); ok {
		t.Error("VULNERABLE: zip symlink member was materialized")
	}
}

// The format is chosen from the filename, so a file whose bytes disagree with
// its extension must be refused before any decoder sees it.
func TestExtractRejectsMagicMismatch(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		content []byte
	}{
		{"gzip extension, not gzip", "fake.tar.gz", []byte("this is not gzip at all")},
		{"zstd extension, not zstd", "fake.tar.zst", []byte("this is not zstd at all")},
		{"lz4 extension, not lz4", "fake.tar.lz4", []byte("this is not lz4 at all")},
		{"zip extension, not zip", "fake.zip", []byte("this is not a zip at all")},
		// A real gzip stream mislabelled as zstd must not be decoded as gzip.
		{"gzip bytes labelled zstd", "mislabelled.zst", compressBytes(t, compressGzip, []byte("x"))},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := extractInto(t, tc.file, tc.content)
			if err == nil {
				t.Fatal("expected a format mismatch to be rejected")
			}
			if !strings.Contains(err.Error(), "does not look like") {
				t.Errorf("expected a magic-mismatch error, got %v", err)
			}
		})
	}
}

// An unsupported extension must never reach a decoder.
func TestExtractRejectsUnsupportedFormat(t *testing.T) {
	for _, name := range []string{"archive.tar", "archive.rar", "archive.7z", "archive.tar.bz2", "plain.txt"} {
		t.Run(name, func(t *testing.T) {
			if isArchiveFile(name) {
				t.Fatalf("%s must not be treated as a supported archive", name)
			}
			_, err := extractInto(t, name, []byte("whatever"))
			if err == nil {
				t.Fatal("expected an unsupported-format error")
			}
		})
	}
}

// The entry-count ceiling must stop a tar with an absurd number of members.
func TestExtractEnforcesFileCountLimit(t *testing.T) {
	var entries []tarEntry
	for i := 0; i < extractMaxFiles+10; i++ {
		entries = append(entries, tarEntry{name: "f" + strconv.Itoa(i), body: "x"})
	}
	archive := compressBytes(t, compressGzip, makeTar(t, entries))

	_, err := extractInto(t, "many.tar.gz", archive)
	if err == nil {
		t.Fatal("expected the file-count limit to be enforced")
	}
	if !strings.Contains(err.Error(), "maximum file count") {
		t.Errorf("expected a file-count error, got %v", err)
	}
}

// The byte budget must be charged against what is actually decoded, not what the
// archive claims, so a highly compressible payload cannot blow past it.
func TestExtractEnforcesByteLimit(t *testing.T) {
	state := &extractState{totalBytes: extractMaxTotalBytes - 4}
	fs := afero.NewMemMapFs()

	err := writeExtractedFile(fs, "/out/big.bin", "big.bin", strings.NewReader(strings.Repeat("A", 64)), false, state)
	if err == nil {
		t.Fatal("expected the total-size limit to be enforced")
	}
	if !strings.Contains(err.Error(), "maximum total decompressed size") {
		t.Errorf("expected a size-limit error, got %v", err)
	}
}

// Extraction must stop when the request is cancelled rather than running the
// archive to completion.
func TestExtractHonoursContextCancellation(t *testing.T) {
	var entries []tarEntry
	for i := 0; i < 500; i++ {
		entries = append(entries, tarEntry{name: "f" + strconv.Itoa(i), body: "x"})
	}
	archive := compressBytes(t, compressGzip, makeTar(t, entries))

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "/c.tar.gz", archive, 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := performExtraction(ctx, fs, "/c.tar.gz", "/out", false, func(extractProgress) {})
	if err == nil {
		t.Fatal("expected extraction to stop on a cancelled context")
	}
}

// Without overwrite, an existing file must not be clobbered.
func TestExtractRespectsOverwriteFlag(t *testing.T) {
	archive := makeZip(t, []zipEntry{{name: "keep.txt", body: "from archive"}})

	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "/a.zip", archive, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fs.MkdirAll("/out", 0o750); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(fs, "/out/keep.txt", []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := performExtraction(context.Background(), fs, "/a.zip", "/out", false, func(extractProgress) {})
	if err == nil {
		t.Fatal("expected an already-exists error")
	}
	if got := readFile(t, fs, "/out/keep.txt"); got != "original" {
		t.Errorf("existing file was overwritten: %q", got)
	}

	// With overwrite it goes through.
	if err := performExtraction(context.Background(), fs, "/a.zip", "/out", true, func(extractProgress) {}); err != nil {
		t.Fatalf("overwrite extraction: %v", err)
	}
	if got := readFile(t, fs, "/out/keep.txt"); got != "from archive" {
		t.Errorf("overwrite did not take effect: %q", got)
	}
}

// --- archive writing ----------------------------------------------------------

// Each download format must produce a stream the corresponding standard reader
// accepts, with the members we put in.
func TestWriteArchiveRoundTrip(t *testing.T) {
	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "/a.txt", []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(fs, "/b.txt", []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries := func() []archiveEntry {
		var out []archiveEntry
		for _, name := range []string{"a.txt", "b.txt"} {
			info, err := fs.Stat("/" + name)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, archiveEntry{
				info:          info,
				nameInArchive: name,
				open: func() (io.ReadCloser, error) {
					return fs.Open("/" + name)
				},
			})
		}
		return out
	}

	t.Run("zip", func(t *testing.T) {
		var buf bytes.Buffer
		if err := writeArchive(context.Background(), &buf, containerZip, compressNone, entries()); err != nil {
			t.Fatalf("writeArchive: %v", err)
		}
		zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		if err != nil {
			t.Fatalf("zip.NewReader: %v", err)
		}
		got := map[string]string{}
		for _, f := range zr.File {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			got[f.Name] = string(b)
		}
		if got["a.txt"] != "alpha" || got["b.txt"] != "beta" {
			t.Errorf("zip contents = %v", got)
		}
	})

	tarCases := []struct {
		name string
		comp compression
		wrap func(io.Reader) (io.Reader, error)
	}{
		{"tar.gz", compressGzip, func(r io.Reader) (io.Reader, error) { return gzip.NewReader(r) }},
		{"tar.zst", compressZstd, func(r io.Reader) (io.Reader, error) {
			d, err := zstd.NewReader(r)
			if err != nil {
				return nil, err
			}
			return d.IOReadCloser(), nil
		}},
		{"tar.lz4", compressLz4, func(r io.Reader) (io.Reader, error) { return lz4.NewReader(r), nil }},
	}

	for _, tc := range tarCases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := writeArchive(context.Background(), &buf, containerTar, tc.comp, entries()); err != nil {
				t.Fatalf("writeArchive: %v", err)
			}
			dec, err := tc.wrap(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatalf("decompress: %v", err)
			}
			tr := tar.NewReader(dec)
			got := map[string]string{}
			for {
				hdr, err := tr.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("tar read: %v", err)
				}
				b, _ := io.ReadAll(tr)
				got[hdr.Name] = string(b)
			}
			if got["a.txt"] != "alpha" || got["b.txt"] != "beta" {
				t.Errorf("%s contents = %v", tc.name, got)
			}
		})
	}
}

// Only the four advertised download formats are accepted; the rest were removed
// with mholt/archives and must not silently fall back to one that remains.
func TestParseQueryAlgorithm(t *testing.T) {
	supported := map[string]string{
		"":       ".zip",
		"zip":    ".zip",
		"true":   ".zip",
		"targz":  ".tar.gz",
		"tarlz4": ".tar.lz4",
		"tarzst": ".tar.zst",
	}
	for algo, wantExt := range supported {
		t.Run("supported/"+algo, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/?algo="+algo, nil)
			ext, _, _, err := parseQueryAlgorithm(r)
			if err != nil {
				t.Fatalf("algo=%q: %v", algo, err)
			}
			if ext != wantExt {
				t.Errorf("algo=%q ext = %q, want %q", algo, ext, wantExt)
			}
		})
	}

	for _, algo := range []string{"tar", "tarbz2", "tarxz", "tarsz", "tarbr", "rar", "7z"} {
		t.Run("removed/"+algo, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/?algo="+algo, nil)
			if _, _, _, err := parseQueryAlgorithm(r); err == nil {
				t.Errorf("algo=%q should no longer be accepted", algo)
			}
		})
	}
}
