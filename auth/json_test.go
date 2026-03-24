package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	recaptchapb "cloud.google.com/go/recaptchaenterprise/v2/apiv1/recaptchaenterprisepb"

	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/users"
)

// mockAssessor implements Assessor for testing.
type mockAssessor struct {
	response *recaptchapb.Assessment
	err      error
}

func (m *mockAssessor) CreateAssessment(_ context.Context, _ *recaptchapb.CreateAssessmentRequest) (*recaptchapb.Assessment, error) {
	return m.response, m.err
}

func validAssessment(score float32, action, hostname string) *recaptchapb.Assessment {
	return &recaptchapb.Assessment{
		TokenProperties: &recaptchapb.TokenProperties{
			Valid:    true,
			Action:   action,
			Hostname: hostname,
		},
		RiskAnalysis: &recaptchapb.RiskAnalysis{
			Score: score,
		},
	}
}

func invalidTokenAssessment() *recaptchapb.Assessment {
	return &recaptchapb.Assessment{
		TokenProperties: &recaptchapb.TokenProperties{
			Valid:         false,
			InvalidReason: recaptchapb.TokenProperties_EXPIRED,
		},
		RiskAnalysis: &recaptchapb.RiskAnalysis{
			Score: 0.9,
		},
	}
}

func TestReCaptchaOk(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		assessor *mockAssessor
		rc       *ReCaptcha
		wantOk   bool
		wantErr  bool
	}{
		"valid token with good score": {
			assessor: &mockAssessor{response: validAssessment(0.9, "login", "example.com")},
			rc:       &ReCaptcha{Key: "key", Secret: "secret", ProjectID: "proj"},
			wantOk:   true,
		},
		"valid token at score threshold": {
			assessor: &mockAssessor{response: validAssessment(0.5, "login", "example.com")},
			rc:       &ReCaptcha{Key: "key", Secret: "secret", ProjectID: "proj"},
			wantOk:   true,
		},
		"invalid token": {
			assessor: &mockAssessor{response: invalidTokenAssessment()},
			rc:       &ReCaptcha{Key: "key", Secret: "secret", ProjectID: "proj"},
			wantOk:   false,
		},
		"wrong action": {
			assessor: &mockAssessor{response: validAssessment(0.9, "signup", "example.com")},
			rc:       &ReCaptcha{Key: "key", Secret: "secret", ProjectID: "proj"},
			wantOk:   false,
		},
		"low score": {
			assessor: &mockAssessor{response: validAssessment(0.3, "login", "example.com")},
			rc:       &ReCaptcha{Key: "key", Secret: "secret", ProjectID: "proj"},
			wantOk:   false,
		},
		"score just below threshold": {
			assessor: &mockAssessor{response: validAssessment(0.49, "login", "example.com")},
			rc:       &ReCaptcha{Key: "key", Secret: "secret", ProjectID: "proj"},
			wantOk:   false,
		},
		"hostname allowed": {
			assessor: &mockAssessor{response: validAssessment(0.9, "login", "192.168.1.100")},
			rc:       &ReCaptcha{Key: "key", Secret: "secret", ProjectID: "proj", AllowedHostnames: []string{"192.168.1.100", "10.0.0.1"}},
			wantOk:   true,
		},
		"hostname rejected": {
			assessor: &mockAssessor{response: validAssessment(0.9, "login", "evil.com")},
			rc:       &ReCaptcha{Key: "key", Secret: "secret", ProjectID: "proj", AllowedHostnames: []string{"192.168.1.100"}},
			wantOk:   false,
		},
		"empty allowed hostnames skips check": {
			assessor: &mockAssessor{response: validAssessment(0.9, "login", "anything.com")},
			rc:       &ReCaptcha{Key: "key", Secret: "secret", ProjectID: "proj", AllowedHostnames: nil},
			wantOk:   true,
		},
		"API error": {
			assessor: &mockAssessor{err: errors.New("connection refused")},
			rc:       &ReCaptcha{Key: "key", Secret: "secret", ProjectID: "proj"},
			wantOk:   false,
			wantErr:  true,
		},
		"zero score bot": {
			assessor: &mockAssessor{response: validAssessment(0.0, "login", "example.com")},
			rc:       &ReCaptcha{Key: "key", Secret: "secret", ProjectID: "proj"},
			wantOk:   false,
		},
		"perfect score": {
			assessor: &mockAssessor{response: validAssessment(1.0, "login", "example.com")},
			rc:       &ReCaptcha{Key: "key", Secret: "secret", ProjectID: "proj"},
			wantOk:   true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tc.rc.Assessor = tc.assessor
			ok, err := tc.rc.Ok("test-token")

			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != tc.wantOk {
				t.Errorf("Ok() = %v, want %v", ok, tc.wantOk)
			}
		})
	}
}

