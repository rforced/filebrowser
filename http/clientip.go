package fbhttp

import (
	"net"
	"net/http"
	"strings"
)

// clientIP returns the address a request should be attributed to, for rate
// limiting and for the security log.
//
// X-Forwarded-For and X-Real-Ip are written by whoever is speaking to us, so
// they are only believed when the immediate peer is one of the configured
// trusted proxies. Taking them unconditionally lets any client mint a fresh
// rate-limit bucket per request just by varying the header, which removes the
// only brute-force brake on both login and share passwords.
func clientIP(r *http.Request, trusted []*net.IPNet) string {
	peer := peerIP(r)
	if !peerIsTrusted(peer, trusted) {
		return peer
	}

	// Read X-Forwarded-For right to left. Everything appended by our own proxies
	// is trusted, so the first address that is not one of them is the closest to
	// the real client that we can actually vouch for; anything further left was
	// supplied by the client and may be invented.
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		hops := strings.Split(forwarded, ",")
		for i := len(hops) - 1; i >= 0; i-- {
			ip := net.ParseIP(strings.TrimSpace(hops[i]))
			if ip == nil {
				// A malformed hop means the chain is not the one our proxies
				// wrote, so nothing to its left can be attributed to them.
				break
			}
			if !containsIP(trusted, ip) {
				return ip.String()
			}
		}
	}

	if header := strings.TrimSpace(r.Header.Get("X-Real-Ip")); header != "" {
		if ip := net.ParseIP(header); ip != nil {
			return ip.String()
		}
	}

	return peer
}

func peerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// A unix-socket listener reports a name rather than host:port.
		return r.RemoteAddr
	}
	return host
}

// peerIsTrusted reports whether the immediate peer is a configured proxy.
//
// A unix-socket peer has no address to match, but it is necessarily on this
// host, so it is trusted exactly when loopback is — which is what an operator
// fronting filebrowser with a local proxy over a socket has configured.
func peerIsTrusted(peer string, trusted []*net.IPNet) bool {
	ip := net.ParseIP(peer)
	if ip == nil {
		ip = net.IPv4(127, 0, 0, 1)
	}
	return containsIP(trusted, ip)
}

func containsIP(nets []*net.IPNet, ip net.IP) bool {
	for _, block := range nets {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}
