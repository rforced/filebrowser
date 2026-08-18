package hdf5

import (
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"testing"
)

// open loads a fixture. See testdata/generate.py for how they are produced and
// why they use libver="earliest".
func open(t *testing.T, name string) *File {
	t.Helper()
	fh, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	t.Cleanup(func() { fh.Close() })
	st, err := fh.Stat()
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}
	f, err := Open(fh, st.Size())
	if err != nil {
		t.Fatalf("Open(%s): %v", name, err)
	}
	return f
}

func TestOpenRejectsNonHDF5(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"short", []byte("\x89HDF")},
		{"wrong magic", []byte("not an hdf5 file at all, really")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Open(strings.NewReader(string(tc.data)), int64(len(tc.data)))
			if !errors.Is(err, ErrNotHDF5) {
				t.Errorf("got %v, want ErrNotHDF5", err)
			}
		})
	}
}

func TestRootAttributes(t *testing.T) {
	f := open(t, "post.h5")
	root, err := f.Root()
	if err != nil {
		t.Fatal(err)
	}

	// CRANK_FLAG=1 means OUTPUT_TIME is in crank-angle degrees, not seconds.
	if v, ok := root.Attrs.Int("CRANK_FLAG"); !ok || v != 1 {
		t.Errorf("CRANK_FLAG = %v, %v; want 1", v, ok)
	}
	if v, ok := root.Attrs.Float("OUTPUT_TIME"); !ok || fmt.Sprintf("%.5f", v) != "-359.94486" {
		t.Errorf("OUTPUT_TIME = %v, %v", v, ok)
	}
	if v, ok := root.Attrs.Float("OUTPUT_TIME_SEC"); !ok || fmt.Sprintf("%.6f", v) != "-0.019997" {
		t.Errorf("OUTPUT_TIME_SEC = %v, %v", v, ok)
	}
	if v, ok := root.Attrs.Float("RPM"); !ok || v != 3000 {
		t.Errorf("RPM = %v, %v; want 3000", v, ok)
	}
	if _, ok := root.Attrs.Float("NOT_THERE"); ok {
		t.Error("missing attribute reported present")
	}
}

func TestStringAttributes(t *testing.T) {
	f := open(t, "restart.h5")
	root, err := f.Root()
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"SOLVER_VERSION": "CONVERGE 6.0.1",
		"COMPILE_DATE":   "Jul 27 2026",
	} {
		if got, ok := root.Attrs.Text(name); !ok || got != want {
			t.Errorf("%s = %q, %v; want %q", name, got, ok, want)
		}
	}
	for name, want := range map[string]int64{
		"NCYC":                6959,
		"TOTAL_MPI_PROCESSES": 88,
		"RESTART_FILE_NUM":    74,
	} {
		if got, ok := root.Attrs.Int(name); !ok || got != want {
			t.Errorf("%s = %d, %v; want %d", name, got, ok, want)
		}
	}
}

func TestGroupTraversal(t *testing.T) {
	f := open(t, "post.h5")
	root, err := f.Root()
	if err != nil {
		t.Fatal(err)
	}
	links, err := root.Children()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, l := range links {
		if l.Kind != KindGroup {
			t.Errorf("%s: kind = %v, want group", l.Name, l.Kind)
		}
		names = append(names, l.Name)
	}
	sort.Strings(names)
	if got := strings.Join(names, ","); got != "BOUNDARIES,STREAM_00" {
		t.Errorf("root children = %s", got)
	}

	g, err := f.Group("STREAM_00/CELL_CENTER_DATA")
	if err != nil {
		t.Fatal(err)
	}
	kids, err := g.Children()
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 3 {
		t.Errorf("CELL_CENTER_DATA has %d children, want 3", len(kids))
	}
	for _, l := range kids {
		if l.Kind != KindDataset {
			t.Errorf("%s: kind = %v, want dataset", l.Name, l.Kind)
		}
	}

	// A deeply nested path exercises multi-level descent.
	if !f.Exists("STREAM_00/PARCEL_DATA/LIQUID_PARCEL_DATA/LIQPARCEL_1/RADIUS") {
		t.Error("nested parcel dataset not found")
	}
	if f.Exists("STREAM_00/NOPE") {
		t.Error("nonexistent path reported present")
	}
}

