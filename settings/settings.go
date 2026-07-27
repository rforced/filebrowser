package settings

import (
	"crypto/rand"
	"fmt"
	"io/fs"
	"log"
	"net"
	"strings"
	"time"

	"github.com/rforced/filebrowser/v2/rules"
)

const DefaultUsersHomeBasePath = "/users"
const DefaultLogoutPage = "/login"
const DefaultMinimumPasswordLength = 12
const DefaultFileMode = 0640
const DefaultDirMode = 0750

// DefaultSessionMaxLifetime caps how far renewals may carry a session past the
// login that started it.
const DefaultSessionMaxLifetime = 24 * time.Hour

// AuthMethod describes an authentication method.
type AuthMethod string

// Settings contain the main settings of the application.
type Settings struct {
	Key                   []byte              `json:"key"`
	Signup                bool                `json:"signup"`
	HideLoginButton       bool                `json:"hideLoginButton"`
	CreateUserDir         bool                `json:"createUserDir"`
	UserHomeBasePath      string              `json:"userHomeBasePath"`
	Defaults              UserDefaults        `json:"defaults"`
	AuthMethod            AuthMethod          `json:"authMethod"`
	LogoutPage            string              `json:"logoutPage"`
	Branding              Branding            `json:"branding"`
	Tus                   Tus                 `json:"tus"`
	Commands              map[string][]string `json:"commands"`
	Shell                 []string            `json:"shell"`
	Rules                 []rules.Rule        `json:"rules"`
	MinimumPasswordLength uint                `json:"minimumPasswordLength"`
	FileMode              fs.FileMode         `json:"fileMode"`
	DirMode               fs.FileMode         `json:"dirMode"`
	HideDotfiles          bool                `json:"hideDotfiles"`
}

// GetRules implements rules.Provider.
func (s *Settings) GetRules() []rules.Rule {
	return s.Rules
}

// Server specific settings.
type Server struct {
	Root                  string `json:"root"`
	BaseURL               string `json:"baseURL"`
	Socket                string `json:"socket"`
	TLSKey                string `json:"tlsKey"`
	TLSCert               string `json:"tlsCert"`
	Port                  string `json:"port"`
	Address               string `json:"address"`
	Log                   string `json:"log"`
	EnableThumbnails      bool   `json:"enableThumbnails"`
	ResizePreview         bool   `json:"resizePreview"`
	EnableExec            bool   `json:"enableExec"`
	TypeDetectionByHeader bool   `json:"typeDetectionByHeader"`
	ImageResolutionCal    bool   `json:"imageResolutionCalculation"`
	AuthHook              string `json:"authHook"`
	TokenExpirationTime   string `json:"tokenExpirationTime"`
	SessionMaxLifetime    string `json:"sessionMaxLifetime"`
	Domain                string `json:"domain"`
	TeamID                string `json:"teamId"`
	FilesystemID          string `json:"filesystemId"`

	// TrustedProxies lists, as IP addresses or CIDR blocks, the reverse proxies
	// allowed to tell us who the client is. Requests arriving from anywhere else
	// have their X-Forwarded-For and X-Real-Ip headers ignored, because a client
	// that can forge the address we rate-limit on has no rate limit at all.
	TrustedProxies []string `json:"trustedProxies"`

	// TrustedProxyNets is TrustedProxies in the form the request path needs. It
	// is parsed once by ParseTrustedProxies at startup and never persisted.
	TrustedProxyNets []*net.IPNet `json:"-"`

	// CaseInsensitiveFs is detected from Root at startup rather than
	// configured, and tells the rule checker to match paths case-insensitively.
	// It is never persisted.
	CaseInsensitiveFs bool `json:"-"`
}

// Clean cleans any variables that might need cleaning.
func (s *Server) Clean() {
	s.BaseURL = strings.TrimSuffix(s.BaseURL, "/")
}

func (s *Server) GetTokenExpirationTime(fallback time.Duration) time.Duration {
	if s.TokenExpirationTime == "" {
		return fallback
	}

	duration, err := time.ParseDuration(s.TokenExpirationTime)
	if err != nil {
		log.Printf("[WARN] Failed to parse tokenExpirationTime: %v", err)
		return fallback
	}
	return duration
}

func (s *Server) GetSessionMaxLifetime(fallback time.Duration) time.Duration {
	if s.SessionMaxLifetime == "" {
		return fallback
	}

	duration, err := time.ParseDuration(s.SessionMaxLifetime)
	if err != nil {
		log.Printf("[WARN] Failed to parse sessionMaxLifetime: %v", err)
		return fallback
	}
	return duration
}

// ParseTrustedProxies resolves TrustedProxies into TrustedProxyNets. Entries may
// be CIDR blocks or bare addresses; a bare address is treated as a single host.
//
// It reports an error rather than skipping a malformed entry: silently dropping
// one would leave the server believing forwarding headers from fewer proxies
// than the operator listed, which fails open on the client address every rate
// limit is keyed to.
func (s *Server) ParseTrustedProxies() error {
	nets := make([]*net.IPNet, 0, len(s.TrustedProxies))

	for _, entry := range s.TrustedProxies {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		if _, block, err := net.ParseCIDR(entry); err == nil {
			nets = append(nets, block)
			continue
		}

		ip := net.ParseIP(entry)
		if ip == nil {
			return fmt.Errorf("trusted proxy %q is neither an IP address nor a CIDR block", entry)
		}

		bits := 8 * net.IPv6len
		if ip.To4() != nil {
			bits = 8 * net.IPv4len
		}
		nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}

	s.TrustedProxyNets = nets
	return nil
}

// GenerateKey generates a key of 512 bits.
func GenerateKey() ([]byte, error) {
	b := make([]byte, 64)
	_, err := rand.Read(b)
	// Note that err == nil only if we read len(b) bytes.
	if err != nil {
		return nil, err
	}

	return b, nil
}
