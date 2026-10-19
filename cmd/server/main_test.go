package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/erwinwahyura/snake-ladder/internal/room"
	"github.com/erwinwahyura/snake-ladder/internal/stats"
)

func TestRoutes(t *testing.T) {
	store, _ := stats.Open("")
	_ = store.Record([]stats.Result{
		{PlayerID: "player-one", Name: "Budi", Won: true, Rolls: 20},
		{PlayerID: "player-two", Name: "Sari", Rolls: 21},
	})
	hub := room.NewHub(room.Config{Recorder: store})
	defer hub.Close()
	srv := httptest.NewServer(routes(hub, store, nil))
	defer srv.Close()

	get := func(path string) (*http.Response, string) {
		t.Helper()
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		return res, string(b)
	}

	res, body := get("/")
	if res.StatusCode != 200 || !strings.Contains(body, "Ular Tangga") {
		t.Fatalf("index: %d", res.StatusCode)
	}
	if csp := res.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
		t.Fatalf("CSP = %q", csp)
	}

	res, _ = get("/manifest.webmanifest")
	if ct := res.Header.Get("Content-Type"); ct != "application/manifest+json" {
		t.Fatalf("manifest content type = %q", ct)
	}
	if res, _ = get("/vendor/three/three.module.min.js"); !strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
		t.Fatal("vendor files should be cached long")
	}
	for _, p := range []string{"/icons/icon-192.png", "/js/main.js", "/style.css"} {
		if res, _ := get(p); res.StatusCode != 200 {
			t.Fatalf("%s: %d", p, res.StatusCode)
		}
	}

	_, body = get("/api/leaderboard?pid=player-two")
	var lb struct {
		Players []stats.Entry `json:"players"`
		Me      *stats.Entry  `json:"me"`
	}
	if err := json.Unmarshal([]byte(body), &lb); err != nil {
		t.Fatal(err)
	}
	if len(lb.Players) != 2 || lb.Players[0].Name != "Budi" || lb.Me == nil || lb.Me.Rank != 2 {
		t.Fatalf("leaderboard = %s", body)
	}
	if strings.Contains(body, "player-one") {
		t.Fatal("player IDs must not leak in the leaderboard")
	}

	if res, _ := get("/healthz"); res.StatusCode != 200 {
		t.Fatal("healthz")
	}
}
