package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/erwinwahyura/snake-ladder/internal/identity"
	"github.com/erwinwahyura/snake-ladder/internal/room"
	"github.com/erwinwahyura/snake-ladder/internal/social"
	"github.com/erwinwahyura/snake-ladder/internal/stats"
)

func TestRoutes(t *testing.T) {
	signer, _ := identity.New([]byte(strings.Repeat("s", 32)))
	one, two := signer.Issue(), signer.Issue()
	idOne, _ := signer.Verify(one)
	idTwo, _ := signer.Verify(two)
	store, _ := stats.Open("")
	_ = store.Record([]stats.Result{
		{PlayerID: idOne, Name: "Budi", Won: true, Rolls: 20, Points: 10},
		{PlayerID: idTwo, Name: "Sari", Rolls: 21, Points: 3},
	})
	friends, _ := social.OpenFriends("")
	var hub *room.Hub
	soc := social.NewService(friends, signer.Verify, func(code string) bool { return hub.Listed(code) })
	hub = room.NewHub(room.Config{Recorder: store, Verify: signer.Verify, Social: soc})
	defer hub.Close()
	srv := httptest.NewServer(routes(app{hub: hub, store: store, signer: signer, social: soc, transfers: identity.NewTransfers()}, nil))
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

	_, body = get("/api/leaderboard?pid=" + two)
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
	if strings.Contains(body, idOne) {
		t.Fatal("player IDs must not leak in the leaderboard")
	}

	// Forged or unknown IDs get no row, and /api/identity replaces them.
	if _, body = get("/api/leaderboard?pid=" + idTwo + ".forged"); strings.Contains(body, `"me"`) {
		t.Fatal("forged pid must not resolve")
	}
	var ident struct{ PID string }
	_, body = get("/api/identity?pid=" + one)
	_ = json.Unmarshal([]byte(body), &ident)
	if ident.PID != one {
		t.Fatalf("valid pid should be kept, got %q", ident.PID)
	}
	_, body = get("/api/identity?pid=made-up-id")
	_ = json.Unmarshal([]byte(body), &ident)
	if _, ok := signer.Verify(ident.PID); !ok || ident.PID == "made-up-id" {
		t.Fatalf("invalid pid should be replaced with a signed one, got %q", ident.PID)
	}
	if _, body = get("/api/rooms"); !strings.Contains(body, `"rooms":[]`) {
		t.Fatalf("rooms = %s", body)
	}

	post := func(path string) (int, map[string]any) {
		t.Helper()
		res, err := http.Post(srv.URL+path, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var m map[string]any
		_ = json.NewDecoder(res.Body).Decode(&m)
		return res.StatusCode, m
	}

	// Transfer: a one-time code carries the identity to another device.
	st, m := post("/api/identity/transfer?pid=" + one)
	code, _ := m["code"].(string)
	if st != 200 || len(code) != 6 {
		t.Fatalf("transfer = %d %v", st, m)
	}
	if st, m = post("/api/identity/claim?code=" + strings.ToLower(code)); st != 200 || m["pid"] != one {
		t.Fatalf("claim = %d %v", st, m)
	}
	if st, _ = post("/api/identity/claim?code=" + code); st != 404 {
		t.Fatalf("second claim = %d", st)
	}
	if st, _ = post("/api/identity/transfer?pid=forged.token"); st != 401 {
		t.Fatalf("transfer with a bad pid = %d", st)
	}

	// Friends: playing together makes friends; the list has statuses.
	friends.Met(map[string]string{idOne: "Budi", idTwo: "Sari"})
	_, body = get("/api/friends?pid=" + one)
	if !strings.Contains(body, `"name":"Sari"`) || strings.Contains(body, `"name":"Budi"`) {
		t.Fatalf("friends = %s", body)
	}

	// Rotate: new key keeps the record and friends; the old key is dead.
	st, m = post("/api/identity/rotate?pid=" + one)
	fresh, _ := m["pid"].(string)
	newID, ok := signer.Verify(fresh)
	if st != 200 || !ok {
		t.Fatalf("rotate = %d %v", st, m)
	}
	if _, ok := signer.Verify(one); ok {
		t.Fatal("old key must stop working")
	}
	if rec, ok := store.Get(newID); !ok || rec.Points != 10 {
		t.Fatalf("record not moved: %+v", rec)
	}
	if !friends.Has(idTwo, newID) {
		t.Fatal("Sari's list should follow the new ID")
	}

	// Reports need a real player and a real table.
	if st, _ = post("/api/rooms/ZZZZ/report?pid=" + two); st != 404 {
		t.Fatalf("report unknown table = %d", st)
	}
	if st, _ = post("/api/rooms/ZZZZ/report?pid=nope"); st != 401 {
		t.Fatalf("report without identity = %d", st)
	}

	if _, body = get("/api/board"); !strings.Contains(body, `"ladders":[{"from":4,"to":14}`) {
		t.Fatalf("board = %s", body)
	}
	if _, body = get("/healthz"); !strings.Contains(body, `"playing":0`) {
		t.Fatalf("healthz = %s", body)
	}
	for _, p := range []string{"/sw.js", "/offline.html"} {
		if res, _ := get(p); res.StatusCode != 200 || res.Header.Get("Cache-Control") != "no-cache" {
			t.Fatalf("%s: %d %q", p, res.StatusCode, res.Header.Get("Cache-Control"))
		}
	}

	if res, _ := get("/healthz"); res.StatusCode != 200 {
		t.Fatal("healthz")
	}
}
