package fbhttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/afero"

	fberrors "github.com/rforced/filebrowser/v2/errors"
	"github.com/rforced/filebrowser/v2/files"
)

const convergeCaseFile = "inputs.in"

const convergeOutputDirPrefix = "outputs_"

const convergeOutputDirKind = "outputs"

const (
	convergeStartMarker = "converge.start"
	convergeDoneMarker  = "converge.done"
	convergeLogName     = "converge.log"
	convergeJobSpec     = "horizon.json"

	convergeActiveWindow = 10 * time.Minute

	convergeLogTailBytes = 64 * 1024

	convergeSmallFileLimit = 512 * 1024

	convergeEndEpsilon = 1e-6

	convergeMaxRunLogReads = 16
)

type convergePattern struct {
	kind     string
	prefix   string
	suffixes []string
	names    []string
}

var convergePatterns = []convergePattern{
	{kind: "echo", suffixes: []string{".echo"}},
	{kind: "restart", prefix: "restart", suffixes: []string{".rst"}},
	{kind: "map", prefix: "map_", suffixes: []string{".h5"}},
	{kind: "out", suffixes: []string{".out"}},
	{kind: "post", prefix: "post", suffixes: []string{".h5", ".cgns"}},
	{kind: "log", suffixes: []string{".log"}},
	{kind: "run", names: []string{"horizon.json", "hosts"}},
	{kind: "nfs", prefix: ".nfs"},
}

func convergeOutputKind(name string) (string, bool) {
	if name == "" {
		return "", false
	}

	hidden := strings.HasPrefix(name, ".")
	lower := strings.ToLower(name)

	for _, p := range convergePatterns {
		// A glob only matches a dotfile when the pattern spells the dot out, so
		// "*.out" must not sweep in a hidden file while ".nfs*" must.
		if hidden && !strings.HasPrefix(p.prefix, ".") {
			continue
		}

		if len(p.names) > 0 {
			if slices.Contains(p.names, lower) {
				return p.kind, true
			}
			continue
		}

		if !strings.HasPrefix(lower, p.prefix) {
			continue
		}

		// No suffix means the pattern ends in the wildcard: the prefix decides.
		if len(p.suffixes) == 0 {
			return p.kind, true
		}

		// Match the suffix against what is left after the prefix, so the two
		// cannot overlap. "*" may expand to nothing, which makes "restart.rst"
		// a match, but ".rst" on its own must not be one.
		rest := lower[len(p.prefix):]
		for _, suffix := range p.suffixes {
			if strings.HasSuffix(rest, suffix) {
				return p.kind, true
			}
		}
	}

	return "", false
}

type convergeMatch struct {
	path    string
	kind    string
	size    int64
	isDir   bool
	modTime time.Time
}

func convergeCanDelete(ctx context.Context, d *data, dir string) bool {
	if len(d.settings.Rules) == 0 && len(d.user.Rules) == 0 {
		return true
	}

	err := afero.Walk(d.user.Fs, dir, func(fPath string, _ os.FileInfo, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			return err
		}
		if !d.CheckRules(fPath) {
			return fberrors.ErrPermissionDenied
		}
		return nil
	})

	return err == nil
}

// convergeOutputsRun is one outputs_* tree — one leg of a restart chain — as a
// cleanup subject: its kind-matching files as individual units, plus the tree
// as a whole unit carrying everything removing it would free.
type convergeOutputsRun struct {
	path  string
	name  string
	stamp time.Time
	total int64
	files int
	deep  []convergeMatch
	// Whether a rule keeps the directory from going whole. Its files stay
	// individually deletable either way.
	deletable bool
}

// convergeScanResult splits what a clean can touch into its deletion units:
// files at the case root, and the outputs_* runs. Runs are ordered newest
// first, which is the order keepRuns counts in.
type convergeScanResult struct {
	root []convergeMatch
	runs []convergeOutputsRun
}

// convergeScanOutputsTree walks one outputs_* tree, filling in its
// kind-matching files and its total footprint. The total counts every file —
// symlinks and rule-denied entries included — because it prices removing the
// directory whole, while the file list holds only what may be deleted
// individually.
//
// Sizes here are allocated, not logical: these numbers are shown to someone
// deciding what to delete, and on ZFS with compression the two differ by up to
// 7x for exactly the kinds people clean most (*.out, *.log).
func convergeScanOutputsTree(ctx context.Context, d *data, run *convergeOutputsRun) {
	_ = afero.Walk(d.user.Fs, run.path, func(fPath string, info os.FileInfo, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil || info == nil || info.IsDir() {
			return nil //nolint:nilerr // an unreadable entry just does not count
		}

		run.total += files.AllocatedSize(info)
		run.files++
		if info.ModTime().After(run.stamp) {
			run.stamp = info.ModTime()
		}

		if files.IsSymlink(info.Mode()) {
			return nil
		}
		if !d.CheckRules(fPath) {
			return nil
		}

		kind, ok := convergeOutputKind(path.Base(fPath))
		if !ok {
			return nil
		}

		run.deep = append(run.deep, convergeMatch{
			path:    fPath,
			kind:    kind,
			size:    files.AllocatedSize(info),
			modTime: info.ModTime(),
		})
		return nil
	})
}

