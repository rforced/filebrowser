package hdf5

import (
	"os"
	"strings"
	"testing"
)

// FuzzOpen drives the parser with mutated files. Every path must return an
// error rather than panic or allocate unboundedly: these bytes reach the
// reader straight from user-writable storage.
func FuzzOpen(f *testing.F) {
	// post.cgns and newstyle.h5 seed the second structure generation: v2
	// headers, link messages and the fractal heap, none of which the native
	// CONVERGE fixtures reach.
	for _, name := range []string{"post.h5", "restart.h5", "odd.h5", "newstyle.h5", "post.cgns", "mixed.cgns"} {
		b, err := os.ReadFile("testdata/" + name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
		// Truncations are the most likely real-world corruption: a post file
		// copied while CONVERGE was still writing it.
		f.Add(b[:len(b)/2])
		f.Add(b[:len(b)/8])
	}
	f.Add([]byte("\x89HDF\r\n\x1a\n"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		file, err := Open(strings.NewReader(string(data)), int64(len(data)))
		if err != nil {
			return
		}

		root, err := file.Root()
		if err != nil {
			return
		}
		_ = root.Attrs

		links, err := root.Children()
		if err != nil {
			return
		}
		for i, l := range links {
			if i > 32 {
				break
			}
			switch l.Kind {
			case KindGroup:
				g, err := file.Group(l.Name)
				if err != nil {
					continue
				}
				kids, err := g.Children()
				if err != nil {
					continue
				}
				for j, k := range kids {
					if j > 32 {
						break
					}
					if k.Kind != KindDataset {
						continue
					}
					readDataset(file, l.Name+"/"+k.Name)
				}
			case KindDataset:
				readDataset(file, l.Name)
			}
		}
	})
}

func readDataset(f *File, path string) {
	ds, err := f.Dataset(path)
	if err != nil {
		return
	}
	// A corrupt dataspace can claim an enormous element count; refuse to
	// materialise anything the file cannot possibly hold.
	if f.size > 0 && ds.ByteSize() > uint64(f.size) {
		return
	}
	_, _ = ds.Floats()
	_, _ = ds.Ints()
	_, _ = ds.Strings()
	_, _ = ds.Stats()
	_, _, _ = ds.Contiguous()
}
