package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/users"
)

func writeHookScript(t *testing.T, body string) string {
	t.Helper()
	return writeShellScript(t, "#!/bin/sh\n", body)
}

func writeBashHookScript(t *testing.T, body string) string {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not available")
	}
	return writeShellScript(t, "#!/usr/bin/env bash\n", body)
}

func writeShellScript(t *testing.T, shebang, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hook.sh")
	if err := os.WriteFile(path, []byte(shebang+body), 0o700); err != nil {
		t.Fatalf("failed to write hook script: %v", err)
	}
	return path
}

func TestRunCommandNoCredentialInjection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell")
	}

	marker := filepath.Join(t.TempDir(), "pwned")

	script := writeHookScript(t, "echo hook.action=block\n")

	a := &HookAuth{
		Command: script,
		Cred: hookCred{
			Username: `"; touch ` + marker + `; #`,
			Password: `$(touch ` + marker + `)`,
		},
	}

	action, err := a.RunCommand(context.Background())
	if err != nil {
		t.Fatalf("RunCommand returned error: %v", err)
	}
	if action != "block" {
		t.Fatalf("expected action %q, got %q", "block", action)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("credential injection executed: marker file %q was created", marker)
	}
}

func TestRunCommandReceivesCredentials(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell")
	}

	script := writeBashHookScript(t, `IFS= read -r -d '' PASSWORD
IFS= read -r -d '' MFA_CODE
if [ "$USERNAME" = alice ] && [ "$PASSWORD" = secret ] && [ "$MFA_CODE" = 123456 ]; then
  echo hook.action=auth
else
  echo hook.action=block
fi
`)

	a := &HookAuth{
		Command: script,
		Cred: hookCred{
			Username: "alice",
			Password: "secret",
			MFACode:  "123456",
		},
	}

	action, err := a.RunCommand(context.Background())
	if err != nil {
		t.Fatalf("RunCommand returned error: %v", err)
	}
	if action != "auth" {
		t.Fatalf("expected action %q, got %q", "auth", action)
	}
}

// A password containing the delimiters a naive framing would break on must
// still arrive intact, and must not bleed into the second field.
func TestRunCommandFramesAwkwardPassword(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell")
	}

	out := filepath.Join(t.TempDir(), "fields.txt")
	script := writeBashHookScript(t, `IFS= read -r -d '' PASSWORD
IFS= read -r -d '' MFA_CODE
printf '[%s][%s]' "$PASSWORD" "$MFA_CODE" > `+out+`
echo hook.action=block
`)

	const password = "line one\nline two\ttabbed  "
	a := &HookAuth{
		Command: script,
		Cred:    hookCred{Username: "alice", Password: password, MFACode: "123456"},
	}

	if _, err := a.RunCommand(context.Background()); err != nil {
		t.Fatalf("RunCommand returned error: %v", err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("failed to read captured fields: %v", err)
	}
	if want := "[" + password + "][123456]"; string(got) != want {
		t.Errorf("hook received %q, want %q", got, want)
	}
}

// A hook written before the second field reads stdin whole. It must still see
// exactly the password when no code is in play, which is the common case and
// the one that decides whether an in-flight deploy can log anyone in.
func TestRunCommandStdinCompatibleWithSingleFieldHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell")
	}

	out := filepath.Join(t.TempDir(), "password.txt")
	script := writeBashHookScript(t, `PASSWORD=$(cat)
printf '[%s]' "$PASSWORD" > `+out+`
echo hook.action=block
`)

	a := &HookAuth{
		Command: script,
		Cred:    hookCred{Username: "alice", Password: "secret"},
	}

	if _, err := a.RunCommand(context.Background()); err != nil {
		t.Fatalf("RunCommand returned error: %v", err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("failed to read captured password: %v", err)
	}
	if string(got) != "[secret]" {
		t.Errorf("a single-field hook received %q, want %q", got, "[secret]")
	}
}

func TestRunCommandKeepsPasswordOutOfEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell")
	}

	out := filepath.Join(t.TempDir(), "env.txt")
	script := writeHookScript(t, "env > "+out+"\necho hook.action=block\n")

	const password = "uniq-passphrase-do-not-leak"
	const mfaCode = "uniq-code-do-not-leak"
	a := &HookAuth{
		Command: script,
		Cred:    hookCred{Username: "alice", Password: password, MFACode: mfaCode},
	}

	if _, err := a.RunCommand(context.Background()); err != nil {
		t.Fatalf("RunCommand returned error: %v", err)
	}

	env, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("failed to read captured environment: %v", err)
	}
	if strings.Contains(string(env), password) {
		t.Error("VULNERABLE: the password was passed in the hook's environment")
	}
	if strings.Contains(string(env), mfaCode) {
		t.Error("VULNERABLE: the MFA code was passed in the hook's environment")
	}
	if !strings.Contains(string(env), "USERNAME=alice") {
		t.Error("the username should still be provided in the environment")
	}
}

// A hook that asks for a second factor must be distinguishable from one that
// rejected the credentials: the login page keeps the form open and prompts for
// a code, rather than reporting the password as wrong.
func TestAuthReturnsMFAChallenge(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell")
	}

	tests := []struct {
		name        string
		output      string
		wantMethod  string
		wantInvalid bool
	}{
		{
			name:       "emailed code",
			output:     "hook.action=mfa\nhook.mfa.method=email\n",
			wantMethod: "email",
		},
		{
			name:       "authenticator code",
			output:     "hook.action=mfa\nhook.mfa.method=totp\n",
			wantMethod: "totp",
		},
		{
			name:        "code rejected",
			output:      "hook.action=mfa\nhook.mfa.method=email\nhook.mfa.error=invalid\n",
			wantMethod:  "email",
			wantInvalid: true,
		},
		{
			name:   "method withheld",
			output: "hook.action=mfa\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &HookAuth{Command: writeHookScript(t, "cat <<'FIELDS'\n"+tt.output+"FIELDS\n")}

			body := strings.NewReader(`{"username":"alice","password":"secret","mfaCode":"000000"}`)
			r := httptest.NewRequest(http.MethodPost, "/api/login", body)

			u, err := a.Auth(r, nil, &settings.Settings{}, &settings.Server{})
			if u != nil {
				t.Fatal("a challenge must not authenticate the user")
			}
			if !errors.Is(err, ErrMFARequired) {
				t.Fatalf("expected an MFA challenge, got %v", err)
			}
			if errors.Is(err, os.ErrPermission) {
				t.Error("a challenge must not read as a rejected credential")
			}

			var challenge *MFAChallenge
			if !errors.As(err, &challenge) {
				t.Fatalf("expected an *MFAChallenge, got %T", err)
			}
			if challenge.Method != tt.wantMethod {
				t.Errorf("method = %q, want %q", challenge.Method, tt.wantMethod)
			}
			if challenge.Invalid != tt.wantInvalid {
				t.Errorf("invalid = %v, want %v", challenge.Invalid, tt.wantInvalid)
			}
		})
	}
}

func TestRunCommandTimesOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell")
	}

	script := writeHookScript(t, "sleep 30\necho hook.action=auth\n")

	a := &HookAuth{
		Command: script,
		Cred:    hookCred{Username: "alice", Password: "secret"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := a.RunCommand(ctx); err == nil {
		t.Fatal("expected a hanging hook to fail rather than block")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("hook was not cut short: took %s (the deadline should release the request)", elapsed)
	}
}

func TestAuthHandoffExchangesTheCodeForTheVouchedUser(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell")
	}

	script := writeBashHookScript(t, `IFS= read -r -d '' CODE
if [ "${1:-}" = handoff ] && [ "$CODE" = one-time-code ]; then
  echo hook.action=auth
  echo hook.user=alice@example.com
  echo user.perm.create=true
else
  echo hook.action=block
fi
`)

	a := &HookAuth{Command: script}

	u, err := a.AuthHandoff(context.Background(), "one-time-code", newProvisionStore(), &settings.Settings{
		Key:      []byte("key"),
		Defaults: settings.UserDefaults{Scope: "."},
	}, &settings.Server{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("AuthHandoff error: %v", err)
	}
	if u.Username != "alice@example.com" {
		t.Errorf("username = %q, want the hook.user the platform vouched for", u.Username)
	}
	if !u.Perm.Create || u.Perm.Admin {
		t.Errorf("permissions did not follow the hook response: %+v", u.Perm)
	}
	if !u.LockPassword {
		t.Error("a handoff-provisioned user must not manage its own password")
	}
}

// A replayed interactive-login response — hook.action=auth with no hook.user —
// must not authenticate as anyone: with no typed username there is no one the
// exchange could legitimately become.
func TestAuthHandoffRefusesAuthWithoutAVouchedUser(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell")
	}

	script := writeHookScript(t, "echo hook.action=auth\n")

	a := &HookAuth{Command: script}

	u, err := a.AuthHandoff(context.Background(), "any", newProvisionStore(), &settings.Settings{}, &settings.Server{})
	if u != nil {
		t.Fatal("an unvouched exchange must not authenticate")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("expected os.ErrPermission, got %v", err)
	}
}

func TestAuthHandoffRefusesABlockedCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell")
	}

	script := writeHookScript(t, "echo hook.action=block\n")

	a := &HookAuth{Command: script}

	if _, err := a.AuthHandoff(context.Background(), "spent", newProvisionStore(), &settings.Settings{}, &settings.Server{}); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("expected os.ErrPermission, got %v", err)
	}
}

// The code travels on stdin like the credentials do: nothing secret may reach
// the hook through its argument list or environment, where other processes on
// the node could read it.
func TestAuthHandoffKeepsTheCodeOffTheCommandLineAndEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell")
	}

	dir := t.TempDir()
	argsOut := filepath.Join(dir, "args.txt")
	envOut := filepath.Join(dir, "env.txt")
	codeOut := filepath.Join(dir, "code.txt")

	script := writeBashHookScript(t, `printf '%s' "$*" > `+argsOut+`
env > `+envOut+`
IFS= read -r -d '' CODE
printf '%s' "$CODE" > `+codeOut+`
echo hook.action=block
`)

	const code = "uniq-handoff-code-do-not-leak"
	a := &HookAuth{Command: script}

	if _, err := a.AuthHandoff(context.Background(), code, newProvisionStore(), &settings.Settings{}, &settings.Server{}); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("expected os.ErrPermission, got %v", err)
	}

	args, err := os.ReadFile(argsOut)
	if err != nil {
		t.Fatalf("failed to read captured args: %v", err)
	}
	if string(args) != "handoff" {
		t.Errorf("hook args = %q, want just the handoff marker", args)
	}

	env, err := os.ReadFile(envOut)
	if err != nil {
		t.Fatalf("failed to read captured environment: %v", err)
	}
	if strings.Contains(string(env), code) {
		t.Error("VULNERABLE: the handoff code was passed in the hook's environment")
	}

	got, err := os.ReadFile(codeOut)
	if err != nil {
		t.Fatalf("failed to read captured code: %v", err)
	}
	if string(got) != code {
		t.Errorf("hook received code %q on stdin, want %q", got, code)
	}
}

func TestSplitCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		command  string
		wantName string
		wantArgs []string
		wantErr  bool
	}{
		{name: "bare path", command: "/usr/bin/hook", wantName: "/usr/bin/hook"},
		{name: "path with args", command: "/usr/bin/hook -v --mode=x",
			wantName: "/usr/bin/hook", wantArgs: []string{"-v", "--mode=x"}},
		{name: "collapses repeated spaces", command: "/usr/bin/hook   -v",
			wantName: "/usr/bin/hook", wantArgs: []string{"-v"}},
		{name: "leading and trailing space", command: "  /usr/bin/hook  ", wantName: "/usr/bin/hook"},
		{name: "tab separated", command: "/usr/bin/hook\t-v",
			wantName: "/usr/bin/hook", wantArgs: []string{"-v"}},
		{name: "double quoted path with space", command: `"/opt/my hook/auth.sh"`,
			wantName: "/opt/my hook/auth.sh"},
		{name: "single quoted path with space", command: `'/opt/my hook/auth.sh' -v`,
			wantName: "/opt/my hook/auth.sh", wantArgs: []string{"-v"}},
		{name: "quoted argument", command: `/usr/bin/hook "two words"`,
			wantName: "/usr/bin/hook", wantArgs: []string{"two words"}},
		{name: "empty", command: "", wantErr: true},
		{name: "only spaces", command: "   ", wantErr: true},
		{name: "unterminated quote", command: `"/opt/hook`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, args, err := splitCommand(tt.command)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tt.command)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.command, err)
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
			if len(args) != len(tt.wantArgs) {
				t.Fatalf("args = %v, want %v", args, tt.wantArgs)
			}
			for i := range args {
				if args[i] != tt.wantArgs[i] {
					t.Errorf("args[%d] = %q, want %q", i, args[i], tt.wantArgs[i])
				}
			}
		})
	}
}