func scanConvergeOutputs(ctx context.Context, d *data, dir string) (*convergeScanResult, error) {
	entries, err := afero.ReadDir(d.user.Fs, dir)
	if err != nil {
		return nil, err
	}

	res := &convergeScanResult{}
	for _, entry := range entries {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}

		name := entry.Name()
		fPath := path.Join(dir, name)

		// The rules are the boundary, not d.Check: hiding dotfiles is a display
		// preference, and the .nfs* leftovers are exactly the hidden files this
		// cleanup exists to collect. Same call checkDescendants makes.
		if !d.CheckRules(fPath) {
			continue
		}

		// ReadDir describes a symlink itself, never its target. Skipping links
		// keeps the cleanup from unlinking one whose name happens to match
		// while leaving the output it points at in place, and from deleting a
		// tree that actually lives somewhere else.
		if files.IsSymlink(entry.Mode()) {
			continue
		}

		if entry.IsDir() {
			if !strings.HasPrefix(strings.ToLower(name), convergeOutputDirPrefix) {
				continue
			}

			run := convergeOutputsRun{path: fPath, name: name, stamp: entry.ModTime()}
			convergeScanOutputsTree(ctx, d, &run)

			run.deletable = convergeCanDelete(ctx, d, fPath)
			if !run.deletable {
				log.Printf("INFO: leaving CONVERGE output directory %s: a rule denies part of it", fPath)
			}

			res.runs = append(res.runs, run)
			continue
		}

		kind, ok := convergeOutputKind(name)
		if !ok {
			continue
		}

		res.root = append(res.root, convergeMatch{
			path:    fPath,
			kind:    kind,
			size:    files.AllocatedSize(entry),
			modTime: entry.ModTime(),
		})
	}

	// Newest first, so keepRuns counts down the chain from the leg a resubmit
	// would pick up from.
	sort.Slice(res.runs, func(i, j int) bool {
		if !res.runs[i].stamp.Equal(res.runs[j].stamp) {
			return res.runs[i].stamp.After(res.runs[j].stamp)
		}
		return res.runs[i].name > res.runs[j].name
	})

	return res, nil
}

func isConvergeCase(d *data, dir string) (bool, error) {
	info, err := d.user.Fs.Stat(dir)
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, nil
	}

	marker := path.Join(dir, convergeCaseFile)
	if !d.Check(marker) {
		return false, nil
	}

	markerInfo, err := d.user.Fs.Stat(marker)
	switch {
	case err == nil:
		return !markerInfo.IsDir(), nil
	case os.IsNotExist(err), errors.Is(err, os.ErrPermission):
		// No deck, or one behind a symlink leaving the user's scope. Either way
		// this is not a case directory we are willing to clean.
		return false, nil
	default:
		return false, err
	}
}

type convergeGroup struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
	Size  int64  `json:"size"`
	// The case-root share of Count/Size. The clean prompt needs the split
	// because selecting "outputs" folders subsumes the files inside them,
	// leaving only the root share for the other kinds.
	RootCount int   `json:"rootCount,omitempty"`
	RootSize  int64 `json:"rootSize,omitempty"`
}

type convergeRestart struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

func convergeRestartsFromMatches(matches []convergeMatch) []convergeRestart {
	restarts := []convergeRestart{}
	for i := range matches {
		if matches[i].kind != "restart" {
			continue
		}
		restarts = append(restarts, convergeRestart{
			Name:     path.Base(matches[i].path),
			Path:     matches[i].path,
			Size:     matches[i].size,
			Modified: matches[i].modTime,
		})
	}

	sort.Slice(restarts, func(i, j int) bool {
		if !restarts[i].Modified.Equal(restarts[j].Modified) {
			return restarts[i].Modified.After(restarts[j].Modified)
		}

		// Same mtime happens whenever a case is restored from an archive that
		// flattened them, and then the name is all there is to go on. Compare
		// the digits as a number: lexically, restart2 outranks restart0010.
		if ni, nj := convergeRestartIndex(restarts[i].Name), convergeRestartIndex(restarts[j].Name); ni != nj {
			return ni > nj
		}
		return restarts[i].Name > restarts[j].Name
	})

	return restarts
}

