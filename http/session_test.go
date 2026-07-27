package fbhttp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/rforced/filebrowser/v2/auth"
	"github.com/rforced/filebrowser/v2/users"
)

const testPassword = "S3cur3P@ssw0rd!xyz"

// createAgedTestToken saves a session that began startedAgo in the past, so that
// the absolute-lifetime ceiling can be exercised without waiting for it.
func createAgedTestToken(t *testing.T, env *httpTestEnv, startedAgo, expiry time.Duration) string {
	t.Helper()

	tokenStr, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}
	if err := env.storage.Tokens.Save(tokenStr, &auth.Token{
		UserID:    env.user.ID,
		ExpiresAt: time.Now().Add(expiry),
		CreatedAt: time.Now().Add(-startedAgo),
	}); err != nil {
		t.Fatalf("failed to save token: %v", err)
	}
	return tokenStr
}

func renew(t *testing.T, env *httpTestEnv, policy tokenPolicy, token string) *httptest.ResponseRecorder {
	t.Helper()

	handler := handle(renewHandler(policy), "", env.storage, env.server)
	r, _ := http.NewRequest(http.MethodPost, "/", http.NoBody)
	r.Header.Set("X-Auth", token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, r)
	return recorder
}

// Renewal must not be able to walk a session forward forever: a token stolen
// once would otherwise outlive every password change made in response.
func TestRenewHandlerRefusesPastAbsoluteLifetime(t *testing.T) {
	t.Parallel()
	env := setupTestStorage(t)

	policy := tokenPolicy{expiration: 2 * time.Hour, maxLifetime: 24 * time.Hour}
	old := createAgedTestToken(t, env, 25*time.Hour, time.Hour)

	recorder := renew(t, env, policy, old)
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}

	if _, err := env.storage.Tokens.Get(old); err == nil {
		t.Error("a session refused for age should have been deleted")
	}
}

func TestRenewHandlerCarriesSessionStartAndCapsExpiry(t *testing.T) {
	t.Parallel()
	env := setupTestStorage(t)

	policy := tokenPolicy{expiration: 2 * time.Hour, maxLifetime: 24 * time.Hour}
	startedAgo := 23 * time.Hour
	old := createAgedTestToken(t, env, startedAgo, time.Hour)

	recorder := renew(t, env, policy, old)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	renewed, err := env.storage.Tokens.Get(recorder.Body.String())
	if err != nil {
		t.Fatalf("renewed token not found: %v", err)
	}

	// The renewal inherits the original login time rather than restarting the
	// clock, or the ceiling could be pushed back one renewal at a time.
	if age := time.Since(renewed.CreatedAt); age < startedAgo {
		t.Errorf("session start moved forward: age = %v, want at least %v", age, startedAgo)
	}

	// Only one hour of the session's lifetime is left, so the two-hour token
	// expiry must be trimmed to it.
	if remaining := time.Until(renewed.ExpiresAt); remaining > time.Hour+time.Minute {
		t.Errorf("expiry = %v away, want it capped at the remaining ~1h of session life", remaining)
	}
}

func putUser(t *testing.T, env *httpTestEnv, token string, body string) *httptest.ResponseRecorder {
	t.Helper()

	handler := handle(userPutHandler, "", env.storage, env.server)
	r, _ := http.NewRequest(http.MethodPut, "/", strings.NewReader(body))
	r.Header.Set("X-Auth", token)
	r.Header.Set("Content-Type", "application/json")
	r = mux.SetURLVars(r, map[string]string{"id": fmt.Sprint(env.user.ID)})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, r)
	return recorder
}

func TestPasswordChangeRevokesOtherSessions(t *testing.T) {
	t.Parallel()
	env := setupTestStorage(t)

	acting := createTestToken(t, env, env.user.ID, time.Hour)
	other := createTestToken(t, env, env.user.ID, time.Hour)

	body, err := json.Marshal(map[string]any{
		"what":             "user",
		"which":            []string{"password"},
		"current_password": testPassword,
		"data":             map[string]any{"id": env.user.ID, "password": "an0th3rG00dP@ssw0rd"},
	})
	if err != nil {
		t.Fatalf("failed to build body: %v", err)
	}

	recorder := putUser(t, env, acting, string(body))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	if _, err := env.storage.Tokens.Get(other); err == nil {
		t.Error("a session other than the caller's survived a password change")
	}

	// The caller keeps the session they are acting from, so that changing your
	// own password does not log you out of the request making the change.
	if _, err := env.storage.Tokens.Get(acting); err != nil {
		t.Errorf("the caller's own session was revoked: %v", err)
	}
}

func TestUnrelatedProfileChangeKeepsSessions(t *testing.T) {
	t.Parallel()
	env := setupTestStorage(t)

	other := createTestToken(t, env, env.user.ID, time.Hour)

	body, err := json.Marshal(map[string]any{
		"what":  "user",
		"which": []string{"locale"},
		"data":  map[string]any{"id": env.user.ID, "locale": "pt"},
	})
	if err != nil {
		t.Fatalf("failed to build body: %v", err)
	}

	recorder := putUser(t, env, createTestToken(t, env, env.user.ID, time.Hour), string(body))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	if _, err := env.storage.Tokens.Get(other); err != nil {
		t.Errorf("a change that touches no credential revoked a session: %v", err)
	}
}

func TestUserDeleteRevokesSessions(t *testing.T) {
	t.Parallel()
	env := setupTestStorage(t)

	// Deleting the only admin is refused, so give the fixture user a peer to
	// delete and act as an admin ourselves.
	admin := &users.User{Username: "admin", Perm: users.Permissions{Admin: true}}
	pwd, err := users.ValidateAndHashPwd(testPassword, 0)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	admin.Password = pwd
	if err := env.storage.Users.Save(admin); err != nil {
		t.Fatalf("failed to save admin: %v", err)
	}

	victim := createTestToken(t, env, env.user.ID, time.Hour)
	adminToken := createTestToken(t, env, admin.ID, time.Hour)

	handler := handle(userDeleteHandler, "", env.storage, env.server)
	body := fmt.Sprintf(`{"current_password":%q}`, testPassword)
	r, _ := http.NewRequest(http.MethodDelete, "/", strings.NewReader(body))
	r.Header.Set("X-Auth", adminToken)
	r = mux.SetURLVars(r, map[string]string{"id": fmt.Sprint(env.user.ID)})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, r)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	if _, err := env.storage.Tokens.Get(victim); err == nil {
		t.Error("a deleted user's session is still in the store")
	}
}
