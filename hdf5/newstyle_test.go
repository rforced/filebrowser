package hdf5

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"strings"
	"testing"
)

// The tests here cover the second structure generation: superblock v2, v2
// object headers, and links held either in the header or in a fractal heap.
// That is what CGNS files are written in, and none of it appears in the native
// CONVERGE format the rest of the suite reads.

func childNames(t *testing.T, f *File, path string) []string {
	t.Helper()
	g, err := f.Group(path)
	if err != nil {
		t.Fatalf("group %q: %v", path, err)
	}
	links, err := g.Children()
	if err != nil {
		t.Fatalf("children of %q: %v", path, err)
	}
	names := make([]string, len(links))
	for i, l := range links {
		names[i] = l.Name
	}
	return names
}

func TestNewStyleLinkStorage(t *testing.T) {
	f := open(t, "newstyle.h5")

	// Three group sizes, three storages: COMPACT keeps its links in the object
	// header, DENSE has outgrown it into a fractal heap, and MANY has outgrown
	// a single heap block into a doubling table of them.
	for _, tc := range []struct {
		group string
		want  int
	}{
		{"COMPACT", 3},
		{"DENSE", 13},
		{"MANY", 200},
	} {
		if got := len(childNames(t, f, tc.group)); got != tc.want {
			t.Errorf("%s: got %d children, want %d", tc.group, got, tc.want)
		}
	}

	// A group's children must be the children of that group and no other: a
	// heap read against the wrong block would still return plausible names.
	names := childNames(t, f, "DENSE")
	for i, name := range names {
		if !strings.HasPrefix(name, "VARIABLE_") {
			t.Errorf("DENSE child %d is %q", i, name)
		}
	}
	if got := childNames(t, f, "MANY")[0]; !strings.HasPrefix(got, "BOUNDARY_") {
		t.Errorf("MANY first child is %q", got)
	}
}

func TestNewStyleLinkKinds(t *testing.T) {
	f := open(t, "newstyle.h5")
	root, err := f.Root()
	if err != nil {
		t.Fatal(err)
	}
	links, err := root.Children()
	if err != nil {
		t.Fatal(err)
	}

	kinds := map[string]Kind{}
	for _, l := range links {
		kinds[l.Name] = l.Kind
	}
	// A link message names an address and nothing else, so every kind here was
	// resolved by opening the target rather than read out of the link.
	for _, name := range []string{"COMPACT", "DENSE", "MANY", "EMPTY"} {
		if kind, ok := kinds[name]; !ok {
			t.Errorf("%s missing from the root", name)
		} else if kind != KindGroup {
			t.Errorf("%s: got kind %v, want group", name, kind)
		}
	}
	// An empty group is still a group. Classifying it as a dataset would have
	// it open as one and fail on the datatype it has never had.
	if got := childNames(t, f, "EMPTY"); len(got) != 0 {
		t.Errorf("EMPTY: got %v, want no children", got)
	}
	if _, listed := kinds["FIELD_0"]; listed {
		t.Error("a grandchild was listed among the root's children")
	}
}

func TestNewStyleSoftAndExternalLinksSkipped(t *testing.T) {
	f := open(t, "newstyle.h5")

	// Neither names an object in this file: the soft link's target is listed
	// under its own name already, and the external one lives elsewhere. What
	// matters is that stepping over them lands exactly on the next message —
	// a byte out and every link after them would decode as rubbish.
	for _, name := range childNames(t, f, "") {
		if name == "soft" || name == "ext" {
			t.Errorf("%s was listed as an object", name)
		}
	}
	if !f.Exists("COMPACT/FIELD_2") {
		t.Error("the links after the soft and external ones went missing")
	}
}

// TestFractalHeapCountIsChecked patches the object count in each heap header
// and expects the read to fail rather than return a short list of children.
//
// The heap is walked in block order, which needs free space to be recognisable
// — it reads as zeroes, and a link message cannot start with one. The count
// the header carries is what turns that from an assumption into something
// checked on every read, so it is worth a test of its own.
func TestFractalHeapCountIsChecked(t *testing.T) {
	raw, err := os.ReadFile("testdata/newstyle.h5")
	if err != nil {
		t.Fatal(err)
	}
	// DENSE, MANY and the stream's cell data are the fixture's heaped groups.
	if n := bytes.Count(raw, []byte("FRHP")); n != 3 {
		t.Fatalf("expected 3 fractal heaps in the fixture, found %d", n)
	}

	// Managed object count: past the 14-byte prefix, four length-sized
	// counters, two addresses, and the iterator offset.
	const countOffset = 14 + 4*8 + 2*8 + 8
	patched := bytes.Clone(raw)
	for i := 0; i+countOffset+8 <= len(patched); {
		j := bytes.Index(patched[i:], []byte("FRHP"))
		if j < 0 {
			break
		}
		at := i + j + countOffset
		binary.LittleEndian.PutUint64(patched[at:], binary.LittleEndian.Uint64(patched[at:])+1)
		i += j + 4
	}

	f, err := Open(strings.NewReader(string(patched)), int64(len(patched)))
	if err != nil {
		t.Fatal(err)
	}
	g, err := f.Group("DENSE")
	if err != nil {
		t.Fatal(err)
	}
	_, err = g.Children()
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("got %v, want ErrUnsupported", err)
	}
}

