package cmd

import (
	"testing"

	"github.com/spf13/pflag"

	"github.com/rforced/filebrowser/v2/auth"
)

func newRecaptchaFlags() *pflag.FlagSet {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("recaptcha.key", "", "")
	flags.String("recaptcha.secret", "", "")
	flags.String("recaptcha.project", "", "")
	flags.String("recaptcha.allowed-hostnames", "", "")
	return flags
}

func TestGetJSONAuthAllFlags(t *testing.T) {
	t.Parallel()

	flags := newRecaptchaFlags()
	_ = flags.Set("recaptcha.key", "site-key")
	_ = flags.Set("recaptcha.secret", "api-key")
	_ = flags.Set("recaptcha.project", "my-project")

	auther, err := getJSONAuth(flags, nil)
	if err != nil {
		t.Fatalf("getJSONAuth() error: %v", err)
	}

	ja, ok := auther.(*auth.JSONAuth)
	if !ok {
		t.Fatalf("expected *auth.JSONAuth, got %T", auther)
	}
	if ja.ReCaptcha == nil {
		t.Fatal("ReCaptcha should be set when all three fields are provided")
	}
	if ja.ReCaptcha.Key != "site-key" {
		t.Errorf("Key = %q, want %q", ja.ReCaptcha.Key, "site-key")
	}
	if ja.ReCaptcha.Secret != "api-key" {
		t.Errorf("Secret = %q, want %q", ja.ReCaptcha.Secret, "api-key")
	}
	if ja.ReCaptcha.ProjectID != "my-project" {
		t.Errorf("ProjectID = %q, want %q", ja.ReCaptcha.ProjectID, "my-project")
	}
}

func TestGetJSONAuthPartialFlags(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		key, secret, project string
	}{
		"only key":           {key: "k"},
		"only secret":        {secret: "s"},
		"only project":       {project: "p"},
		"key and secret":     {key: "k", secret: "s"},
		"key and project":    {key: "k", project: "p"},
		"secret and project": {secret: "s", project: "p"},
		"none set":           {},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			flags := newRecaptchaFlags()
			if tc.key != "" {
				_ = flags.Set("recaptcha.key", tc.key)
			}
			if tc.secret != "" {
				_ = flags.Set("recaptcha.secret", tc.secret)
			}
			if tc.project != "" {
				_ = flags.Set("recaptcha.project", tc.project)
			}

			auther, err := getJSONAuth(flags, nil)
			if err != nil {
				t.Fatalf("getJSONAuth() error: %v", err)
			}

			ja := auther.(*auth.JSONAuth)
			if ja.ReCaptcha != nil {
				t.Error("ReCaptcha should be nil when not all three fields are provided")
			}
		})
	}
}

func TestGetJSONAuthHostnames(t *testing.T) {
	t.Parallel()

	flags := newRecaptchaFlags()
	_ = flags.Set("recaptcha.key", "k")
	_ = flags.Set("recaptcha.secret", "s")
	_ = flags.Set("recaptcha.project", "p")
	_ = flags.Set("recaptcha.allowed-hostnames", "192.168.1.1, 10.0.0.1 ,example.com")

	auther, err := getJSONAuth(flags, nil)
	if err != nil {
		t.Fatalf("getJSONAuth() error: %v", err)
	}

	ja := auther.(*auth.JSONAuth)
	if ja.ReCaptcha == nil {
		t.Fatal("ReCaptcha should be set")
	}

	want := []string{"192.168.1.1", "10.0.0.1", "example.com"}
	if len(ja.ReCaptcha.AllowedHostnames) != len(want) {
		t.Fatalf("AllowedHostnames length = %d, want %d", len(ja.ReCaptcha.AllowedHostnames), len(want))
	}
	for i, h := range ja.ReCaptcha.AllowedHostnames {
		if h != want[i] {
			t.Errorf("AllowedHostnames[%d] = %q, want %q", i, h, want[i])
		}
	}
}

func TestGetJSONAuthNoHostnames(t *testing.T) {
	t.Parallel()

	flags := newRecaptchaFlags()
	_ = flags.Set("recaptcha.key", "k")
	_ = flags.Set("recaptcha.secret", "s")
	_ = flags.Set("recaptcha.project", "p")

	auther, err := getJSONAuth(flags, nil)
	if err != nil {
		t.Fatalf("getJSONAuth() error: %v", err)
	}

	ja := auther.(*auth.JSONAuth)
	if ja.ReCaptcha == nil {
		t.Fatal("ReCaptcha should be set")
	}
	if ja.ReCaptcha.AllowedHostnames != nil {
		t.Errorf("AllowedHostnames should be nil, got %v", ja.ReCaptcha.AllowedHostnames)
	}
}

func TestGetJSONAuthDefaultAuther(t *testing.T) {
	t.Parallel()

	flags := newRecaptchaFlags()

	defaultAuther := map[string]interface{}{
		"recaptcha": map[string]interface{}{
			"key":        "default-key",
			"secret":     "default-secret",
			"project_id": "default-project",
			"allowed_hostnames": []interface{}{
				"192.168.1.1",
				"10.0.0.1",
			},
		},
	}

	auther, err := getJSONAuth(flags, defaultAuther)
	if err != nil {
		t.Fatalf("getJSONAuth() error: %v", err)
	}

	ja := auther.(*auth.JSONAuth)
	if ja.ReCaptcha == nil {
		t.Fatal("ReCaptcha should be populated from defaults")
	}
	if ja.ReCaptcha.Key != "default-key" {
		t.Errorf("Key = %q, want %q", ja.ReCaptcha.Key, "default-key")
	}
	if ja.ReCaptcha.Secret != "default-secret" {
		t.Errorf("Secret = %q, want %q", ja.ReCaptcha.Secret, "default-secret")
	}
	if ja.ReCaptcha.ProjectID != "default-project" {
		t.Errorf("ProjectID = %q, want %q", ja.ReCaptcha.ProjectID, "default-project")
	}
	if len(ja.ReCaptcha.AllowedHostnames) != 2 {
		t.Fatalf("AllowedHostnames length = %d, want 2", len(ja.ReCaptcha.AllowedHostnames))
	}
}

func TestGetJSONAuthFlagsOverrideDefaults(t *testing.T) {
	t.Parallel()

	flags := newRecaptchaFlags()
	_ = flags.Set("recaptcha.key", "flag-key")
	_ = flags.Set("recaptcha.secret", "flag-secret")
	_ = flags.Set("recaptcha.project", "flag-project")

	defaultAuther := map[string]interface{}{
		"recaptcha": map[string]interface{}{
			"key":        "default-key",
			"secret":     "default-secret",
			"project_id": "default-project",
		},
	}

	auther, err := getJSONAuth(flags, defaultAuther)
	if err != nil {
		t.Fatalf("getJSONAuth() error: %v", err)
	}

	ja := auther.(*auth.JSONAuth)
	if ja.ReCaptcha == nil {
		t.Fatal("ReCaptcha should be set")
	}
	if ja.ReCaptcha.Key != "flag-key" {
		t.Errorf("Key = %q, want %q (flags should override defaults)", ja.ReCaptcha.Key, "flag-key")
	}
	if ja.ReCaptcha.Secret != "flag-secret" {
		t.Errorf("Secret = %q, want %q", ja.ReCaptcha.Secret, "flag-secret")
	}
	if ja.ReCaptcha.ProjectID != "flag-project" {
		t.Errorf("ProjectID = %q, want %q", ja.ReCaptcha.ProjectID, "flag-project")
	}
}
