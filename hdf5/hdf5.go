// Package hdf5 reads the narrow dialect of HDF5 that CONVERGE writes.
//
// Verified against CONVERGE 4.1.2 and 6.0.1 output (post*.h5, *.rst, map*.h5,
// *_table.h5): superblock v0, old-style groups (v1 B-tree + local heap +
// symbol table nodes), v1 object headers, and datasets that are always
// contiguous or compact with no filter pipeline. That subset needs none of the
// machinery a general reader does — no fractal heaps, no v2 B-trees, no
// chunk indexing, no decompression — which is what makes a pure-Go
// implementation tractable. cgo is not an option here: the release ships as a
// static linux-amd64 binary.
//
// Anything outside the subset is reported as an error rather than guessed at.
package hdf5

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

var (
	// ErrNotHDF5 means the file does not carry the HDF5 signature.
	ErrNotHDF5 = errors.New("hdf5: not an HDF5 file")
	// ErrUnsupported means the file is HDF5 but uses features outside the
	// CONVERGE subset this reader implements.
	ErrUnsupported = errors.New("hdf5: unsupported HDF5 feature")
	// ErrNotFound means no object exists at the requested path.
	ErrNotFound = errors.New("hdf5: object not found")
)

var signature = [8]byte{0x89, 'H', 'D', 'F', '\r', '\n', 0x1a, '\n'}

// maxObjectHeader caps a single object header read. Real CONVERGE headers are
// a few KB; the bound stops a corrupt length from driving a huge allocation.
const maxObjectHeader = 8 << 20

// maxLinks caps the children of one group, for the same reason.
const maxLinks = 1 << 20

// File is an open HDF5 file. It holds no state beyond the superblock, so all
// reads go straight to the underlying io.ReaderAt.
type File struct {
	r        io.ReaderAt
	size     int64
	offsetSz uint8
	lengthSz uint8
	base     uint64
	rootAddr uint64
}

// Open parses the superblock and locates the root group. It reads only the
// first few hundred bytes; everything else is resolved on demand.
func Open(r io.ReaderAt, size int64) (*File, error) {
	buf := make([]byte, 96)
	n, err := r.ReadAt(buf, 0)
	if err != nil && n < len(buf) {
		if errors.Is(err, io.EOF) {
			return nil, ErrNotHDF5
		}
		return nil, err
	}

	if [8]byte(buf[0:8]) != signature {
		return nil, ErrNotHDF5
	}
	if v := buf[8]; v != 0 {
		return nil, fmt.Errorf("%w: superblock version %d", ErrUnsupported, v)
	}

	f := &File{r: r, size: size, offsetSz: buf[13], lengthSz: buf[14]}
	if f.offsetSz != 4 && f.offsetSz != 8 {
		return nil, fmt.Errorf("%w: offset size %d", ErrUnsupported, f.offsetSz)
	}
	if f.lengthSz != 4 && f.lengthSz != 8 {
		return nil, fmt.Errorf("%w: length size %d", ErrUnsupported, f.lengthSz)
	}

	// v0 superblock: base address follows the 24-byte fixed prefix, then three
	// more offset-sized fields, then the root group's symbol table entry.
	o := int(f.offsetSz)
	f.base = f.offset(buf[24:])
	// Every address in the file is relative to the base. A file whose
	// superblock sits at offset 0 states a base of 0; anything else would
	// have to be added to each address, and reading it as absolute would
	// return whatever bytes happen to live there.
	if f.base != 0 {
		return nil, fmt.Errorf("%w: base address %d", ErrUnsupported, f.base)
	}
	rootEntry := 24 + 4*o
	if len(buf) < rootEntry+2*o+8 {
		return nil, ErrNotHDF5
	}
	// Symbol table entry: link name offset, then the object header address.
	f.rootAddr = f.offset(buf[rootEntry+o:])

	return f, nil
}

func (f *File) offset(b []byte) uint64 {
	if f.offsetSz == 4 {
		return uint64(binary.LittleEndian.Uint32(b))
	}
	return binary.LittleEndian.Uint64(b)
}

func (f *File) length(b []byte) uint64 {
	if f.lengthSz == 4 {
		return uint64(binary.LittleEndian.Uint32(b))
	}
	return binary.LittleEndian.Uint64(b)
}

// undefinedAddr is the all-ones address HDF5 uses for "no such object".
func (f *File) undefined(addr uint64) bool {
	if f.offsetSz == 4 {
		return addr == 0xffffffff
	}
	return addr == 0xffffffffffffffff
}

func (f *File) readAt(addr uint64, n int) ([]byte, error) {
	if n < 0 {
		return nil, fmt.Errorf("%w: negative read", ErrUnsupported)
	}
	if f.size > 0 && (addr > uint64(f.size) || uint64(n) > uint64(f.size)-addr) {
		return nil, fmt.Errorf("%w: read of %d at %d past end of file", ErrNotHDF5, n, addr)
	}
	buf := make([]byte, n)
	if _, err := f.r.ReadAt(buf, int64(addr)); err != nil {
		return nil, err
	}
	return buf, nil
}
