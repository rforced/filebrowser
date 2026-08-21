package fbhttp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/afero"

	"github.com/rforced/filebrowser/v2/files"
)

const (
	udfCMakeFile    = "CMakeLists.txt"
	udfSourceDir    = "src"
	udfBuildDir     = "build"
	udfLibName      = "libconverge_udf.so"
	udfLogName      = "compile.log"
	udfVersionStamp = ".converge_udf_version"

	// udfBuildMarker is the line every CONVERGE UDF package ends on:
	// include($ENV{CVG_SOLVER_ROOT}/share/cmake/CONVERGE_BUILD). Sniffing for it
	// is what separates a UDF package from any other cmake project, the same way
	// surface.dat is recognised by its content and not its name.
	udfBuildMarker = "CONVERGE_BUILD"

	// defaultConvergeApps is where Horizon installs solvers on a FileSystem box,
	// one sub-directory per version. Having it as a default rather than a
	// required flag is what lets this ship without a Horizon-side change.
	defaultConvergeApps = "/mnt/fs/.cache/apps/converge"

	udfBuildTimeout  = 10 * time.Minute
	udfLogLimit      = 1 << 20
	udfMaxConcurrent = 4
)

// udfInstall is one CONVERGE version a UDF can be compiled against, already
// checked for everything the build needs.
type udfInstall struct {
	Version    string `json:"version"`
	envScript  string
	solverRoot string
}

// udfInfo answers what the compile prompt has to ask before it can offer a
// build: whether this is a UDF package at all, what it can be built against,
// and what it was built against last.
type udfInfo struct {
	Package     bool         `json:"package"`
	HasSource   bool         `json:"hasSource"`
	Versions    []udfInstall `json:"versions"`
	LastVersion string       `json:"lastVersion,omitempty"`
}

// udfProgress is one Server-Sent Event of a build. Phase moves from configure
// to build to done; Percent is only meaningful once the build phase starts,
// because cmake's configure step reports no proportion of anything.
type udfProgress struct {
	Phase    string `json:"phase"`
	Percent  int    `json:"percent"`
	Line     string `json:"line,omitempty"`
	Artifact string `json:"artifact,omitempty"`
	LogPath  string `json:"logPath,omitempty"`
	Error    string `json:"error,omitempty"`
}

type udfBuildRequest struct {
	Version string `json:"version"`
}

const (
	udfPhaseConfigure = "configure"
	udfPhaseBuild     = "build"
	udfPhaseDone      = "done"
)

// udfBuildScript is a constant. Everything that varies between builds reaches
// it through the environment, so no part of a request is ever spliced into
// shell text.
//
// set -u is deliberately absent: the CONVERGE environment scripts append to
// PYTHONPATH and LD_LIBRARY_PATH without guarding for them being unset, and
// would abort under it. set -e is armed only after the source for the same
// reason the sourcing is checked by hand — the scripts are not ours.
const udfBuildScript = `
set -o pipefail
source "$CVG_ENV_SCRIPT" || { echo "failed to load the CONVERGE environment script" >&2; exit 1; }
set -e
cd "$UDF_DIR"
if [ "$UDF_RUN_INIT" = "1" ]; then cvg_udf_init --no-example-source; fi
mkdir -p build
cd build
cmake -G "Unix Makefiles" ..
cmake --build .
`

// udfPercent matches the progress prefix cmake's Unix Makefiles generator puts
// on every build line, e.g. "[ 42%] Building C object ...". It is anchored so
// that a percentage quoted inside a compiler diagnostic cannot drive the bar.
var udfPercent = regexp.MustCompile(`^\[\s*(\d+)%\]`)

// convergeAppsDir is where installed solvers are looked for. It is read through
// the OS filesystem rather than the user's: it sits outside every scope.
func convergeAppsDir(d *data) string {
	if d.server != nil && d.server.ConvergeApps != "" {
		return d.server.ConvergeApps
	}
	return defaultConvergeApps
}

