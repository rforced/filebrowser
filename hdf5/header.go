package hdf5

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Message types this reader acts on. Everything else is skipped.
const (
	msgNIL          = 0x0000
	msgDataspace    = 0x0001
	msgDatatype     = 0x0003
	msgLayout       = 0x0008
	msgAttribute    = 0x000C
	msgContinuation = 0x0010
	msgSymbolTable  = 0x0011
)

type message struct {
	typ  uint16
	data []byte
}

// objectHeader is the parsed v1 header of one object: enough to tell a group
// from a dataset and to read either.
type objectHeader struct {
	messages []message
}

// readObjectHeader walks a v1 object header, following continuation blocks.
//
// v1 layout: version, reserved, message count (2), reference count (4),
// header size (4), 4 bytes of padding, then messages aligned to 8 bytes. Each
// message is type (2), size (2), flags (1), 3 reserved, then size bytes of
// data already padded to a multiple of 8.
func (f *File) readObjectHeader(addr uint64) (*objectHeader, error) {
	head, err := f.readAt(addr, 16)
	if err != nil {
		return nil, err
	}
	if head[0] != 1 {
		return nil, fmt.Errorf("%w: object header version %d", ErrUnsupported, head[0])
	}

	count := int(binary.LittleEndian.Uint16(head[2:4]))
	bodySize := int(binary.LittleEndian.Uint32(head[8:12]))
	if bodySize < 0 || bodySize > maxObjectHeader {
		return nil, fmt.Errorf("%w: object header size %d", ErrNotHDF5, bodySize)
	}

	oh := &objectHeader{}
	blocks := []struct {
		addr uint64
		size int
	}{{addr + 16, bodySize}}

	// A header is one block plus however many continuations it points at. The
	// message count spans all of them, so decoding stops on the count rather
	// than on any single block's end.
	for len(blocks) > 0 && len(oh.messages) < count {
		blk := blocks[0]
		blocks = blocks[1:]

		buf, err := f.readAt(blk.addr, blk.size)
		if err != nil {
			return nil, err
		}

		for pos := 0; pos+8 <= len(buf) && len(oh.messages) < count; {
			typ := binary.LittleEndian.Uint16(buf[pos : pos+2])
			size := int(binary.LittleEndian.Uint16(buf[pos+2 : pos+4]))
			pos += 8
			if size < 0 || pos+size > len(buf) {
				break
			}
			data := buf[pos : pos+size]
			pos += size

			switch typ {
			case msgNIL:
			case msgContinuation:
				if len(data) < int(f.offsetSz)+int(f.lengthSz) {
					return nil, fmt.Errorf("%w: short continuation message", ErrNotHDF5)
				}
				next := f.offset(data)
				nextSize := f.length(data[f.offsetSz:])
				if nextSize > maxObjectHeader {
					return nil, fmt.Errorf("%w: continuation size %d", ErrNotHDF5, nextSize)
				}
				blocks = append(blocks, struct {
					addr uint64
					size int
				}{next, int(nextSize)})
				oh.messages = append(oh.messages, message{typ: typ})
			default:
				oh.messages = append(oh.messages, message{typ: typ, data: data})
			}
		}
	}

	return oh, nil
}

func (oh *objectHeader) first(typ uint16) []byte {
	for i := range oh.messages {
		if oh.messages[i].typ == typ {
			return oh.messages[i].data
		}
	}
	return nil
}

// dataspace is the shape of a dataset or attribute.
type dataspace struct {
	dims []uint64
}

// count multiplies the dimensions, reporting false if the product overflows.
// A corrupt dataspace can claim dimensions whose product wraps, which would
// otherwise turn a bounds check into a pass and index out of range.
func (d dataspace) count() (uint64, bool) {
	if len(d.dims) == 0 {
		return 1, true // scalar
	}
	n := uint64(1)
	for _, v := range d.dims {
		if v == 0 {
			return 0, true
		}
		if n > math.MaxUint64/v {
			return 0, false
		}
		n *= v
	}
	return n, true
}

