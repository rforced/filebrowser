package bolt

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	bbolt "go.etcd.io/bbolt"

	"github.com/rforced/filebrowser/v2/auth"
	fberrors "github.com/rforced/filebrowser/v2/errors"
	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/share"
	"github.com/rforced/filebrowser/v2/storage"
	"github.com/rforced/filebrowser/v2/users"
)

const (
	usersBucket  = "User"
	tokensBucket = "Token"
	sharesBucket = "Link"
	configBucket = "config"

	metadataBucket = "__storm_metadata"
	idCounterKey   = "IDcounter"
)

func NewStorage(db *bbolt.DB) (*storage.Storage, error) {
	if err := db.Update(func(tx *bbolt.Tx) error {
		for _, name := range []string{usersBucket, tokensBucket, sharesBucket, configBucket} {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	userStore := users.NewStorage(usersBackend{db: db})
	shareStore := share.NewStorage(shareBackend{db: db})
	settingsStore := settings.NewStorage(settingsBackend{db: db})
	authStore := auth.NewStorage(authBackend{db: db}, userStore)

	if err := saveConfig(db, "version", 2); err != nil {
		return nil, err
	}

	tokenStore := tokenBackend{db: db}
	if err := tokenStore.purgeLegacy(); err != nil {
		return nil, err
	}

	return &storage.Storage{
		Auth:     authStore,
		Users:    userStore,
		Share:    shareStore,
		Settings: settingsStore,
		Tokens:   tokenStore,
	}, nil
}

func itob(id uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, id)
	return b
}

func btoi(b []byte) uint64 {
	if len(b) != 8 {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

func getJSON(tx *bbolt.Tx, bucket string, key []byte, v any) error {
	b := tx.Bucket([]byte(bucket))
	if b == nil {
		return fberrors.ErrNotExist
	}
	raw := b.Get(key)
	if raw == nil {
		return fberrors.ErrNotExist
	}
	return json.Unmarshal(raw, v)
}

func putJSON(tx *bbolt.Tx, bucket string, key []byte, v any) error {
	b, err := tx.CreateBucketIfNotExists([]byte(bucket))
	if err != nil {
		return err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return b.Put(key, raw)
}

func scan[T any](tx *bbolt.Tx, bucket string, fn func(key []byte, v *T) error) error {
	b := tx.Bucket([]byte(bucket))
	if b == nil {
		return nil
	}

	err := b.ForEach(func(k, raw []byte) error {
		if raw == nil {
			return nil
		}
		var v T
		if err := json.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("corrupt record %q in bucket %q: %w", k, bucket, err)
		}
		return fn(k, &v)
	})
	if errors.Is(err, errStopScan) {
		return nil
	}
	return err
}

var errStopScan = errors.New("stop scan")

func saveConfig(db *bbolt.DB, name string, v any) error {
	return db.Update(func(tx *bbolt.Tx) error {
		return putJSON(tx, configBucket, []byte(name), v)
	})
}

func getConfig(db *bbolt.DB, name string, v any) error {
	return db.View(func(tx *bbolt.Tx) error {
		return getJSON(tx, configBucket, []byte(name), v)
	})
}
