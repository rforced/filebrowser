package fbhttp

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path"
	"sort"

	"github.com/spf13/afero"

	"github.com/rforced/filebrowser/v2/files"
)

// usageCancelInterval bounds how often a breakdown walk polls for
// cancellation, for the same reason DirSize does: the poll takes a lock and a
// filesystem holding a few hundred thousand solver outputs is a long stat
// storm.
const usageCancelInterval = 512

// UsageEntry is one row of a directory breakdown — a direct child of the
// scanned directory with its whole subtree folded in.
type UsageEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	// Size is allocated space, so this is what removing the entry reclaims and
	// what ranking by "biggest" ought to mean. LogicalSize rides along so the
	// UI can show the compression ratio instead of leaving the two numbers
	// looking like a contradiction.
	Size        int64 `json:"size"`
	LogicalSize int64 `json:"logicalSize"`
	NumFiles    int64 `json:"numFiles"`
	NumDirs     int64 `json:"numDirs"`
}

// UsageKind tallies one CONVERGE output family across the whole scanned tree,
// cutting across the per-directory rows. It answers "how much of this is
// post*.h5" when the answer is spread over a dozen cases, which is the form
// the clean prompt can act on directly.
type UsageKind struct {
	Kind        string `json:"kind"`
	Count       int64  `json:"count"`
	Size        int64  `json:"size"`
	LogicalSize int64  `json:"logicalSize"`
}

type UsageBreakdown struct {
	Size        int64        `json:"size"`
	LogicalSize int64        `json:"logicalSize"`
	NumFiles    int64        `json:"numFiles"`
	NumDirs     int64        `json:"numDirs"`
	Children    []UsageEntry `json:"children"`
	Kinds       []UsageKind  `json:"kinds,omitempty"`
}

// usageScan accumulates a breakdown across one directory's children.
type usageScan struct {
	ctx   context.Context
	fs    afero.Fs
	seen  int
	kinds map[string]*UsageKind
}

// check polls for cancellation every usageCancelInterval entries.
func (s *usageScan) check() error {
	s.seen++
	if s.seen%usageCancelInterval != 0 {
		return nil
	}
	return s.ctx.Err()
}

func (s *usageScan) tallyKind(name string, info os.FileInfo) {
	if s.kinds == nil {
		return
	}

	kind, ok := convergeOutputKind(name)
	if !ok {
		kind = "other"
	}

	k := s.kinds[kind]
	if k == nil {
		k = &UsageKind{Kind: kind}
		s.kinds[kind] = k
	}
	k.Count++
	k.Size += files.AllocatedSize(info)
	k.LogicalSize += info.Size()
}

// walkChild folds an entire subtree into one row of the breakdown.
func (s *usageScan) walkChild(root string, entry *UsageEntry) error {
	return afero.Walk(s.fs, root, func(fPath string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil //nolint:nilerr // an unreadable entry simply does not count
		}
		if cErr := s.check(); cErr != nil {
			return cErr
		}

		// Directory blocks count toward the total — du counts them and they are
		// ~9KB apiece on ZFS — but a directory has no logical size worth
		// summing, and the root is the row itself rather than something in it.
		entry.Size += files.AllocatedSize(info)
		if info.IsDir() {
			if fPath != root {
				entry.NumDirs++
			}
			return nil
		}

		entry.LogicalSize += info.Size()
		entry.NumFiles++
		s.tallyKind(path.Base(fPath), info)
		return nil
	})
}

// usageBreakdown totals each child of dir, so a caller can see which one is
// actually consuming the filesystem rather than opening them one at a time.
// Rows come back biggest-first, by allocated size.
//
// Entries hidden by a rule are skipped at the top level but still counted
// inside a child's subtree, matching what DirSize has always done: the
// aggregate reveals no names, and excluding them would make these totals
// disagree with du for no benefit.
func usageBreakdown(ctx context.Context, d *data, dir string, wantKinds bool) (*UsageBreakdown, error) {
	entries, err := afero.ReadDir(d.user.Fs, dir)
	if err != nil {
		return nil, err
	}

	scan := &usageScan{ctx: ctx, fs: d.user.Fs}
	if wantKinds {
		scan.kinds = map[string]*UsageKind{}
	}

	resp := &UsageBreakdown{Children: []UsageEntry{}}

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		name := entry.Name()
		fPath := path.Join(dir, name)

		if !d.Check(fPath) {
			continue
		}

		// Never follow a link. du does not, and billing this directory for
		// bytes that live somewhere else would double-count them against the
		// filesystem total.
		if files.IsSymlink(entry.Mode()) {
			continue
		}

		row := UsageEntry{Name: name, IsDir: entry.IsDir()}

		if entry.IsDir() {
			if err := scan.walkChild(fPath, &row); err != nil {
				return nil, err
			}
		} else {
			row.Size = files.AllocatedSize(entry)
			row.LogicalSize = entry.Size()
			row.NumFiles = 1
			scan.tallyKind(name, entry)
		}

		resp.Size += row.Size
		resp.LogicalSize += row.LogicalSize
		resp.NumFiles += row.NumFiles
		resp.NumDirs += row.NumDirs
		if row.IsDir {
			resp.NumDirs++
		}

		resp.Children = append(resp.Children, row)
	}

	// Biggest first: the whole point is to put the offender at the top.
	sort.Slice(resp.Children, func(i, j int) bool {
		if resp.Children[i].Size != resp.Children[j].Size {
			return resp.Children[i].Size > resp.Children[j].Size
		}
		return resp.Children[i].Name < resp.Children[j].Name
	})

	for _, k := range scan.kinds {
		resp.Kinds = append(resp.Kinds, *k)
	}
	sort.Slice(resp.Kinds, func(i, j int) bool {
		if resp.Kinds[i].Size != resp.Kinds[j].Size {
			return resp.Kinds[i].Size > resp.Kinds[j].Size
		}
		return resp.Kinds[i].Kind < resp.Kinds[j].Kind
	})

	return resp, nil
}

var usageBreakdownHandler = withUser(func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	file, err := files.NewFileInfo(&files.FileOptions{
		Fs:         d.user.Fs,
		Path:       r.URL.Path,
		Modify:     d.user.Perm.Modify,
		Expand:     false,
		ReadHeader: false,
		Checker:    d,
		Content:    false,
	})
	if err != nil {
		return errToStatus(err), err
	}
	if !file.IsDir {
		return http.StatusBadRequest, errors.New("not a directory")
	}

	breakdown, err := usageBreakdown(r.Context(), d, file.Path, r.URL.Query().Get("kinds") == "true")
	if err != nil {
		// The client is already gone; there is nothing to write a status to.
		if r.Context().Err() != nil {
			return 0, err
		}
		return http.StatusInternalServerError, err
	}

	return renderJSON(w, r, breakdown)
})
