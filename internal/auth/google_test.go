package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// FakeIDToken builds an unsigned JWT with the given claims.
func FakeIDToken(c map[string]any) string {
	h := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`))
	b, _ := json.Marshal(c)
	return h + "." + base64.RawURLEncoding.EncodeToString(b) + ".sig"
}

func TestStartAndFinish(t *testing.T) {
	var gotForm url.Values
	claims := map[string]any{"sub": "g-123", "given_name": "Budi", "aud": "client-1", "iss": "https://accounts.google.com", "exp": time.Now().Add(time.Hour).Unix()}
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": FakeIDToken(claims)})
	}))
	defer tokenSrv.Close()

	g := NewGoogle("client-1", "secret-1", "https://game.test/auth/google/callback")
	g.TokenURL = tokenSrv.URL
	if NewGoogle("", "x", "y") != nil {
		t.Fatal("unconfigured client must be nil")
	}

	redirect, state := g.Start("guest-token")
	u, _ := url.Parse(redirect)
	q := u.Query()
	if q.Get("client_id") != "client-1" || q.Get("state") != state || q.Get("scope") != "openid profile" || q.Get("code_challenge_method") != "S256" {
		t.Fatalf("auth URL = %s", redirect)
	}

	c, flow, err := g.Finish(context.Background(), state, "the-code")
	if err != nil {
		t.Fatal(err)
	}
	if c.Sub != "g-123" || c.GivenName != "Budi" || flow.PID != "guest-token" {
		t.Fatalf("claims %+v flow %+v", c, flow)
	}
	// PKCE: the verifier we sent hashes to the challenge we advertised.
	sum := sha256.Sum256([]byte(gotForm.Get("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != q.Get("code_challenge") || gotForm.Get("client_secret") != "secret-1" {
		t.Fatalf("token request = %v", gotForm)
	}
	if _, _, err := g.Finish(context.Background(), state, "the-code"); !errors.Is(err, ErrUnknownState) {
		t.Fatalf("state must be single-use, got %v", err)
	}
}

func TestBadTokensRejected(t *testing.T) {
	var claims map[string]any
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": FakeIDToken(claims)})
	}))
	defer tokenSrv.Close()
	g := NewGoogle("client-1", "s", "https://game.test/cb")
	g.TokenURL = tokenSrv.URL
	future := time.Now().Add(time.Hour).Unix()
	cases := map[string]map[string]any{
		"wrong audience": {"sub": "x", "aud": "someone-else", "iss": "accounts.google.com", "exp": future},
		"wrong issuer":   {"sub": "x", "aud": "client-1", "iss": "https://evil.example", "exp": future},
		"expired":        {"sub": "x", "aud": "client-1", "iss": "accounts.google.com", "exp": time.Now().Add(-time.Minute).Unix()},
		"no subject":     {"aud": "client-1", "iss": "accounts.google.com", "exp": future},
	}
	for name, c := range cases {
		claims = c
		_, state := g.Start("")
		if _, _, err := g.Finish(context.Background(), state, "code"); !errors.Is(err, ErrBadToken) {
			t.Errorf("%s: want ErrBadToken, got %v", name, err)
		}
	}

	// An expired flow is refused before Google is even asked.
	_, state := g.Start("")
	now := time.Now().Add(FlowTTL + time.Minute)
	g.now = func() time.Time { return now }
	if _, _, err := g.Finish(context.Background(), state, "code"); !errors.Is(err, ErrUnknownState) {
		t.Fatalf("expired flow: %v", err)
	}
}