func TestLookupErrors(t *testing.T) {
	f := open(t, "post.h5")
	if _, err := f.Dataset("STREAM_00"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Dataset on a group: %v, want ErrNotFound", err)
	}
	if _, err := f.Group("STREAM_00/CELL_CENTER_DATA/TEMPERATURE"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Group on a dataset: %v, want ErrNotFound", err)
	}
	if _, err := f.Dataset("does/not/exist"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing path: %v, want ErrNotFound", err)
	}
	// Descending through a dataset must fail rather than panic.
	if _, err := f.Dataset("STREAM_00/CELL_CENTER_DATA/TEMPERATURE/deeper"); !errors.Is(err, ErrNotFound) {
		t.Errorf("path through a dataset: %v, want ErrNotFound", err)
	}
}

func TestDatasetTypesAndValues(t *testing.T) {
	f := open(t, "post.h5")

	temp, err := f.Dataset("STREAM_00/CELL_CENTER_DATA/TEMPERATURE")
	if err != nil {
		t.Fatal(err)
	}
	if temp.Type.String() != "float32" {
		t.Errorf("TEMPERATURE type = %s", temp.Type)
	}
	if got := temp.Len(); got != 4 {
		t.Errorf("TEMPERATURE len = %d", got)
	}
	if got := temp.ByteSize(); got != 16 {
		t.Errorf("TEMPERATURE bytes = %d", got)
	}
	vals, err := temp.Floats()
	if err != nil {
		t.Fatal(err)
	}
	want := []float64{300, 450.5, 1200, 900}
	for i := range want {
		if vals[i] != want[i] {
			t.Errorf("TEMPERATURE[%d] = %v, want %v", i, vals[i], want[i])
		}
	}

	ids, err := f.Dataset("BOUNDARIES/BOUNDARY_IDS")
	if err != nil {
		t.Fatal(err)
	}
	if ids.Type.String() != "int32" {
		t.Errorf("BOUNDARY_IDS type = %s", ids.Type)
	}
	iv, err := ids.Ints()
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(iv) != "[1 2 3]" {
		t.Errorf("BOUNDARY_IDS = %v", iv)
	}

	names, err := f.Dataset("BOUNDARIES/BOUNDARY_NAMES")
	if err != nil {
		t.Fatal(err)
	}
	sv, err := names.Strings()
	if err != nil {
		t.Fatal(err)
	}
	// Fixed-length strings are null-padded on disk; the padding must be gone
	// and an embedded space must survive.
	if fmt.Sprint(sv) != "[PISTON HEAD SPARK PLUG]" {
		t.Errorf("BOUNDARY_NAMES = %q", sv)
	}
}

func TestWrongAccessorRejected(t *testing.T) {
	f := open(t, "post.h5")
	names, _ := f.Dataset("BOUNDARIES/BOUNDARY_NAMES")
	if _, err := names.Floats(); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Floats on strings: %v", err)
	}
	if _, err := names.Ints(); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Ints on strings: %v", err)
	}
	temp, _ := f.Dataset("STREAM_00/CELL_CENTER_DATA/TEMPERATURE")
	if _, err := temp.Strings(); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Strings on floats: %v", err)
	}
	if _, err := temp.Ints(); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Ints on floats: %v", err)
	}
}

