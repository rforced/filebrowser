package fbhttp

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asdine/storm/v3"
	"github.com/spf13/afero"

	"github.com/rforced/filebrowser/v2/auth"
	"github.com/rforced/filebrowser/v2/files"
	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/storage/bolt"
	"github.com/rforced/filebrowser/v2/users"
)

// Regression for the archive backslash-to-slash zip-slip (GHSA-83xp-526h-j3ww):
// a single in-scope file whose name contains backslashes is a legal POSIX
// filename, not a traversal. The archive builder must never rewrite "\" into the
// path separator "/", which would manufacture an entry like "../../evil.sh" that
// escapes the extraction directory on the downloader's machine.
//
// Uses our fork's authenticated-handler pattern (an opaque token plus a
// ScopedFs-backed customFSUser), not upstream's JWT signToken/scopedUserStorage.
func TestRawArchiveDoesNotManufactureTraversal(t *testing.T) {
	root := t.TempDir()
	scope := filepath.Join(root, "user")
	if err := os.MkdirAll(filepath.Join(scope, "ziptest"), 0o755); err != nil {
		t.Fatal(err)
	}

	// One legal Linux/macOS filename whose bytes include backslashes. It does not
	// traverse on the server; it only becomes "../../evil.sh" if the builder
	// turns "\" into "/".
	planted := filepath.Join(scope, "ziptest", "..\\..\\evil.sh")
	if err := os.WriteFile(planted, []byte("#!/bin/sh\necho PWNED"), 0o644); err != nil {
		t.Skipf("cannot create backslash-named file: %v", err)
	}

	db, err := storm.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	st, err := bolt.NewStorage(db)
	if err != nil {
		t.Fatalf("failed to get storage: %v", err)
	}
	user := &users.User{Username: "u", Password: "pw", Perm: users.Permissions{Download: true}}
	if err := st.Users.Save(user); err != nil {
		t.Fatalf("failed to save user: %v", err)
	}
	if err := st.Settings.Save(&settings.Settings{Key: []byte("test-signing-key")}); err != nil {
		t.Fatalf("failed to save settings: %v", err)
	}
	// customFSUser.Get wraps the scope in a ScopedFs, mirroring production. The
	// backslash file is a legal in-scope file, so the scope guard allows it; only
	// the archive entry name must be neutralized.
	st.Users = &customFSUser{
		Store: st.Users,
		fs:    files.NewScopedFs(afero.NewOsFs(), scope),
	}

	tokenStr, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}
	if err := st.Tokens.Save(&auth.Token{
		Token:     tokenStr,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("failed to save token: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, "/ziptest", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.URL.RawQuery = "algo=zip"
	req.Header.Set("X-Auth", tokenStr)

	rec := httptest.NewRecorder()
	handle(rawHandler, "", st, &settings.Server{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", rec.Code, rec.Body.String())
	}

	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("failed to read zip: %v", err)
	}
	if len(zr.File) == 0 {
		t.Fatal("archive has no entries")
	}

	for _, f := range zr.File {
		// The entry must be a normalized, root-relative path: no ".." segments
		// and no leading "/". A name may legitimately contain ".." as part of a
		// single filename (e.g. ".._.._evil.sh"), which Clean leaves untouched —
		// so compare against the normalized form rather than searching for "..".
		if strings.HasPrefix(f.Name, "/") || path.Clean("/"+f.Name) != "/"+f.Name {
			t.Errorf("VULNERABLE: archive entry escapes root: %q", f.Name)
		}
	}
}

func TestSetContentDisposition(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		filename string
		inline   bool
		expected string
	}{
		"inline simple filename": {
			filename: "document.pdf",
			inline:   true,
			expected: "inline; filename*=utf-8''" + url.PathEscape("document.pdf"),
		},
		"attachment simple filename": {
			filename: "document.pdf",
			inline:   false,
			expected: "attachment; filename*=utf-8''" + url.PathEscape("document.pdf"),
		},
		"inline non-ASCII filename": {
			filename: "日本語.txt",
			inline:   true,
			expected: "inline; filename*=utf-8''" + url.PathEscape("日本語.txt"),
		},
		"attachment non-ASCII filename": {
			filename: "日本語.txt",
			inline:   false,
			expected: "attachment; filename*=utf-8''" + url.PathEscape("日本語.txt"),
		},
		"inline filename with spaces": {
			filename: "my file.txt",
			inline:   true,
			expected: "inline; filename*=utf-8''" + url.PathEscape("my file.txt"),
		},
		"attachment filename with spaces": {
			filename: "my file.txt",
			inline:   false,
			expected: "attachment; filename*=utf-8''" + url.PathEscape("my file.txt"),
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			recorder := httptest.NewRecorder()
			req, err := http.NewRequest(http.MethodGet, "/test", http.NoBody)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}
			if tc.inline {
				req.URL.RawQuery = "inline=true"
			}

			file := &files.FileInfo{Name: tc.filename}

			setContentDisposition(recorder, req, file)

			got := recorder.Header().Get("Content-Disposition")
			if got != tc.expected {
				t.Errorf("Content-Disposition = %q, want %q", got, tc.expected)
			}

			contentType := recorder.Header().Get("Content-Type")
			if tc.inline && contentType != "" {
				t.Errorf("Content-Type = %q, want empty", contentType)
			}
			if !tc.inline && contentType != "application/octet-stream" {
				t.Errorf("Content-Type = %q, want application/octet-stream", contentType)
			}
		})
	}
}
