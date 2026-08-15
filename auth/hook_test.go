package auth

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/users"
)

// writeHookScript writes a POSIX shell script to a temp file and returns its
// path, marking it executable.
func writeHookScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hook.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatalf("failed to write hook script: %v", err)
	}
	return path
}

func TestRunCommandNoCredentialInjection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell")
	}

	marker := filepath.Join(t.TempDir(), "pwned")

	// The hook simply blocks. If the credential were ever interpolated into the
	// command string and evaluated by a shell, the embedded `touch` would
	// create the marker file.
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

	script := writeHookScript(t, `PASSWORD=$(cat)
if [ "$USERNAME" = alice ] && [ "$PASSWORD" = secret ]; then
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

func TestRunCommandKeepsPasswordOutOfEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell")
	}

	out := filepath.Join(t.TempDir(), "env.txt")
	script := writeHookScript(t, "env > "+out+"\necho hook.action=block\n")

	const password = "uniq-passphrase-do-not-leak"
	a := &HookAuth{
		Command: script,
		Cred:    hookCred{Username: "alice", Password: password},
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
	if !strings.Contains(string(env), "USERNAME=alice") {
		t.Error("the username should still be provided in the environment")
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
