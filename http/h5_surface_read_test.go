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

func surfaceConnectedDataset(t *testing.T) *hdf5.Dataset {
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
	ds, err := f.Dataset("STREAM_00/CONNECTIVITY/CONNECTED_CELLS")
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

// The face table is read in blocks now rather than whole, and a block edge
// must not be able to lose a face, shift one, or read an owner slot as a
// neighbour. Every span has to give the same answer as reading it at once —
// these walk the edge across every face in the fixture.
func TestH5ScanBoundaryFacesIsIndependentOfBlockSize(t *testing.T) {
	ds := surfaceConnectedDataset(t)
	ctx := context.Background()

	whole, wholeFluid, err := h5ScanBoundaryFaces(ctx, ds, 5, nil, 1<<21)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture's five faces sit two on boundary 1 and three on boundary 2.
	if len(whole) != 2 || len(whole[1]) != 2 || len(whole[2]) != 3 {
		t.Fatalf("whole read = %v, want 2 faces on boundary 1 and 3 on boundary 2", whole)
	}

	// Odd spans included: the scan has to round them down to a face pair
	// rather than start a block halfway through one.
	for _, span := range []uint64{1, 2, 3, 4, 6, 7, 8, 10, 12, 1 << 21} {
		got, gotFluid, err := h5ScanBoundaryFaces(ctx, ds, 5, nil, span)
		if err != nil {
			t.Fatalf("span %d: %v", span, err)
		}
		if !sameFaceMap(got, whole) {
			t.Errorf("span %d faces = %v, want %v", span, got, whole)
		}
		if !sameFaceMap(gotFluid, wholeFluid) {
			t.Errorf("span %d cells = %v, want %v", span, gotFluid, wholeFluid)
		}
	}
}

// Only the faces the offsets account for are scanned; a face table running
// past them would otherwise contribute faces no offset can place.
func TestH5ScanBoundaryFacesStopsAtTheFaceCount(t *testing.T) {
	ds := surfaceConnectedDataset(t)

	got, _, err := h5ScanBoundaryFaces(context.Background(), ds, 3, nil, 1<<21)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, faces := range got {
		total += len(faces)
		for _, face := range faces {
			if face >= 3 {
				t.Errorf("face %d is past the 3 the offsets name", face)
			}
		}
	}
	if total != 3 {
		t.Errorf("scanned %d faces, want 3", total)
	}
}

func TestH5ScanBoundaryFacesHonoursTheFilter(t *testing.T) {
	ds := surfaceConnectedDataset(t)

	got, fluid, err := h5ScanBoundaryFaces(
		context.Background(), ds, 5, map[int64]bool{2: true}, 1<<21)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[2]) != 3 {
		t.Errorf("filtered = %v, want boundary 2 alone with 3 faces", got)
	}
	if len(fluid[2]) != 3 {
		t.Errorf("filtered cells = %v, want one per face", fluid)
	}
}

func TestH5ScanBoundaryFacesStopsWhenCancelled(t *testing.T) {
	ds := surfaceConnectedDataset(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := h5ScanBoundaryFaces(ctx, ds, 5, nil, 2); err == nil {
		t.Error("want a cancellation error")
	}
}

func sameFaceMap(a, b map[int64][]int32) bool {
	if len(a) != len(b) {
		return false
	}
	for id, want := range b {
		got, ok := a[id]
		if !ok || len(got) != len(want) {
			return false
		}
		for i := range want {
			if got[i] != want[i] {
				return false
			}
		}
	}
	return true
}

// The stride used to be applied inside the extractor, which meant every face
// was fetched and most were then stepped over. Narrowing byBoundary here is
// what keeps the read down to what is drawn, so the two must agree exactly.
func TestH5DrawnFacesNarrowsByBoundaryToWhatIsRead(t *testing.T) {
	offsets := []int64{0, 3, 7, 10, 13, 17}
	ids := []int64{1, 2}
	byBoundary := map[int64][]int32{1: {0, 1}, 2: {2, 3, 4}}
	fluid := map[int64][]int32{1: {10, 11}, 2: {12, 13, 14}}

	drawn := h5DrawnFaces(offsets, ids, byBoundary, fluid, 2, 17)

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
	// The fluid cells are addressed by position within a boundary, so they
	// have to lose exactly the same entries. A cell left behind here colours
	// every face after it with its neighbour's value — a wall that looks
	// entirely plausible and is wrong.
	if got := fluid[1]; len(got) != 1 || got[0] != 10 {
		t.Errorf("boundary 1 cells = %v, want [10]", got)
	}
	if got := fluid[2]; len(got) != 2 || got[0] != 12 || got[1] != 14 {
		t.Errorf("boundary 2 cells = %v, want [12 14]", got)
	}
	for _, id := range ids {
		if len(byBoundary[id]) != len(fluid[id]) {
			t.Errorf("boundary %d kept %d faces but %d cells",
				id, len(byBoundary[id]), len(fluid[id]))
		}
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

	drawn := h5DrawnFaces(offsets, ids, byBoundary, nil, 1, 17)

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

	drawn := h5DrawnFaces(offsets, ids, byBoundary, nil, 2, int64(len(full)))
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
