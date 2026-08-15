package fbhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rforced/filebrowser/v2/users"
)

func postUser(t *testing.T, env *httpTestEnv, token, body string) *httptest.ResponseRecorder {
	t.Helper()

	r, _ := http.NewRequest(http.MethodPost, "/api/users", strings.NewReader(body))
	r.Header.Set("X-Auth", token)
	r.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handle(userPostHandler, "", env.storage, env.server).ServeHTTP(recorder, r)
	return recorder
}

func TestUserPostWithoutPasswordStoresAnUnusableOne(t *testing.T) {
	t.Parallel()

	env := setupTestStorage(t)
	env.user.Perm.Admin = true
	if err := env.storage.Users.Update(env.user, "Perm"); err != nil {
		t.Fatalf("failed to grant admin: %v", err)
	}

	body, err := json.Marshal(map[string]any{
		"what":             "user",
		"which":            []string{},
		"current_password": testPassword,
		"data": map[string]any{
			"username": "hookuser",
			"password": "",
			"scope":    "/",
			"perm":     users.Permissions{Download: true},
		},
	})
	if err != nil {
		t.Fatalf("failed to build body: %v", err)
	}

	recorder := postUser(t, env, createTestToken(t, env, env.user.ID, time.Hour), string(body))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	created, err := env.storage.Users.Get(env.server.Root, "hookuser")
	if err != nil {
		t.Fatalf("created user not found: %v", err)
	}

	if created.Password == "" {
		t.Fatal("user was stored with an empty password")
	}
	for _, guess := range []string{"", "hookuser", "password", created.Password} {
		if users.CheckPwd(guess, created.Password) {
			t.Fatalf("stored password accepts %q", guess)
		}
	}
}

func TestUserPostStillEnforcesPasswordPolicy(t *testing.T) {
	t.Parallel()

	env := setupTestStorage(t)
	env.user.Perm.Admin = true
	if err := env.storage.Users.Update(env.user, "Perm"); err != nil {
		t.Fatalf("failed to grant admin: %v", err)
	}

	set, err := env.storage.Settings.Get()
	if err != nil {
		t.Fatalf("failed to read settings: %v", err)
	}
	set.MinimumPasswordLength = 12
	if err := env.storage.Settings.Save(set); err != nil {
		t.Fatalf("failed to save settings: %v", err)
	}

	token := createTestToken(t, env, env.user.ID, time.Hour)

	testCases := map[string]struct {
		password string
		wantCode int
	}{
		"too short is rejected": {password: "short", wantCode: http.StatusBadRequest},
		"strong is accepted":    {password: "n3w-Us3r-P@ssw0rd", wantCode: http.StatusCreated},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				"what":             "user",
				"which":            []string{},
				"current_password": testPassword,
				"data": map[string]any{
					"username": "user-" + name,
					"password": tc.password,
					"scope":    "/",
					"perm":     users.Permissions{Download: true},
				},
			})
			if err != nil {
				t.Fatalf("failed to build body: %v", err)
			}

			recorder := postUser(t, env, token, string(body))
			if recorder.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d, body: %s", recorder.Code, tc.wantCode, recorder.Body.String())
			}
		})
	}
}