// convergeRestartIndex is the run of digits in a restart's name, or -1 when it
// has none — "restart.rst", which CONVERGE writes for the latest one.
func convergeRestartIndex(name string) int64 {
	digits := strings.TrimSuffix(name, ".rst")
	start := strings.IndexFunc(digits, func(r rune) bool { return r >= '0' && r <= '9' })
	if start < 0 {
		return -1
	}
	end := start
	for end < len(digits) && digits[end] >= '0' && digits[end] <= '9' {
		end++
	}
	n, err := strconv.ParseInt(digits[start:end], 10, 64)
	if err != nil {
		return -1
	}
	return n
}

// convergeOutputDir is one outputs_* run as the clean prompt sees it. Groups
// holds only that run's own files, so sparing it can be priced without a second
// scan.
type convergeOutputDir struct {
	Name      string          `json:"name"`
	Path      string          `json:"path"`
	Size      int64           `json:"size"`
	Count     int             `json:"count"`
	Modified  time.Time       `json:"modified"`
	Deletable bool            `json:"deletable"`
	Groups    []convergeGroup `json:"groups"`
}

type convergeScanResponse struct {
	IsCase     bool                `json:"isCase"`
	Groups     []convergeGroup     `json:"groups"`
	Count      int                 `json:"count"`
	Size       int64               `json:"size"`
	Restarts   []convergeRestart   `json:"restarts"`
	OutputDirs []convergeOutputDir `json:"outputDirs"`
}

func convergeOutputDirs(res *convergeScanResult) []convergeOutputDir {
	dirs := make([]convergeOutputDir, 0, len(res.runs))
	for i := range res.runs {
		run := &res.runs[i]
		dirs = append(dirs, convergeOutputDir{
			Name:      run.name,
			Path:      run.path,
			Size:      run.total,
			Count:     run.files,
			Modified:  run.stamp,
			Deletable: run.deletable,
			Groups:    convergeGroupsOf(run.deep),
		})
	}
	return dirs
}

func groupConvergeScan(res *convergeScanResult) []convergeGroup {
	tallies := make(map[string]*convergeGroup, len(convergePatterns))
	add := func(m *convergeMatch, root bool) {
		tally, ok := tallies[m.kind]
		if !ok {
			tally = &convergeGroup{Kind: m.kind}
			tallies[m.kind] = tally
		}
		tally.Count++
		tally.Size += m.size
		if root {
			tally.RootCount++
			tally.RootSize += m.size
		}
	}

	for i := range res.root {
		add(&res.root[i], true)
	}
	for i := range res.runs {
		for j := range res.runs[i].deep {
			add(&res.runs[i].deep[j], false)
		}
	}

	groups := make([]convergeGroup, 0, len(tallies)+1)
	for _, p := range convergePatterns {
		if tally, ok := tallies[p.kind]; ok {
			groups = append(groups, *tally)
		}
	}

	dirs := convergeGroup{Kind: convergeOutputDirKind}
	for i := range res.runs {
		if !res.runs[i].deletable {
			continue
		}
		dirs.Count++
		dirs.Size += res.runs[i].total
	}
	if dirs.Count > 0 {
		groups = append(groups, dirs)
	}

	return groups
}

// convergeGroupsOf tallies one run's own files by kind, so the prompt can price
// what sparing that run subtracts from the sweep.
func convergeGroupsOf(matches []convergeMatch) []convergeGroup {
	tallies := make(map[string]*convergeGroup, len(convergePatterns))
	for i := range matches {
		tally, ok := tallies[matches[i].kind]
		if !ok {
			tally = &convergeGroup{Kind: matches[i].kind}
			tallies[matches[i].kind] = tally
		}
		tally.Count++
		tally.Size += matches[i].size
	}

	groups := make([]convergeGroup, 0, len(tallies))
	for _, p := range convergePatterns {
		if tally, ok := tallies[p.kind]; ok {
			groups = append(groups, *tally)
		}
	}
	return groups
}

type horizonJobSpec struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	AppKey       string `json:"app_key"`
	AppVersion   string `json:"app_version"`
	CoresPerNode int    `json:"cores_per_node"`
	NodesCount   int    `json:"nodes_count"`
}

type convergeJobInfo struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name,omitempty"`
	AppKey       string `json:"appKey,omitempty"`
	AppVersion   string `json:"appVersion,omitempty"`
	CoresPerNode int    `json:"coresPerNode,omitempty"`
	NodesCount   int    `json:"nodesCount,omitempty"`
}

