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
	msgLinkInfo     = 0x0002
	msgDatatype     = 0x0003
	msgLink         = 0x0006
	msgLayout       = 0x0008
	msgAttribute    = 0x000C
	msgContinuation = 0x0010
	msgSymbolTable  = 0x0011
)

type message struct {
	typ  uint16
	data []byte
}

// objectHeader is the parsed header of one object: enough to tell a group from
// a dataset and to read either.
type objectHeader struct {
	messages []message
}

const (
	ohdrSignature = "OHDR"
	ochkSignature = "OCHK"
)

// maxHeaderChunks bounds a v2 header's continuation chain. Unlike a v1 header,
// which states its message count up front, a v2 header is read until its
// chunks run out — so a continuation message pointing back at its own chunk
// would otherwise be walked forever.
const maxHeaderChunks = 4096

// readObjectHeader parses either object header generation. v1 headers are
// unsigned and start with a version byte; v2 headers carry an "OHDR"
// signature, which is what tells them apart.
func (f *File) readObjectHeader(addr uint64) (*objectHeader, error) {
	// The fixed part of a v1 header is 16 bytes and of a v2 header 6, so a
	// short read is only fatal to the larger one. Clamping to the file lets a
	// small v2 header at the very end of a file be read at all.
	n := 16
	if f.size > 0 && addr < uint64(f.size) && uint64(n) > uint64(f.size)-addr {
		n = int(uint64(f.size) - addr)
	}
	if n < 6 {
		return nil, fmt.Errorf("%w: object header at %d is truncated", ErrNotHDF5, addr)
	}
	head, err := f.readAt(addr, n)
	if err != nil {
		return nil, err
	}
	if string(head[0:4]) == ohdrSignature {
		return f.readObjectHeaderV2(addr, head)
	}
	if n < 16 {
		return nil, fmt.Errorf("%w: object header at %d is truncated", ErrNotHDF5, addr)
	}
	return f.readObjectHeaderV1(addr, head)
}

// readObjectHeaderV1 walks a v1 object header, following continuation blocks.
//
// v1 layout: version, reserved, message count (2), reference count (4),
// header size (4), 4 bytes of padding, then messages aligned to 8 bytes. Each
// message is type (2), size (2), flags (1), 3 reserved, then size bytes of
// data already padded to a multiple of 8.
func (f *File) readObjectHeaderV1(addr uint64, head []byte) (*objectHeader, error) {
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

// headerChunk is one block of messages: chunk zero, which follows the header
// prefix directly, or a continuation block elsewhere in the file.
type headerChunk struct {
	addr uint64
	size int
	cont bool
}

// readObjectHeaderV2 walks a v2 object header — the generation CGNS files are
// written in.
//
// Three things differ from v1 and all three change the parse. The header
// states the size of its first chunk instead of a message count, so messages
// are read until the chunk ends rather than until a tally is met. Message
// prefixes shrink to a one-byte type and lose the 8-byte alignment padding,
// and gain an optional creation-order field whose presence is announced once,
// in the header flags, rather than per message. And every chunk is signed and
// checksummed: continuation blocks carry an "OCHK" signature and a trailing
// checksum that are not messages and must be stepped over.
//
// The checksums are not verified. They guard against a corrupt file, which the
// bounds checks here already refuse to follow, and computing them would mean
// reading every chunk twice.
func (f *File) readObjectHeaderV2(addr uint64, head []byte) (*objectHeader, error) {
	if head[4] != 2 {
		return nil, fmt.Errorf("%w: object header version %d", ErrUnsupported, head[4])
	}
	flags := head[5]

	pos := 6
	if flags&0x20 != 0 {
		pos += 16 // access, modification, change and birth times
	}
	if flags&0x10 != 0 {
		pos += 4 // non-default attribute storage phase change values
	}
	// The low two bits say how wide the chunk size field is: 1, 2, 4 or 8 bytes.
	sizeBytes := 1 << (flags & 0x03)
	prefix, err := f.readAt(addr, pos+sizeBytes)
	if err != nil {
		return nil, err
	}
	chunkSize := decodeUint(prefix[pos:], sizeBytes)
	if chunkSize > maxObjectHeader {
		return nil, fmt.Errorf("%w: object header size %d", ErrNotHDF5, chunkSize)
	}

	// A message prefix is type, size and flags, plus a creation order the whole
	// header either carries or does not.
	prefixLen := 4
	if flags&0x04 != 0 {
		prefixLen = 6
	}

	oh := &objectHeader{}
	pending := []headerChunk{{addr: addr + uint64(pos+sizeBytes), size: int(chunkSize)}}
	for chunks := 0; len(pending) > 0; chunks++ {
		if chunks >= maxHeaderChunks {
			return nil, fmt.Errorf("%w: object header spans more than %d chunks",
				ErrNotHDF5, maxHeaderChunks)
		}
		c := pending[0]
		pending = pending[1:]
		if c.size < 0 || c.size > maxObjectHeader {
			return nil, fmt.Errorf("%w: object header chunk size %d", ErrNotHDF5, c.size)
		}
		buf, err := f.readAt(c.addr, c.size)
		if err != nil {
			return nil, err
		}
		body := buf
		if c.cont {
			if c.size < 8 || string(buf[0:4]) != ochkSignature {
				return nil, fmt.Errorf("%w: bad object header continuation signature", ErrNotHDF5)
			}
			body = buf[4 : c.size-4]
		}

		// A chunk can end with a gap too small to hold another prefix, which is
		// what stops the loop rather than an explicit terminator.
		for p := 0; p+prefixLen <= len(body); {
			typ := uint16(body[p])
			size := int(binary.LittleEndian.Uint16(body[p+1 : p+3]))
			msgFlags := body[p+3]
			p += prefixLen
			if size > len(body)-p {
				break
			}
			data := body[p : p+size]
			p += size

			// A shared message holds a reference to the file's shared message
			// table instead of the message itself. Nothing CONVERGE writes uses
			// them; decoding the reference as the message would produce a
			// plausible-looking datatype out of an index, so it is dropped and
			// whatever needed it reports the message as missing.
			if msgFlags&0x02 != 0 {
				continue
			}

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
				pending = append(pending, headerChunk{addr: next, size: int(nextSize), cont: true})
				oh.messages = append(oh.messages, message{typ: typ})
			default:
				oh.messages = append(oh.messages, message{typ: typ, data: data})
			}
		}
	}

	return oh, nil
}

