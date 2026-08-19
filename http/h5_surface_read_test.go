package fbhttp

import (
	"context"
	"os"
	"testing"

	"github.com/rforced/filebrowser/v2/hdf5"
)

func surfacePolyDataset(t *testing.T) *hdf5.Dataset {
	t.Helper()
	fh, err := os.Open("../hdf5/testdata/post.h5")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fh.Close() })
	st, err := fh.Stat()
	if err != nil {
		t.Fatal(err)
	}
	f, err := hdf5.Open(fh, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	ds, err := f.Dataset("STREAM_00/CONNECTIVITY/POLYGON_TO_VERTEX")
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

// The stride used to be applied inside the extractor, which meant every face
// was fetched and most were then stepped over. Narrowing byBoundary here is
// what keeps the read down to what is drawn, so the two must agree exactly.
func TestH5DrawnFacesNarrowsByBoundaryToWhatIsRead(t *testing.T) {
	offsets := []int64{0, 3, 7, 10, 13, 17}
	ids := []int64{1, 2}
	byBoundary := map[int64][]int32{1: {0, 1}, 2: {2, 3, 4}}

	drawn := h5DrawnFaces(offsets, ids, byBoundary, 2, 17)

	if got, want := len(drawn), 3; got != want {
		t.Fatalf("drawn = %v, want %d faces", drawn, want)
	}
	// Every boundary keeps its own first face and then every second one.
	if got := byBoundary[1]; len(got) != 1 || got[0] != 0 {
		t.Errorf("boundary 1 = %v, want [0]", got)
	}
	if got := byBoundary[2]; len(got) != 2 || got[0] != 2 || got[1] != 4 {
		t.Errorf("boundary 2 = %v, want [2 4]", got)
	}
	for i := 1; i < len(drawn); i++ {
		if drawn[i] <= drawn[i-1] {
			t.Fatalf("drawn = %v, want ascending face order", drawn)
		}
	}
}

func TestH5DrawnFacesDropsRunsTheFileDoesNotBack(t *testing.T) {
	// Face 1 runs past the dataset, face 2 runs backwards, face 3 starts
	// before it. Only face 0 is drawable.
	offsets := []int64{0, 3, 99, 40, -1, 6}
	ids := []int64{1}
	byBoundary := map[int64][]int32{1: {0, 1, 2, 3, 4}}

	drawn := h5DrawnFaces(offsets, ids, byBoundary, 1, 17)

	if len(drawn) != 1 || drawn[0] != 0 {
		t.Fatalf("drawn = %v, want [0]", drawn)
	}
	if got := byBoundary[1]; len(got) != 1 || got[0] != 0 {
		t.Errorf("byBoundary = %v, want [0]", got)
	}
}

// The face table is restated over the concatenated runs in place. If that
// rebasing is off by even one face the extractor silently draws another face's
// vertices, so it is pinned against the runs the original table named.
func TestH5RebaseOffsetsIndexesTheSameVertices(t *testing.T) {
	ds := surfacePolyDataset(t)
	full, err := ds.Ints()
	if err != nil {
		t.Fatal(err)
	}

	for _, drawn := range [][]int32{
		{0, 1, 2, 3, 4},
		{0, 2, 4},
		{1, 3},
		{4},
		{},
	} {
		offsets := []int64{0, 3, 7, 10, 13, 17}
		want := make([][]int64, len(drawn))
		for i, face := range drawn {
			want[i] = append([]int64(nil), full[offsets[face]:offsets[face+1]]...)
		}

		starts, total := h5CompactOffsets(offsets, drawn)
		poly, err := h5ReadFaceVertices(context.Background(), ds, offsets, drawn, total)
		if err != nil {
			t.Fatalf("drawn %v: %v", drawn, err)
		}
		if int64(len(poly)) != total {
			t.Fatalf("drawn %v: read %d vertices, priced %d", drawn, len(poly), total)
		}
		h5RebaseOffsets(offsets, drawn, starts, total)

		for i, face := range drawn {
			got := poly[offsets[face]:offsets[face+1]]
			if len(got) != len(want[i]) {
				t.Fatalf("drawn %v face %d: %v, want %v", drawn, face, got, want[i])
			}
			for k := range got {
				if got[k] != want[i][k] {
					t.Fatalf("drawn %v face %d: %v, want %v", drawn, face, got, want[i])
				}
			}
		}
	}
}

// Nothing read off disk is owed a monotonic face table, and the coalescing
// read indexes into the block it fetched — so a table that runs backwards must
// close the batch rather than slice out of range.
func TestH5ReadFaceVerticesSurvivesANonMonotonicTable(t *testing.T) {
	ds := surfacePolyDataset(t)
	full, err := ds.Ints()
	if err != nil {
		t.Fatal(err)
	}

	// Face 3's run inverts, so it is dropped; the faces that survive are in
	// ascending face order but their runs are not — face 4 sits at [6,10),
	// behind face 2 at [13,17).
	offsets := []int64{0, 3, 13, 17, 6, 10}
	ids := []int64{1}
	byBoundary := map[int64][]int32{1: {0, 1, 2, 3, 4}}

	drawn := h5DrawnFaces(offsets, ids, byBoundary, 2, int64(len(full)))
	if len(drawn) != 3 || drawn[0] != 0 || drawn[1] != 2 || drawn[2] != 4 {
		t.Fatalf("drawn = %v, want [0 2 4]", drawn)
	}
	want := [][]int64{full[0:3], full[13:17], full[6:10]}

	starts, total := h5CompactOffsets(offsets, drawn)
	poly, err := h5ReadFaceVertices(context.Background(), ds, offsets, drawn, total)
	if err != nil {
		t.Fatal(err)
	}
	h5RebaseOffsets(offsets, drawn, starts, total)

	for i, face := range drawn {
		got := poly[offsets[face]:offsets[face+1]]
		if len(got) != len(want[i]) {
			t.Fatalf("face %d = %v, want %v", face, got, want[i])
		}
		for k := range got {
			if got[k] != want[i][k] {
				t.Fatalf("face %d = %v, want %v", face, got, want[i])
			}
		}
	}
}

func TestH5ReadFaceVerticesStopsWhenCancelled(t *testing.T) {
	ds := surfacePolyDataset(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	offsets := []int64{0, 3, 7, 10, 13, 17}
	drawn := []int32{0, 1, 2, 3, 4}
	_, total := h5CompactOffsets(offsets, drawn)
	if _, err := h5ReadFaceVertices(ctx, ds, offsets, drawn, total); err == nil {
		t.Error("want a cancellation error")
	}
}
