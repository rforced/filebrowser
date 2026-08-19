package fbhttp

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rforced/filebrowser/v2/hdf5"
	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/users"
)

// h5Scope copies the generated HDF5 fixtures into a user scope. See
// hdf5/testdata/generate.py for what they contain and why they are shaped like
// CONVERGE output.
func h5Scope(t *testing.T) string {
	t.Helper()
	scope := t.TempDir()
	for _, name := range []string{
		"post.h5", "restart.h5", "odd.h5", "links.h5", "diverged.h5",
		"newstyle.h5", "post.cgns", "mixed.cgns",
	} {
		b, err := os.ReadFile(filepath.Join("..", "hdf5", "testdata", name))
		if err != nil {
			t.Fatalf("read fixture %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(scope, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(scope, "thermo.out"), []byte("not hdf5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(scope, "outputs_original"), 0o755); err != nil {
		t.Fatal(err)
	}
	return scope
}

func h5Handlers(t *testing.T, scope string, perm users.Permissions) (http.Handler, string) {
	t.Helper()
	st := scopedUserStorage(t, scope, perm, []byte("test-signing-key"))
	return handle(h5Handler, "/api/h5", st, &settings.Server{}), issueToken(t, st)
}

func h5Get(t *testing.T, h http.Handler, token, url string) *httptest.ResponseRecorder {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, http.NoBody)
	req.Header.Set("X-Auth", token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestH5SummaryPostFile(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/post.h5")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	var got h5Summary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Kind != "post" {
		t.Errorf("kind = %q, want post", got.Kind)
	}
	// Post files have no SOLVER_VERSION string; the version is spelled across
	// three numeric attributes and has to be recomposed.
	if got.Solver != "CONVERGE 6.0.1" {
		t.Errorf("solver = %q, want CONVERGE 6.0.1", got.Solver)
	}

	// CRANK_FLAG=1 means the sim time is in degrees, not seconds. Getting this
	// wrong would label crank angle as seconds throughout the UI.
	if got.Time == nil {
		t.Fatal("no sim time reported")
	}
	if got.Time.Unit != "deg" {
		t.Errorf("time unit = %q, want deg", got.Time.Unit)
	}
	if v := got.Time.Value; v < -359.95 || v > -359.94 {
		t.Errorf("time value = %v, want ~-359.945", v)
	}
	if got.Time.Seconds == nil {
		t.Error("OUTPUT_TIME_SEC not surfaced")
	}
	if got.Time.RPM != 3000 {
		t.Errorf("rpm = %v, want 3000", got.Time.RPM)
	}

	if len(got.Streams) != 1 {
		t.Fatalf("streams = %d, want 1", len(got.Streams))
	}
	s := got.Streams[0]
	if s.Name != "STREAM_00" || s.Cells != 4 {
		t.Errorf("stream = %+v", s)
	}
	if s.Faces != 5 {
		t.Errorf("faces = %d, want 5", s.Faces)
	}
	if len(s.Variables) != 3 {
		t.Errorf("variables = %d, want 3", len(s.Variables))
	}
	// Sorted, so the manifest reads predictably in the UI.
	if s.Variables[0].Name != "EQUIV_RATIO" {
		t.Errorf("first variable = %q", s.Variables[0].Name)
	}
	if s.Variables[0].Type != "float32" {
		t.Errorf("variable type = %q", s.Variables[0].Type)
	}

	if len(s.Parcels) != 1 {
		t.Fatalf("parcel groups = %d, want 1", len(s.Parcels))
	}
	if s.Parcels[0].Count != 3 || !s.Parcels[0].HasCoords {
		t.Errorf("parcels = %+v", s.Parcels[0])
	}

	if len(got.Boundaries) != 3 {
		t.Fatalf("boundaries = %d, want 3", len(got.Boundaries))
	}
	if got.Boundaries[2].Name != "SPARK PLUG" {
		t.Errorf("boundary name = %q; embedded space should survive", got.Boundaries[2].Name)
	}
	if got.Boundaries[0].Elements != 2 {
		t.Errorf("boundary elements = %d, want 2", got.Boundaries[0].Elements)
	}
}

func TestH5SummaryRestartFile(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/restart.h5")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}
	var got h5Summary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	// These four are what make a restart choosable: which cycle it stopped at,
	// which build wrote it, and how many ranks it was decomposed for.
	if got.Kind != "restart" {
		t.Errorf("kind = %q, want restart", got.Kind)
	}
	if got.Solver != "CONVERGE 6.0.1" {
		t.Errorf("solver = %q", got.Solver)
	}
	if got.Cycle == nil || *got.Cycle != 6959 {
		t.Errorf("cycle = %v", got.Cycle)
	}
	if got.Ranks == nil || *got.Ranks != 88 {
		t.Errorf("ranks = %v", got.Ranks)
	}
	if got.RestartNum == nil || *got.RestartNum != 74 {
		t.Errorf("restart number = %v", got.RestartNum)
	}
	if got.CompileDate != "Jul 27 2026" {
		t.Errorf("compile date = %q", got.CompileDate)
	}
	// The restart carries its time on the stream, not the root — and carries no
	// CRANK_FLAG anywhere, so the unit must be left unset rather than guessed.
	// Verified across CONVERGE 4.1.2, 5.1.1 and 6.0.1: labelling this "s" would
	// relabel crank-angle degrees as seconds on every engine case.
	if got.Time == nil || got.Time.Value != -120.1942 {
		t.Errorf("time = %+v", got.Time)
	}
	if got.Time.Unit != "" {
		t.Errorf("time unit = %q, want empty (the file does not say)", got.Time.Unit)
	}
}

func TestH5Stats(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token,
		"/api/h5/post.h5?stats=STREAM_00/CELL_CENTER_DATA/TEMPERATURE,STREAM_00/CELL_CENTER_DATA/EQUIV_RATIO")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	var got struct {
		Stats []h5StatsEntry `json:"stats"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Stats) != 2 {
		t.Fatalf("entries = %d", len(got.Stats))
	}

	temp := got.Stats[0]
	if temp.Name != "TEMPERATURE" || temp.Min != 300 || temp.Max != 1200 {
		t.Errorf("temperature stats = %+v", temp)
	}

	// The divergence signal: NaN and Inf counted, and kept out of the range.
	eq := got.Stats[1]
	if eq.NaN != 1 || eq.Inf != 1 || eq.Finite != 2 {
		t.Errorf("equiv_ratio stats = %+v", eq)
	}
	if eq.Min != 0.5 || eq.Max != 1.5 {
		t.Errorf("equiv_ratio range = %v..%v", eq.Min, eq.Max)
	}
}

func TestH5StatsReportsPerDatasetErrors(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	// One good, one missing: the request must still succeed and say which failed.
	rec := h5Get(t, h, token, "/api/h5/post.h5?stats=STREAM_00/CELL_CENTER_DATA/TEMPERATURE,STREAM_00/NOPE")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got struct {
		Stats []h5StatsEntry `json:"stats"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Stats[0].Err != "" {
		t.Errorf("good dataset reported error %q", got.Stats[0].Err)
	}
	if got.Stats[1].Err == "" {
		t.Error("missing dataset reported no error")
	}
}

func TestH5Parcels(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token,
		"/api/h5/post.h5?parcels=STREAM_00/PARCEL_DATA/LIQUID_PARCEL_DATA/LIQPARCEL_1&scalar=TEMP")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	var got h5ParcelCloud
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Count != 3 || got.Sent != 3 || got.Stride != 1 {
		t.Errorf("counts = %+v", got)
	}
	if len(got.Points) != 9 {
		t.Fatalf("points = %d floats, want 9", len(got.Points))
	}
	if got.Points[0] != 0 || got.Points[3] != 1 || got.Points[6] != 2 {
		t.Errorf("x coords = %v %v %v", got.Points[0], got.Points[3], got.Points[6])
	}
	if len(got.Radius) != 3 {
		t.Errorf("radius = %v", got.Radius)
	}
	if got.Scalar != "TEMP" || len(got.Values) != 3 {
		t.Errorf("scalar = %q values = %v", got.Scalar, got.Values)
	}
	if got.Range[0] != 300 || got.Range[1] != 350 {
		t.Errorf("scalar range = %v", got.Range)
	}
	// Bounds drive the camera framing.
	if got.Bounds[0] != 0 || got.Bounds[3] != 2 || got.Bounds[5] != 0 {
		t.Errorf("bounds = %v", got.Bounds)
	}
}

func TestH5ParcelsStrideDownsamples(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	// A limit below the parcel count must stride across the whole cloud rather
	// than return its first corner.
	rec := h5Get(t, h, token,
		"/api/h5/post.h5?parcels=STREAM_00/PARCEL_DATA/LIQUID_PARCEL_DATA/LIQPARCEL_1&limit=2")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got h5ParcelCloud
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Stride != 2 || got.Sent != 2 || got.Count != 3 {
		t.Errorf("stride=%d sent=%d count=%d", got.Stride, got.Sent, got.Count)
	}
	if got.Points[3] != 2 {
		t.Errorf("second point x = %v, want 2 (strided, not sequential)", got.Points[3])
	}
}

// TestH5ParcelsSurviveDivergence is the case the parcel view exists for. JSON
// cannot carry NaN or Inf at all, so a diverged spray used to fail marshalling
// and answer 500 — no cloud, no message, nothing to look at.
func TestH5ParcelsSurviveDivergence(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token,
		"/api/h5/diverged.h5?parcels=STREAM_00/PARCEL_DATA/LIQUID_PARCEL_DATA/LIQPARCEL_1&scalar=TEMP")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	var got struct {
		Count  uint64     `json:"count"`
		Sent   uint64     `json:"sent"`
		Points []float32  `json:"points"`
		Values []*float32 `json:"values"`
		Bounds [6]float64 `json:"bounds"`
		Range  [2]float64 `json:"range"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v body = %q", err, rec.Body.String())
	}

	// Three parcels, one of which has lost its position and cannot be placed
	// in the scene. Count still reports the whole cloud.
	if got.Count != 3 || got.Sent != 2 {
		t.Errorf("count = %d, sent = %d; want 3, 2", got.Count, got.Sent)
	}
	if len(got.Points) != 6 {
		t.Fatalf("points = %d floats, want 6", len(got.Points))
	}
	if got.Points[0] != 0 || got.Points[3] != 2 {
		t.Errorf("x coords = %v, %v; want the NaN parcel skipped", got.Points[0], got.Points[3])
	}

	// The parcel that kept its position but lost its temperature is still
	// drawn; its value is null rather than a number it does not have.
	if len(got.Values) != 2 {
		t.Fatalf("values = %v, want 2", got.Values)
	}
	if got.Values[0] == nil || *got.Values[0] != 300 {
		t.Errorf("first value = %v, want 300", got.Values[0])
	}
	if got.Values[1] != nil {
		t.Errorf("non-finite value = %v, want null", *got.Values[1])
	}

	// Neither bounds nor range may inherit the non-finite values.
	if got.Range[0] != 300 || got.Range[1] != 300 {
		t.Errorf("range = %v, want finite values only", got.Range)
	}
	for i, v := range got.Bounds {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Errorf("bounds[%d] = %v, want finite", i, v)
		}
	}
}

// TestH5ParcelsBeforeInjection covers a spray group that exists but holds
// nothing yet, which is every engine case before the injector opens.
func TestH5ParcelsBeforeInjection(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token,
		"/api/h5/diverged.h5?parcels=STREAM_00/PARCEL_DATA/LIQUID_PARCEL_DATA/LIQPARCEL_EMPTY")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}
	// An empty cloud must still be an empty list, not null: the client counts
	// points before it draws them.
	if !strings.Contains(rec.Body.String(), `"points":[]`) {
		t.Errorf("empty cloud body = %q", rec.Body.String())
	}

	var got h5ParcelCloud
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Count != 0 || got.Sent != 0 {
		t.Errorf("count = %d, sent = %d; want 0, 0", got.Count, got.Sent)
	}
	if got.Bounds != [6]float64{} || got.Range != [2]float64{} {
		t.Errorf("bounds = %v range = %v; want zeroed", got.Bounds, got.Range)
	}
}

// TestH5StatsOnDegenerateFields covers the fields that have no spread: all
// zero, all NaN, and no cells at all.
func TestH5StatsOnDegenerateFields(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/diverged.h5?stats="+
		"STREAM_00/CELL_CENTER_DATA/ALL_ZERO,"+
		"STREAM_00/CELL_CENTER_DATA/ALL_NAN,"+
		"STREAM_00/CELL_CENTER_DATA/NO_CELLS")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}
	var got struct {
		Stats []h5StatsEntry `json:"stats"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Stats) != 3 {
		t.Fatalf("entries = %d", len(got.Stats))
	}

	if z := got.Stats[0]; z.Err != "" || z.Finite != 6 || z.Min != 0 || z.Max != 0 || z.Mean != 0 {
		t.Errorf("all-zero stats = %+v", z)
	}
	// Nothing finite means no range to report, and the NaN count is what says
	// the field diverged.
	if n := got.Stats[1]; n.Err != "" || n.NaN != 6 || n.Finite != 0 || n.Min != 0 || n.Max != 0 {
		t.Errorf("all-NaN stats = %+v", n)
	}
	if e := got.Stats[2]; e.Err != "" || e.Count != 0 || e.Finite != 0 {
		t.Errorf("empty field stats = %+v", e)
	}
}

