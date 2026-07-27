package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Token is the stored half of a session. The bearer token itself is handed to
// the client once and never persisted — only its hash is — so read access to the
// database (a backup, a snapshot, a stray copy under a served root) yields no
// usable sessions.
type Token struct {
	// Hash must stay serializable: storm persists records through a JSON codec
	// and would not read the identifier back into a `json:"-"` field.
	Hash   string `json:"hash" storm:"id"`
	UserID uint   `json:"userID" storm:"index"`

	ExpiresAt time.Time `json:"expiresAt"`

	// CreatedAt is the moment the session began, and is carried across renewals
	// rather than reset by them, so that it can bound the session's total life.
	CreatedAt time.Time `json:"createdAt"`
}

func (t *Token) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}

// TokenStore persists sessions. Every method takes the bearer token exactly as
// the client presents it and hashes it internally, so that no caller can write a
// directly usable token to disk by mistake.
type TokenStore interface {
	Save(token string, t *Token) error
	Get(token string) (*Token, error)
	Delete(token string) error

	// DeleteByUser ends every session belonging to userID except those named in
	// keep. Callers pass the token the request authenticated with to avoid
	// logging someone out of the session they are acting from.
	DeleteByUser(userID uint, keep ...string) error

	DeleteExpired() error
}

// HashToken maps a bearer token to the value stored for it.
//
// A bare SHA-256 suffices where a password hash would not: tokens come from
// GenerateToken with 256 bits of CSPRNG entropy, so there is no dictionary to
// run against them and nothing for a salt or a work factor to buy.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func GenerateToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
