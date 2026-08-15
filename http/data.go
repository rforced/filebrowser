package fbhttp

import (
	"log"
	"net/http"
	gopath "path"
	"strconv"

	fbAuth "github.com/rforced/filebrowser/v2/auth"
	"github.com/rforced/filebrowser/v2/rules"
	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/storage"
	"github.com/rforced/filebrowser/v2/users"
)

type handleFunc func(w http.ResponseWriter, r *http.Request, d *data) (int, error)

type data struct {
	settings *settings.Settings
	server   *settings.Server
	store    *storage.Storage
	user     *users.User
	raw      interface{}

	// token is the session the request authenticated with, and tokenStr the
	// bearer string it arrived as. Both are set by withUser: handlers need the
	// former to enforce the absolute session lifetime, and the latter to spare
	// the caller's own session when revoking a user's others.
	token    *fbAuth.Token
	tokenStr string

	// checkerPrefix is prepended to every path before evaluating rules. It is
	// set when the user's filesystem has been rebased onto a subdirectory (as
	// done for public shares), so that rules — which are relative to the user's
	// original scope — are still matched against the real path instead of the
	// rebased one. Empty for regular requests.
	checkerPrefix string
}

// Check implements rules.Checker.
func (d *data) Check(path string) bool {
	if d.user.HideDotfiles && rules.MatchHidden(d.rulePath(path)) {
		return false
	}

	return d.CheckRules(path)
}

// CheckRules reports whether the global and user rules allow path. Unlike
// Check, it ignores HideDotfiles: hiding dotfiles is a display preference, so
// it must not stop a user from operating on a tree that contains one.
func (d *data) CheckRules(path string) bool {
	path = d.rulePath(path)

	allow := true
	for _, rule := range d.settings.Rules {
		if rule.Matches(path, d.server.CaseInsensitiveFs) {
			allow = rule.Allow
		}
	}

	for _, rule := range d.user.Rules {
		if rule.Matches(path, d.server.CaseInsensitiveFs) {
			allow = rule.Allow
		}
	}

	return allow
}

// rulePath canonicalizes path into the form the rules are written in.
func (d *data) rulePath(path string) string {
	// Rules are written as "/"-separated virtual paths, but callers hand us
	// paths built by the OS as well as ones taken from the request: afero.Walk
	// and filepath.Join use "\" on Windows, where the filesystem also treats it
	// as a separator. Canonicalize first so the authorization decision does not
	// depend on which separator the caller happened to use.
	path = slashClean(path)

	// When the filesystem has been rebased (e.g. a public share rooted at a
	// subdirectory), the incoming path is relative to that root. Resolve it
	// back to the user's original scope before matching rules, otherwise rules
	// targeting paths below the share root would be silently bypassed.
	if d.checkerPrefix != "" {
		path = gopath.Join(d.checkerPrefix, path)
	}

	return path
}

func handle(fn handleFunc, prefix string, store *storage.Storage, server *settings.Server) http.Handler {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range globalHeaders {
			w.Header().Set(k, v)
		}

		settings, err := store.Settings.Get()
		if err != nil {
			log.Fatalf("ERROR: couldn't get settings: %v\n", err)
			return
		}

		status, err := fn(w, r, &data{
			store:    store,
			settings: settings,
			server:   server,
		})

		if status >= 400 || err != nil {
			log.Printf("%s: %v %s %v", r.URL.Path, status, clientIP(r, server.TrustedProxyNets), err)
		}

		if status != 0 {
			txt := http.StatusText(status)
			http.Error(w, strconv.Itoa(status)+" "+txt, status)
			return
		}
	})

	return stripPrefix(prefix, handler)
}
