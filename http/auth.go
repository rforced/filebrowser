package fbhttp

import (
	"errors"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	fbAuth "github.com/rforced/filebrowser/v2/auth"
	fberrors "github.com/rforced/filebrowser/v2/errors"
	"github.com/rforced/filebrowser/v2/users"
)

const (
	DefaultTokenExpirationTime = time.Hour * 2
	loginRateLimit             = 10
	loginRateWindow            = time.Minute
	maxAuthBodySize            = 1 << 20 // 1 MiB
)

type tokenPolicy struct {
	expiration  time.Duration
	maxLifetime time.Duration
}

type loginAttempts struct {
	mu    sync.Mutex
	times []time.Time
}

var loginRateLimiter sync.Map // map[string]*loginAttempts

func init() {
	go evictStaleLoginEntries()
}

// evictStaleLoginEntries periodically removes rate limiter entries with no
// recent attempts, preventing unbounded memory growth from many unique IPs.
func evictStaleLoginEntries() {
	ticker := time.NewTicker(loginRateWindow * 2)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-loginRateWindow)
		loginRateLimiter.Range(func(key, value any) bool {
			attempts := value.(*loginAttempts)
			attempts.mu.Lock()
			hasRecent := false
			for _, t := range attempts.times {
				if t.After(cutoff) {
					hasRecent = true
					break
				}
			}
			attempts.mu.Unlock()
			if !hasRecent {
				loginRateLimiter.Delete(key)
			}
			return true
		})
	}
}

// checkLoginRateLimit returns true if the IP has exceeded the login rate limit.
func checkLoginRateLimit(ip string) bool {
	now := time.Now()
	val, _ := loginRateLimiter.LoadOrStore(ip, &loginAttempts{})
	attempts := val.(*loginAttempts)

	attempts.mu.Lock()
	defer attempts.mu.Unlock()

	// Remove attempts outside the rate window
	cutoff := now.Add(-loginRateWindow)
	valid := attempts.times[:0]
	for _, t := range attempts.times {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	attempts.times = valid

	if len(attempts.times) >= loginRateLimit {
		return true
	}

	attempts.times = append(attempts.times, now)
	return false
}

type userInfo struct {
	ID                    uint              `json:"id"`
	Locale                string            `json:"locale"`
	ViewMode              users.ViewMode    `json:"viewMode"`
	SingleClick           bool              `json:"singleClick"`
	RedirectAfterCopyMove bool              `json:"redirectAfterCopyMove"`
	Perm                  users.Permissions `json:"perm"`
	LockPassword          bool              `json:"lockPassword"`
	HideDotfiles          bool              `json:"hideDotfiles"`
	DateFormat            bool              `json:"dateFormat"`
	Username              string            `json:"username"`
}

func userInfoFrom(user *users.User) userInfo {
	return userInfo{
		ID:                    user.ID,
		Locale:                user.Locale,
		ViewMode:              user.ViewMode,
		SingleClick:           user.SingleClick,
		RedirectAfterCopyMove: user.RedirectAfterCopyMove,
		Perm:                  user.Perm,
		LockPassword:          user.LockPassword,
		HideDotfiles:          user.HideDotfiles,
		DateFormat:            user.DateFormat,
		Username:              user.Username,
	}
}

func extractToken(r *http.Request, allowQuery bool) string {
	if token := r.Header.Get("X-Auth"); token != "" {
		return token
	}
	if allowQuery {
		return r.URL.Query().Get("auth")
	}
	return ""
}

func withUser(fn handleFunc) handleFunc {
	return authenticated(fn, false)
}

func withMediaUser(fn handleFunc) handleFunc {
	return authenticated(fn, true)
}

func authenticated(fn handleFunc, allowQueryToken bool) handleFunc {
	return func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
		tokenStr := extractToken(r, allowQueryToken)
		if tokenStr == "" {
			return http.StatusUnauthorized, nil
		}

		token, err := d.store.Tokens.Get(tokenStr)
		if err != nil {
			if errors.Is(err, fberrors.ErrNotExist) {
				return http.StatusUnauthorized, nil
			}
			return http.StatusInternalServerError, err
		}

		if token.IsExpired() {
			_ = d.store.Tokens.Delete(tokenStr)
			return http.StatusUnauthorized, nil
		}

		d.user, err = d.store.Users.Get(d.server.Root, token.UserID)
		if err != nil {
			if errors.Is(err, fberrors.ErrNotExist) {
				_ = d.store.Tokens.Delete(tokenStr)
				return http.StatusUnauthorized, nil
			}
			return http.StatusInternalServerError, err
		}

		d.token = token
		d.tokenStr = tokenStr

		expiration := d.server.GetTokenExpirationTime(DefaultTokenExpirationTime)
		if time.Until(token.ExpiresAt) < expiration/2 {
			w.Header().Set("X-Renew-Token", "true")
		}

		canonicalizeRequestPath(r)
		return fn(w, r, d)
	}
}

