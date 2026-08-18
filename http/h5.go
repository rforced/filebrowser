package fbhttp

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/rforced/filebrowser/v2/hdf5"
)

// h5MaxVariables caps how many datasets one response describes. Real post
// files carry tens; the cap only stops a pathological file from producing a
// huge JSON body.
const h5MaxVariables = 4096

// h5MaxStats caps how many datasets one stats request may scan, since each one
// is a full read of that variable.
const h5MaxStats = 64

// h5MaxParcels caps how many parcels are sent to the browser in one go. The
// v6 engine case carries ~115k; beyond a few hundred thousand points the
// client is the bottleneck, not the wire.
const h5MaxParcels = 500000

type h5Time struct {
	Value float64 `json:"value"`
	// Unit is empty when the file does not say. Restarts and map files carry a
	// sim time but no CRANK_FLAG, so guessing "s" would silently relabel crank
	// degrees as seconds; callers that know the deck can supply the unit.
	Unit    string   `json:"unit,omitempty"`
	Seconds *float64 `json:"seconds,omitempty"`
	RPM     float64  `json:"rpm,omitempty"`
}

type h5Variable struct {
	Name  string   `json:"name"`
	Path  string   `json:"path"`
	Type  string   `json:"type"`
	Dims  []uint64 `json:"dims"`
	Bytes uint64   `json:"bytes"`
}

type h5ParcelGroup struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Count     uint64   `json:"count"`
	Variables []string `json:"variables"`
	HasCoords bool     `json:"hasCoords"`
}

type h5Stream struct {
	Name      string          `json:"name"`
	Cells     uint64          `json:"cells,omitempty"`
	Vertices  uint64          `json:"vertices,omitempty"`
	Faces     uint64          `json:"faces,omitempty"`
	Variables []h5Variable    `json:"variables"`
	Parcels   []h5ParcelGroup `json:"parcels,omitempty"`
}

type h5Boundary struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Elements int64  `json:"elements"`
}

type h5Summary struct {
	Name        string       `json:"name"`
	Size        int64        `json:"size"`
	Kind        string       `json:"kind"`
	Solver      string       `json:"solver,omitempty"`
	CompileDate string       `json:"compileDate,omitempty"`
	Time        *h5Time      `json:"time,omitempty"`
	Cycle       *int64       `json:"cycle,omitempty"`
	Ranks       *int64       `json:"ranks,omitempty"`
	RestartNum  *int64       `json:"restartNumber,omitempty"`
	Streams     []h5Stream   `json:"streams"`
	Boundaries  []h5Boundary `json:"boundaries,omitempty"`
	Truncated   bool         `json:"truncated,omitempty"`
}

type h5StatsEntry struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Type string `json:"type"`
	Err  string `json:"error,omitempty"`
	hdf5.Stats
}

type h5ParcelCloud struct {
	Path   string     `json:"path"`
	Count  uint64     `json:"count"`
	Sent   uint64     `json:"sent"`
	Stride uint64     `json:"stride"`
	Scalar string     `json:"scalar,omitempty"`
	Bounds [6]float64 `json:"bounds"`
	Points h5Floats   `json:"points"`
	Radius h5Floats   `json:"radius,omitempty"`
	Values h5Floats   `json:"values,omitempty"`
	Range  [2]float64 `json:"range"`
	Vars   []string   `json:"variables"`
}

// h5Floats is a float array that survives a diverged run. encoding/json
// refuses NaN and Inf outright, which would turn the one view someone opens to
// look at a divergence into a 500; here they become null, which the client can
// draw as "no value".
type h5Floats []float32

func (v h5Floats) MarshalJSON() ([]byte, error) {
	buf := make([]byte, 0, len(v)*8+2)
	buf = append(buf, '[')
	for i, f := range v {
		if i > 0 {
			buf = append(buf, ',')
		}
		if f64 := float64(f); math.IsNaN(f64) || math.IsInf(f64, 0) {
			buf = append(buf, "null"...)
		} else {
			buf = strconv.AppendFloat(buf, f64, 'g', -1, 32)
		}
	}
	return append(buf, ']'), nil
}

