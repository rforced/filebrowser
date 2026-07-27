package fbhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rforced/filebrowser/v2/share"
)

func TestShareListHandlerPermissions(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sharePerm    bool
		downloadPerm bool
		expectedCode int
	}{
		{
			name:         "both share and download permissions allows listing",
			sharePerm:    true,
			downloadPerm: true,
			expectedCode: http.StatusOK,
		},
		{
			name:         "missing download permission is forbidden",
			sharePerm:    true,
			downloadPerm: false,
			expectedCode: http.StatusForbidden,
		},
		{
			name:         "missing share permission is forbidden",
			sharePerm:    false,
			downloadPerm: true,
			expectedCode: http.StatusForbidden,
		},
		{
			name:         "missing both permissions is forbidden",
			sharePerm:    false,
			downloadPerm: false,
			expectedCode: http.StatusForbidden,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := setupTestStorage(t)
			env.user.Perm.Share = tc.sharePerm
			env.user.Perm.Download = tc.downloadPerm
			if err := env.storage.Users.Update(env.user, "Perm"); err != nil {
				t.Fatalf("failed to update user permissions: %v", err)
			}

			if err := env.storage.Share.Save(&share.Link{
				Hash:   "share-1",
				Path:   "/docs/file.txt",
				UserID: env.user.ID,
				Expire: time.Now().Add(time.Hour).Unix(),
			}); err != nil {
				t.Fatalf("failed to save share: %v", err)
			}

			tokenStr := createTestToken(t, env, env.user.ID, time.Hour)

			handler := handle(shareListHandler, "", env.storage, env.server)
			req, err := http.NewRequest(http.MethodGet, "/api/shares", http.NoBody)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}
			req.Header.Set("X-Auth", tokenStr)

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != tc.expectedCode {
				t.Fatalf("status = %d, want %d, body: %s", recorder.Code, tc.expectedCode, recorder.Body.String())
			}

			if tc.expectedCode != http.StatusOK {
				return
			}

			var links []share.Link
			if err := json.NewDecoder(recorder.Body).Decode(&links); err != nil {
				t.Fatalf("failed to decode response body: %v", err)
			}

			if len(links) != 1 {
				t.Fatalf("links length = %d, want 1", len(links))
			}
			if links[0].Hash != "share-1" {
				t.Fatalf("share hash = %q, want %q", links[0].Hash, "share-1")
			}
		})
	}
}

// Regression for the share secret exposure (GHSA-833g-cqhp-h72j): the share API
// must not serialize the bcrypt password hash, while still persisting it
// server-side so password-protected shares keep working. Our fork has no bypass
// token field, so password_hash is the only secret to guard.
func TestShareListHandlerDoesNotLeakPasswordHash(t *testing.T) {
	t.Parallel()

	env := setupTestStorage(t)
	env.user.Perm.Share = true
	env.user.Perm.Download = true
	if err := env.storage.Users.Update(env.user, "Perm"); err != nil {
		t.Fatalf("failed to update user permissions: %v", err)
	}

	if err := env.storage.Share.Save(&share.Link{
		Hash:         "share-secret",
		Path:         "/docs/file.txt",
		UserID:       env.user.ID,
		Expire:       time.Now().Add(time.Hour).Unix(),
		PasswordHash: "$2a$10$abcdefghijklmnopqrstuv",
	}); err != nil {
		t.Fatalf("failed to save share: %v", err)
	}

	tokenStr := createTestToken(t, env, env.user.ID, time.Hour)

	handler := handle(shareListHandler, "", env.storage, env.server)
	req, err := http.NewRequest(http.MethodGet, "/api/shares", http.NoBody)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("X-Auth", tokenStr)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", recorder.Code, recorder.Body.String())
	}

	// The raw body must not carry the secret under any key.
	if strings.Contains(recorder.Body.String(), "password_hash") ||
		strings.Contains(recorder.Body.String(), "$2a$10$") {
		t.Fatalf("VULNERABLE: response leaks password hash: %s", recorder.Body.String())
	}

	var resp []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("response length = %d, want 1", len(resp))
	}
	if _, ok := resp[0]["password_hash"]; ok {
		t.Fatalf("VULNERABLE: response includes password_hash key: %s", recorder.Body.String())
	}
	if resp[0]["hasPassword"] != true {
		t.Fatalf("hasPassword = %v, want true", resp[0]["hasPassword"])
	}

	// The secret must still be persisted server-side.
	stored, err := env.storage.Share.GetByHash("share-secret")
	if err != nil {
		t.Fatalf("share not stored: %v", err)
	}
	if stored.PasswordHash == "" {
		t.Fatal("server-side password hash was not persisted")
	}
}

