package fbhttp

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/rforced/filebrowser/v2/hdf5"
)

// The boundary surface of a CGNS file, cut to the same payload the native
// format produces so that one viewer draws both.
//
// The two formats disagree about where the wetted surface is written down. A
// post*.h5 hides it in the face table — a boundary face is one whose owner
// slot holds -(id+1) instead of a cell — so finding it means scanning every
// face in the mesh. CGNS states it outright: each BC_t node lists the elements
// on that patch, by name, in a PointList. Nothing has to be scanned, and the
// names are the ones from boundary.in rather than ids to be looked up
// elsewhere.
//
// What is harder here is colouring. The native format's face table names the
// cell on the fluid side, which is the value the wall is drawn with; CGNS
// records the relationship the other way round, cells pointing at their faces,
// so the map has to be inverted. That is a pass over the cell section, which
// is why it is only done when a scalar is actually asked for.
//
// Everything past the reading is shared with the native path: the same face
// pricing, the same stride, the same ear clipper, the same binary framing.

// cgnsSection is one Elements_t node: its type and the span of the zone's
// global element numbering it occupies.
type cgnsSection struct {
	path  string
	kind  int64
	first int64
	last  int64
}

func (s cgnsSection) count() int64 { return s.last - s.first + 1 }

// cgnsSurfaceResponse extracts the boundary surface of one zone.
func cgnsSurfaceResponse(w http.ResponseWriter, r *http.Request, f *hdf5.File, query map[string][]string) (int, error) {
	ctx := r.Context()
	if err := h5SurfaceSem.Acquire(ctx, 1); err != nil {
		return h5Abandoned()
	}
	defer h5SurfaceSem.Release(1)

	stream := strings.Trim(firstValue(query, "surface"), "/")
	if stream == "" || stream == "1" || stream == "true" {
		stream = "STREAM_00"
	}
	zone, err := cgnsZonePath(f, stream)
	if err != nil {
		return http.StatusNotFound, err
	}
	// Only an unstructured zone divides itself into element sections; a
	// structured one is an i-j-k block whose faces are implied by the indexing.
	// CONVERGE writes the former, and saying so is better than reading a
	// structured zone's coordinates and finding nothing to draw them with.
	if kind := cgnsText(f, zone+"/ZoneType"); kind != "" && kind != "Unstructured" {
		return http.StatusUnprocessableEntity,
			fmt.Errorf("%s is a %s zone, which carries no element sections", stream, kind)
	}

	sections, err := cgnsElementSections(f, zone)
	if err != nil {
		return http.StatusNotFound, err
	}
	faces, err := cgnsBuildFaces(f, sections)
	if err != nil {
		return http.StatusNotFound, fmt.Errorf("%s: %w", zone, err)
	}
	offsets := faces.offsets
	if ctx.Err() != nil {
		return h5Abandoned()
	}

	xs, ys, zs, err := cgnsVertexCoords(f, zone)
	if err != nil {
		return http.StatusNotFound, err
	}
	if ctx.Err() != nil {
		return h5Abandoned()
	}

	filter := h5SurfaceFilter(firstValue(query, "boundaries"))
	ids, names, byBoundary, err := cgnsBoundaryFaces(f, zone, faces, filter)
	if err != nil {
		return http.StatusUnprocessableEntity, err
	}
	if len(byBoundary) == 0 {
		return http.StatusUnprocessableEntity,
			fmt.Errorf("%s has no boundary faces", stream)
	}

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
		return http.StatusRequestEntityTooLarge,
			fmt.Errorf("surface too large to draw: %d triangles from %d boundary faces",
				drawnTriangles, facesTotal)
	}

	drawn := h5DrawnFaces(offsets, ids, byBoundary, stride, faces.polyLen)
	starts, total := h5CompactOffsets(offsets, drawn)
	if total > h5MaxSurfaceConnectivity {
		return http.StatusRequestEntityTooLarge,
			fmt.Errorf("surface too large to extract: %d faces, %d face vertices",
				len(drawn), total)
	}

	polyToVertex, err := faces.readVertices(ctx, drawn, total)
	if err != nil {
		if ctx.Err() != nil {
			return h5Abandoned()
		}
		return http.StatusNotFound, err
	}
	// CGNS numbers vertices from one. A zero would be a vertex the file does
	// not have, and rebasing it to -1 is what has the extractor drop the face
	// rather than read the coordinate before the first.
	for i := range polyToVertex {
		polyToVertex[i]--
	}
	h5RebaseOffsets(offsets, drawn, starts, total)

	// Inverting the cell section is the one expensive thing CGNS asks for that
	// the native format does not, so it is paid only when the wall is being
	// coloured. Geometry alone never reads the cells at all.
	scalar := firstValue(query, "scalar")
	var cellValues []float64
	var faceOwners []int64
	if scalar != "" {
		if cellValues, err = cgnsCellValues(f, zone, scalar); err != nil {
			return http.StatusNotFound, err
		}
		if faceOwners, err = cgnsFaceOwners(ctx, f, faces, sections); err != nil {
			if ctx.Err() != nil {
				return h5Abandoned()
			}
			return http.StatusUnprocessableEntity, err
		}
	}

	edges := firstValue(query, "edges")
	surface, err := h5ExtractSurface(ctx, h5SurfaceInput{
		Offsets:      offsets,
		PolyToVertex: polyToVertex,
		Connected:    faceOwners,
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

	for i := range surface.Boundaries {
		surface.Boundaries[i].Name = names[surface.Boundaries[i].ID]
	}

	header := surface.Header
	header.Stream = stream
	header.FacesTotal = facesTotal
	header.Stride = stride
	header.Truncated = stride > 1
	if len(surface.Values) > 0 {
		header.Scalar = scalar
	}
	header.Edges = len(surface.Edges)
	header.Boundaries = surface.Boundaries

	if ctx.Err() != nil {
		return h5Abandoned()
	}
	return h5WriteSurface(w, r, header, surface.Positions, surface.Indices, surface.Values, surface.Edges)
}

// cgnsZonePath resolves what the client called a stream to the zone holding
// the mesh. A base names its zone, and a zone names itself.
func cgnsZonePath(f *hdf5.File, stream string) (string, error) {
	_, label := cgnsLabel(f, stream)
	switch label {
	case "Zone_t":
		return stream, nil
	case "CGNSBase_t":
		zones := cgnsChildrenLabelled(f, stream, "Zone_t")
		if len(zones) == 0 {
			return "", fmt.Errorf("%s holds no zone", stream)
		}
		return stream + "/" + zones[0], nil
	}
	return "", fmt.Errorf("no CGNS zone at %s", stream)
}

// cgnsElementSections lists the zone's Elements_t nodes, in the order of the
// global element numbering they divide up.
func cgnsElementSections(f *hdf5.File, zone string) ([]cgnsSection, error) {
	g, err := f.Group(zone)
	if err != nil {
		return nil, err
	}
	links, err := g.Children()
	if err != nil {
		return nil, err
	}

	var out []cgnsSection
	for _, l := range links {
		if l.Kind != hdf5.KindGroup {
			continue
		}
		path := zone + "/" + l.Name
		if _, label := cgnsLabel(f, path); label != "Elements_t" {
			continue
		}
		kind, first, last, err := cgnsElementSection(f, path)
		if err != nil || last < first || first < 1 {
			continue
		}
		out = append(out, cgnsSection{path: path, kind: kind, first: first, last: last})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].first < out[j].first })
	return out, nil
}

