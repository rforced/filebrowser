package fbhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
