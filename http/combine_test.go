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

const combineHeader = `# CONVERGE 6.0.1/  Jul 27 2026       Run Date:Tue Aug 18 18:27:50 2026
# column        1                2
#           Crank           Swirl
#           (deg)          (none)
#
`

func combineTestHandler(t *testing.T, userScope string, perm users.Permissions) (http.Handler, string) {
	t.Helper()

	st := scopedUserStorage(t, userScope, perm, []byte("test-signing-key"))
	return handle(combineHandler, "/api/combine", st, &settings.Server{}), issueToken(t, st)
}

func writeCombineFile(t *testing.T, root, rel, body string) {
	t.Helper()

	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// combineCase lays down a case directory with an inputs.in marker.
func combineCase(t *testing.T, caseDir string) {
	t.Helper()

	writeCombineFile(t, caseDir, "inputs.in", "version: 6\n")
}

func postCombine(t *testing.T, userScope, urlPath string, perm users.Permissions) *httptest.ResponseRecorder {
	t.Helper()

	handler, token := combineTestHandler(t, userScope, perm)
	req, _ := http.NewRequest(http.MethodPost, "/api/combine"+urlPath, http.NoBody)
	req.Header.Set("X-Auth", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func readCombined(t *testing.T, caseDir, rel string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(caseDir, convergeCombinedDir, rel))
	if err != nil {
		t.Fatalf("reading combined %s: %v", rel, err)
	}
	return string(body)
}

func TestCombineLegRankOrdersRestartsNumerically(t *testing.T) {
	legs := []combineLeg{}
	for _, name := range []string{
		"outputs_restart10", "outputs_hand_made", "outputs_restart2", "outputs_original", "outputs_restart1",
	} {
		rank, seq := combineLegRank(name)
		legs = append(legs, combineLeg{name: name, rank: rank, seq: seq})
	}
	sortCombineLegs(legs)

	var got []string
	for _, leg := range legs {
		got = append(got, leg.name)
	}

	want := []string{
		"outputs_original", "outputs_restart1", "outputs_restart2", "outputs_restart10", "outputs_hand_made",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("leg order = %v, want %v", got, want)
		}
	}
}

// TestCombineStitchesLegsInOrder covers the ordinary chain: each leg re-prints
// the checkpoint row the one before it ended on, and that row must appear once.
func TestCombineStitchesLegsInOrder(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	combineCase(t, caseDir)

	writeCombineFile(t, caseDir, "outputs_original/stream0/thermo.out",
		combineHeader+"0 1\n10 2\n20 3\n")
	writeCombineFile(t, caseDir, "outputs_restart1/stream0/thermo.out",
		combineHeader+"20 3\n30 4\n")
	writeCombineFile(t, caseDir, "outputs_restart2/stream0/thermo.out",
		combineHeader+"30 4\n40 5\n")

	rec := postCombine(t, userScope, "/case", users.Permissions{Create: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	var resp combineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Files != 1 || resp.Legs != 3 {
		t.Errorf("response = %+v, want 1 file across 3 legs", resp)
	}

	want := combineHeader + "0 1\n10 2\n20 3\n30 4\n40 5\n"
	if got := readCombined(t, caseDir, "stream0/thermo.out"); got != want {
		t.Errorf("combined =\n%q\nwant\n%q", got, want)
	}
}

// TestCombineNewerLegSupersedesBacktrack is the case that separates our seam
// rule from the shell script's. outputs_restart1 picks up at 10, so the rows
// outputs_original wrote past 10 belong to a trajectory that was abandoned and
// the newer leg's re-solve of the same span is what survives.
func TestCombineNewerLegSupersedesBacktrack(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	combineCase(t, caseDir)

	writeCombineFile(t, caseDir, "outputs_original/stream0/thermo.out",
		combineHeader+"0 1\n10 2\n20 3\n30 4\n")
	writeCombineFile(t, caseDir, "outputs_restart1/stream0/thermo.out",
		combineHeader+"10 5\n40 6\n")

	if rec := postCombine(t, userScope, "/case", users.Permissions{Create: true}); rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	want := combineHeader + "0 1\n10 5\n40 6\n"
	if got := readCombined(t, caseDir, "stream0/thermo.out"); got != want {
		t.Errorf("combined =\n%q\nwant\n%q", got, want)
	}
}

// TestCombineMirrorsEveryRelativePath pins the one rule that replaces the
// script's per-filename stream handling: files are matched by their path below
// the leg root, so a leg-root file, stream0 and stream1 all land correctly and a
// file only one leg has is copied whole.
func TestCombineMirrorsEveryRelativePath(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	combineCase(t, caseDir)

	writeCombineFile(t, caseDir, "outputs_original/memory_usage.out", combineHeader+"0 1\n10 2\n")
	writeCombineFile(t, caseDir, "outputs_original/stream0/thermo.out", combineHeader+"0 1\n10 2\n")
	writeCombineFile(t, caseDir, "outputs_original/stream1/thermo.out", combineHeader+"0 7\n10 8\n")
	writeCombineFile(t, caseDir, "outputs_original/post00100.h5", "not an out file")

	writeCombineFile(t, caseDir, "outputs_restart1/memory_usage.out", combineHeader+"10 2\n20 3\n")
	writeCombineFile(t, caseDir, "outputs_restart1/stream0/thermo.out", combineHeader+"10 2\n20 3\n")
	writeCombineFile(t, caseDir, "outputs_restart1/stream1/thermo.out", combineHeader+"10 8\n20 9\n")
	// Only the second leg wrote this one; it is copied rather than stitched.
	writeCombineFile(t, caseDir, "outputs_restart1/stream0/user_probe.out", combineHeader+"10 1\n20 2\n")

	rec := postCombine(t, userScope, "/case", users.Permissions{Create: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	var resp combineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Files != 4 {
		t.Errorf("Files = %d, want the 4 .out paths and not the post file", resp.Files)
	}

	for rel, want := range map[string]string{
		"memory_usage.out":       combineHeader + "0 1\n10 2\n20 3\n",
		"stream0/thermo.out":     combineHeader + "0 1\n10 2\n20 3\n",
		"stream1/thermo.out":     combineHeader + "0 7\n10 8\n20 9\n",
		"stream0/user_probe.out": combineHeader + "10 1\n20 2\n",
	} {
		if got := readCombined(t, caseDir, rel); got != want {
			t.Errorf("%s =\n%q\nwant\n%q", rel, got, want)
		}
	}

	if _, err := os.Stat(filepath.Join(caseDir, convergeCombinedDir, "post00100.h5")); !os.IsNotExist(err) {
		t.Error("the post file should not have been copied")
	}
}

// TestCombineKeepsInlineTagsAndOneHeader covers time.out, whose rows close with
// a free-text dt_limiter tag introduced by a hash. The tag is part of the row
// and must survive; a header a restart re-prints part-way through must not.
func TestCombineKeepsInlineTagsAndOneHeader(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	combineCase(t, caseDir)

	writeCombineFile(t, caseDir, "outputs_original/stream0/time.out",
		combineHeader+"0 1\n10 2  #dt_grow\n")
	writeCombineFile(t, caseDir, "outputs_restart1/stream0/time.out",
		combineHeader+"10 2  #dt_grow\n20 3  #dt_piso, max_piso_recover reached\n"+
			combineHeader+"30 4  #dt_mach\n")

	if rec := postCombine(t, userScope, "/case", users.Permissions{Create: true}); rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	want := combineHeader + "0 1\n10 2  #dt_grow\n" +
		"20 3  #dt_piso, max_piso_recover reached\n30 4  #dt_mach\n"
	if got := readCombined(t, caseDir, "stream0/time.out"); got != want {
		t.Errorf("combined =\n%q\nwant\n%q", got, want)
	}
}

// TestCombineDoesNotCountItsOwnOutputAsALeg keeps a second combine from folding
// the first result back in on itself: with one real leg beside it, there is
// still nothing to combine.
func TestCombineDoesNotCountItsOwnOutputAsALeg(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	combineCase(t, caseDir)

	writeCombineFile(t, caseDir, "outputs_original/stream0/thermo.out", combineHeader+"0 1\n")
	writeCombineFile(t, caseDir, convergeCombinedDir+"/stream0/thermo.out", combineHeader+"0 1\n")

	rec := postCombine(t, userScope, "/case", users.Permissions{Create: true})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %q, want 400", rec.Code, rec.Body.String())
	}

	var detail clientError
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decoding error body: %v", err)
	}
	if detail.Code != "combineNeedsRuns" {
		t.Errorf("code = %q, want combineNeedsRuns", detail.Code)
	}
}

// TestCombineRejectsExistingDestination is the error the button surfaces as a
// toast: combining twice never overwrites the first result.
func TestCombineRejectsExistingDestination(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	combineCase(t, caseDir)

	writeCombineFile(t, caseDir, "outputs_original/stream0/thermo.out", combineHeader+"0 1\n")
	writeCombineFile(t, caseDir, "outputs_restart1/stream0/thermo.out", combineHeader+"10 2\n")
	writeCombineFile(t, caseDir, convergeCombinedDir+"/stream0/thermo.out", combineHeader+"999 9\n")

	rec := postCombine(t, userScope, "/case", users.Permissions{Create: true})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body = %q, want 409", rec.Code, rec.Body.String())
	}

	var detail clientError
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decoding error body: %v", err)
	}
	if detail.Code != "combinedExists" {
		t.Errorf("code = %q, want combinedExists", detail.Code)
	}

	if got := readCombined(t, caseDir, "stream0/thermo.out"); got != combineHeader+"999 9\n" {
		t.Errorf("the existing result was modified: %q", got)
	}
}

func TestCombineRequiresTwoLegs(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	combineCase(t, caseDir)

	writeCombineFile(t, caseDir, "outputs_original/stream0/thermo.out", combineHeader+"0 1\n")

	rec := postCombine(t, userScope, "/case", users.Permissions{Create: true})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %q, want 400", rec.Code, rec.Body.String())
	}

	var detail clientError
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decoding error body: %v", err)
	}
	if detail.Code != "combineNeedsRuns" {
		t.Errorf("code = %q, want combineNeedsRuns", detail.Code)
	}
}

