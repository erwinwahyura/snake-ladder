// Package identity issues signed guest player IDs.
//
// There are no accounts: the first visit gets a random ID signed by the
// server, which the browser keeps. The signature means nobody can invent
// an ID (or guess someone else's) to post results under it. Moving to a
// new device is done with a private link that carries the same signed ID.
//
// Format: "<id>.<sig>" where id is 16 base32 characters and sig is the
// first 16 bytes of HMAC-SHA256(secret, id), base64url without padding.
package identity

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Signer signs and checks player IDs with a server secret, and keeps the
// list of IDs their owners have retired ("Ganti kunci").
type Signer struct {
	secret []byte

	mu          sync.Mutex
	revoked     map[string]bool
	revokedPath string // empty: in memory only
}

// New returns a signer for a given secret (at least 32 bytes).
func New(secret []byte) (*Signer, error) {
	if len(secret) < 32 {
		return nil, errors.New("identity: secret must be at least 32 bytes")
	}
	return &Signer{secret: append([]byte(nil), secret...), revoked: map[string]bool{}}, nil
}

// LoadOrCreate reads the secret from path, creating a random one the first
// time. Keep this file: losing it signs everyone out of their records.
func LoadOrCreate(path string) (*Signer, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		b = make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, b, 0o600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	sg, err := New(b)
	if err != nil {
		return nil, err
	}
	// Retired IDs live next to the key, one per line.
	sg.revokedPath = path + ".revoked"
	if data, err := os.ReadFile(sg.revokedPath); err == nil {
		for _, id := range strings.Fields(string(data)) {
			sg.revoked[id] = true
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return sg, nil
}

// Revoke retires an ID: its token stops verifying, for good.
func (s *Signer) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.revoked[id] {
		return nil
	}
	s.revoked[id] = true
	if s.revokedPath == "" {
		return nil
	}
	f, err := os.OpenFile(s.revokedPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(id + "\n"); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Issue makes a new signed player ID.
func (s *Signer) Issue() string {
	id := strings.ToLower(rand.Text()[:16])
	return id + "." + s.sig(id)
}

// TokenFor signs an existing ID, for handing a signed-in player their
// account's identity on a new device. Never call it with an ID taken from
// a request: only with one the server already trusts.
func (s *Signer) TokenFor(id string) string { return id + "." + s.sig(id) }

// Verify checks a signed player ID and returns its stable ID part, which
// is what the leaderboard stores.
func (s *Signer) Verify(token string) (string, bool) {
	id, sig, ok := strings.Cut(token, ".")
	if !ok || len(id) != 16 || len(sig) > 64 {
		return "", false
	}
	if !hmac.Equal([]byte(sig), []byte(s.sig(id))) {
		return "", false
	}
	s.mu.Lock()
	dead := s.revoked[id]
	s.mu.Unlock()
	if dead {
		return "", false
	}
	return id, true
}

func (s *Signer) sig(id string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(id))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:16])
}