func TestHookCannotGrantAdmin(t *testing.T) {
	t.Parallel()

	store := newProvisionStore()
	srv := &settings.Server{Root: t.TempDir()}
	s := &settings.Settings{
		Key: []byte("key"),
		Defaults: settings.UserDefaults{
			Scope: ".",
			Perm:  users.Permissions{Create: true},
		},
	}

	u, err := newHookAuth(store, s, srv, "mallory", map[string]string{
		"user.perm.admin": "true",
	}).SaveUser()
	if err != nil {
		t.Fatalf("SaveUser error: %v", err)
	}

	if u.Perm.Admin {
		t.Error("VULNERABLE: the hook granted administrator rights")
	}
}

func TestHookAdminClaimDoesNotImplyOtherPerms(t *testing.T) {
	t.Parallel()

	store := newProvisionStore()
	srv := &settings.Server{Root: t.TempDir()}
	s := &settings.Settings{
		Key: []byte("key"),
		Defaults: settings.UserDefaults{
			Scope: ".",
			Perm:  users.Permissions{}, // nothing granted by default
		},
	}

	u, err := newHookAuth(store, s, srv, "mallory", map[string]string{
		"user.perm.admin": "true",
	}).SaveUser()
	if err != nil {
		t.Fatalf("SaveUser error: %v", err)
	}

	if u.Perm.Admin || u.Perm.Delete || u.Perm.Modify || u.Perm.Share || u.Perm.Download {
		t.Errorf("VULNERABLE: admin claim leaked permissions: %+v", u.Perm)
	}
}

func newHookAuth(store *provisionStore, s *settings.Settings, srv *settings.Server, username string, fields map[string]string) *HookAuth {
	fields["hook.action"] = "auth"
	return &HookAuth{
		Users:    store,
		Settings: s,
		Server:   srv,
		Cred:     hookCred{Username: username, Password: "a-strong-password"},
		Fields:   hookFields{Values: fields},
	}
}

// With CreateUserDir enabled and no explicit scope from the hook, a provisioned
// hook user must receive its own home directory rather than the server root.
func TestHookSaveUserCreateUserDirIsolatesScope(t *testing.T) {
	t.Parallel()

	store := newProvisionStore()
	srv := &settings.Server{Root: t.TempDir()}
	s := &settings.Settings{
		Key:              []byte("key"),
		CreateUserDir:    true,
		UserHomeBasePath: "/users",
		Defaults: settings.UserDefaults{
			Scope: ".",
			Perm:  users.Permissions{Create: true},
		},
	}

	u, err := newHookAuth(store, s, srv, "alice", map[string]string{}).SaveUser()
	if err != nil {
		t.Fatalf("SaveUser error: %v", err)
	}
	if u.Scope != "/users/alice" {
		t.Errorf("hook user without explicit scope: expected /users/alice, got %q", u.Scope)
	}
}

// A scope explicitly returned by the hook takes precedence over the automatic
// per-user home directory derivation.
func TestHookSaveUserRespectsExplicitScope(t *testing.T) {
	t.Parallel()

	store := newProvisionStore()
	srv := &settings.Server{Root: t.TempDir()}
	s := &settings.Settings{
		Key:              []byte("key"),
		CreateUserDir:    true,
		UserHomeBasePath: "/users",
		Defaults: settings.UserDefaults{
			Scope: ".",
			Perm:  users.Permissions{Create: true},
		},
	}

	u, err := newHookAuth(store, s, srv, "teamlead", map[string]string{"user.scope": "/shared/team"}).SaveUser()
	if err != nil {
		t.Fatalf("SaveUser error: %v", err)
	}
	if u.Scope != "/shared/team" {
		t.Errorf("explicit hook scope should win, got %q", u.Scope)
	}
}