// cgnsFaceWidth is the vertex count of a fixed-size surface element, or zero
// for anything that is not one. NGON_n is the variable-width case and carries
// an offset table instead.
func cgnsFaceWidth(kind int64) int64 {
	switch kind {
	case cgnsTri3:
		return 3
	case cgnsQuad4:
		return 4
	}
	return 0
}

// cgnsFaces is every drawable face in a zone as one table, whatever the file
// splits them across.
//
// A post file keeps all of them in a single NGON_n section, but a surface file
// need not: the boundary conditions in one address a triangle section and a
// polygon section by turns, through the numbering the zone shares between them.
// Merging first means the rest of the extraction — pricing, striding, reading
// — sees one face table and one numbering, as the native format has.
//
// The offsets are cumulative across sections, so a face's length is right
// wherever it came from, and each section remembers where its own connectivity
// starts so a read can be turned back into a local one.
type cgnsFaces struct {
	sections []cgnsFaceSection
	offsets  []int64
	count    int64
	// polyLen is the length of the concatenated connectivity the offsets
	// address, which is what bounds a face's vertex run.
	polyLen int64
}

type cgnsFaceSection struct {
	sec   cgnsSection
	conn  *hdf5.Dataset
	first int64 // this section's first face, in the merged numbering
	count int64
	base  int64 // where its connectivity starts in the merged space
	// origin is the section's own first offset, which the standard puts at
	// zero and this does not assume.
	origin int64
}