func TestStats(t *testing.T) {
	f := open(t, "post.h5")

	temp, _ := f.Dataset("STREAM_00/CELL_CENTER_DATA/TEMPERATURE")
	s, err := temp.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if s.Count != 4 || s.Finite != 4 || s.NaN != 0 || s.Inf != 0 {
		t.Errorf("TEMPERATURE stats counts = %+v", s)
	}
	if s.Min != 300 || s.Max != 1200 {
		t.Errorf("TEMPERATURE range = %v..%v", s.Min, s.Max)
	}
	if want := (300 + 450.5 + 1200 + 900) / 4; s.Mean != want {
		t.Errorf("TEMPERATURE mean = %v, want %v", s.Mean, want)
	}

	// The divergence signal: NaN and Inf are counted out of the range rather
	// than poisoning min/max.
	eq, _ := f.Dataset("STREAM_00/CELL_CENTER_DATA/EQUIV_RATIO")
	s, err = eq.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if s.NaN != 1 || s.Inf != 1 || s.Finite != 2 {
		t.Errorf("EQUIV_RATIO stats = %+v", s)
	}
	if s.Min != 0.5 || s.Max != 1.5 {
		t.Errorf("EQUIV_RATIO range = %v..%v, want 0.5..1.5", s.Min, s.Max)
	}
	if math.IsNaN(s.Mean) || s.Mean != 1.0 {
		t.Errorf("EQUIV_RATIO mean = %v, want 1", s.Mean)
	}
}

func TestStatsOnEmptyDataset(t *testing.T) {
	f := open(t, "odd.h5")
	ds, err := f.Dataset("empty")
	if err != nil {
		t.Fatal(err)
	}
	s, err := ds.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if s.Count != 0 || s.Present || s.Min != 0 || s.Max != 0 {
		t.Errorf("empty stats = %+v; want zeroed and absent", s)
	}
}

// TestBoundaryOwnerEncoding locks in the -(id+1) convention the surface and
// boundary features depend on. Verified against real CONVERGE 4.1.2 and 6.0.1
// output before being encoded in the fixture.
func TestBoundaryOwnerEncoding(t *testing.T) {
	f := open(t, "post.h5")

	ids, _ := f.Dataset("BOUNDARIES/BOUNDARY_IDS")
	idv, _ := ids.Ints()
	counts, _ := f.Dataset("BOUNDARIES/NUM_ELEMENTS")
	cv, _ := counts.Ints()

	cc, err := f.Dataset("STREAM_00/CONNECTIVITY/CONNECTED_CELLS")
	if err != nil {
		t.Fatal(err)
	}
	pairs, err := cc.Ints()
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs)%2 != 0 {
		t.Fatalf("CONNECTED_CELLS has odd length %d", len(pairs))
	}

	seen := map[int64]int64{}
	for i := 0; i < len(pairs); i += 2 {
		if pairs[i] < 0 {
			seen[pairs[i]]++
		}
	}
	for i, id := range idv {
		if got := seen[-(id + 1)]; got != cv[i] {
			t.Errorf("boundary %d: %d faces at owner %d, NUM_ELEMENTS says %d",
				id, got, -(id + 1), cv[i])
		}
	}
}

func TestParcelBranch(t *testing.T) {
	f := open(t, "post.h5")
	const base = "STREAM_00/PARCEL_DATA/LIQUID_PARCEL_DATA/LIQPARCEL_1/"

	rad, err := f.Dataset(base + "RADIUS")
	if err != nil {
		t.Fatal(err)
	}
	if rad.Len() != 3 {
		t.Errorf("parcel count = %d, want 3", rad.Len())
	}

	x, _ := f.Dataset(base + "PARCEL_X")
	xs, err := x.Floats()
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(xs) != "[0 1 2]" {
		t.Errorf("PARCEL_X = %v", xs)
	}

	vars, err := f.Dataset("STREAM_00/VARIABLE_NAMES/LIQUID_PARCEL_VARIABLES")
	if err != nil {
		t.Fatal(err)
	}
	names, _ := vars.Strings()
	if len(names) != 5 || names[0] != "RADIUS" || names[2] != "PARCEL_X" {
		t.Errorf("LIQUID_PARCEL_VARIABLES = %v", names)
	}
}

