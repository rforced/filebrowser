package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	fberrors "github.com/rforced/filebrowser/v2/errors"
	"github.com/rforced/filebrowser/v2/files"
	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/users"
)

const MethodHookAuth settings.AuthMethod = "hook"

const hookTimeout = 30 * time.Second

const hookWaitDelay = 2 * time.Second

type hookCred struct {
	Password  string `json:"password"`
	Username  string `json:"username"`
	ReCaptcha string `json:"recaptcha"`
}

// HookAuth is a hook implementation of an Auther.
type HookAuth struct {
	Users     users.Store        `json:"-"`
	Settings  *settings.Settings `json:"-"`
	Server    *settings.Server   `json:"-"`
	Cred      hookCred           `json:"-"`
	Fields    hookFields         `json:"-"`
	Command   string             `json:"command"`
	ReCaptcha *ReCaptcha         `json:"recaptcha" yaml:"recaptcha"`
}

// Auth authenticates the user via a json in content body.
func (a *HookAuth) Auth(r *http.Request, usr users.Store, stg *settings.Settings, srv *settings.Server) (*users.User, error) {
	var cred hookCred

	if r.Body == nil {
		return nil, os.ErrPermission
	}

	err := json.NewDecoder(r.Body).Decode(&cred)
	if err != nil {
		return nil, os.ErrPermission
	}

	if a.ReCaptcha != nil && a.ReCaptcha.Secret != "" {
		ok, err := a.ReCaptcha.Ok(cred.ReCaptcha)
		if err != nil {
			return nil, err
		}

		if !ok {
			return nil, ErrCaptchaFailed
		}
	}

	a.Users = usr
	a.Settings = stg
	a.Server = srv
	a.Cred = cred

	action, err := a.RunCommand(r.Context())
	if err != nil {
		return nil, err
	}

	switch action {
	case "auth":
		u, err := a.SaveUser()
		if err != nil {
			return nil, err
		}
		return u, nil
	case "block":
		return nil, os.ErrPermission
	case "pass":
		u, err := a.Users.Get(a.Server.Root, a.Cred.Username)
		if err != nil || !users.CheckPwd(a.Cred.Password, u.Password) {
			return nil, os.ErrPermission
		}
		return u, nil
	default:
		return nil, fmt.Errorf("invalid hook action: %s", action)
	}
}

func (a *HookAuth) RunCommand(ctx context.Context) (string, error) {
	name, args, err := splitCommand(a.Command)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = hookWaitDelay
	cmd.Env = append(os.Environ(), fmt.Sprintf("USERNAME=%s", a.Cred.Username))
	cmd.Stdin = strings.NewReader(a.Cred.Password)

	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("auth hook timed out after %s", hookTimeout)
		}
		return "", err
	}

	a.GetValues(string(out))

	return a.Fields.Values["hook.action"], nil
}

func splitCommand(command string) (name string, args []string, err error) {
	var parts []string
	var current strings.Builder
	var quote rune
	inToken := false

	for _, r := range command {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inToken = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if inToken {
				parts = append(parts, current.String())
				current.Reset()
				inToken = false
			}
		default:
			current.WriteRune(r)
			inToken = true
		}
	}

	if quote != 0 {
		return "", nil, errors.New("auth hook command has an unterminated quote")
	}
	if inToken {
		parts = append(parts, current.String())
	}
	if len(parts) == 0 {
		return "", nil, errors.New("auth hook command is empty")
	}

	return parts[0], parts[1:], nil
}

func (a *HookAuth) GetValues(s string) {
	m := map[string]string{}

	// make line breaks consistent on Windows platform
	s = strings.ReplaceAll(s, "\r\n", "\n")

	// iterate input lines
	for val := range strings.Lines(s) {
		v := strings.SplitN(val, "=", 2)

		// skips non key and value format
		if len(v) != 2 {
			continue
		}

		fieldKey := strings.TrimSpace(v[0])
		fieldValue := strings.TrimSpace(v[1])

		if a.Fields.IsValid(fieldKey) {
			m[fieldKey] = fieldValue
		}
	}

	a.Fields.Values = m
}

