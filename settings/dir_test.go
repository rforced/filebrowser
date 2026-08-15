package settings

import (
	"errors"
	"testing"

	fberrors "github.com/rforced/filebrowser/v2/errors"
	"github.com/rforced/filebrowser/v2/users"
)

// fakeUserBackend is the minimum users.StorageBackend needed to exercise the
// provisioning collision guard: it indexes saved users by scope.
type fakeUserBackend struct {
	byScope map[string]*users.User
	nextID  uint
}

func newFakeUserBackend() *fakeUserBackend {
	return &fakeUserBackend{byScope: map[string]*users.User{}}
}

func (f *fakeUserBackend) GetByScope(scope string) (*users.User, error) {
	if u, ok := f.byScope[scope]; ok {
		return u, nil
	}
	return nil, fberrors.ErrNotExist
}

func (f *fakeUserBackend) Save(u *users.User) error {
	f.nextID++
	u.ID = f.nextID
	f.byScope[u.Scope] = u
	return nil
}

func (f *fakeUserBackend) GetBy(interface{}) (*users.User, error) { return nil, fberrors.ErrNotExist }
func (f *fakeUserBackend) Gets() ([]*users.User, error)           { return nil, nil }
func (f *fakeUserBackend) Update(*users.User, ...string) error    { return nil }
func (f *fakeUserBackend) DeleteByID(uint) error                  { return nil }
func (f *fakeUserBackend) DeleteByUsername(string) error          { return nil }
func (f *fakeUserBackend) CountAdmins() (int, error)              { return 0, nil }

// A user provisioned with CreateUserDir must receive a per-user home directory
// derived from its username, not the default scope which normalizes to the
// server root.
func TestCreateUserHomeDerivesPerUserScope(t *testing.T) {
	s := &Settings{CreateUserDir: true, UserHomeBasePath: "/users"}

	user := &users.User{Username: "alice", Scope: "."}
	derived, err := s.CreateUserHome(user, t.TempDir(), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !derived {
		t.Error("expected the scope to be reported as derived")
	}
	if user.Scope != "/users/alice" {
		t.Errorf("expected derived scope /users/alice, got %q", user.Scope)
	}
}

// A scope explicitly supplied by the caller (e.g. returned by an auth hook) must
// be preserved instead of being replaced by a derived home directory, and must
// not be reported as derived: it is legitimate for several users to share it.
func TestCreateUserHomePreservesExplicitScope(t *testing.T) {
	s := &Settings{CreateUserDir: true, UserHomeBasePath: "/users"}

	user := &users.User{Username: "alice", Scope: "/custom"}
	derived, err := s.CreateUserHome(user, t.TempDir(), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if derived {
		t.Error("an explicit scope must not be reported as derived")
	}
	if user.Scope != "/custom" {
		t.Errorf("explicit scope should be preserved, got %q", user.Scope)
	}
}

// Regression for the username-normalization home-directory collision
// (GHSA-7rc3-g7h6-22m7): two distinct usernames that cleanUsername() normalizes
// to the same directory must not be handed the same home directory. The second
// provisioning attempt is rejected.
//
// This exercises the CreateUserHome + SaveProvisioned pair directly rather than
// through an HTTP handler, because that pair is the guard and every remaining
// provisioning path goes through it. Hook auth (auth/hook.go SaveUser) is the
// live caller; the signup and proxy-auth callers were removed, but deleting
// them did not remove the vector.
func TestProvisioningRejectsCollidingNormalizedScope(t *testing.T) {
	s := &Settings{CreateUserDir: true, UserHomeBasePath: "/users"}
	root := t.TempDir()
	store := users.NewStorage(newFakeUserBackend())

	provision := func(username string) error {
		user := &users.User{Username: username, Password: "x", Scope: "."}
		derived, err := s.CreateUserHome(user, root, false)
		if err != nil {
			return err
		}
		return store.SaveProvisioned(user, derived)
	}

	// Victim registers first and gets /users/team-x.
	if err := provision("team-x"); err != nil {
		t.Fatalf("first provisioning failed: %v", err)
	}

	// Attacker picks a distinct username normalizing to the same scope.
	if err := provision("team--x"); !errors.Is(err, fberrors.ErrExist) {
		t.Fatalf("VULNERABLE: colliding provisioning expected ErrExist, got %v", err)
	}

	// The shared scope must still be owned solely by the first user.
	owner, err := store.GetByScope("/users/team-x")
	if err != nil {
		t.Fatalf("expected first user to own the scope: %v", err)
	}
	if owner.Username != "team-x" {
		t.Fatalf("scope owner = %q, want team-x", owner.Username)
	}
}
