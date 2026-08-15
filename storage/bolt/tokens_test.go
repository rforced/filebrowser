package bolt

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	bbolt "go.etcd.io/bbolt"

	"github.com/rforced/filebrowser/v2/auth"
	fberrors "github.com/rforced/filebrowser/v2/errors"
)

func newTestTokenStore(t *testing.T) auth.TokenStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close db: %v", err)
		}
	})
	return tokenBackend{db: db}
}

func TestTokenStore_SaveAndGet(t *testing.T) {
	t.Parallel()
	store := newTestTokenStore(t)

	token := &auth.Token{
		UserID:    1,
		ExpiresAt: time.Now().Add(1 * time.Hour),
		CreatedAt: time.Now(),
	}

	if err := store.Save("test-token-123", token); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	got, err := store.Get("test-token-123")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}

	if got.Hash != auth.HashToken("test-token-123") {
		t.Errorf("Hash = %q, want %q", got.Hash, auth.HashToken("test-token-123"))
	}
	if got.UserID != token.UserID {
		t.Errorf("UserID = %d, want %d", got.UserID, token.UserID)
	}
}

func TestTokenStore_GetNotFound(t *testing.T) {
	t.Parallel()
	store := newTestTokenStore(t)

	_, err := store.Get("nonexistent")
	if err != fberrors.ErrNotExist {
		t.Errorf("Get() error = %v, want %v", err, fberrors.ErrNotExist)
	}
}

func TestTokenStore_Delete(t *testing.T) {
	t.Parallel()
	store := newTestTokenStore(t)

	token := &auth.Token{
		UserID:    1,
		ExpiresAt: time.Now().Add(1 * time.Hour),
		CreatedAt: time.Now(),
	}
	if err := store.Save("to-delete", token); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	if err := store.Delete("to-delete"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	_, err := store.Get("to-delete")
	if err != fberrors.ErrNotExist {
		t.Errorf("Get() after Delete() error = %v, want %v", err, fberrors.ErrNotExist)
	}
}

func TestTokenStore_DeleteNonexistent(t *testing.T) {
	t.Parallel()
	store := newTestTokenStore(t)

	// Deleting a nonexistent token should not error
	if err := store.Delete("nonexistent"); err != nil {
		t.Errorf("Delete() of nonexistent token error: %v", err)
	}
}

func TestTokenStore_DeleteByUser(t *testing.T) {
	t.Parallel()
	store := newTestTokenStore(t)

	// Save tokens for two different users
	for raw, userID := range map[string]uint{
		"user1-tok1": 1,
		"user1-tok2": 1,
		"user2-tok1": 2,
	} {
		tok := &auth.Token{UserID: userID, ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now()}
		if err := store.Save(raw, tok); err != nil {
			t.Fatalf("Save() error: %v", err)
		}
	}

	// Delete all tokens for user 1
	if err := store.DeleteByUser(1); err != nil {
		t.Fatalf("DeleteByUser() error: %v", err)
	}

	// User 1 tokens should be gone
	if _, err := store.Get("user1-tok1"); err != fberrors.ErrNotExist {
		t.Errorf("user1-tok1 should be deleted, got err: %v", err)
	}
	if _, err := store.Get("user1-tok2"); err != fberrors.ErrNotExist {
		t.Errorf("user1-tok2 should be deleted, got err: %v", err)
	}

	// User 2 token should still exist
	if _, err := store.Get("user2-tok1"); err != nil {
		t.Errorf("user2-tok1 should still exist, got err: %v", err)
	}
}

func TestTokenStore_DeleteByUserNoTokens(t *testing.T) {
	t.Parallel()
	store := newTestTokenStore(t)

	// Should not error when no tokens exist for user
	if err := store.DeleteByUser(999); err != nil {
		t.Errorf("DeleteByUser() with no tokens error: %v", err)
	}
}

func TestTokenStore_DeleteExpired(t *testing.T) {
	t.Parallel()
	store := newTestTokenStore(t)

	for raw, tok := range map[string]*auth.Token{
		"expired1": {UserID: 1, ExpiresAt: time.Now().Add(-1 * time.Hour), CreatedAt: time.Now()},
		"expired2": {UserID: 2, ExpiresAt: time.Now().Add(-1 * time.Minute), CreatedAt: time.Now()},
		"valid1":   {UserID: 1, ExpiresAt: time.Now().Add(1 * time.Hour), CreatedAt: time.Now()},
		"valid2":   {UserID: 3, ExpiresAt: time.Now().Add(2 * time.Hour), CreatedAt: time.Now()},
	} {
		if err := store.Save(raw, tok); err != nil {
			t.Fatalf("Save() error: %v", err)
		}
	}

	if err := store.DeleteExpired(); err != nil {
		t.Fatalf("DeleteExpired() error: %v", err)
	}

	// Expired tokens should be gone
	if _, err := store.Get("expired1"); err != fberrors.ErrNotExist {
		t.Errorf("expired1 should be deleted, got err: %v", err)
	}
	if _, err := store.Get("expired2"); err != fberrors.ErrNotExist {
		t.Errorf("expired2 should be deleted, got err: %v", err)
	}

	// Valid tokens should remain
	if _, err := store.Get("valid1"); err != nil {
		t.Errorf("valid1 should still exist, got err: %v", err)
	}
	if _, err := store.Get("valid2"); err != nil {
		t.Errorf("valid2 should still exist, got err: %v", err)
	}
}

func TestTokenStore_DeleteExpiredNoExpired(t *testing.T) {
	t.Parallel()
	store := newTestTokenStore(t)

	// Should not error when no expired tokens exist
	if err := store.DeleteExpired(); err != nil {
		t.Errorf("DeleteExpired() with no expired tokens error: %v", err)
	}
}

// The bearer token must not be recoverable from the database. Before hashing at
// rest, read access to the file — a backup, a snapshot, a copy that ended up
// under a served root — handed over every live session verbatim.
func TestTokenStoreDoesNotPersistBearerToken(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	store := tokenBackend{db: db}

	const raw = "b8f1c0de5a2947e6b3d18f70c95a4e21b8f1c0de5a2947e6b3d18f70c95a4e21"
	if err := store.Save(raw, &auth.Token{UserID: 1, ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now()}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("failed to close db: %v", err)
	}

	contents, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("failed to read db: %v", err)
	}

	if bytes.Contains(contents, []byte(raw)) {
		t.Error("the bearer token was written to the database in the clear")
	}
	if !bytes.Contains(contents, []byte(auth.HashToken(raw))) {
		t.Error("the token hash was not written to the database")
	}
}