// TestH5SummaryReportsUnlistableStream pins the rule that a stream the reader
// cannot describe is an error, not an omission: a summary reporting no streams
// reads as an empty file, which is the one answer that is certainly wrong.
//
// The damage is done to the fractal heap that holds the stream's cell data —
// its object count is stepped past what the blocks hold, which is the check
// that stops a heap from being half-read. The root's own links sit in its
// object header and are untouched, so the file still opens and still lists:
// the summary has to fail on the stream alone, which is the case the rule is
// about.
func TestH5SummaryReportsUnlistableStream(t *testing.T) {
	scope := h5Scope(t)
	raw, err := os.ReadFile(filepath.Join("..", "hdf5", "testdata", "newstyle.h5"))
	if err != nil {
		t.Fatal(err)
	}
	const countOffset = 14 + 4*8 + 2*8 + 8
	for i := 0; ; {
		j := strings.Index(string(raw[i:]), "FRHP")
		if j < 0 || i+j+countOffset+8 > len(raw) {
			break
		}
		at := i + j + countOffset
		binary.LittleEndian.PutUint64(raw[at:], binary.LittleEndian.Uint64(raw[at:])+1)
		i += j + 4
	}
	if err := os.WriteFile(filepath.Join(scope, "damaged.h5"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	h, token := h5Handlers(t, scope, users.Permissions{Download: true})
	rec := h5Get(t, h, token, "/api/h5/damaged.h5")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d body = %q, want 422", rec.Code, rec.Body.String())
	}
}

// TestH5SummaryOverHeapedLinks describes a CONVERGE-shaped file written in the
// structure generation CGNS uses: the stream's cell data has outgrown its
// object header into a fractal heap. Nothing above the reader should notice.
func TestH5SummaryOverHeapedLinks(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/newstyle.h5")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}
	var got h5Summary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Streams) != 1 || len(got.Streams[0].Variables) != 13 {
		t.Fatalf("streams = %+v", got.Streams)
	}
	if got.Streams[0].Variables[0].Name != "DENSITY" {
		t.Errorf("variables are not sorted: %+v", got.Streams[0].Variables)
	}
}

