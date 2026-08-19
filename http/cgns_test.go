package fbhttp

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/rforced/filebrowser/v2/users"
)

// post.cgns is the committed CONVERGE 6.0.1 output: a supersonic channel at
// its first write, 558 cells across five named boundaries.

func cgnsSummary(t *testing.T) h5Summary {
	t.Helper()
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})
	rec := h5Get(t, h, token, "/api/h5/post.cgns")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}
	var got h5Summary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got
}

// TestCGNSSummaryLooksLikeAPostFile is the point of the whole mapping: a CGNS
// file has to reach the viewer as the same shape a post*.h5 does, or every
// component above the endpoint needs a second code path.
func TestCGNSSummaryLooksLikeAPostFile(t *testing.T) {
	got := cgnsSummary(t)

	if got.Kind != "post" {
		t.Errorf("kind = %q, want post", got.Kind)
	}
	if got.Solver != "CONVERGE 6.0.1" {
		t.Errorf("solver = %q", got.Solver)
	}
	if got.Cycle == nil || *got.Cycle != 0 {
		t.Errorf("cycle = %v, want the zeroth iteration", got.Cycle)
	}
	if len(got.Streams) != 1 {
		t.Fatalf("streams = %+v", got.Streams)
	}

	st := got.Streams[0]
	if st.Name != "STREAM_00" {
		t.Errorf("stream name = %q", st.Name)
	}
	// The zone states vertices and cells; the face count is the span of the
	// polygon section, which is a separate node entirely.
	if st.Cells != 558 || st.Vertices != 1602 || st.Faces != 2694 {
		t.Errorf("mesh = %d cells, %d vertices, %d faces", st.Cells, st.Vertices, st.Faces)
	}

	if len(st.Variables) != 12 {
		t.Fatalf("got %d variables, want 12", len(st.Variables))
	}
	if st.Variables[0].Name != "DENSITY" {
		t.Errorf("variables are not sorted: %q", st.Variables[0].Name)
	}
	// GridLocation sits beside the fields under the same flow solution node and
	// is not one of them; its label is the only thing that says so.
	for _, v := range st.Variables {
		if v.Name == "GridLocation" {
			t.Error("GridLocation was listed as a field")
		}
	}
	// The path names the node. " data" is how CGNS stores the values, and
	// putting it on the wire would leave every field with the same name.
	temp := st.Variables[0]
	if strings.HasSuffix(temp.Path, cgnsData) {
		t.Errorf("path %q exposes the payload dataset", temp.Path)
	}
	if temp.Type != "float32" || len(temp.Dims) != 1 || temp.Dims[0] != 558 {
		t.Errorf("DENSITY is %s%v", temp.Type, temp.Dims)
	}
}

// TestCGNSBoundariesCarryTheirNames is what CGNS gives that the native format
// does not: the boundary names from the deck, on the patches themselves.
func TestCGNSBoundariesCarryTheirNames(t *testing.T) {
	got := cgnsSummary(t)

	want := []h5Boundary{
		{ID: 1, Name: "INLET", Elements: 32},
		{ID: 2, Name: "OUTLET", Elements: 24},
		{ID: 3, Name: "ADIABATIC_WALLS", Elements: 180},
		{ID: 4, Name: "SIDE_WALLS1", Elements: 687},
		{ID: 5, Name: "SIDE_WALL2", Elements: 691},
	}
	if len(got.Boundaries) != len(want) {
		t.Fatalf("boundaries = %+v", got.Boundaries)
	}
	for i, b := range got.Boundaries {
		if b != want[i] {
			t.Errorf("boundary %d = %+v, want %+v", i, b, want[i])
		}
	}
}

// TestCGNSSimTimeUnit pins the rule that outlives every other detail here: the
// unit comes from CRANK_FLAG or it is left off. This case is crank_flag = 0,
// so the time is seconds and says so.
func TestCGNSSimTimeUnit(t *testing.T) {
	got := cgnsSummary(t)

	if got.Time == nil {
		t.Fatal("no sim time")
	}
	if got.Time.Value != 0 || got.Time.Unit != "s" {
		t.Errorf("time = %+v, want 0 s", got.Time)
	}
}

