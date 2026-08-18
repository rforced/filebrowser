package fbhttp

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/marusama/semaphore/v2"

	"github.com/rforced/filebrowser/v2/hdf5"
)

// A post file stores its mesh face-by-face in CSR form, and marks a boundary
// face by writing the boundary into the owner slot as -(id+1) rather than a
// cell index. That encoding is what makes the boundary surface recoverable:
// filter the faces whose owner is negative, look their vertices up through
// POLYGON_TO_VERTEX, and the result is the wetted geometry with the real
// boundary names attached. Verified exact across CONVERGE 4, 5 and 6 — the
// negative count equals sum(BOUNDARIES/NUM_ELEMENTS) in every file measured.

// h5DefaultSurfaceTriangles bounds one response. The largest post file
// measured (CONVERGE 6, 225k cells) yields 787k triangles from 425k boundary
// faces, so the default carries several times the biggest real case before a
// surface has to be strided down.
const h5DefaultSurfaceTriangles = 3_000_000

// h5MaxConnectivity caps the connectivity arrays the extractor will decode.
// Decoding widens the file's int32 storage to int64, so without a ceiling a
// file far outside anything CONVERGE writes would be answered by exhausting
// the box's memory instead of by an error.
const h5MaxConnectivity = 64 << 20

// h5SurfaceMagic identifies the binary framing below. The payload is a few
// megabytes of positions and indices — as JSON the largest measured case would
// be ~26MB of text and several seconds of parsing, so it goes over the wire in
// the layout the GPU wants.
const h5SurfaceMagic = "FBSURF01"

// h5SurfaceSem bounds concurrent extractions. One surface reads a post file's
// whole connectivity — hundreds of megabytes, decompressed by ZFS on the way
// in — before a single triangle is cut, so a viewer prefetching frames will
// otherwise put as many of those in flight as the browser opens sockets and
// saturate the box. Half the cores leaves the rest of the server responsive.
var h5SurfaceSem = semaphore.New(max(1, runtime.GOMAXPROCS(0)/2))

// h5Abandoned answers a request whose client has already gone away: no body,
// no status, no log line. A viewer scrubbing or stepping through frames
// cancels constantly, and each cancellation is the client behaving correctly
// rather than a fault worth recording.
func h5Abandoned() (int, error) {
	return 0, nil
}

type h5SurfaceBoundary struct {
	ID   int64  `json:"id"`
	Name string `json:"name,omitempty"`
	// Faces is how many were drawn, which is below the file's own count when
	// the surface was strided down.
	Faces     int `json:"faces"`
	Triangles int `json:"triangles"`
	// IndexOffset and IndexCount address this boundary's slice of the shared
	// index array, so the client can build one mesh per boundary over a single
	// uploaded vertex buffer.
	IndexOffset int `json:"indexOffset"`
	IndexCount  int `json:"indexCount"`
	// EdgeOffset and EdgeCount address this boundary's slice of the edge index
	// array; both stay zero unless edges were requested.
	EdgeOffset int `json:"edgeOffset"`
	EdgeCount  int `json:"edgeCount"`
}

type h5SurfaceHeader struct {
	Stream     string              `json:"stream"`
	Vertices   int                 `json:"vertices"`
	Triangles  int                 `json:"triangles"`
	Faces      int                 `json:"faces"`
	FacesTotal int                 `json:"facesTotal"`
	Stride     int                 `json:"stride"`
	Truncated  bool                `json:"truncated,omitempty"`
	Skipped    int                 `json:"skipped,omitempty"`
	Bounds     [6]float64          `json:"bounds"`
	Scalar     string              `json:"scalar,omitempty"`
	Range      [2]float64          `json:"range"`
	Edges      int                 `json:"edges,omitempty"`
	Boundaries []h5SurfaceBoundary `json:"boundaries"`
}