// TestH5ScalarVariableDims pins the shape of a 0-rank dataset on the wire. A
// nil slice marshals to null, and the client multiplies the dimensions out to
// a element count as soon as it renders the row.
func TestH5ScalarVariableDims(t *testing.T) {
	fh, err := os.Open(filepath.Join("..", "hdf5", "testdata", "odd.h5"))
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		t.Fatal(err)
	}
	f, err := hdf5.Open(fh, st.Size())
	if err != nil {
		t.Fatal(err)
	}

	vars, _, err := h5ReadVariables(f, "")
	if err != nil {
		t.Fatal(err)
	}
	var scalar *h5Variable
	for i := range vars {
		if vars[i].Name == "scalar" {
			scalar = &vars[i]
		}
	}
	if scalar == nil {
		t.Fatal("no scalar dataset in the fixture")
	}
	if scalar.Dims == nil {
		t.Error("scalar dims are nil; they must marshal as an empty list")
	}
	body, err := json.Marshal(scalar)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"dims":[]`) {
		t.Errorf("scalar variable = %s", body)
	}
}

func TestH5ParcelsMissingGroup(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})
	rec := h5Get(t, h, token, "/api/h5/post.h5?parcels=STREAM_00/NOT_PARCELS")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestH5SubsetDownload(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token,
		"/api/h5/post.h5?subset=STREAM_00/CELL_CENTER_DATA/TEMPERATURE,STREAM_00/CELL_CENTER_DATA/PRESSURE")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("content-type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "post_subset.csv") {
		t.Errorf("content-disposition = %q", cd)
	}

	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want header + 4 rows: %q", len(lines), lines)
	}
	if lines[0] != "TEMPERATURE,PRESSURE" {
		t.Errorf("header = %q", lines[0])
	}
	if lines[1] != "300,100000" {
		t.Errorf("first row = %q", lines[1])
	}
	if lines[2] != "450.5,110000" {
		t.Errorf("second row = %q", lines[2])
	}
}

// h5GetQueryAuth requests with the token in the query and no header, the way a
// browser-initiated download arrives.
func h5GetQueryAuth(t *testing.T, h http.Handler, token, url string) *httptest.ResponseRecorder {
	t.Helper()
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	req, _ := http.NewRequest(http.MethodGet, url+sep+"auth="+token, http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestH5QueryTokenOnlyForDownload pins the auth split. The CSV subset is pulled
// by an anchor that cannot carry a header, so it accepts a query token like
// /api/raw does; the JSON modes are all script-driven and must not, because a
// token in a URL survives in history, proxy logs and referrers.
func TestH5QueryTokenOnlyForDownload(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5GetQueryAuth(t, h, token,
		"/api/h5/post.h5?subset=STREAM_00/CELL_CENTER_DATA/TEMPERATURE")
	if rec.Code != http.StatusOK {
		t.Errorf("subset via query token: status = %d, want 200", rec.Code)
	}
	if !strings.HasPrefix(rec.Body.String(), "TEMPERATURE") {
		t.Errorf("subset body = %q", rec.Body.String()[:min(40, rec.Body.Len())])
	}

	for _, url := range []string{
		"/api/h5/post.h5",
		"/api/h5/post.h5?stats=STREAM_00/CELL_CENTER_DATA/TEMPERATURE",
		"/api/h5/post.h5?parcels=STREAM_00/PARCEL_DATA/LIQUID_PARCEL_DATA/LIQPARCEL_1",
	} {
		if rec := h5GetQueryAuth(t, h, token, url); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s via query token: status = %d, want 401", url, rec.Code)
		}
	}

	// The same requests must still work with the header.
	if rec := h5Get(t, h, token, "/api/h5/post.h5"); rec.Code != http.StatusOK {
		t.Errorf("summary via header: status = %d, want 200", rec.Code)
	}
}

func TestH5RejectsNonHDF5(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/thermo.out")
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want 415", rec.Code)
	}
}

func TestH5RejectsDirectory(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/outputs_original")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestH5RejectsMissingFile(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	rec := h5Get(t, h, token, "/api/h5/nope.h5")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestH5RequiresDownloadPermission(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: false})

	for _, url := range []string{
		"/api/h5/post.h5",
		"/api/h5/post.h5?stats=STREAM_00/CELL_CENTER_DATA/TEMPERATURE",
		"/api/h5/post.h5?subset=STREAM_00/CELL_CENTER_DATA/TEMPERATURE",
	} {
		if rec := h5Get(t, h, token, url); rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", url, rec.Code)
		}
	}
}

func TestH5ChunkedDatasetIsAnError(t *testing.T) {
	h, token := h5Handlers(t, h5Scope(t), users.Permissions{Download: true})

	// odd.h5 holds chunked and gzipped datasets. The summary still works —
	// they are skipped — but asking for their values must fail loudly rather
	// than return zeroes.
	rec := h5Get(t, h, token, "/api/h5/odd.h5")
	if rec.Code != http.StatusOK {
		t.Fatalf("summary status = %d body = %q", rec.Code, rec.Body.String())
	}

	rec = h5Get(t, h, token, "/api/h5/odd.h5?stats=chunked")
	if rec.Code != http.StatusOK {
		t.Fatalf("stats status = %d", rec.Code)
	}
	var got struct {
		Stats []h5StatsEntry `json:"stats"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Stats[0].Err == "" {
		t.Error("chunked dataset reported no error")
	}
}
