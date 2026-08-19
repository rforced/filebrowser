package fbhttp

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	fbAuth "github.com/rforced/filebrowser/v2/auth"
	"github.com/rforced/filebrowser/v2/files"
	"github.com/rforced/filebrowser/v2/share"
)

const (
	shareRateLimit     = 10
	shareRateWindow    = time.Minute
	shareCaptchaWindow = 15 * time.Minute
	shareCaptchaAction = "share"
)

type shareAttempts struct {
	mu    sync.Mutex
	times []time.Time
}

var shareRateLimiter sync.Map // map[string]*shareAttempts

func init() {
	go evictStaleShareEntries()
}

// evictStaleShareEntries periodically removes rate limiter entries with no
// recent attempts, preventing unbounded memory growth from many unique IPs.
func evictStaleShareEntries() {
	ticker := time.NewTicker(shareRateWindow * 2)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-shareCaptchaWindow)
		shareRateLimiter.Range(func(key, value any) bool {
			attempts := value.(*shareAttempts)
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
				shareRateLimiter.Delete(key)
			}
			return true
		})
	}
}

func recentShareFailures(key string, window time.Duration) int {
	val, ok := shareRateLimiter.Load(key)
	if !ok {
		return 0
	}
	attempts := val.(*shareAttempts)

	attempts.mu.Lock()
	defer attempts.mu.Unlock()

	now := time.Now()
	kept := attempts.times[:0]
	count := 0
	for _, t := range attempts.times {
		if t.After(now.Add(-shareCaptchaWindow)) {
			kept = append(kept, t)
		}
		if t.After(now.Add(-window)) {
			count++
		}
	}
	attempts.times = kept

	return count
}

func recordShareFailure(key string) {
	val, _ := shareRateLimiter.LoadOrStore(key, &shareAttempts{})
	attempts := val.(*shareAttempts)

	attempts.mu.Lock()
	defer attempts.mu.Unlock()
	attempts.times = append(attempts.times, time.Now())
}

func clearShareFailures(key string) {
	shareRateLimiter.Delete(key)
}

var withHashFile = func(fn handleFunc) handleFunc {
	return func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
		id, ifPath := ifPathWithName(r)
		link, err := d.store.Share.GetByHash(id)
		if err != nil {
			return errToStatus(err), err
		}

		ip := clientIP(r, d.server.TrustedProxyNets)

		status, detail, err := authenticateShareRequest(r, d, link, ip, id)
		if status == http.StatusTooManyRequests {
			log.Printf("share auth rate limit exceeded for IP %s on share %s", ip, id)
			w.Header().Set("Retry-After", "60")
		}
		if detail != nil {
			return renderClientError(w, status, *detail)
		}
		if status != 0 || err != nil {
			return status, err
		}

		user, err := d.store.Users.Get(d.server.Root, link.UserID)
		if err != nil {
			return errToStatus(err), err
		}

		if !user.Perm.Download {
			return http.StatusForbidden, nil
		}

		d.user = user

		file, err := files.NewFileInfo(&files.FileOptions{
			Fs:         d.user.Fs,
			Path:       link.Path,
			Modify:     d.user.Perm.Modify,
			Expand:     false,
			ReadHeader: d.server.TypeDetectionByHeader,
			Checker:    d,
		})
		if err != nil {
			return errToStatus(err), err
		}

		// share base path. Canonicalized because it roots both the rebased
		// filesystem and checkerPrefix below, and a stored path that is not
		// "/"-separated would make the two disagree on Windows.
		basePath := slashClean(link.Path)

		// file relative path
		filePath := ""

		if file.IsDir {
			filePath = ifPath
		}

		// set fs root to the shared file/folder
		d.user.Fs = files.NewScopedFs(d.user.Fs, basePath)

		// the filesystem is now rebased onto basePath, so paths handed to the
		// rule checker are relative to it. Resolve them back to the user's
		// original scope so deny rules below the share root keep applying.
		d.checkerPrefix = basePath

		file, err = files.NewFileInfo(&files.FileOptions{
			Fs:      d.user.Fs,
			Path:    filePath,
			Modify:  d.user.Perm.Modify,
			Expand:  true,
			Checker: d,
		})
		if err != nil {
			return errToStatus(err), err
		}

		if file.IsDir {
			// extract name from the last directory in the path
			name := filepath.Base(strings.TrimRight(link.Path, string(filepath.Separator)))
			file.Name = name
		}

		d.raw = file
		return fn(w, r, d)
	}
}