// h5SurfaceResponse extracts the boundary surface of one stream and writes it
// as positions plus per-boundary index ranges.
//
// Values are per-vertex, averaged over the boundary faces meeting at that
// vertex: the scalar is a cell quantity, so it is genuinely per-face, but
// sending it per-face would mean unshared vertices and roughly triple the
// payload. The averaging is visible only as a gradient across the width of one
// cell, which is the same interpolation a post-processor shows for point data.
func h5SurfaceResponse(w http.ResponseWriter, r *http.Request, f *hdf5.File, query map[string][]string) (int, error) {
	// Queueing here rather than at the door means a request whose client left
	// while it waited never starts: Acquire gives up as soon as ctx is done.
	ctx := r.Context()
	if err := h5SurfaceSem.Acquire(ctx, 1); err != nil {
		return h5Abandoned()
	}
	defer h5SurfaceSem.Release(1)

	stream := strings.Trim(firstValue(query, "surface"), "/")
	if stream == "" || stream == "1" || stream == "true" {
		stream = "STREAM_00"
	}
	if _, err := f.Group(stream); err != nil {
		return http.StatusNotFound, err
	}

	offsets, err := h5Ints(f, stream+"/CONNECTIVITY/POLYGON_OFFSET")
	if err != nil {
		return http.StatusNotFound, fmt.Errorf("%s carries no face connectivity: %w", stream, err)
	}
	polyToVertex, err := h5Ints(f, stream+"/CONNECTIVITY/POLYGON_TO_VERTEX")
	if err != nil {
		return http.StatusNotFound, err
	}
	connected, err := h5Ints(f, stream+"/CONNECTIVITY/CONNECTED_CELLS")
	if err != nil {
		return http.StatusNotFound, err
	}
	if len(polyToVertex) > h5MaxConnectivity || len(offsets) > h5MaxConnectivity {
		return http.StatusRequestEntityTooLarge,
			fmt.Errorf("mesh too large to extract: %d faces, %d face vertices", len(offsets)-1, len(polyToVertex))
	}
	if ctx.Err() != nil {
		return h5Abandoned()
	}

	xs, ys, zs, err := h5VertexCoords(f, stream)
	if err != nil {
		return http.StatusNotFound, err
	}
	if ctx.Err() != nil {
		return h5Abandoned()
	}

	faceCount := len(offsets) - 1
	if faceCount < 1 {
		return http.StatusUnprocessableEntity, fmt.Errorf("%s has no faces", stream)
	}

	// Group the boundary faces by boundary so each one lands in a contiguous
	// run of the index array.
	filter := h5SurfaceFilter(firstValue(query, "boundaries"))
	byBoundary := map[int64][]int32{}
	for face := 0; face < faceCount; face++ {
		if 2*face >= len(connected) {
			break
		}
		owner := connected[2*face]
		if owner >= 0 {
			continue // interior face: both sides are cells
		}
		id := -owner - 1
		if filter != nil && !filter[id] {
			continue
		}
		byBoundary[id] = append(byBoundary[id], int32(face))
	}
	if len(byBoundary) == 0 {
		return http.StatusUnprocessableEntity,
			fmt.Errorf("%s has no boundary faces", stream)
	}

	ids := make([]int64, 0, len(byBoundary))
	for id := range byBoundary {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	// Price the whole surface before drawing any of it, so the stride is
	// decided once rather than discovered part-way through.
	facesTotal, trianglesTotal := 0, 0
	for _, id := range ids {
		for _, face := range byBoundary[id] {
			n := h5FaceSize(offsets, int(face))
			if n < 3 {
				continue
			}
			facesTotal++
			trianglesTotal += n - 2
		}
	}

	limit := h5DefaultSurfaceTriangles
	if v := firstValue(query, "limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n < limit {
			limit = n
		}
	}
	stride := 1
	if trianglesTotal > limit && limit > 0 {
		stride = (trianglesTotal + limit - 1) / limit
	}

	scalar := firstValue(query, "scalar")
	var cellValues []float64
	if scalar != "" {
		if cellValues, err = h5CellValues(f, stream, scalar); err != nil {
			return http.StatusNotFound, err
		}
	}

	edges := firstValue(query, "edges")
	surface, err := h5ExtractSurface(ctx, h5SurfaceInput{
		Offsets:      offsets,
		PolyToVertex: polyToVertex,
		Connected:    connected,
		X:            xs, Y: ys, Z: zs,
		IDs:        ids,
		ByBoundary: byBoundary,
		CellValues: cellValues,
		Stride:     stride,
		WithEdges:  edges == "1" || edges == "true",
	})
	if err != nil {
		return h5Abandoned()
	}

	names := map[int64]string{}
	for _, b := range h5ReadBoundaries(f) {
		names[b.ID] = b.Name
	}
	for i := range surface.Boundaries {
		surface.Boundaries[i].Name = names[surface.Boundaries[i].ID]
	}

	header := surface.Header
	header.Stream = stream
	header.FacesTotal = facesTotal
	header.Stride = stride
	header.Truncated = stride > 1
	header.Scalar = scalar
	header.Edges = len(surface.Edges)
	header.Boundaries = surface.Boundaries

	// Framing the response copies every array into one contiguous buffer, which
	// is worth skipping outright when there is no longer anyone to send it to.
	if ctx.Err() != nil {
		return h5Abandoned()
	}
	return h5WriteSurface(w, header, surface.Positions, surface.Indices, surface.Values, surface.Edges)
}