// cgnsBuildFaces merges the zone's face sections into one table.
func cgnsBuildFaces(f *hdf5.File, sections []cgnsSection) (*cgnsFaces, error) {
	faces := &cgnsFaces{}
	for _, s := range sections {
		if s.kind != cgnsNGon && cgnsFaceWidth(s.kind) == 0 {
			continue
		}
		offsets, conn, err := cgnsSectionOffsets(f, s)
		if err != nil {
			return nil, err
		}
		n := s.count()
		origin := offsets[0]
		length := offsets[n] - origin
		if length < 0 || length > int64(conn.Len()) {
			return nil, fmt.Errorf("%s offsets run past its connectivity", s.path)
		}
		if faces.count+n > h5MaxFaces {
			return nil, fmt.Errorf("mesh too large to extract: %d faces", faces.count+n)
		}

		section := cgnsFaceSection{
			sec: s, conn: conn, first: faces.count, count: n,
			base: faces.polyLen, origin: origin,
		}
		for i := int64(0); i < n; i++ {
			faces.offsets = append(faces.offsets, offsets[i]-origin+section.base)
		}
		faces.sections = append(faces.sections, section)
		faces.count += n
		faces.polyLen += length
	}
	if faces.count == 0 {
		return nil, fmt.Errorf("no polygon or triangle section")
	}
	faces.offsets = append(faces.offsets, faces.polyLen)
	return faces, nil
}

// index maps a global element id to a face in the merged table, reporting
// false for an id that names something else — a cell, most often.
func (fs *cgnsFaces) index(element int64) (int32, bool) {
	for _, s := range fs.sections {
		if element >= s.sec.first && element <= s.sec.last {
			return int32(s.first + element - s.sec.first), true
		}
	}
	return 0, false
}

// readVertices fetches the vertex runs of the drawn faces, section by section.
//
// The merged numbering runs in section order and the drawn list is sorted, so
// each section's faces are one contiguous stretch of it. That is what lets the
// native format's coalescing reader do the actual fetching: it is handed one
// section at a time, with the offsets rebased onto that section's own
// connectivity.
func (fs *cgnsFaces) readVertices(ctx context.Context, drawn []int32, total int64) ([]int64, error) {
	if len(fs.sections) == 1 && fs.sections[0].origin == 0 {
		// One section is the common case — every post file — and its offsets
		// are already the merged ones, so nothing has to be rebased or copied.
		return h5ReadFaceVertices(ctx, fs.sections[0].conn, fs.offsets, drawn, total)
	}

	out := make([]int64, 0, total)
	at := 0
	for _, s := range fs.sections {
		start := at
		for at < len(drawn) && int64(drawn[at]) < s.first+s.count {
			at++
		}
		if at == start {
			continue
		}

		local := make([]int32, at-start)
		subtotal := int64(0)
		for i, face := range drawn[start:at] {
			local[i] = face - int32(s.first)
			subtotal += fs.offsets[face+1] - fs.offsets[face]
		}
		offsets := make([]int64, s.count+1)
		for i := int64(0); i <= s.count; i++ {
			offsets[i] = fs.offsets[s.first+i] - s.base + s.origin
		}

		run, err := h5ReadFaceVertices(ctx, s.conn, offsets, local, subtotal)
		if err != nil {
			return nil, err
		}
		out = append(out, run...)
	}
	return out, nil
}