func TestReCaptchaOkRequestFields(t *testing.T) {
	t.Parallel()

	var captured *recaptchapb.CreateAssessmentRequest
	assessor := &mockAssessor{response: validAssessment(0.9, "login", "example.com")}

	// Wrap to capture the request
	rc := &ReCaptcha{
		Key:       "my-site-key",
		Secret:    "my-secret",
		ProjectID: "my-project",
		Assessor: assessorFunc(func(ctx context.Context, req *recaptchapb.CreateAssessmentRequest) (*recaptchapb.Assessment, error) {
			captured = req
			return assessor.CreateAssessment(ctx, req)
		}),
	}

	_, err := rc.Ok("the-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if captured == nil {
		t.Fatal("CreateAssessment was not called")
	}
	if captured.Parent != "projects/my-project" {
		t.Errorf("Parent = %q, want %q", captured.Parent, "projects/my-project")
	}
	if captured.Assessment.Event.Token != "the-token" {
		t.Errorf("Token = %q, want %q", captured.Assessment.Event.Token, "the-token")
	}
	if captured.Assessment.Event.SiteKey != "my-site-key" {
		t.Errorf("SiteKey = %q, want %q", captured.Assessment.Event.SiteKey, "my-site-key")
	}
}

// assessorFunc adapts a function to the Assessor interface.
type assessorFunc func(ctx context.Context, req *recaptchapb.CreateAssessmentRequest) (*recaptchapb.Assessment, error)

func (f assessorFunc) CreateAssessment(ctx context.Context, req *recaptchapb.CreateAssessmentRequest) (*recaptchapb.Assessment, error) {
	return f(ctx, req)
}

// mockUserStore is a minimal users.Store for Auth tests.
type mockUserStore struct {
	user *users.User
	err  error
}

func (m *mockUserStore) Get(_ string, id interface{}) (*users.User, error) {
	if m.err != nil {
		return nil, m.err
	}
	username, ok := id.(string)
	if !ok {
		return nil, os.ErrNotExist
	}
	if m.user != nil && m.user.Username == username {
		return m.user, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockUserStore) Gets(_ string) ([]*users.User, error)    { return nil, nil }
func (m *mockUserStore) Save(_ *users.User) error                { return nil }
func (m *mockUserStore) Update(_ *users.User, _ ...string) error { return nil }
func (m *mockUserStore) Delete(_ interface{}) error              { return nil }
func (m *mockUserStore) LastUpdate(_ uint) int64                 { return 0 }

func makeTestUser(username, password string) *users.User {
	u := &users.User{Username: username}
	u.Password, _ = users.HashPwd(password)
	return u
}

func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(v); err != nil {
		t.Fatalf("failed to encode body: %v", err)
	}
	return buf
}

func TestJSONAuthWithRecaptchaDisabled(t *testing.T) {
	t.Parallel()

	testUser := makeTestUser("admin", "password123")
	store := &mockUserStore{user: testUser}
	srv := &settings.Server{Root: "/"}
	set := &settings.Settings{}

	a := JSONAuth{ReCaptcha: nil}

	body := jsonBody(t, jsonCred{Username: "admin", Password: "password123"})
	req := httptest.NewRequest(http.MethodPost, "/api/login", body)
	req.Header.Set("Content-Type", "application/json")

	u, err := a.Auth(req, store, set, srv)
	if err != nil {
		t.Fatalf("Auth() returned error: %v", err)
	}
	if u.Username != "admin" {
		t.Errorf("Username = %q, want %q", u.Username, "admin")
	}
}