type h5SurfaceInput struct {
	Offsets      []int64
	PolyToVertex []int64
	Connected    []int64
	X, Y, Z      []float64
	IDs          []int64
	ByBoundary   map[int64][]int32
	CellValues   []float64
	Stride       int
	WithEdges    bool
}

type h5SurfaceResult struct {
	Header     h5SurfaceHeader
	Boundaries []h5SurfaceBoundary
	Positions  []float32
	Indices    []uint32
	Values     []float32
	Edges      []uint32
}

// h5ExtractSurface walks the selected faces and builds the drawable mesh. It
// is separated from the HTTP layer so the geometry can be tested directly.
//
// It gives up when ctx is cancelled: this is the long pole of a surface
// request — hundreds of thousands of faces, each triangulated — and running it
// out for a client that has already navigated away is the difference between
// an idle box and a saturated one.
func h5ExtractSurface(ctx context.Context, in h5SurfaceInput) (h5SurfaceResult, error) {
	nverts := min(len(in.X), min(len(in.Y), len(in.Z)))
	// Vertices are shared between faces, so the mesh is emitted indexed over a
	// compacted vertex list: only the vertices the boundary actually touches
	// are sent, which on the measured cases is under a third of the mesh.
	remap := make([]int32, nverts)
	for i := range remap {
		remap[i] = -1
	}

	out := h5SurfaceResult{}
	// Accumulators for the per-vertex scalar average.
	var sums []float64
	var counts []int32

	poly := make([][3]float64, 0, 16)
	local := make([]uint32, 0, 48)
	global := make([]uint32, 0, 16)
	skipped := 0

	bounds := [6]float64{
		math.Inf(1), math.Inf(1), math.Inf(1),
		math.Inf(-1), math.Inf(-1), math.Inf(-1),
	}

	var seenEdges map[uint64]struct{}
	walked := 0
	for _, id := range in.IDs {
		entry := h5SurfaceBoundary{
			ID:          id,
			IndexOffset: len(out.Indices),
			EdgeOffset:  len(out.Edges),
		}
		if in.WithEdges {
			seenEdges = make(map[uint64]struct{}, 2*len(in.ByBoundary[id]))
		}

		for k, face := range in.ByBoundary[id] {
			// Polled rather than tested per face: the check is cheap, but so is
			// a strided iteration, and this loop runs into the millions.
			walked++
			if walked%4096 == 0 && ctx.Err() != nil {
				return h5SurfaceResult{}, ctx.Err()
			}
			if in.Stride > 1 && k%in.Stride != 0 {
				continue
			}
			start, end := in.Offsets[face], in.Offsets[face+1]
			if start < 0 || end > int64(len(in.PolyToVertex)) || end-start < 3 {
				continue
			}

			// Collect the polygon, refusing it outright if any corner is
			// unusable: a face with a NaN corner cannot be placed in the scene
			// at all, and one bad coordinate would drag the whole model's
			// bounding box — and so the camera — off with it.
			poly = poly[:0]
			global = global[:0]
			ok := true
			for i := start; i < end; i++ {
				v := in.PolyToVertex[i]
				if v < 0 || v >= int64(nverts) {
					ok = false
					break
				}
				x, y, z := in.X[v], in.Y[v], in.Z[v]
				if !h5Finite(x) || !h5Finite(y) || !h5Finite(z) {
					ok = false
					break
				}
				poly = append(poly, [3]float64{x, y, z})
				global = append(global, uint32(v))
			}
			if !ok {
				skipped++
				continue
			}

			local = earClip(poly, local[:0])
			if len(local) == 0 {
				skipped++
				continue
			}

			// Map this face's corners into the compacted vertex list.
			for i, v := range global {
				if remap[v] >= 0 {
					continue
				}
				remap[v] = int32(len(out.Positions) / 3)
				out.Positions = append(out.Positions,
					float32(poly[i][0]), float32(poly[i][1]), float32(poly[i][2]))
				sums = append(sums, 0)
				counts = append(counts, 0)

				bounds[0] = math.Min(bounds[0], poly[i][0])
				bounds[1] = math.Min(bounds[1], poly[i][1])
				bounds[2] = math.Min(bounds[2], poly[i][2])
				bounds[3] = math.Max(bounds[3], poly[i][0])
				bounds[4] = math.Max(bounds[4], poly[i][1])
				bounds[5] = math.Max(bounds[5], poly[i][2])
			}

			for _, corner := range local {
				out.Indices = append(out.Indices, uint32(remap[global[corner]]))
			}

			// The edges are the polygon's perimeter, never the triangulation:
			// an ear-clip diagonal is an artifact of drawing, not of the mesh.
			// Neighbouring faces of one boundary share their common edge, so
			// each is deduplicated within the boundary's run.
			if in.WithEdges {
				for i := range global {
					a := uint32(remap[global[i]])
					b := uint32(remap[global[(i+1)%len(global)]])
					if a == b {
						continue
					}
					key := uint64(min(a, b))<<32 | uint64(max(a, b))
					if _, ok := seenEdges[key]; ok {
						continue
					}
					seenEdges[key] = struct{}{}
					out.Edges = append(out.Edges, a, b)
				}
			}

			if in.CellValues != nil {
				// The cell on the fluid side sits in the slot the boundary did
				// not take; the owner slot is always the negative one.
				cell := in.Connected[2*int(face)+1]
				if cell >= 0 && cell < int64(len(in.CellValues)) {
					if v := in.CellValues[cell]; h5Finite(v) {
						for _, g := range global {
							ci := remap[g]
							sums[ci] += v
							counts[ci]++
						}
					}
				}
			}

			entry.Faces++
			entry.Triangles += len(local) / 3
		}

		entry.IndexCount = len(out.Indices) - entry.IndexOffset
		entry.EdgeCount = len(out.Edges) - entry.EdgeOffset
		if entry.IndexCount == 0 {
			continue
		}
		out.Boundaries = append(out.Boundaries, entry)
	}

	if len(out.Positions) == 0 {
		bounds = [6]float64{}
	}

	valueRange := [2]float64{math.Inf(1), math.Inf(-1)}
	if in.CellValues != nil {
		out.Values = make([]float32, len(counts))
		for i, c := range counts {
			if c == 0 {
				// Touched only by faces with no usable value. NaN travels to
				// the client as a hole in the field rather than as a reading.
				out.Values[i] = float32(math.NaN())
				continue
			}
			v := sums[i] / float64(c)
			out.Values[i] = float32(v)
			valueRange[0] = math.Min(valueRange[0], v)
			valueRange[1] = math.Max(valueRange[1], v)
		}
	}
	if math.IsInf(valueRange[0], 1) {
		valueRange = [2]float64{0, 0}
	}

	faces, triangles := 0, 0
	for _, b := range out.Boundaries {
		faces += b.Faces
		triangles += b.Triangles
	}
	out.Header = h5SurfaceHeader{
		Vertices:  len(out.Positions) / 3,
		Triangles: triangles,
		Faces:     faces,
		Skipped:   skipped,
		Bounds:    bounds,
		Range:     valueRange,
	}
	return out, nil
}

