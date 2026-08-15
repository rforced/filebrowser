package bolt

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	bbolt "go.etcd.io/bbolt"

	"github.com/rforced/filebrowser/v2/auth"
	fberrors "github.com/rforced/filebrowser/v2/errors"
	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/share"
	"github.com/rforced/filebrowser/v2/users"
)

func openTestDB(t *testing.T) *bbolt.DB {
	t.Helper()
	db, err := bbolt.Open(filepath.Join(t.TempDir(), "db"), 0o600, nil)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func writeStormRecord(t *testing.T, db *bbolt.DB, bucket string, key, value []byte) {
	t.Helper()
	if err := db.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucket))
		if err != nil {
			return err
		}
		return b.Put(key, value)
	}); err != nil {
		t.Fatalf("write %s/%q: %v", bucket, key, err)
	}
}

func writeStormIndex(t *testing.T, db *bbolt.DB, bucket, index string, key, value []byte) {
	t.Helper()
	if err := db.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucket))
		if err != nil {
			return err
		}
		idx, err := b.CreateBucketIfNotExists([]byte(index))
		if err != nil {
			return err
		}
		return idx.Put(key, value)
	}); err != nil {
		t.Fatalf("write index %s/%s: %v", bucket, index, err)
	}
}

func TestOpensStormWrittenDatabase(t *testing.T) {
	db := openTestDB(t)

	writeStormRecord(t, db, usersBucket, itob(1),
		[]byte(`{"id":1,"username":"alice","password":"hash1","scope":"/alice","perm":{"admin":true}}`))
	writeStormRecord(t, db, usersBucket, itob(2),
		[]byte(`{"id":2,"username":"bob","password":"hash2","scope":"/bob","perm":{"download":true}}`))
	writeStormRecord(t, db, usersBucket, itob(7),
		[]byte(`{"id":7,"username":"carol","password":"hash3","scope":"/carol"}`))

	if err := db.Update(func(tx *bbolt.Tx) error {
		b, _ := tx.CreateBucketIfNotExists([]byte(usersBucket))
		meta, err := b.CreateBucketIfNotExists([]byte(metadataBucket))
		if err != nil {
			return err
		}
		return meta.Put([]byte(idCounterKey), itob(7))
	}); err != nil {
		t.Fatal(err)
	}

	writeStormRecord(t, db, tokensBucket, []byte("abc123"),
		[]byte(`{"hash":"abc123","userID":2,"expiresAt":"2099-01-01T00:00:00Z","createdAt":"2020-01-01T00:00:00Z"}`))
	writeStormRecord(t, db, sharesBucket, []byte("sh1"),
		[]byte(`{"hash":"sh1","path":"/docs","userID":2,"expire":0,"password_hash":"ph"}`))
	writeStormRecord(t, db, configBucket, []byte("settings"),
		[]byte(`{"key":"aw==","minimumPasswordLength":12,"branding":{"name":"Legacy"}}`))
	writeStormRecord(t, db, configBucket, []byte("server"),
		[]byte(`{"root":"/srv","port":"8080"}`))

	writeStormIndex(t, db, usersBucket, "__storm_index_Username", []byte("alice"), itob(1))
	writeStormIndex(t, db, usersBucket, "__storm_index_ID", itob(1), itob(1))
	writeStormIndex(t, db, sharesBucket, "__storm_index_Path", []byte("/docs__sh1"), []byte("sh1"))
	writeStormIndex(t, db, tokensBucket, "__storm_index_UserID", []byte("x"), []byte("abc123"))

	st, err := NewStorage(db)
	if err != nil {
		t.Fatalf("NewStorage on a storm database: %v", err)
	}

	t.Run("users by id", func(t *testing.T) {
		u, err := st.Users.Get("", uint(1))
		if err != nil {
			t.Fatalf("get user 1: %v", err)
		}
		if u.Username != "alice" || !u.Perm.Admin {
			t.Errorf("user 1 = %+v", u)
		}
	})

	t.Run("users by username", func(t *testing.T) {
		u, err := st.Users.Get("", "bob")
		if err != nil {
			t.Fatalf("get bob: %v", err)
		}
		if u.ID != 2 || !u.Perm.Download {
			t.Errorf("bob = %+v", u)
		}
	})

	t.Run("all users, index buckets skipped", func(t *testing.T) {
		all, err := st.Users.Gets("")
		if err != nil {
			t.Fatalf("gets: %v", err)
		}
		if len(all) != 3 {
			t.Fatalf("expected 3 users, got %d — an index sub-bucket may have been read as a record", len(all))
		}
	})

	t.Run("token", func(t *testing.T) {
		raw := "abc123"
		if err := st.Tokens.Save(raw, &auth.Token{UserID: 2, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatalf("save token: %v", err)
		}
		got, err := st.Tokens.Get(raw)
		if err != nil {
			t.Fatalf("get token: %v", err)
		}
		if got.UserID != 2 {
			t.Errorf("token userID = %d, want 2", got.UserID)
		}
	})

	t.Run("share", func(t *testing.T) {
		l, err := st.Share.GetByHash("sh1")
		if err != nil {
			t.Fatalf("get share: %v", err)
		}
		if l.Path != "/docs" || l.UserID != 2 {
			t.Errorf("share = %+v", l)
		}
	})

	t.Run("settings", func(t *testing.T) {
		set, err := st.Settings.Get()
		if err != nil {
			t.Fatalf("get settings: %v", err)
		}
		if set.Branding.Name != "Legacy" {
			t.Errorf("branding name = %q, want Legacy", set.Branding.Name)
		}
		srv, err := st.Settings.GetServer()
		if err != nil {
			t.Fatalf("get server: %v", err)
		}
		if srv.Root != "/srv" {
			t.Errorf("root = %q, want /srv", srv.Root)
		}
	})

	t.Run("new user continues the id sequence", func(t *testing.T) {
		u := &users.User{Username: "dave", Password: "pw", Scope: "/dave"}
		if err := st.Users.Save(u); err != nil {
			t.Fatalf("save dave: %v", err)
		}
		if u.ID != 8 {
			t.Errorf("dave got id %d, want 8 (the counter was at 7)", u.ID)
		}
	})
}

func TestUserIDsAreNeverReused(t *testing.T) {
	db := openTestDB(t)
	st, err := NewStorage(db)
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}

	first := &users.User{Username: "first", Password: "pw", Scope: "/first"}
	if err := st.Users.Save(first); err != nil {
		t.Fatal(err)
	}
	second := &users.User{Username: "second", Password: "pw", Scope: "/second"}
	if err := st.Users.Save(second); err != nil {
		t.Fatal(err)
	}
	if second.ID != 2 {
		t.Fatalf("second user id = %d, want 2", second.ID)
	}

	if err := st.Share.Save(&share.Link{Hash: "orphan", Path: "/p", UserID: second.ID}); err != nil {
		t.Fatal(err)
	}

	if err := st.Users.Delete(second.ID); err != nil {
		t.Fatalf("delete second: %v", err)
	}

	third := &users.User{Username: "third", Password: "pw", Scope: "/third"}
	if err := st.Users.Save(third); err != nil {
		t.Fatal(err)
	}

	if third.ID == second.ID {
		t.Fatalf("VULNERABLE: id %d was reused, inheriting the deleted user's share links", third.ID)
	}
	if third.ID != 3 {
		t.Errorf("third user id = %d, want 3", third.ID)
	}
}

