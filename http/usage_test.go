package fbhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/users"
)

func usageHandler(t *testing.T, userScope string) (http.Handler, string) {
	t.Helper()

	st := scopedUserStorage(t, userScope,
		users.Permissions{Download: true}, []byte("test-signing-key"))
	return handle(usageBreakdownHandler, "/api/usage/breakdown", st, &settings.Server{}),
		issueToken(t, st)
}

func getBreakdown(t *testing.T, h http.Handler, token, target string) UsageBreakdown {
	t.Helper()

	req, _ := http.NewRequest(http.MethodGet, target, http.NoBody)
	req.Header.Set("X-Auth", token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	var got UsageBreakdown
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return got
}

// writeSized writes a file of n bytes, so a fixture can be made large enough
// that block rounding cannot reorder the rows under it.
func writeSized(t *testing.T, n int, parts ...string) {
	t.Helper()

	p := filepath.Join(parts...)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, n), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestUsageBreakdownRanksChildrenBySize(t *testing.T) {
	scope := t.TempDir()
	root := filepath.Join(scope, "root")

	// small/ is one file; big/ is spread over a nested tree, so the ranking can
	// only come out right if the whole subtree is folded into the row.
	writeSized(t, 64*1024, root, "big", "a.dat")
	writeSized(t, 64*1024, root, "big", "nested", "b.dat")
	writeSized(t, 8*1024, root, "small", "c.dat")
	writeSized(t, 32*1024, root, "loose.dat")

	h, token := usageHandler(t, scope)
	got := getBreakdown(t, h, token, "/api/usage/breakdown/root")

	if len(got.Children) != 3 {
		t.Fatalf("children = %+v, want big, small and loose.dat", got.Children)
	}

	wantOrder := []string{"big", "loose.dat", "small"}
	for i, name := range wantOrder {
		if got.Children[i].Name != name {
			t.Errorf("children[%d] = %q, want %q (all: %+v)", i, got.Children[i].Name, name, got.Children)
		}
	}

	big := got.Children[0]
	if big.LogicalSize != 128*1024 {
		t.Errorf("big logical size = %d, want %d", big.LogicalSize, 128*1024)
	}
	if big.NumFiles != 2 || big.NumDirs != 1 {
		t.Errorf("big counts = %d files / %d dirs, want 2 and 1", big.NumFiles, big.NumDirs)
	}
	// Allocated includes the directories' own blocks, so it is at least the
	// content and always block-aligned.
	if big.Size < big.LogicalSize {
		t.Errorf("big allocated %d < logical %d", big.Size, big.LogicalSize)
	}

	loose := got.Children[1]
	if loose.IsDir || loose.NumFiles != 1 || loose.LogicalSize != 32*1024 {
		t.Errorf("loose.dat = %+v, want a single 32KiB file", loose)
	}

	// The totals are the rows added up.
	var sum int64
	for _, c := range got.Children {
		sum += c.Size
	}
	if got.Size != sum {
		t.Errorf("total size = %d, want the sum of the rows %d", got.Size, sum)
	}
	if got.NumFiles != 4 {
		t.Errorf("total files = %d, want 4", got.NumFiles)
	}
	// big, big/nested, small
	if got.NumDirs != 3 {
		t.Errorf("total dirs = %d, want 3", got.NumDirs)
	}
}

// A link is not storage. Counting its target would bill this directory for
// bytes that live somewhere else, and double-count them for the filesystem.
func TestUsageBreakdownSkipsSymlinks(t *testing.T) {
	scope := t.TempDir()
	root := filepath.Join(scope, "root")

	writeSized(t, 16*1024, root, "real", "payload.dat")
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	h, token := usageHandler(t, scope)
	got := getBreakdown(t, h, token, "/api/usage/breakdown/root")

	if len(got.Children) != 1 || got.Children[0].Name != "real" {
		t.Fatalf("children = %+v, want only the real directory", got.Children)
	}
	if got.LogicalSize != 16*1024 {
		t.Errorf("logical size = %d, want the payload counted once", got.LogicalSize)
	}
}

func TestUsageBreakdownKindRollup(t *testing.T) {
	scope := t.TempDir()
	root := filepath.Join(scope, "root")

	// Two cases, each holding the same families, so the rollup has to cut
	// across the directory rows to be useful.
	writeSized(t, 32*1024, root, "case_a", "outputs_original", "thermo.out")
	writeSized(t, 64*1024, root, "case_a", "outputs_original", "post00100.h5")
	writeSized(t, 32*1024, root, "case_b", "outputs_original", "dynamic.out")
	writeSized(t, 8*1024, root, "case_b", "notes.txt")

	h, token := usageHandler(t, scope)

	plain := getBreakdown(t, h, token, "/api/usage/breakdown/root")
	if plain.Kinds != nil {
		t.Errorf("kinds = %+v, want them omitted unless asked for", plain.Kinds)
	}

	got := getBreakdown(t, h, token, "/api/usage/breakdown/root?kinds=true")

	kinds := map[string]UsageKind{}
	for _, k := range got.Kinds {
		kinds[k.Kind] = k
	}

	if k := kinds["out"]; k.Count != 2 || k.LogicalSize != 64*1024 {
		t.Errorf("out kind = %+v, want both .out files across the two cases", k)
	}
	if k := kinds["post"]; k.Count != 1 || k.LogicalSize != 64*1024 {
		t.Errorf("post kind = %+v, want the one h5", k)
	}
	// Anything unrecognized still has to be accounted for, or the rollup would
	// silently fail to add up to the total.
	if k := kinds["other"]; k.Count != 1 || k.LogicalSize != 8*1024 {
		t.Errorf("other kind = %+v, want notes.txt", k)
	}

	var kindTotal int64
	for _, k := range got.Kinds {
		kindTotal += k.LogicalSize
	}
	if kindTotal != got.LogicalSize {
		t.Errorf("kinds total %d != breakdown total %d", kindTotal, got.LogicalSize)
	}
}

func TestUsageBreakdownRejectsFiles(t *testing.T) {
	scope := t.TempDir()
	writeSized(t, 128, scope, "solo.dat")

	h, token := usageHandler(t, scope)

	req, _ := http.NewRequest(http.MethodGet, "/api/usage/breakdown/solo.dat", http.NoBody)
	req.Header.Set("X-Auth", token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a file", rec.Code)
	}
}
