package bolt

import (
	"errors"
	"time"

	"github.com/asdine/storm/v3"
	"github.com/asdine/storm/v3/q"

	"github.com/rforced/filebrowser/v2/auth"
	fberrors "github.com/rforced/filebrowser/v2/errors"
)

type tokenBackend struct {
	db *storm.DB
}

func (s tokenBackend) Save(token string, t *auth.Token) error {
	t.Hash = auth.HashToken(token)
	return s.db.Save(t)
}

func (s tokenBackend) Get(token string) (*auth.Token, error) {
	var t auth.Token
	err := s.db.One("Hash", auth.HashToken(token), &t)
	if errors.Is(err, storm.ErrNotFound) {
		return nil, fberrors.ErrNotExist
	}
	return &t, err
}

func (s tokenBackend) Delete(token string) error {
	err := s.db.DeleteStruct(&auth.Token{Hash: auth.HashToken(token)})
	if errors.Is(err, storm.ErrNotFound) {
		return nil
	}
	return err
}

func (s tokenBackend) DeleteByUser(userID uint, keep ...string) error {
	kept := make(map[string]struct{}, len(keep))
	for _, token := range keep {
		if token != "" {
			kept[auth.HashToken(token)] = struct{}{}
		}
	}

	var tokens []auth.Token
	err := s.db.Select(q.Eq("UserID", userID)).Find(&tokens)
	if errors.Is(err, storm.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, t := range tokens {
		if _, ok := kept[t.Hash]; ok {
			continue
		}
		err = errors.Join(err, s.db.DeleteStruct(&t))
	}
	return err
}

func (s tokenBackend) DeleteExpired() error {
	var tokens []auth.Token
	err := s.db.Select(q.Lt("ExpiresAt", time.Now())).Find(&tokens)
	if errors.Is(err, storm.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	// Collect failures rather than returning on the first one: this runs as a
	// periodic sweep, and one unremovable record must not leave every expired
	// session behind it in the database.
	for _, t := range tokens {
		err = errors.Join(err, s.db.DeleteStruct(&t))
	}
	return err
}

// purgeLegacy removes sessions written before tokens were stored hashed. Their
// records are keyed by the bearer token itself, so they no longer authenticate
// anything; and because that key does not decode into the current struct, the
// expiry sweep cannot delete them by identity either.
func (s tokenBackend) purgeLegacy() error {
	err := s.db.Select(q.Eq("Hash", "")).Delete(new(auth.Token))
	if errors.Is(err, storm.ErrNotFound) {
		return nil
	}
	return err
}