// parseDataspace handles versions 1 and 2. Only the current dimensions are
// used; max dimensions and permutation indices are skipped.
func (f *File) parseDataspace(b []byte) (dataspace, error) {
	if len(b) < 8 {
		return dataspace{}, fmt.Errorf("%w: short dataspace message", ErrNotHDF5)
	}
	version := b[0]
	if version != 1 && version != 2 {
		return dataspace{}, fmt.Errorf("%w: dataspace version %d", ErrUnsupported, version)
	}
	rank := int(b[1])

	// v1 reserves bytes 4-7; v2 replaces them with a type byte at 4.
	pos := 8
	if version == 2 {
		if b[4] == 0 {
			rank = 0 // scalar
		}
	}

	ds := dataspace{}
	l := int(f.lengthSz)
	for i := 0; i < rank; i++ {
		if pos+l > len(b) {
			return dataspace{}, fmt.Errorf("%w: truncated dataspace dims", ErrNotHDF5)
		}
		ds.dims = append(ds.dims, f.length(b[pos:]))
		pos += l
	}
	return ds, nil
}

const (
	layoutCompact    = 0
	layoutContiguous = 1
	layoutChunked    = 2
)

type layout struct {
	class   uint8
	addr    uint64
	size    uint64
	compact []byte
}

// parseLayout handles data layout message versions 1, 2 and 3. Chunked
// datasets are recognised but rejected: CONVERGE does not write them, and
// supporting them would mean implementing chunk B-trees and the filter
// pipeline for no gain.
func (f *File) parseLayout(b []byte) (layout, error) {
	if len(b) < 2 {
		return layout{}, fmt.Errorf("%w: short layout message", ErrNotHDF5)
	}
	version := b[0]
	o := int(f.offsetSz)

	switch version {
	case 1, 2:
		rank := int(b[1])
		class := b[2]
		pos := 8
		if class == layoutCompact {
			// Compact data follows the dimension list.
			pos += rank * 4
			if pos+4 > len(b) {
				return layout{}, fmt.Errorf("%w: truncated compact layout", ErrNotHDF5)
			}
			n := int(binary.LittleEndian.Uint32(b[pos:]))
			pos += 4
			if pos+n > len(b) {
				return layout{}, fmt.Errorf("%w: truncated compact data", ErrNotHDF5)
			}
			return layout{class: layoutCompact, compact: b[pos : pos+n]}, nil
		}
		if pos+o > len(b) {
			return layout{}, fmt.Errorf("%w: truncated layout address", ErrNotHDF5)
		}
		addr := f.offset(b[pos:])
		if class == layoutChunked {
			return layout{}, fmt.Errorf("%w: chunked dataset", ErrUnsupported)
		}
		return layout{class: layoutContiguous, addr: addr}, nil

	case 3, 4:
		class := b[1]
		pos := 2
		switch class {
		case layoutCompact:
			if pos+2 > len(b) {
				return layout{}, fmt.Errorf("%w: truncated compact layout", ErrNotHDF5)
			}
			n := int(binary.LittleEndian.Uint16(b[pos:]))
			pos += 2
			if pos+n > len(b) {
				return layout{}, fmt.Errorf("%w: truncated compact data", ErrNotHDF5)
			}
			return layout{class: layoutCompact, compact: b[pos : pos+n]}, nil
		case layoutContiguous:
			if pos+o+int(f.lengthSz) > len(b) {
				return layout{}, fmt.Errorf("%w: truncated contiguous layout", ErrNotHDF5)
			}
			return layout{
				class: layoutContiguous,
				addr:  f.offset(b[pos:]),
				size:  f.length(b[pos+o:]),
			}, nil
		case layoutChunked:
			return layout{}, fmt.Errorf("%w: chunked dataset", ErrUnsupported)
		default:
			return layout{}, fmt.Errorf("%w: layout class %d", ErrUnsupported, class)
		}
	}

	return layout{}, fmt.Errorf("%w: layout version %d", ErrUnsupported, version)
}