// isUdfPackage reports whether dir holds a CONVERGE UDF package, judged by the
// content of its CMakeLists.txt rather than by the names of the files around
// it. The reference package carries no inputs.in, so being a CONVERGE case is
// neither necessary nor sufficient.
func isUdfPackage(d *data, dir string) (bool, error) {
	info, err := d.user.Fs.Stat(dir)
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, nil
	}

	fPath := path.Join(dir, udfCMakeFile)
	if !d.Check(fPath) {
		return false, nil
	}

	markerInfo, err := d.user.Fs.Stat(fPath)
	switch {
	case err == nil:
		if markerInfo.IsDir() {
			return false, nil
		}
	case os.IsNotExist(err), errors.Is(err, os.ErrPermission):
		return false, nil
	default:
		return false, err
	}

	content, err := convergeReadSmall(d.user.Fs, fPath)
	if err != nil {
		// Too large or unreadable. Either way this is not the small stock file
		// cvg_udf_init writes, so it is not a package we offer to build.
		return false, nil //nolint:nilerr // an unreadable marker is a non-match, not a failure
	}

	return strings.Contains(string(content), udfBuildMarker), nil
}

// udfInstalls lists the CONVERGE versions in root that are complete enough to
// compile a UDF, newest first. A version missing any one of its environment
// script, cvg_udf_init or the cmake include is dropped rather than offered and
// then failed halfway through a build.
func udfInstalls(root string) []udfInstall {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	installs := make([]udfInstall, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if install, ok := udfInstallAt(root, entry.Name()); ok {
			installs = append(installs, install)
		}
	}

	sort.Slice(installs, func(i, j int) bool {
		return compareConvergeVersions(installs[i].Version, installs[j].Version) > 0
	})
	return installs
}

// udfInstallAt validates one <apps>/<name> install tree.
func udfInstallAt(root, name string) (udfInstall, bool) {
	base := filepath.Join(root, name)

	// .installed is Horizon's own marker for a finished install; a tree without
	// it may still be unpacking.
	if _, err := os.Stat(filepath.Join(base, ".installed")); err != nil {
		return udfInstall{}, false
	}

	version, topdir, ok := udfSolverTree(base, name)
	if !ok {
		return udfInstall{}, false
	}

	solverRoot := filepath.Join(topdir, "CONVERGE")
	for _, needed := range []string{
		filepath.Join(solverRoot, "share", "cmake", udfBuildMarker),
		filepath.Join(solverRoot, "x64", "bin", "cvg_udf_init"),
	} {
		if _, err := os.Stat(needed); err != nil {
			return udfInstall{}, false
		}
	}

	envScript, ok := udfEnvScript(topdir, version)
	if !ok {
		return udfInstall{}, false
	}

	return udfInstall{Version: version, envScript: envScript, solverRoot: solverRoot}, true
}

// udfSolverTree finds the CONVERGE_CFD/<version> directory inside an install.
// The inner version normally repeats the install directory's name, but the
// name is the packaging's choice and the inner directory is the solver's, so a
// lone mismatched entry is taken rather than refused.
func udfSolverTree(base, name string) (version, topdir string, ok bool) {
	parent := filepath.Join(base, "Convergent_Science", "CONVERGE_CFD")

	if info, err := os.Stat(filepath.Join(parent, name)); err == nil && info.IsDir() {
		return name, filepath.Join(parent, name), true
	}

	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", "", false
	}
	dirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}
	if len(dirs) != 1 {
		return "", "", false
	}
	return dirs[0], filepath.Join(parent, dirs[0]), true
}

// udfEnvMPIRank orders the MPI flavours an install may ship. Which one is
// present moves with the major version — v6 has INTEL, v5 HPCX, v4
// CONVERGE-HPCX — so the flavour is discovered rather than mapped from the
// version. "common" is never a candidate: v5 and v6 keep the UDF compile
// variables there, but it is sourced by the flavour scripts and v4 has no such
// directory at all.
func udfEnvMPIRank(name string) int {
	switch {
	case name == "common":
		return -1
	case name == "INTEL":
		return 3
	case name == "HPCX":
		return 2
	case strings.HasPrefix(name, "CONVERGE-"):
		return 1
	default:
		return 0
	}
}

// udfEnvScript picks the environment script to source before building. It must
// be sourced and not executed: CONVERGE_BUILD aborts with a FATAL_ERROR when
// CVG_SOLVER_ROOT is unset, and the v5/v6 scripts refuse to run as a command.
func udfEnvScript(topdir, version string) (string, bool) {
	scripts := filepath.Join(topdir, "environment", "x64", "scripts", "CONVERGE")

	entries, err := os.ReadDir(scripts)
	if err != nil {
		return "", false
	}

	best, bestName, bestRank := "", "", 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		rank := udfEnvMPIRank(entry.Name())
		if rank < 0 {
			continue
		}

		candidate := filepath.Join(scripts, entry.Name(), version+".sh")
		if info, statErr := os.Stat(candidate); statErr != nil || info.IsDir() {
			continue
		}
		if best == "" || rank > bestRank || (rank == bestRank && entry.Name() < bestName) {
			best, bestName, bestRank = candidate, entry.Name(), rank
		}
	}

	return best, best != ""
}

