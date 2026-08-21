package fbhttp

import (
	"encoding/base64"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/rforced/filebrowser/v2/settings"
)

func TestSecurityHeadersOnEveryRoute(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := securityHeaders(&settings.Server{}, inner)

	paths := []string{
		"/",              // SPA index — previously uncovered
		"/files/nested",  // SPA route — previously uncovered
		"/share/abc",     // SPA route — previously uncovered
		"/api/me",        // matched route
		"/static/app.js", // static assets
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			csp := rec.Header().Get("Content-Security-Policy")
			if csp == "" {
				t.Fatalf("no Content-Security-Policy on %s", path)
			}
			for _, directive := range []string{"default-src 'self'", "frame-ancestors 'none'"} {
				if !strings.Contains(csp, directive) {
					t.Errorf("CSP on %s is missing %q: %s", path, directive, csp)
				}
			}
			for _, allowed := range []string{
				"https://fonts.googleapis.com",
				"https://kit.fontawesome.com",
				"https://fonts.gstatic.com",
				"https://ka-p.fontawesome.com",
			} {
				if !strings.Contains(csp, allowed) {
					t.Errorf("CSP on %s no longer allows %s: %s", path, allowed, csp)
				}
			}
			for _, forbidden := range []string{"'unsafe-inline'", "'unsafe-eval'"} {
				if strings.Contains(scriptSrc(csp), forbidden) {
					t.Errorf("CSP on %s allows %s in script-src: %s", path, forbidden, csp)
				}
			}
			if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
				t.Errorf("Referrer-Policy on %s = %q, want no-referrer", path, got)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options on %s = %q, want nosniff", path, got)
			}
		})
	}
}

func scriptSrc(csp string) string {
	for _, directive := range strings.Split(csp, ";") {
		directive = strings.TrimSpace(directive)
		if after, ok := strings.CutPrefix(directive, "script-src "); ok {
			return after
		}
	}
	return ""
}

func TestSecurityHeadersIssueAFreshNoncePerResponse(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}

	for range 24 {
		var fromContext string
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fromContext = cspNonce(r)
			w.WriteHeader(http.StatusOK)
		})

		rec := httptest.NewRecorder()
		securityHeaders(&settings.Server{}, inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		if fromContext == "" {
			t.Fatal("no nonce published to the request context")
		}
		if seen[fromContext] {
			t.Fatalf("nonce %q was reused across responses", fromContext)
		}
		seen[fromContext] = true

		if _, err := base64.RawURLEncoding.DecodeString(fromContext); err != nil {
			t.Errorf("nonce %q is not the base64 a CSP nonce must be: %v", fromContext, err)
		}

		if got := template.HTMLEscapeString(fromContext); got != fromContext {
			t.Errorf("nonce %q needs HTML escaping (%q), so header and attribute would disagree", fromContext, got)
		}

		want := "'nonce-" + fromContext + "'"
		if got := scriptSrc(rec.Header().Get("Content-Security-Policy")); !strings.Contains(got, want) {
			t.Errorf("script-src %q does not carry %s", got, want)
		}
	}
}

func TestContentSecurityPolicyAdmitsWhatTheAppLoads(t *testing.T) {
	t.Parallel()

	csp := contentSecurityPolicy("n0nce", "")

	for directive, wants := range map[string][]string{
		"default-src":  {"'self'"},
		"script-src":   {"'self'", "'nonce-n0nce'", "https://www.google.com", "https://www.gstatic.com"},
		"style-src":    {"'self'", "'unsafe-inline'", "https://fonts.googleapis.com", "https://kit.fontawesome.com", "https://ka-p.fontawesome.com"},
		"font-src":     {"'self'", "data:", "https://fonts.gstatic.com", "https://ka-p.fontawesome.com"},
		"img-src":      {"'self'", "data:", "blob:"},
		"connect-src":  {"'self'"},
		"frame-src":    {"'self'", "https://www.google.com"},
		"manifest-src": {"'self'", "blob:"},
	} {
		value := directiveOf(csp, directive)
		if value == "" {
			t.Errorf("policy has no %s directive: %s", directive, csp)
			continue
		}
		for _, want := range wants {
			if !strings.Contains(value, want) {
				t.Errorf("%s = %q, missing %s", directive, value, want)
			}
		}
	}

	for _, absent := range []string{"object-src", "media-src"} {
		if directiveOf(csp, absent) != "" {
			t.Errorf("policy pins %s; it should inherit default-src: %s", absent, csp)
		}
	}

	if strings.Contains(csp, "http://") {
		t.Errorf("policy names a concrete origin, which breaks IP access: %s", csp)
	}

	if strings.Contains(contentSecurityPolicy("", ""), "nonce-") {
		t.Errorf("empty nonce leaked into the policy: %s", contentSecurityPolicy("", ""))
	}
}

func directiveOf(csp, name string) string {
	for _, directive := range strings.Split(csp, ";") {
		directive = strings.TrimSpace(directive)
		if after, ok := strings.CutPrefix(directive, name+" "); ok {
			return after
		}
	}
	return ""
}

func TestIndexTemplateNoncesEveryInlineScript(t *testing.T) {
	t.Parallel()

	const templatePath = "../frontend/public/index.html"

	raw, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", templatePath, err)
	}

	tags := regexp.MustCompile(`<script[^>]*>`).FindAllString(string(raw), -1)
	if len(tags) == 0 {
		t.Fatalf("no script tags found in %s — has it moved?", templatePath)
	}

	inline := 0
	for _, tag := range tags {
		if strings.Contains(tag, "src=") {
			continue
		}
		inline++
		if !strings.Contains(tag, `nonce="[{[ .Nonce ]}]"`) {
			t.Errorf("inline script without the nonce placeholder: %s", tag)
		}
	}

	if inline == 0 {
		t.Error("no inline scripts found; the nonce plumbing may now be dead code")
	}
}

// The frame-ancestors directive follows the server configuration so a deployment
// can name the one platform origin allowed to embed horizon, while a server
// that configures nothing stays unframeable.
func TestFrameAncestorsFollowsConfiguration(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	serve := func(server *settings.Server) string {
		rec := httptest.NewRecorder()
		securityHeaders(server, inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		return rec.Header().Get("Content-Security-Policy")
	}

	if csp := serve(&settings.Server{}); !strings.Contains(csp, "frame-ancestors 'none';") {
		t.Errorf("an unconfigured server must stay unframeable: %s", csp)
	}

	configured := serve(&settings.Server{FrameAncestors: "https://horizon.example"})
	if !strings.Contains(configured, "frame-ancestors https://horizon.example;") {
		t.Errorf("the configured origin is missing from frame-ancestors: %s", configured)
	}
	if strings.Contains(configured, "'none'") {
		t.Errorf("'none' survived alongside the configured origin, which forbids framing entirely: %s", configured)
	}
}

func TestSecurityHeadersComposeWithHandlerPolicy(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Content-Security-Policy", `script-src 'none';`)
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	securityHeaders(&settings.Server{}, inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/raw/x", nil))

	policies := rec.Header().Values("Content-Security-Policy")
	if len(policies) != 2 {
		t.Fatalf("expected the baseline and the handler policy, got %d: %v", len(policies), policies)
	}
	if !strings.Contains(policies[0], "frame-ancestors 'none'") {
		t.Errorf("baseline policy was replaced: %v", policies)
	}
	if !strings.Contains(policies[1], "script-src 'none'") {
		t.Errorf("handler policy missing: %v", policies)
	}
}
