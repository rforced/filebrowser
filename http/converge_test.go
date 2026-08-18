package fbhttp

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rforced/filebrowser/v2/files"
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
		{"parcel map is an output", "map_parcel_-1.201942e+02.h5", "map", true},

		// Inputs that live at a case root and share the output prefixes. These
		// are irreplaceable — map.h5 is the mapped initial condition a case was
		// seeded from, sl_table.h5 a precomputed lookup — and an outputs sweep
		// must never reach them. They are spared today only because the map
		// pattern requires the underscore, so pin it.
		{"mapped initial conditions are an input", "map.h5", "", false},
		{"laminar flame speed table is an input", "sl_table.h5", "", false},
		{"flamelet table is an input", "fgm_table.h5", "", false},

		// out: *.out
		{"out file", "thermo.out", "out", true},

		// log: *.log
		{"log file", "converge.log", "log", true},
		{"log with path-ish name", "job.12345.log", "log", true},
		{"log wrong extension", "converge.logs", "", false},

		// run: horizon.json / hosts, matched whole.
		{"horizon", "horizon.json", "run", true},
		{"hosts", "hosts", "run", true},
		{"uppercase hosts", "HOSTS", "run", true},
		{"mixed case horizon", "Horizon.JSON", "run", true},
		{"copy of hosts", "hosts.bak", "", false},
		{"hosts with a suffix in the name", "hostsfile", "", false},
		{"horizon without the extension", "horizon", "", false},
		{"another json", "settings.json", "", false},

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
	// Named close enough to the run files to be swept by a sloppy match.
	write("hosts.bak")

	// One of every output family, at the top level. 10 entries.
	write("run.echo")
	write("restart0100.rst")
	write("map_00001.h5")
	write("thermo.out")
	write("post00100.h5")
	write("post00100.cgns")
	write("converge.log")
	write("horizon.json")
	write("hosts")
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

	return 10 + 2
}

// allocatedOf sums what the named files occupy on disk. The scan reports
// allocated rather than logical size, so a fixture's expected total depends on
// the host filesystem's block size and has to be measured rather than written
// down — a one-byte file costs a whole block on ext4 and nothing at all on a
// filesystem that inlines small extents. Enumerating the paths independently
// still catches both a miscount and a regression to info.Size().
func allocatedOf(t *testing.T, dir string, rel ...string) int64 {
	t.Helper()

	var total int64
	for _, r := range rel {
		info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(r)))
		if err != nil {
			t.Fatalf("stat %s: %v", r, err)
		}
		total += files.AllocatedSize(info)
	}
	return total
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

// TestConvergeRestartOrderTiebreak covers restarts that share an mtime, which
// is what a case restored from an archive looks like. The name is then all
// there is to order by, and comparing it lexically puts restart2 above
// restart0010 — the wrong leg to resubmit from.
func TestConvergeRestartOrderTiebreak(t *testing.T) {
	stamp := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	matches := []convergeMatch{
		{path: "/case/restart2.rst", kind: "restart", modTime: stamp},
		{path: "/case/restart0010.rst", kind: "restart", modTime: stamp},
		{path: "/case/restart9.rst", kind: "restart", modTime: stamp},
		// No digits at all: CONVERGE writes this one for the latest restart,
		// and it sorts below the numbered ones rather than above them.
		{path: "/case/restart.rst", kind: "restart", modTime: stamp},
	}

	var names []string
	for _, r := range convergeRestartsFromMatches(matches) {
		names = append(names, r.Name)
	}
	want := "restart0010.rst,restart9.rst,restart2.rst,restart.rst"
	if got := strings.Join(names, ","); got != want {
		t.Errorf("restart order = %s, want %s", got, want)
	}
}

