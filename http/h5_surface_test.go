package fbhttp

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"maps"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"

	"github.com/rforced/filebrowser/v2/users"
)

// decoded is the binary surface response taken apart.
type decodedSurface struct {
	header    h5SurfaceHeader
	positions []float32
	indices   []uint32
	values    []float32
	edges     []uint32
}

func decodeSurface(t *testing.T, rec *httptest.ResponseRecorder) decodedSurface {
	t.Helper()
	body := rec.Body.Bytes()
	if len(body) < 12 {
		t.Fatalf("body too short: %d bytes", len(body))
	}
	if got := string(body[:8]); got != h5SurfaceMagic {
		t.Fatalf("magic = %q, want %q", got, h5SurfaceMagic)
	}

	headerLen := int(binary.LittleEndian.Uint32(body[8:12]))
	if headerLen%4 != 0 {
		t.Errorf("header length %d is not 4-byte aligned, so the client cannot "+
			"lay typed-array views over the payload", headerLen)
	}

	var out decodedSurface
	if err := json.Unmarshal(body[12:12+headerLen], &out.header); err != nil {
		t.Fatalf("header: %v", err)
	}

	cursor := 12 + headerLen
	read := func(n int) []float32 {
		vals := make([]float32, n)
		for i := range vals {
			vals[i] = math.Float32frombits(binary.LittleEndian.Uint32(body[cursor:]))
			cursor += 4
		}
		return vals
	}

	out.positions = read(out.header.Vertices * 3)
	out.indices = make([]uint32, out.header.Triangles*3)
	for i := range out.indices {
		out.indices[i] = binary.LittleEndian.Uint32(body[cursor:])
		cursor += 4
	}
	if out.header.Scalar != "" {
		out.values = read(out.header.Vertices)
	}
	out.edges = make([]uint32, out.header.Edges)
	for i := range out.edges {
		out.edges[i] = binary.LittleEndian.Uint32(body[cursor:])
		cursor += 4
	}
	if cursor != len(body) {
		t.Errorf("decoded %d bytes of a %d byte body", cursor, len(body))
	}
	return out
}

// The fixture's five faces all sit on a boundary: two on id 1 (owner -2) and
// three on id 2 (owner -3), sized 3, 4, 3, 3, 4. Ear clipping yields n-2
// triangles each, so 1+2 and 1+1+2.
func TestH5SurfaceExtractsBoundaryFaces(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/post.h5?surface=STREAM_00")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("content type = %q", ct)
	}

	got := decodeSurface(t, rec)
	if got.header.Faces != 5 {
		t.Errorf("faces = %d, want 5", got.header.Faces)
	}
	if got.header.Triangles != 7 {
		t.Errorf("triangles = %d, want 7", got.header.Triangles)
	}
	// Only four of the fixture's six vertices are referenced by a boundary
	// face, and the surface carries the ones it uses.
	if got.header.Vertices != 4 {
		t.Errorf("vertices = %d, want 4", got.header.Vertices)
	}
	if got.header.Stride != 1 || got.header.Truncated {
		t.Errorf("stride = %d truncated = %v, want an untruncated surface",
			got.header.Stride, got.header.Truncated)
	}
	if got.header.Skipped != 0 {
		t.Errorf("skipped = %d, want 0", got.header.Skipped)
	}

	if len(got.header.Boundaries) != 2 {
		t.Fatalf("boundaries = %d, want 2", len(got.header.Boundaries))
	}
	// The names come from BOUNDARIES/BOUNDARY_NAMES, which is the whole point
	// of reading the surface out of the post file rather than surface.dat.
	if b := got.header.Boundaries[0]; b.ID != 1 || b.Name != "PISTON" || b.Faces != 2 || b.Triangles != 3 {
		t.Errorf("boundary[0] = %+v, want id 1 PISTON with 2 faces and 3 triangles", b)
	}
	if b := got.header.Boundaries[1]; b.ID != 2 || b.Name != "HEAD" || b.Faces != 3 || b.Triangles != 4 {
		t.Errorf("boundary[1] = %+v, want id 2 HEAD with 3 faces and 4 triangles", b)
	}

	// Boundary 3 has NUM_ELEMENTS 0 and so contributes no faces; it must not
	// appear as an empty mesh.
	for _, b := range got.header.Boundaries {
		if b.ID == 3 {
			t.Error("boundary 3 has no faces but was included")
		}
	}

	// Index ranges must tile the array exactly and in order, or a boundary
	// would draw another's triangles.
	cursor := 0
	for _, b := range got.header.Boundaries {
		if b.IndexOffset != cursor {
			t.Errorf("boundary %d index offset = %d, want %d", b.ID, b.IndexOffset, cursor)
		}
		if b.IndexCount != b.Triangles*3 {
			t.Errorf("boundary %d index count = %d, want %d", b.ID, b.IndexCount, b.Triangles*3)
		}
		cursor += b.IndexCount
	}
	if cursor != len(got.indices) {
		t.Errorf("index ranges cover %d of %d indices", cursor, len(got.indices))
	}

	for i, v := range got.indices {
		if int(v) >= got.header.Vertices {
			t.Fatalf("index[%d] = %d addresses no vertex (%d sent)", i, v, got.header.Vertices)
		}
	}
	if len(got.values) != 0 {
		t.Errorf("no scalar was requested but %d values were sent", len(got.values))
	}
}