// h5FiniteCoord reports whether a position survives the trip to float32. A
// coordinate that does not cannot be placed in the scene at all, unlike a
// scalar value, which is still worth sending as null.
func h5FiniteCoord(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// h5Handler serves metadata, statistics, parcel clouds and variable subsets
// for the HDF5 files CONVERGE writes. Everything it does is read-only.
//
// It takes withMediaUser for the sake of one mode: the CSV subset is fetched
// by a plain anchor so the browser can stream it to disk, and an anchor cannot
// set the X-Auth header — the same reason /api/raw and /api/preview accept a
// query token. Every other mode is called from script, so the guard below
// keeps them header-only rather than widening token-in-URL exposure to the
// whole endpoint.
var h5Handler = withMediaUser(func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	if !d.user.Perm.Download {
		return http.StatusForbidden, nil
	}

	if r.URL.Query().Get("subset") == "" && r.Header.Get("X-Auth") == "" {
		return http.StatusUnauthorized, nil
	}

	path := r.URL.Path
	if !d.Check(path) {
		return http.StatusForbidden, nil
	}

	info, err := d.user.Fs.Stat(path)
	if err != nil {
		return errToStatus(err), err
	}
	if info.IsDir() {
		return http.StatusBadRequest, fmt.Errorf("%s is a directory", path)
	}

	fh, err := d.user.Fs.Open(path)
	if err != nil {
		return errToStatus(err), err
	}
	defer fh.Close()

	f, err := hdf5.Open(fh, info.Size())
	if err != nil {
		if errors.Is(err, hdf5.ErrNotHDF5) {
			return http.StatusUnsupportedMediaType, err
		}
		return http.StatusUnprocessableEntity, err
	}

	query := r.URL.Query()
	switch {
	case query.Get("stats") != "":
		return h5StatsResponse(w, r, f, query.Get("stats"))
	case query.Get("parcels") != "":
		return h5ParcelResponse(w, r, f, query)
	case query.Get("subset") != "":
		return h5SubsetResponse(w, r, f, info.Name(), query)
	}

	summary, err := h5Describe(f, info.Name(), info.Size())
	if err != nil {
		return http.StatusUnprocessableEntity, err
	}
	return renderJSON(w, r, summary)
})

// h5Describe builds the CONVERGE-flavoured view of a file. The HDF5 package
// stays generic; the knowledge of what STREAM_00 and BOUNDARIES mean lives
// here, next to the rest of the CONVERGE handling.
func h5Describe(f *hdf5.File, name string, size int64) (*h5Summary, error) {
	root, err := f.Root()
	if err != nil {
		return nil, err
	}

	s := &h5Summary{
		Name:    name,
		Size:    size,
		Kind:    h5Kind(name, root),
		Streams: []h5Stream{},
	}

	// Restarts carry a version string; map files carry a bare number; post
	// files spell it out across three numeric attributes.
	if v, ok := root.Attrs.Text("SOLVER_VERSION"); ok {
		s.Solver = v
	} else if v, ok := root.Attrs.Text("VERSION"); ok {
		s.Solver = "CONVERGE " + v
	} else if major, ok := root.Attrs.Int("VERSION_NUM1"); ok {
		minor, _ := root.Attrs.Int("VERSION_NUM2")
		patch, _ := root.Attrs.Int("VERSION_NUM3")
		s.Solver = fmt.Sprintf("CONVERGE %d.%d.%d", major, minor, patch)
	}
	if v, ok := root.Attrs.Text("COMPILE_DATE"); ok {
		s.CompileDate = v
	}
	if v, ok := root.Attrs.Int("NCYC"); ok {
		s.Cycle = &v
	}
	if v, ok := root.Attrs.Int("TOTAL_MPI_PROCESSES"); ok {
		s.Ranks = &v
	}
	if v, ok := root.Attrs.Int("RESTART_FILE_NUM"); ok {
		s.RestartNum = &v
	}
	s.Time = h5ReadTime(root.Attrs)

	links, err := root.Children()
	if err != nil {
		return nil, err
	}
	for _, l := range links {
		if l.Kind != hdf5.KindGroup {
			continue
		}
		switch {
		case l.Name == "BOUNDARIES":
			s.Boundaries = h5ReadBoundaries(f)
		case strings.HasPrefix(l.Name, "STREAM_"):
			st, truncated, err := h5ReadStream(f, l.Name, root.Attrs)
			if err != nil {
				// A stream the reader cannot describe is reported rather than
				// dropped: a summary listing no streams reads as an empty
				// file, which is the one answer that is certainly wrong.
				return nil, err
			}
			s.Truncated = s.Truncated || truncated
			s.Streams = append(s.Streams, st)
		}
	}

	// A restart holds its sim time on the stream, not the root.
	if s.Time == nil {
		for _, name := range []string{"STREAM_00", "STREAM_0"} {
			g, err := f.Group(name)
			if err != nil {
				continue
			}
			if s.Time = h5ReadTime(g.Attrs); s.Time != nil {
				break
			}
		}
	}

	return s, nil
}