func TestConvergeScanCountsThroughOutputDirs(t *testing.T) {
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

	// Count prices the full sweep: 10 root files + the 2 outputs_* trees taken
	// whole. Everything under archive/ is out of reach.
	if got.Count != wantCount {
		t.Errorf("Count = %d, want %d (groups: %+v)", got.Count, wantCount, got.Groups)
	}

	// Kind tallies see through the outputs_* directories; the root share is
	// reported alongside so the prompt can subtract what the folders subsume.
	wantGroups := map[string][2]int{
		"echo": {1, 1}, "restart": {2, 1}, "map": {1, 1}, "out": {2, 1},
		"post": {4, 2}, "log": {1, 1}, "run": {2, 2}, "nfs": {1, 1},
	}
	gotGroups := map[string]convergeGroup{}
	for _, g := range got.Groups {
		gotGroups[g.Kind] = g
	}
	for kind, want := range wantGroups {
		g := gotGroups[kind]
		if g.Count != want[0] || g.RootCount != want[1] {
			t.Errorf("group %q = count %d root %d, want count %d root %d",
				kind, g.Count, g.RootCount, want[0], want[1])
		}
	}

	// The outputs row is the directories as units, priced whole: all 5 files
	// across both trees, the unmatched notes.txt included.
	wantOutputs := allocatedOf(t, caseDir,
		"outputs_original/post00200.h5",
		"outputs_original/thermo.out",
		"outputs_original/notes.txt",
		"outputs_original/nested/post00300.h5",
		"outputs_restart0100/restart0200.rst",
	)
	if g := gotGroups["outputs"]; g.Count != 2 || g.Size != wantOutputs {
		t.Errorf("outputs group = %+v, want 2 dirs totalling %d bytes", g, wantOutputs)
	}

	// Both restarts are offered to the keep-newest picker, the nested one too.
	if len(got.Restarts) != 2 {
		t.Errorf("restarts = %+v, want the root and the nested restart", got.Restarts)
	}

	// Groups keep convergePatterns' order, with the directories last, so the
	// prompt is stable.
	var order []string
	for _, g := range got.Groups {
		order = append(order, g.Kind)
	}
	want := []string{"echo", "restart", "map", "out", "post", "log", "run", "nfs", "outputs"}
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
		"horizon.json",
		"hosts",
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
		"hosts.bak",
		"archive/run.echo",
		"archive/outputs_c/thermo.out",
	}
	for _, rel := range kept {
		if _, err := os.Stat(filepath.Join(caseDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected %s to survive the cleanup: %v", rel, err)
		}
	}
}

