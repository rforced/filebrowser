package hdf5

import "testing"

// A ranged read must agree with the whole-dataset read for every span, since
// the surface extractor now reaches for face runs instead of the whole
// connectivity and has no way to notice a decode that quietly differs.
func TestIntsRangeAgreesWithFullRead(t *testing.T) {
	f := open(t, "post.h5")

	ds, err := f.Dataset("STREAM_00/CONNECTIVITY/POLYGON_TO_VERTEX")
	if err != nil {
		t.Fatal(err)
	}
	all, err := ds.Ints()
	if err != nil {
		t.Fatal(err)
	}

	for start := uint64(0); start <= ds.Len(); start++ {
		for count := uint64(0); start+count <= ds.Len(); count++ {
			got, err := ds.IntsRange(start, count)
			if err != nil {
				t.Fatalf("IntsRange(%d, %d): %v", start, count, err)
			}
			want := all[start : start+count]
			if len(got) != len(want) {
				t.Fatalf("IntsRange(%d, %d) length = %d, want %d",
					start, count, len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("IntsRange(%d, %d)[%d] = %d, want %d",
						start, count, i, got[i], want[i])
				}
			}
		}
	}
}

func TestIntsRangeRejectsSpansPastTheEnd(t *testing.T) {
	f := open(t, "post.h5")

	ds, err := f.Dataset("STREAM_00/CONNECTIVITY/POLYGON_TO_VERTEX")
	if err != nil {
		t.Fatal(err)
	}
	n := ds.Len()

	for _, tc := range []struct{ start, count uint64 }{
		{0, n + 1},
		{n, 1},
		{n + 1, 0},
		{1, n},
		{^uint64(0), 1},
		{1, ^uint64(0)},
	} {
		if _, err := ds.IntsRange(tc.start, tc.count); err == nil {
			t.Errorf("IntsRange(%d, %d) of %d elements: want error", tc.start, tc.count, n)
		}
	}
}
