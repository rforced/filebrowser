package bolt

import (
	"errors"
	"time"

	bbolt "go.etcd.io/bbolt"

	"github.com/rforced/filebrowser/v2/auth"
	fberrors "github.com/rforced/filebrowser/v2/errors"
)

type tokenBackend struct {
	db *bbolt.DB
}

func (s tokenBackend) Save(token string, t *auth.Token) error {
	t.Hash = auth.HashToken(token)
	return s.db.Update(func(tx *bbolt.Tx) error {
		return putJSON(tx, tokensBucket, []byte(t.Hash), t)
	})
}

func (s tokenBackend) Get(token string) (*auth.Token, error) {
	t := &auth.Token{}
	err := s.db.View(func(tx *bbolt.Tx) error {
		return getJSON(tx, tokensBucket, []byte(auth.HashToken(token)), t)
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s tokenBackend) Delete(token string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(tokensBucket))
		if b == nil {
			return nil
		}
		return b.Delete([]byte(auth.HashToken(token)))
	})
}

func (s tokenBackend) DeleteByUser(userID uint, keep ...string) error {
	kept := make(map[string]struct{}, len(keep))
	for _, token := range keep {
		if token != "" {
			kept[auth.HashToken(token)] = struct{}{}
		}
	}

	return s.deleteMatching(func(key []byte, t *auth.Token) bool {
		if t.UserID != userID {
			return false
		}
		_, spared := kept[string(key)]
		return !spared
	})
}

func (s tokenBackend) DeleteExpired() error {
	now := time.Now()
	return s.deleteMatching(func(_ []byte, t *auth.Token) bool {
		return t.ExpiresAt.Before(now)
	})
}

func (s tokenBackend) purgeLegacy() error {
	return s.deleteMatching(func(_ []byte, t *auth.Token) bool {
		return t.Hash == ""
	})
}

func (s tokenBackend) deleteMatching(match func(key []byte, t *auth.Token) bool) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		var doomed [][]byte

		err := scan(tx, tokensBucket, func(key []byte, t *auth.Token) error {
			if match(key, t) {
				doomed = append(doomed, append([]byte(nil), key...))
			}
			return nil
		})
		if err != nil {
			return err
		}

		b := tx.Bucket([]byte(tokensBucket))
		if b == nil {
			return nil
		}
		for _, key := range doomed {
			err = errors.Join(err, b.Delete(key))
		}
		return err
	})
}

var _ auth.TokenStore = tokenBackend{}

var _ = fberrors.ErrNotExist