// earClip triangulates one simple planar polygon, returning corner indices
// into pts. Boundary polygons are small — 7 corners at most in the CONVERGE 4
// cases measured, 10 in CONVERGE 6 — but a cut cell can still produce a
// non-convex face, where a fan from corner 0 lays triangles outside the
// polygon. Clipping ears costs the same n-2 triangles and cannot.
func earClip(pts [][3]float64, out []uint32) []uint32 {
	n := len(pts)
	if n < 3 {
		return out
	}
	if n == 3 {
		return append(out, 0, 1, 2)
	}

	// Newell's normal, which is stable for the slightly non-planar faces a cut
	// cell produces, then drop the axis it points most strongly along to get a
	// projection that cannot collapse the polygon to a line.
	var nx, ny, nz float64
	for i := 0; i < n; i++ {
		a, b := pts[i], pts[(i+1)%n]
		nx += (a[1] - b[1]) * (a[2] + b[2])
		ny += (a[2] - b[2]) * (a[0] + b[0])
		nz += (a[0] - b[0]) * (a[1] + b[1])
	}
	ax, ay := 0, 1
	switch {
	case math.Abs(nx) >= math.Abs(ny) && math.Abs(nx) >= math.Abs(nz):
		ax, ay = 1, 2
	case math.Abs(ny) >= math.Abs(nz):
		ax, ay = 2, 0
	}

	flat := make([][2]float64, n)
	for i, p := range pts {
		flat[i] = [2]float64{p[ax], p[ay]}
	}

	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	if h5SignedArea(flat, idx) < 0 {
		for i, j := 0, len(idx)-1; i < j; i, j = i+1, j-1 {
			idx[i], idx[j] = idx[j], idx[i]
		}
	}

	for len(idx) > 3 {
		clipped := false
		for i := range idx {
			prev := idx[(i+len(idx)-1)%len(idx)]
			curr := idx[i]
			next := idx[(i+1)%len(idx)]
			if !h5IsEar(flat, idx, prev, curr, next) {
				continue
			}
			out = append(out, uint32(prev), uint32(curr), uint32(next))
			idx = append(idx[:i], idx[i+1:]...)
			clipped = true
			break
		}
		if !clipped {
			// Degenerate: collinear corners, or a face that is not simple.
			// Fanning what is left keeps the surface closed, where dropping it
			// would read as missing geometry.
			break
		}
	}
	for i := 1; i+1 < len(idx); i++ {
		out = append(out, uint32(idx[0]), uint32(idx[i]), uint32(idx[i+1]))
	}
	return out
}

