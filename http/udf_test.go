package fbhttp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/rforced/filebrowser/v2/files"
	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/users"
)

// udfStockCMake is the tail of the CMakeLists.txt cvg_udf_init copies out of a
// CONVERGE install. The include line is the whole of the marker.
const udfStockCMake = `cmake_minimum_required( VERSION 3.27 )
project( "CONVERGE UDF Development Package" C )
set( ENV{CONVERGE_UDF_OFFICIAL} TRUE )
include($ENV{CVG_SOLVER_ROOT}/share/cmake/CONVERGE_BUILD)
`

func writeUdfFile(t *testing.T, root, rel, body string) {
	t.Helper()

	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// udfInstallTree lays down one CONVERGE install the way Horizon unpacks it.
// mpiDirs are the environment sub-directories to create under scripts/CONVERGE.
func udfInstallTree(t *testing.T, apps, version string, mpiDirs []string, opts ...func(base, topdir string)) {
	t.Helper()

	base := filepath.Join(apps, version)
	topdir := filepath.Join(base, "Convergent_Science", "CONVERGE_CFD", version)

	writeUdfFile(t, base, ".installed", "")
	writeUdfFile(t, topdir, filepath.Join("CONVERGE", "share", "cmake", "CONVERGE_BUILD"), "# build rules\n")
	writeUdfFile(t, topdir, filepath.Join("CONVERGE", "x64", "bin", "cvg_udf_init"), "#!/bin/bash\n")

	for _, mpi := range mpiDirs {
		writeUdfFile(t, topdir,
			filepath.Join("environment", "x64", "scripts", "CONVERGE", mpi, version+".sh"),
			"# env\n")
	}

	for _, opt := range opts {
		opt(base, topdir)
	}
}

// udfPackage lays down a compilable package: the stock CMakeLists plus a source.
func udfPackage(t *testing.T, dir string) {
	t.Helper()

	writeUdfFile(t, dir, "CMakeLists.txt", udfStockCMake)
	writeUdfFile(t, dir, filepath.Join("src", "configure.c"), "#include <CONVERGE/udf.h>\n")
}

// udfTestData builds the request context the file-level helpers take, for the
// cases that are about what lands on disk rather than what the endpoint answers.
func udfTestData(t *testing.T, root string, perm users.Permissions) *data {
	t.Helper()

	return &data{
		settings: &settings.Settings{},
		server:   &settings.Server{},
		user: &users.User{
			Username: "u",
			Perm:     perm,
			Fs:       files.NewScopedFs(afero.NewOsFs(), root),
		},
	}
}

func udfTestHandler(
	t *testing.T,
	fn handleFunc,
	userScope, apps string,
	perm users.Permissions,
) (http.Handler, string) {
	t.Helper()

	st := scopedUserStorage(t, userScope, perm, []byte("test-signing-key"))
	server := &settings.Server{ConvergeApps: apps}
	return handle(fn, "/api/udf", st, server), issueToken(t, st)
}

func requestUdf(
	t *testing.T,
	fn handleFunc,
	method, userScope, apps, urlPath string,
	body string,
	perm users.Permissions,
) *httptest.ResponseRecorder {
	t.Helper()

	handler, token := udfTestHandler(t, fn, userScope, apps, perm)

	var reader io.Reader = http.NoBody
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, "/api/udf"+urlPath, reader)
	req.Header.Set("X-Auth", token)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestUdfInstallsDiscoversFlavourPerVersion(t *testing.T) {
	apps := t.TempDir()

	// The MPI flavour moves with the major version, which is why it is
	// discovered rather than mapped: v6 ships INTEL, v5 HPCX, v4 CONVERGE-HPCX.
	udfInstallTree(t, apps, "6.0.1", []string{"INTEL", "SERIAL", "common"})
	udfInstallTree(t, apps, "5.1.1", []string{"HPCX", "common"})
	udfInstallTree(t, apps, "4.1.2", []string{"CONVERGE-HPCX"})

	installs := udfInstalls(apps)
	if len(installs) != 3 {
		t.Fatalf("got %d installs, want 3: %+v", len(installs), installs)
	}

	want := []struct{ version, mpi string }{
		{"6.0.1", "INTEL"},
		{"5.1.1", "HPCX"},
		{"4.1.2", "CONVERGE-HPCX"},
	}
	for i, w := range want {
		if installs[i].Version != w.version {
			t.Errorf("install %d: got version %q, want %q", i, installs[i].Version, w.version)
		}
		if got := filepath.Base(filepath.Dir(installs[i].envScript)); got != w.mpi {
			t.Errorf("%s: got env flavour %q, want %q", w.version, got, w.mpi)
		}
		if base := filepath.Base(installs[i].envScript); base != w.version+".sh" {
			t.Errorf("%s: got env script %q, want %s.sh", w.version, base, w.version)
		}
	}
}

// common holds the UDF compile variables on v5 and v6, but it is sourced by the
// flavour script and v4 has no such directory. Picking it would leave MPI_TYPE
// unset and the environment half-loaded.
func TestUdfInstallsNeverPicksCommon(t *testing.T) {
	apps := t.TempDir()
	udfInstallTree(t, apps, "6.0.1", []string{"common"})

	if installs := udfInstalls(apps); len(installs) != 0 {
		t.Fatalf("got %+v, want no installs when only common is present", installs)
	}
}

func TestUdfInstallsSkipsIncomplete(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(base, topdir string)
	}{
		{"no .installed marker", func(base, _ string) {
			os.Remove(filepath.Join(base, ".installed"))
		}},
		{"no environment script", func(_, topdir string) {
			os.RemoveAll(filepath.Join(topdir, "environment"))
		}},
		{"no cvg_udf_init", func(_, topdir string) {
			os.Remove(filepath.Join(topdir, "CONVERGE", "x64", "bin", "cvg_udf_init"))
		}},
		{"no cmake include", func(_, topdir string) {
			os.Remove(filepath.Join(topdir, "CONVERGE", "share", "cmake", "CONVERGE_BUILD"))
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			apps := t.TempDir()
			udfInstallTree(t, apps, "6.0.1", []string{"INTEL"}, tc.mutate)

			if installs := udfInstalls(apps); len(installs) != 0 {
				t.Fatalf("got %+v, want the incomplete install skipped", installs)
			}
		})
	}
}

func TestCompareConvergeVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"6.0.1", "5.1.1", 1},
		{"10.0", "9.1", 1},
		{"6.0.1", "6.0", 1},
		{"6.0.1", "6.0.1", 0},
		{"4.1.2", "6.0.1", -1},
		{"6.0.1-rc", "6.0.1", 1},
	}

	for _, tc := range cases {
		if got := compareConvergeVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestUdfPercentParsing(t *testing.T) {
	cases := []struct {
		line string
		want int
	}{
		{"[ 42%] Building C object CMakeFiles/converge_udf_user.dir/src/configure.c.o", 42},
		{"[100%] Linking C shared library libconverge_udf.so", 100},
		{"[  7%] Building C object", 7},
		{"-- Configuring done", -1},
		{"src/configure.c:12:3: warning: only [ 50%] of cases handled", -1},
	}

	for _, tc := range cases {
		match := udfPercent.FindStringSubmatch(tc.line)
		if tc.want < 0 {
			if match != nil {
				t.Errorf("%q: matched %v, want no match", tc.line, match)
			}
			continue
		}
		if match == nil {
			t.Errorf("%q: no match, want %d", tc.line, tc.want)
			continue
		}
		if match[1] != strconv.Itoa(tc.want) {
			t.Errorf("%q: got %q, want %d", tc.line, match[1], tc.want)
		}
	}
}

func TestUdfRegistryLimits(t *testing.T) {
	reg := &udfRegistry{active: map[string]struct{}{}}

	if err := reg.acquire("/a"); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	// Two makes in one build directory write over each other's objects.
	if err := reg.acquire("/a"); !errors.Is(err, errUdfBuilding) {
		t.Fatalf("second acquire of the same dir: got %v, want errUdfBuilding", err)
	}

	for i := 1; i < udfMaxConcurrent; i++ {
		if err := reg.acquire(filepath.Join("/dir", strconv.Itoa(i))); err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
	}
	if err := reg.acquire("/over"); !errors.Is(err, errUdfBusy) {
		t.Fatalf("acquire past the cap: got %v, want errUdfBusy", err)
	}

	reg.release("/a")
	if err := reg.acquire("/a"); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
}

func TestUdfInfoRecognisesPackage(t *testing.T) {
	root := t.TempDir()
	apps := t.TempDir()
	udfInstallTree(t, apps, "6.0.1", []string{"INTEL"})
	udfInstallTree(t, apps, "5.1.1", []string{"HPCX"})

	// The reference package carries no inputs.in: being a CONVERGE case is not
	// what makes a directory compilable.
	udfPackage(t, filepath.Join(root, "pkg"))
	writeUdfFile(t, root, filepath.Join("pkg", "build", ".converge_udf_version"), "5.1.1\n")

	rec := requestUdf(t, udfInfoHandler, http.MethodGet, root, apps, "/pkg", "", users.Permissions{Create: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var info udfInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if !info.Package {
		t.Error("got package=false, want true")
	}
	if !info.HasSource {
		t.Error("got hasSource=false, want true")
	}
	if info.LastVersion != "5.1.1" {
		t.Errorf("got lastVersion %q, want 5.1.1", info.LastVersion)
	}
	if len(info.Versions) != 2 || info.Versions[0].Version != "6.0.1" {
		t.Errorf("got versions %+v, want 6.0.1 first of two", info.Versions)
	}
}

func TestUdfInfoRejectsNonPackage(t *testing.T) {
	cases := []struct {
		name  string
		build func(root string)
	}{
		{"plain cmake project", func(root string) {
			writeUdfFile(t, root, filepath.Join("pkg", "CMakeLists.txt"),
				"cmake_minimum_required(VERSION 3.10)\nproject(hello C)\nadd_executable(hello main.c)\n")
		}},
		{"no CMakeLists at all", func(root string) {
			writeUdfFile(t, root, filepath.Join("pkg", "src", "configure.c"), "int main(void){return 0;}\n")
		}},
		{"CMakeLists is a directory", func(root string) {
			if err := os.MkdirAll(filepath.Join(root, "pkg", "CMakeLists.txt"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			apps := t.TempDir()
			udfInstallTree(t, apps, "6.0.1", []string{"INTEL"})
			tc.build(root)

			rec := requestUdf(t, udfInfoHandler, http.MethodGet, root, apps, "/pkg", "", users.Permissions{Create: true})
			if rec.Code != http.StatusOK {
				t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
			}

			var info udfInfo
			if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
				t.Fatal(err)
			}
			if info.Package {
				t.Error("got package=true, want false")
			}
			if len(info.Versions) != 0 {
				t.Errorf("got versions %+v, want none for a non-package", info.Versions)
			}
		})
	}
}

// A version the client names has to match one we discovered before anything is
// run, because it decides which environment script the build sources.
func TestUdfBuildRejectsUnknownVersion(t *testing.T) {
	root := t.TempDir()
	apps := t.TempDir()
	udfInstallTree(t, apps, "6.0.1", []string{"INTEL"})
	udfPackage(t, filepath.Join(root, "pkg"))

	for _, body := range []string{
		`{"version":"7.0.0"}`,
		`{"version":""}`,
		`{"version":"../../../etc"}`,
		`{"version":"6.0.1; rm -rf /"}`,
	} {
		rec := requestUdf(t, udfBuildHandler, http.MethodPost, root, apps, "/pkg", body, users.Permissions{Create: true})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got status %d, want 400", body, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "udfUnknownVersion") {
			t.Errorf("%s: got body %q, want udfUnknownVersion", body, rec.Body.String())
		}
	}
}

func TestUdfBuildRejectsNonPackage(t *testing.T) {
	root := t.TempDir()
	apps := t.TempDir()
	udfInstallTree(t, apps, "6.0.1", []string{"INTEL"})
	writeUdfFile(t, root, filepath.Join("plain", "CMakeLists.txt"), "project(hello C)\n")

	rec := requestUdf(t, udfBuildHandler, http.MethodPost, root, apps, "/plain",
		`{"version":"6.0.1"}`, users.Permissions{Create: true})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestUdfBuildRequiresCreatePermission(t *testing.T) {
	root := t.TempDir()
	apps := t.TempDir()
	udfInstallTree(t, apps, "6.0.1", []string{"INTEL"})
	udfPackage(t, filepath.Join(root, "pkg"))

	rec := requestUdf(t, udfBuildHandler, http.MethodPost, root, apps, "/pkg",
		`{"version":"6.0.1"}`, users.Permissions{})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("got status %d, want 403", rec.Code)
	}
}

// A second build in the same directory is refused rather than queued: the two
// makes would write over each other in one build tree.
func TestUdfBuildRejectsConcurrentBuildOfSameDir(t *testing.T) {
	root := t.TempDir()
	apps := t.TempDir()
	udfInstallTree(t, apps, "6.0.1", []string{"INTEL"})
	udfPackage(t, filepath.Join(root, "pkg"))

	key := filepath.Join(root, "pkg")
	if err := udfBuilds.acquire(key); err != nil {
		t.Fatal(err)
	}
	defer udfBuilds.release(key)

	rec := requestUdf(t, udfBuildHandler, http.MethodPost, root, apps, "/pkg",
		`{"version":"6.0.1"}`, users.Permissions{Create: true})
	if rec.Code != http.StatusConflict {
		t.Fatalf("got status %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "udfBuilding") {
		t.Errorf("got body %q, want udfBuilding", rec.Body.String())
	}
}

// cmake refuses a cache generated for another directory, and copying a package
// is how these get shared — the reference v6_UDF_Example ships one naming a
// workstation. Changing CONVERGE version is deliberately not a trigger: the
// build reconfigures every time, which rewrites the include paths itself.
func TestUdfPrepareBuildDirClearsStaleCache(t *testing.T) {
	cases := []struct {
		name     string
		cache    string
		wantGone bool
	}{
		{"cache from another machine", "CMAKE_CACHEFILE_DIR:INTERNAL=/pwork/kwhyte/UDF_library/01/build\n", true},
		{"cache with no build directory recorded", "CMAKE_C_COMPILER:FILEPATH=/bin/cc\n", true},
		{"cache for this directory", "", false},
		{"never configured", "-", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			pkg := filepath.Join(root, "pkg")
			realBuildDir := filepath.Join(pkg, "build")
			udfPackage(t, pkg)

			writeUdfFile(t, pkg, filepath.Join("build", "keep.txt"), "objects\n")
			switch tc.cache {
			case "-":
				// No cache at all.
			case "":
				writeUdfFile(t, pkg, filepath.Join("build", "CMakeCache.txt"),
					"CMAKE_CACHEFILE_DIR:INTERNAL="+realBuildDir+"\n")
			default:
				writeUdfFile(t, pkg, filepath.Join("build", "CMakeCache.txt"), tc.cache)
			}

			d := udfTestData(t, root, users.Permissions{Create: true})
			if err := udfPrepareBuildDir(d, "/pkg/build", realBuildDir); err != nil {
				t.Fatal(err)
			}

			_, err := os.Stat(filepath.Join(pkg, "build", "keep.txt"))
			if tc.wantGone && err == nil {
				t.Error("a build directory cmake would refuse was kept")
			}
			if !tc.wantGone && err != nil {
				t.Errorf("build directory was cleared when it should have been kept: %v", err)
			}
		})
	}
}

func TestUdfCacheDir(t *testing.T) {
	cases := []struct {
		name, content, want string
	}{
		{"real cache", "CMAKE_CACHEFILE_DIR:INTERNAL=/tmp/pkg/build\nCMAKE_HOME_DIRECTORY:INTERNAL=/tmp/pkg\n", "/tmp/pkg/build"},
		{"first line", "CMAKE_CACHEFILE_DIR:INTERNAL=/a/b\n", "/a/b"},
		{"no trailing newline", "CMAKE_CACHEFILE_DIR:INTERNAL=/a/b", "/a/b"},
		{"absent", "CMAKE_C_COMPILER:FILEPATH=/bin/cc\n", ""},
		// The home directory is a different key and must not be mistaken for it.
		{"home directory only", "CMAKE_HOME_DIRECTORY:INTERNAL=/tmp/pkg\n", ""},
		{"empty", "", ""},
	}

	for _, tc := range cases {
		if got := udfCacheDir([]byte(tc.content)); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestUdfPublishArtifactCopiesToPackageRoot(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "pkg")
	udfPackage(t, pkg)
	writeUdfFile(t, pkg, filepath.Join("build", "libconverge_udf.so"), "ELF\n")

	d := udfTestData(t, root, users.Permissions{Create: true})
	got, err := udfPublishArtifact(d, "/pkg", "/pkg/build")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/pkg/libconverge_udf.so" {
		t.Errorf("got artifact path %q, want /pkg/libconverge_udf.so", got)
	}

	// The environment puts "./" first on LD_LIBRARY_PATH, so the package root is
	// where the solver looks.
	body, err := os.ReadFile(filepath.Join(pkg, "libconverge_udf.so"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ELF\n" {
		t.Errorf("got %q, want the built library copied verbatim", body)
	}
	if _, err := os.Stat(filepath.Join(pkg, "libconverge_udf.so.tmp")); err == nil {
		t.Error("the temporary copy was left behind")
	}
}

func TestUdfPublishArtifactReportsMissingLibrary(t *testing.T) {
	root := t.TempDir()
	udfPackage(t, filepath.Join(root, "pkg"))

	d := udfTestData(t, root, users.Permissions{Create: true})
	if _, err := udfPublishArtifact(d, "/pkg", "/pkg/build"); err == nil {
		t.Fatal("got nil error, want a failure when the build produced nothing")
	}
}

// "exit status 2" names nothing, and neither does make's summary. Both are the
// last lines of a failed build, and the diagnostic above them is the point.
func TestUdfFailureMessagePrefersCompilerDiagnostic(t *testing.T) {
	exitErr := exec.Command("/bin/sh", "-c", "exit 2").Run()

	cases := []struct {
		name, output, want string
	}{
		{
			// The shape a real failed build ends on, taken from a broken
			// configure.c compiled on the FileSystem box.
			name: "gcc diagnostic under three make summaries",
			output: "[ 33%] Building C object CMakeFiles/converge_udf_user.dir/src/configure.c.o\n" +
				"src/configure.c:10:1: error: expected ';' at end of input\n" +
				"gmake[2]: *** [CMakeFiles/converge_udf_user.dir/build.make:79: configure.c.o] Error 1\n" +
				"gmake[1]: *** [CMakeFiles/Makefile2:122: all] Error 2\n" +
				"gmake: *** [Makefile:136: all] Error 2\n",
			want: "src/configure.c:10:1: error: expected ';' at end of input",
		},
		{
			name:   "cmake configure failure",
			output: "-- Configuring incomplete\nCMake Error at CMakeLists.txt:42 (include):\n",
			want:   "CMake Error at CMakeLists.txt:42 (include):",
		},
		{
			// Nothing says "error", so the last line that is not make's is it.
			name:   "no diagnostic to find",
			output: "linker input file unused\ngmake: *** [Makefile:136: all] Error 2\n",
			want:   "linker input file unused",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := udfFailureMessage(exitErr, []byte(tc.output)); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}

	if got := udfFailureMessage(exitErr, nil); got != exitErr.Error() {
		t.Errorf("got %q, want the raw error when there is no output", got)
	}

	// Only make summaries is the same as nothing worth quoting.
	if got := udfFailureMessage(exitErr, []byte("gmake: *** [Makefile:136: all] Error 2\n")); got != exitErr.Error() {
		t.Errorf("got %q, want the raw error when only make spoke", got)
	}

	long := "src/a.c:1:1: error: " + strings.Repeat("x", 400)
	got := udfFailureMessage(exitErr, []byte(long+"\n"))
	if len([]rune(got)) != udfFailureMessageLimit+1 || !strings.HasSuffix(got, "…") {
		t.Errorf("got a message of %d runes, want it truncated to %d plus an ellipsis",
			len([]rune(got)), udfFailureMessageLimit)
	}
}
