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

func convergeDirSize(ctx context.Context, afs afero.Fs, dir string) int64 {
	var total int64

	_ = afero.Walk(afs, dir, func(_ string, info os.FileInfo, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil || info == nil || info.IsDir() {
			return nil //nolint:nilerr // an unreadable entry just does not count
		}
		total += info.Size()
		return nil
	})

	return total
}

func scanConvergeOutputs(ctx context.Context, d *data, dir string) ([]convergeMatch, error) {
	entries, err := afero.ReadDir(d.user.Fs, dir)
	if err != nil {
		return nil, err
	}

	var matches []convergeMatch
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
			if !convergeCanDelete(ctx, d, fPath) {
				log.Printf("INFO: leaving CONVERGE output directory %s: a rule denies part of it", fPath)
				continue
			}

			matches = append(matches, convergeMatch{
				path:    fPath,
				kind:    convergeOutputDirKind,
				size:    convergeDirSize(ctx, d.user.Fs, fPath),
				isDir:   true,
				modTime: entry.ModTime(),
			})
			continue
		}

		kind, ok := convergeOutputKind(name)
		if !ok {
			continue
		}

		matches = append(matches, convergeMatch{
			path:    fPath,
			kind:    kind,
			size:    entry.Size(),
			modTime: entry.ModTime(),
		})
	}

	return matches, nil
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

		return restarts[i].Name > restarts[j].Name
	})

	return restarts
}

type convergeScanResponse struct {
	IsCase   bool              `json:"isCase"`
	Groups   []convergeGroup   `json:"groups"`
	Count    int               `json:"count"`
	Size     int64             `json:"size"`
	Restarts []convergeRestart `json:"restarts"`
}

func groupConvergeMatches(matches []convergeMatch) []convergeGroup {
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
	if tally, ok := tallies[convergeOutputDirKind]; ok {
		groups = append(groups, *tally)
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
	Current float64  `json:"current"`
	Unit    string   `json:"unit"`
	Start   *float64 `json:"start,omitempty"`
	End     *float64 `json:"end,omitempty"`
}

type convergeSummaryResponse struct {
	IsCase       bool              `json:"isCase"`
	Status       string            `json:"status,omitempty"`
	Groups       []convergeGroup   `json:"groups"`
	Count        int               `json:"count"`
	Size         int64             `json:"size"`
	Restarts     []convergeRestart `json:"restarts"`
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

	switch name {
	case convergeStartMarker:
		e := s.run(runDir)
		e.hasStart = true
		if info.ModTime().After(e.start) {
			e.start = info.ModTime()
		}
		return
	case convergeDoneMarker:
		e := s.run(runDir)
		e.hasDone = true
		if info.ModTime().After(e.done) {
			e.done = info.ModTime()
		}
		return
	}

	kind, ok := convergeOutputKind(path.Base(fPath))
	if !ok {
		if countOther {
			s.tally("other", info.Size())
		}
		return
	}

	s.tally(kind, info.Size())

	switch kind {
	case "restart":
		s.restarts = append(s.restarts, convergeMatch{
			path:    fPath,
			kind:    kind,
			size:    info.Size(),
			modTime: info.ModTime(),
		})
	case "out", "log":
		e := s.run(runDir)
		if info.ModTime().After(e.activity) {
			e.activity = info.ModTime()
		}
		if name == convergeLogName {
			if info.ModTime().After(e.logMod) {
				e.logPath = fPath
				e.logMod = info.ModTime()
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
// a marker-less run from an old CONVERGE still outweighs an older finished one.
func (s *convergeSummaryScan) chooseRun() *convergeRunEvidence {
	var chosen *convergeRunEvidence
	chosenDir := ""
	for dir, e := range s.runs {
		if e.stamp().IsZero() {
			continue
		}
		if chosen == nil || e.stamp().After(chosen.stamp()) ||
			(e.stamp().Equal(chosen.stamp()) && dir > chosenDir) {
			chosen, chosenDir = e, dir
		}
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
		return nil, nil, unit
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
		})
	}

	scan, err := summarizeConvergeCase(r.Context(), d, dir)
	if err != nil {
		return errToStatus(err), err
	}

	run := scan.chooseRun()
	var logTail []byte
	if run != nil {
		logTail = convergeReadLogTail(d, run.logPath)
	}
	status, lastActivity := resolveConvergeStatus(run, logTail, time.Now())

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
		Job:          convergeJobFromSpec(d, dir),
		LogPath:      logPath,
		LastActivity: lastActivity,
	}

	start, end, unit := convergeDeckTimes(d, dir)
	if current := convergeLogCurrentTime(logTail, unit); current != nil {
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
			Groups:   []convergeGroup{},
			Restarts: []convergeRestart{},
		})
	}

	matches, err := scanConvergeOutputs(r.Context(), d, dir)
	if err != nil {
		return errToStatus(err), err
	}

	resp := &convergeScanResponse{
		IsCase:   true,
		Groups:   groupConvergeMatches(matches),
		Count:    len(matches),
		Restarts: convergeRestartsFromMatches(matches),
	}
	for i := range matches {
		resp.Size += matches[i].size
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

func filterConvergeMatches(matches []convergeMatch, req *convergeCleanRequest) []convergeMatch {
	if len(req.Kinds) > 0 {
		kept := matches[:0:0]
		for i := range matches {
			if slices.Contains(req.Kinds, matches[i].kind) {
				kept = append(kept, matches[i])
			}
		}
		matches = kept
	}

	if req.KeepRestarts > 0 {
		restarts := convergeRestartsFromMatches(matches)
		spared := map[string]bool{}
		for _, restart := range restarts[:min(req.KeepRestarts, len(restarts))] {
			spared[restart.Path] = true
		}

		kept := matches[:0:0]
		for i := range matches {
			if matches[i].kind == "restart" && spared[matches[i].path] {
				continue
			}
			kept = append(kept, matches[i])
		}
		matches = kept
	}

	return matches
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

	matches, err := scanConvergeOutputs(r.Context(), d, dir)
	if err != nil {
		return errToStatus(err), err
	}
	matches = filterConvergeMatches(matches, cleanReq)

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