func h5SignedArea(flat [][2]float64, idx []int) float64 {
	area := 0.0
	for i := range idx {
		a := flat[idx[i]]
		b := flat[idx[(i+1)%len(idx)]]
		area += a[0]*b[1] - b[0]*a[1]
	}
	return area / 2
}

// h5IsEar reports whether the corner at curr can be clipped: convex, and with
// no other corner of the polygon inside the triangle it would cut off.
func h5IsEar(flat [][2]float64, idx []int, prev, curr, next int) bool {
	a, b, c := flat[prev], flat[curr], flat[next]
	cross := (b[0]-a[0])*(c[1]-a[1]) - (b[1]-a[1])*(c[0]-a[0])
	if cross <= 0 {
		return false // reflex or degenerate with the polygon wound CCW
	}
	for _, other := range idx {
		if other == prev || other == curr || other == next {
			continue
		}
		if h5PointInTriangle(flat[other], a, b, c) {
			return false
		}
	}
	return true
}

func h5PointInTriangle(p, a, b, c [2]float64) bool {
	d1 := (p[0]-b[0])*(a[1]-b[1]) - (a[0]-b[0])*(p[1]-b[1])
	d2 := (p[0]-c[0])*(b[1]-c[1]) - (b[0]-c[0])*(p[1]-c[1])
	d3 := (p[0]-a[0])*(c[1]-a[1]) - (c[0]-a[0])*(p[1]-a[1])
	hasNeg := d1 < 0 || d2 < 0 || d3 < 0
	hasPos := d1 > 0 || d2 > 0 || d3 > 0
	return !hasNeg || !hasPos
}