// cgnsSectionOffsets reads one section's offset table and opens its
// connectivity.
//
// The offsets are what make a polygon section readable a face at a time, and
// CGNS has only carried them since 4.0 — before that a section packed the
// vertex count in front of each face, which cannot be indexed without reading
// the whole thing. A file written that way is refused rather than misread.
// Fixed-width sections need no table at all: every element is the same size.
func cgnsSectionOffsets(f *hdf5.File, sec cgnsSection) ([]int64, *hdf5.Dataset, error) {
	conn, err := cgnsPayload(f, sec.path+"/ElementConnectivity")
	if err != nil {
		return nil, nil, fmt.Errorf("%s has no connectivity: %w", sec.path, err)
	}

	count := sec.count()
	if width := cgnsFaceWidth(sec.kind); width > 0 {
		offsets := make([]int64, count+1)
		for i := range offsets {
			offsets[i] = int64(i) * width
		}
		return offsets, conn, nil
	}

	ds, err := cgnsPayload(f, sec.path+"/ElementStartOffset")
	if err != nil {
		return nil, nil, fmt.Errorf("%s has no element offsets, which CGNS below 4.0 did not write: %w",
			sec.path, err)
	}
	offsets, err := ds.Ints()
	if err != nil {
		return nil, nil, err
	}
	if int64(len(offsets)) != count+1 {
		return nil, nil, fmt.Errorf("%s states %d faces but offsets %d",
			sec.path, count, len(offsets)-1)
	}
	return offsets, conn, nil
}

// cgnsVertexCoords reads the zone's grid coordinates.
func cgnsVertexCoords(f *hdf5.File, zone string) (xs, ys, zs []float64, err error) {
	grids := cgnsChildrenLabelled(f, zone, "GridCoordinates_t")
	if len(grids) == 0 {
		return nil, nil, nil, fmt.Errorf("%s has no grid coordinates", zone)
	}
	base := zone + "/" + grids[0]

	var got [3][]float64
	for i, axis := range []string{"CoordinateX", "CoordinateY", "CoordinateZ"} {
		ds, err := cgnsPayload(f, base+"/"+axis)
		if err != nil {
			return nil, nil, nil, err
		}
		if got[i], err = ds.Floats(); err != nil {
			return nil, nil, nil, err
		}
	}
	return got[0], got[1], got[2], nil
}

// cgnsBoundaryFaces turns the zone's boundary conditions into the face lists
// the extractor draws, keyed by the boundary id CONVERGE knows them by.
//
// Element ids are global to the zone — the faces are numbered first and the
// cells after them — so a patch's ids are rebased onto the face section, and
// anything landing outside it is dropped rather than drawn as some other
// element.
func cgnsBoundaryFaces(f *hdf5.File, zone string, faces *cgnsFaces, filter map[int64]bool) (
	[]int64, map[int64]string, map[int64][]int32, error,
) {
	zoneBC := cgnsChildrenLabelled(f, zone, "ZoneBC_t")
	if len(zoneBC) == 0 {
		return nil, nil, nil, fmt.Errorf("%s has no boundary conditions", zone)
	}
	path := zone + "/" + zoneBC[0]

	names := map[int64]string{}
	byBoundary := map[int64][]int32{}
	var ids []int64

	for i, name := range cgnsChildrenLabelled(f, path, "BC_t") {
		node := path + "/" + name
		// The id CONVERGE knows the boundary by is written under the patch's
		// global data. Without one the patch still draws, numbered in order,
		// so a file that omits it loses the cross-reference and nothing else.
		id := int64(i + 1)
		if v, err := cgnsInts(f, node+"/GLOBAL_DATA/DirichletData/BOUNDARY_ID"); err == nil && len(v) > 0 {
			id = v[0]
		}
		if filter != nil && !filter[id] {
			continue
		}
		// What a patch's list means depends on where it is located: element ids
		// at a face centre, vertex ids at a vertex. Reading one as the other
		// would draw a patch made of unrelated faces, so anything that is not
		// face-centred is left out. CONVERGE writes GridLocation on every patch;
		// a file that omits it is taken at the reading that makes its PointList
		// a list of faces, since that is the only one this can draw.
		if at := cgnsText(f, node+"/GridLocation"); at != "" && at != "FaceCenter" {
			continue
		}

		elems := cgnsPatchElements(f, node, faces.count)
		patch := make([]int32, 0, len(elems))
		for _, e := range elems {
			// A patch can address any section the zone numbers, and a vertex-
			// located one addresses no face at all; both are dropped here rather
			// than drawn as whatever element sits at that index.
			if idx, ok := faces.index(e); ok {
				patch = append(patch, idx)
			}
		}
		if len(patch) == 0 {
			continue
		}
		// Ascending, so that the runs the extractor asks for are fetched in one
		// sweep of the file rather than seeking backwards between them.
		sort.Slice(patch, func(a, b int) bool { return patch[a] < patch[b] })

		if _, seen := byBoundary[id]; !seen {
			ids = append(ids, id)
			names[id] = name
		}
		byBoundary[id] = append(byBoundary[id], patch...)
	}

	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, names, byBoundary, nil
}