func TestJSONAuthWithRecaptchaEnabled(t *testing.T) {
	t.Parallel()

	testUser := makeTestUser("admin", "password123")
	store := &mockUserStore{user: testUser}
	srv := &settings.Server{Root: "/"}
	set := &settings.Settings{}

	tests := map[string]struct {
		assessor *mockAssessor
		wantErr  bool
	}{
		"valid recaptcha allows login": {
			assessor: &mockAssessor{response: validAssessment(0.9, "login", "localhost")},
			wantErr:  false,
		},
		"invalid recaptcha blocks login": {
			assessor: &mockAssessor{response: invalidTokenAssessment()},
			wantErr:  true,
		},
		"recaptcha API error blocks login": {
			assessor: &mockAssessor{err: errors.New("API down")},
			wantErr:  true,
		},
		"low score blocks login": {
			assessor: &mockAssessor{response: validAssessment(0.1, "login", "localhost")},
			wantErr:  true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			a := JSONAuth{
				ReCaptcha: &ReCaptcha{
					Key:       "key",
					Secret:    "secret",
					ProjectID: "proj",
					Assessor:  tc.assessor,
				},
			}

			body := jsonBody(t, jsonCred{Username: "admin", Password: "password123", ReCaptcha: "token"})
			req := httptest.NewRequest(http.MethodPost, "/api/login", body)
			req.Header.Set("Content-Type", "application/json")

			u, err := a.Auth(req, store, set, srv)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if u.Username != "admin" {
					t.Errorf("Username = %q, want %q", u.Username, "admin")
				}
			}
		})
	}
}

func TestJSONAuthNilBody(t *testing.T) {
	t.Parallel()

	a := JSONAuth{}
	srv := &settings.Server{Root: "/"}
	set := &settings.Settings{}
	store := &mockUserStore{}

	req := httptest.NewRequest(http.MethodPost, "/api/login", nil)
	req.Body = nil

	_, err := a.Auth(req, store, set, srv)
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("expected os.ErrPermission, got %v", err)
	}
}

func TestJSONAuthBadCredentials(t *testing.T) {
	t.Parallel()

	testUser := makeTestUser("admin", "password123")
	store := &mockUserStore{user: testUser}
	srv := &settings.Server{Root: "/"}
	set := &settings.Settings{}

	a := JSONAuth{}

	body := jsonBody(t, jsonCred{Username: "admin", Password: "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/api/login", body)

	_, err := a.Auth(req, store, set, srv)
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("expected os.ErrPermission, got %v", err)
	}
}

func TestJSONAuthUnknownUser(t *testing.T) {
	t.Parallel()

	store := &mockUserStore{user: makeTestUser("admin", "pass")}
	srv := &settings.Server{Root: "/"}
	set := &settings.Settings{}

	a := JSONAuth{}

	body := jsonBody(t, jsonCred{Username: "nobody", Password: "pass"})
	req := httptest.NewRequest(http.MethodPost, "/api/login", body)

	_, err := a.Auth(req, store, set, srv)
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("expected os.ErrPermission, got %v", err)
	}
}

func TestJSONAuthLoginPage(t *testing.T) {
	t.Parallel()

	a := JSONAuth{}
	if !a.LoginPage() {
		t.Error("LoginPage() should return true")
	}
}

func TestReCaptchaStructJSON(t *testing.T) {
	t.Parallel()

	rc := &ReCaptcha{
		Key:              "site-key",
		Secret:           "api-key",
		ProjectID:        "my-project",
		AllowedHostnames: []string{"192.168.1.1"},
		Assessor:         &mockAssessor{},
	}

	data, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Assessor should not appear in JSON (json:"-")
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if _, ok := m["Assessor"]; ok {
		t.Error("Assessor field should be excluded from JSON")
	}
	if m["key"] != "site-key" {
		t.Errorf("key = %v, want %q", m["key"], "site-key")
	}
	if m["project_id"] != "my-project" {
		t.Errorf("project_id = %v, want %q", m["project_id"], "my-project")
	}
}
