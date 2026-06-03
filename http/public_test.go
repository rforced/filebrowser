package fbhttp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/asdine/storm/v3"
	"github.com/spf13/afero"

	"github.com/rforced/filebrowser/v2/rules"
	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/share"
	"github.com/rforced/filebrowser/v2/storage/bolt"
	"github.com/rforced/filebrowser/v2/users"
)

var testIPCounter atomic.Uint64

func TestPublicShareHandlerAuthentication(t *testing.T) {
	t.Parallel()

	// Reset the share rate limiter so parallel test runs don't interfere.
	shareRateLimiter.Clear()

	const passwordBcrypt = "$2y$10$TFAmdCbyd/mEZDe5fUeZJu.MaJQXRTwdqb/IQV.eTn6dWrF58gCSe"
	testCases := map[string]struct {
		share              *share.Link
		req                *http.Request
		expectedStatusCode int
	}{
		"Share without password is forbidden": {
			share:              &share.Link{Hash: "h", UserID: 1},
			req:                newHTTPRequest(t),
			expectedStatusCode: 403,
		},
		"Private share, no auth provided, 401": {
			share:              &share.Link{Hash: "h", UserID: 1, PasswordHash: passwordBcrypt},
			req:                newHTTPRequest(t),
			expectedStatusCode: 401,
		},
		"Private share, token in URL is ignored, 401": {
			share:              &share.Link{Hash: "h", UserID: 1, PasswordHash: passwordBcrypt},
			req:                newHTTPRequest(t, func(r *http.Request) { r.URL.RawQuery = "token=123" }),
			expectedStatusCode: 401,
		},
		"Private share, authentication via password": {
			share:              &share.Link{Hash: "h", UserID: 1, PasswordHash: passwordBcrypt},
			req:                newHTTPRequest(t, func(r *http.Request) { r.Header.Set("X-SHARE-PASSWORD", "password") }),
			expectedStatusCode: 200,
		},
		"Private share, authentication via invalid password, 401": {
			share:              &share.Link{Hash: "h", UserID: 1, PasswordHash: passwordBcrypt},
			req:                newHTTPRequest(t, func(r *http.Request) { r.Header.Set("X-SHARE-PASSWORD", "wrong-password") }),
			expectedStatusCode: 401,
		},
		"Private share, authentication via query parameter": {
			share:              &share.Link{Hash: "h", UserID: 1, PasswordHash: passwordBcrypt},
			req:                newHTTPRequest(t, func(r *http.Request) { r.URL.RawQuery = "password=password" }),
			expectedStatusCode: 200,
		},
		"Private share, authentication via invalid query parameter, 401": {
			share:              &share.Link{Hash: "h", UserID: 1, PasswordHash: passwordBcrypt},
			req:                newHTTPRequest(t, func(r *http.Request) { r.URL.RawQuery = "password=wrong-password" }),
			expectedStatusCode: 401,
		},
	}

	for name, tc := range testCases {
		for handlerName, handler := range map[string]handleFunc{"public share handler": publicShareHandler, "public dl handler": publicDlHandler} {
			name, tc, handlerName, handler := name, tc, handlerName, handler
			t.Run(fmt.Sprintf("%s: %s", handlerName, name), func(t *testing.T) {
				t.Parallel()

				dbPath := filepath.Join(t.TempDir(), "db")
				db, err := storm.Open(dbPath)
				if err != nil {
					t.Fatalf("failed to open db: %v", err)
				}

				t.Cleanup(func() {
					if err := db.Close(); err != nil {
						t.Errorf("failed to close db: %v", err)
					}
				})

				storage, err := bolt.NewStorage(db)
				if err != nil {
					t.Fatalf("failed to get storage: %v", err)
				}
				if err := storage.Share.Save(tc.share); err != nil {
					t.Fatalf("failed to save share: %v", err)
				}
				if err := storage.Users.Save(&users.User{Username: "username", Password: "pw"}); err != nil {
					t.Fatalf("failed to save user: %v", err)
				}
				if err := storage.Settings.Save(&settings.Settings{Key: []byte("key")}); err != nil {
					t.Fatalf("failed to save settings: %v", err)
				}

				storage.Users = &customFSUser{
					Store: storage.Users,
					fs:    &afero.MemMapFs{},
				}

				// Assign a unique remote address to each subtest to avoid
				// hitting the share rate limiter across parallel tests.
				c := testIPCounter.Add(1)
				req := tc.req.Clone(t.Context())
				req.RemoteAddr = fmt.Sprintf("10.0.0.%d:12345", c)

				recorder := httptest.NewRecorder()
				handler := handle(handler, "", storage, &settings.Server{})

				handler.ServeHTTP(recorder, req)
				result := recorder.Result()
				defer result.Body.Close()
				if result.StatusCode != tc.expectedStatusCode {
					t.Errorf("expected status code %d, got status code %d", tc.expectedStatusCode, result.StatusCode)
				}
			})
		}
	}
}

