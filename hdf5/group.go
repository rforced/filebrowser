package hdf5

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
)

// Kind distinguishes the two object types CONVERGE files contain.
type Kind int

const (
	KindGroup Kind = iota
	KindDataset
)

// Link is one named child of a group.
type Link struct {
	Name string
	Kind Kind
	addr uint64
}

// Group is a named container of links, with its own attributes.
type Group struct {
	f     *File
	addr  uint64
	Name  string
	Attrs Attrs
}

// Root returns the file's root group.
func (f *File) Root() (*Group, error) {
	return f.groupAt(f.rootAddr, "/")
}

func (f *File) groupAt(addr uint64, name string) (*Group, error) {
	oh, err := f.readObjectHeader(addr)
	if err != nil {
		return nil, err
	}
	attrs, err := f.parseAttrs(oh)
	if err != nil {
		return nil, err
	}
	return &Group{f: f, addr: addr, Name: name, Attrs: attrs}, nil
}

// Children lists the group's links in the order the B-tree yields them, which
// for symbol-table groups is lexicographic by name.
func (g *Group) Children() ([]Link, error) {
	oh, err := g.f.readObjectHeader(g.addr)
	if err != nil {
		return nil, err
	}

	st := oh.first(msgSymbolTable)
	if st == nil {
		// A new-style group keeps its links in link messages and a fractal
		// heap instead of a symbol table. Reporting "no children" would hide
		// every dataset it holds, so say so instead.
		if oh.first(msgLink) != nil || oh.first(msgLinkInfo) != nil {
			return nil, fmt.Errorf("%w: group %s stores links without a symbol table", ErrUnsupported, g.Name)
		}
		// A group with no link storage at all has no children (or is not a
		// group at all, which callers detect via Kind).
		return nil, nil
	}
	o := int(g.f.offsetSz)
	if len(st) < 2*o {
		return nil, fmt.Errorf("%w: short symbol table message", ErrNotHDF5)
	}
	btreeAddr := g.f.offset(st)
	heapAddr := g.f.offset(st[o:])

	heap, err := g.f.readLocalHeap(heapAddr)
	if err != nil {
		return nil, err
	}

	var links []Link
	if err := g.f.walkBTree(btreeAddr, heap, &links, 0); err != nil {
		return nil, err
	}
	return links, nil
}

// localHeap is the name storage a symbol-table group's B-tree keys index into.
type localHeap struct {
	data []byte
}

