package fbhttp

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rforced/filebrowser/v2/settings"
)

func trustedNets(t *testing.T, entries ...string) []*net.IPNet {
	t.Helper()
	s := &settings.Server{TrustedProxies: entries}
	if err := s.ParseTrustedProxies(); err != nil {
		t.Fatalf("ParseTrustedProxies() error: %v", err)
	}
	return s.TrustedProxyNets
}

func TestClientIP(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		trusted    []string
		remoteAddr string
		forwarded  string
		realIP     string
		want       string
	}{
		"no trusted proxies ignores forwarding headers": {
			remoteAddr: "203.0.113.9:1234",
			forwarded:  "198.51.100.7",
			realIP:     "198.51.100.8",
			want:       "203.0.113.9",
		},
		"untrusted peer ignores forwarding headers": {
			trusted:    []string{"10.0.0.0/8"},
			remoteAddr: "203.0.113.9:1234",
			forwarded:  "198.51.100.7",
			want:       "203.0.113.9",
		},
		"trusted peer takes the last untrusted hop": {
			trusted:    []string{"10.0.0.0/8"},
			remoteAddr: "10.0.0.2:1234",
			forwarded:  "198.51.100.7, 10.0.0.9",
			want:       "198.51.100.7",
		},
		"client-appended hops to the left are not believed": {
			trusted:    []string{"10.0.0.0/8"},
			remoteAddr: "10.0.0.2:1234",
			forwarded:  "1.1.1.1, 198.51.100.7, 10.0.0.9",
			want:       "198.51.100.7",
		},
		"malformed hop stops the walk": {
			trusted:    []string{"10.0.0.0/8"},
			remoteAddr: "10.0.0.2:1234",
			forwarded:  "198.51.100.7, not-an-ip, 10.0.0.9",
			want:       "10.0.0.2",
		},
		"falls back to X-Real-Ip when the whole chain is ours": {
			trusted:    []string{"10.0.0.0/8"},
			remoteAddr: "10.0.0.2:1234",
			forwarded:  "10.0.0.8, 10.0.0.9",
			realIP:     "198.51.100.7",
			want:       "198.51.100.7",
		},
		"falls back to the peer when nothing else is usable": {
			trusted:    []string{"10.0.0.0/8"},
			remoteAddr: "10.0.0.2:1234",
			want:       "10.0.0.2",
		},
		"single trusted host is matched exactly": {
			trusted:    []string{"192.0.2.1"},
			remoteAddr: "192.0.2.1:1234",
			forwarded:  "198.51.100.7",
			want:       "198.51.100.7",
		},
		"neighbour of a trusted host is not trusted": {
			trusted:    []string{"192.0.2.1"},
			remoteAddr: "192.0.2.2:1234",
			forwarded:  "198.51.100.7",
			want:       "192.0.2.2",
		},
		"unix socket peer is trusted when loopback is": {
			trusted:    []string{"127.0.0.0/8"},
			remoteAddr: "@",
			forwarded:  "198.51.100.7",
			want:       "198.51.100.7",
		},
		"unix socket peer is not trusted otherwise": {
			trusted:    []string{"10.0.0.0/8"},
			remoteAddr: "@",
			forwarded:  "198.51.100.7",
			want:       "@",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remoteAddr
			if tc.forwarded != "" {
				r.Header.Set("X-Forwarded-For", tc.forwarded)
			}
			if tc.realIP != "" {
				r.Header.Set("X-Real-Ip", tc.realIP)
			}

			if got := clientIP(r, trustedNets(t, tc.trusted...)); got != tc.want {
				t.Errorf("clientIP() = %q, want %q", got, tc.want)
			}
		})
	}
}

// A rate limiter is only a limit if the attacker cannot choose its key. Before
// trusted-proxy validation, varying X-Forwarded-For gave every request a fresh
// bucket and removed the brake on login and share-password guessing entirely.
func TestClientIPIgnoresSpoofedForwardedForFromUntrustedPeer(t *testing.T) {
	t.Parallel()

	seen := map[string]struct{}{}
	for i := range 30 {
		r := httptest.NewRequest(http.MethodPost, "/api/login", nil)
		r.RemoteAddr = "203.0.113.9:1234"
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i))
		seen[clientIP(r, nil)] = struct{}{}
	}

	if len(seen) != 1 {
		t.Errorf("spoofed headers produced %d distinct rate-limit keys, want 1", len(seen))
	}
}

func TestParseTrustedProxiesRejectsMalformedEntry(t *testing.T) {
	t.Parallel()

	s := &settings.Server{TrustedProxies: []string{"10.0.0.0/8", "not-an-ip"}}
	if err := s.ParseTrustedProxies(); err == nil {
		t.Fatal("ParseTrustedProxies() = nil, want an error for a malformed entry")
	}
}