// TestPublicShareHandlerRules ensures that owner rules keep applying to paths
// below a shared directory, even though the share rebases the filesystem onto
// that directory. A deny rule relative to the owner's scope must not be
// bypassable by requesting the blocked path through the public share.
func TestPublicShareHandlerRules(t *testing.T) {
	t.Parallel()

	// Reset the share rate limiter so parallel test runs don't interfere.
	shareRateLimiter.Clear()

	// bcrypt hash of "password".
	const passwordBcrypt = "$2y$10$TFAmdCbyd/mEZDe5fUeZJu.MaJQXRTwdqb/IQV.eTn6dWrF58gCSe"

	testCases := map[string]struct {
		handler            handleFunc
		path               string
		expectedStatusCode int
	}{
		"blocked file via dl handler, 403": {
			handler:            publicDlHandler,
			path:               "h/private/secret.txt",
			expectedStatusCode: 403,
		},
		"blocked dir listing via share handler, 403": {
			handler:            publicShareHandler,
			path:               "h/private/",
			expectedStatusCode: 403,
		},
		"blocked dir download via dl handler, 403": {
			handler:            publicDlHandler,
			path:               "h/private/",
			expectedStatusCode: 403,
		},
		"allowed file via dl handler, 200": {
			handler:            publicDlHandler,
			path:               "h/public/readme.txt",
			expectedStatusCode: 200,
		},
		"allowed dir listing via share handler, 200": {
			handler:            publicShareHandler,
			path:               "h/public/",
			expectedStatusCode: 200,
		},
	}

	for name, tc := range testCases {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dbPath := filepath.Join(t.TempDir(), "db")
			db, err := storm.Open(dbPath)
			if err != nil {
				t.Fatalf("failed to open db: %v", err)
			}
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Errorf("failed to close db: %v", err)
				}
			})

			storage, err := bolt.NewStorage(db)
			if err != nil {
				t.Fatalf("failed to get storage: %v", err)
			}
			if err := storage.Share.Save(&share.Link{Hash: "h", UserID: 1, Path: "/projects", PasswordHash: passwordBcrypt}); err != nil {
				t.Fatalf("failed to save share: %v", err)
			}
			if err := storage.Users.Save(&users.User{
				Username: "username",
				Password: "pw",
				Perm:     users.Permissions{Share: true, Download: true},
				Rules: []rules.Rule{
					{Allow: false, Path: "/projects/private"},
				},
			}); err != nil {
				t.Fatalf("failed to save user: %v", err)
			}
			if err := storage.Settings.Save(&settings.Settings{Key: []byte("key")}); err != nil {
				t.Fatalf("failed to save settings: %v", err)
			}

			fs := afero.NewMemMapFs()
			if err := afero.WriteFile(fs, "/projects/private/secret.txt", []byte("top secret"), 0o600); err != nil {
				t.Fatalf("failed to write secret file: %v", err)
			}
			if err := afero.WriteFile(fs, "/projects/public/readme.txt", []byte("hello"), 0o600); err != nil {
				t.Fatalf("failed to write public file: %v", err)
			}

			storage.Users = &customFSUser{
				Store: storage.Users,
				fs:    fs,
			}

			req := newHTTPRequest(t, func(r *http.Request) {
				r.URL.Path = tc.path
				r.Header.Set("X-SHARE-PASSWORD", "password")
			})

			// Assign a unique remote address to each subtest to avoid
			// hitting the share rate limiter across parallel tests.
			c := testIPCounter.Add(1)
			req.RemoteAddr = fmt.Sprintf("10.1.0.%d:12345", c)

			recorder := httptest.NewRecorder()
			handler := handle(tc.handler, "", storage, &settings.Server{})

			handler.ServeHTTP(recorder, req)
			result := recorder.Result()
			defer result.Body.Close()
			if result.StatusCode != tc.expectedStatusCode {
				t.Errorf("expected status code %d, got status code %d", tc.expectedStatusCode, result.StatusCode)
			}
		})
	}
}

func newHTTPRequest(t *testing.T, requestModifiers ...func(*http.Request)) *http.Request {
	t.Helper()
	r, err := http.NewRequest(http.MethodGet, "h", http.NoBody)
	if err != nil {
		t.Fatalf("failed to construct request: %v", err)
	}
	for _, modify := range requestModifiers {
		modify(r)
	}
	return r
}

type customFSUser struct {
	users.Store
	fs afero.Fs
}

func (cu *customFSUser) Get(baseScope string, id interface{}) (*users.User, error) {
	user, err := cu.Store.Get(baseScope, id)
	if err != nil {
		return nil, err
	}
	user.Fs = cu.fs

	return user, nil
}
