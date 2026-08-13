package fbhttp

import (
	"errors"
	"log"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/spf13/afero"

	"github.com/rforced/filebrowser/v2/files"
)

const convergeCaseFile = "inputs.in"

const convergeOutputDirPrefix = "outputs_"

type convergePattern struct {
	kind     string
	prefix   string
	suffixes []string
}

var convergePatterns = []convergePattern{
	{kind: "echo", suffixes: []string{".echo"}},
	{kind: "restart", prefix: "restart", suffixes: []string{".rst"}},
	{kind: "map", prefix: "map_", suffixes: []string{".h5"}},
	{kind: "out", suffixes: []string{".out"}},
	{kind: "post", prefix: "post", suffixes: []string{".h5", ".cgns"}},
}

func convergeOutputKind(name string) (string, bool) {
	if name == "" || strings.HasPrefix(name, ".") {
		return "", false
	}

	lower := strings.ToLower(name)
	for _, p := range convergePatterns {
		if !strings.HasPrefix(lower, p.prefix) {
			continue
		}

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
	path string
	kind string
	size int64
}

func convergeOutputsIn(d *data, dir string) (matches []convergeMatch, outputDirs []string, err error) {
	entries, err := afero.ReadDir(d.user.Fs, dir)
	if err != nil {
		return nil, nil, err
	}

	for _, entry := range entries {
		name := entry.Name()
		fPath := path.Join(dir, name)

		// Nothing the user cannot see may be swept. The prompt lists exactly
		// what the cleanup will remove, and a rule that hides a path has to
		// deny deleting it just the same.
		if !d.Check(fPath) {
			continue
		}

		// ReadDir describes a symlink itself, never its target. Skipping links
		// keeps the cleanup from unlinking one whose name happens to match
		// while leaving the output it points at in place, and from descending
		// into a directory that actually lives somewhere else.
		if files.IsSymlink(entry.Mode()) {
			continue
		}

		if entry.IsDir() {
			if strings.HasPrefix(strings.ToLower(name), convergeOutputDirPrefix) {
				outputDirs = append(outputDirs, fPath)
			}
			continue
		}

		kind, ok := convergeOutputKind(name)
		if !ok {
			continue
		}

		matches = append(matches, convergeMatch{path: fPath, kind: kind, size: entry.Size()})
	}

	return matches, outputDirs, nil
}

func scanConvergeOutputs(d *data, dir string) ([]convergeMatch, error) {
	matches, outputDirs, err := convergeOutputsIn(d, dir)
	if err != nil {
		return nil, err
	}

	for _, outputDir := range outputDirs {
		// One unreadable outputs_* directory should not stop the rest of the
		// case from being cleaned, so treat it as empty and say so in the log.
		nested, _, err := convergeOutputsIn(d, outputDir)
		if err != nil {
			log.Printf("WARNING: skipping CONVERGE output directory %s: %v", outputDir, err)
			continue
		}
		matches = append(matches, nested...)
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

type convergeScanResponse struct {
	IsCase bool            `json:"isCase"`
	Groups []convergeGroup `json:"groups"`
	Count  int             `json:"count"`
	Size   int64           `json:"size"`
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

	return groups
}

var convergeScanHandler = withUser(func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	// Cleaning is a delete, so previewing one is only offered to users who
	// could go through with it.
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
		return renderJSON(w, r, &convergeScanResponse{Groups: []convergeGroup{}})
	}

	matches, err := scanConvergeOutputs(d, dir)
	if err != nil {
		return errToStatus(err), err
	}

	resp := &convergeScanResponse{
		IsCase: true,
		Groups: groupConvergeMatches(matches),
		Count:  len(matches),
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

var convergeCleanHandler = withUser(func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
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
		return http.StatusBadRequest, errors.New("not a CONVERGE case directory")
	}

	matches, err := scanConvergeOutputs(d, dir)
	if err != nil {
		return errToStatus(err), err
	}

	resp := &convergeCleanResponse{}
	for i := range matches {
		// Deleting a large case is a long loop of filesystem work. Stop as soon
		// as nobody is waiting for the answer rather than running it to the end
		// for a client that has gone.
		if ctxErr := r.Context().Err(); ctxErr != nil {
			return 0, ctxErr
		}

		match := matches[i]

		// Remove, never RemoveAll: every match was a regular file when it was
		// scanned, and refusing to recurse means a directory that took its place
		// in the meantime cannot be swept away along with it.
		err := d.RunHook(func() error {
			return d.user.Fs.Remove(match.path)
		}, "delete", match.path, "", d.user)
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
