package fbhttp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/afero"

	"github.com/rforced/filebrowser/v2/files"
)

const convergeCombinedDir = "outputs_combined"

const (
	combineMaxFiles = 20000
	combineMaxBytes = 20 * 1024 * 1024 * 1024 // 20 GB
	combineMaxLine  = 1024 * 1024
)

type combineResponse struct {
	Path  string `json:"path"`
	Files int    `json:"files"`
	Legs  int    `json:"legs"`
	Bytes int64  `json:"bytes"`
}

// combineLegPreview is one leg as the confirmation prompt lists it.
type combineLegPreview struct {
	Name  string `json:"name"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

// combinePreviewResponse describes the combine a POST to the same path would
// perform, in the order the legs will be joined. Bytes is what the sources
// hold, so it is an upper bound on the result: the seams only ever drop rows.
type combinePreviewResponse struct {
	Name   string              `json:"name"`
	Legs   []combineLegPreview `json:"legs"`
	Files  int                 `json:"files"`
	Bytes  int64               `json:"bytes"`
	Exists bool                `json:"exists"`
}

// combineLeg is one outputs_* tree taking part in a combine, with the position
// its name gives it in the restart chain.
type combineLeg struct {
	name string
	path string
	rank int
	seq  int
}

// combineLegRank orders the legs the way CONVERGE writes them: outputs_original
// opens the chain, outputs_restart<N> follows in numeric order, and anything a
// user named by hand trails behind, ordered by name. Numeric ordering matters
// because restart10 must not sort above restart2.
func combineLegRank(name string) (rank, seq int) {
	lower := strings.ToLower(name)

	rest, ok := strings.CutPrefix(lower, convergeOutputDirPrefix)
	if !ok {
		return 2, math.MaxInt
	}
	if rest == "original" {
		return 0, 0
	}
	if digits, found := strings.CutPrefix(rest, "restart"); found {
		if n, err := strconv.Atoi(digits); err == nil {
			return 1, n
		}
	}
	return 2, math.MaxInt
}

func sortCombineLegs(legs []combineLeg) {
	sort.Slice(legs, func(i, j int) bool {
		if legs[i].rank != legs[j].rank {
			return legs[i].rank < legs[j].rank
		}
		if legs[i].seq != legs[j].seq {
			return legs[i].seq < legs[j].seq
		}
		return legs[i].name < legs[j].name
	})
}

// convergeCombineLegs lists the outputs_* trees that may feed a combine, in
// chain order. The combined directory is never a source: taking it would fold
// an earlier result back into the next one.
func convergeCombineLegs(d *data, dir string) ([]combineLeg, error) {
	entries, err := afero.ReadDir(d.user.Fs, dir)
	if err != nil {
		return nil, err
	}

	legs := make([]combineLeg, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || files.IsSymlink(entry.Mode()) {
			continue
		}

		name := entry.Name()
		lower := strings.ToLower(name)
		if !strings.HasPrefix(lower, convergeOutputDirPrefix) || lower == convergeCombinedDir {
			continue
		}

		fPath := path.Join(dir, name)
		if !d.CheckRules(fPath) {
			continue
		}

		rank, seq := combineLegRank(name)
		legs = append(legs, combineLeg{name: name, path: fPath, rank: rank, seq: seq})
	}

	sortCombineLegs(legs)
	return legs, nil
}

// combineWalkLeg visits every .out file in one leg, handing back the path each
// one takes inside it. The preview a prompt shows and the combine it confirms
// both see their files through here, so what gets listed is what gets written.
func combineWalkLeg(
	ctx context.Context,
	d *data,
	leg combineLeg,
	visit func(fPath, rel string, info os.FileInfo),
) error {
	prefix := leg.path + "/"

	return afero.Walk(d.user.Fs, leg.path, func(fPath string, info os.FileInfo, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil || info == nil || info.IsDir() {
			return nil //nolint:nilerr // an unreadable entry simply does not take part
		}
		if files.IsSymlink(info.Mode()) || !d.CheckRules(fPath) {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(info.Name()), ".out") {
			return nil
		}

		rel, ok := strings.CutPrefix(fPath, prefix)
		if !ok || rel == "" {
			return nil
		}

		visit(fPath, rel, info)
		return nil
	})
}

// combineSources gathers every .out file in the legs, keyed by its path
// relative to the leg root. That key carries the whole of the layout logic: it
// puts stream0/thermo.out beside its counterparts in the other legs, keeps a
// second stream apart, and handles the files CONVERGE writes at the leg root
// (memory_usage.out, cell_count_ranks.out, walltime.out) without naming any of
// them. Paths come back sorted so a combine is reproducible.
func combineSources(ctx context.Context, d *data, legs []combineLeg) (map[string][]string, []string, error) {
	sources := map[string][]string{}
	order := []string{}

	for _, leg := range legs {
		err := combineWalkLeg(ctx, d, leg, func(fPath, rel string, _ os.FileInfo) {
			if _, seen := sources[rel]; !seen {
				order = append(order, rel)
			}
			sources[rel] = append(sources[rel], fPath)
		})
		if err != nil {
			return nil, nil, err
		}
	}

	sort.Strings(order)
	return sources, order, nil
}

// combineFirstTime reads the first data row's leading column, which is the
// solver's own progression axis and so the time a leg picks up from. A file
// with no readable row reports false, which leaves the seam before it uncut.
func combineFirstTime(afs afero.Fs, fPath string) (float64, bool) {
	f, err := afs.Open(fPath)
	if err != nil {
		return 0, false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), combineMaxLine)
	for scanner.Scan() {
		if value, ok := combineRowTime(scanner.Text()); ok {
			return value, true
		}
	}
	return 0, false
}

// combineRowTime reports the leading column of a data row. Comment and blank
// lines are not data. A row whose first field will not parse is still a row: it
// is copied, it just cannot be compared against a seam.
func combineRowTime(line string) (float64, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return 0, false
	}

	field := trimmed
	if cut := strings.IndexAny(trimmed, " \t"); cut > 0 {
		field = trimmed[:cut]
	}

	value, err := strconv.ParseFloat(field, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

// combineState prices the whole combine, so a case that would fill the disk is
// caught on the aggregate rather than per file.
type combineState struct {
	files int
	bytes int64
}

func (s *combineState) charge(n int) error {
	s.bytes += int64(n)
	if s.bytes > combineMaxBytes {
		return errors.New("combined output exceeds the maximum total size")
	}
	return nil
}

// combineOutFile writes one relative path's legs into a single file, oldest leg
// first.
//
// The seam rule is the one stitchOutTables applies in the plotter: a leg
// contributes rows up to, but not including, the first row at or past where the
// next leg picks up. The newer leg wins, which drops both the checkpoint row it
// re-prints verbatim and any rows from a trajectory a backtracking restart
// abandoned. A combined file therefore reads exactly like the chain view of the
// same output.
func combineOutFile(ctx context.Context, afs afero.Fs, sources []string, target string, state *combineState) error {
	if err := afs.MkdirAll(path.Dir(target), 0750); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", path.Dir(target), err)
	}

	out, err := afs.OpenFile(target, writeFileFlags(), 0640)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", path.Base(target), err)
	}
	defer out.Close()

	if err := combineStream(ctx, afs, sources, out, state); err != nil {
		return err
	}
	return nil
}

// combineStream writes the joined legs to w. Splitting it out from the file
// case is what lets one .out be combined straight down a response body: the
// download a user takes from the chain view and the file a combine leaves on
// disk come off the same code, so they cannot drift apart.
func combineStream(ctx context.Context, afs afero.Fs, sources []string, w io.Writer, state *combineState) error {
	buffered := bufio.NewWriter(w)

	// Each leg stops where the one after it starts.
	cutoffs := make([]float64, len(sources))
	hasCutoff := make([]bool, len(sources))
	for i := 1; i < len(sources); i++ {
		cutoffs[i-1], hasCutoff[i-1] = combineFirstTime(afs, sources[i])
	}

	for i, source := range sources {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err := combineAppendLeg(ctx, afs, source, buffered, i == 0, cutoffs[i], hasCutoff[i], state); err != nil {
			return err
		}
	}

	if err := buffered.Flush(); err != nil {
		return fmt.Errorf("failed to write combined output: %w", err)
	}
	return nil
}

// combineAppendLeg copies one leg's rows into the open combined file. Only the
// first leg contributes a header; every other comment line is dropped, which
// also takes care of the header a restart re-prints part-way through a file.
//
// Rows are copied byte for byte. time.out closes each one with a free-text
// dt_limiter tag introduced by a hash, and that tag is part of the row.
func combineAppendLeg(
	ctx context.Context,
	afs afero.Fs,
	source string,
	out *bufio.Writer,
	withHeader bool,
	cutoff float64,
	hasCutoff bool,
	state *combineState,
) error {
	f, err := afs.Open(source)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", source, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), combineMaxLine)

	lines := 0
	inHeader := true

	write := func(line string) error {
		if _, err := out.WriteString(line); err != nil {
			return err
		}
		if err := out.WriteByte('\n'); err != nil {
			return err
		}
		return state.charge(len(line) + 1)
	}

	for scanner.Scan() {
		lines++
		if lines%4096 == 0 {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
		}

		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			if inHeader && withHeader {
				if err := write(line); err != nil {
					return err
				}
			}
			continue
		}
		inHeader = false

		if hasCutoff {
			if value, ok := combineRowTime(line); ok && value >= cutoff {
				break
			}
		}

		if err := write(line); err != nil {
			return err
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read %s: %w", source, err)
	}
	return nil
}

// combineViewHandler answers for the combine of whatever the path names: a
// case directory describes the combine a POST would run, a single .out file
// streams its own joined form. Both are reads, and neither writes anything.
var combineViewHandler = withUser(func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	target := r.URL.Path
	if !d.Check(target) {
		return http.StatusForbidden, nil
	}

	info, err := d.user.Fs.Stat(target)
	if err != nil {
		return errToStatus(err), err
	}

	if info.IsDir() {
		return combinePreviewResult(w, r, d, target)
	}
	return combineFileResult(w, r, d, target, info.Name())
})

// combinePreviewResult lists the legs a combine would join, in the order it
// would join them. A case with too few legs is not an error here: the prompt
// asking the question is better placed to say so than a failed request.
func combinePreviewResult(w http.ResponseWriter, r *http.Request, d *data, dir string) (int, error) {
	if !d.user.Perm.Create {
		return http.StatusForbidden, nil
	}

	isCase, err := isConvergeCase(d, dir)
	if err != nil {
		return errToStatus(err), err
	}
	if !isCase {
		return http.StatusBadRequest, errors.New("not a CONVERGE case directory")
	}

	legs, err := convergeCombineLegs(d, dir)
	if err != nil {
		return errToStatus(err), err
	}

	resp := &combinePreviewResponse{
		Name: convergeCombinedDir,
		Legs: make([]combineLegPreview, 0, len(legs)),
	}

	distinct := map[string]struct{}{}
	for _, leg := range legs {
		view := combineLegPreview{Name: leg.name}
		err := combineWalkLeg(r.Context(), d, leg, func(_, rel string, info os.FileInfo) {
			view.Files++
			view.Bytes += info.Size()
			distinct[rel] = struct{}{}
		})
		if err != nil {
			return errToStatus(err), err
		}

		resp.Legs = append(resp.Legs, view)
		resp.Bytes += view.Bytes
	}
	resp.Files = len(distinct)

	_, statErr := d.user.Fs.Stat(path.Join(dir, convergeCombinedDir))
	switch {
	case statErr == nil:
		resp.Exists = true
	case !os.IsNotExist(statErr):
		return errToStatus(statErr), statErr
	}

	return renderJSON(w, r, resp)
}

// combineFileResult streams one .out file joined across its whole chain. This
// is what the plotter's full-chain view is looking at, so downloading from
// there hands over the file being plotted rather than the single leg it was
// opened from — and every leg takes part, including the ones the plotter left
// out to stay inside its own byte budget.
func combineFileResult(w http.ResponseWriter, r *http.Request, d *data, fPath, name string) (int, error) {
	if !d.user.Perm.Download {
		return http.StatusForbidden, nil
	}
	if !strings.HasSuffix(strings.ToLower(name), ".out") {
		return http.StatusBadRequest, errors.New("not a CONVERGE output file")
	}

	caseRoot, remainder, ok := combineCaseFor(fPath)
	if !ok {
		return http.StatusBadRequest, errors.New("file is not inside a CONVERGE output directory")
	}
	if !d.Check(caseRoot) {
		return http.StatusForbidden, nil
	}

	isCase, err := isConvergeCase(d, caseRoot)
	if err != nil {
		return errToStatus(err), err
	}
	if !isCase {
		return http.StatusBadRequest, errors.New("not a CONVERGE case directory")
	}

	sources, err := combineFileSources(d, caseRoot, remainder)
	if err != nil {
		return errToStatus(err), err
	}
	if len(sources) == 0 {
		return http.StatusNotFound, nil
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition",
		"attachment; filename*=utf-8''"+url.PathEscape(combinedFileName(name)))

	// The body is on its way from here, so a failure part-way through can only
	// end the response; the status has already gone out.
	if err := combineStream(r.Context(), d.user.Fs, sources, w, &combineState{}); err != nil {
		return 0, err
	}
	return 0, nil
}

// combineCaseFor locates the case a .out file belongs to by climbing to the
// leg directory above it — the outputs_* name CONVERGE writes a run into. The
// case is that leg's parent, and what hangs below the leg is the path the same
// output takes in every other leg.
func combineCaseFor(fPath string) (caseRoot, remainder string, ok bool) {
	for dir := path.Dir(fPath); dir != "" && dir != "/" && dir != "."; dir = path.Dir(dir) {
		name := strings.ToLower(path.Base(dir))
		if !strings.HasPrefix(name, convergeOutputDirPrefix) || name == convergeCombinedDir {
			continue
		}
		return path.Dir(dir), strings.TrimPrefix(fPath, dir), true
	}
	return "", "", false
}

// combineFileSources lists one output across every leg that wrote it, in chain
// order. A leg that never wrote the file simply sits the join out.
func combineFileSources(d *data, caseRoot, remainder string) ([]string, error) {
	legs, err := convergeCombineLegs(d, caseRoot)
	if err != nil {
		return nil, err
	}

	sources := make([]string, 0, len(legs))
	for _, leg := range legs {
		fPath := leg.path + remainder
		if !d.CheckRules(fPath) {
			continue
		}

		info, err := combineLstat(d.user.Fs, fPath)
		if err != nil || info.IsDir() || files.IsSymlink(info.Mode()) {
			continue
		}
		sources = append(sources, fPath)
	}

	return sources, nil
}

// combineLstat looks at the entry itself rather than what it points at, which
// is what the leg walk sees; a symlinked leg file is skipped by both.
func combineLstat(afs afero.Fs, fPath string) (os.FileInfo, error) {
	if lstater, ok := afs.(afero.Lstater); ok {
		info, _, err := lstater.LstatIfPossible(fPath)
		return info, err
	}
	return afs.Stat(fPath)
}

// combinedFileName names the download after the folder a whole-case combine
// would write, so thermo.out arrives as thermo_combined.out and does not land
// on top of the single-leg file already in the user's downloads.
func combinedFileName(name string) string {
	ext := path.Ext(name)
	return strings.TrimSuffix(name, ext) + "_combined" + ext
}

// combineHandler joins every .out file across a case's outputs_* legs into a
// single outputs_combined tree.
//
// The work rides the request: nothing is queued and nothing outlives the
// client. When the browser goes away the request context is cancelled, the
// partial tree is removed, and the case is left as it was.
var combineHandler = withUser(func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	if !d.user.Perm.Create {
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
		return http.StatusBadRequest, errors.New("not a CONVERGE case directory")
	}

	legs, err := convergeCombineLegs(d, dir)
	if err != nil {
		return errToStatus(err), err
	}
	if len(legs) < 2 {
		return renderClientError(w, http.StatusBadRequest, clientError{
			Code:    "combineNeedsRuns",
			Message: "this case has fewer than two output directories to combine",
		})
	}

	target := path.Join(dir, convergeCombinedDir)
	if !d.Check(target) {
		return http.StatusForbidden, nil
	}

	_, statErr := d.user.Fs.Stat(target)
	switch {
	case statErr == nil:
		return renderClientError(w, http.StatusConflict, clientError{
			Code:    "combinedExists",
			Message: convergeCombinedDir + " already exists",
			Params:  map[string]string{"name": convergeCombinedDir},
		})
	case !os.IsNotExist(statErr):
		return errToStatus(statErr), statErr
	}

	resp, err := performCombine(r.Context(), d, target, legs)
	if err != nil {
		// A half-written tree is worse than none: it reads as a real run and
		// blocks the next attempt.
		_ = d.user.Fs.RemoveAll(target)

		if errors.Is(err, context.Canceled) {
			return 0, nil
		}
		return errToStatus(err), err
	}

	return renderJSON(w, r, resp)
})

func performCombine(ctx context.Context, d *data, target string, legs []combineLeg) (*combineResponse, error) {
	sources, order, err := combineSources(ctx, d, legs)
	if err != nil {
		return nil, err
	}
	if len(order) == 0 {
		return nil, errors.New("no .out files found in the output directories")
	}
	if len(order) > combineMaxFiles {
		return nil, fmt.Errorf("the output directories hold more than %d .out files", combineMaxFiles)
	}

	if err := d.user.Fs.MkdirAll(target, 0750); err != nil {
		return nil, err
	}

	state := &combineState{}
	for _, rel := range order {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}

		out, err := resolveInside(target, rel)
		if err != nil {
			return nil, err
		}
		if !d.Check(out) {
			continue
		}

		if err := combineOutFile(ctx, d.user.Fs, sources[rel], out, state); err != nil {
			return nil, err
		}
		state.files++
	}

	return &combineResponse{
		Path:  target,
		Files: state.files,
		Legs:  len(legs),
		Bytes: state.bytes,
	}, nil
}