func TestCombineRejectsNonCaseDirectory(t *testing.T) {
	userScope := t.TempDir()
	plain := filepath.Join(userScope, "plain")

	writeCombineFile(t, plain, "outputs_original/stream0/thermo.out", combineHeader+"0 1\n")
	writeCombineFile(t, plain, "outputs_restart1/stream0/thermo.out", combineHeader+"10 2\n")

	rec := postCombine(t, userScope, "/plain", users.Permissions{Create: true})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %q, want 400", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(plain, convergeCombinedDir)); !os.IsNotExist(err) {
		t.Error("nothing should have been written for a non-case directory")
	}
}

func TestCombineRequiresCreatePermission(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	combineCase(t, caseDir)

	writeCombineFile(t, caseDir, "outputs_original/stream0/thermo.out", combineHeader+"0 1\n")
	writeCombineFile(t, caseDir, "outputs_restart1/stream0/thermo.out", combineHeader+"10 2\n")

	rec := postCombine(t, userScope, "/case", users.Permissions{Delete: true})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(caseDir, convergeCombinedDir)); !os.IsNotExist(err) {
		t.Error("nothing should have been written without create permission")
	}
}

// TestCombineSkipsSymlinkedLegs mirrors the rest of the CONVERGE handlers: a
// link is described by ReadDir as itself, and following one would read a tree
// that lives outside the case.
func TestCombineSkipsSymlinkedLegs(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	combineCase(t, caseDir)

	writeCombineFile(t, caseDir, "outputs_original/stream0/thermo.out", combineHeader+"0 1\n")
	writeCombineFile(t, caseDir, "elsewhere/stream0/thermo.out", combineHeader+"10 2\n")

	if err := os.Symlink(filepath.Join(caseDir, "elsewhere"), filepath.Join(caseDir, "outputs_link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	rec := postCombine(t, userScope, "/case", users.Permissions{Create: true})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %q, want 400 for a single real leg", rec.Code, rec.Body.String())
	}
}

