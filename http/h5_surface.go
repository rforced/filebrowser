package fbhttp

import (
	"compress/gzip"
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
	"sync"
	"sync/atomic"

	"github.com/klauspost/compress/zstd"
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

// h5MaxSurfaceTriangles is the most one response will carry. A client that
// names a smaller budget is thinned to fit it; one that names none is drawn
// whole, and past this it is refused rather than thinned.
//
// Refused rather than thinned because thinning is a bad trade at this size.
// The stride drops whole faces, and on a measured 2.2M-cell case dropping half
// of them shed only 9.6% of the vertices — the survivors still touch nearly
// every one — so it gave up half the wall to save under a third of the wire.
// What arrives is not a coarser surface but a holed one, which is worse than
// being told the surface is too big to draw.
//
// Priced rather than guessed, because the number is a memory budget wearing a
// geometry label. At this ceiling one response is ~206MB of positions and
// indices, ~228MB carrying a scalar and ~323MB carrying edges as well — and
// h5WriteSurface holds the packed buffer and the arrays it copies at the same
// time, so peak is about twice that. A FileSystem box runs two extractions at
// once. Raising it means paying that again: price it before moving it.
//
// The largest post file measured is 8.5M triangles from 4.5M boundary faces,
// so this leaves a real case comfortably inside. It is a plain decimal because
// it counts triangles, not bytes: 16 << 20 reads as sixteen million and is
// 16,777,216, which is how it was set too high the first time.
const h5MaxSurfaceTriangles = 12_000_000

// h5MaxFaces caps the mesh whose face table the extractor will decode. Only
// POLYGON_OFFSET is read whole now — 8 bytes a face once widened from the
// file's int32 storage — because it is indexed randomly, by drawn face, right
// through the cut. CONNECTED_CELLS is the same size again twice over and is
// read in blocks instead: it is wanted once per face, in order, which is what
// h5ScanBoundaryFaces exploits.
//
// Priced against the box this runs on rather than the mesh: a FileSystem is
// 16GB and 4 threads with the solver beside it. At this ceiling the face table
// is ~805MB, which is what the old 32<<20 cost at 24 bytes a face — so the
// same memory now buys three times the geometry. A measured 27.4M-cell case
// has 84,125,742 faces and sits inside it; the mesh that prompted the raise
// had 37.5M and was refused by 11%.
const h5MaxFaces = 96 << 20

// cgnsMaxFaces is deliberately the lower, older ceiling. CGNS stores the
// relationship the other way round, so colouring a wall inverts the cell
// section into an owner table of 16 bytes a face on top of the offsets — the
// native path's saving does not carry over, and neither does its headroom.
const cgnsMaxFaces = 32 << 20

// h5FaceScanSpan is how much of CONNECTED_CELLS is decoded at a time. Even by
// construction, so a block always starts on a face's owner slot rather than
// halfway through a pair.
const h5FaceScanSpan = 1 << 21

// h5MaxSurfaceConnectivity caps the face vertices actually decoded, which are
// the drawn boundary faces alone. It bounds the wetted surface a request may
// ask for rather than the mesh it was cut from: the interior dwarfs the
// boundary on any real case, and none of it reaches the browser.
const h5MaxSurfaceConnectivity = 64 << 20

// h5PolyReadSpan is how far apart two faces' vertex runs may sit before they
// are fetched separately. Boundary faces are scattered through the face table,
// so coalescing turns their runs into a handful of reads while still skipping
// the interior spans between them.
const h5PolyReadSpan = 1 << 20

// h5LimitError names which of the ceilings above a request ran into. The
// distinction is the whole point: a mesh too large to read is a fact about the
// file and no detail step changes it, while a surface too large to draw is
// answered by a lower one. Both are the same 413, so the code is all the
// client has to tell them apart — and sending neither is what had the viewer
// answer an unreadable mesh with "try a lower step", advice that cannot work.
type h5LimitError struct {
	code    string
	message string
	params  map[string]string
}

func (e *h5LimitError) Error() string { return e.message }

func h5MeshTooLarge(faces, limit int) *h5LimitError {
	return &h5LimitError{
		code:    "meshTooLarge",
		message: fmt.Sprintf("mesh too large to extract: %d faces", faces),
		params: map[string]string{
			"faces": strconv.Itoa(faces),
			"limit": strconv.Itoa(limit),
		},
	}
}

func h5SurfaceTooLarge(triangles, faces int) *h5LimitError {
	return &h5LimitError{
		code: "surfaceTooLarge",
		message: fmt.Sprintf("surface too large to draw: %d triangles from %d boundary faces",
			triangles, faces),
		params: map[string]string{
			"triangles": strconv.Itoa(triangles),
			"faces":     strconv.Itoa(faces),
			"limit":     strconv.Itoa(h5MaxSurfaceTriangles),
		},
	}
}

func h5ConnectivityTooLarge(faces int, faceVertices int64) *h5LimitError {
	return &h5LimitError{
		code: "surfaceTooLarge",
		message: fmt.Sprintf("surface too large to extract: %d faces, %d face vertices",
			faces, faceVertices),
		params: map[string]string{
			"faces":        strconv.Itoa(faces),
			"faceVertices": strconv.FormatInt(faceVertices, 10),
			"limit":        strconv.Itoa(h5MaxSurfaceConnectivity),
		},
	}
}

// h5ScalarTooLarge is the CGNS-only refusal. Colouring a wall there means
// inverting the cell section, which is a whole-mesh read the geometry never
// pays for — so unlike the others this one is answered by dropping the
// colour-by rather than by dropping detail.
func h5ScalarTooLarge(references int64, limit int) *h5LimitError {
	return &h5LimitError{
		code: "scalarTooLarge",
		message: fmt.Sprintf("too many face references to invert for a scalar: %d",
			references),
		params: map[string]string{
			"references": strconv.FormatInt(references, 10),
			"limit":      strconv.Itoa(limit),
		},
	}
}

// h5TooLarge answers a refusal with the cause named, rather than with a bare
// status the client has to guess at.
func h5TooLarge(w http.ResponseWriter, limit *h5LimitError) (int, error) {
	return renderClientError(w, http.StatusRequestEntityTooLarge, clientError{
		Code:    limit.code,
		Message: limit.message,
		Params:  limit.params,
	})
}

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

// h5SoloFaces is the mesh size past which an extraction takes every permit and
// runs on its own. The permits count cores, but what a large mesh spends is
// memory: measured on a 27.4M-cell case at 84M faces, one extraction holds
// ~2.3GB, and a FileSystem box has 16GB shared with a running solver and a ZFS
// ARC that wants half of it. Two of those at once is how it runs out.
//
// Set where one extraction reaches roughly half a gigabyte, which the same
// measurement puts at ~27 bytes a face. Below it two still run together, which
// is what an ordinary case wants.
const h5SoloFaces = 16 << 20

// h5SurfaceWeight is how many permits one mesh takes.
func h5SurfaceWeight(faceCount int) int {
	if faceCount >= h5SoloFaces {
		return h5SurfaceSem.GetLimit()
	}
	return 1
}

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
	Stream     string     `json:"stream"`
	Vertices   int        `json:"vertices"`
	Triangles  int        `json:"triangles"`
	Faces      int        `json:"faces"`
	FacesTotal int        `json:"facesTotal"`
	Stride     int        `json:"stride"`
	Truncated  bool       `json:"truncated,omitempty"`
	Skipped    int        `json:"skipped,omitempty"`
	Bounds     [6]float64 `json:"bounds"`
	Scalar     string     `json:"scalar,omitempty"`
	Range      [2]float64 `json:"range"`
	// Unresolved counts the vertices carrying no usable reading of the scalar.
	// Every vertex unresolved draws the whole wall in the ramp's no-value grey,
	// which is indistinguishable from a broken viewer unless it is said out
	// loud — so the count travels rather than being left to be inferred.
	Unresolved int                 `json:"unresolved,omitempty"`
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
	ctx := r.Context()

	stream := strings.Trim(firstValue(query, "surface"), "/")
	if stream == "" || stream == "1" || stream == "true" {
		stream = "STREAM_00"
	}
	if _, err := f.Group(stream); err != nil {
		return http.StatusNotFound, err
	}

	offsetsDS, err := f.Dataset(stream + "/CONNECTIVITY/POLYGON_OFFSET")
	if err != nil {
		return http.StatusNotFound, fmt.Errorf("%s carries no face connectivity: %w", stream, err)
	}
	polyDS, err := f.Dataset(stream + "/CONNECTIVITY/POLYGON_TO_VERTEX")
	if err != nil {
		return http.StatusNotFound, err
	}
	connDS, err := f.Dataset(stream + "/CONNECTIVITY/CONNECTED_CELLS")
	if err != nil {
		return http.StatusNotFound, err
	}
	// Priced from the headers, before a byte of any of them is decoded: the
	// arrays below widen to int64 on the way in, so a check made after the read
	// would be reporting a mesh the box had already made room for.
	if offsetsDS.Len() > h5MaxFaces+1 {
		return h5TooLarge(w, h5MeshTooLarge(int(offsetsDS.Len()-1), h5MaxFaces))
	}

	faceCount := int(offsetsDS.Len()) - 1
	if faceCount < 1 {
		return http.StatusUnprocessableEntity, fmt.Errorf("%s has no faces", stream)
	}

	// Queueing here rather than at the door means a request whose client left
	// while it waited never starts: Acquire gives up as soon as ctx is done.
	// Everything above this is header parsing — the reads all sit below it.
	//
	// Weighted by the mesh, because the permits were sized for cores and the
	// binding resource is memory: two large extractions fit the CPU budget and
	// not the box.
	weight := h5SurfaceWeight(faceCount)
	if err := h5SurfaceSem.Acquire(ctx, weight); err != nil {
		return h5Abandoned()
	}
	defer h5SurfaceSem.Release(weight)

	offsets, err := offsetsDS.Ints()
	if err != nil {
		return http.StatusNotFound, err
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

	// Group the boundary faces by boundary so each one lands in a contiguous
	// run of the index array.
	filter := h5SurfaceFilter(firstValue(query, "boundaries"))
	byBoundary, fluid, err := h5ScanBoundaryFaces(ctx, connDS, faceCount, filter, h5FaceScanSpan)
	if err != nil {
		if ctx.Err() != nil {
			return h5Abandoned()
		}
		return http.StatusNotFound, err
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

	stride := h5SurfaceStride(trianglesTotal, firstValue(query, "limit"))
	if drawnTriangles := trianglesTotal / stride; drawnTriangles > h5MaxSurfaceTriangles {
		return h5TooLarge(w, h5SurfaceTooLarge(drawnTriangles, facesTotal))
	}

	// Settling the stride here rather than inside the extractor means the faces
	// the stride drops are never fetched: at the top step of a large geometry
	// that is most of the boundary, and all of it would otherwise be read only
	// to be stepped over.
	drawn := h5DrawnFaces(offsets, ids, byBoundary, fluid, stride, int64(polyDS.Len()))
	starts, total := h5CompactOffsets(offsets, drawn)
	if total > h5MaxSurfaceConnectivity {
		return h5TooLarge(w, h5ConnectivityTooLarge(len(drawn), total))
	}

	polyToVertex, err := h5ReadFaceVertices(ctx, polyDS, offsets, drawn, total)
	if err != nil {
		if ctx.Err() != nil {
			return h5Abandoned()
		}
		return http.StatusNotFound, err
	}
	// The runs are concatenated in face order, so the face table can be
	// restated over them in place; every face left in byBoundary is drawn.
	h5RebaseOffsets(offsets, drawn, starts, total)

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
		FluidCells:   fluid,
		X:            xs, Y: ys, Z: zs,
		IDs:        ids,
		ByBoundary: byBoundary,
		CellValues: cellValues,
		Stride:     1,
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
	// The client lays its values view over the payload whenever the header
	// names a scalar, so naming one the payload does not carry would have it
	// read the edge indices back as readings, or run off the end of the buffer
	// entirely. An empty cell variable is the case that reaches here.
	if len(surface.Values) > 0 {
		header.Scalar = scalar
	}
	header.Edges = len(surface.Edges)
	header.Boundaries = surface.Boundaries

	// Framing the response copies every array into one contiguous buffer, which
	// is worth skipping outright when there is no longer anyone to send it to.
	if ctx.Err() != nil {
		return h5Abandoned()
	}
	return h5WriteSurface(w, r, header, surface.Positions, surface.Indices, surface.Values, surface.Edges)
}

// h5SurfaceStride settles how far apart the drawn faces sit. A client that
// names a triangle budget is thinned to fit it; one that names none is asking
// for the surface as it is, and gets it whole however large — the top step
// exists to be believed, and a wall quietly drawn at half its faces reads as a
// broken viewer rather than as a setting.
func h5SurfaceStride(trianglesTotal int, limit string) int {
	if limit == "" || trianglesTotal < 1 {
		return 1
	}
	n, err := strconv.Atoi(limit)
	if err != nil || n < 1 || trianglesTotal <= n {
		return 1
	}
	return (trianglesTotal + n - 1) / n
}

// h5ScanBoundaryFaces walks the face table in blocks and keeps only what the
// surface needs: which faces sit on a boundary, and for each the cell on the
// fluid side that colours it. A boundary face is marked by a negative owner,
// -(id+1), so the whole mesh has to be looked at — but only looked at, once,
// in order, and nothing about the 98% that is interior is worth holding.
//
// That is the difference between 24 bytes per mesh face and 8 per boundary
// face. Measured on a 27.4M-cell post file whose 84,125,742 faces carry
// 1,366,195 boundary faces between them: reading CONNECTED_CELLS whole left
// 1,926MB resident, this leaves 67MB — and it is faster, because the bytes it
// does not widen are bytes it does not fault in or hand to the collector.
// span is a parameter so that a test can drive the block edge across every
// face in a fixture: the one thing this must not be able to do is lose or
// shift a face at a boundary between two reads.
func h5ScanBoundaryFaces(ctx context.Context, ds *hdf5.Dataset, faceCount int, filter map[int64]bool, span uint64) (
	map[int64][]int32, map[int64][]int32, error,
) {
	byBoundary := map[int64][]int32{}
	fluid := map[int64][]int32{}
	if span < 2 {
		span = 2
	}
	span -= span % 2

	// The face table may run past the offsets; the offsets are what say how
	// many faces there are, so the scan stops where they do.
	total := ds.Len()
	if faces := 2 * uint64(faceCount); total > faces {
		total = faces
	}

	var reader hdf5.IntsBlockReader
	for start := uint64(0); start < total; start += span {
		n := span
		if start+n > total {
			n = total - start
		}
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		block, err := reader.Range(ds, start, n)
		if err != nil {
			return nil, nil, err
		}
		base := int32(start / 2)
		for i := 0; i+1 < len(block); i += 2 {
			owner := block[i]
			if owner >= 0 {
				continue // interior face: both sides are cells
			}
			id := -owner - 1
			if filter != nil && !filter[id] {
				continue
			}
			byBoundary[id] = append(byBoundary[id], base+int32(i/2))
			// The boundary is always the owner, so the neighbour slot is the
			// fluid cell. Kept narrow: a mesh this reader will open cannot
			// have more cells than an int32 addresses.
			fluid[id] = append(fluid[id], int32(block[i+1]))
		}
	}
	return byBoundary, fluid, nil
}

// h5DrawnFaces reduces each boundary to the faces that will actually be drawn
// — the stride's survivors, minus any whose vertex run the file does not back
// — and returns them all in ascending face order. byBoundary is narrowed to
// match, so what the extractor walks and what was fetched cannot drift apart.
// fluid, when given, is narrowed in the same pass and by the same condition:
// it is indexed by position within a boundary, not by face, so a face dropped
// from one and not the other would colour every face after it with its
// neighbour's cell. Filtering both in one loop is what makes that impossible
// rather than merely unlikely.
func h5DrawnFaces(offsets []int64, ids []int64, byBoundary, fluid map[int64][]int32, stride int, polyLen int64) []int32 {
	drawn := make([]int32, 0, 1024)
	for _, id := range ids {
		faces := byBoundary[id]
		cells := fluid[id]
		kept := faces[:0]
		keptCells := cells[:0]
		for k, face := range faces {
			if stride > 1 && k%stride != 0 {
				continue
			}
			if int(face)+1 >= len(offsets) {
				continue
			}
			start, end := offsets[face], offsets[face+1]
			if start < 0 || end < start || end > polyLen {
				continue
			}
			kept = append(kept, face)
			if k < len(cells) {
				keptCells = append(keptCells, cells[k])
			}
		}
		byBoundary[id] = kept
		if fluid != nil {
			fluid[id] = keptCells
		}
		drawn = append(drawn, kept...)
	}
	sort.Slice(drawn, func(i, j int) bool { return drawn[i] < drawn[j] })
	return drawn
}

// h5CompactOffsets gives each drawn face its start in the concatenated run,
// and the total length of them all.
func h5CompactOffsets(offsets []int64, drawn []int32) ([]int64, int64) {
	starts := make([]int64, len(drawn))
	total := int64(0)
	for i, face := range drawn {
		starts[i] = total
		// h5DrawnFaces has already dropped the inverted runs; clamping here
		// keeps the pricing total whatever it is handed, since a negative one
		// would be a capacity the allocator panics on.
		if n := offsets[face+1] - offsets[face]; n > 0 {
			total += n
		}
	}
	return starts, total
}

// h5ReadFaceVertices fetches the vertex runs of the drawn faces and returns
// them concatenated in face order.
func h5ReadFaceVertices(ctx context.Context, ds *hdf5.Dataset, offsets []int64, drawn []int32, total int64) ([]int64, error) {
	out := make([]int64, 0, total)
	var reader hdf5.IntsBlockReader
	for i := 0; i < len(drawn); {
		start := offsets[drawn[i]]
		end := offsets[drawn[i]+1]
		// A well-formed table runs ascending, but nothing read off disk is
		// owed that, so the batch closes on anything that would reach outside
		// the block it is about to fetch.
		j := i + 1
		for j < len(drawn) {
			s, e := offsets[drawn[j]], offsets[drawn[j]+1]
			if s < start || e-start > h5PolyReadSpan {
				break
			}
			if e > end {
				end = e
			}
			j++
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		block, err := reader.Range(ds, uint64(start), uint64(end-start))
		if err != nil {
			return nil, err
		}
		for k := i; k < j; k++ {
			s, e := offsets[drawn[k]], offsets[drawn[k]+1]
			if e <= s {
				continue
			}
			out = append(out, block[s-start:e-start]...)
		}
		i = j
	}
	return out, nil
}

// h5RebaseOffsets restates the face table over the concatenated runs. Only the
// drawn faces are given meaningful bounds; nothing else is read from it.
func h5RebaseOffsets(offsets []int64, drawn []int32, starts []int64, total int64) {
	for i, face := range drawn {
		end := total
		if i+1 < len(drawn) {
			end = starts[i+1]
		}
		offsets[face] = starts[i]
		offsets[face+1] = end
	}
}

type h5SurfaceInput struct {
	Offsets      []int64
	PolyToVertex []int64
	// FluidCells runs parallel to ByBoundary: the cell whose value colours
	// each boundary face, in the same order as the faces themselves. Holding
	// it per boundary rather than per mesh face is what lets the face table be
	// streamed instead of resident.
	FluidCells map[int64][]int32
	// Coordinates are stored float32 and sent float32, so they are never
	// widened: on a 29M-vertex mesh the round trip through float64 is 348MB
	// held for precision the wire discards.
	X, Y, Z    []float32
	IDs        []int64
	ByBoundary map[int64][]int32
	CellValues []float64
	Stride     int
	WithEdges  bool
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

	parts, err := h5CutBoundaries(ctx, in, nverts)
	if err != nil {
		return h5SurfaceResult{}, err
	}

	out := h5SurfaceResult{}
	// Vertices are shared between faces, so the mesh is emitted indexed over a
	// compacted vertex list: only the vertices the boundary actually touches
	// are sent, which on the measured cases is under a third of the mesh.
	remap := make([]int32, nverts)
	for i := range remap {
		remap[i] = -1
	}
	// Accumulators for the per-vertex scalar average.
	var sums []float64
	var counts []int32

	bounds := [6]float64{
		math.Inf(1), math.Inf(1), math.Inf(1),
		math.Inf(-1), math.Inf(-1), math.Inf(-1),
	}
	skipped := 0
	global := make([]int32, 0, 1024)

	// Stitched in the order the boundaries were asked for, whatever order they
	// were cut in: a boundary owns a contiguous run of the index array, and the
	// compacted vertex list is shared across all of them.
	for i := range parts {
		p := &parts[i]
		entry := h5SurfaceBoundary{
			ID:          p.id,
			IndexOffset: len(out.Indices),
			EdgeOffset:  len(out.Edges),
			Faces:       p.faces,
			Triangles:   p.triangles,
		}
		skipped += p.skipped

		global = global[:0]
		for l, v := range p.verts {
			g := remap[v]
			if g < 0 {
				g = int32(len(out.Positions) / 3)
				remap[v] = g
				out.Positions = append(out.Positions,
					p.coords[3*l], p.coords[3*l+1], p.coords[3*l+2])
				sums = append(sums, 0)
				counts = append(counts, 0)
			}
			global = append(global, g)
		}
		for _, li := range p.indices {
			out.Indices = append(out.Indices, uint32(global[li]))
		}
		for _, le := range p.edges {
			out.Edges = append(out.Edges, uint32(global[le]))
		}
		for l := range p.verts {
			g := global[l]
			sums[g] += p.sums[l]
			counts[g] += p.counts[l]
		}
		if len(p.verts) > 0 {
			for a := 0; a < 3; a++ {
				bounds[a] = math.Min(bounds[a], p.bounds[a])
				bounds[a+3] = math.Max(bounds[a+3], p.bounds[a+3])
			}
		}

		entry.IndexCount = len(out.Indices) - entry.IndexOffset
		entry.EdgeCount = len(out.Edges) - entry.EdgeOffset
		// Handed back as it is stitched: the parts together are the size of the
		// surface again, and holding them all to the end would double it.
		*p = h5BoundaryPart{}
		if entry.IndexCount == 0 {
			continue
		}
		out.Boundaries = append(out.Boundaries, entry)
	}

	if len(out.Positions) == 0 {
		bounds = [6]float64{}
	}

	valueRange := [2]float64{math.Inf(1), math.Inf(-1)}
	unresolved := 0
	if in.CellValues != nil {
		out.Values = make([]float32, len(counts))
		for i, c := range counts {
			if c == 0 {
				// Touched only by faces with no usable value. NaN travels to
				// the client as a hole in the field rather than as a reading.
				out.Values[i] = float32(math.NaN())
				unresolved++
				continue
			}
			v := sums[i] / float64(c)
			// The wire carries float32, so a field beyond its range would
			// arrive as ±Inf — which the ramp draws as the same grey it uses
			// for no reading at all, while the legend still showed the finite
			// span this average came from. Better to say the vertex has no
			// reading than to hand the client an infinity dressed as one.
			f := float32(v)
			if math.IsInf(float64(f), 0) {
				out.Values[i] = float32(math.NaN())
				unresolved++
				continue
			}
			out.Values[i] = f
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
		Vertices:   len(out.Positions) / 3,
		Triangles:  triangles,
		Faces:      faces,
		Skipped:    skipped,
		Bounds:     bounds,
		Range:      valueRange,
		Unresolved: unresolved,
	}
	return out, nil
}

// h5BoundaryPart is one boundary cut on its own, indexed over a vertex list of
// its own. Nothing in it names another boundary, which is what lets them be
// cut at the same time and stitched afterwards.
type h5BoundaryPart struct {
	id        int64
	verts     []int32
	coords    []float32
	indices   []uint32
	edges     []uint32
	sums      []float64
	counts    []int32
	bounds    [6]float64
	faces     int
	triangles int
	skipped   int
}

// h5BoundaryScratch is one worker's reusable working set. slots is the size of
// the whole mesh, so it is built once per worker and handed from boundary to
// boundary rather than allocated per boundary.
type h5BoundaryScratch struct {
	slots []int32
	poly  [][3]float64
	tri   []uint32
	corn  []uint32
	seen  map[uint64]struct{}
}

func h5NewScratch(nverts int, withEdges bool) *h5BoundaryScratch {
	s := &h5BoundaryScratch{
		slots: make([]int32, nverts),
		poly:  make([][3]float64, 0, 16),
		tri:   make([]uint32, 0, 48),
		corn:  make([]uint32, 0, 16),
	}
	for i := range s.slots {
		s.slots[i] = -1
	}
	if withEdges {
		s.seen = make(map[uint64]struct{}, 1024)
	}
	return s
}

// h5SurfaceScratchBudget caps what the workers' slot tables may cost between
// them. Each worker holds one int32 per vertex of the whole mesh, which is
// 116MB on a 29M-vertex case — on a large mesh the parallelism is bought with
// memory the box has not got. Losing it there costs less than it looks: one
// wall is routinely most of the wetted surface, measured at 73.8% on that same
// case, so the workers behind the largest one finish early and wait anyway.
const h5SurfaceScratchBudget = 192 << 20

// h5SurfaceWorkers is how many boundaries are cut at once. Two cores are held
// back: a FileSystem box runs the solver beside this, and the response still
// has to be packed once the geometry is done. More workers than boundaries
// would only sit idle.
func h5SurfaceWorkers(boundaries, nverts int) int {
	n := runtime.GOMAXPROCS(0) - 2
	if n > boundaries {
		n = boundaries
	}
	if nverts > 0 {
		if afford := h5SurfaceScratchBudget / (4 * nverts); n > afford {
			n = afford
		}
	}
	if n < 1 {
		n = 1
	}
	return n
}

// h5CutBoundaries cuts every boundary, concurrently when there is more than
// one to cut, and returns them in the order they were asked for.
func h5CutBoundaries(ctx context.Context, in h5SurfaceInput, nverts int) ([]h5BoundaryPart, error) {
	parts := make([]h5BoundaryPart, len(in.IDs))
	workers := h5SurfaceWorkers(len(in.IDs), nverts)

	if workers <= 1 {
		s := h5NewScratch(nverts, in.WithEdges)
		for i, id := range in.IDs {
			p, err := h5CutBoundary(ctx, in, id, s, nverts)
			if err != nil {
				return nil, err
			}
			parts[i] = p
		}
		return parts, nil
	}

	// Largest first. One wall is routinely most of the wetted surface, and
	// reaching it last would leave every other worker waiting on it alone.
	order := make([]int, len(in.IDs))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return len(in.ByBoundary[in.IDs[order[a]]]) >
			len(in.ByBoundary[in.IDs[order[b]]])
	})

	jobs := make(chan int)
	errs := make([]error, workers)
	var failed atomic.Bool
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			var s *h5BoundaryScratch
			for i := range jobs {
				// Drained rather than returned from: a worker that walked away
				// from the channel would block the one still feeding it.
				if failed.Load() {
					continue
				}
				if s == nil {
					s = h5NewScratch(nverts, in.WithEdges)
				}
				p, err := h5CutBoundary(ctx, in, in.IDs[i], s, nverts)
				if err != nil {
					errs[w] = err
					failed.Store(true)
					continue
				}
				parts[i] = p
			}
		}(w)
	}
	for _, i := range order {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return parts, nil
}

// h5CutBoundary walks one boundary's faces and triangulates them against a
// vertex list local to it.
//
// It gives up when ctx is cancelled: this is the long pole of a surface
// request — hundreds of thousands of faces, each triangulated — and running it
// out for a client that has already navigated away is the difference between
// an idle box and a saturated one.
func h5CutBoundary(ctx context.Context, in h5SurfaceInput, id int64, s *h5BoundaryScratch, nverts int) (h5BoundaryPart, error) {
	part := h5BoundaryPart{
		id: id,
		bounds: [6]float64{
			math.Inf(1), math.Inf(1), math.Inf(1),
			math.Inf(-1), math.Inf(-1), math.Inf(-1),
		},
	}
	if s.seen != nil {
		clear(s.seen)
	}

	walked := 0
	for k, face := range in.ByBoundary[id] {
		// Polled rather than tested per face: the check is cheap, but so is
		// a strided iteration, and this loop runs into the millions.
		walked++
		if walked%4096 == 0 && ctx.Err() != nil {
			return h5BoundaryPart{}, ctx.Err()
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
		s.poly = s.poly[:0]
		s.corn = s.corn[:0]
		ok := true
		for i := start; i < end; i++ {
			v := in.PolyToVertex[i]
			if v < 0 || v >= int64(nverts) {
				ok = false
				break
			}
			x, y, z := in.X[v], in.Y[v], in.Z[v]
			if !h5FiniteCoord(x) || !h5FiniteCoord(y) || !h5FiniteCoord(z) {
				ok = false
				break
			}
			s.poly = append(s.poly, [3]float64{float64(x), float64(y), float64(z)})
			s.corn = append(s.corn, uint32(v))
		}
		if !ok {
			part.skipped++
			continue
		}

		s.tri = earClip(s.poly, s.tri[:0])
		if len(s.tri) == 0 {
			part.skipped++
			continue
		}

		// Map this face's corners into the boundary's own vertex list.
		for i, v := range s.corn {
			if s.slots[v] >= 0 {
				continue
			}
			s.slots[v] = int32(len(part.verts))
			part.verts = append(part.verts, int32(v))
			part.coords = append(part.coords,
				float32(s.poly[i][0]), float32(s.poly[i][1]), float32(s.poly[i][2]))
			part.sums = append(part.sums, 0)
			part.counts = append(part.counts, 0)

			part.bounds[0] = math.Min(part.bounds[0], s.poly[i][0])
			part.bounds[1] = math.Min(part.bounds[1], s.poly[i][1])
			part.bounds[2] = math.Min(part.bounds[2], s.poly[i][2])
			part.bounds[3] = math.Max(part.bounds[3], s.poly[i][0])
			part.bounds[4] = math.Max(part.bounds[4], s.poly[i][1])
			part.bounds[5] = math.Max(part.bounds[5], s.poly[i][2])
		}

		for _, corner := range s.tri {
			part.indices = append(part.indices, uint32(s.slots[s.corn[corner]]))
		}

		// The edges are the polygon's perimeter, never the triangulation:
		// an ear-clip diagonal is an artifact of drawing, not of the mesh.
		// Neighbouring faces of one boundary share their common edge, so
		// each is deduplicated within the boundary's run.
		if in.WithEdges {
			for i := range s.corn {
				a := uint32(s.slots[s.corn[i]])
				b := uint32(s.slots[s.corn[(i+1)%len(s.corn)]])
				if a == b {
					continue
				}
				key := uint64(min(a, b))<<32 | uint64(max(a, b))
				if _, dup := s.seen[key]; dup {
					continue
				}
				s.seen[key] = struct{}{}
				part.edges = append(part.edges, a, b)
			}
		}

		if in.CellValues != nil {
			// The cell on the fluid side, collected when this face was found:
			// the boundary always takes the owner slot, so the neighbour is
			// the cell whose value the wall is drawn with.
			cells := in.FluidCells[id]
			cell := -1
			if k < len(cells) {
				cell = int(cells[k])
			}
			if cell >= 0 && cell < len(in.CellValues) {
				if v := in.CellValues[cell]; h5Finite(v) {
					for _, g := range s.corn {
						ci := s.slots[g]
						part.sums[ci] += v
						part.counts[ci]++
					}
				}
			}
		}

		part.faces++
		part.triangles += len(s.tri) / 3
	}

	// Released for the next boundary this worker takes, which is cheaper than
	// clearing a table the size of the mesh between every one.
	for _, v := range part.verts {
		s.slots[v] = -1
	}
	return part, nil
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

func h5VertexCoords(f *hdf5.File, stream string) (xs, ys, zs []float32, err error) {
	var got [3][]float32
	for i, axis := range []string{"X", "Y", "Z"} {
		ds, err := f.Dataset(stream + "/VERTEX_COORDINATES/" + axis)
		if err != nil {
			return nil, nil, nil, err
		}
		if got[i], err = ds.Float32s(); err != nil {
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
func h5WriteSurface(w http.ResponseWriter, r *http.Request, header h5SurfaceHeader, positions []float32, indices []uint32, values []float32, edges []uint32) (int, error) {
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

	// The surface is the largest thing this server sends and playback is bound
	// by the wire, not by the CPU that packed it. Index arrays especially are
	// mostly high zero bytes and give most of the saving back. Speed over ratio
	// deliberately: several frames may be in flight at once, and the point is
	// to spend less wall-clock than the bytes would have cost.
	w.Header().Add("Vary", "Accept-Encoding")

	switch h5Encoding(r) {
	case "zstd":
		// No Content-Length: the packed size is not known until the stream is
		// closed, and buffering it twice to learn it would undo the saving in
		// memory on a response this size.
		w.Header().Set("Content-Encoding", "zstd")
		enc := h5ZstdWriters.Get().(*zstd.Encoder)
		defer h5ZstdWriters.Put(enc)
		enc.Reset(w)
		if _, err := enc.Write(buf); err != nil {
			enc.Close()
			return 0, err
		}
		if err := enc.Close(); err != nil {
			return 0, err
		}
	case "gzip":
		w.Header().Set("Content-Encoding", "gzip")
		zw, err := gzip.NewWriterLevel(w, gzip.BestSpeed)
		if err != nil {
			return http.StatusInternalServerError, err
		}
		if _, err := zw.Write(buf); err != nil {
			zw.Close()
			return 0, err
		}
		if err := zw.Close(); err != nil {
			return 0, err
		}
	default:
		w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
		if _, err := w.Write(buf); err != nil {
			return 0, err
		}
	}
	return 0, nil
}

// h5ZstdWriters keeps the encoders alive between requests: one carries a
// window and a match table that cost more to build than packing a frame does.
// Two threads measured 1.6x one on this payload and four barely beat two, so
// it stops there — the extraction the next request is running wants the rest.
var h5ZstdWriters = sync.Pool{
	New: func() any {
		enc, _ := zstd.NewWriter(nil,
			zstd.WithEncoderLevel(zstd.SpeedDefault),
			zstd.WithEncoderConcurrency(2))
		return enc
	},
}

// h5Encoding picks the encoding for the surface from the ones the client named,
// preferring zstd: at its fastest setting it packs this payload in a fraction
// of the time gzip takes and still lands smaller. A client that names neither
// is sent the bytes as they are.
func h5Encoding(r *http.Request) string {
	zstdOK, gzipOK := false, false
	for _, enc := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(enc, ";")
		name = strings.TrimSpace(name)
		if name != "zstd" && name != "gzip" {
			continue
		}
		// "gzip;q=0" is a refusal, and answering it with gzip anyway would
		// send a body the client has said it cannot read.
		if v := strings.TrimSpace(params); strings.HasPrefix(v, "q=") {
			if q, err := strconv.ParseFloat(v[2:], 64); err == nil && q == 0 {
				continue
			}
		}
		if name == "zstd" {
			zstdOK = true
		} else {
			gzipOK = true
		}
	}
	switch {
	case zstdOK:
		return "zstd"
	case gzipOK:
		return "gzip"
	}
	return ""
}