// cgnsPatchElements reads the elements one boundary condition covers, in
// either of the two forms CGNS allows: a list of ids, or a range of them.
//
// Both are capped at the number of faces in the zone. A patch cannot cover
// more than the whole mesh, and the two forms fail differently without the
// cap: a list is a dataset and would be read at whatever length it claims,
// while a range is two numbers that could name an interval no machine can
// hold. Counting up to the cap rather than subtracting the ends keeps a
// corrupt pair from wrapping into a length that looks reasonable.
func cgnsPatchElements(f *hdf5.File, node string, faces int64) []int64 {
	if ds, err := cgnsPayload(f, node+"/PointList"); err == nil {
		if ds.Len() > uint64(faces) {
			return nil
		}
		if v, err := ds.Ints(); err == nil {
			return v
		}
	}

	v, err := cgnsInts(f, node+"/PointRange")
	if err != nil || len(v) < 2 {
		return nil
	}
	first, last := v[0], v[1]
	if first < 1 {
		first = 1
	}
	if last < first {
		return nil
	}
	out := make([]int64, 0, 64)
	for e := first; e <= last && int64(len(out)) < faces; e++ {
		out = append(out, e)
	}
	return out
}

// cgnsCellValues reads one field off the zone's flow solution.
func cgnsCellValues(f *hdf5.File, zone, name string) ([]float64, error) {
	if strings.ContainsAny(name, "/") {
		return nil, fmt.Errorf("invalid variable name %q", name)
	}
	for _, sol := range cgnsChildrenLabelled(f, zone, "FlowSolution_t") {
		ds, err := cgnsPayload(f, zone+"/"+sol+"/"+name)
		if err != nil {
			continue
		}
		return ds.Floats()
	}
	return nil, fmt.Errorf("no cell variable %s in %s", name, zone)
}

// cgnsFaceOwners inverts the cell section into the face table the extractor
// expects: for each face, the cell on its fluid side.
//
// It is laid out the way a post*.h5 stores it — two slots per face, the cell
// in the second — because that is what h5ExtractSurface reads, and matching
// the layout is what lets one extractor serve both formats. The first slot,
// which the native format uses to mark the boundary, is left alone: here the
// boundary conditions have already said which faces are theirs.
func cgnsFaceOwners(ctx context.Context, f *hdf5.File, faces *cgnsFaces, sections []cgnsSection) ([]int64, error) {
	var cell cgnsSection
	for _, s := range sections {
		if s.kind == cgnsNFace {
			cell = s
			break
		}
	}
	if cell.kind != cgnsNFace {
		return nil, fmt.Errorf("no cell section to take values from")
	}

	offsets, err := cgnsInts(f, cell.path+"/ElementStartOffset")
	if err != nil {
		return nil, fmt.Errorf("%s has no element offsets: %w", cell.path, err)
	}
	if int64(len(offsets)) != cell.count()+1 {
		return nil, fmt.Errorf("%s states %d cells but offsets %d",
			cell.path, cell.count(), len(offsets)-1)
	}
	connDS, err := cgnsPayload(f, cell.path+"/ElementConnectivity")
	if err != nil {
		return nil, err
	}
	// Priced from the header, as the native path prices its face table: every
	// cell's face references are read at once, and widening them afterwards is
	// too late to refuse a mesh the box cannot hold.
	if connDS.Len() > h5MaxSurfaceConnectivity {
		return nil, fmt.Errorf("%s holds %d face references, too many to invert",
			cell.path, connDS.Len())
	}
	conn, err := connDS.Ints()
	if err != nil {
		return nil, err
	}

	owners := make([]int64, 2*faces.count)
	for i := range owners {
		owners[i] = -1
	}
	origin := offsets[0]
	for c := int64(0); c < cell.count(); c++ {
		if c%4096 == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		start, end := offsets[c]-origin, offsets[c+1]-origin
		if start < 0 || end > int64(len(conn)) || end < start {
			continue
		}
		for _, ref := range conn[start:end] {
			// The sign carries the face's orientation relative to the cell,
			// which says nothing about which cell it is.
			if ref < 0 {
				ref = -ref
			}
			if idx, ok := faces.index(ref); ok {
				owners[2*int64(idx)+1] = c
			}
		}
	}
	return owners, nil
}
