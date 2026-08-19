package fbhttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	bbolt "go.etcd.io/bbolt"
	"golang.org/x/crypto/bcrypt"

	fbAuth "github.com/rforced/filebrowser/v2/auth"
	"github.com/rforced/filebrowser/v2/files"
	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/share"
	"github.com/rforced/filebrowser/v2/storage"
	"github.com/rforced/filebrowser/v2/storage/bolt"
	"github.com/rforced/filebrowser/v2/users"
)

const captchaTestPassword = "sharePassword"

func newCaptchaShareEnv(t *testing.T, recaptcha *fbAuth.ReCaptcha) *storage.Storage {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(captchaTestPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	db, err := bbolt.Open(filepath.Join(t.TempDir(), "db"), 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	st, err := bolt.NewStorage(db)
	if err != nil {
		t.Fatalf("failed to get storage: %v", err)
	}
	if err := st.Share.Save(&share.Link{Hash: "h", UserID: 1, Path: "/", PasswordHash: string(hash)}); err != nil {
		t.Fatalf("failed to save share: %v", err)
	}
	if err := st.Users.Save(&users.User{
		Username: "owner",
		Password: "pw",
		Perm:     users.Permissions{Share: true, Download: true},
	}); err != nil {
		t.Fatalf("failed to save user: %v", err)
	}

	method := settings.AuthMethod("")
	if recaptcha != nil {
		method = fbAuth.MethodJSONAuth
		if err := st.Auth.Save(&fbAuth.JSONAuth{ReCaptcha: recaptcha}); err != nil {
			t.Fatalf("failed to save auther: %v", err)
		}
	}
	if err := st.Settings.Save(&settings.Settings{Key: []byte("key"), AuthMethod: method}); err != nil {
		t.Fatalf("failed to save settings: %v", err)
	}

	st.Users = &customFSUser{Store: st.Users, fs: files.NewScopedFs(&afero.MemMapFs{}, "/")}
	return st
}

func fullReCaptcha() *fbAuth.ReCaptcha {
	return &fbAuth.ReCaptcha{Key: "site-key", Secret: "secret", ProjectID: "project"}
}

// stubShareAssessment answers for Google, and reports the tokens it was asked
// about so a test can tell an assessment that happened from one that did not.
func stubShareAssessment(t *testing.T, ok bool, err error) *[]string {
	t.Helper()

	original := assessShareCaptcha
	t.Cleanup(func() { assessShareCaptcha = original })

	seen := &[]string{}
	assessShareCaptcha = func(_ *fbAuth.ReCaptcha, token string) (bool, error) {
		*seen = append(*seen, token)
		return ok, err
	}

	return seen
}

type shareAttempt struct {
	password string
	captcha  string
}

func requestShare(t *testing.T, st *storage.Storage, ip string, attempt shareAttempt) *httptest.ResponseRecorder {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, "h", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-SHARE-PASSWORD", attempt.password)
	if attempt.captcha != "" {
		req.Header.Set("X-SHARE-CAPTCHA", attempt.captcha)
	}
	req.RemoteAddr = ip + ":12345"

	rec := httptest.NewRecorder()
	handle(publicShareHandler, "", st, &settings.Server{}).ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var detail struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		return ""
	}
	return detail.Code
}

// TestShareCaptchaFollowsFailures pins the policy: a visitor who has not
// guessed wrong is never asked for a token, and one who has cannot try again
// without one.
func TestShareCaptchaFollowsFailures(t *testing.T) {
	shareRateLimiter.Clear()

	st := newCaptchaShareEnv(t, fullReCaptcha())
	seen := stubShareAssessment(t, true, nil)
	const ip = "10.20.0.1"

	rec := requestShare(t, st, ip, shareAttempt{password: captchaTestPassword})
	if rec.Code != http.StatusOK {
		t.Fatalf("first attempt with the right password: got %d, want 200", rec.Code)
	}
	if len(*seen) != 0 {
		t.Errorf("a clean visitor was assessed: %v", *seen)
	}

	rec = requestShare(t, st, ip, shareAttempt{password: "wrong"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: got %d, want 401", rec.Code)
	}

	rec = requestShare(t, st, ip, shareAttempt{password: captchaTestPassword})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("guess after a failure: got %d, want 401", rec.Code)
	}
	if code := errorCode(t, rec); code != "captchaRequired" {
		t.Errorf("error code = %q, want %q", code, "captchaRequired")
	}
	if len(*seen) != 0 {
		t.Errorf("a missing token was assessed: %v", *seen)
	}

	rec = requestShare(t, st, ip, shareAttempt{password: captchaTestPassword, captcha: "token"})
	if rec.Code != http.StatusOK {
		t.Fatalf("attempt with a token: got %d, want 200", rec.Code)
	}
	if len(*seen) != 1 || (*seen)[0] != "token" {
		t.Errorf("assessed tokens = %v, want [token]", *seen)
	}

	// The right password ends the guessing the captcha was there to slow, so
	// the download links and command-line clients that cannot carry a token
	// have to work again afterwards.
	rec = requestShare(t, st, ip, shareAttempt{password: captchaTestPassword})
	if rec.Code != http.StatusOK {
		t.Errorf("after a successful unlock: got %d, want 200", rec.Code)
	}
}

func TestShareCaptchaRejectsBadToken(t *testing.T) {
	shareRateLimiter.Clear()

	st := newCaptchaShareEnv(t, fullReCaptcha())
	seen := stubShareAssessment(t, false, nil)
	const ip = "10.20.0.2"

	rec := requestShare(t, st, ip, shareAttempt{password: captchaTestPassword, captcha: "bad"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token with the right password: got %d, want 401", rec.Code)
	}
	if code := errorCode(t, rec); code != "captchaFailed" {
		t.Errorf("error code = %q, want %q", code, "captchaFailed")
	}

	// A rejected token is an attempt: the assessment API is ours to pay for, so
	// junk tokens have to run into the same limit as junk passwords.
	for i := 1; i < shareRateLimit; i++ {
		requestShare(t, st, ip, shareAttempt{password: captchaTestPassword, captcha: "bad"})
	}

	rec = requestShare(t, st, ip, shareAttempt{password: captchaTestPassword, captcha: "bad"})
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("after %d rejected tokens: got %d, want 429", shareRateLimit, rec.Code)
	}
	if len(*seen) != shareRateLimit {
		t.Errorf("assessments = %d, want %d", len(*seen), shareRateLimit)
	}
}

// TestShareCaptchaSurvivesAssessmentOutage keeps an unreachable Google from
// sealing every share: the request falls back to the password and rate limit.
func TestShareCaptchaSurvivesAssessmentOutage(t *testing.T) {
	shareRateLimiter.Clear()

	st := newCaptchaShareEnv(t, fullReCaptcha())
	stubShareAssessment(t, false, errors.New("connection refused"))

	rec := requestShare(t, st, "10.20.0.3", shareAttempt{password: captchaTestPassword, captcha: "token"})
	if rec.Code != http.StatusOK {
		t.Errorf("assessment outage: got %d, want 200", rec.Code)
	}
}

// TestShareCaptchaIgnoredWhenUnconfigured covers the deployments with no
// reCAPTCHA keys, where a wrong password must not lock the address out.
func TestShareCaptchaIgnoredWhenUnconfigured(t *testing.T) {
	shareRateLimiter.Clear()

	st := newCaptchaShareEnv(t, nil)
	seen := stubShareAssessment(t, true, nil)
	const ip = "10.20.0.4"

	if rec := requestShare(t, st, ip, shareAttempt{password: "wrong"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: got %d, want 401", rec.Code)
	}
	if rec := requestShare(t, st, ip, shareAttempt{password: captchaTestPassword}); rec.Code != http.StatusOK {
		t.Errorf("retry without a captcha: got %d, want 200", rec.Code)
	}
	if len(*seen) != 0 {
		t.Errorf("assessed a token with no reCAPTCHA configured: %v", *seen)
	}
}

// TestShareRateLimitCountsFailures keeps browsing and downloading off the
// limiter: only wrong passwords count towards it.
func TestShareRateLimitCountsFailures(t *testing.T) {
	shareRateLimiter.Clear()

	st := newCaptchaShareEnv(t, nil)
	const ip = "10.20.0.5"

	for i := 0; i < shareRateLimit*2; i++ {
		if rec := requestShare(t, st, ip, shareAttempt{password: captchaTestPassword}); rec.Code != http.StatusOK {
			t.Fatalf("request %d with the right password: got %d, want 200", i, rec.Code)
		}
	}

	for i := 0; i < shareRateLimit; i++ {
		if rec := requestShare(t, st, ip, shareAttempt{password: "wrong"}); rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d: got %d, want 401", i, rec.Code)
		}
	}

	rec := requestShare(t, st, ip, shareAttempt{password: captchaTestPassword})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("after %d failures: got %d, want 429", shareRateLimit, rec.Code)
	}
	if retry := rec.Header().Get("Retry-After"); retry != "60" {
		t.Errorf("Retry-After = %q, want %q", retry, "60")
	}
}

// TestShareCaptchaIsolatedByAddressAndShare makes sure one visitor's failure
// does not put a captcha in front of everybody else.
func TestShareCaptchaIsolatedByAddressAndShare(t *testing.T) {
	shareRateLimiter.Clear()

	st := newCaptchaShareEnv(t, fullReCaptcha())
	stubShareAssessment(t, true, nil)

	if rec := requestShare(t, st, "10.20.0.6", shareAttempt{password: "wrong"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: got %d, want 401", rec.Code)
	}

	rec := requestShare(t, st, "10.20.0.7", shareAttempt{password: captchaTestPassword})
	if rec.Code != http.StatusOK {
		t.Errorf("another address: got %d (%s), want 200", rec.Code, errorCode(t, rec))
	}
}
