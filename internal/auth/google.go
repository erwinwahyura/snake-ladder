// Package auth signs players in with Google, using the OAuth 2.0
// authorization-code flow with PKCE, entirely on the server. No Google
// script runs in the page, so the strict content security policy stays.
//
// Only the "openid profile" scopes are asked for. What we keep is Google's
// stable subject ID ("sub"); the first name is used once, to suggest a
// display name, and never stored.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Google endpoints; overridden in tests.
const (
	GoogleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	GoogleTokenURL = "https://oauth2.googleapis.com/token"
)

// FlowTTL is how long someone has to finish signing in at Google.
const FlowTTL = 10 * time.Minute

// Google holds the OAuth client settings.
type Google struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string // e.g. https://ulartangga.erwarx.com/auth/google/callback
	AuthURL      string
	TokenURL     string
	HTTP         *http.Client

	mu    sync.Mutex
	flows map[string]Flow // state → flow
	now   func() time.Time
}

// Flow is one sign-in in progress.
type Flow struct {
	Verifier string    // PKCE code verifier
	PID      string    // the guest identity the browser had, if any
	Expires  time.Time //
}

// Claims are the parts of Google's ID token we use.
type Claims struct {
	Sub       string `json:"sub"`
	GivenName string `json:"given_name"`
	Aud       string `json:"aud"`
	Iss       string `json:"iss"`
	Exp       int64  `json:"exp"`
}

// NewGoogle returns a client, or nil when it isn't configured (sign-in is
// then simply not offered).
func NewGoogle(clientID, secret, redirectURL string) *Google {
	if clientID == "" || secret == "" || redirectURL == "" {
		return nil
	}
	return &Google{
		ClientID: clientID, ClientSecret: secret, RedirectURL: redirectURL,
		AuthURL: GoogleAuthURL, TokenURL: GoogleTokenURL,
		HTTP:  &http.Client{Timeout: 10 * time.Second},
		flows: map[string]Flow{},
		now:   time.Now,
	}
}

// Start begins a sign-in: it returns the Google URL to send the browser
// to, and the state value the browser must bring back (we also put it in
// a cookie, so a callback link sent by someone else can't log you in to
// their account).
func (g *Google) Start(pid string) (redirect, state string) {
	state = randomString(24)
	verifier := randomString(48)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	g.mu.Lock()
	g.sweep()
	g.flows[state] = Flow{Verifier: verifier, PID: pid, Expires: g.now().Add(FlowTTL)}
	g.mu.Unlock()

	q := url.Values{
		"client_id":             {g.ClientID},
		"redirect_uri":          {g.RedirectURL},
		"response_type":         {"code"},
		"scope":                 {"openid profile"},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"prompt":                {"select_account"},
	}
	return g.AuthURL + "?" + q.Encode(), state
}

// Errors from Finish.
var (
	ErrUnknownState = errors.New("sign-in expired or not started here")
	ErrBadToken     = errors.New("google returned an unusable token")
)

// Finish trades the code from the callback for the player's Google
// identity. The state is used up either way.
func (g *Google) Finish(ctx context.Context, state, code string) (Claims, Flow, error) {
	g.mu.Lock()
	g.sweep()
	flow, ok := g.flows[state]
	delete(g.flows, state)
	g.mu.Unlock()
	if !ok || code == "" {
		return Claims{}, Flow{}, ErrUnknownState
	}

	form := url.Values{
		"code":          {code},
		"client_id":     {g.ClientID},
		"client_secret": {g.ClientSecret},
		"redirect_uri":  {g.RedirectURL},
		"grant_type":    {"authorization_code"},
		"code_verifier": {flow.Verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Claims{}, flow, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := g.HTTP.Do(req)
	if err != nil {
		return Claims{}, flow, fmt.Errorf("token exchange: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if res.StatusCode != http.StatusOK {
		return Claims{}, flow, fmt.Errorf("token exchange: %s", res.Status)
	}
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.IDToken == "" {
		return Claims{}, flow, ErrBadToken
	}
	c, err := g.claims(tok.IDToken)
	return c, flow, err
}

// claims reads the ID token. It came straight from Google's token
// endpoint over TLS in exchange for our client secret, so OpenID Connect
// lets us skip the signature check (Core 1.0, §3.1.3.7); we still check
// who it is for, who issued it and that it is current.
func (g *Google) claims(idToken string) (Claims, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return Claims{}, ErrBadToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrBadToken
	}
	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return Claims{}, ErrBadToken
	}
	switch {
	case c.Sub == "":
		return Claims{}, ErrBadToken
	case c.Aud != g.ClientID:
		return Claims{}, fmt.Errorf("%w: wrong audience", ErrBadToken)
	case c.Iss != "https://accounts.google.com" && c.Iss != "accounts.google.com":
		return Claims{}, fmt.Errorf("%w: wrong issuer", ErrBadToken)
	case g.now().Unix() > c.Exp:
		return Claims{}, fmt.Errorf("%w: expired", ErrBadToken)
	}
	return c, nil
}

func (g *Google) sweep() {
	now := g.now()
	for s, f := range g.flows {
		if now.After(f.Expires) {
			delete(g.flows, s)
		}
	}
}

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
