package bolt

import (
	"fmt"
	"reflect"
	"strings"

	bbolt "go.etcd.io/bbolt"

	fberrors "github.com/rforced/filebrowser/v2/errors"
	"github.com/rforced/filebrowser/v2/users"
)

type usersBackend struct {
	db *bbolt.DB
}

func (st usersBackend) GetBy(i interface{}) (*users.User, error) {
	switch v := i.(type) {
	case uint:
		return st.getByID(v)
	case string:
		return st.getByUsername(v)
	default:
		return nil, fberrors.ErrInvalidDataType
	}
}

func (st usersBackend) getByID(id uint) (*users.User, error) {
	user := &users.User{}
	err := st.db.View(func(tx *bbolt.Tx) error {
		return getJSON(tx, usersBucket, itob(uint64(id)), user)
	})
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (st usersBackend) getByUsername(username string) (*users.User, error) {
	var found *users.User
	err := st.db.View(func(tx *bbolt.Tx) error {
		return scan(tx, usersBucket, func(_ []byte, u *users.User) error {
			if u.Username == username {
				found = u
				return errStopScan
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, fberrors.ErrNotExist
	}
	return found, nil
}

func (st usersBackend) GetByScope(scope string) (*users.User, error) {
	var found *users.User
	err := st.db.View(func(tx *bbolt.Tx) error {
		return scan(tx, usersBucket, func(_ []byte, u *users.User) error {
			if strings.EqualFold(u.Scope, scope) {
				found = u
				return errStopScan
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, fberrors.ErrNotExist
	}
	return found, nil
}

func (st usersBackend) Gets() ([]*users.User, error) {
	var allUsers []*users.User
	err := st.db.View(func(tx *bbolt.Tx) error {
		return scan(tx, usersBucket, func(_ []byte, u *users.User) error {
			allUsers = append(allUsers, u)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return allUsers, nil
}

func (st usersBackend) Update(user *users.User, fields ...string) error {
	if len(fields) == 0 {
		return st.Save(user)
	}

	return st.db.Update(func(tx *bbolt.Tx) error {
		stored := &users.User{}
		if err := getJSON(tx, usersBucket, itob(uint64(user.ID)), stored); err != nil {
			return err
		}

		src := reflect.ValueOf(user).Elem()
		dst := reflect.ValueOf(stored).Elem()

		for _, field := range fields {
			srcField := src.FieldByName(field)
			if !srcField.IsValid() {
				return fmt.Errorf("invalid field: %s", field)
			}
			dstField := dst.FieldByName(field)
			if !dstField.CanSet() {
				return fmt.Errorf("cannot set field: %s", field)
			}
			dstField.Set(srcField)
		}

		if err := st.checkUsernameFree(tx, stored); err != nil {
			return err
		}
		return putJSON(tx, usersBucket, itob(uint64(stored.ID)), stored)
	})
}

func (st usersBackend) Save(user *users.User) error {
	return st.db.Update(func(tx *bbolt.Tx) error {
		if user.ID == 0 {
			id, err := nextUserID(tx)
			if err != nil {
				return err
			}
			user.ID = uint(id)
		} else if err := bumpUserIDCounter(tx, uint64(user.ID)); err != nil {
			return err
		}

		if err := st.checkUsernameFree(tx, user); err != nil {
			return err
		}

		return putJSON(tx, usersBucket, itob(uint64(user.ID)), user)
	})
}

func (st usersBackend) checkUsernameFree(tx *bbolt.Tx, user *users.User) error {
	return scan(tx, usersBucket, func(_ []byte, other *users.User) error {
		if other.ID != user.ID && other.Username == user.Username {
			return fberrors.ErrExist
		}
		return nil
	})
}

func (st usersBackend) DeleteByID(id uint) error {
	return st.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(usersBucket))
		if b == nil {
			return nil
		}
		return b.Delete(itob(uint64(id)))
	})
}

func (st usersBackend) DeleteByUsername(username string) error {
	user, err := st.getByUsername(username)
	if err != nil {
		return err
	}
	return st.DeleteByID(user.ID)
}

func (st usersBackend) CountAdmins() (int, error) {
	count := 0
	err := st.db.View(func(tx *bbolt.Tx) error {
		return scan(tx, usersBucket, func(_ []byte, u *users.User) error {
			if u.Perm.Admin {
				count++
			}
			return nil
		})
	})
	return count, err
}