// Values are per-cell in the file and per-vertex on the wire, averaged over
// the faces meeting at each vertex. The fixture's faces carry vertices
// [0,1,2], [0,1,2,3], [1,2,3], [0,2,3] and [0,1,2,3] and sit on cells 0, 1, 2,
// 3 and 0.
func TestH5SurfaceScalarAveragesAdjacentCells(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/post.h5?surface=STREAM_00&scalar=TEMPERATURE")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}

	got := decodeSurface(t, rec)
	if got.header.Scalar != "TEMPERATURE" {
		t.Fatalf("scalar = %q", got.header.Scalar)
	}
	if len(got.values) != got.header.Vertices {
		t.Fatalf("got %d values for %d vertices", len(got.values), got.header.Vertices)
	}

	// TEMPERATURE is [300, 450.5, 1200, 900] over the four cells. Vertex 3 is
	// a corner of the last four faces, on cells 1, 2, 3 and 0.
	want := float32((450.5 + 1200 + 900 + 300) / 4)
	if math.Abs(float64(got.values[3]-want)) > 1e-3 {
		t.Errorf("vertex 3 value = %v, want %v", got.values[3], want)
	}

	// Vertex 0 misses the face on cell 2, so it must not carry that cell's
	// value — this is what separates a real average from a whole-field mean.
	if wantV0 := float32((300 + 450.5 + 900 + 300) / 4); math.Abs(float64(got.values[0]-wantV0)) > 1e-3 {
		t.Errorf("vertex 0 value = %v, want %v", got.values[0], wantV0)
	}

	lo, hi := got.header.Range[0], got.header.Range[1]
	if lo > hi || lo < 300 || hi > 1200 {
		t.Errorf("range = [%v %v], want it inside the field's own bounds", lo, hi)
	}
}

// A field that has gone non-finite is exactly when someone opens the viewer,
// so NaN has to survive the trip rather than take the surface down or pose as
// a reading.
func TestH5SurfaceScalarSurvivesNonFinite(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/post.h5?surface=STREAM_00&scalar=EQUIV_RATIO")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}

	got := decodeSurface(t, rec)
	// EQUIV_RATIO is [0.5, NaN, Inf, 1.5]: the finite cells still set the
	// range, and no non-finite value may leak into it.
	for i, v := range got.header.Range {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Errorf("range[%d] = %v, which is not a number the legend can print", i, v)
		}
	}
	if got.header.Range[0] < 0.5 || got.header.Range[1] > 1.5 {
		t.Errorf("range = %v, want it drawn only from the finite cells", got.header.Range)
	}
	if got.header.Triangles != 7 {
		t.Errorf("triangles = %d: a non-finite field must not drop geometry", got.header.Triangles)
	}

	// The client draws a vertex with no reading in the ramp's no-value grey, so
	// the count of them has to be reported: a surface that is all grey and a
	// surface the viewer failed to colour look exactly alike otherwise.
	holes := 0
	for _, v := range got.values {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			holes++
		}
	}
	if got.header.Unresolved != holes {
		t.Errorf("unresolved = %d, want the %d vertices the payload has no reading for",
			got.header.Unresolved, holes)
	}
	for i, v := range got.values {
		if math.IsInf(float64(v), 0) {
			t.Errorf("values[%d] = %v: an infinity draws as the same grey as no "+
				"reading while the legend still shows a finite span, so it must "+
				"travel as NaN instead", i, v)
		}
	}
}

