package hdf5

import (
	"fmt"
	"math"
)

// Dataset is an array of one datatype, stored contiguously or compactly.
type Dataset struct {
	f     *File
	Name  string
	Type  Datatype
	Dims  []uint64
	Attrs Attrs

	count  uint64
	bytes  uint64
	layout layout
}

func (f *File) datasetAt(addr uint64, name string) (*Dataset, error) {
	oh, err := f.readObjectHeader(addr)
	if err != nil {
		return nil, err
	}

	dtMsg := oh.first(msgDatatype)
	if dtMsg == nil {
		return nil, fmt.Errorf("%w: dataset %s has no datatype", ErrNotHDF5, name)
	}
	dt, err := parseDatatype(dtMsg)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	spaceMsg := oh.first(msgDataspace)
	if spaceMsg == nil {
		return nil, fmt.Errorf("%w: dataset %s has no dataspace", ErrNotHDF5, name)
	}
	space, err := f.parseDataspace(spaceMsg)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	layoutMsg := oh.first(msgLayout)
	if layoutMsg == nil {
		return nil, fmt.Errorf("%w: dataset %s has no layout", ErrNotHDF5, name)
	}
	lay, err := f.parseLayout(layoutMsg)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	attrs, err := f.parseAttrs(oh)
	if err != nil {
		return nil, err
	}

	// Size is validated once, here, so every later read can trust it. A
	// dataset cannot be larger than the file that holds it, which bounds both
	// the allocation and the int conversion in Raw.
	count, ok := space.count()
	if !ok || count > math.MaxUint64/uint64(dt.Size) {
		return nil, fmt.Errorf("%w: %s element count overflows", ErrNotHDF5, name)
	}
	bytes := count * uint64(dt.Size)
	if f.size > 0 && bytes > uint64(f.size) {
		return nil, fmt.Errorf("%w: %s claims %d bytes in a %d byte file",
			ErrNotHDF5, name, bytes, f.size)
	}

	return &Dataset{
		f: f, Name: name, Type: dt, Dims: space.dims, Attrs: attrs,
		count: count, bytes: bytes, layout: lay,
	}, nil
}

// Len is the number of elements across all dimensions.
func (d *Dataset) Len() uint64 { return d.count }

// ByteSize is the dataset's storage footprint.
func (d *Dataset) ByteSize() uint64 { return d.bytes }

// Contiguous reports whether the data is one unbroken extent, and if so where.
// The subset download relies on this to copy bytes without decoding them.
func (d *Dataset) Contiguous() (addr, size uint64, ok bool) {
	if d.layout.class != layoutContiguous || d.f.undefined(d.layout.addr) {
		return 0, 0, false
	}
	size = d.layout.size
	if size == 0 {
		size = d.ByteSize()
	}
	return d.layout.addr, size, true
}

// Raw returns the dataset's bytes as stored.
func (d *Dataset) Raw() ([]byte, error) {
	switch d.layout.class {
	case layoutCompact:
		return d.layout.compact, nil
	case layoutContiguous:
		if d.f.undefined(d.layout.addr) {
			// An unallocated dataset reads as zeroes rather than an error;
			// CONVERGE writes these for boundaries with no elements.
			return make([]byte, d.ByteSize()), nil
		}
		return d.f.readAt(d.layout.addr, int(d.ByteSize()))
	}
	return nil, fmt.Errorf("%w: layout class %d", ErrUnsupported, d.layout.class)
}

// Floats decodes the dataset to float64, widening from whatever numeric type
// it is stored as.
func (d *Dataset) Floats() ([]float64, error) {
	if !d.Type.Numeric() {
		return nil, fmt.Errorf("%w: %s is %s", ErrUnsupported, d.Name, d.Type)
	}
	raw, err := d.Raw()
	if err != nil {
		return nil, err
	}
	if uint64(len(raw)) < d.bytes {
		return nil, fmt.Errorf("%w: %s truncated", ErrNotHDF5, d.Name)
	}
	n := int(d.count)
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		out[i] = d.Type.decodeFloat(raw[i*d.Type.Size:])
	}
	return out, nil
}

// Ints decodes an integer dataset.
func (d *Dataset) Ints() ([]int64, error) {
	if d.Type.Class != ClassInt {
		return nil, fmt.Errorf("%w: %s is %s", ErrUnsupported, d.Name, d.Type)
	}
	raw, err := d.Raw()
	if err != nil {
		return nil, err
	}
	if uint64(len(raw)) < d.bytes {
		return nil, fmt.Errorf("%w: %s truncated", ErrNotHDF5, d.Name)
	}
	n := int(d.count)
	out := make([]int64, n)
	for i := 0; i < n; i++ {
		out[i] = d.Type.decodeInt(raw[i*d.Type.Size:])
	}
	return out, nil
}

// Strings decodes a fixed-length string dataset.
func (d *Dataset) Strings() ([]string, error) {
	if d.Type.Class != ClassString {
		return nil, fmt.Errorf("%w: %s is %s", ErrUnsupported, d.Name, d.Type)
	}
	raw, err := d.Raw()
	if err != nil {
		return nil, err
	}
	if uint64(len(raw)) < d.bytes {
		return nil, fmt.Errorf("%w: %s truncated", ErrNotHDF5, d.Name)
	}
	n := int(d.count)
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = d.Type.decodeString(raw[i*d.Type.Size : (i+1)*d.Type.Size])
	}
	return out, nil
}

// Stats summarises a numeric dataset.
type Stats struct {
	Count   uint64  `json:"count"`
	Min     float64 `json:"min"`
	Max     float64 `json:"max"`
	Mean    float64 `json:"mean"`
	NaN     uint64  `json:"nan"`
	Inf     uint64  `json:"inf"`
	Finite  uint64  `json:"finite"`
	Present bool    `json:"-"`
}

// Stats computes min/max/mean over the finite values, counting NaN and Inf
// separately. A run that has diverged shows up here as a non-zero NaN count or
// an absurd max, which is the cheapest divergence check available.
func (d *Dataset) Stats() (Stats, error) {
	values, err := d.Floats()
	if err != nil {
		return Stats{}, err
	}
	s := Stats{Count: uint64(len(values)), Min: math.Inf(1), Max: math.Inf(-1)}
	sum := 0.0
	for _, v := range values {
		switch {
		case math.IsNaN(v):
			s.NaN++
		case math.IsInf(v, 0):
			s.Inf++
		default:
			s.Finite++
			sum += v
			if v < s.Min {
				s.Min = v
			}
			if v > s.Max {
				s.Max = v
			}
		}
	}
	if s.Finite > 0 {
		s.Mean = sum / float64(s.Finite)
		s.Present = true
	} else {
		s.Min, s.Max = 0, 0
	}
	return s, nil
}
