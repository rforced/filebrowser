package fbhttp

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/rforced/filebrowser/v2/users"
)

func TestH5SurfaceStride(t *testing.T) {
	for _, tc := range []struct {
		total int
		limit string
		want  int
	}{
		// No budget named draws the surface whole, however large it is.
		{8_487_832, "", 1},
		{1, "", 1},
		{0, "", 1},
		// A budget above the surface changes nothing.
		{100, "100", 1},
		{100, "1000", 1},
		// Below it, the surface is thinned just far enough to fit.
		{8_487_832, "5000000", 2},
		{100, "50", 2},
		{100, "33", 4},
		{100, "1", 100},
		// A budget that is not one is no budget at all, and must never be read
		// as a stride of zero: that would divide by it later.
		{100, "0", 1},
		{100, "-5", 1},
		{100, "nonsense", 1},
		{100, "1e6", 1},
	} {
		if got := h5SurfaceStride(tc.total, tc.limit); got != tc.want {
			t.Errorf("h5SurfaceStride(%d, %q) = %d, want %d",
				tc.total, tc.limit, got, tc.want)
		}
	}
}

// The ceiling is a memory budget wearing a geometry label, so it is pinned
// from both sides: past every real case, and inside what one response can be
// packed into. The ratios are the measured file's, which the payload formula
// reproduces to within a megabyte of what it actually wrote.
func TestH5MaxSurfaceTrianglesIsPricedNotGuessed(t *testing.T) {
	const (
		measured     = 8_487_832
		perVertex    = 4_214_417.0 / 8_486_962.0
		perFace      = 4_496_951.0 / 8_486_962.0
		edgesPerFace = 1.96
		mib          = 1 << 20
	)

	if h5MaxSurfaceTriangles <= measured {
		t.Errorf("h5MaxSurfaceTriangles = %d, want room past the %d measured",
			h5MaxSurfaceTriangles, measured)
	}

	// What h5WriteSurface would frame at the ceiling: positions and indices at
	// 12 bytes each, the scalar at 4 a vertex, an edge segment at 8.
	verts := float64(h5MaxSurfaceTriangles) * perVertex
	faces := float64(h5MaxSurfaceTriangles) * perFace
	payload := (verts*12 + float64(h5MaxSurfaceTriangles)*12 +
		verts*4 + faces*edgesPerFace*8) / mib

	// The packed buffer and the arrays it is copied from are live together, and
	// a FileSystem box runs two of these at once.
	const budgetMiB = 400
	if payload > budgetMiB {
		t.Errorf("a response at the ceiling frames %.0f MiB, past the %d MiB budget: "+
			"h5MaxSurfaceTriangles = %d is set higher than one response can be packed at",
			payload, budgetMiB, h5MaxSurfaceTriangles)
	}
	// Whole at the top step, and refused only past the ceiling.
	if got := h5SurfaceStride(measured, ""); got != 1 {
		t.Errorf("stride = %d for a measured case at the top step, want it whole", got)
	}
	if measured/h5SurfaceStride(measured, "") > h5MaxSurfaceTriangles {
		t.Error("a measured case would be refused at the top step")
	}
	over := h5MaxSurfaceTriangles + 1
	if over/h5SurfaceStride(over, "") <= h5MaxSurfaceTriangles {
		t.Error("a surface past the ceiling would not be refused")
	}
}

// The top step used to sit at the server's own ceiling, so a surface past it
// came back quietly halved. It must arrive whole now.
func TestH5SurfaceDrawsWholeWithoutABudget(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/post.h5?surface=STREAM_00")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}

	got := decodeSurface(t, rec)
	if got.header.Stride != 1 {
		t.Errorf("stride = %d, want the surface drawn whole", got.header.Stride)
	}
	if got.header.Truncated {
		t.Error("truncated = true on a surface nothing was dropped from")
	}
	if got.header.Faces != got.header.FacesTotal {
		t.Errorf("faces = %d of %d, want every one drawn",
			got.header.Faces, got.header.FacesTotal)
	}
}

// A budget the surface already fits inside must not thin it either.
func TestH5SurfaceKeepsWholeUnderAGenerousBudget(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	whole := decodeSurface(t, h5Get(t, h, token, "/api/h5/post.h5?surface=STREAM_00"))
	rec := h5Get(t, h, token,
		"/api/h5/post.h5?surface=STREAM_00&limit="+strconv.Itoa(whole.header.Triangles*4))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}

	got := decodeSurface(t, rec)
	if got.header.Stride != 1 || got.header.Truncated {
		t.Errorf("stride = %d truncated = %v, want the surface left whole",
			got.header.Stride, got.header.Truncated)
	}
	if got.header.Triangles != whole.header.Triangles {
		t.Errorf("triangles = %d, want the %d the whole surface has",
			got.header.Triangles, whole.header.Triangles)
	}
}
