package main

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// identityRoutes: moving a guest identity between devices and retiring it.
func (a app) identityRoutes(mux *http.ServeMux) {
	// A short one-time code to carry your record to another device.
	mux.HandleFunc("POST /api/identity/transfer", func(w http.ResponseWriter, r *http.Request) {
		tok := r.URL.Query().Get("pid")
		if _, ok := a.signer.Verify(tok); !ok {
			http.Error(w, "unknown player", http.StatusUnauthorized)
			return
		}
		code, exp := a.transfers.Create(tok)
		writeJSON(w, map[string]any{"code": code, "expires": exp.UnixMilli()})
	})
	// The other device trades the code for the identity.
	mux.HandleFunc("POST /api/identity/claim", func(w http.ResponseWriter, r *http.Request) {
		if !a.claims.allow(clientIP(r)) {
			http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
			return
		}
		code := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("code")))
		tok, ok := a.transfers.Claim(code)
		if _, valid := a.signer.Verify(tok); !ok || !valid {
			http.Error(w, "code not found or expired", http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"pid": tok})
	})
	// "Ganti kunci": a fresh ID takes over the record and friends, and the
	// old one stops working everywhere (say, after sharing it by mistake).
	mux.HandleFunc("POST /api/identity/rotate", func(w http.ResponseWriter, r *http.Request) {
		oldID, ok := a.signer.Verify(r.URL.Query().Get("pid"))
		if !ok {
			http.Error(w, "unknown player", http.StatusUnauthorized)
			return
		}
		tok := a.signer.Issue()
		newID, _ := a.signer.Verify(tok)
		if err := a.store.Move(oldID, newID); err != nil {
			http.Error(w, "could not move record", http.StatusInternalServerError)
			return
		}
		if err := a.social.Friends.Move(oldID, newID); err != nil {
			http.Error(w, "could not move friends", http.StatusInternalServerError)
			return
		}
		if err := a.signer.Revoke(oldID); err != nil {
			http.Error(w, "could not retire old key", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"pid": tok})
	})
}

// socialRoutes: Teman main and reporting tables.
func (a app) socialRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/friends", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		id, ok := a.signer.Verify(r.URL.Query().Get("pid"))
		if !ok {
			writeJSON(w, map[string]any{"friends": []any{}})
			return
		}
		writeJSON(w, map[string]any{"friends": a.social.List(id)})
	})
	mux.HandleFunc("POST /api/friends/remove", func(w http.ResponseWriter, r *http.Request) {
		id, ok := a.signer.Verify(r.URL.Query().Get("pid"))
		if !ok {
			http.Error(w, "unknown player", http.StatusUnauthorized)
			return
		}
		if err := a.social.Friends.Remove(id, r.URL.Query().Get("id")); err != nil {
			http.Error(w, "could not remove", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/rooms/{code}/report", func(w http.ResponseWriter, r *http.Request) {
		id, ok := a.signer.Verify(r.URL.Query().Get("pid"))
		if !ok {
			http.Error(w, "unknown player", http.StatusUnauthorized)
			return
		}
		if !a.hub.Report(r.PathValue("code"), id) {
			http.Error(w, "table not found", http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
}

// limiter allows a few attempts per client per window, for endpoints that
// take a guessable secret.
type limiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newLimiter() *limiter { return &limiter{hits: map[string][]time.Time{}} }

const (
	claimWindow = 10 * time.Minute
	claimMax    = 20
)

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	recent := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < claimWindow {
			recent = append(recent, t)
		}
	}
	if len(recent) >= claimMax {
		l.hits[key] = recent
		return false
	}
	l.hits[key] = append(recent, now)
	return true
}

// clientIP trusts X-Forwarded-For only from a local proxy (Caddy on the
// same box); otherwise anyone could pick their own rate-limit bucket.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			return strings.TrimSpace(first)
		}
	}
	return host
}