func TestUsernamesAreUnique(t *testing.T) {
	db := openTestDB(t)
	st, err := NewStorage(db)
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}

	if err := st.Users.Save(&users.User{Username: "dup", Password: "pw", Scope: "/a"}); err != nil {
		t.Fatal(err)
	}
	err = st.Users.Save(&users.User{Username: "dup", Password: "pw", Scope: "/b"})
	if !errors.Is(err, fberrors.ErrExist) {
		t.Fatalf("expected ErrExist for a duplicate username, got %v", err)
	}

	existing, err := st.Users.Get("", "dup")
	if err != nil {
		t.Fatal(err)
	}
	existing.Locale = "de"
	if err := st.Users.Save(existing); err != nil {
		t.Errorf("re-saving an existing user should not conflict with itself: %v", err)
	}
}

func TestUpdateOnlyTouchesNamedFields(t *testing.T) {
	db := openTestDB(t)
	st, err := NewStorage(db)
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}

	original := &users.User{
		Username: "u", Password: "original-hash", Scope: "/scope",
		Locale: "en", Perm: users.Permissions{Admin: true, Download: true},
	}
	if err := st.Users.Save(original); err != nil {
		t.Fatal(err)
	}

	partial := &users.User{ID: original.ID, Username: "u", Locale: "de"}
	if err := st.Users.Update(partial, "Locale"); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := st.Users.Get("", original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Locale != "de" {
		t.Errorf("locale = %q, want de", got.Locale)
	}
	if got.Password != "original-hash" {
		t.Errorf("password was clobbered: %q", got.Password)
	}
	if got.Scope != "/scope" {
		t.Errorf("scope was clobbered: %q", got.Scope)
	}
	if !got.Perm.Admin || !got.Perm.Download {
		t.Errorf("permissions were clobbered: %+v", got.Perm)
	}
}

func TestRecordsRemainPlainJSON(t *testing.T) {
	db := openTestDB(t)
	st, err := NewStorage(db)
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}

	if err := st.Users.Save(&users.User{Username: "alice", Password: "pw", Scope: "/alice"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Settings.Save(&settings.Settings{Key: []byte("k"), Branding: settings.Branding{Name: "N"}}); err != nil {
		t.Fatal(err)
	}

	if err := db.View(func(tx *bbolt.Tx) error {
		raw := tx.Bucket([]byte(usersBucket)).Get(itob(1))
		if raw == nil {
			return errors.New("user 1 is not stored under its 8-byte big-endian id")
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return err
		}
		if decoded["username"] != "alice" {
			return errors.New("user record is not JSON keyed by the struct's json tags")
		}

		raw = tx.Bucket([]byte(configBucket)).Get([]byte("settings"))
		if raw == nil {
			return errors.New(`settings are not stored at config/"settings"`)
		}
		return json.Unmarshal(raw, &decoded)
	}); err != nil {
		t.Errorf("on-disk format changed: %v", err)
	}
}