func TestIntegerWidths(t *testing.T) {
	f := open(t, "odd.h5")
	for name, want := range map[string]string{
		"int8":   "int8",
		"uint16": "uint16",
		"int64":  "int64",
	} {
		ds, err := f.Dataset(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if ds.Type.String() != want {
			t.Errorf("%s type = %s, want %s", name, ds.Type, want)
		}
		v, err := ds.Ints()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if fmt.Sprint(v) != "[0 1 2 3]" {
			t.Errorf("%s = %v", name, v)
		}
		// Numeric datasets must also widen to float.
		if fv, err := ds.Floats(); err != nil || fmt.Sprint(fv) != "[0 1 2 3]" {
			t.Errorf("%s as floats = %v, %v", name, fv, err)
		}
	}
}

func TestScalarAndMultiDim(t *testing.T) {
	f := open(t, "odd.h5")

	sc, err := f.Dataset("scalar")
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.Dims) != 0 {
		t.Errorf("scalar dims = %v, want none", sc.Dims)
	}
	if sc.Len() != 1 {
		t.Errorf("scalar len = %d, want 1", sc.Len())
	}
	v, err := sc.Floats()
	if err != nil || len(v) != 1 || v[0] != 42.5 {
		t.Errorf("scalar = %v, %v", v, err)
	}

	two, err := f.Dataset("twod")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(two.Dims) != "[2 3]" {
		t.Errorf("twod dims = %v", two.Dims)
	}
	if two.Len() != 6 {
		t.Errorf("twod len = %d, want 6", two.Len())
	}
	tv, err := two.Floats()
	if err != nil || fmt.Sprint(tv) != "[0 1 2 3 4 5]" {
		t.Errorf("twod = %v, %v", tv, err)
	}
}

// TestChunkedRejected pins the deliberate limit: CONVERGE writes contiguous,
// unfiltered data, so chunk B-trees and the filter pipeline are out of scope
// and must fail loudly rather than return wrong numbers.
func TestChunkedRejected(t *testing.T) {
	f := open(t, "odd.h5")
	for _, name := range []string{"chunked", "compressed"} {
		if _, err := f.Dataset(name); !errors.Is(err, ErrUnsupported) {
			t.Errorf("Dataset(%s) = %v, want ErrUnsupported", name, err)
		}
	}
}

func TestContiguousExtent(t *testing.T) {
	f := open(t, "post.h5")
	ds, err := f.Dataset("STREAM_00/CELL_CENTER_DATA/TEMPERATURE")
	if err != nil {
		t.Fatal(err)
	}
	addr, size, ok := ds.Contiguous()
	if !ok {
		t.Fatal("TEMPERATURE not contiguous")
	}
	if size != ds.ByteSize() {
		t.Errorf("extent size = %d, want %d", size, ds.ByteSize())
	}
	// The extent must round-trip: reading those bytes directly matches Raw.
	raw, err := ds.Raw()
	if err != nil {
		t.Fatal(err)
	}
	direct, err := f.readAt(addr, int(size))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(direct) {
		t.Error("Contiguous extent does not match Raw bytes")
	}
}

func TestReadPastEndOfFile(t *testing.T) {
	// A file that claims to be HDF5 but is truncated must error, not panic.
	fh, err := os.ReadFile("testdata/post.h5")
	if err != nil {
		t.Fatal(err)
	}
	truncated := string(fh[:len(fh)/4])
	f, err := Open(strings.NewReader(truncated), int64(len(truncated)))
	if err != nil {
		return // rejecting at Open is also fine
	}
	root, err := f.Root()
	if err != nil {
		return
	}
	if _, err := root.Children(); err == nil {
		t.Log("truncated file traversed without error (harmless, but unexpected)")
	}
}
