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

// The block reader exists to stop a walk allocating a pair of buffers per
// read, so it hands back a slice it will overwrite next time. That is only
// safe if it decodes exactly what the allocating path does, for every span and
// in every order — a reused buffer that kept a stale tail would show up as
// geometry from the previous block.
func TestIntsBlockReaderAgreesWithIntsRange(t *testing.T) {
	f := open(t, "post.h5")

	ds, err := f.Dataset("STREAM_00/CONNECTIVITY/POLYGON_TO_VERTEX")
	if err != nil {
		t.Fatal(err)
	}
	all, err := ds.Ints()
	if err != nil {
		t.Fatal(err)
	}

	// One reader across every span: a fresh one per read would not exercise
	// the reuse this exists for.
	var r IntsBlockReader
	for start := uint64(0); start <= ds.Len(); start++ {
		for count := uint64(0); start+count <= ds.Len(); count++ {
			got, err := r.Range(ds, start, count)
			if err != nil {
				t.Fatalf("Range(%d, %d): %v", start, count, err)
			}
			want := all[start : start+count]
			if len(got) != len(want) {
				t.Fatalf("Range(%d, %d) length = %d, want %d",
					start, count, len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("Range(%d, %d)[%d] = %d, want %d",
						start, count, i, got[i], want[i])
				}
			}
		}
	}
}

// Shrinking spans are where a reused buffer leaks its tail: the slice must be
// cut to the count asked for, not to whatever the buffer grew to.
func TestIntsBlockReaderShrinksToTheCountAsked(t *testing.T) {
	f := open(t, "post.h5")

	ds, err := f.Dataset("STREAM_00/CONNECTIVITY/POLYGON_TO_VERTEX")
	if err != nil {
		t.Fatal(err)
	}
	all, err := ds.Ints()
	if err != nil {
		t.Fatal(err)
	}

	var r IntsBlockReader
	if _, err := r.Range(ds, 0, ds.Len()); err != nil {
		t.Fatal(err)
	}
	got, err := r.Range(ds, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("length = %d after a larger read, want 2", len(got))
	}
	if got[0] != all[1] || got[1] != all[2] {
		t.Errorf("got %v, want %v", got, all[1:3])
	}
}

func TestIntsBlockReaderRejectsSpansPastTheEnd(t *testing.T) {
	f := open(t, "post.h5")

	ds, err := f.Dataset("STREAM_00/CONNECTIVITY/POLYGON_TO_VERTEX")
	if err != nil {
		t.Fatal(err)
	}

	var r IntsBlockReader
	if _, err := r.Range(ds, ds.Len(), 1); err == nil {
		t.Error("want an error for a span past the end")
	}
	if _, err := r.Range(ds, 0, ds.Len()+1); err == nil {
		t.Error("want an error for a count past the end")
	}
}
