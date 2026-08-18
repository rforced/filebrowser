package hdf5

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Attr is one attribute's decoded value. CONVERGE attributes are short — a
// scalar or a handful of elements — so they are decoded eagerly when the
// object header is read.
type Attr struct {
	Name   string
	Type   Datatype
	Floats []float64
	Ints   []int64
	Texts  []string
}

// Attrs indexes attributes by name.
type Attrs map[string]Attr

// Float returns the first element as a float64.
func (a Attrs) Float(name string) (float64, bool) {
	at, ok := a[name]
	if !ok || len(at.Floats) == 0 {
		return 0, false
	}
	return at.Floats[0], true
}

// Int returns the first element as an int64.
func (a Attrs) Int(name string) (int64, bool) {
	at, ok := a[name]
	if !ok {
		return 0, false
	}
	if len(at.Ints) > 0 {
		return at.Ints[0], true
	}
	if len(at.Floats) > 0 {
		return int64(at.Floats[0]), true
	}
	return 0, false
}

// Text returns the first element as a string.
func (a Attrs) Text(name string) (string, bool) {
	at, ok := a[name]
	if !ok || len(at.Texts) == 0 {
		return "", false
	}
	return at.Texts[0], true
}

// parseAttrs decodes every attribute message in an object header.
func (f *File) parseAttrs(oh *objectHeader) (Attrs, error) {
	var attrs Attrs
	for i := range oh.messages {
		if oh.messages[i].typ != msgAttribute {
			continue
		}
		a, err := f.parseAttr(oh.messages[i].data)
		if err != nil {
			// One malformed attribute must not make the whole file
			// unreadable: metadata display is best-effort.
			continue
		}
		if attrs == nil {
			attrs = Attrs{}
		}
		attrs[a.Name] = a
	}
	return attrs, nil
}

// parseAttr handles attribute message versions 1-3.
//
// v1: name, datatype and dataspace sub-messages are each padded to a multiple
// of 8 bytes. v2 and v3 drop the padding; v3 additionally carries a character
// set byte before the name.
func (f *File) parseAttr(b []byte) (Attr, error) {
	if len(b) < 8 {
		return Attr{}, fmt.Errorf("%w: short attribute message", ErrNotHDF5)
	}
	version := b[0]
	nameSize := int(binary.LittleEndian.Uint16(b[2:4]))
	typeSize := int(binary.LittleEndian.Uint16(b[4:6]))
	spaceSize := int(binary.LittleEndian.Uint16(b[6:8]))

	pos := 8
	pad := func(n int) int {
		if version == 1 {
			return (n + 7) &^ 7
		}
		return n
	}
	if version == 3 {
		// Character set encoding byte for the name.
		pos++
	}
	if version > 3 {
		return Attr{}, fmt.Errorf("%w: attribute version %d", ErrUnsupported, version)
	}

	if pos+pad(nameSize) > len(b) {
		return Attr{}, fmt.Errorf("%w: truncated attribute name", ErrNotHDF5)
	}
	name := string(b[pos : pos+nameSize])
	if i := indexByte([]byte(name), 0); i >= 0 {
		name = name[:i]
	}
	pos += pad(nameSize)

	if pos+pad(typeSize) > len(b) {
		return Attr{}, fmt.Errorf("%w: truncated attribute datatype", ErrNotHDF5)
	}
	dt, err := parseDatatype(b[pos : pos+typeSize])
	if err != nil {
		return Attr{}, err
	}
	pos += pad(typeSize)

	if pos+pad(spaceSize) > len(b) {
		return Attr{}, fmt.Errorf("%w: truncated attribute dataspace", ErrNotHDF5)
	}
	space, err := f.parseDataspace(b[pos : pos+spaceSize])
	if err != nil {
		return Attr{}, err
	}
	pos += pad(spaceSize)

	data := b[pos:]
	count, ok := space.count()
	// Attributes are inline in the object header, so the message itself bounds
	// how many elements can exist. Dividing rather than multiplying keeps a
	// corrupt dataspace from wrapping the check.
	if !ok || dt.Size <= 0 || count > uint64(len(data)/dt.Size) {
		return Attr{}, fmt.Errorf("%w: truncated attribute data", ErrNotHDF5)
	}
	n := int(count)

	a := Attr{Name: name, Type: dt}
	for i := 0; i < n; i++ {
		elem := data[i*dt.Size:]
		switch dt.Class {
		case ClassString:
			a.Texts = append(a.Texts, dt.decodeString(elem[:dt.Size]))
		case ClassInt:
			a.Ints = append(a.Ints, dt.decodeInt(elem))
			a.Floats = append(a.Floats, dt.decodeFloat(elem))
		case ClassFloat:
			v := dt.decodeFloat(elem)
			a.Floats = append(a.Floats, v)
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				a.Ints = append(a.Ints, int64(v))
			}
		}
	}
	return a, nil
}
