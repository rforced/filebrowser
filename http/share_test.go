package fbhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