// h5ReadTime resolves the sim time and, where the file says so, its unit.
// CRANK_FLAG is the only thing that distinguishes seconds from crank-angle
// degrees, and post files are the only kind that carry it: a .rst holds its
// time in STREAM_00/TIME_STEP and a map file in the same place, both with no
// unit marker anywhere in the file. Verified across CONVERGE 4.1.2, 5.1.1 and
// 6.0.1 — so the unit is left unset rather than assumed.
func h5ReadTime(attrs hdf5.Attrs) *h5Time {
	value, ok := attrs.Float("OUTPUT_TIME")
	if !ok {
		if value, ok = attrs.Float("TIME_STEP"); !ok {
			return nil
		}
	}

	t := &h5Time{Value: value}
	if flag, ok := attrs.Int("CRANK_FLAG"); ok {
		if flag != 0 {
			t.Unit = "deg"
		} else {
			t.Unit = "s"
		}
	}
	if v, ok := attrs.Float("CRANK_ANGLE"); ok {
		t.Unit = "deg"
		t.Value = v
	}
	if v, ok := attrs.Float("OUTPUT_TIME_SEC"); ok {
		sec := v
		t.Seconds = &sec
	}
	if v, ok := attrs.Float("RPM"); ok {
		t.RPM = v
	}
	return t
}

func h5Kind(name string, root *hdf5.Group) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".rst"):
		return "restart"
	case strings.HasPrefix(lower, "post"):
		return "post"
	case strings.HasPrefix(lower, "map"):
		return "map"
	case strings.Contains(lower, "table"):
		return "table"
	}
	if _, ok := root.Attrs.Int("NCYC"); ok {
		return "restart"
	}
	return "dataset"
}

func h5ReadBoundaries(f *hdf5.File) []h5Boundary {
	ids, err := f.Dataset("BOUNDARIES/BOUNDARY_IDS")
	if err != nil {
		return nil
	}
	idv, err := ids.Ints()
	if err != nil {
		return nil
	}

	names := make([]string, 0, len(idv))
	if ds, err := f.Dataset("BOUNDARIES/BOUNDARY_NAMES"); err == nil {
		if v, err := ds.Strings(); err == nil {
			names = v
		}
	}
	var counts []int64
	if ds, err := f.Dataset("BOUNDARIES/NUM_ELEMENTS"); err == nil {
		if v, err := ds.Ints(); err == nil {
			counts = v
		}
	}

	out := make([]h5Boundary, 0, len(idv))
	for i, id := range idv {
		b := h5Boundary{ID: id}
		if i < len(names) {
			b.Name = names[i]
		}
		if i < len(counts) {
			b.Elements = counts[i]
		}
		out = append(out, b)
	}
	return out
}