// SaveUser updates the existing user or creates a new one when not found
func (a *HookAuth) SaveUser() (*users.User, error) {
	u, err := a.Users.Get(a.Server.Root, a.Cred.Username)
	if err != nil && !errors.Is(err, fberrors.ErrNotExist) {
		return nil, err
	}

	if u == nil {
		// When the hook auth is enabled, the external command is the source of
		// truth for credentials, so the local minimum-length policy does not
		// apply.
		pass, err := users.HashPwd(a.Cred.Password)
		if err != nil {
			return nil, err
		}

		// create user with the provided credentials
		d := &users.User{
			Username:              a.Cred.Username,
			Password:              pass,
			Scope:                 a.Settings.Defaults.Scope,
			Locale:                a.Settings.Defaults.Locale,
			ViewMode:              a.Settings.Defaults.ViewMode,
			SingleClick:           a.Settings.Defaults.SingleClick,
			RedirectAfterCopyMove: a.Settings.Defaults.RedirectAfterCopyMove,
			Sorting:               a.Settings.Defaults.Sorting,
			Perm:                  a.Settings.Defaults.Perm,
			DateFormat:            a.Settings.Defaults.DateFormat,
			HideDotfiles:          a.Settings.Defaults.HideDotfiles,
		}
		u = a.GetUser(d)

		// A scope explicitly returned by the hook takes precedence over the
		// automatic per-user home directory derivation.
		_, explicitScope := a.Fields.Values["user.scope"]
		derivedScope, err := a.Settings.CreateUserHome(u, a.Server.Root, explicitScope)
		if err != nil {
			return nil, err
		}
		log.Printf("user: %s, home dir: [%s].", u.Username, u.Scope)

		if err := a.Users.SaveProvisioned(u, derivedScope); err != nil {
			return nil, err
		}
	} else if p := !users.CheckPwd(a.Cred.Password, u.Password); len(a.Fields.Values) > 1 || p {
		u = a.GetUser(u)

		// update the password when it doesn't match the current
		if p {
			// Hook auth bypasses the local minimum-length policy.
			pass, err := users.HashPwd(a.Cred.Password)
			if err != nil {
				return nil, err
			}
			u.Password = pass
		}

		// update user with provided fields
		err := a.Users.Update(u)
		if err != nil {
			return nil, err
		}
	}

	return u, nil
}

func (a *HookAuth) GetUser(d *users.User) *users.User {
	isAdmin := d.Perm.Admin
	perms := users.Permissions{
		Admin:    isAdmin,
		Create:   isAdmin || a.Fields.GetBoolean("user.perm.create", d.Perm.Create),
		Rename:   isAdmin || a.Fields.GetBoolean("user.perm.rename", d.Perm.Rename),
		Modify:   isAdmin || a.Fields.GetBoolean("user.perm.modify", d.Perm.Modify),
		Delete:   isAdmin || a.Fields.GetBoolean("user.perm.delete", d.Perm.Delete),
		Share:    isAdmin || a.Fields.GetBoolean("user.perm.share", d.Perm.Share),
		Download: isAdmin || a.Fields.GetBoolean("user.perm.download", d.Perm.Download),
	}
	user := users.User{
		ID:                    d.ID,
		Username:              d.Username,
		Password:              d.Password,
		Scope:                 a.Fields.GetString("user.scope", d.Scope),
		Locale:                a.Fields.GetString("user.locale", d.Locale),
		ViewMode:              users.ViewMode(a.Fields.GetString("user.viewMode", string(d.ViewMode))),
		SingleClick:           a.Fields.GetBoolean("user.singleClick", d.SingleClick),
		RedirectAfterCopyMove: a.Fields.GetBoolean("user.redirectAfterCopyMove", d.RedirectAfterCopyMove),
		Sorting: files.Sorting{
			Asc: a.Fields.GetBoolean("user.sorting.asc", d.Sorting.Asc),
			By:  a.Fields.GetString("user.sorting.by", d.Sorting.By),
		},
		DateFormat:   a.Fields.GetBoolean("user.dateFormat", d.DateFormat),
		HideDotfiles: a.Fields.GetBoolean("user.hideDotfiles", d.HideDotfiles),
		Perm:         perms,
		LockPassword: true,
	}

	return &user
}

// hookFields is used to access fields from the hook
type hookFields struct {
	Values map[string]string
}

// validHookFields contains names of the fields that can be used
var validHookFields = []string{
	"hook.action",
	"user.scope",
	"user.locale",
	"user.viewMode",
	"user.singleClick",
	"user.redirectAfterCopyMove",
	"user.sorting.by",
	"user.sorting.asc",
	"user.hideDotfiles",
	"user.perm.create",
	"user.perm.rename",
	"user.perm.modify",
	"user.perm.delete",
	"user.perm.share",
	"user.perm.download",
}

// IsValid checks if the provided field is on the valid fields list
func (hf *hookFields) IsValid(field string) bool {
	return slices.Contains(validHookFields, field)
}

// GetString returns the string value or provided default
func (hf *hookFields) GetString(k, dv string) string {
	val, ok := hf.Values[k]
	if ok {
		return val
	}
	return dv
}

// GetBoolean returns the bool value or provided default
func (hf *hookFields) GetBoolean(k string, dv bool) bool {
	val, ok := hf.Values[k]
	if ok {
		return val == "true"
	}
	return dv
}

// GetArray returns the array value or provided default
func (hf *hookFields) GetArray(k string, dv []string) []string {
	val, ok := hf.Values[k]
	if ok && strings.TrimSpace(val) != "" {
		return strings.Split(val, " ")
	}
	return dv
}
