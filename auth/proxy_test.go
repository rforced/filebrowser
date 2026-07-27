package auth

import (
	"net/http"
	"strings"
	"testing"

	fberrors "github.com/rforced/filebrowser/v2/errors"
	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/users"
)

// provisionStore is a users.Store that actually records saved users, so the
// auto-provisioning paths (proxy and hook) can be exercised end to end. It is
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
func (m *provisionStore) LastUpdate(_ uint) int64    { return 0 }

// With CreateUserDir enabled, two distinct proxy-authenticated users must each
// receive their own home directory instead of both inheriting the server root.
func TestProxyAuthCreateUserDirIsolatesScope(t *testing.T) {
	t.Parallel()

	store := newProvisionStore()
	srv := &settings.Server{Root: t.TempDir()}
	s := &settings.Settings{
		Key:              []byte("key"),
		AuthMethod:       MethodProxyAuth,
		CreateUserDir:    true,
		UserHomeBasePath: "/users",
		Defaults: settings.UserDefaults{
			Scope: ".",
			Perm:  users.Permissions{Create: true},
		},
	}

	auth := ProxyAuth{Header: "X-Remote-User"}
	provision := func(name string) *users.User {
		req, _ := http.NewRequest(http.MethodGet, "/", http.NoBody)
		req.Header.Set("X-Remote-User", name)
		u, err := auth.Auth(req, store, s, srv)
		if err != nil {
			t.Fatalf("Auth(%q) error: %v", name, err)
		}
		return u
	}

	alice := provision("alice")
	bob := provision("bob")

	if alice.Scope == "/" || bob.Scope == "/" {
		t.Fatalf("provisioned users inherited the server root: alice=%q bob=%q", alice.Scope, bob.Scope)
	}
	if alice.Scope == bob.Scope {
		t.Fatalf("distinct users must get distinct scopes, both got %q", alice.Scope)
	}
	if alice.Scope != "/users/alice" {
		t.Errorf("expected /users/alice, got %q", alice.Scope)
	}
}
