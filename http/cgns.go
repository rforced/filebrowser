package fbhttp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rforced/filebrowser/v2/hdf5"
)

// CGNS is the second format CONVERGE writes 3D field output in, chosen by
// write_cgns_flag in post.in. The file is HDF5 underneath, but nothing above
// the container is shared with post*.h5: where the native format hangs flat
// arrays off STREAM_00, CGNS nests a typed node tree — a base holding a zone
// holding grid coordinates, element sections, boundary conditions and a flow
// solution.
//
// The mapping is uniform enough to be read generically. Every node is a group
// whose payload is a child dataset named " data" — with a leading space, which
// is why no path here may be split on whitespace — and whose CGNS type is an
// attribute rather than anything structural. Labels are what this file keys
// on: names are the case engineer's (INLET, CELL_FACES), while labels are the
// standard's (BC_t, Elements_t).
//
// What comes out is the same h5Summary the native format produces, so the
// viewer, the statistics endpoint and the CSV subset need know nothing about
// which format they are looking at.
const (
	// cgnsData is the child every CGNS node keeps its payload in.
	cgnsData = " data"

	// cgnsRootLabel marks the ADF-to-HDF5 mapping and is on the root group of
	// every CGNS file. It is checked first because it is an attribute already
	// parsed: listing the root to look for the version node costs an object
	// header per child, and would be paid by every post*.h5 opened.
	cgnsRootLabel = "Root Node of HDF5 File"

	// cgnsVersionNode is what confirms it. The label above says only that the
	// file was written through the ADF mapping, which is a container convention
	// rather than proof of a CGNS tree — and reading a native post file through
	// the CGNS mapping would report a file full of fields as empty.
	cgnsVersionNode = "CGNSLibraryVersion"

	// cgnsMaxText bounds a string node. CGNS stores text as an int8 array, so
	// nothing about the datatype limits how much a corrupt one could claim.
	cgnsMaxText = 4096
)

// CGNS element types, from the standard's ElementType_t enumeration. CONVERGE
// writes the polyhedral pair: a face section and a cell section that indexes
// it. TRI_3 appears in surface files, which are triangulated already.
const (
	cgnsTri3  = 5
	cgnsQuad4 = 7
	cgnsNGon  = 22
	cgnsNFace = 23
)

// cgnsFile reports whether an open file is CGNS rather than native CONVERGE.
func cgnsFile(f *hdf5.File, root *hdf5.Group) bool {
	if label, _ := root.Attrs.Text("label"); label != cgnsRootLabel {
		return false
	}
	return f.Exists(cgnsVersionNode)
}

// cgnsLabel opens a node and reports its CGNS type.
func cgnsLabel(f *hdf5.File, path string) (*hdf5.Group, string) {
	g, err := f.Group(path)
	if err != nil {
		return nil, ""
	}
	label, _ := g.Attrs.Text("label")
	return g, label
}

// cgnsChildrenLabelled lists the child nodes of one node carrying a given
// label, in file order. The payload dataset is not a node and never matches.
func cgnsChildrenLabelled(f *hdf5.File, path, label string) []string {
	g, err := f.Group(path)
	if err != nil {
		return nil
	}
	links, err := g.Children()
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range links {
		if l.Kind != hdf5.KindGroup {
			continue
		}
		if _, got := cgnsLabel(f, path+"/"+l.Name); got == label {
			out = append(out, l.Name)
		}
	}
	return out
}

// cgnsPayload opens a node's payload dataset.
func cgnsPayload(f *hdf5.File, path string) (*hdf5.Dataset, error) {
	return f.Dataset(path + "/" + cgnsData)
}

func cgnsInts(f *hdf5.File, path string) ([]int64, error) {
	ds, err := cgnsPayload(f, path)
	if err != nil {
		return nil, err
	}
	return ds.Ints()
}

func cgnsFloats(f *hdf5.File, path string) ([]float64, error) {
	ds, err := cgnsPayload(f, path)
	if err != nil {
		return nil, err
	}
	return ds.Floats()
}

// cgnsText reads a node whose payload is text. CGNS stores strings as arrays
// of signed bytes rather than as an HDF5 string type, so this decodes what the
// reader hands back as int8 data.
func cgnsText(f *hdf5.File, path string) string {
	ds, err := cgnsPayload(f, path)
	if err != nil || ds.Type.Class != hdf5.ClassInt || ds.Type.Size != 1 {
		return ""
	}
	if ds.Len() > cgnsMaxText {
		return ""
	}
	raw, err := ds.Raw()
	if err != nil {
		return ""
	}
	if i := strings.IndexByte(string(raw), 0); i >= 0 {
		raw = raw[:i]
	}
	return strings.TrimRight(string(raw), " ")
}

