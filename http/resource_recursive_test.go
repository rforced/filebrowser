package fbhttp

import (
	"encoding/json"
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

// TestResourceGetRecursiveHandler exercises the recursive listing endpoint used
// by the frontend's conflict detection. It verifies the happy path (a flat,
// recursive listing) and that the fork's symlink hardening is honored: a
// symlink whose target escapes the user's scope must not be disclosed, matching
// readListing's behavior.
func TestResourceGetRecursiveHandler(t *testing.T) {
	root := t.TempDir()
	scope := filepath.Join(root, "user")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(filepath.Join(scope, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scope, "top.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scope, "folder", "nested.txt"), []byte("bb"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A symlink inside the scope pointing outside it.
	symlinks := true
	if err := os.Symlink(outside, filepath.Join(scope, "escape_link")); err != nil {
		t.Logf("symlinks unavailable, skipping escape assertion: %v", err)
		symlinks = false
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
	user := &users.User{Username: "u", Password: "pw"}
	if err := st.Users.Save(user); err != nil {
		t.Fatalf("failed to save user: %v", err)
	}
	if err := st.Settings.Save(&settings.Settings{Key: []byte("test-signing-key")}); err != nil {
		t.Fatalf("failed to save settings: %v", err)
	}
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

	req, err := http.NewRequest(http.MethodGet, "/api/resources/recursive/", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Auth", tokenStr)

	recorder := httptest.NewRecorder()
	handler := handle(resourceGetRecursiveHandler, "/api/resources/recursive", st, &settings.Server{})
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	var entries []RecursiveEntry
	if err := json.Unmarshal(recorder.Body.Bytes(), &entries); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	got := make(map[string]bool, len(entries))
	for _, e := range entries {
		got[e.Path] = true
	}

	for _, want := range []string{"/top.txt", "/folder", "/folder/nested.txt"} {
		if !got[want] {
			t.Errorf("expected entry %q in recursive listing, got %v", want, entries)
		}
	}

	if symlinks {
		if got["/escape_link"] {
			t.Errorf("VULNERABLE: scope-escaping symlink was disclosed in recursive listing")
		}
		// The escaped target's contents must never be reachable through the link.
		if got["/escape_link/secret.txt"] {
			t.Errorf("VULNERABLE: recursive listing descended into scope-escaping symlink")
		}
	}
}