// The client lays its values view over the payload whenever the header names a
// scalar. Naming one the payload does not carry would have it read the bytes
// after the indices — the edge array, or nothing at all — back as readings.
func TestH5SurfaceHeaderScalarMatchesPayload(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	for _, url := range []string{
		"/api/h5/post.h5?surface=STREAM_00",
		"/api/h5/post.h5?surface=STREAM_00&scalar=TEMPERATURE",
		"/api/h5/post.h5?surface=STREAM_00&scalar=EQUIV_RATIO&edges=1",
	} {
		t.Run(url, func(t *testing.T) {
			rec := h5Get(t, h, token, url)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
			}

			// decodeSurface reads the values array only when the header names a
			// scalar, and fails the test if that leaves the body over- or
			// under-consumed — which is exactly the client's own rule.
			got := decodeSurface(t, rec)
			if got.header.Scalar != "" && len(got.values) != got.header.Vertices {
				t.Errorf("header names %q but carries %d of %d values",
					got.header.Scalar, len(got.values), got.header.Vertices)
			}
			if got.header.Scalar == "" && got.header.Unresolved != 0 {
				t.Errorf("unresolved = %d with no scalar named", got.header.Unresolved)
			}
		})
	}
}

// Edges are the polygon perimeters, never the triangulation: a quad face
// contributes its four sides and not its ear-clip diagonal, and neighbouring
// faces of one boundary share their common edge exactly once. An edge on the
// junction of two boundaries belongs to both outlines.
func TestH5SurfaceEdges(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/post.h5?surface=STREAM_00&edges=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}

	got := decodeSurface(t, rec)
	if got.header.Edges != len(got.edges) {
		t.Fatalf("header says %d edge indices, body carries %d", got.header.Edges, len(got.edges))
	}

	edgeSet := func(b h5SurfaceBoundary) map[[2]uint32]bool {
		set := map[[2]uint32]bool{}
		for i := b.EdgeOffset; i < b.EdgeOffset+b.EdgeCount; i += 2 {
			a, c := got.edges[i], got.edges[i+1]
			if a == c {
				t.Fatalf("boundary %d carries the degenerate edge %d-%d", b.ID, a, c)
			}
			if int(a) >= got.header.Vertices || int(c) >= got.header.Vertices {
				t.Fatalf("edge %d-%d addresses no vertex (%d sent)", a, c, got.header.Vertices)
			}
			key := [2]uint32{min(a, c), max(a, c)}
			if set[key] {
				t.Fatalf("boundary %d sends edge %v twice", b.ID, key)
			}
			set[key] = true
		}
		return set
	}

	// Boundary 1's faces carry vertices [0,1,2] and [0,1,2,3]: perimeters
	// {01,12,02} and {01,12,23,03}, five unique edges — a fan diagonal like
	// 0-2 appearing inside the quad would be a sixth.
	b := got.header.Boundaries[0]
	if b.EdgeOffset != 0 || b.EdgeCount != 10 {
		t.Errorf("boundary 1 edges at %d+%d, want 0+10", b.EdgeOffset, b.EdgeCount)
	}
	want := map[[2]uint32]bool{
		{0, 1}: true, {1, 2}: true, {0, 2}: true, {2, 3}: true, {0, 3}: true,
	}
	if set := edgeSet(b); !maps.Equal(set, want) {
		t.Errorf("boundary 1 edges = %v, want %v", set, want)
	}

	// Boundary 2's faces are [1,2,3], [0,2,3] and [0,1,2,3]: six unique edges.
	b = got.header.Boundaries[1]
	if b.EdgeOffset != 10 || b.EdgeCount != 12 {
		t.Errorf("boundary 2 edges at %d+%d, want 10+12", b.EdgeOffset, b.EdgeCount)
	}
	want = map[[2]uint32]bool{
		{1, 2}: true, {2, 3}: true, {1, 3}: true,
		{0, 2}: true, {0, 3}: true, {0, 1}: true,
	}
	if set := edgeSet(b); !maps.Equal(set, want) {
		t.Errorf("boundary 2 edges = %v, want %v", set, want)
	}

	// Without the flag the section must be absent, not empty-but-present.
	rec = h5Get(t, h, token, "/api/h5/post.h5?surface=STREAM_00")
	if plain := decodeSurface(t, rec); plain.header.Edges != 0 {
		t.Errorf("edges = %d without edges=1, want none", plain.header.Edges)
	}
}