type convergeProgress struct {
	Current float64 `json:"current"`
	// Absent when the deck could not be read: the client renders the number
	// bare rather than labelling it with a guess.
	Unit  string   `json:"unit,omitempty"`
	Start *float64 `json:"start,omitempty"`
	End   *float64 `json:"end,omitempty"`
}

// convergeRun is one leg of a restart chain: a single outputs_* tree. From
// CONVERGE 6 a case accumulates one per launch (outputs_original,
// outputs_restart1, …), each holding only the slice of the solve it ran.
type convergeRun struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Status   string    `json:"status"`
	Size     int64     `json:"size"`
	Count    int       `json:"count"`
	Modified time.Time `json:"modified"`
	LogPath  string    `json:"logPath,omitempty"`
	// The simulation time this leg reached, read from its own log. The leg
	// before it supplies the time it started from, so the chain reads as a
	// contiguous span without every run having to name its own beginning.
	End *float64 `json:"end,omitempty"`
}

type convergeSummaryResponse struct {
	IsCase       bool              `json:"isCase"`
	Status       string            `json:"status,omitempty"`
	Groups       []convergeGroup   `json:"groups"`
	Count        int               `json:"count"`
	Size         int64             `json:"size"`
	Restarts     []convergeRestart `json:"restarts"`
	Runs         []convergeRun     `json:"runs"`
	Job          *convergeJobInfo  `json:"job,omitempty"`
	LogPath      string            `json:"logPath,omitempty"`
	LastActivity *time.Time        `json:"lastActivity,omitempty"`
	Progress     *convergeProgress `json:"progress,omitempty"`
}

type convergeRunEvidence struct {
	start    time.Time
	done     time.Time
	hasStart bool
	hasDone  bool
	activity time.Time
	logPath  string
	logMod   time.Time

	// Set only for an outputs_* tree: the case root is a run too in the old
	// flat layout, but it is not a leg of a restart chain anyone can point at.
	outputDir bool
	name      string
	dirMod    time.Time
	count     int
	size      int64
}

// order is what sorts a restart chain. It is stamp() widened to the directory's
// own mtime so a run whose files were all swept still keeps its place, and is
// deliberately not what chooseRun weighs — an empty folder must not outrank a
// run that actually produced something.
func (e *convergeRunEvidence) order() time.Time {
	if e.dirMod.After(e.stamp()) {
		return e.dirMod
	}
	return e.stamp()
}

func (e *convergeRunEvidence) stamp() time.Time {
	stamp := e.activity
	for _, t := range []time.Time{e.start, e.done, e.logMod} {
		if t.After(stamp) {
			stamp = t
		}
	}
	return stamp
}

type convergeSummaryScan struct {
	tallies  map[string]*convergeGroup
	count    int
	size     int64
	restarts []convergeMatch

	logPath string
	logMod  time.Time

	runs map[string]*convergeRunEvidence
}

func newConvergeSummaryScan() *convergeSummaryScan {
	return &convergeSummaryScan{
		tallies: map[string]*convergeGroup{},
		runs:    map[string]*convergeRunEvidence{},
	}
}

func (s *convergeSummaryScan) run(dir string) *convergeRunEvidence {
	e, ok := s.runs[dir]
	if !ok {
		e = &convergeRunEvidence{}
		s.runs[dir] = e
	}
	return e
}

func (s *convergeSummaryScan) tally(kind string, size int64) {
	tally, ok := s.tallies[kind]
	if !ok {
		tally = &convergeGroup{Kind: kind}
		s.tallies[kind] = tally
	}
	tally.Count++
	tally.Size += size
	s.count++
	s.size += size
}

func (s *convergeSummaryScan) file(runDir, fPath string, info os.FileInfo, countOther bool) {
	name := strings.ToLower(path.Base(fPath))

	run := s.run(runDir)
	run.count++
	run.size += files.AllocatedSize(info)

	switch name {
	case convergeStartMarker:
		run.hasStart = true
		if info.ModTime().After(run.start) {
			run.start = info.ModTime()
		}
		return
	case convergeDoneMarker:
		run.hasDone = true
		if info.ModTime().After(run.done) {
			run.done = info.ModTime()
		}
		return
	}

	kind, ok := convergeOutputKind(path.Base(fPath))
	if !ok {
		if countOther {
			s.tally("other", files.AllocatedSize(info))
		}
		return
	}

	s.tally(kind, files.AllocatedSize(info))

	switch kind {
	case "restart":
		s.restarts = append(s.restarts, convergeMatch{
			path:    fPath,
			kind:    kind,
			size:    files.AllocatedSize(info),
			modTime: info.ModTime(),
		})
	case "out", "log":
		if info.ModTime().After(run.activity) {
			run.activity = info.ModTime()
		}
		if name == convergeLogName {
			if info.ModTime().After(run.logMod) {
				run.logPath = fPath
				run.logMod = info.ModTime()
			}
			if info.ModTime().After(s.logMod) {
				s.logPath = fPath
				s.logMod = info.ModTime()
			}
		}
	}
}

