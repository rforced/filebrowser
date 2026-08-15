package auth

import (
	"strings"

	fberrors "github.com/rforced/filebrowser/v2/errors"
	"github.com/rforced/filebrowser/v2/users"
)

// provisionStore is a users.Store that actually records saved users, so the
// auto-provisioning path (hook auth) can be exercised end to end. It is
// separate from json_test.go's single-user mockUserStore, which reports
// os.ErrNotExist and discards saves.
type provisionStore struct {
	users map[string]*users.User
}

func newProvisionStore() *provisionStore {
	return &provisionStore{users: make(map[string]*users.User)}
}

func (m *provisionStore) Get(_ string, id interface{}) (*users.User, error) {
	if v, ok := id.(string); ok {
		if u, ok := m.users[v]; ok {
			return u, nil
		}
	}
	return nil, fberrors.ErrNotExist
}

// GetByScope reflects the users that have actually been saved, so a scope
// collision between two provisioned users is detected rather than assumed away.
// The match is case-insensitive, like the bolt backend's.
func (m *provisionStore) GetByScope(scope string) (*users.User, error) {
	for _, u := range m.users {
		if strings.EqualFold(u.Scope, scope) {
			return u, nil
		}
	}
	return nil, fberrors.ErrNotExist
}

func (m *provisionStore) Gets(_ string) ([]*users.User, error)    { return nil, nil }
func (m *provisionStore) Update(_ *users.User, _ ...string) error { return nil }
func (m *provisionStore) Save(user *users.User) error {
	m.users[user.Username] = user
	return nil
}

func (m *provisionStore) SaveProvisioned(user *users.User, derivedScope bool) error {
	if derivedScope {
		if _, err := m.GetByScope(user.Scope); err == nil {
			return fberrors.ErrExist
		}
	}
	return m.Save(user)
}

func (m *provisionStore) Delete(_ interface{}) error { return nil }

func (m *provisionStore) LastUpdate(_ uint) int64 { return 0 }
