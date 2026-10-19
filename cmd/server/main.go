// Command server runs Ular Tangga: the web client, the WebSocket game
// server and the leaderboard API, all from one binary.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/erwinwahyura/snake-ladder/internal/identity"
	"github.com/erwinwahyura/snake-ladder/internal/room"
	"github.com/erwinwahyura/snake-ladder/internal/social"
	"github.com/erwinwahyura/snake-ladder/internal/stats"
	"github.com/erwinwahyura/snake-ladder/web"
)

func main() {
	addr := flag.String("addr", env("ADDR", ":8080"), "listen address")
	dataDir := flag.String("data", env("DATA_DIR", "./data"), "directory for leaderboard.json")
	origins := flag.String("origins", env("ORIGINS", ""), "extra allowed WebSocket origins, comma-separated (e.g. game.erwarx.com)")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(*addr, *dataDir, splitList(*origins)); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func run(addr, dataDir string, origins []string) error {
	store, err := stats.Open(filepath.Join(dataDir, "leaderboard.json"))
	if err != nil {
		return err
	}
	signer, err := identity.LoadOrCreate(filepath.Join(dataDir, "identity.key"))
	if err != nil {
		return err
	}
	friends, err := social.OpenFriends(filepath.Join(dataDir, "friends.json"))
	if err != nil {
		return err
	}
	var hub *room.Hub
	soc := social.NewService(friends, signer.Verify, func(code string) bool { return hub.Listed(code) })
	hub = room.NewHub(room.Config{Recorder: store, Verify: signer.Verify, Social: soc})

	srv := &http.Server{
		Addr:              addr,
		Handler:           routes(app{hub: hub, store: store, signer: signer, social: soc, transfers: identity.NewTransfers()}, origins),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", addr, "data", dataDir)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	hub.Close() // close game sockets; Shutdown does not wait for hijacked conns
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// app is everything the HTTP routes need.
type app struct {
	hub       *room.Hub
	store     *stats.Store
	signer    *identity.Signer
	social    *social.Service
	transfers *identity.Transfers
	claims    *limiter
}

func routes(a app, origins []string) http.Handler {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
	if a.claims == nil {
		a.claims = newLimiter()
	}
	hub, store, signer := a.hub, a.store, a.signer
	mux := http.NewServeMux()
	mux.Handle("GET /ws", hub.Handler(origins))
	mux.Handle("GET /ws/presence", a.social.Handler(origins))
	a.identityRoutes(mux)
	a.socialRoutes(mux)
	mux.HandleFunc("GET /api/leaderboard", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		resp := map[string]any{"players": store.Top(20)}
		// ?pid= adds the caller's own row, even outside the top 20.
		if id, ok := signer.Verify(r.URL.Query().Get("pid")); ok {
			if me, ok := store.Rank(id); ok {
				resp["me"] = me
			}
		}
		writeJSON(w, resp)
	})
	// A guest identity: the browser's signed ID back if it is valid, a new
	// one otherwise. No sign-up; see docs/RESEARCH.md.
	mux.HandleFunc("GET /api/identity", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		pid := r.URL.Query().Get("pid")
		if _, ok := signer.Verify(pid); !ok {
			pid = signer.Issue()
		}
		writeJSON(w, map[string]any{"pid": pid})
	})
	mux.HandleFunc("GET /api/rooms", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, map[string]any{"rooms": hub.Public(20)})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "rooms": hub.Len()})
	})

	static, err := fs.Sub(web.Files, ".")
	if err != nil {
		panic(err)
	}
	files := http.FileServerFS(static)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/vendor/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; connect-src 'self' ws: wss:; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