// compareConvergeVersions orders dotted version strings by numeric component,
// so that 10.0 sorts above 9.1 and 6.0.1 above 6.0.
func compareConvergeVersions(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	for i := 0; i < len(aParts) || i < len(bParts); i++ {
		aNum, aErr := udfVersionPart(aParts, i)
		bNum, bErr := udfVersionPart(bParts, i)
		if aErr || bErr {
			return strings.Compare(a, b)
		}
		if aNum != bNum {
			if aNum < bNum {
				return -1
			}
			return 1
		}
	}
	return 0
}

func udfVersionPart(parts []string, i int) (int, bool) {
	if i >= len(parts) {
		return 0, false
	}
	n, err := strconv.Atoi(parts[i])
	if err != nil {
		return 0, true
	}
	return n, false
}

// udfRegistry keeps two builds out of one build directory, where a pair of
// concurrent makes would write over each other's objects, and caps how many
// compilers the box runs at once.
type udfRegistry struct {
	mu     sync.Mutex
	active map[string]struct{}
}

var udfBuilds = &udfRegistry{active: map[string]struct{}{}}

var (
	errUdfBuilding = errors.New("a build is already running for this directory")
	errUdfBusy     = errors.New("too many builds are already running")
)

func (r *udfRegistry) acquire(key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.active[key]; ok {
		return errUdfBuilding
	}
	if len(r.active) >= udfMaxConcurrent {
		return errUdfBusy
	}
	r.active[key] = struct{}{}
	return nil
}

func (r *udfRegistry) release(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.active, key)
}

// udfInfoHandler describes the UDF build the POST to the same path would run.
var udfInfoHandler = withUser(func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	if !d.user.Perm.Create {
		return http.StatusForbidden, nil
	}

	dir := r.URL.Path
	if !d.Check(dir) {
		return http.StatusForbidden, nil
	}

	isPackage, err := isUdfPackage(d, dir)
	if err != nil {
		return errToStatus(err), err
	}

	info := udfInfo{Package: isPackage, Versions: []udfInstall{}}
	if !isPackage {
		return renderJSON(w, r, info)
	}

	info.Versions = udfInstalls(convergeAppsDir(d))
	info.HasSource = udfHasSource(d, dir)
	info.LastVersion = udfLastVersion(d, dir)

	return renderJSON(w, r, info)
})

// udfHasSource reports whether the package has anything to compile. An empty
// src/ is not an error here: CONVERGE_BUILD would silently fall back to its own
// samples directory, and the prompt would rather say so first.
func udfHasSource(d *data, dir string) bool {
	srcDir := path.Join(dir, udfSourceDir)
	if !d.CheckRules(srcDir) {
		return false
	}

	entries, err := afero.ReadDir(d.user.Fs, srcDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && entry.Size() > 0 {
			return true
		}
	}
	return false
}