// h5ReadStream describes one STREAM_* group: its mesh sizes, the cell
// variables, and any parcel clouds hanging off it.
func h5ReadStream(f *hdf5.File, name string, _ hdf5.Attrs) (h5Stream, bool, error) {
	g, err := f.Group(name)
	if err != nil {
		return h5Stream{}, false, err
	}
	st := h5Stream{Name: name, Variables: []h5Variable{}}

	if v, ok := g.Attrs.Int("CELL_COUNT"); ok && v > 0 {
		st.Cells = uint64(v)
	}
	if ds, err := f.Dataset(name + "/VERTEX_COORDINATES/X"); err == nil {
		st.Vertices = ds.Len()
	}
	if ds, err := f.Dataset(name + "/CONNECTIVITY/POLYGON_OFFSET"); err == nil && ds.Len() > 0 {
		st.Faces = ds.Len() - 1
	}

	truncated := false
	// CELL_CENTER_DATA is the post file's name for it; restarts and maps use
	// CELL_CENTER for the same thing.
	for _, sub := range []string{"CELL_CENTER_DATA", "CELL_CENTER"} {
		vars, cut, err := h5ReadVariables(f, name+"/"+sub)
		if err != nil {
			return h5Stream{}, false, err
		}
		truncated = truncated || cut
		st.Variables = append(st.Variables, vars...)
	}
	sort.Slice(st.Variables, func(i, j int) bool { return st.Variables[i].Name < st.Variables[j].Name })

	parcels, err := h5ReadParcelGroups(f, name+"/PARCEL_DATA")
	if err != nil {
		return h5Stream{}, false, err
	}
	st.Parcels = parcels
	return st, truncated, nil
}