func TestSuperblockVersion3(t *testing.T) {
	// v3 differs from v2 only in what the file consistency flags mean, which is
	// not something a reader acts on, so the two are read by the same code. No
	// CONVERGE build writes one; flipping the version byte of a v2 file is what
	// proves the branch is reachable rather than that such a file parses.
	raw, err := os.ReadFile("testdata/newstyle.h5")
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Clone(raw)
	raw[8] = 3

	f, err := Open(strings.NewReader(string(raw)), int64(len(raw)))
	if err != nil {
		t.Fatalf("superblock v3: %v", err)
	}
	if got := len(childNames(t, f, "DENSE")); got != 13 {
		t.Errorf("got %d children, want 13", got)
	}
}

func TestSuperblockVersion1Rejected(t *testing.T) {
	raw, err := os.ReadFile("testdata/newstyle.h5")
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Clone(raw)
	raw[8] = 1

	_, err = Open(strings.NewReader(string(raw)), int64(len(raw)))
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("got %v, want ErrUnsupported", err)
	}
}

// TestCGNSPostFile reads the committed CONVERGE 6.0.1 output. Everything above
// is generated by h5py; this is the format as CONVERGE actually writes it.
func TestCGNSPostFile(t *testing.T) {
	f := open(t, "post.cgns")

	if got := childNames(t, f, ""); !equalStrings(got, []string{" format", " hdf5version", "CGNSLibraryVersion", "STREAM_00"}) {
		t.Errorf("root children: %q", got)
	}
	// CGNS calls a node's payload " data", with a leading space, and that name
	// has to survive path resolution intact.
	version, err := f.Dataset("CGNSLibraryVersion/ data")
	if err != nil {
		t.Fatal(err)
	}
	if vals, err := version.Floats(); err != nil || len(vals) != 1 || vals[0] != 4.5 {
		t.Errorf("CGNS library version: %v %v", vals, err)
	}

	// The zone, the flow solution and the header node are the three groups in
	// a CGNS post file with more than eight children — the ones a reader
	// without dense link storage cannot see into at all.
	zone := childNames(t, f, "STREAM_00/Zone")
	want := []string{" data", "ZoneType", "GridCoordinates", "CELL_FACES", "CELLS",
		"ZoneBC", "CELL_CENTER_DATA", "FlowEquationSet", "ZoneIterativeData", "HEADER"}
	if !equalStrings(zone, want) {
		t.Errorf("zone children:\n got %q\nwant %q", zone, want)
	}
	if got := childNames(t, f, "STREAM_00/Zone/CELL_CENTER_DATA"); len(got) != 13 {
		t.Errorf("got %d flow solution children, want 13", len(got))
	}
	if got := childNames(t, f, "STREAM_00/Zone/ZoneBC"); !equalStrings(got,
		[]string{"INLET", "OUTLET", "ADIABATIC_WALLS", "SIDE_WALLS1", "SIDE_WALL2"}) {
		t.Errorf("boundaries: %q", got)
	}

	g, err := f.Group("STREAM_00/Zone")
	if err != nil {
		t.Fatal(err)
	}
	// Every CGNS node carries its type in attributes rather than in the link.
	if label, _ := g.Attrs.Text("label"); label != "Zone_t" {
		t.Errorf("zone label %q, want Zone_t", label)
	}
	if name, _ := g.Attrs.Text("name"); name != "Zone" {
		t.Errorf("zone name %q, want Zone", name)
	}

	dims, err := f.Dataset("STREAM_00/Zone/ data")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := dims.Ints(); err != nil || !equalInts(got, []int64{1602, 558, 0}) {
		t.Errorf("zone dims %v, %v", got, err)
	}

	// Vertices are float64 here, where the native format writes float32.
	coords, err := f.Dataset("STREAM_00/Zone/GridCoordinates/CoordinateX/ data")
	if err != nil {
		t.Fatal(err)
	}
	if coords.Type.String() != "float64" || coords.Len() != 1602 {
		t.Errorf("CoordinateX is %s%v", coords.Type, coords.Dims)
	}
	if vals, err := coords.Floats(); err != nil || vals[0] != 0.12 {
		t.Errorf("CoordinateX[0] = %v, %v", vals[0], err)
	}

	// Element sections are int64 and share one global numbering: the faces run
	// first, the cells after them.
	faces, err := f.Dataset("STREAM_00/Zone/CELL_FACES/ElementRange/ data")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := faces.Ints(); err != nil || !equalInts(got, []int64{1, 2694}) {
		t.Errorf("face element range %v, %v", got, err)
	}
	cells, err := f.Dataset("STREAM_00/Zone/CELLS/ElementRange/ data")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := cells.Ints(); err != nil || !equalInts(got, []int64{2695, 3252}) {
		t.Errorf("cell element range %v, %v", got, err)
	}

	// The sim time and its unit live in datasets under HEADER, not in root
	// attributes as they do in a post*.h5.
	for _, tc := range []struct {
		path string
		want float64
	}{
		{"OUTPUT_TIME", 0},
		{"CRANK_FLAG", 0},
		{"VERSION_FLAG", 4},
		{"VERSION_NUM1", 6},
	} {
		ds, err := f.Dataset("STREAM_00/Zone/HEADER/" + tc.path + "/ data")
		if err != nil {
			t.Errorf("HEADER/%s: %v", tc.path, err)
			continue
		}
		vals, err := ds.Floats()
		if err != nil || len(vals) != 1 || vals[0] != tc.want {
			t.Errorf("HEADER/%s = %v (%v), want %v", tc.path, vals, err, tc.want)
		}
	}

	temp, err := f.Dataset("STREAM_00/Zone/CELL_CENTER_DATA/TEMPERATURE/ data")
	if err != nil {
		t.Fatal(err)
	}
	stats, err := temp.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Count != 558 || stats.Min != 300 || stats.Max != 300 {
		t.Errorf("temperature stats %+v", stats)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func equalInts(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
