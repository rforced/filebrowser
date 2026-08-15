package fbhttp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	bbolt "go.etcd.io/bbolt"

	"github.com/rforced/filebrowser/v2/auth"
	"github.com/rforced/filebrowser/v2/diskcache"
	"github.com/rforced/filebrowser/v2/files"
	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/storage"
	"github.com/rforced/filebrowser/v2/storage/bolt"
	"github.com/rforced/filebrowser/v2/users"
)

func scopedUserStorage(t *testing.T, userScope string, perm users.Permissions, key []byte) *storage.Storage {
	t.Helper()
	db, err := bbolt.Open(filepath.Join(t.TempDir(), "db"), 0o600, nil)
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
	if err := st.Tokens.Save(tokenStr, &auth.Token{
		UserID:    1,
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("failed to save token: %v", err)
	}
	return tokenStr
}

// A trailing slash on POST /api/resources means "create a directory", which is
// the path the web UI uses for directory uploads. This covered the after_upload
// hook before the command runner was removed; the directory creation it also
// asserted is still the live behaviour.
func TestResourcePostCreatesDirectory(t *testing.T) {
	root := t.TempDir()
	userScope := filepath.Join(root, "user")
	if err := os.MkdirAll(userScope, 0o755); err != nil {
		t.Fatal(err)
	}

	key := []byte("test-signing-key")
	perm := users.Permissions{Create: true}
	st := scopedUserStorage(t, userScope, perm, key)
	if err := st.Settings.Save(&settings.Settings{Key: key}); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodPost, "/created/", http.NoBody)
	req.Header.Set("X-Auth", issueToken(t, st))
	rec := httptest.NewRecorder()
	handle(resourcePostHandler(diskcache.NewNoOp()), "", st, &settings.Server{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected directory creation to return 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	info, err := os.Stat(filepath.Join(userScope, "created"))
	if err != nil {
		t.Fatalf("expected directory to be created, got %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected a directory")
	}
}

func TestUploadRefusesToOverwriteWithoutOverride(t *testing.T) {
	root := t.TempDir()
	userScope := filepath.Join(root, "user")
	if err := os.MkdirAll(userScope, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userScope, "keep.txt"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	key := []byte("test-signing-key")
	st := scopedUserStorage(t, userScope, users.Permissions{Create: true, Modify: true}, key)
	if err := st.Settings.Save(&settings.Settings{Key: key}); err != nil {
		t.Fatal(err)
	}
	token := issueToken(t, st)

	post := func(query string) *httptest.ResponseRecorder {
		req, _ := http.NewRequest(http.MethodPost, "/keep.txt"+query, strings.NewReader("replacement"))
		req.Header.Set("X-Auth", token)
		rec := httptest.NewRecorder()
		handle(resourcePostHandler(diskcache.NewNoOp()), "", st, &settings.Server{}).ServeHTTP(rec, req)
		return rec
	}

	if rec := post(""); rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 without override, got %d body=%q", rec.Code, rec.Body.String())
	}
	if got, _ := os.ReadFile(filepath.Join(userScope, "keep.txt")); string(got) != "original" {
		t.Fatalf("VULNERABLE: the file was overwritten without override: %q", got)
	}

	if rec := post("?override=true"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with override=true, got %d body=%q", rec.Code, rec.Body.String())
	}
	if got, _ := os.ReadFile(filepath.Join(userScope, "keep.txt")); string(got) != "replacement" {
		t.Errorf("override=true did not write: %q", got)
	}
}