func TestH5SurfaceBoundaryFilter(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/post.h5?surface=STREAM_00&boundaries=2")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}

	got := decodeSurface(t, rec)
	if len(got.header.Boundaries) != 1 || got.header.Boundaries[0].ID != 2 {
		t.Fatalf("boundaries = %+v, want only id 2", got.header.Boundaries)
	}
	if got.header.Faces != 3 || got.header.Triangles != 4 {
		t.Errorf("faces = %d triangles = %d, want 3 and 4", got.header.Faces, got.header.Triangles)
	}
}

// Over the budget the surface is strided down rather than refused, and says so
// — a viewer that silently drew a fraction of the wall would read as a mesh
// with holes in it.
func TestH5SurfaceStridesOverBudget(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/post.h5?surface=STREAM_00&limit=2")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}

	got := decodeSurface(t, rec)
	if got.header.Stride < 2 {
		t.Errorf("stride = %d, want the surface strided down", got.header.Stride)
	}
	if !got.header.Truncated {
		t.Error("truncated = false, so the client would present a partial wall as the whole one")
	}
	if got.header.FacesTotal != 5 {
		t.Errorf("facesTotal = %d, want the file's own count of 5", got.header.FacesTotal)
	}
	if got.header.Faces >= got.header.FacesTotal {
		t.Errorf("faces = %d, want fewer than the %d in the file", got.header.Faces, got.header.FacesTotal)
	}
	// Every boundary that exists must keep at least one face: a boundary that
	// vanished entirely would read as absent from the case.
	if len(got.header.Boundaries) != 2 {
		t.Errorf("boundaries = %d, want both to survive striding", len(got.header.Boundaries))
	}
}

func TestH5SurfaceRejectsFilesWithoutAMesh(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	for _, tc := range []struct{ name, url string }{
		{"restart has no connectivity", "/api/h5/restart.h5?surface=STREAM_00"},
		{"missing stream", "/api/h5/post.h5?surface=STREAM_99"},
		{"unknown scalar", "/api/h5/post.h5?surface=STREAM_00&scalar=NOT_A_FIELD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if rec := h5Get(t, h, token, tc.url); rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", rec.Code)
			}
		})
	}
}

// The surface is fetched from script, so it stays header-only. Only the CSV
// subset may travel on a query token, because only it is loaded by an anchor.
func TestH5SurfaceRequiresAuthHeader(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	req, _ := http.NewRequest(http.MethodGet,
		"/api/h5/post.h5?surface=STREAM_00&auth="+token, http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for a token in the query", rec.Code)
	}
}

// A cut cell can produce a non-convex face. Fanning one from corner 0 lays
// triangles outside it — for this arrowhead a fan covers 14 against a true
// area of 10 — so the triangulation has to clip ears instead.
func TestEarClipHandlesNonConvexPolygons(t *testing.T) {
	arrow := [][3]float64{{0, 0, 0}, {4, 0, 0}, {4, 4, 0}, {2, 1, 0}, {0, 4, 0}}

	tris := earClip(arrow, nil)
	if len(tris) != 9 {
		t.Fatalf("got %d indices, want 9 (three triangles for five corners)", len(tris))
	}

	area := 0.0
	for i := 0; i < len(tris); i += 3 {
		a, b, c := arrow[tris[i]], arrow[tris[i+1]], arrow[tris[i+2]]
		area += math.Abs((b[0]-a[0])*(c[1]-a[1])-(c[0]-a[0])*(b[1]-a[1])) / 2
	}
	if math.Abs(area-10) > 1e-9 {
		t.Errorf("triangulated area = %v, want 10 — the notch was covered over", area)
	}

	for _, i := range tris {
		if int(i) >= len(arrow) {
			t.Fatalf("corner index %d addresses no vertex", i)
		}
	}
}

