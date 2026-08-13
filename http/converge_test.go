package fbhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/storage"
	"github.com/rforced/filebrowser/v2/users"
)

func TestConvergeOutputKind(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		wantKind string
		want     bool
	}{
		// echo: *.echo
		{"echo file", "run.echo", "echo", true},
		{"echo bare glob", "a.echo", "echo", true},

		// restart: restart*.rst
		{"restart numbered", "restart0100.rst", "restart", true},
		{"restart bare", "restart.rst", "restart", true},
		{"rst without restart prefix", "mesh.rst", "", false},
		{"restart wrong extension", "restart0100.dat", "", false},

		// map: map_*.h5
		{"map file", "map_00001.h5", "map", true},
		{"map without underscore", "map0001.h5", "", false},
		{"h5 that is neither map nor post", "results.h5", "", false},

		// out: *.out
		{"out file", "thermo.out", "out", true},

		// post: post*.h5 / post*.cgns
		{"post h5", "post00100.h5", "post", true},
		{"post cgns", "post00100.cgns", "post", true},
		{"post bare h5", "post.h5", "post", true},
		{"cgns without post prefix", "mesh.cgns", "", false},

		// Case-insensitive, since the same case can be served off a
		// case-insensitive mount.
		{"uppercase echo", "RUN.ECHO", "echo", true},
		{"mixed case restart", "Restart0100.RST", "restart", true},

		// Inputs and other case files must never match.
		{"input deck", "inputs.in", "", false},
		{"surface geometry", "surface.dat", "", false},
		{"empty name", "", "", false},

		// A glob does not match a dotfile unless the dot is written out.
		{"hidden out", ".out", "", false},
		{"hidden echo", ".echo", "", false},
		{"hidden but longer", ".hidden.out", "", false},

		// Suffix alone is not enough where the pattern has a literal prefix.
		{"suffix only rst", ".rst", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, ok := convergeOutputKind(tt.filename)
			if ok != tt.want || kind != tt.wantKind {
				t.Errorf("convergeOutputKind(%q) = (%q, %v), want (%q, %v)",
					tt.filename, kind, ok, tt.wantKind, tt.want)
			}
		})
	}
}

