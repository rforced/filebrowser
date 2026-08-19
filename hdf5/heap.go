package hdf5

import (
	"encoding/binary"
	"fmt"
)

const (
	frhpSignature = "FRHP"
	fhdbSignature = "FHDB"
	fhibSignature = "FHIB"
)

// Bounds on a heap walk. A group's links are a few hundred bytes each, so a
// heap holding real link data is kilobytes; these stop a corrupt header from
// describing a heap larger than the machine.
const (
	maxHeapBlocks     = 1 << 16
	maxHeapBytes      = 64 << 20
	maxHeapBlockBytes = 1 << 26
)

// fractalHeap is where a group keeps its links once there are more of them
// than the object header will hold — eight, by HDF5's default. CGNS files
// cross that line exactly where it matters: the zone, the flow solution and
// the header node all have nine or more children.
//
// Only the managed-object side is implemented. Managed objects live in direct
// blocks laid out by a doubling table; huge objects are stored on their own
// and tiny ones inside the heap ID. A link message is neither — it is tens of
// bytes, far above the heap ID width and far below the direct block size — so
// a heap claiming either kind is a heap this reader has misunderstood, and it
// says so rather than returning a partial list of children.
type fractalHeap struct {
	f *File

	// objects is how many managed objects the header says are stored, and is
	// what the walk below is checked against.
	objects        uint64
	tableWidth     int
	startBlockSize uint64
	maxDirectSize  uint64
	// offsetBytes is the width of the heap offset each block states, derived
	// from the maximum heap size in bits.
	offsetBytes int
	rootAddr    uint64
	rootRows    int
	checksummed bool
}

// openFractalHeap parses a heap header.
func (f *File) openFractalHeap(addr uint64) (*fractalHeap, error) {
	o, l := int(f.offsetSz), int(f.lengthSz)
	// Fixed prefix (14), twelve length-sized counters, three addresses, four
	// two-byte fields and the checksum.
	head, err := f.readAt(addr, 22+12*l+3*o+4)
	if err != nil {
		return nil, err
	}
	if string(head[0:4]) != frhpSignature {
		return nil, fmt.Errorf("%w: bad fractal heap signature", ErrNotHDF5)
	}
	if head[4] != 0 {
		return nil, fmt.Errorf("%w: fractal heap version %d", ErrUnsupported, head[4])
	}

	filterLen := binary.LittleEndian.Uint16(head[7:9])
	if filterLen != 0 {
		return nil, fmt.Errorf("%w: filtered fractal heap", ErrUnsupported)
	}
	flags := head[9]

	p := 14
	p += l     // next huge object ID
	p += o     // v2 B-tree address of huge objects
	p += l     // free space in managed blocks
	p += o     // free space manager address
	p += 2 * l // managed space, allocated managed space
	p += l     // direct block iterator offset
	managed := f.length(head[p:])
	p += l
	p += l // size of huge objects
	huge := f.length(head[p:])
	p += l
	p += l // size of tiny objects
	tiny := f.length(head[p:])
	p += l

	width := binary.LittleEndian.Uint16(head[p:])
	p += 2
	start := f.length(head[p:])
	p += l
	maxDirect := f.length(head[p:])
	p += l
	maxHeapBits := binary.LittleEndian.Uint16(head[p:])
	p += 2
	p += 2 // starting number of rows in the root indirect block
	rootAddr := f.offset(head[p:])
	p += o
	rootRows := binary.LittleEndian.Uint16(head[p:])

	if huge != 0 || tiny != 0 {
		return nil, fmt.Errorf("%w: fractal heap with %d huge and %d tiny objects",
			ErrUnsupported, huge, tiny)
	}
	if width == 0 || start == 0 || maxDirect < start {
		return nil, fmt.Errorf("%w: fractal heap doubling table %d x %d..%d",
			ErrNotHDF5, width, start, maxDirect)
	}
	if start > maxHeapBlockBytes || maxDirect > maxHeapBlockBytes {
		return nil, fmt.Errorf("%w: fractal heap block size %d", ErrNotHDF5, maxDirect)
	}
	if maxHeapBits == 0 || maxHeapBits > 64 {
		return nil, fmt.Errorf("%w: fractal heap address size %d bits", ErrNotHDF5, maxHeapBits)
	}

	return &fractalHeap{
		f:              f,
		objects:        managed,
		tableWidth:     int(width),
		startBlockSize: start,
		maxDirectSize:  maxDirect,
		offsetBytes:    int((maxHeapBits + 7) / 8),
		rootAddr:       rootAddr,
		rootRows:       int(rootRows),
		checksummed:    flags&0x02 != 0,
	}, nil
}

