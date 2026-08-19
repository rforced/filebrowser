package fbhttp

import (
	"crypto/sha256"
	"fmt"
	"net/http/httptest"
	"os"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

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

	reportBoundaries(t, f, stream)

	ultra := map[string][]string{"surface": {stream}}
	ultraEdges := map[string][]string{"surface": {stream}, "edges": {"1"}}
	low := map[string][]string{"surface": {stream}, "limit": {"200000"}}

	for _, tc := range []struct {
		name  string
		enc   string
		query map[string][]string
	}{
		{"ultra  identity", "", ultra},
		{"ultra  gzip", "gzip", ultra},
		{"ultra  zstd", "zstd", ultra},
		{"ultra+e identity", "", ultraEdges},
		{"ultra+e gzip", "gzip", ultraEdges},
		{"ultra+e zstd", "zstd", ultraEdges},
		{"low    identity", "", low},
		{"low    zstd", "zstd", low},
	} {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/h5/probe", nil)
		if tc.enc != "" {
			req.Header.Set("Accept-Encoding", tc.enc)
		}
		start := time.Now()
		status, err := h5SurfaceResponse(rec, req, f, tc.query)
		elapsed := time.Since(start)

		runtime.ReadMemStats(&after)
		peak := float64(after.TotalAlloc-before.TotalAlloc) / (1 << 20)
		live := float64(after.HeapAlloc) / (1 << 20)

		fmt.Printf("\n%-18s status %d  %.3fs  encoding %q\n",
			tc.name, status, elapsed.Seconds(), rec.Header().Get("Content-Encoding"))
		if err != nil {
			fmt.Printf("  error: %v\n", err)
			continue
		}
		fmt.Printf("  payload   %.1f MB  sha %x  workers %d\n",
			float64(rec.Body.Len())/(1<<20),
			sha256.Sum256(rec.Body.Bytes()), h5SurfaceWorkers(1<<20))
		if tc.enc == "" {
			d := decodeSurface(t, rec)
			h := d.header
			fmt.Printf("  drawn     %d faces of %d, %d triangles, %d vertices\n",
				h.Faces, h.FacesTotal, h.Triangles, h.Vertices)
			fmt.Printf("  stride    %d (truncated %v, skipped %d)\n",
				h.Stride, h.Truncated, h.Skipped)
			if h.Stride > 1 {
				fmt.Printf("  WHOLE     ~%d triangles at stride 1, ceiling is %d\n",
					h.Triangles*h.Stride, h5MaxSurfaceTriangles)
			}
		}
		if tc.enc == "" && rec.Body.Len() > 1<<20 {
			benchEncoders(rec.Body.Bytes())
		}
		fmt.Printf("  allocated %.0f MB total, %.0f MB live after\n", peak, live)
		fmt.Printf("  sys       %.0f MB from the OS\n", float64(after.Sys)/(1<<20))
	}
}

// benchEncoders times the packers against the payload they would actually be
// handed, since how well this one compresses is the whole question.
func benchEncoders(payload []byte) {
	raw := float64(len(payload)) / (1 << 20)
	for _, tc := range []struct {
		name string
		opts []zstd.EOption
	}{
		{"fastest, 1 thread", []zstd.EOption{
			zstd.WithEncoderLevel(zstd.SpeedDefault), zstd.WithEncoderConcurrency(1)}},
		{"fastest, 2 threads", []zstd.EOption{
			zstd.WithEncoderLevel(zstd.SpeedDefault), zstd.WithEncoderConcurrency(2)}},
		{"fastest, 4 threads", []zstd.EOption{
			zstd.WithEncoderLevel(zstd.SpeedDefault), zstd.WithEncoderConcurrency(4)}},
		{"fastest, 2t, no crc", []zstd.EOption{
			zstd.WithEncoderLevel(zstd.SpeedDefault), zstd.WithEncoderConcurrency(2),
			zstd.WithEncoderCRC(false)}},
		{"default, 2 threads", []zstd.EOption{
			zstd.WithEncoderLevel(zstd.SpeedDefault), zstd.WithEncoderConcurrency(2)}},
	} {
		enc, err := zstd.NewWriter(nil, tc.opts...)
		if err != nil {
			fmt.Printf("    %-20s error %v\n", tc.name, err)
			continue
		}
		var sink countingWriter
		enc.Reset(&sink)
		start := time.Now()
		if _, err := enc.Write(payload); err != nil {
			fmt.Printf("    %-20s error %v\n", tc.name, err)
			enc.Close()
			continue
		}
		err = enc.Close()
		elapsed := time.Since(start)
		if err != nil {
			fmt.Printf("    %-20s error %v\n", tc.name, err)
			continue
		}
		fmt.Printf("    %-20s %6.1f MB  %.3fs  %5.0f MB/s\n",
			tc.name, float64(sink)/(1<<20), elapsed.Seconds(), raw/elapsed.Seconds())
	}
}

type countingWriter int

func (c *countingWriter) Write(p []byte) (int, error) {
	*c += countingWriter(len(p))
	return len(p), nil
}

// reportBoundaries shows how the wetted surface is split up, because the cut
// runs a boundary at a time and one that holds most of the faces sets the
// floor on what any number of workers can do.
func reportBoundaries(t *testing.T, f *hdf5.File, stream string) {
	t.Helper()
	connDS, err := f.Dataset(stream + "/CONNECTIVITY/CONNECTED_CELLS")
	if err != nil {
		return
	}
	connected, err := connDS.Ints()
	if err != nil {
		return
	}
	count := map[int64]int{}
	total := 0
	for i := 0; i+1 < len(connected); i += 2 {
		if owner := connected[i]; owner < 0 {
			count[-owner-1]++
			total++
		}
	}
	ids := make([]int64, 0, len(count))
	for id := range count {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool { return count[ids[a]] > count[ids[b]] })

	fmt.Printf("boundary faces %d across %d boundaries\n", total, len(ids))
	for i, id := range ids {
		if i >= 8 {
			fmt.Printf("  ... %d more\n", len(ids)-i)
			break
		}
		fmt.Printf("  id %-4d %9d  %5.1f%%\n", id, count[id],
			100*float64(count[id])/float64(total))
	}
}