func (h localHeap) name(off uint64) string {
	if off >= uint64(len(h.data)) {
		return ""
	}
	b := h.data[off:]
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

// readLocalHeap parses a HEAP block: signature, version, reserved, then the
// data segment size, free list head and data segment address.
func (f *File) readLocalHeap(addr uint64) (localHeap, error) {
	l := int(f.lengthSz)
	o := int(f.offsetSz)
	head, err := f.readAt(addr, 8+2*l+o)
	if err != nil {
		return localHeap{}, err
	}
	if string(head[0:4]) != "HEAP" {
		return localHeap{}, fmt.Errorf("%w: bad local heap signature", ErrNotHDF5)
	}
	size := f.length(head[8:])
	dataAddr := f.offset(head[8+2*l:])
	if size > maxObjectHeader {
		return localHeap{}, fmt.Errorf("%w: local heap size %d", ErrNotHDF5, size)
	}
	data, err := f.readAt(dataAddr, int(size))
	if err != nil {
		return localHeap{}, err
	}
	return localHeap{data: data}, nil
}

// maxBTreeDepth bounds the descent. Group B-trees are two or three levels
// deep in practice; the cap is what stops a child pointer that loops back on
// itself from recursing until the stack overflows, which in Go is a fatal
// error no recover() can catch.
const maxBTreeDepth = 32

// walkBTree descends a v1 B-tree of node type 0 (group nodes), collecting the
// symbol table entries at the leaves.
//
// Node layout: "TREE", node type, level, entries used (2), left sibling,
// right sibling, then alternating keys and child addresses with one trailing
// key. Keys are length-sized offsets into the local heap; at level 0 the
// children are symbol table nodes, above that they are further B-tree nodes.
func (f *File) walkBTree(addr uint64, heap localHeap, out *[]Link, depth int) error {
	if f.undefined(addr) {
		return nil
	}
	if depth > maxBTreeDepth {
		return fmt.Errorf("%w: B-tree nested deeper than %d levels", ErrNotHDF5, maxBTreeDepth)
	}
	o := int(f.offsetSz)
	l := int(f.lengthSz)

	head, err := f.readAt(addr, 8+2*o)
	if err != nil {
		return err
	}
	if string(head[0:4]) != "TREE" {
		return fmt.Errorf("%w: bad B-tree signature", ErrNotHDF5)
	}
	if head[4] != 0 {
		return fmt.Errorf("%w: B-tree node type %d", ErrUnsupported, head[4])
	}
	level := head[5]
	used := int(binary.LittleEndian.Uint16(head[6:8]))
	if used < 0 || used > maxLinks {
		return fmt.Errorf("%w: B-tree entries %d", ErrNotHDF5, used)
	}

	body, err := f.readAt(addr+uint64(8+2*o), used*(l+o)+l)
	if err != nil {
		return err
	}

	for i := 0; i < used; i++ {
		child := f.offset(body[i*(l+o)+l:])
		if level > 0 {
			if err := f.walkBTree(child, heap, out, depth+1); err != nil {
				return err
			}
			continue
		}
		if err := f.readSymbolTableNode(child, heap, out); err != nil {
			return err
		}
	}
	return nil
}

// symbolEntrySize is the on-disk size of a symbol table entry: link name
// offset, object header address, cache type, reserved, and 16 bytes of
// scratch pad.
func (f *File) symbolEntrySize() int { return 2*int(f.offsetSz) + 8 + 16 }

// readSymbolTableNode reads an SNOD block and appends one Link per symbol.
func (f *File) readSymbolTableNode(addr uint64, heap localHeap, out *[]Link) error {
	head, err := f.readAt(addr, 8)
	if err != nil {
		return err
	}
	if string(head[0:4]) != "SNOD" {
		return fmt.Errorf("%w: bad symbol table node signature", ErrNotHDF5)
	}
	n := int(binary.LittleEndian.Uint16(head[6:8]))
	if n < 0 || n > maxLinks {
		return fmt.Errorf("%w: symbol count %d", ErrNotHDF5, n)
	}
	if len(*out)+n > maxLinks {
		return fmt.Errorf("%w: too many links", ErrNotHDF5)
	}

	entries, err := f.readAt(addr+8, n*f.symbolEntrySize())
	if err != nil {
		return err
	}
	o := int(f.offsetSz)
	for i := 0; i < n; i++ {
		e := entries[i*f.symbolEntrySize():]
		name := heap.name(f.offset(e))
		objAddr := f.offset(e[o:])
		cacheType := binary.LittleEndian.Uint32(e[2*o : 2*o+4])

		// Cache type 1 means the scratch pad holds the group's B-tree and heap
		// addresses, so the object is definitely a group. Type 0 needs the
		// object header to tell group from dataset; a link storage message
		// there is the marker.
		kind := KindDataset
		switch cacheType {
		case 1:
			kind = KindGroup
		case 2:
			// A symbolic link names no object of its own: the scratch pad
			// holds the target path and the header address is undefined.
			// Whatever it points at is listed under its real name, so drop
			// the entry rather than failing the whole group on it.
			continue
		default:
			oh, err := f.readObjectHeader(objAddr)
			if err != nil {
				return err
			}
			if oh.first(msgSymbolTable) != nil || oh.first(msgLink) != nil || oh.first(msgLinkInfo) != nil {
				kind = KindGroup
			}
		}

		*out = append(*out, Link{Name: name, Kind: kind, addr: objAddr})
	}
	return nil
}

// find resolves a slash-separated path from the root, returning the link that
// names the final component.
func (f *File) find(path string) (Link, error) {
	parts := strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
	if len(parts) == 0 {
		return Link{Name: "/", Kind: KindGroup, addr: f.rootAddr}, nil
	}

	cur := Link{Name: "/", Kind: KindGroup, addr: f.rootAddr}
	for _, part := range parts {
		if cur.Kind != KindGroup {
			return Link{}, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		g, err := f.groupAt(cur.addr, cur.Name)
		if err != nil {
			return Link{}, err
		}
		links, err := g.Children()
		if err != nil {
			return Link{}, err
		}
		found := false
		for _, l := range links {
			if l.Name == part {
				cur, found = l, true
				break
			}
		}
		if !found {
			return Link{}, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
	}
	return cur, nil
}

// Group opens a group by absolute path, e.g. "STREAM_00/CELL_CENTER_DATA".
func (f *File) Group(path string) (*Group, error) {
	l, err := f.find(path)
	if err != nil {
		return nil, err
	}
	if l.Kind != KindGroup {
		return nil, fmt.Errorf("%w: %s is not a group", ErrNotFound, path)
	}
	return f.groupAt(l.addr, l.Name)
}

// Dataset opens a dataset by absolute path.
func (f *File) Dataset(path string) (*Dataset, error) {
	l, err := f.find(path)
	if err != nil {
		return nil, err
	}
	if l.Kind != KindDataset {
		return nil, fmt.Errorf("%w: %s is not a dataset", ErrNotFound, path)
	}
	return f.datasetAt(l.addr, l.Name)
}

// Exists reports whether an object is present at path.
func (f *File) Exists(path string) bool {
	_, err := f.find(path)
	return err == nil
}