// rowBlockSize is the size of every direct block in one row of the doubling
// table. The first two rows share the starting size and each row after that
// doubles.
func (h *fractalHeap) rowBlockSize(row int) uint64 {
	if row <= 1 {
		return h.startBlockSize
	}
	return h.startBlockSize << uint(row-1)
}

// directRows is how many rows hold direct blocks; past that a row's entries
// address indirect blocks instead.
func (h *fractalHeap) directRows() int {
	rows := 2
	for size := h.startBlockSize; size < h.maxDirectSize; size *= 2 {
		rows++
	}
	return rows
}

// blocks returns the object area of every allocated direct block, in heap
// order. The root block is a direct block until the heap outgrows one, at
// which point it becomes an indirect block whose rows address the direct ones.
func (h *fractalHeap) blocks() ([][]byte, error) {
	if h.f.undefined(h.rootAddr) {
		return nil, nil
	}
	if h.rootRows == 0 {
		b, err := h.readDirect(h.rootAddr, h.startBlockSize)
		if err != nil {
			return nil, err
		}
		return [][]byte{b}, nil
	}

	var out [][]byte
	total := 0
	if err := h.readIndirect(h.rootAddr, h.rootRows, &out, &total); err != nil {
		return nil, err
	}
	return out, nil
}

// readIndirect walks one indirect block's rows of child direct blocks.
//
// Indirect blocks that address further indirect blocks are refused rather than
// followed. That only happens once a heap outgrows every direct row — with
// HDF5's defaults, half a megabyte of link data, or tens of thousands of
// children in a single group. Refusing is honest about what has been read;
// returning the direct rows alone would report a group as missing most of its
// children.
func (h *fractalHeap) readIndirect(addr uint64, rows int, out *[][]byte, total *int) error {
	o := int(h.f.offsetSz)
	headLen := 5 + o + h.offsetBytes
	if rows < 0 || rows > 64 {
		return fmt.Errorf("%w: fractal heap indirect block with %d rows", ErrNotHDF5, rows)
	}
	entries := rows * h.tableWidth
	if entries > maxHeapBlocks {
		return fmt.Errorf("%w: fractal heap indirect block with %d entries", ErrNotHDF5, entries)
	}

	buf, err := h.f.readAt(addr, headLen+entries*o+4)
	if err != nil {
		return err
	}
	if string(buf[0:4]) != fhibSignature {
		return fmt.Errorf("%w: bad fractal heap indirect block signature", ErrNotHDF5)
	}
	if buf[4] != 0 {
		return fmt.Errorf("%w: fractal heap indirect block version %d", ErrUnsupported, buf[4])
	}

	direct := h.directRows()
	p := headLen
	for row := 0; row < rows; row++ {
		size := h.rowBlockSize(row)
		for col := 0; col < h.tableWidth; col++ {
			child := h.f.offset(buf[p:])
			p += o
			if h.f.undefined(child) {
				continue
			}
			if row >= direct {
				return fmt.Errorf("%w: fractal heap nested more than one level deep", ErrUnsupported)
			}
			block, err := h.readDirect(child, size)
			if err != nil {
				return err
			}
			*total += len(block)
			if *total > maxHeapBytes || len(*out) >= maxHeapBlocks {
				return fmt.Errorf("%w: fractal heap larger than %d bytes", ErrNotHDF5, maxHeapBytes)
			}
			*out = append(*out, block)
		}
	}
	return nil
}

// readDirect returns one direct block's object area — the block past its
// signature, heap address, heap offset and optional checksum.
func (h *fractalHeap) readDirect(addr, size uint64) ([]byte, error) {
	if size > maxHeapBlockBytes {
		return nil, fmt.Errorf("%w: fractal heap direct block of %d bytes", ErrNotHDF5, size)
	}
	buf, err := h.f.readAt(addr, int(size))
	if err != nil {
		return nil, err
	}
	if len(buf) < 5 || string(buf[0:4]) != fhdbSignature {
		return nil, fmt.Errorf("%w: bad fractal heap direct block signature", ErrNotHDF5)
	}
	if buf[4] != 0 {
		return nil, fmt.Errorf("%w: fractal heap direct block version %d", ErrUnsupported, buf[4])
	}

	head := 5 + int(h.f.offsetSz) + h.offsetBytes
	if h.checksummed {
		head += 4
	}
	if head > len(buf) {
		return nil, fmt.Errorf("%w: truncated fractal heap direct block", ErrNotHDF5)
	}
	return buf[head:], nil
}
