package fbhttp

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/rforced/filebrowser/v2/users"
)

func TestH5SurfaceWorkersLeavesHeadroom(t *testing.T) {
	restore := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(restore)

	for _, tc := range []struct {
		procs, boundaries, want int
	}{
		// Two cores are held back for the rest of the server.
		{8, 32, 6},
		{4, 32, 2},
		// Never below one, however little there is to go round.
		{3, 32, 1},
		{2, 32, 1},
		{1, 32, 1},
		// Never more workers than there are boundaries to give them.
		{16, 3, 3},
		{16, 1, 1},
		{16, 0, 1},
	} {
		runtime.GOMAXPROCS(tc.procs)
		if got := h5SurfaceWorkers(tc.boundaries); got != tc.want {
			t.Errorf("h5SurfaceWorkers(%d) on %d procs = %d, want %d",
				tc.boundaries, tc.procs, got, tc.want)
		}
	}
}

// The boundaries are cut concurrently but stitched in the order they were
// asked for, so the surface must not depend on how many workers cut it —
// including the per-vertex averages, which are summed boundary by boundary.
func TestH5SurfaceParallelMatchesSequential(t *testing.T) {
	restore := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(restore)

	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	for _, query := range []string{
		"/api/h5/post.h5?surface=STREAM_00",
		"/api/h5/post.h5?surface=STREAM_00&scalar=TEMPERATURE",
		"/api/h5/post.h5?surface=STREAM_00&scalar=TEMPERATURE&edges=1",
		"/api/h5/post.h5?surface=STREAM_00&edges=1",
	} {
		var want []byte
		for _, procs := range []int{1, 3, 4, 8, 16} {
			runtime.GOMAXPROCS(procs)

			req, _ := http.NewRequest(http.MethodGet, query, http.NoBody)
			req.Header.Set("X-Auth", token)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("%s on %d procs: status %d", query, procs, rec.Code)
			}
			got := rec.Body.Bytes()
			if want == nil {
				want = got
				continue
			}
			if string(got) != string(want) {
				t.Errorf("%s on %d procs: surface differs from the sequential cut",
					query, procs)
			}
		}
	}
}

// A boundary is cut against a vertex list of its own and the slots are handed
// back afterwards, so a worker taking a second boundary must not see anything
// the first one left behind.
func TestH5CutBoundaryReusesScratchCleanly(t *testing.T) {
	in := h5SurfaceInput{
		Offsets:      []int64{0, 3, 6},
		PolyToVertex: []int64{0, 1, 2, 0, 1, 2},
		Connected:    []int64{-2, 0, -3, 0},
		X:            []float64{0, 1, 0},
		Y:            []float64{0, 0, 1},
		Z:            []float64{0, 0, 0},
		IDs:          []int64{1, 2},
		ByBoundary:   map[int64][]int32{1: {0}, 2: {1}},
		Stride:       1,
		WithEdges:    true,
	}

	s := h5NewScratch(3, true)
	first, err := h5CutBoundary(context.Background(), in, 1, s, 3)
	if err != nil {
		t.Fatal(err)
	}
	second, err := h5CutBoundary(context.Background(), in, 2, s, 3)
	if err != nil {
		t.Fatal(err)
	}

	// Both faces name the same three vertices, so each boundary must have
	// taken all three for itself rather than the second finding them spent.
	for name, p := range map[string]h5BoundaryPart{"first": first, "second": second} {
		if len(p.verts) != 3 {
			t.Errorf("%s boundary took %d vertices, want 3", name, p.verts)
		}
		if len(p.indices) != 3 {
			t.Errorf("%s boundary made %d indices, want 3", name, len(p.indices))
		}
		if len(p.edges) != 6 {
			t.Errorf("%s boundary made %d edge indices, want 6", name, len(p.edges))
		}
		if p.faces != 1 || p.triangles != 1 {
			t.Errorf("%s boundary = %d faces %d triangles, want 1 and 1",
				name, p.faces, p.triangles)
		}
		if math.IsInf(p.bounds[0], 1) {
			t.Errorf("%s boundary left its bounds unset", name)
		}
	}

	for i, v := range s.slots {
		if v != -1 {
			t.Errorf("scratch slot %d = %d, want it released", i, v)
		}
	}
}
