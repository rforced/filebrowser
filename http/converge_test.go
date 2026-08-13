package fbhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
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

		// log: *.log
		{"log file", "converge.log", "log", true},
		{"log with path-ish name", "job.12345.log", "log", true},
		{"log wrong extension", "converge.logs", "", false},

		// nfs: .nfs* — a prefix-only pattern, and the one that matches dotfiles.
		{"nfs stub", ".nfs00000000012a4b5c00000001", "nfs", true},
		{"nfs bare", ".nfs", "nfs", true},
		{"uppercase nfs stub", ".NFS0000ABCD", "nfs", true},
		{"nfs without leading dot", "nfs0000abcd", "", false},
		// ".nfs*" is taken literally, so anything under that prefix goes. Kept
		// as written rather than narrowed to the hex stubs NFS actually emits.
		{"anything under the nfs prefix", ".nfsrc", "nfs", true},

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

// convergeCase builds a case directory holding one of every output family, two
// outputs_* directories, and a pile of files that must survive.
//
// It returns the count of entries a cleanup should remove: each surface-level
// output file, plus each outputs_* directory as one entry.
func convergeCase(t *testing.T, dir string) int {
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

	// One of every output family, at the top level. 8 entries.
	write("run.echo")
	write("restart0100.rst")
	write("map_00001.h5")
	write("thermo.out")
	write("post00100.h5")
	write("post00100.cgns")
	write("converge.log")
	write(".nfs00000000012a4b5c00000001")

	// CONVERGE's own output directories. Each counts as a single entry, since
	// the whole tree goes — including the nested directory and the file inside
	// it that matches nothing.
	write("outputs_original", "post00200.h5")
	write("outputs_original", "thermo.out")
	write("outputs_original", "notes.txt")
	write("outputs_original", "nested", "post00300.h5")
	write("outputs_restart0100", "restart0200.rst")

	// An archive of a previous run: it holds matching names and an outputs_*
	// directory of its own, but it is not this case's surface level.
	write("archive", "run.echo")
	write("archive", "outputs_c", "thermo.out")

	return 8 + 2
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
	wantCount := convergeCase(t, caseDir)

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

	// 8 output files at the top level + the 2 outputs_* directories, each of
	// which counts once because the whole tree goes. Everything under archive/
	// is out of reach.
	if got.Count != wantCount {
		t.Errorf("Count = %d, want %d (groups: %+v)", got.Count, wantCount, got.Groups)
	}

	wantGroups := map[string]int{
		"echo": 1, "restart": 1, "map": 1, "out": 1, "post": 2,
		"log": 1, "nfs": 1, "outputs": 2,
	}
	gotGroups := map[string]int{}
	for _, g := range got.Groups {
		gotGroups[g.Kind] = g.Count
	}
	for kind, want := range wantGroups {
		if gotGroups[kind] != want {
			t.Errorf("group %q count = %d, want %d", kind, gotGroups[kind], want)
		}
	}

	// The outputs_* size covers the whole tree, including the file that matches
	// no pattern and the one in the nested directory: 5 files of one byte.
	for _, g := range got.Groups {
		if g.Kind == "outputs" && g.Size != 5 {
			t.Errorf("outputs group size = %d, want 5 (the whole tree)", g.Size)
		}
	}

	// Groups keep convergePatterns' order, with the directories last, so the
	// prompt is stable.
	var order []string
	for _, g := range got.Groups {
		order = append(order, g.Kind)
	}
	want := []string{"echo", "restart", "map", "out", "post", "log", "nfs", "outputs"}
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
	wantDeleted := convergeCase(t, caseDir)

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
	if got.Deleted != wantDeleted || got.Failed != 0 {
		t.Errorf("deleted = %d failed = %d, want %d and 0", got.Deleted, got.Failed, wantDeleted)
	}

	gone := []string{
		"run.echo",
		"restart0100.rst",
		"map_00001.h5",
		"thermo.out",
		"post00100.h5",
		"post00100.cgns",
		"converge.log",
		".nfs00000000012a4b5c00000001",
		// The whole tree goes, including what matched nothing on its own.
		"outputs_original",
		"outputs_original/notes.txt",
		"outputs_original/nested/post00300.h5",
		"outputs_restart0100",
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
		"archive/run.echo",
		"archive/outputs_c/thermo.out",
	}
	for _, rel := range kept {
		if _, err := os.Stat(filepath.Join(caseDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected %s to survive the cleanup: %v", rel, err)
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
	wantCount := convergeCase(t, caseDir)

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
	if scanned.Count != wantCount {
		t.Errorf("Count = %d, want %d — symlinks must not be swept", scanned.Count, wantCount)
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
	if _, err := os.Lstat(filepath.Join(caseDir, "outputs_link")); err != nil {
		t.Errorf("symlink named like an output directory was removed: %v", err)
	}
	// The whole-tree removal must not have followed the link out of scope.
	if _, err := os.Stat(filepath.Join(outside, "thermo.out")); err != nil {
		t.Errorf("VULNERABLE: cleanup reached a file outside the user's scope: %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("VULNERABLE: cleanup removed a directory outside the user's scope: %v", err)
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

	// Everything else still went, both output directories included.
	want := []string{"archive", "combust.in", "inputs.in", "surface.dat", "thermo.out"}
	if names := convergeSurvivors(t, caseDir); !slices.Equal(names, want) {
		t.Errorf("surviving entries = %v, want %v", names, want)
	}
}

// A recursive delete authorizes only its root, so a rule denying anything below
// an outputs_* directory has to keep the whole directory: removing the parent
// would otherwise be a way to delete what the rule protects.
func TestConvergeRuleInsideOutputDirKeepsWholeTree(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	convergeCase(t, caseDir)

	key := []byte("test-signing-key")
	st := denyRuleStorage(t, userScope, "/case/outputs_original/notes.txt", users.Permissions{Delete: true}, key)
	scan, clean, token := convergeHandlers(t, st)

	req, _ := http.NewRequest(http.MethodGet, "/api/converge/case", http.NoBody)
	req.Header.Set("X-Auth", token)
	rec := httptest.NewRecorder()
	scan.ServeHTTP(rec, req)

	var scanned convergeScanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &scanned); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// 8 files + only the one clean output directory.
	if scanned.Count != 9 {
		t.Errorf("Count = %d, want 9 (groups: %+v)", scanned.Count, scanned.Groups)
	}

	req, _ = http.NewRequest(http.MethodPost, "/api/converge/case", http.NoBody)
	req.Header.Set("X-Auth", token)
	rec = httptest.NewRecorder()
	clean.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	if _, err := os.Stat(filepath.Join(caseDir, "outputs_original", "notes.txt")); err != nil {
		t.Errorf("VULNERABLE: a rule-denied file was deleted with its parent: %v", err)
	}
	// The rest of that tree stays too — the directory was skipped whole.
	if _, err := os.Stat(filepath.Join(caseDir, "outputs_original", "post00200.h5")); err != nil {
		t.Errorf("expected the skipped output directory to be left intact: %v", err)
	}

	want := []string{"archive", "combust.in", "inputs.in", "outputs_original", "surface.dat"}
	if names := convergeSurvivors(t, caseDir); !slices.Equal(names, want) {
		t.Errorf("surviving entries = %v, want %v", names, want)
	}
}

func convergeSurvivors(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}