// h5ReadVariables lists one group of datasets. A group that is simply absent
// is not an error — post files name it CELL_CENTER_DATA and restarts
// CELL_CENTER, so one of the two always misses — but a group the reader cannot
// list is, because reporting it as empty would describe a file full of
// variables as having none.
func h5ReadVariables(f *hdf5.File, path string) ([]h5Variable, bool, error) {
	g, err := f.Group(path)
	if err != nil {
		if errors.Is(err, hdf5.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	links, err := g.Children()
	if err != nil {
		return nil, false, err
	}

	out := make([]h5Variable, 0, len(links))
	for _, l := range links {
		if l.Kind != hdf5.KindDataset {
			continue
		}
		if len(out) >= h5MaxVariables {
			return out, true, nil
		}
		ds, err := f.Dataset(path + "/" + l.Name)
		if err != nil {
			continue
		}
		// A scalar dataset has no dimensions. Sending null instead of an
		// empty list would leave the client multiplying out a missing array.
		dims := ds.Dims
		if dims == nil {
			dims = []uint64{}
		}
		out = append(out, h5Variable{
			Name:  l.Name,
			Path:  path + "/" + l.Name,
			Type:  ds.Type.String(),
			Dims:  dims,
			Bytes: ds.ByteSize(),
		})
	}
	return out, false, nil
}

// h5ReadParcelGroups walks the parcel branch. v6 post files nest one level
// (LIQUID_PARCEL_DATA/LIQPARCEL_1) while map_parcel files add a state level
// above it (AIRBORNE/…, WALL_ATTACHED/…), so the walk descends until it finds
// groups that hold datasets.
func h5ReadParcelGroups(f *hdf5.File, path string) ([]h5ParcelGroup, error) {
	g, err := f.Group(path)
	if err != nil {
		// Most files carry no parcels at all; only a branch the reader cannot
		// walk is worth reporting.
		if errors.Is(err, hdf5.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	var out []h5ParcelGroup
	var walkErr error
	var walk func(prefix string, group *hdf5.Group, depth int)
	walk = func(prefix string, group *hdf5.Group, depth int) {
		if depth > 4 || len(out) >= 64 {
			return
		}
		links, err := group.Children()
		if err != nil {
			if walkErr == nil {
				walkErr = err
			}
			return
		}

		var vars []string
		var count uint64
		coords := 0
		for _, l := range links {
			if l.Kind != hdf5.KindDataset {
				continue
			}
			vars = append(vars, l.Name)
			if ds, err := f.Dataset(prefix + "/" + l.Name); err == nil && ds.Len() > count {
				count = ds.Len()
			}
			switch l.Name {
			case "PARCEL_X", "PARCEL_Y", "PARCEL_Z", "XX_0", "XX_1", "XX_2":
				coords++
			}
		}
		if len(vars) > 0 {
			sort.Strings(vars)
			out = append(out, h5ParcelGroup{
				Name:      strings.TrimPrefix(prefix, path+"/"),
				Path:      prefix,
				Count:     count,
				Variables: vars,
				HasCoords: coords >= 3,
			})
			return
		}

		for _, l := range links {
			if l.Kind != hdf5.KindGroup {
				continue
			}
			sub, err := f.Group(prefix + "/" + l.Name)
			if err != nil {
				if walkErr == nil {
					walkErr = err
				}
				continue
			}
			walk(prefix+"/"+l.Name, sub, depth+1)
		}
	}
	walk(path, g, 0)
	return out, walkErr
}

// h5StatsResponse scans the named datasets. Each is a full read of that
// variable, which is why the count is capped.
func h5StatsResponse(w http.ResponseWriter, r *http.Request, f *hdf5.File, list string) (int, error) {
	paths := h5SplitList(list)
	if len(paths) == 0 {
		return http.StatusBadRequest, errors.New("no datasets requested")
	}
	if len(paths) > h5MaxStats {
		return http.StatusBadRequest, fmt.Errorf("at most %d datasets per request", h5MaxStats)
	}

	out := make([]h5StatsEntry, 0, len(paths))
	for _, p := range paths {
		entry := h5StatsEntry{Path: p, Name: p[strings.LastIndex(p, "/")+1:]}
		ds, err := f.Dataset(p)
		if err != nil {
			entry.Err = err.Error()
			out = append(out, entry)
			continue
		}
		entry.Type = ds.Type.String()
		st, err := ds.Stats()
		if err != nil {
			entry.Err = err.Error()
			out = append(out, entry)
			continue
		}
		entry.Stats = st
		out = append(out, entry)
	}
	return renderJSON(w, r, map[string]any{"stats": out})
}

// h5ParcelResponse extracts a parcel cloud: positions, radii and one optional
// scalar. Parcels need no mesh or connectivity, which is what makes this the
// cheapest 3D view of a post file.
func h5ParcelResponse(w http.ResponseWriter, r *http.Request, f *hdf5.File, query map[string][]string) (int, error) {
	base := strings.Trim(firstValue(query, "parcels"), "/")
	g, err := f.Group(base)
	if err != nil {
		return http.StatusNotFound, err
	}
	links, err := g.Children()
	if err != nil {
		return http.StatusUnprocessableEntity, err
	}

	var names []string
	for _, l := range links {
		if l.Kind == hdf5.KindDataset {
			names = append(names, l.Name)
		}
	}
	sort.Strings(names)

	xs, ys, zs, err := h5ParcelCoords(f, base)
	if err != nil {
		return http.StatusUnprocessableEntity, err
	}
	total := uint64(len(xs))

	limit := uint64(h5MaxParcels)
	if v := firstValue(query, "limit"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil && n > 0 && n < limit {
			limit = n
		}
	}
	// Uniform stride rather than a head slice: a truncated cloud must still
	// look like the spray, not like its first corner.
	stride := uint64(1)
	if total > limit {
		stride = (total + limit - 1) / limit
	}

	cloud := h5ParcelCloud{
		Path: base, Count: total, Stride: stride, Vars: names,
		Bounds: [6]float64{math.Inf(1), math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1), math.Inf(-1)},
		Range:  [2]float64{math.Inf(1), math.Inf(-1)},
	}

	var radius []float64
	if ds, err := f.Dataset(base + "/RADIUS"); err == nil {
		radius, _ = ds.Floats()
	}
	scalar := firstValue(query, "scalar")
	var values []float64
	if scalar != "" {
		ds, err := f.Dataset(base + "/" + scalar)
		if err != nil {
			return http.StatusNotFound, err
		}
		if values, err = ds.Floats(); err != nil {
			return http.StatusUnprocessableEntity, err
		}
		cloud.Scalar = scalar
	}

	for i := uint64(0); i < total; i += stride {
		x, y, z := float32(xs[i]), float32(ys[i]), float32(zs[i])
		// A parcel with no usable position is dropped rather than sent as a
		// hole: it cannot be drawn, and a NaN here would poison the bounds
		// the camera frames on. Count minus Sent still says how many went.
		if !h5FiniteCoord(x) || !h5FiniteCoord(y) || !h5FiniteCoord(z) {
			continue
		}
		cloud.Points = append(cloud.Points, x, y, z)
		cloud.Bounds[0] = math.Min(cloud.Bounds[0], xs[i])
		cloud.Bounds[1] = math.Min(cloud.Bounds[1], ys[i])
		cloud.Bounds[2] = math.Min(cloud.Bounds[2], zs[i])
		cloud.Bounds[3] = math.Max(cloud.Bounds[3], xs[i])
		cloud.Bounds[4] = math.Max(cloud.Bounds[4], ys[i])
		cloud.Bounds[5] = math.Max(cloud.Bounds[5], zs[i])
		if i < uint64(len(radius)) {
			cloud.Radius = append(cloud.Radius, float32(radius[i]))
		}
		if i < uint64(len(values)) {
			v := values[i]
			cloud.Values = append(cloud.Values, float32(v))
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				cloud.Range[0] = math.Min(cloud.Range[0], v)
				cloud.Range[1] = math.Max(cloud.Range[1], v)
			}
		}
		cloud.Sent++
	}
	if math.IsInf(cloud.Range[0], 1) {
		cloud.Range = [2]float64{0, 0}
	}
	if cloud.Sent == 0 {
		cloud.Bounds = [6]float64{}
	}

	return renderJSON(w, r, cloud)
}

// h5ParcelCoords reads parcel positions under either naming scheme: post
// files use PARCEL_X/Y/Z, map_parcel files use XX_0/1/2.
func h5ParcelCoords(f *hdf5.File, base string) (xs, ys, zs []float64, err error) {
	for _, set := range [][3]string{
		{"PARCEL_X", "PARCEL_Y", "PARCEL_Z"},
		{"XX_0", "XX_1", "XX_2"},
	} {
		var got [3][]float64
		ok := true
		for i, name := range set {
			ds, err := f.Dataset(base + "/" + name)
			if err != nil {
				ok = false
				break
			}
			if got[i], err = ds.Floats(); err != nil {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		n := min(len(got[0]), min(len(got[1]), len(got[2])))
		return got[0][:n], got[1][:n], got[2][:n], nil
	}
	return nil, nil, nil, fmt.Errorf("no parcel coordinates under %s", base)
}

// h5SubsetResponse streams the chosen variables as CSV. Pulling one field
// out of a post file is a seek and a read, so this turns a 60-220MB download
// into a few hundred KB over a WAN link.
func h5SubsetResponse(w http.ResponseWriter, _ *http.Request, f *hdf5.File, name string, query map[string][]string) (int, error) {
	paths := h5SplitList(firstValue(query, "subset"))
	if len(paths) == 0 {
		return http.StatusBadRequest, errors.New("no variables requested")
	}
	if len(paths) > h5MaxStats {
		return http.StatusBadRequest, fmt.Errorf("at most %d variables per request", h5MaxStats)
	}

	columns := make([][]float64, 0, len(paths))
	headers := make([]string, 0, len(paths))
	rows := 0
	for _, p := range paths {
		ds, err := f.Dataset(p)
		if err != nil {
			return http.StatusNotFound, err
		}
		values, err := ds.Floats()
		if err != nil {
			return http.StatusUnprocessableEntity, err
		}
		columns = append(columns, values)
		headers = append(headers, p[strings.LastIndex(p, "/")+1:])
		if len(values) > rows {
			rows = len(values)
		}
	}

	base := strings.TrimSuffix(strings.TrimSuffix(name, ".h5"), ".rst")
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", base+"_subset.csv"))

	var line strings.Builder
	line.WriteString(strings.Join(headers, ","))
	line.WriteByte('\n')
	if _, err := w.Write([]byte(line.String())); err != nil {
		return 0, err
	}

	buf := make([]byte, 0, 64*len(columns))
	for i := 0; i < rows; i++ {
		buf = buf[:0]
		for c, col := range columns {
			if c > 0 {
				buf = append(buf, ',')
			}
			if i < len(col) {
				buf = strconv.AppendFloat(buf, col[i], 'g', -1, 64)
			}
		}
		buf = append(buf, '\n')
		if _, err := w.Write(buf); err != nil {
			return 0, err
		}
	}
	return 0, nil
}

func h5SplitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(strings.Trim(p, "/")); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstValue(query map[string][]string, key string) string {
	if v := query[key]; len(v) > 0 {
		return v[0]
	}
	return ""
}
