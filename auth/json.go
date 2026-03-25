package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"slices"

	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/users"
)

// assessmentResponse mirrors the relevant fields from a reCAPTCHA Enterprise
// CreateAssessment REST response.
type assessmentResponse struct {
	TokenProperties struct {
		Valid         bool   `json:"valid"`
		InvalidReason string `json:"invalidReason,omitempty"`
		Action        string `json:"action"`
		Hostname      string `json:"hostname"`
	} `json:"tokenProperties"`
	RiskAnalysis struct {
		Score   float32  `json:"score"`
		Reasons []string `json:"reasons,omitempty"`
	} `json:"riskAnalysis"`
}

// assessmentRequest mirrors the CreateAssessment REST request body.
type assessmentRequest struct {
	Event struct {
		Token          string `json:"token"`
		SiteKey        string `json:"siteKey"`
		ExpectedAction string `json:"expectedAction"`
	} `json:"event"`
}

// Assessor abstracts the reCAPTCHA Enterprise assessment call for testing.
type Assessor interface {
	CreateAssessment(ctx context.Context, projectID string, req *assessmentRequest) (*assessmentResponse, error)
}

// MethodJSONAuth is used to identify json auth.
const MethodJSONAuth settings.AuthMethod = "json"

// ErrCaptchaFailed is returned when the reCAPTCHA verification fails.
var ErrCaptchaFailed = errors.New("captcha verification failed")

// dummyHash is used to prevent user enumeration timing attacks.
// It MUST be a valid bcrypt hash.
const dummyHash = "$2a$10$O4mEMeOL/nit6zqe.WQXauLRbRlzb3IgLHsa26Pf0N/GiU9b.wK1m"

type jsonCred struct {
	Password  string `json:"password"`
	Username  string `json:"username"`
	ReCaptcha string `json:"recaptcha"`
}

// JSONAuth is a json implementation of an Auther.
type JSONAuth struct {
	ReCaptcha *ReCaptcha `json:"recaptcha" yaml:"recaptcha"`
}

// Auth authenticates the user via a json in content body.
func (a JSONAuth) Auth(r *http.Request, usr users.Store, _ *settings.Settings, srv *settings.Server) (*users.User, error) {
	var cred jsonCred

	if r.Body == nil {
		return nil, os.ErrPermission
	}

	err := json.NewDecoder(r.Body).Decode(&cred)
	if err != nil {
		return nil, os.ErrPermission
	}

	// If ReCaptcha is enabled, check the code.
	if a.ReCaptcha != nil && a.ReCaptcha.Secret != "" {
		ok, err := a.ReCaptcha.Ok(cred.ReCaptcha)

		if err != nil {
			return nil, err
		}

		if !ok {
			return nil, ErrCaptchaFailed
		}
	}

	u, err := usr.Get(srv.Root, cred.Username)

	hash := dummyHash
	if err == nil {
		hash = u.Password
	}

	if !users.CheckPwd(cred.Password, hash) {
		return nil, os.ErrPermission
	}

	if err != nil {
		return nil, os.ErrPermission
	}

	return u, nil
}

// LoginPage tells that json auth doesn't require a login page.
func (a JSONAuth) LoginPage() bool {
	return true
}

// ReCaptcha identifies a reCAPTCHA Enterprise connection.
type ReCaptcha struct {
	Key              string   `json:"key"`
	Secret           string   `json:"secret"`
	ProjectID        string   `json:"project_id"`
	AllowedHostnames []string `json:"allowed_hostnames,omitempty"`
	Assessor         Assessor `json:"-" yaml:"-"`
}

// restAssessor calls the reCAPTCHA Enterprise REST API with an API key.
type restAssessor struct {
	apiKey     string
	httpClient *http.Client
}

func (a *restAssessor) CreateAssessment(_ context.Context, projectID string, req *assessmentRequest) (*assessmentResponse, error) {
	url := fmt.Sprintf(
		"https://recaptchaenterprise.googleapis.com/v1/projects/%s/assessments?key=%s",
		projectID, a.apiKey,
	)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("error marshalling assessment request: %w", err)
	}

	client := a.httpClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("error sending assessment request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading assessment response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("reCAPTCHA API returned status %d: %s", resp.StatusCode, respBody)
	}

	var result assessmentResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("error decoding assessment response: %w", err)
	}

	return &result, nil
}

// Ok checks if a reCAPTCHA Enterprise token is valid by creating an assessment.
func (r *ReCaptcha) Ok(token string) (bool, error) {
	if token == "" {
		log.Printf("[reCAPTCHA] Empty token received, rejecting")
		return false, nil
	}

	ctx := context.Background()

	assessor := r.Assessor
	if assessor == nil {
		assessor = &restAssessor{apiKey: r.Secret}
	}

	req := &assessmentRequest{}
	req.Event.Token = token
	req.Event.SiteKey = r.Key
	req.Event.ExpectedAction = "login"

	response, err := assessor.CreateAssessment(ctx, r.ProjectID, req)
	if err != nil {
		log.Printf("[reCAPTCHA] CreateAssessment error: %v", err)
		return false, fmt.Errorf("error calling CreateAssessment: %w", err)
	}

	log.Printf("[reCAPTCHA] Assessment result: valid=%v action=%q score=%.1f hostname=%q",
		response.TokenProperties.Valid,
		response.TokenProperties.Action,
		response.RiskAnalysis.Score,
		response.TokenProperties.Hostname,
	)

	// Check if the token is valid.
	if !response.TokenProperties.Valid {
		log.Printf("[reCAPTCHA] Token invalid, reason: %s", response.TokenProperties.InvalidReason)
		return false, nil
	}

	// Check if the expected action was executed.
	if response.TokenProperties.Action != "login" {
		log.Printf("[reCAPTCHA] Action mismatch: got %q, want %q", response.TokenProperties.Action, "login")
		return false, nil
	}

	// Score must be above the threshold (0.5 recommended by Google)
	if response.RiskAnalysis.Score < 0.5 {
		log.Printf("[reCAPTCHA] Score %.1f below threshold 0.5", response.RiskAnalysis.Score)
		return false, nil
	}

	// When domain validation is disabled in the reCAPTCHA console (e.g. for
	// IP-only deployments), anyone who obtains the site key can generate tokens
	// from any origin. Google recommends checking the hostname returned in the
	// response against an explicit allowlist to compensate.
	if len(r.AllowedHostnames) > 0 {
		hostname := response.TokenProperties.Hostname
		if !slices.Contains(r.AllowedHostnames, hostname) {
			log.Printf("[reCAPTCHA] Hostname %q not in allowed list %v", hostname, r.AllowedHostnames)
			return false, nil
		}
	}

	return true, nil
}