func (s *convergeSummaryScan) groups() []convergeGroup {
	groups := make([]convergeGroup, 0, len(s.tallies))
	for _, p := range convergePatterns {
		if tally, ok := s.tallies[p.kind]; ok {
			groups = append(groups, *tally)
		}
	}
	if tally, ok := s.tallies["other"]; ok {
		groups = append(groups, *tally)
	}
	return groups
}

// chooseRun picks the run directory with the newest evidence of any kind, so
// a marker-less run from an old CONVERGE still outweighs an older finished
// one. CONVERGE 6 writes one outputs_* directory per run (outputs_original,
// outputs_restart1, …); the case root is only itself a run in the old flat
// layout. When outputs_* runs exist, the root must show run-shaped evidence —
// a marker or a converge.log — to compete: Horizon's job files at the root
// (horizon.log, mech_check.out) are touched after the solver's last write and
// would otherwise pose as a newer run that never produced a verdict.
func (s *convergeSummaryScan) chooseRun(caseDir string) *convergeRunEvidence {
	var chosen *convergeRunEvidence
	chosenDir := ""
	consider := func(dir string, e *convergeRunEvidence) {
		if e == nil || e.stamp().IsZero() {
			return
		}
		if chosen == nil || e.stamp().After(chosen.stamp()) ||
			(e.stamp().Equal(chosen.stamp()) && dir > chosenDir) {
			chosen, chosenDir = e, dir
		}
	}

	for dir, e := range s.runs {
		if dir != caseDir {
			consider(dir, e)
		}
	}

	root := s.runs[caseDir]
	if root != nil &&
		(chosen == nil || root.hasStart || root.hasDone || root.logPath != "") {
		consider(caseDir, root)
	}

	return chosen
}

func resolveConvergeStatus(e *convergeRunEvidence, logTail []byte, now time.Time) (string, *time.Time) {
	if e == nil {
		return "idle", nil
	}

	stamp := e.stamp()
	switch {
	case e.hasDone:
		return "completed", &stamp
	case convergeLogSaysComplete(logTail):
		return "completed", &stamp
	case now.Sub(stamp) <= convergeActiveWindow:
		return "running", &stamp
	case e.hasStart || e.logPath != "":
		return "interrupted", &stamp
	default:
		return "idle", nil
	}
}

// convergeNeedsRestart separates a solve that reached end_time from one that
// only reached its wall-clock limit. CONVERGE writes "normal termination" and a
// restart file for both, so the marker alone reads as success on a case that is
// really a leg of a chain waiting to be resubmitted.
func convergeNeedsRestart(status string, current, start, end *float64) bool {
	if status != "completed" || current == nil || end == nil {
		return false
	}

	span := *end
	if start != nil {
		span = *end - *start
	}
	if span <= 0 {
		return false
	}

	return *end-*current > span*convergeEndEpsilon
}

func convergeWalkRunDir(ctx context.Context, d *data, runDir, dir string, scan *convergeSummaryScan) {
	_ = afero.Walk(d.user.Fs, dir, func(fPath string, info os.FileInfo, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil || info == nil {
			return nil //nolint:nilerr // an unreadable entry just does not count
		}
		if info.IsDir() {
			return nil
		}
		if !d.CheckRules(fPath) {
			return nil
		}
		scan.file(runDir, fPath, info, true)
		return nil
	})
}

func summarizeConvergeCase(ctx context.Context, d *data, dir string) (*convergeSummaryScan, error) {
	entries, err := afero.ReadDir(d.user.Fs, dir)
	if err != nil {
		return nil, err
	}

	scan := newConvergeSummaryScan()

	for _, entry := range entries {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}

		name := entry.Name()
		fPath := path.Join(dir, name)

		if !d.CheckRules(fPath) {
			continue
		}
		if files.IsSymlink(entry.Mode()) {
			continue
		}

		if entry.IsDir() {
			lower := strings.ToLower(name)
			switch {
			case strings.HasPrefix(lower, convergeOutputDirPrefix):
				run := scan.run(fPath)
				run.outputDir = true
				run.name = name
				run.dirMod = entry.ModTime()
				convergeWalkRunDir(ctx, d, fPath, fPath, scan)
			case lower == "output" || lower == "stream0":
				convergeWalkRunDir(ctx, d, dir, fPath, scan)
			}
			continue
		}

		scan.file(dir, fPath, entry, false)
	}

	return scan, nil
}