// ref to https://github.com/filebrowser/filebrowser/pull/727
// `/api/public/dl/MEEuZK-v/file-name.txt` for old browsers to save file with correct name
func ifPathWithName(r *http.Request) (id, filePath string) {
	pathElements := strings.Split(r.URL.Path, "/")
	// prevent maliciously constructed parameters like `/api/public/dl/XZzCDnK2_not_exists_hash_name`
	// len(pathElements) will be 1, and golang will panic `runtime error: index out of range`

	switch len(pathElements) {
	case 1:
		return r.URL.Path, "/"
	default:
		// Public share routes do not pass through withUser, so canonicalize the
		// share-relative path here instead.
		return pathElements[0], slashClean(path.Join(pathElements[1:]...))
	}
}

var publicShareHandler = withHashFile(func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	file := d.raw.(*files.FileInfo)

	if file.IsDir {
		file.Sorting = files.Sorting{By: "name", Asc: false}
		file.ApplySort()
		return renderJSON(w, r, file)
	}

	return renderJSON(w, r, file)
})

var publicDlHandler = withHashFile(func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	file := d.raw.(*files.FileInfo)
	if !file.IsDir {
		return rawFileHandler(w, r, file)
	}

	return rawDirHandler(w, r, d, file)
})

func authenticateShareRequest(r *http.Request, d *data, l *share.Link, ip, id string) (int, *clientError, error) {
	if l.PasswordHash == "" {
		return http.StatusForbidden, nil, fmt.Errorf("share is not password-protected")
	}

	password, err := url.QueryUnescape(r.Header.Get("X-SHARE-PASSWORD"))
	if err != nil {
		return http.StatusBadRequest, nil, err
	}
	if password == "" {
		password = r.URL.Query().Get("password")
	}
	if password == "" {
		return http.StatusUnauthorized, nil, nil
	}

	attemptKey := ip + ":" + id
	if recentShareFailures(attemptKey, shareRateWindow) >= shareRateLimit {
		return http.StatusTooManyRequests, nil, nil
	}

	detail, err := checkShareCaptcha(r, d, attemptKey, id)
	if err != nil {
		return http.StatusInternalServerError, nil, err
	}
	if detail != nil {
		if detail.Code == "captchaFailed" {
			recordShareFailure(attemptKey)
		}

		return http.StatusUnauthorized, detail, nil
	}

	if err := bcrypt.CompareHashAndPassword([]byte(l.PasswordHash), []byte(password)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			recordShareFailure(attemptKey)
			return http.StatusUnauthorized, nil, nil
		}
		return http.StatusInternalServerError, nil, err
	}

	clearShareFailures(attemptKey)
	return 0, nil, nil
}

var assessShareCaptcha = func(recaptcha *fbAuth.ReCaptcha, token string) (bool, error) {
	return recaptcha.OkAction(token, shareCaptchaAction)
}

func checkShareCaptcha(r *http.Request, d *data, attemptKey, id string) (*clientError, error) {
	recaptcha, err := configuredReCaptcha(d)
	if err != nil {
		return nil, err
	}
	if recaptcha == nil {
		return nil, nil
	}

	token := r.Header.Get("X-SHARE-CAPTCHA")
	if token == "" {
		if recentShareFailures(attemptKey, shareCaptchaWindow) == 0 {
			return nil, nil
		}

		return &clientError{
			Code:    "captchaRequired",
			Message: "security verification required",
		}, nil
	}

	ok, err := assessShareCaptcha(recaptcha, token)
	if err != nil {
		log.Printf("share captcha assessment failed on share %s: %v", id, err)
		return nil, nil
	}
	if !ok {
		return &clientError{
			Code:    "captchaFailed",
			Message: "security verification failed",
		}, nil
	}

	return nil, nil
}
