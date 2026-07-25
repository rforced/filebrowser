package fbhttp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/asdine/storm/v3"
	"github.com/spf13/afero"

	"github.com/rforced/filebrowser/v2/auth"
	"github.com/rforced/filebrowser/v2/diskcache"
	"github.com/rforced/filebrowser/v2/files"
	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/storage"
	"github.com/rforced/filebrowser/v2/storage/bolt"
	"github.com/rforced/filebrowser/v2/users"
)

// scopedUserStorage returns a storage whose single user (ID 1) is scoped to
// userScope through a symlink-confining ScopedFs (via customFSUser), mirroring
// production. The signature matches upstream's helper so upstream test files
// port over with only their token line adjusted.
func scopedUserStorage(t *testing.T, userScope string, perm users.Permissions, key []byte) *storage.Storage {
	t.Helper()
	db, err := storm.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	st, err := bolt.NewStorage(db)
	if err != nil {
		t.Fatalf("failed to get storage: %v", err)
	}
	if err := st.Users.Save(&users.User{Username: "u", Password: "pw", Perm: perm}); err != nil {
		t.Fatalf("failed to save user: %v", err)
	}
	if err := st.Settings.Save(&settings.Settings{Key: key}); err != nil {
		t.Fatalf("failed to save settings: %v", err)
	}
	st.Users = &customFSUser{
		Store: st.Users,
		fs:    files.NewScopedFs(afero.NewOsFs(), userScope),
	}
	return st
}

// issueToken is our fork's replacement for upstream's signToken: we use
// server-side opaque tokens instead of stateless JWTs, so the token has to be
// persisted in the store rather than signed with the settings key.
func issueToken(t *testing.T, st *storage.Storage) string {
	t.Helper()
	tokenStr, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}
	if err := st.Tokens.Save(&auth.Token{
		Token:     tokenStr,
		UserID:    1,
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("failed to save token: %v", err)
	}
	return tokenStr
}

func TestResourcePostRunsUploadHooksForDirectories(t *testing.T) {
	root := t.TempDir()
	userScope := filepath.Join(root, "user")
	if err := os.MkdirAll(userScope, 0o755); err != nil {
		t.Fatal(err)
	}

	key := []byte("test-signing-key")
	perm := users.Permissions{Create: true}
	st := scopedUserStorage(t, userScope, perm, key)
	if err := st.Settings.Save(&settings.Settings{
		Key: key,
		Commands: map[string][]string{
			"after_upload": {"filebrowser-hook-command-that-does-not-exist"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodPost, "/created/", http.NoBody)
	req.Header.Set("X-Auth", issueToken(t, st))
	rec := httptest.NewRecorder()
	handle(resourcePostHandler(diskcache.NewNoOp()), "", st, &settings.Server{EnableExec: true}).ServeHTTP(rec, req)

	// A missing after_upload command makes the request fail only if the hook ran.
	// It avoids a platform-specific helper executable while still exercising the
	// same path the web UI uses for directory uploads.
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected directory upload hook failure to return 500, got %d body=%q", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(userScope, "created")); err != nil {
		t.Fatalf("expected directory to be created before its after hook, got %v", err)
	}
}
