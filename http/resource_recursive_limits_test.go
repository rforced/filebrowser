package fbhttp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListingDepth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		root, path string
		want       int
	}{
		{"/", "/", 0},
		{"/", "/a", 1},
		{"/", "/a/b", 2},
		{"/", "/a/b/c.txt", 3},
		{"/base", "/base", 0},
		{"/base", "/base/a", 1},
		{"/base", "/base/a/b", 2},
		// A trailing slash on the root must not shift the count.
		{"/base/", "/base/a", 1},
	}

	for _, tt := range tests {
		t.Run(tt.root+" -> "+tt.path, func(t *testing.T) {
			if got := listingDepth(tt.root, tt.path); got != tt.want {
				t.Errorf("listingDepth(%q, %q) = %d, want %d", tt.root, tt.path, got, tt.want)
			}
		})
	}
}

// requestRecursive walks userScope through the handler and decodes the response.
func requestRecursive(t *testing.T, userScope string) RecursiveListing {
	t.Helper()

	handler, token := recursiveTestHandler(t, userScope)
	req, err := http.NewRequest(http.MethodGet, "/api/resources/recursive/", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Auth", token)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	var listing RecursiveListing
	if err := json.Unmarshal(rec.Body.Bytes(), &listing); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return listing
}

// The walk stats every entry through the scoped filesystem and the whole result
// is marshalled into memory before anything is written, so an unbounded listing
// of a large tree is a long I/O burn and a large allocation that any
// authenticated user can ask for repeatedly.
func TestRecursiveListingStopsAtEntryLimit(t *testing.T) {
	original := recursiveMaxEntries
	recursiveMaxEntries = 5
	t.Cleanup(func() { recursiveMaxEntries = original })

	userScope := t.TempDir()
	for i := range 40 {
		name := filepath.Join(userScope, fmt.Sprintf("file%02d.txt", i))
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	listing := requestRecursive(t, userScope)

	if !listing.Truncated {
		t.Error("a listing cut short by the entry limit must report itself truncated")
	}
	if len(listing.Items) > recursiveMaxEntries {
		t.Errorf("returned %d entries, limit is %d", len(listing.Items), recursiveMaxEntries)
	}
}

// A single deep chain must not stretch the walk on its own. The directory at the
// limit is still listed — only its contents are refused — so the client sees
// that something is there.
func TestRecursiveListingStopsAtDepthLimit(t *testing.T) {
	original := recursiveMaxDepth
	recursiveMaxDepth = 3
	t.Cleanup(func() { recursiveMaxDepth = original })

	userScope := t.TempDir()
	deep := filepath.Join(userScope, "a", "b", "c", "d", "e")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "buried.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	listing := requestRecursive(t, userScope)

	if !listing.Truncated {
		t.Error("a listing cut short by the depth limit must report itself truncated")
	}

	for _, e := range listing.Items {
		if depth := listingDepth("/", e.Path); depth > recursiveMaxDepth {
			t.Errorf("entry %q is at depth %d, past the limit of %d", e.Path, depth, recursiveMaxDepth)
		}
		if strings.Contains(e.Path, "buried.txt") {
			t.Errorf("descended past the depth limit: %q", e.Path)
		}
	}

	// The directory sitting on the limit is still reported.
	var sawLimitDir bool
	for _, e := range listing.Items {
		if e.Path == "/a/b/c" {
			sawLimitDir = true
		}
	}
	if !sawLimitDir {
		t.Error("the directory at the depth limit should still be listed, only not descended into")
	}
}

// A tree comfortably inside both limits must not be flagged, or the client would
// discard a perfectly good listing.
func TestRecursiveListingWithinLimitsIsNotTruncated(t *testing.T) {
	userScope := t.TempDir()
	if err := os.MkdirAll(filepath.Join(userScope, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"a.txt", filepath.Join("sub", "b.txt")} {
		if err := os.WriteFile(filepath.Join(userScope, p), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	listing := requestRecursive(t, userScope)

	if listing.Truncated {
		t.Error("a small tree must not be reported as truncated")
	}
	if len(listing.Items) != 3 {
		t.Errorf("got %d entries, want 3: %+v", len(listing.Items), listing.Items)
	}
}