func udfLastVersion(d *data, dir string) string {
	fPath := path.Join(dir, udfBuildDir, udfVersionStamp)
	if !d.CheckRules(fPath) {
		return ""
	}

	content, err := convergeReadSmall(d.user.Fs, fPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

// udfBuildHandler compiles a UDF package against a chosen CONVERGE install and
// streams the compiler's progress as Server-Sent Events.
//
// The work rides the request, as the combine does: cancelling the stream kills
// the build's process group and leaves the build directory where it stopped.
var udfBuildHandler = withUser(func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	if !d.user.Perm.Create {
		return http.StatusForbidden, nil
	}

	dir := r.URL.Path
	if !d.Check(dir) {
		return http.StatusForbidden, nil
	}

	isPackage, err := isUdfPackage(d, dir)
	if err != nil {
		return errToStatus(err), err
	}
	if !isPackage {
		return http.StatusBadRequest, errors.New("not a CONVERGE UDF package")
	}

	var req udfBuildRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err)
		}
	}

	// The version names an entry in the discovered list and is never used as
	// text: everything the build runs comes from the install we matched.
	install, ok := udfFindInstall(udfInstalls(convergeAppsDir(d)), req.Version)
	if !ok {
		return renderClientError(w, http.StatusBadRequest, clientError{
			Code:    "udfUnknownVersion",
			Message: "that CONVERGE version is not installed",
		})
	}

	file, err := files.NewFileInfo(&files.FileOptions{
		Fs:      d.user.Fs,
		Path:    dir,
		Modify:  d.user.Perm.Modify,
		Checker: d,
	})
	if err != nil {
		return errToStatus(err), err
	}
	realDir := file.RealPath()
	if !filepath.IsAbs(realDir) {
		return http.StatusInternalServerError, errors.New("cannot resolve the package directory on disk")
	}

	if err := udfBuilds.acquire(realDir); err != nil {
		status := http.StatusTooManyRequests
		code := "udfBusy"
		if errors.Is(err, errUdfBuilding) {
			status, code = http.StatusConflict, "udfBuilding"
		}
		return renderClientError(w, status, clientError{Code: code, Message: err.Error()})
	}
	defer udfBuilds.release(realDir)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return http.StatusInternalServerError, errors.New("streaming not supported")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	send := func(p udfProgress) {
		payload, _ := json.Marshal(p)
		fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
	}

	runUdfBuild(r.Context(), d, dir, realDir, install, send)
	return 0, nil
})

func udfFindInstall(installs []udfInstall, version string) (udfInstall, bool) {
	if version == "" {
		return udfInstall{}, false
	}
	for _, install := range installs {
		if install.Version == version {
			return install, true
		}
	}
	return udfInstall{}, false
}

// runUdfBuild prepares the build directory, runs the compile and reports what
// happened. It writes its own terminal event, so it never returns an error.
func runUdfBuild(
	ctx context.Context,
	d *data,
	dir, realDir string,
	install udfInstall,
	send func(udfProgress),
) {
	buildDir := path.Join(dir, udfBuildDir)
	logPath := path.Join(dir, udfLogName)

	// cvg_udf_init backs up CMakeLists.txt before refreshing it, so running it
	// on every compile would leave a .back file behind each time. A package that
	// already has a build directory has been through it once.
	runInit := "0"
	if _, err := d.user.Fs.Stat(buildDir); err != nil {
		runInit = "1"
	}

	if err := udfPrepareBuildDir(d, buildDir, filepath.Join(realDir, udfBuildDir)); err != nil {
		send(udfProgress{Phase: udfPhaseDone, Error: err.Error()})
		return
	}

	send(udfProgress{Phase: udfPhaseConfigure})

	output, runErr := udfRunScript(ctx, realDir, install, runInit, send)

	// The log is worth keeping whether the build succeeded, failed or was
	// cancelled: a -Werror wall is far more than an error message can carry.
	if !d.CheckRules(logPath) || afero.WriteFile(d.user.Fs, logPath, output, 0o644) != nil {
		logPath = ""
	}

	if runErr != nil {
		send(udfProgress{
			Phase:   udfPhaseDone,
			Error:   udfFailureMessage(runErr, output),
			LogPath: logPath,
		})
		return
	}

	artifact, err := udfPublishArtifact(d, dir, buildDir)
	if err != nil {
		send(udfProgress{Phase: udfPhaseDone, Error: err.Error(), LogPath: logPath})
		return
	}

	_ = afero.WriteFile(d.user.Fs, path.Join(buildDir, udfVersionStamp), []byte(install.Version), 0o644)

	send(udfProgress{
		Phase:    udfPhaseDone,
		Percent:  100,
		Artifact: artifact,
		LogPath:  logPath,
	})
}

// udfPrepareBuildDir clears a build directory cmake would refuse to reuse.
//
// The cache records the absolute paths it was generated for, so a package that
// has been copied or moved fails to configure until it goes — and copying is
// how these get shared: the reference v6_UDF_Example ships a cache naming a
// workstation none of this ever ran on.
//
// Nothing else is cleared. Changing CONVERGE version needs no wipe, because the
// build always configures before it compiles and that reconfigure rewrites the
// include paths in flags.make, which in turn rebuilds every object.
func udfPrepareBuildDir(d *data, buildDir, realBuildDir string) error {
	content, err := convergeReadSmall(d.user.Fs, path.Join(buildDir, "CMakeCache.txt"))
	if err != nil {
		// No cache, or one too large to be cmake's: nothing stale to clear.
		return nil //nolint:nilerr // an unreadable cache is not a reason to fail the build
	}

	if udfCacheDir(content) == realBuildDir {
		return nil
	}

	if err := d.user.Fs.RemoveAll(buildDir); err != nil {
		return fmt.Errorf("could not clear the stale build directory: %w", err)
	}
	return nil
}