// convergeRuns lays out the restart chain newest-first, matching Restarts, and
// gives each leg its own verdict. The needsRestart refinement is deliberately
// not applied here: end_time moves as a chain is extended, so only the newest
// leg can be judged against the deck sitting in the case today.
//
// Only the newest few legs pay for a log read — the simulation time a leg
// reached lives at the end of its own converge.log, and a long chain is not
// worth that many seeks.
func convergeRuns(d *data, scan *convergeSummaryScan, unit string, now time.Time) []convergeRun {
	dirs := make([]string, 0, len(scan.runs))
	for dir, e := range scan.runs {
		if e.outputDir {
			dirs = append(dirs, dir)
		}
	}

	sort.Slice(dirs, func(i, j int) bool {
		a, b := scan.runs[dirs[i]].order(), scan.runs[dirs[j]].order()
		if !a.Equal(b) {
			return a.After(b)
		}
		return dirs[i] > dirs[j]
	})

	runs := make([]convergeRun, 0, len(dirs))
	for i, dir := range dirs {
		e := scan.runs[dir]

		var tail []byte
		if i < convergeMaxRunLogReads {
			tail = convergeReadLogTail(d, e.logPath)
		}

		status, _ := resolveConvergeStatus(e, tail, now)
		runs = append(runs, convergeRun{
			Name:     e.name,
			Path:     dir,
			Status:   status,
			Size:     e.size,
			Count:    e.count,
			Modified: e.order(),
			LogPath:  e.logPath,
			End:      convergeLogCurrentTime(tail, unit),
		})
	}

	return runs
}

func convergeReadSmall(afs afero.Fs, fPath string) ([]byte, error) {
	f, err := afs.Open(fPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > convergeSmallFileLimit {
		return nil, errors.New("file too large")
	}

	return io.ReadAll(io.LimitReader(f, convergeSmallFileLimit))
}

func convergeJobFromSpec(d *data, dir string) *convergeJobInfo {
	fPath := path.Join(dir, convergeJobSpec)
	if !d.CheckRules(fPath) {
		return nil
	}

	raw, err := convergeReadSmall(d.user.Fs, fPath)
	if err != nil {
		return nil
	}

	var spec horizonJobSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return nil
	}

	return &convergeJobInfo{
		ID:           spec.ID,
		Name:         spec.Name,
		AppKey:       spec.AppKey,
		AppVersion:   spec.AppVersion,
		CoresPerNode: spec.CoresPerNode,
		NodesCount:   spec.NodesCount,
	}
}

func convergeDeckTimes(d *data, dir string) (start, end *float64, unit string) {
	unit = "s"

	raw, err := convergeReadSmall(d.user.Fs, path.Join(dir, convergeCaseFile))
	if err != nil {
		// Without the deck there is nothing that says which unit this case
		// runs in. "s" is only CONVERGE's default for a deck we have actually
		// read; guessing it here would label crank degrees as seconds.
		return nil, nil, ""
	}

	parse := func(fields []string) *float64 {
		if len(fields) < 2 {
			return nil
		}
		v, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return nil
		}
		return &v
	}

	for line := range strings.Lines(string(raw)) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "start_time:":
			start = parse(fields)
		case "end_time:":
			end = parse(fields)
		case "crank_flag:":
			if len(fields) > 1 && (fields[1] == "1" || fields[1] == "2") {
				unit = "deg"
			}
		}
	}

	return start, end, unit
}

func convergeReadLogTail(d *data, logPath string) []byte {
	if logPath == "" || !d.CheckRules(logPath) {
		return nil
	}

	f, err := d.user.Fs.Open(logPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil
	}
	if info.Size() > convergeLogTailBytes {
		if _, err := f.Seek(info.Size()-convergeLogTailBytes, io.SeekStart); err != nil {
			return nil
		}
	}

	tail, err := io.ReadAll(io.LimitReader(f, convergeLogTailBytes))
	if err != nil {
		return nil
	}
	return tail
}

var convergeCompletionMarks = []string{"normal termination"}

func convergeLogSaysComplete(tail []byte) bool {
	if len(tail) == 0 {
		return false
	}
	lower := strings.ToLower(string(tail))
	for _, mark := range convergeCompletionMarks {
		if strings.Contains(lower, mark) {
			return true
		}
	}
	return false
}