// decodeUint reads a little-endian unsigned integer of 1, 2, 4 or 8 bytes.
// HDF5 sizes several fields this way, naming the width in a nearby flag.
func decodeUint(b []byte, n int) uint64 {
	if n > len(b) {
		n = len(b)
	}
	var v uint64
	for i := n - 1; i >= 0; i-- {
		v = v<<8 | uint64(b[i])
	}
	return v
}

func (oh *objectHeader) first(typ uint16) []byte {
	for i := range oh.messages {
		if oh.messages[i].typ == typ {
			return oh.messages[i].data
		}
	}
	return nil
}

// all returns every message of one type. Link messages are the reason it
// exists: a group under the compact threshold stores one per child.
func (oh *objectHeader) all(typ uint16) [][]byte {
	var out [][]byte
	for i := range oh.messages {
		if oh.messages[i].typ == typ {
			out = append(out, oh.messages[i].data)
		}
	}
	return out
}

// dataspace is the shape of a dataset or attribute.
type dataspace struct {
	dims []uint64
	// null marks an H5S_NULL space. It carries no dimensions, like a scalar,
	// but holds no elements at all — the difference decides whether a
	// dataset reads as one fabricated zero or as empty.
	null bool
}

// count multiplies the dimensions, reporting false if the product overflows.
// A corrupt dataspace can claim dimensions whose product wraps, which would
// otherwise turn a bounds check into a pass and index out of range.
func (d dataspace) count() (uint64, bool) {
	if d.null {
		return 0, true
	}
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
	if len(b) < 4 {
		return dataspace{}, fmt.Errorf("%w: short dataspace message", ErrNotHDF5)
	}
	rank := int(b[1])

	// v1: version, rank, flags, then five reserved bytes before the dims.
	// v2: version, rank, flags, then a type byte at 3 — scalar (0) and null
	// (2) spaces carry no dims — with the dims directly at 4.
	var pos int
	null := false
	switch b[0] {
	case 1:
		if len(b) < 8 {
			return dataspace{}, fmt.Errorf("%w: short dataspace message", ErrNotHDF5)
		}
		pos = 8
	case 2:
		pos = 4
		if b[3] == 0 || b[3] == 2 {
			rank = 0
		}
		null = b[3] == 2
	default:
		return dataspace{}, fmt.Errorf("%w: dataspace version %d", ErrUnsupported, b[0])
	}

	ds := dataspace{null: null}
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
