package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/erwinwahyura/snake-ladder/internal/auth"
	"github.com/erwinwahyura/snake-ladder/internal/identity"
	"github.com/erwinwahyura/snake-ladder/internal/room"
	"github.com/erwinwahyura/snake-ladder/internal/social"
	"github.com/erwinwahyura/snake-ladder/internal/stats"
)

// fakeGoogle answers the token exchange with an ID token for whichever
// Google account the test says is signing in.
type fakeGoogle struct{ sub, name string }

func (f *fakeGoogle) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := json.Marshal(map[string]any{
			"sub": f.sub, "given_name": f.name, "aud": "client-1",
			"iss": "https://accounts.google.com", "exp": time.Now().Add(time.Hour).Unix(),
		})
		tok := "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".sig"
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": tok})
	}
}

func TestGoogleSignIn(t *testing.T) {
	fg := &fakeGoogle{}
	tokenSrv := httptest.NewServer(fg.handler())
	defer tokenSrv.Close()

	signer, _ := identity.New([]byte(strings.Repeat("s", 32)))
	store, _ := stats.Open("")
	friends, _ := social.OpenFriends("")
	accounts, _ := identity.OpenAccounts("")
	soc := social.NewService(friends, signer.Verify, nil)
	hub := room.NewHub(room.Config{Recorder: store, Verify: signer.Verify, Social: soc})
	defer hub.Close()
	g := auth.NewGoogle("client-1", "secret", "http://game.test/auth/google/callback")
	g.TokenURL = tokenSrv.URL
	srv := httptest.NewServer(routes(app{hub: hub, store: store, signer: signer, social: soc,
		transfers: identity.NewTransfers(), accounts: accounts, google: g}, nil))
	defer srv.Close()

	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	// signIn runs the whole dance as one browser holding guestToken, and
	// returns the identity the page ends up with (or the login_error).
	signIn := func(guestToken string, withCookie bool) (pid, loginErr, name string) {
		t.Helper()
		res, err := noFollow.PostForm(srv.URL+"/auth/google", url.Values{"pid": {guestToken}})
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusSeeOther {
			t.Fatalf("start: %d", res.StatusCode)
		}
		loc, _ := url.Parse(res.Header.Get("Location"))
		if !strings.HasPrefix(loc.String(), auth.GoogleAuthURL) {
			t.Fatalf("not sent to Google: %s", loc)
		}
		state := loc.Query().Get("state")

		req, _ := http.NewRequest("GET", srv.URL+"/auth/google/callback?code=c&state="+url.QueryEscape(state), nil)
		if withCookie {
			for _, c := range res.Cookies() {
				req.AddCookie(c)
			}
		}
		cb, err := noFollow.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		cb.Body.Close()
		back, _ := url.Parse(cb.Header.Get("Location"))
		if e := back.Query().Get("login_error"); e != "" {
			return "", e, ""
		}
		claim, err := http.Post(srv.URL+"/api/identity/claim?code="+back.Query().Get("login"), "", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer claim.Body.Close()
		var m map[string]string
		_ = json.NewDecoder(claim.Body).Decode(&m)
		return m["pid"], "", back.Query().Get("nama")
	}
	idOf := func(tok string) string { id, _ := signer.Verify(tok); return id }

	// Phone: a guest with a won game signs in for the first time. Same
	// identity, now linked; first name offered for the display name.
	phone := signer.Issue()
	_ = store.Record([]stats.Result{{PlayerID: idOf(phone), Name: "Budi", Won: true, Rolls: 20, Points: 10}, {PlayerID: "someone-else", Name: "X", Points: 3}})
	fg.sub, fg.name = "google-budi", "Budi"
	pid, loginErr, name := signIn(phone, true)
	if loginErr != "" || pid != phone || name != "Budi" || !accounts.Linked(idOf(phone)) {
		t.Fatalf("first sign-in: pid=%q err=%q name=%q", pid, loginErr, name)
	}

	// Laptop: a different guest with its own game signs in to the same
	// Google account. It gets the phone's identity, with its game merged in.
	laptop := signer.Issue()
	_ = store.Record([]stats.Result{{PlayerID: idOf(laptop), Name: "Budi", Points: 3}, {PlayerID: "someone-else", Name: "X", Won: true, Points: 10}})
	pid, _, _ = signIn(laptop, true)
	if pid != phone {
		t.Fatal("same Google account must give the same identity on every device")
	}
	if rec, _ := store.Get(idOf(phone)); rec.Games != 2 || rec.Points != 13 {
		t.Fatalf("laptop's guest game should be merged in: %+v", rec)
	}
	if _, ok := signer.Verify(laptop); ok {
		t.Fatal("the merged guest identity is retired")
	}

	// A callback without our cookie (a link from someone else) is refused.
	if _, loginErr, _ = signIn(signer.Issue(), false); loginErr != "state" {
		t.Fatalf("missing cookie: %q", loginErr)
	}

	// Someone already linked to one Google account signs in with another:
	// they get a fresh identity rather than stealing or merging the first.
	fg.sub = "google-other"
	pid, _, _ = signIn(phone, true)
	if pid == phone || idOf(pid) == "" {
		t.Fatalf("second Google account must get its own identity, got %q", pid)
	}

	// /api/identity reports the link; "Ganti kunci" keeps it.
	var ident struct {
		PID           string
		Google        bool
		GoogleEnabled bool
	}
	res, _ := http.Get(srv.URL + "/api/identity?pid=" + phone)
	_ = json.NewDecoder(res.Body).Decode(&ident)
	res.Body.Close()
	if !ident.Google || !ident.GoogleEnabled {
		t.Fatalf("identity = %+v", ident)
	}
	rot, _ := http.Post(srv.URL+"/api/identity/rotate?pid="+phone, "", nil)
	var m map[string]string
	_ = json.NewDecoder(rot.Body).Decode(&m)
	rot.Body.Close()
	fg.sub = "google-budi"
	if pid, _, _ = signIn("", true); pid != m["pid"] {
		t.Fatal("after Ganti kunci, signing in must give the new key")
	}
}

func TestGoogleOffWhenNotConfigured(t *testing.T) {
	signer, _ := identity.New([]byte(strings.Repeat("s", 32)))
	store, _ := stats.Open("")
	friends, _ := social.OpenFriends("")
	soc := social.NewService(friends, signer.Verify, nil)
	hub := room.NewHub(room.Config{Verify: signer.Verify})
	defer hub.Close()
	srv := httptest.NewServer(routes(app{hub: hub, store: store, signer: signer, social: soc, transfers: identity.NewTransfers()}, nil))
	defer srv.Close()
	res, _ := http.PostForm(srv.URL+"/auth/google", url.Values{})
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("sign-in should be off: %d", res.StatusCode)
	}
	res, _ = http.Get(srv.URL + "/api/identity")
	var m map[string]any
	_ = json.NewDecoder(res.Body).Decode(&m)
	if m["googleEnabled"] != false {
		t.Fatalf("identity = %v", m)
	}
}