// cgnsDescribe builds the summary for a CGNS file.
//
// A base is what the native format calls a stream — CONVERGE names them the
// same way, STREAM_00 — and a zone is the mesh inside it. One zone per base is
// what CONVERGE writes; more are allowed by the standard, so a second one is
// listed under its own name rather than silently replacing the first.
func cgnsDescribe(f *hdf5.File, name string, size int64) (*h5Summary, error) {
	root, err := f.Root()
	if err != nil {
		return nil, err
	}
	links, err := root.Children()
	if err != nil {
		return nil, err
	}

	s := &h5Summary{
		Name:    name,
		Size:    size,
		Kind:    h5Kind(name, root),
		Streams: []h5Stream{},
	}

	for _, l := range links {
		if l.Kind != hdf5.KindGroup {
			continue
		}
		base := l.Name
		if _, label := cgnsLabel(f, base); label != "CGNSBase_t" {
			continue
		}

		zones := cgnsChildrenLabelled(f, base, "Zone_t")
		for _, zone := range zones {
			stream := base
			if len(zones) > 1 {
				stream = base + "/" + zone
			}
			st, boundaries, err := cgnsReadZone(f, base+"/"+zone, stream)
			if err != nil {
				// Same rule as the native format: a stream that cannot be
				// described is reported, because a summary listing no streams
				// reads as an empty file.
				return nil, err
			}
			s.Streams = append(s.Streams, st)
			s.Boundaries = append(s.Boundaries, boundaries...)

			if s.Time == nil {
				s.Time = cgnsReadTime(f, base+"/"+zone)
			}
			if s.Solver == "" {
				s.Solver = cgnsReadSolver(f, base+"/"+zone)
			}
		}

		// BaseIterativeData_t carries the cycle the output was written at,
		// which the native format keeps only in restarts.
		if s.Cycle == nil {
			for _, iter := range cgnsChildrenLabelled(f, base, "BaseIterativeData_t") {
				if v, err := cgnsInts(f, base+"/"+iter+"/IterationValues"); err == nil && len(v) > 0 {
					cycle := v[0]
					s.Cycle = &cycle
					break
				}
			}
		}
	}

	return s, nil
}

// cgnsReadZone describes one zone: its mesh sizes, the fields on it, and the
// boundary patches it carries.
func cgnsReadZone(f *hdf5.File, path, stream string) (h5Stream, []h5Boundary, error) {
	g, err := f.Group(path)
	if err != nil {
		return h5Stream{}, nil, err
	}
	links, err := g.Children()
	if err != nil {
		return h5Stream{}, nil, err
	}

	st := h5Stream{Name: stream, Variables: []h5Variable{}}
	// A zone states its own size: vertices, then cells, then boundary
	// vertices, which CONVERGE leaves at zero.
	if dims, err := cgnsInts(f, path); err == nil && len(dims) >= 2 {
		if dims[0] > 0 {
			st.Vertices = uint64(dims[0])
		}
		if dims[1] > 0 {
			st.Cells = uint64(dims[1])
		}
	}

	var boundaries []h5Boundary
	for _, l := range links {
		if l.Kind != hdf5.KindGroup {
			continue
		}
		child := path + "/" + l.Name
		_, label := cgnsLabel(f, child)
		switch label {
		case "Elements_t":
			// Element sections share one global numbering across the zone, so
			// how many faces there are is the span of the face section rather
			// than anything the zone itself states.
			kind, first, last, err := cgnsElementSection(f, child)
			if err != nil || last < first {
				continue
			}
			if kind == cgnsNGon || cgnsFaceWidth(kind) > 0 {
				st.Faces += uint64(last - first + 1)
			}
		case "FlowSolution_t":
			st.Variables = append(st.Variables, cgnsReadFields(f, child)...)
		case "ZoneBC_t":
			boundaries = append(boundaries, cgnsReadBoundaries(f, child)...)
		}
	}
	// Sorted for the same reason the native format's are: the viewer lists them
	// as it receives them, and file order is whatever the writer happened to do.
	sort.Slice(st.Variables, func(i, j int) bool { return st.Variables[i].Name < st.Variables[j].Name })

	return st, boundaries, nil
}

