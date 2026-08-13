package fbhttp

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/spf13/afero"

	fberrors "github.com/rforced/filebrowser/v2/errors"
	"github.com/rforced/filebrowser/v2/files"
)

const convergeCaseFile = "inputs.in"

const convergeOutputDirPrefix = "outputs_"

const convergeOutputDirKind = "outputs"

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
	path  string
	kind  string
	size  int64
	isDir bool
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
				path:  fPath,
				kind:  convergeOutputDirKind,
				size:  convergeDirSize(ctx, d.user.Fs, fPath),
				isDir: true,
			})
			continue
		}

		kind, ok := convergeOutputKind(name)
		if !ok {
			continue
		}

		matches = append(matches, convergeMatch{path: fPath, kind: kind, size: entry.Size()})
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
	if tally, ok := tallies[convergeOutputDirKind]; ok {
		groups = append(groups, *tally)
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

	matches, err := scanConvergeOutputs(r.Context(), d, dir)
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

	matches, err := scanConvergeOutputs(r.Context(), d, dir)
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

		// Files go one at a time, so a directory that took a scanned file's
		// place in the meantime cannot be swept away with it. The outputs_*
		// directories exist only to hold output, so those go whole — Remove
		// would refuse a non-empty one.
		remove := d.user.Fs.Remove
		if match.isDir {
			remove = d.user.Fs.RemoveAll
		}

		err := d.RunHook(func() error {
			return remove(match.path)
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