func TestEarClipDegenerateInput(t *testing.T) {
	t.Run("triangle passes through", func(t *testing.T) {
		got := earClip([][3]float64{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}, nil)
		if len(got) != 3 {
			t.Errorf("got %v, want one triangle", got)
		}
	})

	t.Run("too few corners", func(t *testing.T) {
		if got := earClip([][3]float64{{0, 0, 0}, {1, 0, 0}}, nil); len(got) != 0 {
			t.Errorf("got %v, want nothing", got)
		}
	})

	// Collinear corners have no ear to clip. The polygon is degenerate either
	// way, but it must terminate and must not emit a corner that is not there.
	t.Run("collinear", func(t *testing.T) {
		got := earClip([][3]float64{{0, 0, 0}, {1, 0, 0}, {2, 0, 0}, {3, 0, 0}}, nil)
		for _, i := range got {
			if int(i) >= 4 {
				t.Fatalf("corner index %d addresses no vertex", i)
			}
		}
	})
}

// Playback is bound by how fast the surface crosses the wire, so the response
// has to compress when the client will take it — and must still be the same
// bytes underneath.
func TestH5SurfaceCompressesWhenAccepted(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	req, _ := http.NewRequest(http.MethodGet,
		"/api/h5/post.h5?surface=STREAM_00&scalar=TEMPERATURE", http.NoBody)
	req.Header.Set("X-Auth", token)
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	// A stale Content-Length would be the uncompressed size and truncate the
	// body at the client.
	if got := rec.Header().Get("Content-Length"); got != "" {
		t.Errorf("Content-Length = %q, want it unset on a compressed body", got)
	}
	if got := rec.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Errorf("Vary = %q, want it to name Accept-Encoding", got)
	}

	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	plain, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gzip body: %v", err)
	}

	uncompressed := h5Get(t, h, token,
		"/api/h5/post.h5?surface=STREAM_00&scalar=TEMPERATURE")
	if !bytes.Equal(plain, uncompressed.Body.Bytes()) {
		t.Error("compressed body does not decode to the uncompressed one")
	}

	// The decoder has to survive the round trip, not just the byte compare.
	inflated := httptest.NewRecorder()
	inflated.Body = bytes.NewBuffer(plain)
	inflated.Code = http.StatusOK
	if got := decodeSurface(t, inflated); got.header.Triangles == 0 {
		t.Error("triangles = 0 after inflating")
	}
}

// Every browser that reaches this viewer names zstd, and it is the encoding
// the surface is actually sent under; the body has to survive the round trip.
func TestH5SurfacePrefersZstdWhenAccepted(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	req, _ := http.NewRequest(http.MethodGet,
		"/api/h5/post.h5?surface=STREAM_00&scalar=TEMPERATURE", http.NoBody)
	req.Header.Set("X-Auth", token)
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Encoding"); got != "zstd" {
		t.Fatalf("Content-Encoding = %q, want zstd", got)
	}
	if got := rec.Header().Get("Content-Length"); got != "" {
		t.Errorf("Content-Length = %q, want it unset on a compressed body", got)
	}
	if got := rec.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Errorf("Vary = %q, want it to name Accept-Encoding", got)
	}

	zr, err := zstd.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("zstd reader: %v", err)
	}
	defer zr.Close()
	plain, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("zstd body: %v", err)
	}

	uncompressed := h5Get(t, h, token,
		"/api/h5/post.h5?surface=STREAM_00&scalar=TEMPERATURE")
	if !bytes.Equal(plain, uncompressed.Body.Bytes()) {
		t.Error("packed body does not decode to the uncompressed one")
	}

	inflated := httptest.NewRecorder()
	inflated.Body = bytes.NewBuffer(plain)
	inflated.Code = http.StatusOK
	if got := decodeSurface(t, inflated); got.header.Triangles == 0 {
		t.Error("triangles = 0 after unpacking")
	}
}

// The encoders are pooled and reset per response, so a second request must not
// inherit anything from the first.
func TestH5SurfaceZstdSurvivesAPooledEncoder(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	var first []byte
	for i := 0; i < 4; i++ {
		req, _ := http.NewRequest(http.MethodGet,
			"/api/h5/post.h5?surface=STREAM_00", http.NoBody)
		req.Header.Set("X-Auth", token)
		req.Header.Set("Accept-Encoding", "zstd")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		zr, err := zstd.NewReader(rec.Body)
		if err != nil {
			t.Fatalf("run %d: zstd reader: %v", i, err)
		}
		plain, err := io.ReadAll(zr)
		zr.Close()
		if err != nil {
			t.Fatalf("run %d: zstd body: %v", i, err)
		}
		if i == 0 {
			first = plain
			continue
		}
		if !bytes.Equal(plain, first) {
			t.Fatalf("run %d decoded to a different surface", i)
		}
	}
}

