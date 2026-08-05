package fbhttp

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"github.com/spf13/afero"
	"golang.org/x/image/tiff"

	"github.com/rforced/filebrowser/v2/files"
	"github.com/rforced/filebrowser/v2/img"
)

// noopCache satisfies FileCache without persisting anything, so createPreview
// always renders rather than replaying an earlier run's bytes.
type noopCache struct{}

func (noopCache) Store(context.Context, string, []byte) error { return nil }
func (noopCache) Load(context.Context, string) ([]byte, bool, error) {
	return nil, false, nil
}
func (noopCache) Delete(context.Context, string) error { return nil }

func TestCreatePreview_BigPreviewEncoding(t *testing.T) {
	t.Parallel()

	// No mainstream browser draws TIFF, so a big preview of one has to come
	// back re-encoded. PNG must be left alone: forcing JPEG for every big
	// preview would flatten its alpha channel.
	cases := map[string]struct {
		file       string
		write      func(t *testing.T, fs afero.Fs, path string)
		format     img.Format
		wantFormat string
	}{
		"tiff is re-encoded as jpeg": {
			file:       "/scan.tiff",
			write:      writeTIFF,
			format:     img.FormatTiff,
			wantFormat: "jpeg",
		},
		"png keeps its own format": {
			file:       "/photo.png",
			write:      writePNG,
			format:     img.FormatPng,
			wantFormat: "png",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			tc.write(t, fs, tc.file)

			file := &files.FileInfo{
				Fs:      fs,
				Path:    tc.file,
				Name:    tc.file,
				ModTime: time.Unix(0, 0),
			}

			out, err := createPreview(img.New(1), noopCache{}, file, PreviewSizeBig, tc.format)
			if err != nil {
				t.Fatalf("createPreview: %v", err)
			}

			_, got, err := image.DecodeConfig(bytes.NewReader(out))
			if err != nil {
				t.Fatalf("decoding preview: %v", err)
			}
			if got != tc.wantFormat {
				t.Errorf("expected %s preview, got %s", tc.wantFormat, got)
			}
		})
	}
}

func TestCreatePreview_ThumbAlwaysJpeg(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeTIFF(t, fs, "/scan.tiff")

	file := &files.FileInfo{Fs: fs, Path: "/scan.tiff", Name: "/scan.tiff", ModTime: time.Unix(0, 0)}

	out, err := createPreview(img.New(1), noopCache{}, file, PreviewSizeThumb, img.FormatTiff)
	if err != nil {
		t.Fatalf("createPreview: %v", err)
	}

	if _, got, err := image.DecodeConfig(bytes.NewReader(out)); err != nil {
		t.Fatalf("decoding thumbnail: %v", err)
	} else if got != "jpeg" {
		t.Errorf("expected jpeg thumbnail, got %s", got)
	}
}

func TestPreviewCacheKey_VariesByVersionAndSize(t *testing.T) {
	t.Parallel()

	file := &files.FileInfo{Path: "/scan.tiff", ModTime: time.Unix(0, 0)}

	// The key carries only path, mtime and size, so an encoding change has
	// nothing else to invalidate on — hence the version constant.
	if previewCacheKey(file, PreviewSizeBig) == previewCacheKey(file, PreviewSizeThumb) {
		t.Error("big and thumb previews must not share a cache key")
	}
	if previewCacheVersion < 2 {
		t.Errorf("previewCacheVersion must be bumped past the TIFF-encoded era, got %d", previewCacheVersion)
	}
}

func writeTIFF(t *testing.T, fs afero.Fs, path string) {
	t.Helper()
	buf := &bytes.Buffer{}
	if err := tiff.Encode(buf, newTestImage(), nil); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(fs, path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writePNG(t *testing.T, fs afero.Fs, path string) {
	t.Helper()
	buf := &bytes.Buffer{}
	if err := png.Encode(buf, newTestImage()); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(fs, path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newTestImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := range 48 {
		for x := range 64 {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x80, A: 0xff})
		}
	}
	return img
}