func convergeLogCurrentTime(tail []byte, unit string) *float64 {
	key := "time="
	if unit == "deg" {
		key = "crank="
	}

	lines := strings.Split(string(tail), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if !strings.Contains(line, "dt=") {
			continue
		}
		idx := strings.Index(line, key)
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(line[idx+len(key):])
		if cut := strings.IndexAny(rest, ", \t"); cut >= 0 {
			rest = rest[:cut]
		}
		v, err := strconv.ParseFloat(rest, 64)
		if err != nil {
			return nil
		}
		return &v
	}

	return nil
}

var convergeSummaryHandler = func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	dir := r.URL.Path
	if !d.Check(dir) {
		return http.StatusForbidden, nil
	}

	isCase, err := isConvergeCase(d, dir)
	if err != nil {
		return errToStatus(err), err
	}
	if !isCase {
		return renderJSON(w, r, &convergeSummaryResponse{
			Groups:   []convergeGroup{},
			Restarts: []convergeRestart{},
			Runs:     []convergeRun{},
		})
	}

	scan, err := summarizeConvergeCase(r.Context(), d, dir)
	if err != nil {
		return errToStatus(err), err
	}

	now := time.Now()

	run := scan.chooseRun(dir)
	var logTail []byte
	if run != nil {
		logTail = convergeReadLogTail(d, run.logPath)
	}
	status, lastActivity := resolveConvergeStatus(run, logTail, now)

	start, end, unit := convergeDeckTimes(d, dir)
	current := convergeLogCurrentTime(logTail, unit)
	if convergeNeedsRestart(status, current, start, end) {
		status = "needsRestart"
	}

	logPath := scan.logPath
	if run != nil && run.logPath != "" {
		logPath = run.logPath
	}

	resp := &convergeSummaryResponse{
		IsCase:       true,
		Status:       status,
		Groups:       scan.groups(),
		Count:        scan.count,
		Size:         scan.size,
		Restarts:     convergeRestartsFromMatches(scan.restarts),
		Runs:         convergeRuns(d, scan, unit, now),
		Job:          convergeJobFromSpec(d, dir),
		LogPath:      logPath,
		LastActivity: lastActivity,
	}

	if current != nil {
		resp.Progress = &convergeProgress{
			Current: *current,
			Unit:    unit,
			Start:   start,
			End:     end,
		}
	}

	return renderJSON(w, r, resp)
}

var convergeScanHandler = withUser(func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	if r.URL.Query().Get("summary") == "true" {
		return convergeSummaryHandler(w, r, d)
	}

	if !d.user.Perm.Delete {
		return http.StatusForbidden, nil
	}

	dir := r.URL.Path
	if !d.Check(dir) {
		return http.StatusForbidden, nil
	}

	isCase, err := isConvergeCase(d, dir)
	if err != nil {
		return errToStatus(err), err
	}
	if !isCase {
		return renderJSON(w, r, &convergeScanResponse{
			Groups:     []convergeGroup{},
			Restarts:   []convergeRestart{},
			OutputDirs: []convergeOutputDir{},
		})
	}

	res, err := scanConvergeOutputs(r.Context(), d, dir)
	if err != nil {
		return errToStatus(err), err
	}

	matches := slices.Clone(res.root)
	for i := range res.runs {
		matches = append(matches, res.runs[i].deep...)
	}

	// Count and Size price the full sweep: root files plus each outputs_*
	// tree taken whole, so the deep files are already inside the dir totals.
	resp := &convergeScanResponse{
		IsCase:     true,
		Groups:     groupConvergeScan(res),
		Count:      len(res.root),
		Restarts:   convergeRestartsFromMatches(matches),
		OutputDirs: convergeOutputDirs(res),
	}
	for i := range res.root {
		resp.Size += res.root[i].size
	}
	for i := range res.runs {
		if !res.runs[i].deletable {
			continue
		}
		resp.Count++
		resp.Size += res.runs[i].total
	}

	return renderJSON(w, r, resp)
})

type convergeCleanResponse struct {
	Deleted int   `json:"deleted"`
	Size    int64 `json:"size"`
	Failed  int   `json:"failed"`
}

type convergeCleanRequest struct {
	Kinds        []string `json:"kinds"`
	KeepRestarts int      `json:"keepRestarts"`
	// The newest N outputs_* runs are left untouched — neither removed whole
	// nor picked at file by file. This is the "drop the superseded legs, keep
	// the one a resubmit continues from" sweep.
	KeepRuns int `json:"keepRuns"`
}

