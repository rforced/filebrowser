package hdf5

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

// Class is the subset of HDF5 datatype classes CONVERGE uses.
type Class int

const (
	ClassInt Class = iota
	ClassFloat
	ClassString
)

func (c Class) String() string {
	switch c {
	case ClassInt:
		return "int"
	case ClassFloat:
		return "float"
	case ClassString:
		return "string"
	}
	return "unknown"
}

// Datatype describes one element's storage.
type Datatype struct {
	Class  Class
	Size   int
	Signed bool
	// BigEndian is recorded so decoding stays correct if a file ever arrives
	// from a big-endian writer; CONVERGE output is little-endian.
	BigEndian bool
}

// String renders the type the way h5dump would, e.g. "float32", "int64",
// "string[20]".
func (d Datatype) String() string {
	switch d.Class {
	case ClassInt:
		if d.Signed {
			return fmt.Sprintf("int%d", d.Size*8)
		}
		return fmt.Sprintf("uint%d", d.Size*8)
	case ClassFloat:
		return fmt.Sprintf("float%d", d.Size*8)
	case ClassString:
		return fmt.Sprintf("string[%d]", d.Size)
	}
	return "unknown"
}

// Numeric reports whether values convert to float64.
func (d Datatype) Numeric() bool { return d.Class == ClassInt || d.Class == ClassFloat }

// parseDatatype reads a datatype message. Byte 0 packs version (high nibble)
// and class (low nibble); bytes 1-3 are class bit fields and bytes 4-7 the
// element size. Only the classes CONVERGE emits are accepted.
func parseDatatype(b []byte) (Datatype, error) {
	if len(b) < 8 {
		return Datatype{}, fmt.Errorf("%w: short datatype message", ErrNotHDF5)
	}
	version := b[0] >> 4
	if version < 1 || version > 3 {
		return Datatype{}, fmt.Errorf("%w: datatype version %d", ErrUnsupported, version)
	}
	class := b[0] & 0x0f
	bits := uint32(b[1]) | uint32(b[2])<<8 | uint32(b[3])<<16
	size := int(binary.LittleEndian.Uint32(b[4:8]))
	if size <= 0 || size > 1<<20 {
		return Datatype{}, fmt.Errorf("%w: datatype size %d", ErrNotHDF5, size)
	}

	switch class {
	case 0: // fixed-point
		if size != 1 && size != 2 && size != 4 && size != 8 {
			return Datatype{}, fmt.Errorf("%w: %d-byte integer", ErrUnsupported, size)
		}
		return Datatype{
			Class:     ClassInt,
			Size:      size,
			Signed:    bits&0x8 != 0,
			BigEndian: bits&0x1 != 0,
		}, nil
	case 1: // floating-point
		if size != 4 && size != 8 {
			return Datatype{}, fmt.Errorf("%w: %d-byte float", ErrUnsupported, size)
		}
		return Datatype{Class: ClassFloat, Size: size, BigEndian: bits&0x1 != 0}, nil
	case 3: // string
		return Datatype{Class: ClassString, Size: size}, nil
	}
	return Datatype{}, fmt.Errorf("%w: datatype class %d", ErrUnsupported, class)
}

// decodeFloat converts one element to float64. Integers widen; strings do not
// convert and are rejected by the caller.
func (d Datatype) decodeFloat(b []byte) float64 {
	switch d.Class {
	case ClassFloat:
		if d.Size == 4 {
			return float64(math.Float32frombits(d.uint32(b)))
		}
		return math.Float64frombits(d.uint64(b))
	case ClassInt:
		if d.Signed {
			return float64(d.decodeInt(b))
		}
		switch d.Size {
		case 1:
			return float64(b[0])
		case 2:
			return float64(d.uint16(b))
		case 4:
			return float64(d.uint32(b))
		default:
			return float64(d.uint64(b))
		}
	}
	return 0
}

func (d Datatype) decodeInt(b []byte) int64 {
	switch d.Size {
	case 1:
		if d.Signed {
			return int64(int8(b[0]))
		}
		return int64(b[0])
	case 2:
		if d.Signed {
			return int64(int16(d.uint16(b)))
		}
		return int64(d.uint16(b))
	case 4:
		if d.Signed {
			return int64(int32(d.uint32(b)))
		}
		return int64(d.uint32(b))
	default:
		return int64(d.uint64(b))
	}
}

func (d Datatype) uint16(b []byte) uint16 {
	if d.BigEndian {
		return binary.BigEndian.Uint16(b)
	}
	return binary.LittleEndian.Uint16(b)
}

func (d Datatype) uint32(b []byte) uint32 {
	if d.BigEndian {
		return binary.BigEndian.Uint32(b)
	}
	return binary.LittleEndian.Uint32(b)
}

func (d Datatype) uint64(b []byte) uint64 {
	if d.BigEndian {
		return binary.BigEndian.Uint64(b)
	}
	return binary.LittleEndian.Uint64(b)
}

// decodeString trims the null and space padding HDF5 fixed-length strings
// carry. CONVERGE pads boundary names with nulls to a fixed width.
func (d Datatype) decodeString(b []byte) string {
	if i := indexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimRight(string(b), " ")
}

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}