func withAdmin(fn handleFunc) handleFunc {
	return withUser(func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
		if !d.user.Perm.Admin {
			return http.StatusForbidden, nil
		}

		return fn(w, r, d)
	})
}

func loginHandler(policy tokenPolicy) handleFunc {
	return func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodySize)
		}

		// Check rate limit before any auth or recaptcha logic
		ip := clientIP(r, d.server.TrustedProxyNets)
		if checkLoginRateLimit(ip) {
			log.Printf("login rate limit exceeded for IP %s", ip)
			w.Header().Set("Retry-After", "60")
			return http.StatusTooManyRequests, nil
		}

		auther, err := d.store.Auth.Get(d.settings.AuthMethod)
		if err != nil {
			return http.StatusInternalServerError, err
		}

		user, err := auther.Auth(r, d.store.Users, d.settings, d.server)
		switch {
		case errors.Is(err, fbAuth.ErrCaptchaFailed):
			return http.StatusTooManyRequests, nil
		case errors.Is(err, os.ErrPermission):
			return http.StatusForbidden, nil
		case err != nil:
			return http.StatusInternalServerError, err
		}

		return createAndReturnToken(w, d, user, policy, time.Now())
	}
}

func renewHandler(policy tokenPolicy) handleFunc {
	return withUser(func(w http.ResponseWriter, _ *http.Request, d *data) (int, error) {
		// Renewal slides the expiry forward, but only within the session's
		// absolute lifetime. Without that ceiling a stolen token could be walked
		// forward indefinitely and would outlive any password change.
		startedAt := d.token.CreatedAt
		_ = d.store.Tokens.Delete(d.tokenStr)

		if time.Since(startedAt) >= policy.maxLifetime {
			return http.StatusUnauthorized, nil
		}

		return createAndReturnToken(w, d, d.user, policy, startedAt)
	})
}

var logoutHandler = withUser(func(_ http.ResponseWriter, _ *http.Request, d *data) (int, error) {
	_ = d.store.Tokens.Delete(d.tokenStr)
	return http.StatusOK, nil
})

var meHandler = withUser(func(w http.ResponseWriter, _ *http.Request, d *data) (int, error) {
	info := userInfoFrom(d.user)
	return renderJSON(w, nil, info)
})

// createAndReturnToken issues a session token for user and writes the bearer
// string to the response. startedAt is when the session began: time.Now() for a
// fresh login, and the original login time for a renewal, so that the absolute
// lifetime is measured from the login rather than from the last renewal.
func createAndReturnToken(w http.ResponseWriter, d *data, user *users.User, policy tokenPolicy, startedAt time.Time) (int, error) {
	tokenStr, err := fbAuth.GenerateToken()
	if err != nil {
		return http.StatusInternalServerError, err
	}

	expiresAt := time.Now().Add(policy.expiration)
	if limit := startedAt.Add(policy.maxLifetime); expiresAt.After(limit) {
		expiresAt = limit
	}

	token := &fbAuth.Token{
		UserID:    user.ID,
		ExpiresAt: expiresAt,
		CreatedAt: startedAt,
	}

	if err := d.store.Tokens.Save(tokenStr, token); err != nil {
		return http.StatusInternalServerError, err
	}

	w.Header().Set("Content-Type", "text/plain")
	if _, err := w.Write([]byte(tokenStr)); err != nil {
		return http.StatusInternalServerError, err
	}
	return 0, nil
}
