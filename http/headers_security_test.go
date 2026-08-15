package fbhttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeadersOnEveryRoute(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := securityHeaders(inner)

	paths := []string{
		"/",              // SPA index — previously uncovered
		"/files/nested",  // SPA route — previously uncovered
		"/share/abc",     // SPA route — previously uncovered
		"/health",        // matched route
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
			if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
				t.Errorf("Referrer-Policy on %s = %q, want no-referrer", path, got)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options on %s = %q, want nosniff", path, got)
			}
		})
	}
}

func TestSecurityHeadersComposeWithHandlerPolicy(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Content-Security-Policy", `script-src 'none';`)
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	securityHeaders(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/raw/x", nil))

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