func h5FaceSize(offsets []int64, face int) int {
	if face+1 >= len(offsets) {
		return 0
	}
	return int(offsets[face+1] - offsets[face])
}

func h5Finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func h5Ints(f *hdf5.File, path string) ([]int64, error) {
	ds, err := f.Dataset(path)
	if err != nil {
		return nil, err
	}
	return ds.Ints()
}

func h5VertexCoords(f *hdf5.File, stream string) (xs, ys, zs []float64, err error) {
	var got [3][]float64
	for i, axis := range []string{"X", "Y", "Z"} {
		ds, err := f.Dataset(stream + "/VERTEX_COORDINATES/" + axis)
		if err != nil {
			return nil, nil, nil, err
		}
		if got[i], err = ds.Floats(); err != nil {
			return nil, nil, nil, err
		}
	}
	return got[0], got[1], got[2], nil
}

// h5CellValues reads one cell-centred variable. Post files hold them under
// CELL_CENTER_DATA and restarts under CELL_CENTER, the same split the summary
// handles.
func h5CellValues(f *hdf5.File, stream, name string) ([]float64, error) {
	if strings.ContainsAny(name, "/") {
		return nil, fmt.Errorf("invalid variable name %q", name)
	}
	for _, group := range []string{"CELL_CENTER_DATA", "CELL_CENTER"} {
		ds, err := f.Dataset(stream + "/" + group + "/" + name)
		if err != nil {
			continue
		}
		return ds.Floats()
	}
	return nil, fmt.Errorf("no cell variable %s in %s", name, stream)
}

func h5SurfaceFilter(list string) map[int64]bool {
	ids := h5SplitList(list)
	if len(ids) == 0 {
		return nil
	}
	out := map[int64]bool{}
	for _, s := range ids {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			out[v] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// h5WriteSurface frames the mesh: an 8-byte magic, the header length, the JSON
// header padded to a 4-byte boundary, then the raw arrays. The padding is what
// lets the client lay Float32Array and Uint32Array views straight over the
// response buffer instead of copying it.
func h5WriteSurface(w http.ResponseWriter, header h5SurfaceHeader, positions []float32, indices []uint32, values []float32, edges []uint32) (int, error) {
	if header.Boundaries == nil {
		header.Boundaries = []h5SurfaceBoundary{}
	}
	meta, err := json.Marshal(header)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	for len(meta)%4 != 0 {
		meta = append(meta, ' ')
	}

	total := len(h5SurfaceMagic) + 4 + len(meta) +
		len(positions)*4 + len(indices)*4 + len(values)*4 + len(edges)*4
	buf := make([]byte, 0, total)
	buf = append(buf, h5SurfaceMagic...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(meta)))
	buf = append(buf, meta...)
	for _, v := range positions {
		buf = binary.LittleEndian.AppendUint32(buf, math.Float32bits(v))
	}
	for _, v := range indices {
		buf = binary.LittleEndian.AppendUint32(buf, v)
	}
	for _, v := range values {
		buf = binary.LittleEndian.AppendUint32(buf, math.Float32bits(v))
	}
	for _, v := range edges {
		buf = binary.LittleEndian.AppendUint32(buf, v)
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
	if _, err := w.Write(buf); err != nil {
		return 0, err
	}
	return 0, nil
}