// cgnsReadFields lists the data arrays of one flow solution. GridLocation is a
// sibling of theirs rather than a field, and is filtered out by its label.
func cgnsReadFields(f *hdf5.File, path string) []h5Variable {
	var out []h5Variable
	for _, name := range cgnsChildrenLabelled(f, path, "DataArray_t") {
		if len(out) >= h5MaxVariables {
			break
		}
		node := path + "/" + name
		ds, err := cgnsPayload(f, node)
		if err != nil {
			continue
		}
		dims := ds.Dims
		if dims == nil {
			dims = []uint64{}
		}
		// The path names the node, not its payload: " data" is an artefact of
		// how CGNS maps onto HDF5, and every field would otherwise be called
		// the same thing on the wire.
		out = append(out, h5Variable{
			Name:  name,
			Path:  node,
			Type:  ds.Type.String(),
			Dims:  dims,
			Bytes: ds.ByteSize(),
		})
	}
	return out
}

// cgnsReadBoundaries lists the boundary patches under a ZoneBC node.
//
// Names here are the ones from the case's boundary.in — INLET, ADIABATIC_WALLS
// — which the native format only carries in a separate BOUNDARIES group. The
// id CONVERGE knows the boundary by is one level down, under the global data
// set; a patch without one still lists, under its name.
func cgnsReadBoundaries(f *hdf5.File, path string) []h5Boundary {
	var out []h5Boundary
	for _, name := range cgnsChildrenLabelled(f, path, "BC_t") {
		b := h5Boundary{Name: name}
		if ids, err := cgnsInts(f, path+"/"+name+"/GLOBAL_DATA/DirichletData/BOUNDARY_ID"); err == nil && len(ids) > 0 {
			b.ID = ids[0]
		}
		if ds, err := cgnsPayload(f, path+"/"+name+"/PointList"); err == nil {
			b.Elements = int64(ds.Len())
		}
		out = append(out, b)
	}
	return out
}

// cgnsElementSection reads a section's type and the span of the global element
// numbering it occupies.
func cgnsElementSection(f *hdf5.File, path string) (kind, first, last int64, err error) {
	head, err := cgnsInts(f, path)
	if err != nil {
		return 0, 0, 0, err
	}
	if len(head) < 1 {
		return 0, 0, 0, fmt.Errorf("%s has no element type", path)
	}
	span, err := cgnsInts(f, path+"/ElementRange")
	if err != nil {
		return 0, 0, 0, err
	}
	if len(span) < 2 {
		return 0, 0, 0, fmt.Errorf("%s has no element range", path)
	}
	return head[0], span[0], span[1], nil
}

// cgnsReadTime resolves the sim time from the HEADER node CONVERGE writes into
// every zone.
//
// The values are datasets here rather than the attributes a post*.h5 carries,
// but they are the same values under the same names, so they are gathered into
// an attribute set and handed to the same reader. That keeps one rule in one
// place: CRANK_FLAG decides whether the number is seconds or crank-angle
// degrees, and a file that does not say goes unlabelled rather than being
// guessed at.
func cgnsReadTime(f *hdf5.File, zone string) *h5Time {
	attrs := cgnsHeaderAttrs(f, zone)
	if attrs == nil {
		return nil
	}
	return h5ReadTime(attrs)
}

func cgnsReadSolver(f *hdf5.File, zone string) string {
	attrs := cgnsHeaderAttrs(f, zone)
	major, ok := attrs.Int("VERSION_NUM1")
	if !ok {
		return ""
	}
	minor, _ := attrs.Int("VERSION_NUM2")
	patch, _ := attrs.Int("VERSION_NUM3")
	return fmt.Sprintf("CONVERGE %d.%d.%d", major, minor, patch)
}

// cgnsHeaderAttrs reads the zone's HEADER node into an attribute set, so the
// values CONVERGE writes as datasets in CGNS can be read by the code that
// expects them as attributes.
func cgnsHeaderAttrs(f *hdf5.File, zone string) hdf5.Attrs {
	header := zone + "/HEADER"
	if _, label := cgnsLabel(f, header); label == "" {
		return nil
	}
	names := cgnsChildrenLabelled(f, header, "DataArray_t")
	if len(names) == 0 {
		return nil
	}

	attrs := hdf5.Attrs{}
	for _, name := range names {
		values, err := cgnsFloats(f, header+"/"+name)
		if err != nil || len(values) == 0 {
			continue
		}
		attr := hdf5.Attr{Name: name, Floats: values}
		for _, v := range values {
			attr.Ints = append(attr.Ints, int64(v))
		}
		attrs[name] = attr
	}
	if len(attrs) == 0 {
		return nil
	}
	return attrs
}
