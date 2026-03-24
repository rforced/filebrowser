package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"

	recaptcha "cloud.google.com/go/recaptchaenterprise/v2/apiv1"
	recaptchapb "cloud.google.com/go/recaptchaenterprise/v2/apiv1/recaptchaenterprisepb"
	"google.golang.org/api/option"

	"github.com/rforced/filebrowser/v2/settings"
	"github.com/rforced/filebrowser/v2/users"
)

type Assessor interface {
	CreateAssessment(ctx context.Context, req *recaptchapb.CreateAssessmentRequest) (*recaptchapb.Assessment, error)
}

// MethodJSONAuth is used to identify json auth.
const MethodJSONAuth settings.AuthMethod = "json"

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
			return nil, os.ErrPermission
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

// gcloudAssessor wraps the real Google Cloud reCAPTCHA Enterprise client.
type gcloudAssessor struct {
	client *recaptcha.Client
}

func (g *gcloudAssessor) CreateAssessment(ctx context.Context, req *recaptchapb.CreateAssessmentRequest) (*recaptchapb.Assessment, error) {
	return g.client.CreateAssessment(ctx, req)
}

func (g *gcloudAssessor) Close() error {
	return g.client.Close()
}

// Ok checks if a reCAPTCHA Enterprise token is valid by creating an assessment.
func (r *ReCaptcha) Ok(token string) (bool, error) {
	ctx := context.Background()

	assessor := r.Assessor
	if assessor == nil {
		client, err := recaptcha.NewClient(ctx, option.WithAPIKey(r.Secret))
		if err != nil {
			return false, fmt.Errorf("error creating reCAPTCHA client: %w", err)
		}
		defer client.Close()
		assessor = &gcloudAssessor{client: client}
	}

	event := &recaptchapb.Event{
		Token:   token,
		SiteKey: r.Key,
	}

	request := &recaptchapb.CreateAssessmentRequest{
		Assessment: &recaptchapb.Assessment{
			Event: event,
		},
		Parent: fmt.Sprintf("projects/%s", r.ProjectID),
	}

	response, err := assessor.CreateAssessment(ctx, request)
	if err != nil {
		return false, fmt.Errorf("error calling CreateAssessment: %w", err)
	}

	// Check if the token is valid.
	if !response.TokenProperties.Valid {
		return false, nil
	}

	// Check if the expected action was executed.
	if response.TokenProperties.Action != "login" {
		return false, nil
	}

	// Score must be above threshold (0.5 recommended by Google)
	if response.RiskAnalysis.Score < 0.5 {
		return false, nil
	}

	// When domain validation is disabled in the reCAPTCHA console (e.g. for
	// IP-only deployments), anyone who obtains the site key can generate tokens
	// from any origin. Google recommends checking the hostname returned in the
	// response against an explicit allow-list to compensate.
	if len(r.AllowedHostnames) > 0 {
		hostname := response.TokenProperties.Hostname
		if !slices.Contains(r.AllowedHostnames, hostname) {
			return false, nil
		}
	}

	return true, nil
}