func TestTokenStore_DeleteByUserKeepsListedTokens(t *testing.T) {
	t.Parallel()
	store := newTestTokenStore(t)

	for raw := range map[string]struct{}{"keep-me": {}, "revoke-me": {}} {
		if err := store.Save(raw, &auth.Token{UserID: 1, ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now()}); err != nil {
			t.Fatalf("Save() error: %v", err)
		}
	}

	if err := store.DeleteByUser(1, "keep-me"); err != nil {
		t.Fatalf("DeleteByUser() error: %v", err)
	}

	if _, err := store.Get("keep-me"); err != nil {
		t.Errorf("kept token was deleted: %v", err)
	}
	if _, err := store.Get("revoke-me"); err != fberrors.ErrNotExist {
		t.Errorf("revoke-me should be deleted, got err: %v", err)
	}
}

type legacyToken struct {
	Token     string    `json:"token"`
	UserID    uint      `json:"userID"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

func TestNewStorageDropsPreHashingTokens(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	legacy := legacyToken{
		Token:     "legacy-plaintext-token",
		UserID:    1,
		ExpiresAt: time.Now().Add(-time.Hour),
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("failed to marshal legacy token: %v", err)
	}
	if err := db.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(tokensBucket))
		if err != nil {
			return err
		}
		return b.Put([]byte(legacy.Token), raw)
	}); err != nil {
		t.Fatalf("failed to save legacy token: %v", err)
	}

	if _, err := NewStorage(db); err != nil {
		t.Fatalf("NewStorage() error: %v", err)
	}

	remaining := 0
	if err := db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(tokensBucket))
		if b == nil {
			return nil
		}
		return b.ForEach(func(_, v []byte) error {
			if v != nil {
				remaining++
			}
			return nil
		})
	}); err != nil {
		t.Fatalf("scan error: %v", err)
	}
	if remaining != 0 {
		t.Errorf("%d pre-hashing token(s) survived the upgrade", remaining)
	}

	// The sweep that could not key those records must now run clean.
	if err := (tokenBackend{db: db}).DeleteExpired(); err != nil {
		t.Errorf("DeleteExpired() error after purge: %v", err)
	}
}