// TestConvergeCleanSparesCaseInputs guards the irreplaceable inputs that share
// a prefix with output kinds. map.h5 is the mapped initial condition a case was
// seeded from — often the only copy, and tens of megabytes of a previous
// solve — while *_table.h5 files are precomputed lookups the deck needs to run
// at all. Neither can be regenerated by rerunning the case, and both sit at the
// case root next to the outputs. Only the underscore in the map_ pattern keeps
// them out of a sweep today, so assert the behaviour and not just the pattern.
func TestConvergeCleanSparesCaseInputs(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	convergeCase(t, caseDir)

	inputs := []string{"map.h5", "sl_table.h5", "fgm_table.h5"}
	for _, name := range inputs {
		if err := os.WriteFile(filepath.Join(caseDir, name), []byte("initial conditions"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	_, clean, token := convergeTestHandlers(t, userScope, users.Permissions{Delete: true})

	// Empty body is a full sweep: the broadest thing a user can ask for.
	req, _ := http.NewRequest(http.MethodPost, "/api/converge/case", http.NoBody)
	req.Header.Set("X-Auth", token)
	rec := httptest.NewRecorder()
	clean.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	for _, name := range inputs {
		if _, err := os.Stat(filepath.Join(caseDir, name)); err != nil {
			t.Errorf("%s is a case input and must survive a full sweep: %v", name, err)
		}
	}

	// The output that does share the prefix must still be swept, so the guard
	// above cannot be satisfied by simply never matching map files.
	if _, err := os.Stat(filepath.Join(caseDir, "map_00001.h5")); !os.IsNotExist(err) {
		t.Errorf("map_00001.h5 is an output and should have been deleted, stat err = %v", err)
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
	want := []string{"archive", "combust.in", "hosts.bak", "inputs.in", "surface.dat", "thermo.out"}
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
	// 10 files + only the one clean output directory.
	if scanned.Count != 11 {
		t.Errorf("Count = %d, want 11 (groups: %+v)", scanned.Count, scanned.Groups)
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

	want := []string{"archive", "combust.in", "hosts.bak", "inputs.in", "outputs_original", "surface.dat"}
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

func writeCaseFile(t *testing.T, dir string, content string, parts ...string) string {
	t.Helper()

	p := filepath.Join(append([]string{dir}, parts...)...)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func getConvergeSummary(t *testing.T, scan http.Handler, token, target string) convergeSummaryResponse {
	t.Helper()

	req, _ := http.NewRequest(http.MethodGet, target+"?summary=true", http.NoBody)
	req.Header.Set("X-Auth", token)
	rec := httptest.NewRecorder()
	scan.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("summary status = %d body = %q", rec.Code, rec.Body.String())
	}

	var got convergeSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode summary: %v", err)
	}
	return got
}

// The summary only reports; it must be reachable without the delete
// permission that gates the clean preview.
func TestConvergeSummaryNeedsOnlyReadAccess(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	convergeCase(t, caseDir)

	scan, _, token := convergeTestHandlers(t, userScope, users.Permissions{Download: true})

	got := getConvergeSummary(t, scan, token, "/api/converge/case")
	if !got.IsCase {
		t.Error("summary did not recognize the case directory")
	}

	// Without ?summary the same GET is still the clean preview, still gated.
	req, _ := http.NewRequest(http.MethodGet, "/api/converge/case", http.NoBody)
	req.Header.Set("X-Auth", token)
	rec := httptest.NewRecorder()
	scan.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("clean preview without delete permission = %d, want 403", rec.Code)
	}
}

func TestConvergeSummaryClassifiesRunTrees(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")

	writeCaseFile(t, caseDir, "deck", "inputs.in")
	writeCaseFile(t, caseDir, "x", "run.echo")

	// A CONVERGE 6 run tree: streams and 3D output live below outputs_*.
	writeCaseFile(t, caseDir, "", "outputs_original", "converge.start")
	writeCaseFile(t, caseDir, "", "outputs_original", "converge.done")
	writeCaseFile(t, caseDir, "log line", "outputs_original", "converge.log")
	writeCaseFile(t, caseDir, "12345", "outputs_original", "restart0001.rst")
	writeCaseFile(t, caseDir, "h5", "outputs_original", "output", "post000001_-4.81000e+02.h5")
	writeCaseFile(t, caseDir, "cols", "outputs_original", "stream0", "dynamic.out")
	writeCaseFile(t, caseDir, "png", "outputs_original", "paraview_catalyst", "images", "slice1_000_000009.png")

	scan, _, token := convergeTestHandlers(t, userScope, users.Permissions{Delete: true})
	got := getConvergeSummary(t, scan, token, "/api/converge/case")

	if !got.IsCase || got.Status != "completed" {
		t.Errorf("isCase = %v status = %q, want a completed case", got.IsCase, got.Status)
	}

	wantGroups := map[string]int{"echo": 1, "restart": 1, "post": 1, "out": 1, "log": 1, "other": 1}
	gotGroups := map[string]int{}
	for _, g := range got.Groups {
		gotGroups[g.Kind] = g.Count
	}
	for kind, want := range wantGroups {
		if gotGroups[kind] != want {
			t.Errorf("group %q count = %d, want %d (groups %+v)", kind, gotGroups[kind], want, got.Groups)
		}
	}

	// The markers are bookkeeping, not output to report.
	if gotGroups["run"] != 0 {
		t.Errorf("markers were tallied as output: %+v", got.Groups)
	}

	wantRestart := allocatedOf(t, caseDir, "outputs_original/restart0001.rst")
	if len(got.Restarts) != 1 || got.Restarts[0].Name != "restart0001.rst" ||
		got.Restarts[0].Size != wantRestart {
		t.Errorf("restarts = %+v, want the one restart file", got.Restarts)
	}
	if got.LogPath != "/case/outputs_original/converge.log" {
		t.Errorf("logPath = %q", got.LogPath)
	}
}

func TestConvergeSummaryStatuses(t *testing.T) {
	old := time.Now().Add(-2 * time.Hour)

	tests := []struct {
		name  string
		build func(t *testing.T, caseDir string)
		want  string
	}{
		{
			name:  "idle without markers",
			build: func(_ *testing.T, _ string) {},
			want:  "idle",
		},
		{
			name: "running while streams are fresh",
			build: func(t *testing.T, caseDir string) {
				writeCaseFile(t, caseDir, "", "outputs_1", "converge.start")
				writeCaseFile(t, caseDir, "data", "outputs_1", "stream0", "thermo.out")
			},
			want: "running",
		},
		{
			name: "interrupted once the streams go quiet",
			build: func(t *testing.T, caseDir string) {
				start := writeCaseFile(t, caseDir, "", "outputs_1", "converge.start")
				out := writeCaseFile(t, caseDir, "data", "outputs_1", "stream0", "thermo.out")
				for _, p := range []string{start, out} {
					if err := os.Chtimes(p, old, old); err != nil {
						t.Fatal(err)
					}
				}
			},
			want: "interrupted",
		},
		{
			name: "completed regardless of age",
			build: func(t *testing.T, caseDir string) {
				start := writeCaseFile(t, caseDir, "", "outputs_1", "converge.start")
				done := writeCaseFile(t, caseDir, "", "outputs_1", "converge.done")
				for _, p := range []string{start, done} {
					if err := os.Chtimes(p, old, old); err != nil {
						t.Fatal(err)
					}
				}
			},
			want: "completed",
		},
		{
			name: "newest run speaks for the case",
			build: func(t *testing.T, caseDir string) {
				start := writeCaseFile(t, caseDir, "", "outputs_1", "converge.start")
				done := writeCaseFile(t, caseDir, "", "outputs_1", "converge.done")
				for _, p := range []string{start, done} {
					if err := os.Chtimes(p, old, old); err != nil {
						t.Fatal(err)
					}
				}
				writeCaseFile(t, caseDir, "", "outputs_2", "converge.start")
				writeCaseFile(t, caseDir, "data", "outputs_2", "stream0", "thermo.out")
			},
			want: "running",
		},
		{
			name: "root run layout",
			build: func(t *testing.T, caseDir string) {
				writeCaseFile(t, caseDir, "", "converge.start")
				writeCaseFile(t, caseDir, "data", "stream0", "thermo.out")
			},
			want: "running",
		},

		{
			name: "completed via log epilogue without markers",
			build: func(t *testing.T, caseDir string) {
				log := writeCaseFile(t, caseDir,
					"solver chatter\nnormal termination\nProgram used 8092.551994 seconds.\n",
					"outputs_1", "converge.log")
				if err := os.Chtimes(log, old, old); err != nil {
					t.Fatal(err)
				}
			},
			want: "completed",
		},
		{
			name: "running from stream writes alone",
			build: func(t *testing.T, caseDir string) {
				writeCaseFile(t, caseDir, "data", "stream0", "thermo.out")
			},
			want: "running",
		},
		{
			name: "interrupted when the log just stops",
			build: func(t *testing.T, caseDir string) {
				log := writeCaseFile(t, caseDir,
					"solver chatter\n   time=   -2.672e-02, crank=   -4.809e+02, dt=    5.000e-07\n",
					"outputs_1", "converge.log")
				if err := os.Chtimes(log, old, old); err != nil {
					t.Fatal(err)
				}
			},
			want: "interrupted",
		},
		{
			name: "stale streams with no log or markers stay unjudged",
			build: func(t *testing.T, caseDir string) {
				out := writeCaseFile(t, caseDir, "data", "stream0", "thermo.out")
				if err := os.Chtimes(out, old, old); err != nil {
					t.Fatal(err)
				}
			},
			want: "idle",
		},
		{
			name: "done without start",
			build: func(t *testing.T, caseDir string) {
				writeCaseFile(t, caseDir, "", "outputs_1", "converge.done")
			},
			want: "completed",
		},
		{
			name: "newer marker-less crash outweighs an old completed run",
			build: func(t *testing.T, caseDir string) {
				older := old.Add(-time.Hour)
				for _, name := range []string{"converge.start", "converge.done"} {
					p := writeCaseFile(t, caseDir, "", "outputs_1", name)
					if err := os.Chtimes(p, older, older); err != nil {
						t.Fatal(err)
					}
				}
				log := writeCaseFile(t, caseDir, "still going\n", "outputs_2", "converge.log")
				if err := os.Chtimes(log, old, old); err != nil {
					t.Fatal(err)
				}
			},
			want: "interrupted",
		},

		// Horizon's own job files land at the case root after the solver's last
		// write; they must not pose as a newer run without a verdict.
		{
			name: "job log at root does not eclipse the finished run",
			build: func(t *testing.T, caseDir string) {
				for _, name := range []string{"converge.start", "converge.done"} {
					p := writeCaseFile(t, caseDir, "", "outputs_original", name)
					if err := os.Chtimes(p, old, old); err != nil {
						t.Fatal(err)
					}
				}
				writeCaseFile(t, caseDir, "job teardown chatter", "horizon.log")
				writeCaseFile(t, caseDir, "mech ok", "mech_check.out")
			},
			want: "completed",
		},
		{
			name: "restart run supersedes the original run",
			build: func(t *testing.T, caseDir string) {
				older := old.Add(-time.Hour)
				for _, name := range []string{"converge.start", "converge.done"} {
					p := writeCaseFile(t, caseDir, "", "outputs_original", name)
					if err := os.Chtimes(p, older, older); err != nil {
						t.Fatal(err)
					}
				}
				start := writeCaseFile(t, caseDir, "", "outputs_restart1", "converge.start")
				out := writeCaseFile(t, caseDir, "data", "outputs_restart1", "stream0", "thermo.out")
				for _, p := range []string{start, out} {
					if err := os.Chtimes(p, old, old); err != nil {
						t.Fatal(err)
					}
				}
			},
			want: "interrupted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userScope := t.TempDir()
			caseDir := filepath.Join(userScope, "case")
			writeCaseFile(t, caseDir, "deck", "inputs.in")
			tt.build(t, caseDir)

			scan, _, token := convergeTestHandlers(t, userScope, users.Permissions{Delete: true})
			got := getConvergeSummary(t, scan, token, "/api/converge/case")
			if got.Status != tt.want {
				t.Errorf("status = %q, want %q", got.Status, tt.want)
			}
		})
	}
}

func TestConvergeSummaryJobAndProgress(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")

	writeCaseFile(t, caseDir, `version: 6
simulation_control:
   crank_flag:            1                # engine run in crank angle degrees
   start_time:            -481             # Start time
   end_time:              860              # End time
`, "inputs.in")

	writeCaseFile(t, caseDir, `{
  "id": "h35iv4se2qfi",
  "name": "Test",
  "app_key": "converge",
  "app_version": "6.0.1",
  "cores_per_node": 32,
  "nodes_count": 2
}`, "horizon.json")

	writeCaseFile(t, caseDir, `header
   time=   -2.672172222e-02, crank=   -4.809910000e+02, dt=    5.000000000e-07
Ustar iterations= 1 residual= 5.5901e-01
   time=   -2.601000000e-02, crank=   -1.431106000e+02, dt=    5.000000000e-07
trailing chatter
`, "outputs_original", "converge.log")
	writeCaseFile(t, caseDir, "", "outputs_original", "converge.start")

	scan, _, token := convergeTestHandlers(t, userScope, users.Permissions{Delete: true})
	got := getConvergeSummary(t, scan, token, "/api/converge/case")

	if got.Job == nil {
		t.Fatal("job info missing")
	}
	if got.Job.Name != "Test" || got.Job.AppVersion != "6.0.1" || got.Job.NodesCount != 2 || got.Job.CoresPerNode != 32 {
		t.Errorf("job = %+v", got.Job)
	}

	if got.Progress == nil {
		t.Fatal("progress missing")
	}
	if got.Progress.Unit != "deg" {
		t.Errorf("unit = %q, want deg", got.Progress.Unit)
	}
	if got.Progress.Current != -143.1106 {
		t.Errorf("current = %v, want -143.1106", got.Progress.Current)
	}
	if got.Progress.Start == nil || *got.Progress.Start != -481 {
		t.Errorf("start = %v, want -481", got.Progress.Start)
	}
	if got.Progress.End == nil || *got.Progress.End != 860 {
		t.Errorf("end = %v, want 860", got.Progress.End)
	}
}

func TestConvergeCleanSelectiveKinds(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	convergeCase(t, caseDir)

	_, clean, token := convergeTestHandlers(t, userScope, users.Permissions{Delete: true})

	body := strings.NewReader(`{"kinds":["echo","log"]}`)
	req, _ := http.NewRequest(http.MethodPost, "/api/converge/case", body)
	req.Header.Set("X-Auth", token)
	rec := httptest.NewRecorder()
	clean.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	var got convergeCleanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Deleted != 2 || got.Failed != 0 {
		t.Errorf("deleted = %d failed = %d, want 2 and 0", got.Deleted, got.Failed)
	}

	for _, rel := range []string{"run.echo", "converge.log"} {
		if _, err := os.Stat(filepath.Join(caseDir, rel)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted", rel)
		}
	}
	// Everything outside the two selected families stays, outputs_* included.
	for _, rel := range []string{
		"restart0100.rst", "map_00001.h5", "thermo.out", "post00100.h5",
		"horizon.json", "hosts", "outputs_original", "outputs_restart0100",
	} {
		if _, err := os.Stat(filepath.Join(caseDir, rel)); err != nil {
			t.Errorf("expected %s to survive a selective clean: %v", rel, err)
		}
	}
}

func TestConvergeCleanKeepsNewestRestarts(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	writeCaseFile(t, caseDir, "deck", "inputs.in")

	now := time.Now()
	for i, name := range []string{"restart0001.rst", "restart0002.rst", "restart0003.rst"} {
		p := writeCaseFile(t, caseDir, "data", name)
		mod := now.Add(time.Duration(i-3) * time.Hour)
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatal(err)
		}
	}

	_, clean, token := convergeTestHandlers(t, userScope, users.Permissions{Delete: true})

	body := strings.NewReader(`{"kinds":["restart"],"keepRestarts":2}`)
	req, _ := http.NewRequest(http.MethodPost, "/api/converge/case", body)
	req.Header.Set("X-Auth", token)
	rec := httptest.NewRecorder()
	clean.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	var got convergeCleanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Deleted != 1 {
		t.Errorf("deleted = %d, want 1", got.Deleted)
	}

	if _, err := os.Stat(filepath.Join(caseDir, "restart0001.rst")); !os.IsNotExist(err) {
		t.Error("expected the oldest restart to be deleted")
	}
	for _, rel := range []string{"restart0002.rst", "restart0003.rst"} {
		if _, err := os.Stat(filepath.Join(caseDir, rel)); err != nil {
			t.Errorf("expected %s to be kept: %v", rel, err)
		}
	}
}

func convergeCleanRequestJSON(t *testing.T, clean http.Handler, token, body string) convergeCleanResponse {
	t.Helper()

	req, _ := http.NewRequest(http.MethodPost, "/api/converge/case", strings.NewReader(body))
	req.Header.Set("X-Auth", token)
	rec := httptest.NewRecorder()
	clean.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	var got convergeCleanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

// A kind selected without "outputs" reaches matching files inside the
// outputs_* trees while the trees themselves stay.
func TestConvergeCleanKindsReachInsideOutputDirs(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	convergeCase(t, caseDir)

	_, clean, token := convergeTestHandlers(t, userScope, users.Permissions{Delete: true})
	got := convergeCleanRequestJSON(t, clean, token, `{"kinds":["post"]}`)

	if got.Deleted != 4 || got.Failed != 0 {
		t.Errorf("deleted = %d failed = %d, want 4 and 0", got.Deleted, got.Failed)
	}

	for _, rel := range []string{
		"post00100.h5", "post00100.cgns",
		"outputs_original/post00200.h5", "outputs_original/nested/post00300.h5",
	} {
		if _, err := os.Stat(filepath.Join(caseDir, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted", rel)
		}
	}
	for _, rel := range []string{
		"outputs_original", "outputs_original/thermo.out", "outputs_original/notes.txt",
		"outputs_restart0100/restart0200.rst", "thermo.out", "restart0100.rst",
	} {
		if _, err := os.Stat(filepath.Join(caseDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected %s to survive: %v", rel, err)
		}
	}
}

// Selecting the outputs folders takes them whole; other selected kinds then
// only reach their root-level files.
func TestConvergeCleanOutputDirsSubsumeDeepFiles(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	convergeCase(t, caseDir)

	_, clean, token := convergeTestHandlers(t, userScope, users.Permissions{Delete: true})
	got := convergeCleanRequestJSON(t, clean, token, `{"kinds":["outputs","post"]}`)

	// The two trees whole plus the two root post files.
	if got.Deleted != 4 || got.Failed != 0 {
		t.Errorf("deleted = %d failed = %d, want 4 and 0", got.Deleted, got.Failed)
	}

	for _, rel := range []string{
		"post00100.h5", "post00100.cgns", "outputs_original", "outputs_restart0100",
	} {
		if _, err := os.Stat(filepath.Join(caseDir, rel)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted", rel)
		}
	}
	for _, rel := range []string{"thermo.out", "restart0100.rst", "converge.log"} {
		if _, err := os.Stat(filepath.Join(caseDir, rel)); err != nil {
			t.Errorf("expected %s to survive: %v", rel, err)
		}
	}
}

// keep-newest ranks root and nested restarts together.
func TestConvergeCleanKeepRestartsSpansOutputDirs(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	writeCaseFile(t, caseDir, "deck", "inputs.in")

	now := time.Now()
	paths := []string{
		"restart0001.rst",
		"outputs_original/restart0002.rst",
		"outputs_original/restart0003.rst",
	}
	for i, rel := range paths {
		p := writeCaseFile(t, caseDir, "data", strings.Split(rel, "/")...)
		mod := now.Add(time.Duration(i-3) * time.Hour)
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatal(err)
		}
	}

	_, clean, token := convergeTestHandlers(t, userScope, users.Permissions{Delete: true})
	got := convergeCleanRequestJSON(t, clean, token, `{"kinds":["restart"],"keepRestarts":2}`)

	if got.Deleted != 1 {
		t.Errorf("deleted = %d, want 1 (the oldest, at the root)", got.Deleted)
	}
	if _, err := os.Stat(filepath.Join(caseDir, "restart0001.rst")); !os.IsNotExist(err) {
		t.Error("expected the oldest root restart to be deleted")
	}
	for _, rel := range paths[1:] {
		if _, err := os.Stat(filepath.Join(caseDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected %s to be kept: %v", rel, err)
		}
	}
}

// A symlink inside an outputs_* tree is not a file a kind selection may
// unlink; only taking the folder whole removes it.
func TestConvergeCleanDeepSymlinksSkipped(t *testing.T) {
	root := t.TempDir()
	userScope := filepath.Join(root, "user")
	outside := filepath.Join(root, "outside")
	caseDir := filepath.Join(userScope, "case")

	writeCaseFile(t, caseDir, "deck", "inputs.in")
	writeCaseFile(t, caseDir, "data", "outputs_original", "thermo.out")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.out"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.out"),
		filepath.Join(caseDir, "outputs_original", "linked.out")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, clean, token := convergeTestHandlers(t, userScope, users.Permissions{Delete: true})
	got := convergeCleanRequestJSON(t, clean, token, `{"kinds":["out"]}`)

	if got.Deleted != 1 {
		t.Errorf("deleted = %d, want only the real thermo.out", got.Deleted)
	}
	if _, err := os.Lstat(filepath.Join(caseDir, "outputs_original", "linked.out")); err != nil {
		t.Errorf("a symlink inside the tree was removed by a kind selection: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "secret.out")); err != nil {
		t.Errorf("VULNERABLE: the link target was touched: %v", err)
	}
}

func TestConvergeCleanRejectsBadRequests(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	convergeCase(t, caseDir)

	_, clean, token := convergeTestHandlers(t, userScope, users.Permissions{Delete: true})

	for name, body := range map[string]string{
		"unknown kind":          `{"kinds":["deck"]}`,
		"negative keepRestarts": `{"keepRestarts":-1}`,
		"malformed json":        `{"kinds":`,
	} {
		req, _ := http.NewRequest(http.MethodPost, "/api/converge/case", strings.NewReader(body))
		req.Header.Set("X-Auth", token)
		rec := httptest.NewRecorder()
		clean.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}

	// Nothing may have been swept on the way to a rejection.
	if _, err := os.Stat(filepath.Join(caseDir, "run.echo")); err != nil {
		t.Errorf("a rejected request deleted output anyway: %v", err)
	}
}

// convergeChain builds a three-leg restart chain the way CONVERGE 6 leaves one
// behind: a directory per launch, each holding only the slice of the solve it
// ran, and a deck whose end_time is still out ahead of the newest leg.
func convergeChain(t *testing.T, caseDir, endTime string) {
	t.Helper()

	writeCaseFile(t, caseDir, `
   crank_flag:            1
   start_time:            360.015
   end_time:              `+endTime+`
`, "inputs.in")

	legs := []struct {
		dir string
		end string
		age time.Duration
	}{
		{"outputs_original", "1.800108700e+03", 48 * time.Hour},
		{"outputs_restart1", "3.600015244e+03", 24 * time.Hour},
		{"outputs_restart2", "4.016760984e+03", 2 * time.Hour},
	}

	for _, leg := range legs {
		logLine := "   time=    2.307831667e-01, crank=    " + leg.end +
			", dt=    1.405163090e-05, time-step limit = dt_cfl \nnormal termination\n"

		written := []string{
			writeCaseFile(t, caseDir, "", leg.dir, "converge.start"),
			writeCaseFile(t, caseDir, "", leg.dir, "converge.done"),
			writeCaseFile(t, caseDir, logLine, leg.dir, "converge.log"),
			writeCaseFile(t, caseDir, "rst", leg.dir, "restart0001.rst"),
			writeCaseFile(t, caseDir, "cols", leg.dir, "stream0", "thermo.out"),
		}

		// The directory goes last: writing into it moves its mtime, and that
		// mtime is what orders a leg whose files have all been swept.
		stamp := time.Now().Add(-leg.age)
		for _, p := range append(written, filepath.Join(caseDir, leg.dir)) {
			if err := os.Chtimes(p, stamp, stamp); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestConvergeNeedsRestart(t *testing.T) {
	at := func(v float64) *float64 { return &v }

	tests := []struct {
		name                string
		status              string
		current, start, end *float64
		want                bool
	}{
		{"cut off short of end_time", "completed", at(4016.76), at(360.015), at(7200), true},
		{"reached end_time", "completed", at(7200.01), at(360.015), at(7200), false},
		// CONVERGE writes the last row at or just past end_time; a rounding
		// tick under it is still a finished solve.
		{"a hair under end_time", "completed", at(7199.9999), at(360.015), at(7200), false},
		{"no start in the deck", "completed", at(500), nil, at(7200), true},
		{"still running", "running", at(4016.76), at(360.015), at(7200), false},
		{"interrupted stays interrupted", "interrupted", at(4016.76), at(360.015), at(7200), false},
		{"no end in the deck", "completed", at(4016.76), at(360.015), nil, false},
		{"no progress read", "completed", nil, at(360.015), at(7200), false},
		{"degenerate span", "completed", at(10), at(7200), at(7200), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convergeNeedsRestart(tt.status, tt.current, tt.start, tt.end)
			if got != tt.want {
				t.Errorf("convergeNeedsRestart = %v, want %v", got, tt.want)
			}
		})
	}
}

// A leg that hit its wall-clock limit writes converge.done and "normal
// termination" just like a finished solve. The deck is what tells them apart.
func TestConvergeSummaryStatusAcrossChain(t *testing.T) {
	for _, tt := range []struct {
		name    string
		endTime string
		want    string
	}{
		{"deck still asks for more", "7200", "needsRestart"},
		{"newest leg reached end_time", "4016.76", "completed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			userScope := t.TempDir()
			caseDir := filepath.Join(userScope, "case")
			convergeChain(t, caseDir, tt.endTime)

			scan, _, token := convergeTestHandlers(t, userScope, users.Permissions{Delete: true})
			got := getConvergeSummary(t, scan, token, "/api/converge/case")

			if got.Status != tt.want {
				t.Errorf("status = %q, want %q", got.Status, tt.want)
			}
			if got.LogPath != "/case/outputs_restart2/converge.log" {
				t.Errorf("LogPath = %q, want the newest leg's log", got.LogPath)
			}
			if got.Progress == nil || got.Progress.Current != 4016.760984 {
				t.Errorf("Progress = %+v, want the newest leg's crank", got.Progress)
			}
		})
	}
}

func TestConvergeSummaryRunChain(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	convergeChain(t, caseDir, "7200")

	scan, _, token := convergeTestHandlers(t, userScope, users.Permissions{Download: true})
	got := getConvergeSummary(t, scan, token, "/api/converge/case")

	// Newest first, the same order Restarts uses and the order keepRuns counts
	// in. Each leg reports where it stopped; the one before it says where it
	// started.
	wantNames := []string{"outputs_restart2", "outputs_restart1", "outputs_original"}
	wantEnds := []float64{4016.760984, 3600.015244, 1800.1087}

	if len(got.Runs) != len(wantNames) {
		t.Fatalf("Runs = %+v, want %d legs", got.Runs, len(wantNames))
	}

	for i, run := range got.Runs {
		if run.Name != wantNames[i] {
			t.Errorf("Runs[%d].Name = %q, want %q", i, run.Name, wantNames[i])
		}
		if run.Path != "/case/"+wantNames[i] {
			t.Errorf("Runs[%d].Path = %q", i, run.Path)
		}
		if run.End == nil || *run.End != wantEnds[i] {
			t.Errorf("Runs[%d].End = %v, want %v", i, run.End, wantEnds[i])
		}
		// Each leg carries its own done marker, and the needsRestart refinement
		// belongs to the case, not to a leg whose end_time has moved on.
		if run.Status != "completed" {
			t.Errorf("Runs[%d].Status = %q, want completed", i, run.Status)
		}
		if run.Count != 5 || run.Size == 0 {
			t.Errorf("Runs[%d] count/size = %d/%d, want 5 files and a footprint",
				i, run.Count, run.Size)
		}
	}
}

// keepRuns holds the newest legs back from the whole sweep, not just from the
// folder deletion: sparing a leg means leaving it intact.
func TestConvergeCleanKeepRuns(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantDeleted int
		wantGone    []string
		wantKept    []string
	}{
		{
			name:        "folders whole, newest leg spared",
			body:        `{"kinds":["outputs"],"keepRuns":1}`,
			wantDeleted: 2,
			wantGone:    []string{"outputs_original", "outputs_restart1"},
			wantKept: []string{
				"outputs_restart2/restart0001.rst",
				"outputs_restart2/stream0/thermo.out",
			},
		},
		{
			name:        "a kind reaching inside stops at the spared leg",
			body:        `{"kinds":["restart"],"keepRuns":1}`,
			wantDeleted: 3,
			wantGone: []string{
				"restart0163.rst",
				"outputs_original/restart0001.rst",
				"outputs_restart1/restart0001.rst",
			},
			wantKept: []string{"outputs_restart2/restart0001.rst"},
		},
		{
			name:        "keep-newest and keep-runs compose",
			body:        `{"kinds":["restart"],"keepRuns":1,"keepRestarts":1}`,
			wantDeleted: 2,
			wantGone: []string{
				"outputs_original/restart0001.rst",
				"outputs_restart1/restart0001.rst",
			},
			wantKept: []string{
				"restart0163.rst",
				"outputs_restart2/restart0001.rst",
			},
		},
		{
			name:        "keeping every leg leaves the chain alone",
			body:        `{"kinds":["outputs"],"keepRuns":9}`,
			wantDeleted: 0,
			wantKept: []string{
				"outputs_original", "outputs_restart1", "outputs_restart2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userScope := t.TempDir()
			caseDir := filepath.Join(userScope, "case")
			convergeChain(t, caseDir, "7200")
			// The copy CONVERGE leaves at the case root when a leg hands off.
			writeCaseFile(t, caseDir, "rst", "restart0163.rst")

			_, clean, token := convergeTestHandlers(t, userScope, users.Permissions{Delete: true})
			got := convergeCleanRequestJSON(t, clean, token, tt.body)

			if got.Deleted != tt.wantDeleted || got.Failed != 0 {
				t.Errorf("deleted = %d failed = %d, want %d and 0",
					got.Deleted, got.Failed, tt.wantDeleted)
			}
			for _, rel := range tt.wantGone {
				if _, err := os.Stat(filepath.Join(caseDir, rel)); !os.IsNotExist(err) {
					t.Errorf("%s survived the sweep", rel)
				}
			}
			for _, rel := range tt.wantKept {
				if _, err := os.Stat(filepath.Join(caseDir, rel)); err != nil {
					t.Errorf("%s should have been spared: %v", rel, err)
				}
			}
		})
	}
}

func TestConvergeScanReportsOutputDirs(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	convergeChain(t, caseDir, "7200")

	scan, _, token := convergeTestHandlers(t, userScope, users.Permissions{Delete: true})

	req, _ := http.NewRequest(http.MethodGet, "/api/converge/case", http.NoBody)
	req.Header.Set("X-Auth", token)
	rec := httptest.NewRecorder()
	scan.ServeHTTP(rec, req)

	var got convergeScanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	wantNames := []string{"outputs_restart2", "outputs_restart1", "outputs_original"}
	if len(got.OutputDirs) != len(wantNames) {
		t.Fatalf("OutputDirs = %+v, want %d", got.OutputDirs, len(wantNames))
	}

	for i, dir := range got.OutputDirs {
		if dir.Name != wantNames[i] {
			t.Errorf("OutputDirs[%d].Name = %q, want %q", i, dir.Name, wantNames[i])
		}
		if !dir.Deletable {
			t.Errorf("OutputDirs[%d] should be deletable", i)
		}
		// Groups price what sparing this leg subtracts: its own restart, out
		// and log files. converge.start and converge.done match no kind.
		kinds := map[string]int{}
		for _, group := range dir.Groups {
			kinds[group.Kind] = group.Count
		}
		if want := (map[string]int{"restart": 1, "out": 1, "log": 1}); !maps.Equal(kinds, want) {
			t.Errorf("OutputDirs[%d].Groups = %v, want %v", i, kinds, want)
		}
	}
}
