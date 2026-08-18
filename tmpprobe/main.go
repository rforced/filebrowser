package main

import (
	"bytes"
	"fmt"
	"os"
)

func main() {
	b, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	// superblock: 8-byte magic then version byte
	magic := []byte("\x89HDF\r\n\x1a\n")
	off := bytes.Index(b, magic)
	fmt.Printf("superblock at %d, version=%d, size=%d\n", off, b[off+8], len(b))

	sigs := []string{"OHDR", "OCHK", "FRHP", "FHDB", "FHIB", "BTHD", "BTIN", "BTLF", "TREE", "HEAP", "SNOD", "FSHD", "FSSE", "SMTB", "GCOL"}
	for _, s := range sigs {
		fmt.Printf("  %-6s %d\n", s, bytes.Count(b, []byte(s)))
	}
	// CGNS node names live as plain strings; sample them
	for _, needle := range []string{"CGNSLibraryVersion", "Elements_t", "ElementStartOffset", "GridCoordinates", "ZoneBC", "BOUNDARY_", "INLAID_MESH", "SURFACE_TRIANGLES", "CoordinateX", "Zone_t", "Base_t"} {
		fmt.Printf("  str %-20s %d\n", needle, bytes.Count(b, []byte(needle)))
	}
}
