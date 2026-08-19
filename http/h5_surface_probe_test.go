package fbhttp

import (
	"fmt"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/rforced/filebrowser/v2/hdf5"
)

// TestH5SurfaceProbe measures one real post file through the real extraction
// path. It is a measuring tool, not an assertion: point FB_PROBE_H5 at a file
// and it reports what the surface endpoint would cost to answer.
//
//	CGO_ENABLED=0 go test -c -o /tmp/h5probe ./http
//	FB_PROBE_H5=/path/post.h5 /tmp/h5probe -test.run H5SurfaceProbe -test.v
func TestH5SurfaceProbe(t *testing.T) {
	path := os.Getenv("FB_PROBE_H5")
	if path == "" {
		t.Skip("set FB_PROBE_H5 to a post file to measure it")
	}

	fh, err := os.Open(path)
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

	stream := os.Getenv("FB_PROBE_STREAM")
	if stream == "" {
		stream = "STREAM_00"
	}
	fmt.Printf("file      %s (%.0f MB)\n", path, float64(st.Size())/(1<<20))

	if ds, err := f.Dataset(stream + "/CONNECTIVITY/POLYGON_OFFSET"); err == nil {
		fmt.Printf("faces     %d\n", ds.Len()-1)
	}
	if ds, err := f.Dataset(stream + "/CONNECTIVITY/POLYGON_TO_VERTEX"); err == nil {
		fmt.Printf("face vtx  %d (%.0f MB widened to int64)\n",
			ds.Len(), float64(ds.Len()*8)/(1<<20))
	}
	if ds, err := f.Dataset(stream + "/VERTEX_COORDINATES/X"); err == nil {
		fmt.Printf("vertices  %d\n", ds.Len())
	}

	for _, tc := range []struct {
		name  string
		query map[string][]string
	}{
		{"low     200k", map[string][]string{"surface": {stream}, "limit": {"200000"}}},
		{"medium  500k", map[string][]string{"surface": {stream}, "limit": {"500000"}}},
		{"high      2M", map[string][]string{"surface": {stream}, "limit": {"2000000"}}},
		{"ultra     5M", map[string][]string{"surface": {stream}}},
		{"low   + edges", map[string][]string{"surface": {stream}, "limit": {"200000"}, "edges": {"1"}}},
		{"high  + edges", map[string][]string{"surface": {stream}, "limit": {"2000000"}, "edges": {"1"}}},
		{"ultra + edges", map[string][]string{"surface": {stream}, "edges": {"1"}}},
	} {
		if tc.query["scalar"] != nil && tc.query["scalar"][0] == "" {
			continue
		}
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/h5/probe", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		start := time.Now()
		status, err := h5SurfaceResponse(rec, req, f, tc.query)
		elapsed := time.Since(start)

		runtime.ReadMemStats(&after)
		peak := float64(after.TotalAlloc-before.TotalAlloc) / (1 << 20)
		live := float64(after.HeapAlloc) / (1 << 20)

		fmt.Printf("\n%-18s status %d  %.2fs\n", tc.name, status, elapsed.Seconds())
		if err != nil {
			fmt.Printf("  error: %v\n", err)
			continue
		}
		fmt.Printf("  payload   %.1f MB\n", float64(rec.Body.Len())/(1<<20))
		fmt.Printf("  allocated %.0f MB total, %.0f MB live after\n", peak, live)
		fmt.Printf("  sys       %.0f MB from the OS\n", float64(after.Sys)/(1<<20))
	}
}