// A share password is the only thing between the link and the file, so it has to
// clear the same bar as an account password rather than merely being non-empty.
func TestSharePostEnforcesPasswordPolicy(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		password     string
		expectedCode int
		// wantCode is the machine-readable reason the response must carry, so
		// that the UI can tell the sharer which rule they broke.
		wantCode   string
		wantParams map[string]string
	}{
		"single character is rejected": {
			password:     "x",
			expectedCode: http.StatusBadRequest,
			wantCode:     "passwordTooShort",
			wantParams:   map[string]string{"min": "12"},
		},
		"just under the minimum": {
			password:     "elevenchars",
			expectedCode: http.StatusBadRequest,
			wantCode:     "passwordTooShort",
			wantParams:   map[string]string{"min": "12"},
		},
		"common password is rejected": {
			password:     "123456789012",
			expectedCode: http.StatusBadRequest,
			wantCode:     "passwordTooCommon",
		},
		"long enough and not common": {
			password:     "sh4re-P@ssw0rd-x",
			expectedCode: http.StatusOK,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			env := setupTestStorage(t)
			env.user.Perm.Share = true
			env.user.Perm.Download = true
			if err := env.storage.Users.Update(env.user, "Perm"); err != nil {
				t.Fatalf("failed to update user permissions: %v", err)
			}

			set, err := env.storage.Settings.Get()
			if err != nil {
				t.Fatalf("failed to read settings: %v", err)
			}
			set.MinimumPasswordLength = 12
			if err := env.storage.Settings.Save(set); err != nil {
				t.Fatalf("failed to save settings: %v", err)
			}

			if err := os.WriteFile(filepath.Join(env.server.Root, "shared.txt"), []byte("data"), 0o600); err != nil {
				t.Fatalf("failed to create file: %v", err)
			}

			body, err := json.Marshal(share.CreateBody{Password: tc.password, Expires: "1", Unit: "hours"})
			if err != nil {
				t.Fatalf("failed to build body: %v", err)
			}

			handler := handle(sharePostHandler, "", env.storage, env.server)
			req, _ := http.NewRequest(http.MethodPost, "/shared.txt", strings.NewReader(string(body)))
			req.Header.Set("X-Auth", createTestToken(t, env, env.user.ID, time.Hour))
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != tc.expectedCode {
				t.Fatalf("status = %d, want %d, body: %s", recorder.Code, tc.expectedCode, recorder.Body.String())
			}

			if tc.wantCode == "" {
				return
			}

			var detail struct {
				Code    string            `json:"code"`
				Message string            `json:"message"`
				Params  map[string]string `json:"params"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
				t.Fatalf("rejection body is not a structured error: %v (body: %s)", err, recorder.Body.String())
			}

			if detail.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", detail.Code, tc.wantCode)
			}
			// The English rendering is what a client with no translations shows.
			if detail.Message == "" {
				t.Error("rejection carries no message")
			}
			for k, want := range tc.wantParams {
				if detail.Params[k] != want {
					t.Errorf("params[%q] = %q, want %q", k, detail.Params[k], want)
				}
			}
		})
	}
}