// TestCGNSStatsAndSubsetTakeNodePaths checks the two modes that are handed a
// path from the summary and have to find the values under it.
func TestCGNSStatsAndSubsetTakeNodePaths(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})
	const field = "STREAM_00/Zone/CELL_CENTER_DATA/TEMPERATURE"

	rec := h5Get(t, h, token, "/api/h5/post.cgns?stats="+field)
	if rec.Code != http.StatusOK {
		t.Fatalf("stats status = %d body = %q", rec.Code, rec.Body.String())
	}
	var stats struct {
		Stats []h5StatsEntry `json:"stats"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if len(stats.Stats) != 1 {
		t.Fatalf("stats = %+v", stats.Stats)
	}
	entry := stats.Stats[0]
	if entry.Err != "" {
		t.Fatalf("stats error: %s", entry.Err)
	}
	// The whole field is at the initial condition, so every cell reads 300 K.
	if entry.Name != "TEMPERATURE" || entry.Count != 558 || entry.Min != 300 || entry.Max != 300 {
		t.Errorf("stats = %+v", entry)
	}

	rec = h5Get(t, h, token, "/api/h5/post.cgns?subset="+field)
	if rec.Code != http.StatusOK {
		t.Fatalf("subset status = %d", rec.Code)
	}
	if disp := rec.Header().Get("Content-Disposition"); !strings.Contains(disp, "post_subset.csv") {
		t.Errorf("content-disposition = %q", disp)
	}
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 559 {
		t.Fatalf("got %d CSV lines, want a header and 558 rows", len(lines))
	}
	if lines[0] != "TEMPERATURE" {
		t.Errorf("CSV header = %q", lines[0])
	}
	if lines[1] != "300" {
		t.Errorf("first row = %q", lines[1])
	}
}

// TestNativeFileIsNotTreatedAsCGNS guards the detection: the two formats share
// an endpoint, and a post*.h5 read through the CGNS mapping would report a file
// full of fields as empty.
//
// newstyle.h5 is the interesting half of it. It carries the root label CGNS
// files are marked with, but holds a CONVERGE-shaped stream rather than a CGNS
// tree — so the label alone cannot be what decides.
func TestNativeFileIsNotTreatedAsCGNS(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	for _, name := range []string{"post.h5", "newstyle.h5"} {
		rec := h5Get(t, h, token, "/api/h5/"+name)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", name, rec.Code)
		}
		var got h5Summary
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Streams) != 1 || len(got.Streams[0].Variables) == 0 {
			t.Errorf("%s summarised as %+v", name, got.Streams)
		}
	}
}

// TestCGNSSurfaceIsTheWholeWettedBoundary checks the geometry against what the
// summary said was there. The two are read completely differently — the summary
// counts a patch's PointList, the surface walks the polygons those ids point at
// — so agreeing is worth something.
func TestCGNSSurfaceIsTheWholeWettedBoundary(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/post.cgns?surface=STREAM_00")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}
	got := decodeSurface(t, rec)

	if got.header.Stream != "STREAM_00" {
		t.Errorf("stream = %q", got.header.Stream)
	}
	// 32 + 24 + 180 + 687 + 691, the patch sizes the summary reports.
	if got.header.Faces != 1614 || got.header.FacesTotal != 1614 {
		t.Errorf("faces = %d of %d, want 1614", got.header.Faces, got.header.FacesTotal)
	}
	if got.header.Stride != 1 || got.header.Truncated {
		t.Errorf("an unbudgeted request was thinned: stride %d", got.header.Stride)
	}

	names := map[int64]string{}
	faces := 0
	for _, b := range got.header.Boundaries {
		names[b.ID] = b.Name
		faces += b.Faces
		if b.IndexCount != b.Triangles*3 {
			t.Errorf("%s: %d indices for %d triangles", b.Name, b.IndexCount, b.Triangles)
		}
		if b.IndexOffset+b.IndexCount > len(got.indices) {
			t.Errorf("%s addresses indices past the payload", b.Name)
		}
	}
	if faces != 1614 {
		t.Errorf("boundary faces sum to %d", faces)
	}
	// The names come off the BC nodes themselves, which is what CGNS has and
	// the native format does not.
	if names[1] != "INLET" || names[5] != "SIDE_WALL2" {
		t.Errorf("boundary names = %v", names)
	}

	for _, v := range got.indices {
		if int(v) >= got.header.Vertices {
			t.Fatalf("index %d addresses vertex %d of %d", v, v, got.header.Vertices)
		}
	}

	// The bounding box is the file's own coordinate range. Vertex ids in CGNS
	// start at one, so an off-by-one in rebasing them would shift every corner
	// onto its neighbour and move this box.
	want := [6]float64{0, -0.06, -9.765625e-07, 0.16, 0, 9.765625e-07}
	for i, v := range got.header.Bounds {
		if math.Abs(v-want[i]) > 1e-12 {
			t.Errorf("bounds = %v, want %v", got.header.Bounds, want)
			break
		}
	}
}

// TestCGNSSurfaceScalarComesFromTheCells pins the inversion: CGNS records
// cells pointing at their faces, and colouring a wall needs the opposite.
// Unresolved counts the vertices that came out with no reading, so zero is the
// claim that every drawn face found its cell.
func TestCGNSSurfaceScalarComesFromTheCells(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/post.cgns?surface=STREAM_00&scalar=TEMPERATURE")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}
	got := decodeSurface(t, rec)

	if got.header.Scalar != "TEMPERATURE" {
		t.Fatalf("scalar = %q", got.header.Scalar)
	}
	if got.header.Unresolved != 0 {
		t.Errorf("%d vertices resolved to no cell value", got.header.Unresolved)
	}
	// The case starts uniform, so every wall reads the initial condition.
	if got.header.Range != [2]float64{300, 300} {
		t.Errorf("range = %v, want 300 K throughout", got.header.Range)
	}
	if len(got.values) != got.header.Vertices {
		t.Fatalf("%d values for %d vertices", len(got.values), got.header.Vertices)
	}
	drawn := 0
	for _, v := range got.values {
		if v == 300 {
			drawn++
		}
	}
	if drawn == 0 {
		t.Error("no vertex carries the field's value")
	}

	// A field the zone does not have is a 404 rather than an uncoloured wall.
	rec = h5Get(t, h, token, "/api/h5/post.cgns?surface=STREAM_00&scalar=NO_SUCH_FIELD")
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown scalar status = %d, want 404", rec.Code)
	}
}

func TestCGNSSurfaceBoundaryFilterAndStride(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/post.cgns?surface=STREAM_00&boundaries=1,2")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	got := decodeSurface(t, rec)
	if len(got.header.Boundaries) != 2 || got.header.Faces != 56 {
		t.Errorf("filtered surface = %d boundaries, %d faces",
			len(got.header.Boundaries), got.header.Faces)
	}

	// A triangle budget thins the surface; the header says so rather than
	// letting a holed wall read as the whole one.
	rec = h5Get(t, h, token, "/api/h5/post.cgns?surface=STREAM_00&limit=400")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	got = decodeSurface(t, rec)
	if got.header.Stride < 2 || !got.header.Truncated {
		t.Errorf("stride = %d, truncated = %v", got.header.Stride, got.header.Truncated)
	}
	if got.header.Faces >= 1614 || got.header.FacesTotal != 1614 {
		t.Errorf("thinned to %d of %d faces", got.header.Faces, got.header.FacesTotal)
	}
}

func TestCGNSSurfaceUnknownStream(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/post.cgns?surface=STREAM_09")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// mixed.cgns is generated rather than real: a cube whose faces are split
// across a fixed-width triangle section and a variable-width polygon one, with
// one boundary given as a range and the other as a list. Every one of those is
// allowed by the standard and none appears in the post file CONVERGE wrote.

func TestCGNSMergesElementSections(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/mixed.cgns")
	if rec.Code != http.StatusOK {
		t.Fatalf("summary status = %d body = %q", rec.Code, rec.Body.String())
	}
	var summary h5Summary
	if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Streams) != 1 {
		t.Fatalf("streams = %+v", summary.Streams)
	}
	// Two triangles and three polygons, counted across both sections; the cell
	// section is not a face and is not among them.
	if got := summary.Streams[0]; got.Faces != 5 || got.Cells != 1 || got.Vertices != 8 {
		t.Errorf("mesh = %d faces, %d cells, %d vertices", got.Faces, got.Cells, got.Vertices)
	}
	if summary.Cycle == nil || *summary.Cycle != 4200 {
		t.Errorf("cycle = %v", summary.Cycle)
	}

	rec = h5Get(t, h, token, "/api/h5/mixed.cgns?surface=STREAM_00&scalar=TEMPERATURE")
	if rec.Code != http.StatusOK {
		t.Fatalf("surface status = %d body = %q", rec.Code, rec.Body.String())
	}
	got := decodeSurface(t, rec)

	// Both patches drew, one addressed by range and one by list, and the ear
	// clipper turned 3, 4 and 3-vertex faces into 1, 2 and 1 triangles: the two
	// triangles, then a quad, a triangle and a quad.
	if got.header.Faces != 5 || got.header.Triangles != 7 {
		t.Errorf("surface = %d faces, %d triangles, want 5 and 7",
			got.header.Faces, got.header.Triangles)
	}
	names := map[int64]string{}
	for _, b := range got.header.Boundaries {
		names[b.ID] = b.Name
	}
	if names[7] != "TOP" || names[8] != "SIDES" {
		t.Errorf("boundaries = %v, want the range-addressed one and the listed one", names)
	}
	// The cube is the unit cube, which is only true if the fixed-width section's
	// synthesised offsets and the polygon section's own table agree about where
	// each face's vertices are.
	if got.header.Bounds != [6]float64{0, 0, 0, 1, 1, 1} {
		t.Errorf("bounds = %v", got.header.Bounds)
	}
	// One cell owns every face, so the whole surface takes its value — across
	// both sections, which is what the merged numbering is for.
	if got.header.Unresolved != 0 || got.header.Range != [2]float64{500, 500} {
		t.Errorf("scalar = %v, %d unresolved", got.header.Range, got.header.Unresolved)
	}
}

// TestCGNSCrankAngleTime is the other half of the sim-time rule: this fixture
// is an engine case, so the same numbers mean degrees rather than seconds.
func TestCGNSCrankAngleTime(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/mixed.cgns")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got h5Summary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Time == nil {
		t.Fatal("no sim time")
	}
	if got.Time.Unit != "deg" {
		t.Errorf("unit = %q, want deg — CRANK_FLAG is set", got.Time.Unit)
	}
	if math.Abs(got.Time.Value+359.94) > 1e-9 || got.Time.RPM != 2000 {
		t.Errorf("time = %+v", got.Time)
	}
}