// TestCombineWithoutOriginalLeg covers a case restarted from an archive, where
// there is no outputs_original to open the chain.
func TestCombineWithoutOriginalLeg(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	combineCase(t, caseDir)

	writeCombineFile(t, caseDir, "outputs_restart2/stream0/thermo.out", combineHeader+"20 3\n30 4\n")
	writeCombineFile(t, caseDir, "outputs_restart10/stream0/thermo.out", combineHeader+"30 4\n40 5\n")

	if rec := postCombine(t, userScope, "/case", users.Permissions{Create: true}); rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	want := combineHeader + "20 3\n30 4\n40 5\n"
	if got := readCombined(t, caseDir, "stream0/thermo.out"); got != want {
		t.Errorf("combined =\n%q\nwant\n%q", got, want)
	}
}

// TestCombineWithoutParsableTimes falls back to a plain append, which is the
// most a file with no numeric leading column can be given.
func TestCombineWithoutParsableTimes(t *testing.T) {
	userScope := t.TempDir()
	caseDir := filepath.Join(userScope, "case")
	combineCase(t, caseDir)

	writeCombineFile(t, caseDir, "outputs_original/stream0/notes.out", "# header\nalpha 1\n")
	writeCombineFile(t, caseDir, "outputs_restart1/stream0/notes.out", "# header\nbeta 2\n")

	if rec := postCombine(t, userScope, "/case", users.Permissions{Create: true}); rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	want := "# header\nalpha 1\nbeta 2\n"
	if got := readCombined(t, caseDir, "stream0/notes.out"); got != want {
		t.Errorf("combined =\n%q\nwant\n%q", got, want)
	}
}
