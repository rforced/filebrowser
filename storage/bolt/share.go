package bolt

import (
	"errors"
	"strings"

	bbolt "go.etcd.io/bbolt"

	fberrors "github.com/rforced/filebrowser/v2/errors"
	"github.com/rforced/filebrowser/v2/share"
)

type shareBackend struct {
	db *bbolt.DB
}

func (s shareBackend) find(match func(*share.Link) bool) ([]*share.Link, error) {
	var links []*share.Link
	err := s.db.View(func(tx *bbolt.Tx) error {
		return scan(tx, sharesBucket, func(_ []byte, l *share.Link) error {
			if match(l) {
				links = append(links, l)
			}
			return nil
		})
	})
	return links, err
}

func (s shareBackend) All() ([]*share.Link, error) {
	return s.find(func(*share.Link) bool { return true })
}

func (s shareBackend) FindByUserID(id uint) ([]*share.Link, error) {
	return s.find(func(l *share.Link) bool { return l.UserID == id })
}

func (s shareBackend) GetByHash(hash string) (*share.Link, error) {
	link := &share.Link{}
	err := s.db.View(func(tx *bbolt.Tx) error {
		return getJSON(tx, sharesBucket, []byte(hash), link)
	})
	if err != nil {
		return nil, err
	}
	return link, nil
}

func (s shareBackend) GetPermanent(path string, id uint) (*share.Link, error) {
	links, err := s.find(func(l *share.Link) bool {
		return l.Path == path && l.Expire == 0 && l.UserID == id
	})
	if err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return nil, fberrors.ErrNotExist
	}
	return links[0], nil
}

func (s shareBackend) Gets(path string) ([]*share.Link, error) {
	links, err := s.find(func(l *share.Link) bool {
		return l.Path == path
	})
	if err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return links, fberrors.ErrNotExist
	}
	return links, nil
}

func (s shareBackend) Save(l *share.Link) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return putJSON(tx, sharesBucket, []byte(l.Hash), l)
	})
}

func (s shareBackend) Delete(hash string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(sharesBucket))
		if b == nil {
			return nil
		}
		return b.Delete([]byte(hash))
	})
}

func movedUnder(linkPath, prefix string) bool {
	return linkPath == prefix || strings.HasPrefix(linkPath, prefix+"/")
}

func (s shareBackend) UpdatePathPrefix(oldPath, newPath string, userID uint) error {
	// Share paths are stored without a trailing slash.
	from := strings.TrimRight(oldPath, "/")
	to := strings.TrimRight(newPath, "/")

	if from == "" || from == to {
		return nil
	}

	return s.db.Update(func(tx *bbolt.Tx) error {
		var moved []*share.Link

		err := scan(tx, sharesBucket, func(_ []byte, l *share.Link) error {
			if l.UserID == userID && movedUnder(l.Path, from) {
				l.Path = to + strings.TrimPrefix(l.Path, from)
				moved = append(moved, l)
			}
			return nil
		})
		if err != nil {
			return err
		}

		for _, l := range moved {
			err = errors.Join(err, putJSON(tx, sharesBucket, []byte(l.Hash), l))
		}
		return err
	})
}

func (s shareBackend) DeleteWithPathPrefix(pathPrefix string, userID uint) error {
	prefix := strings.TrimRight(pathPrefix, "/")

	return s.db.Update(func(tx *bbolt.Tx) error {
		var doomed [][]byte

		err := scan(tx, sharesBucket, func(key []byte, l *share.Link) error {
			if l.UserID == userID && movedUnder(l.Path, prefix) {
				doomed = append(doomed, append([]byte(nil), key...))
			}
			return nil
		})
		if err != nil {
			return err
		}

		b := tx.Bucket([]byte(sharesBucket))
		if b == nil {
			return nil
		}
		for _, key := range doomed {
			err = errors.Join(err, b.Delete(key))
		}
		return err
	})
}