func parseConvergeCleanRequest(r *http.Request) (*convergeCleanRequest, error) {
	req := &convergeCleanRequest{}

	raw, err := io.ReadAll(io.LimitReader(r.Body, convergeSmallFileLimit))
	if err != nil {
		return nil, err
	}

	if len(raw) == 0 {
		return req, nil
	}

	if err := json.Unmarshal(raw, req); err != nil {
		return nil, err
	}
	if req.KeepRestarts < 0 {
		return nil, errors.New("keepRestarts must not be negative")
	}
	if req.KeepRuns < 0 {
		return nil, errors.New("keepRuns must not be negative")
	}

	known := make(map[string]bool, len(convergePatterns)+1)
	for _, p := range convergePatterns {
		known[p.kind] = true
	}
	known[convergeOutputDirKind] = true

	for _, kind := range req.Kinds {
		if !known[kind] {
			return nil, errors.New("unknown CONVERGE output kind: " + kind)
		}
	}

	return req, nil
}

// assembleConvergeClean turns a scan into the deletion units the request asks
// for. Selecting "outputs" takes each outputs_* tree whole — contents
// included — so the deep files inside them only become individual units when
// the folders themselves are spared. The newest KeepRuns runs are skipped
// either way. An empty kind list keeps the original contract: everything,
// folders whole.
func assembleConvergeClean(res *convergeScanResult, req *convergeCleanRequest) []convergeMatch {
	selected := func(kind string) bool {
		return len(req.Kinds) == 0 || slices.Contains(req.Kinds, kind)
	}
	wholeDirs := selected(convergeOutputDirKind)

	var units []convergeMatch
	for i := range res.runs {
		if i < req.KeepRuns {
			continue
		}

		run := &res.runs[i]
		if wholeDirs {
			if run.deletable {
				units = append(units, convergeMatch{
					path:    run.path,
					kind:    convergeOutputDirKind,
					size:    run.total,
					isDir:   true,
					modTime: run.stamp,
				})
			}
			continue
		}

		for j := range run.deep {
			if selected(run.deep[j].kind) {
				units = append(units, run.deep[j])
			}
		}
	}

	for i := range res.root {
		if selected(res.root[i].kind) {
			units = append(units, res.root[i])
		}
	}

	return applyKeepRestarts(units, req.KeepRestarts)
}

// applyKeepRestarts spares the newest keep restart files among the assembled
// units. Restarts inside outputs_* trees taken whole are not units and cannot
// be spared — the folder subsumes them.
func applyKeepRestarts(units []convergeMatch, keep int) []convergeMatch {
	if keep <= 0 {
		return units
	}

	restarts := convergeRestartsFromMatches(units)
	spared := map[string]bool{}
	for _, restart := range restarts[:min(keep, len(restarts))] {
		spared[restart.Path] = true
	}

	kept := units[:0:0]
	for i := range units {
		if units[i].kind == "restart" && spared[units[i].path] {
			continue
		}
		kept = append(kept, units[i])
	}
	return kept
}

var convergeCleanHandler = withUser(func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	if !d.user.Perm.Delete {
		return http.StatusForbidden, nil
	}

	dir := r.URL.Path
	if !d.Check(dir) {
		return http.StatusForbidden, nil
	}

	cleanReq, err := parseConvergeCleanRequest(r)
	if err != nil {
		return http.StatusBadRequest, err
	}

	isCase, err := isConvergeCase(d, dir)
	if err != nil {
		return errToStatus(err), err
	}
	if !isCase {
		return http.StatusBadRequest, errors.New("not a CONVERGE case directory")
	}

	res, err := scanConvergeOutputs(r.Context(), d, dir)
	if err != nil {
		return errToStatus(err), err
	}
	matches := assembleConvergeClean(res, cleanReq)

	resp := &convergeCleanResponse{}
	for i := range matches {
		if ctxErr := r.Context().Err(); ctxErr != nil {
			return 0, ctxErr
		}

		match := matches[i]

		remove := d.user.Fs.Remove
		if match.isDir {
			remove = d.user.Fs.RemoveAll
		}

		err := remove(match.path)
		if err != nil {
			log.Printf("WARNING: could not delete CONVERGE output %s: %v", match.path, err)
			resp.Failed++
			continue
		}

		resp.Deleted++
		resp.Size += match.size

		// A share still aimed at a path we just freed is the dangling link that
		// sharePostHandler refuses to issue, so it goes with the file.
		if err := d.store.Share.DeleteWithPathPrefix(match.path, d.user.ID); err != nil {
			log.Printf("WARNING: Error(s) occurred while deleting associated shares with file: %s", err)
		}
	}

	return renderJSON(w, r, resp)
})
