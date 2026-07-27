package fbhttp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rforced/filebrowser/v2/diskcache"
	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/share"
	"github.com/rforced/filebrowser/v2/users"
)

// A share records the path it was created for, and sharePostHandler will only
// issue one for a path that exists — precisely so that a link can never point at
// a path something else might later occupy. Renaming has to carry the share
// along or it reopens that hole: the old path is free again, and whatever lands
// there next inherits the link.
func TestRenameMovesSharesWithTheFile(t *testing.T) {
	root := t.TempDir()
	userScope := filepath.Join(root, "user")
	if err := os.MkdirAll(filepath.Join(userScope, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userScope, "docs", "secret.txt"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	st := scopedUserStorage(t, userScope, users.Permissions{Rename: true, Modify: true}, []byte("k"))
	for _, link := range []*share.Link{
		{Hash: "on-dir", Path: "/docs", UserID: 1, PasswordHash: "$2a$10$hash"},
		{Hash: "on-file", Path: "/docs/secret.txt", UserID: 1, PasswordHash: "$2a$10$hash"},
	} {
		if err := st.Share.Save(link); err != nil {
			t.Fatal(err)
		}
	}

	req, _ := http.NewRequest(http.MethodPatch, "/docs?action=rename&destination=/archive", http.NoBody)
	req.Header.Set("X-Auth", issueToken(t, st))
	rec := httptest.NewRecorder()
	handle(resourcePatchHandler(diskcache.NewNoOp()), "", st, &settings.Server{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("rename status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	for hash, want := range map[string]string{
		"on-dir":  "/archive",
		"on-file": "/archive/secret.txt",
	} {
		link, err := st.Share.GetByHash(hash)
		if err != nil {
			t.Fatalf("share %q missing after rename: %v", hash, err)
		}
		if link.Path != want {
			t.Errorf("share %q path = %q, want %q", hash, link.Path, want)
		}
	}
}