// convergeCase builds a case directory holding one of every output family, an
// outputs_* directory of its own, and a pile of files that must survive.
func convergeCase(t *testing.T, dir string) {
	t.Helper()

	write := func(parts ...string) {
		p := filepath.Join(append([]string{dir}, parts...)...)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// The deck that marks this a case directory, plus inputs that must stay.
	write("inputs.in")
	write("surface.dat")
	write("combust.in")

	// One of every output family, at the top level.
	write("run.echo")
	write("restart0100.rst")
	write("map_00001.h5")
	write("thermo.out")
	write("post00100.h5")
	write("post00100.cgns")

	// CONVERGE's own output directories, one level down.
	write("outputs_a", "post00200.h5")
	write("outputs_a", "thermo.out")
	write("outputs_b", "restart0200.rst")

	// A nested case and an archive of a previous run: both hold matching names
	// and must be left alone, since neither is this case's surface level.
	write("outputs_a", "nested", "post00300.h5")
	write("archive", "run.echo")
	write("archive", "outputs_c", "thermo.out")
}

func convergeHandlers(t *testing.T, st *storage.Storage) (scan, clean http.Handler, token string) {
	t.Helper()

	return handle(convergeScanHandler, "/api/converge", st, &settings.Server{}),
		handle(convergeCleanHandler, "/api/converge", st, &settings.Server{}),
		issueToken(t, st)
}

func convergeTestHandlers(t *testing.T, userScope string, perm users.Permissions) (scan, clean http.Handler, token string) {
	t.Helper()

	return convergeHandlers(t, scopedUserStorage(t, userScope, perm, []byte("test-signing-key")))
}

func TestConvergeScanIsShallow(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	convergeCase(t, caseDir)

	scan, _, token := convergeTestHandlers(t, userScope, users.Permissions{Delete: true})

	req, _ := http.NewRequest(http.MethodGet, "/api/converge/case", http.NoBody)
	req.Header.Set("X-Auth", token)
	rec := httptest.NewRecorder()
	scan.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	var got convergeScanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !got.IsCase {
		t.Fatal("expected the directory to be recognized as a CONVERGE case")
	}

	// 6 at the top level + 3 inside outputs_*. The nested directory under
	// outputs_a and everything under archive/ is out of reach.
	if got.Count != 9 {
		t.Errorf("Count = %d, want 9 (groups: %+v)", got.Count, got.Groups)
	}

	wantGroups := map[string]int{"echo": 1, "restart": 2, "map": 1, "out": 2, "post": 3}
	gotGroups := map[string]int{}
	for _, g := range got.Groups {
		gotGroups[g.Kind] = g.Count
	}
	for kind, want := range wantGroups {
		if gotGroups[kind] != want {
			t.Errorf("group %q count = %d, want %d", kind, gotGroups[kind], want)
		}
	}

	// Groups keep convergePatterns' order so the prompt is stable.
	var order []string
	for _, g := range got.Groups {
		order = append(order, g.Kind)
	}
	want := []string{"echo", "restart", "map", "out", "post"}
	for i := range want {
		if i >= len(order) || order[i] != want[i] {
			t.Errorf("group order = %v, want %v", order, want)
			break
		}
	}
}

func TestConvergeScanRejectsNonCaseDirectory(t *testing.T) {
	userScope := t.TempDir()
	plain := filepath.Join(userScope, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plain, "thermo.out"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	scan, clean, token := convergeTestHandlers(t, userScope, users.Permissions{Delete: true})

	req, _ := http.NewRequest(http.MethodGet, "/api/converge/plain", http.NoBody)
	req.Header.Set("X-Auth", token)
	rec := httptest.NewRecorder()
	scan.ServeHTTP(rec, req)

	var got convergeScanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got.IsCase || got.Count != 0 {
		t.Errorf("a directory without inputs.in reported as a case: %+v", got)
	}

	// And the cleanup refuses it outright, so the matching file survives even
	// if the endpoint is called directly.
	req, _ = http.NewRequest(http.MethodPost, "/api/converge/plain", http.NoBody)
	req.Header.Set("X-Auth", token)
	rec = httptest.NewRecorder()
	clean.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("clean status = %d, want 400", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(plain, "thermo.out")); err != nil {
		t.Errorf("VULNERABLE: cleaned a directory that is not a CONVERGE case: %v", err)
	}
}

func TestConvergeCleanDeletesOnlySurfaceOutputs(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	convergeCase(t, caseDir)

	_, clean, token := convergeTestHandlers(t, userScope, users.Permissions{Delete: true})

	req, _ := http.NewRequest(http.MethodPost, "/api/converge/case", http.NoBody)
	req.Header.Set("X-Auth", token)
	rec := httptest.NewRecorder()
	clean.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	var got convergeCleanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got.Deleted != 9 || got.Failed != 0 {
		t.Errorf("deleted = %d failed = %d, want 9 and 0", got.Deleted, got.Failed)
	}

	gone := []string{
		"run.echo",
		"restart0100.rst",
		"map_00001.h5",
		"thermo.out",
		"post00100.h5",
		"post00100.cgns",
		"outputs_a/post00200.h5",
		"outputs_a/thermo.out",
		"outputs_b/restart0200.rst",
	}
	for _, rel := range gone {
		if _, err := os.Stat(filepath.Join(caseDir, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted, stat err = %v", rel, err)
		}
	}

	kept := []string{
		"inputs.in",
		"surface.dat",
		"combust.in",
		"outputs_a/nested/post00300.h5",
		"archive/run.echo",
		"archive/outputs_c/thermo.out",
	}
	for _, rel := range kept {
		if _, err := os.Stat(filepath.Join(caseDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected %s to survive the cleanup: %v", rel, err)
		}
	}

	// The output directories themselves stay; only their contents go.
	for _, rel := range []string{"outputs_a", "outputs_b"} {
		info, err := os.Stat(filepath.Join(caseDir, rel))
		if err != nil || !info.IsDir() {
			t.Errorf("expected %s to remain a directory: %v", rel, err)
		}
	}
}

func TestConvergeRequiresDeletePermission(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	convergeCase(t, caseDir)

	// Everything but Delete.
	perm := users.Permissions{Create: true, Modify: true, Download: true, Rename: true, Share: true}
	scan, clean, token := convergeTestHandlers(t, userScope, perm)

	for name, handler := range map[string]http.Handler{"scan": scan, "clean": clean} {
		method := http.MethodGet
		if name == "clean" {
			method = http.MethodPost
		}

		req, _ := http.NewRequest(method, "/api/converge/case", http.NoBody)
		req.Header.Set("X-Auth", token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("%s status = %d, want 403", name, rec.Code)
		}
	}

	if _, err := os.Stat(filepath.Join(caseDir, "run.echo")); err != nil {
		t.Errorf("VULNERABLE: output deleted without the delete permission: %v", err)
	}
}

func TestConvergeSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	userScope := filepath.Join(root, "user")
	outside := filepath.Join(root, "outside")
	caseDir := filepath.Join(userScope, "case")
	convergeCase(t, caseDir)

	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "thermo.out"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A link named like an output, and a link named like an output directory,
	// both pointing outside the user's scope.
	if err := os.Symlink(filepath.Join(outside, "thermo.out"), filepath.Join(caseDir, "linked.out")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(caseDir, "outputs_link")); err != nil {
		t.Fatal(err)
	}

	scan, clean, token := convergeTestHandlers(t, userScope, users.Permissions{Delete: true})

	req, _ := http.NewRequest(http.MethodGet, "/api/converge/case", http.NoBody)
	req.Header.Set("X-Auth", token)
	rec := httptest.NewRecorder()
	scan.ServeHTTP(rec, req)

	var scanned convergeScanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &scanned); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if scanned.Count != 9 {
		t.Errorf("Count = %d, want 9 — symlinks must not be swept", scanned.Count)
	}

	req, _ = http.NewRequest(http.MethodPost, "/api/converge/case", http.NoBody)
	req.Header.Set("X-Auth", token)
	rec = httptest.NewRecorder()
	clean.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	if _, err := os.Lstat(filepath.Join(caseDir, "linked.out")); err != nil {
		t.Errorf("symlink named like an output was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "thermo.out")); err != nil {
		t.Errorf("VULNERABLE: cleanup reached a file outside the user's scope: %v", err)
	}
}

func TestConvergeHonorsRules(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	convergeCase(t, caseDir)

	key := []byte("test-signing-key")
	st := denyRuleStorage(t, userScope, "/case/thermo.out", users.Permissions{Delete: true}, key)
	_, clean, token := convergeHandlers(t, st)

	req, _ := http.NewRequest(http.MethodPost, "/api/converge/case", http.NoBody)
	req.Header.Set("X-Auth", token)
	rec := httptest.NewRecorder()
	clean.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	if _, err := os.Stat(filepath.Join(caseDir, "thermo.out")); err != nil {
		t.Errorf("VULNERABLE: a rule-denied file was deleted: %v", err)
	}

	// Everything else still went.
	survivors, err := os.ReadDir(caseDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range survivors {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	want := []string{"archive", "combust.in", "inputs.in", "outputs_a", "outputs_b", "surface.dat", "thermo.out"}
	if len(names) != len(want) {
		t.Errorf("surviving entries = %v, want %v", names, want)
	}
}
