package fbhttp

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	gopath "path"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
)

// zstdDecoderMaxMemory caps the window a single zstd frame may ask us to
// allocate. A frame header names its own window size, so without a ceiling a
// crafted header makes us reserve a large buffer before a byte of payload is
// decoded — a cheap way to push the process into the OOM killer.
const zstdDecoderMaxMemory = 64 << 20 // 64 MiB

// compression is the stream codec wrapped around an archive, or applied on its
// own to a single file.
type compression int

const (
	compressNone compression = iota
	compressGzip
	compressZstd
	compressLz4
)

// newWriter wraps w in the codec. The returned closer must be closed to flush
// the trailer; closing it does not close w.
func (c compression) newWriter(w io.Writer) (io.WriteCloser, error) {
	switch c {
	case compressNone:
		return nopWriteCloser{w}, nil
	case compressGzip:
		return gzip.NewWriter(w), nil
	case compressZstd:
		return zstd.NewWriter(w)
	case compressLz4:
		return lz4.NewWriter(w), nil
	default:
		return nil, errors.New("unknown compression")
	}
}

// newReader wraps r in the codec's decoder.
func (c compression) newReader(r io.Reader) (io.ReadCloser, error) {
	switch c {
	case compressNone:
		return io.NopCloser(r), nil
	case compressGzip:
		zr, err := gzip.NewReader(r)
		if err != nil {
			return nil, err
		}
		return zr, nil
	case compressZstd:
		zr, err := zstd.NewReader(r, zstd.WithDecoderMaxMemory(zstdDecoderMaxMemory))
		if err != nil {
			return nil, err
		}
		return zr.IOReadCloser(), nil
	case compressLz4:
		return io.NopCloser(lz4.NewReader(r)), nil
	default:
		return nil, errors.New("unknown compression")
	}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// container is the archive layout inside a (possibly compressed) stream.
type container int

const (
	// containerNone is a bare compressed file: one payload, no member names.
	containerNone container = iota
	containerTar
	containerZip
)

// magic is the leading byte signature a codec or container must present. It is
// checked against the extension we dispatched on so that a file named ".zip"
// that is really something else is refused up front, rather than being handed
// to a decoder that was picked by name alone.
func (c compression) magic() []byte {
	switch c {
	case compressGzip:
		return []byte{0x1f, 0x8b}
	case compressZstd:
		return []byte{0x28, 0xb5, 0x2f, 0xfd}
	case compressLz4:
		return []byte{0x04, 0x22, 0x4d, 0x18}
	default:
		return nil
	}
}

// archiveEntry is one member of an archive being built.
type archiveEntry struct {
	info os.FileInfo
	// nameInArchive is the member's path, already normalized and verified by
	// the caller to be root-relative with no ".." segments.
	nameInArchive string
	open          func() (io.ReadCloser, error)
}

// writeArchive streams entries to w in the requested layout. Only regular files
// and directories are emitted: anything else (symlink, device, socket, fifo)
// is skipped, so an archive can never carry a link for the downloader's
// extractor to follow.
func writeArchive(ctx context.Context, w io.Writer, cont container, comp compression, entries []archiveEntry) error {
	switch cont {
	case containerZip:
		return writeZipArchive(ctx, w, entries)
	case containerTar:
		return writeTarArchive(ctx, w, comp, entries)
	default:
		return errors.New("unsupported archive container")
	}
}

func writeZipArchive(ctx context.Context, w io.Writer, entries []archiveEntry) error {
	zw := zip.NewWriter(w)

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.info.Mode().IsRegular() && !entry.info.IsDir() {
			continue
		}

		header, err := zip.FileInfoHeader(entry.info)
		if err != nil {
			return err
		}
		header.Name = entry.nameInArchive
		if entry.info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}

		dst, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		if entry.info.IsDir() {
			continue
		}

		if err := copyEntry(dst, entry); err != nil {
			return err
		}
	}

	return zw.Close()
}

func writeTarArchive(ctx context.Context, w io.Writer, comp compression, entries []archiveEntry) error {
	cw, err := comp.newWriter(w)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(cw)

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.info.Mode().IsRegular() && !entry.info.IsDir() {
			continue
		}

		// The empty link target is safe here because non-regular, non-directory
		// entries were already skipped, so this never describes a link.
		header, err := tar.FileInfoHeader(entry.info, "")
		if err != nil {
			return err
		}
		header.Name = entry.nameInArchive
		if entry.info.IsDir() {
			header.Name += "/"
		}

		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if entry.info.IsDir() {
			continue
		}

		if err := copyEntry(tw, entry); err != nil {
			return err
		}
	}

	if err := tw.Close(); err != nil {
		return err
	}
	return cw.Close()
}

func copyEntry(dst io.Writer, entry archiveEntry) error {
	src, err := entry.open()
	if err != nil {
		return err
	}
	defer src.Close()

	_, err = io.Copy(dst, src)
	return err
}

// checkMagic verifies that the first bytes of r match want, then reports
// whether the stream should be rewound by the caller. It reads at most
// len(want) bytes.
func checkMagic(r io.ReaderAt, want []byte, label string) error {
	if len(want) == 0 {
		return nil
	}

	got := make([]byte, len(want))
	n, err := r.ReadAt(got, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("failed to read archive header: %w", err)
	}
	if n < len(want) || !bytes.Equal(got, want) {
		return fmt.Errorf("file does not look like a %s archive", label)
	}
	return nil
}

// tarMagicOffset is where the POSIX ustar signature lives in a tar header
// block.
const tarMagicOffset = 257

// checkTarMagic verifies the ustar signature of an uncompressed tar. Very old
// v7 tars have no signature at all, so an absent one is not treated as fatal —
// the tar reader itself rejects a stream that is not a tar.
func checkTarMagic(r io.ReaderAt) error {
	got := make([]byte, 5)
	n, err := r.ReadAt(got, tarMagicOffset)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("failed to read archive header: %w", err)
	}
	if n == 5 && !bytes.Equal(got, []byte("ustar")) {
		return errors.New("file does not look like a tar archive")
	}
	return nil
}

func resolveInside(destDir, name string) (string, error) {
	root := gopath.Clean(destDir)
	target := gopath.Join(root, name)

	if target == root {
		return target, nil
	}

	prefix := root
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	if !strings.HasPrefix(target, prefix) {
		return "", fmt.Errorf("illegal file path in archive: %s", name)
	}
	return target, nil
}

func safeArchiveName(name string) (string, error) {
	// A backslash is a path separator to a Windows-authored archive but a legal
	// filename byte on POSIX. Neutralize it rather than translating it, so that
	// "..\..\x" cannot become a traversal.
	name = strings.ReplaceAll(name, "\\", "_")

	cleaned := gopath.Clean(name)
	if cleaned == "" || cleaned == "." {
		return "", nil
	}

	if strings.HasPrefix(cleaned, "/") || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", fmt.Errorf("illegal file path in archive: %s", name)
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == ".." {
			return "", fmt.Errorf("illegal file path in archive: %s", name)
		}
	}

	return cleaned, nil
}
