package main

import (
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

const oauthCookie = "ut_oauth"

// authRoutes: optional "Masuk dengan Google". Playing never needs it; it
// attaches a Google account to the player's guest identity so their
// record follows them to any device.
func (a app) authRoutes(mux *http.ServeMux) {
	// The page posts its current guest identity here (a form post, so the
	// token stays out of URLs and logs), and we send the browser to Google.
	mux.HandleFunc("POST /auth/google", func(w http.ResponseWriter, r *http.Request) {
		if a.google == nil {
			http.NotFound(w, r)
			return
		}
		redirect, state := a.google.Start(r.PostFormValue("pid"))
		http.SetCookie(w, &http.Cookie{
			Name: oauthCookie, Value: state, Path: "/auth/google",
			MaxAge: int((10 * time.Minute).Seconds()), HttpOnly: true, Secure: a.secureCookies,
			SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, redirect, http.StatusSeeOther)
	})

	mux.HandleFunc("GET /auth/google/callback", func(w http.ResponseWriter, r *http.Request) {
		if a.google == nil {
			http.NotFound(w, r)
			return
		}
		back := func(q url.Values) { http.Redirect(w, r, "/?"+q.Encode(), http.StatusSeeOther) }
		http.SetCookie(w, &http.Cookie{Name: oauthCookie, Path: "/auth/google", MaxAge: -1, HttpOnly: true, Secure: a.secureCookies})

		q := r.URL.Query()
		if q.Get("error") != "" { // the player pressed cancel at Google
			back(url.Values{"login_error": {"cancelled"}})
			return
		}
		state := q.Get("state")
		if c, err := r.Cookie(oauthCookie); err != nil || c.Value != state || state == "" {
			// Not started in this browser: maybe a link from someone else.
			back(url.Values{"login_error": {"state"}})
			return
		}
		claims, flow, err := a.google.Finish(r.Context(), state, q.Get("code"))
		if err != nil {
			slog.Warn("google sign-in failed", "err", err)
			back(url.Values{"login_error": {"failed"}})
			return
		}
		token, err := a.signIn(claims.Sub, flow.PID)
		if err != nil {
			slog.Error("google sign-in: link", "err", err)
			back(url.Values{"login_error": {"failed"}})
			return
		}
		// Hand the identity over the same way as "Pindah ke HP lain": a
		// one-time code the page claims, so no token lands in the URL.
		code, _ := a.transfers.Create(token)
		back(url.Values{"login": {code}, "nama": {claims.GivenName}})
	})
}

// signIn decides which identity a Google account gets:
//   - already linked: that player, with this browser's guest record (if it
//     isn't someone else's account) merged in;
//   - new: this browser's guest identity becomes the account, or a fresh
//     one if the guest is already linked to a different Google account.
func (a app) signIn(sub, guestToken string) (string, error) {
	guestID, guestOK := a.signer.Verify(guestToken)
	if id, ok := a.accounts.Google(sub); ok {
		if guestOK && guestID != id && !a.accounts.Linked(guestID) {
			if err := a.store.Move(guestID, id); err != nil {
				return "", err
			}
			if err := a.social.Friends.Move(guestID, id); err != nil {
				return "", err
			}
			if err := a.signer.Revoke(guestID); err != nil {
				return "", err
			}
		}
		return a.signer.TokenFor(id), nil
	}
	if guestOK && !a.accounts.Linked(guestID) {
		return guestToken, a.accounts.LinkGoogle(sub, guestID)
	}
	fresh := a.signer.Issue()
	id, _ := a.signer.Verify(fresh)
	return fresh, a.accounts.LinkGoogle(sub, id)
}
