package fbhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/asdine/storm/v3"
	"github.com/spf13/afero"

	"github.com/rforced/filebrowser/v2/auth"
	"github.com/rforced/filebrowser/v2/files"
	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/storage/bolt"
	"github.com/rforced/filebrowser/v2/users"
)

// noopFileCache is a FileCache that does nothing, for handlers that only need a
// cache to satisfy their signature.
type noopFileCache struct{}

func (noopFileCache) Store(context.Context, string, []byte) error        { return nil }
func (noopFileCache) Load(context.Context, string) ([]byte, bool, error) { return nil, false, nil }
func (noopFileCache) Delete(context.Context, string) error               { return nil }

// TestResourceCopyDoesNotDereferenceEscapingSymlink drives the real copy handler
// end-to-end. Copying an in-scope directory that contains a symlink whose target
// escapes the user's scope must not exfiltrate the out-of-scope content into the
// destination (GHSA-c2gv-wf5f-hjhh). A legitimate in-scope file in the same
// directory must still be copied, proving the guard does not over-block.
func TestResourceCopyDoesNotDereferenceEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	scope := filepath.Join(root, "user")
	if err := os.MkdirAll(filepath.Join(scope, "srcdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	// An ordinary in-scope file, to prove a normal copy still works.
	if err := os.WriteFile(filepath.Join(scope, "srcdir", "normal.txt"), []byte("in-scope"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The secret living outside the user's scope.
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("OUT-OF-SCOPE-SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}

	// An escaping symlink planted inside the user's scope (out-of-band, as the
	// advisory's preconditions describe).
	if err := os.Symlink(secret, filepath.Join(scope, "srcdir", "link.txt")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	db, err := storm.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	st, err := bolt.NewStorage(db)
	if err != nil {
		t.Fatalf("failed to get storage: %v", err)
	}
	user := &users.User{Username: "u", Password: "pw", Perm: users.Permissions{Create: true, Modify: true}}
	if err := st.Users.Save(user); err != nil {
		t.Fatalf("failed to save user: %v", err)
	}
	if err := st.Settings.Save(&settings.Settings{
		Key:      []byte("test-signing-key"),
		FileMode: settings.DefaultFileMode,
		DirMode:  settings.DefaultDirMode,
	}); err != nil {
		t.Fatalf("failed to save settings: %v", err)
	}
	// customFSUser.Get wraps the scope in a ScopedFs, mirroring production.
	st.Users = &customFSUser{
		Store: st.Users,
		fs:    files.NewScopedFs(afero.NewOsFs(), scope),
	}

	tokenStr, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}
	if err := st.Tokens.Save(&auth.Token{
		Token:     tokenStr,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("failed to save token: %v", err)
	}

	req, err := http.NewRequest(http.MethodPatch, "/api/resources/srcdir/", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	q := req.URL.Query()
	q.Set("action", "copy")
	q.Set("destination", "/dstdir")
	req.URL.RawQuery = q.Encode()
	req.Header.Set("X-Auth", tokenStr)

	rec := httptest.NewRecorder()
	handler := handle(resourcePatchHandler(noopFileCache{}), "/api/resources", st, &settings.Server{})
	handler.ServeHTTP(rec, req)
	t.Logf("copy status=%d body=%q", rec.Code, rec.Body.String())

	// Security invariant: the escaping symlink's target content must never land
	// inside the user's scope.
	leaked := filepath.Join(scope, "dstdir", "link.txt")
	if data, readErr := os.ReadFile(leaked); readErr == nil {
		t.Fatalf("VULNERABLE: out-of-scope content landed in scope at %s: %q", leaked, string(data))
	}

	// The legitimate in-scope file must still have been copied.
	copied := filepath.Join(scope, "dstdir", "normal.txt")
	if data, readErr := os.ReadFile(copied); readErr != nil || string(data) != "in-scope" {
		t.Fatalf("expected in-scope file to be copied to %s, got data=%q err=%v", copied, string(data), readErr)
	}
}