// udfCacheDir reads the build directory a CMakeCache.txt was generated for.
// A cache that does not name one is treated as naming nowhere, so it is cleared
// like any other cache that does not match.
func udfCacheDir(content []byte) string {
	const key = "CMAKE_CACHEFILE_DIR:INTERNAL="

	for line := range strings.Lines(string(content)) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), key); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

func udfPublishArtifact(d *data, dir, buildDir string) (string, error) {
	src := path.Join(buildDir, udfLibName)
	if _, err := d.user.Fs.Stat(src); err != nil {
		return "", fmt.Errorf("the build reported success but produced no %s", udfLibName)
	}

	dst := path.Join(dir, udfLibName)
	if !d.Check(dst) {
		return "", errors.New("cannot write the compiled library to the package directory")
	}

	if err := d.user.Fs.Rename(src, dst); err != nil {
		return "", fmt.Errorf("could not move %s into the package: %w", udfLibName, err)
	}

	return dst, nil
}

// udfRunScript runs the build and returns everything it printed. Output is
// capped so that a compiler looping on diagnostics cannot grow the response
// without bound; progress keeps flowing past the cap, only the kept log stops.
func udfRunScript(
	ctx context.Context,
	realDir string,
	install udfInstall,
	runInit string,
	send func(udfProgress),
) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, udfBuildTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/bash", "-c", udfBuildScript)
	cmd.Dir = realDir
	cmd.Env = append(os.Environ(),
		"CVG_ENV_SCRIPT="+install.envScript,
		"UDF_DIR="+realDir,
		"UDF_RUN_INIT="+runInit,
	)
	// stderr joins stdout so that a compiler error reads in place among the
	// lines that led to it.
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout

	// exec.CommandContext kills only bash; cmake and cc are its children and
	// would carry on compiling after a cancelled request. Their own process
	// group is what makes them killable as a unit.
	udfSetProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			udfKill(cmd)
		case <-done:
		}
	}()

	log := make([]byte, 0, 64*1024)
	phase := udfPhaseConfigure
	percent := 0

	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if len(log) < udfLogLimit {
			log = append(log, line...)
			log = append(log, '\n')
		}

		if match := udfPercent.FindStringSubmatch(line); match != nil {
			phase = udfPhaseBuild
			if n, convErr := strconv.Atoi(match[1]); convErr == nil {
				percent = n
			}
		}
		send(udfProgress{Phase: phase, Percent: percent, Line: line})
	}

	waitErr := cmd.Wait()
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return log, fmt.Errorf("the build did not finish within %s", udfBuildTimeout)
		}
		return log, ctxErr
	}
	return log, waitErr
}

// udfMakeSummary matches the lines make prints on its way out, e.g.
// "gmake[2]: *** [CMakeFiles/...] Error 1". They are always the last thing in a
// failed build and always say less than what failed above them.
var udfMakeSummary = regexp.MustCompile(`^g?make(\[\d+\])?:`)

// udfFailureMessageLimit keeps a single enormous template diagnostic from
// filling the toast it is reported through; the whole thing is in compile.log.
const udfFailureMessageLimit = 300

// udfFailureMessage turns an exit status into something worth showing.
//
// "exit status 2" names nothing, and so does make's own summary — the line that
// matters is the diagnostic above it, which is what a compiler puts its file,
// line and reason on.
func udfFailureMessage(err error, output []byte) string {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return err.Error()
	}

	lines := strings.Split(strings.TrimRight(string(output), "\n"), "\n")

	fallback := ""
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || udfMakeSummary.MatchString(line) {
			continue
		}
		if strings.Contains(strings.ToLower(line), "error") {
			return udfTruncate(line)
		}
		if fallback == "" {
			fallback = line
		}
	}

	if fallback != "" {
		return udfTruncate(fallback)
	}
	return err.Error()
}

func udfTruncate(line string) string {
	runes := []rune(line)
	if len(runes) <= udfFailureMessageLimit {
		return line
	}
	return string(runes[:udfFailureMessageLimit]) + "…"
}