// A client that cannot take gzip still has to be answered, and answered with a
// length it can trust.
func TestH5SurfaceUncompressedWithoutAcceptEncoding(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/post.h5?surface=STREAM_00")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q, want it unset", got)
	}
	if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(rec.Body.Len()) {
		t.Errorf("Content-Length = %q, want %d", got, rec.Body.Len())
	}
	decodeSurface(t, rec)
}

// "x-gzip" and a q-value are legal ways to say something other than gzip; a
// substring match on the header would compress for a client that cannot inflate.
func TestH5SurfaceEncodingOnlyWhenNamed(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   string
	}{
		{"gzip", "gzip"},
		{"gzip;q=1.0, identity;q=0.5", "gzip"},
		{"deflate, gzip", "gzip"},
		{" GZIP ", ""},
		{"x-gzip", ""},
		{"identity", ""},
		{"", ""},
		// zstd wins wherever it is named, whichever order it arrives in.
		{"zstd", "zstd"},
		{"gzip, deflate, br, zstd", "zstd"},
		{"zstd, gzip", "zstd"},
		{"zstd;q=1.0, gzip;q=0.8", "zstd"},
		{" ZSTD ", ""},
		// A zero quality is a refusal, not a preference.
		{"zstd;q=0, gzip", "gzip"},
		{"zstd;q=0.0, gzip;q=0", ""},
		{"gzip;q=0", ""},
	} {
		req, _ := http.NewRequest(http.MethodGet, "/", http.NoBody)
		req.Header.Set("Accept-Encoding", tc.header)
		if got := h5Encoding(req); got != tc.want {
			t.Errorf("h5Encoding(%q) = %q, want %q", tc.header, got, tc.want)
		}
	}
}

// A viewer that navigates away mid-extraction has to stop the work, not merely
// stop reading its result: a surface is the most expensive thing this server
// builds, and a prefetching client can queue many at once.
func TestH5SurfaceStopsWhenTheClientHasGone(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"/api/h5/post.h5?surface=STREAM_00", http.NoBody)
	req.Header.Set("X-Auth", token)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Body.Len() != 0 {
		t.Errorf("wrote %d bytes for a client that had already gone", rec.Body.Len())
	}
}

// The handler's own checks sit between the expensive steps; this covers the
// poll inside the face walk, which is where a large case spends its time.
func TestH5ExtractSurfaceHonoursCancellation(t *testing.T) {
	// Big enough to reach a poll, which is deliberately periodic rather than
	// per face.
	const faces = 8192

	in := h5SurfaceInput{
		Offsets:      make([]int64, faces+1),
		PolyToVertex: make([]int64, 3*faces),
		Connected:    make([]int64, 2*faces),
		X:            make([]float64, 3*faces),
		Y:            make([]float64, 3*faces),
		Z:            make([]float64, 3*faces),
		IDs:          []int64{1},
		ByBoundary:   map[int64][]int32{1: make([]int32, faces)},
		Stride:       1,
	}
	for i := 0; i < faces; i++ {
		in.Offsets[i] = int64(3 * i)
		in.ByBoundary[1][i] = int32(i)
		in.Connected[2*i] = -2 // boundary 1 owns the face
		for k := 0; k < 3; k++ {
			v := 3*i + k
			in.PolyToVertex[v] = int64(v)
		}
		in.X[3*i], in.Y[3*i] = float64(i), 0
		in.X[3*i+1], in.Y[3*i+1] = float64(i)+1, 0
		in.X[3*i+2], in.Y[3*i+2] = float64(i), 1
	}
	in.Offsets[faces] = int64(3 * faces)

	if _, err := h5ExtractSurface(context.Background(), in); err != nil {
		t.Fatalf("uncancelled extraction failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h5ExtractSurface(ctx, in); err == nil {
		t.Error("extraction ran to completion for a cancelled request")
	}
}
