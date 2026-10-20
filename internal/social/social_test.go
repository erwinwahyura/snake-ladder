package social

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestFriendsStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "friends.json")
	f, _ := OpenFriends(path)
	if err := f.Met(map[string]string{"budi": "Budi", "sari": "Sari", "joko": "Joko"}); err != nil {
		t.Fatal(err)
	}
	if got := f.List("budi"); len(got) != 2 {
		t.Fatalf("budi's friends = %+v", got)
	}
	if !f.Has("sari", "budi") || f.Has("budi", "budi") {
		t.Fatal("friendship is mutual and never with yourself")
	}
	_ = f.Remove("budi", "joko")
	if f.Has("budi", "joko") || !f.Has("joko", "budi") {
		t.Fatal("remove is one-way")
	}
	_ = f.Move("sari", "sari2")
	if !f.Has("budi", "sari2") || f.Has("budi", "sari") || len(f.List("sari2")) != 2 {
		t.Fatal("move renames the player everywhere")
	}
	f2, _ := OpenFriends(path)
	if !f2.Has("budi", "sari2") {
		t.Fatal("friends must survive a restart")
	}

	// Cap: the oldest drop off.
	now := time.Now()
	f.now = func() time.Time { now = now.Add(time.Second); return now }
	for i := 0; i < MaxFriends+5; i++ {
		_ = f.Met(map[string]string{"many": "M", string(rune('A'+i%26)) + strings.Repeat("x", i/26): "x"})
	}
	if n := len(f.List("many")); n != MaxFriends {
		t.Fatalf("cap = %d", n)
	}
}

func verifyAny(tok string) (string, bool) { return tok, tok != "" }

func TestPresenceStatusAndInvites(t *testing.T) {
	f, _ := OpenFriends("")
	listed := map[string]bool{"PUBL": true}
	s := NewService(f, verifyAny, func(code string) bool { return listed[code] })
	s.Met(map[string]string{"budi": "Budi", "sari": "Sari", "joko": "Joko"})

	srv := httptest.NewServer(s.Handler(nil))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/?pid=sari", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	waitStatus := func(id, want string) FriendView {
		t.Helper()
		for i := 0; i < 100; i++ {
			for _, v := range s.List("budi") {
				if v.ID == id && v.Status == want {
					return v
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("%s never became %q: %+v", id, want, s.List("budi"))
		return FriendView{}
	}
	waitStatus("sari", StatusOnline)
	if v := s.List("budi"); v[0].ID != "sari" {
		t.Fatalf("online friends first: %+v", v)
	}

	if got := s.Invite("budi", "Budi", "sari", "K7QX"); got != InviteOK {
		t.Fatalf("invite = %q", got)
	}
	_, b, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	_ = json.Unmarshal(b, &m)
	if m["t"] != "invite" || m["from"] != "Budi" || m["room"] != "K7QX" {
		t.Fatalf("invite message = %s", b)
	}
	if got := s.Invite("budi", "Budi", "sari", "K7QX"); got != InviteTooSoon {
		t.Fatalf("second invite = %q", got)
	}
	if got := s.Invite("budi", "Budi", "joko", "K7QX"); got != InviteOffline {
		t.Fatalf("offline invite = %q", got)
	}
	if got := s.Invite("budi", "Budi", "stranger", "K7QX"); got != InviteNotFriend {
		t.Fatalf("stranger invite = %q", got)
	}

	s.Seated("joko", "PUBL", true)
	if v := waitStatus("joko", StatusPlaying); v.Table != "PUBL" {
		t.Fatalf("joko at a public table should be joinable: %+v", v)
	}
	s.Seated("joko", "PUBL", false)
	waitStatus("joko", StatusOffline)

	conn.Close(websocket.StatusNormalClosure, "")
	waitStatus("sari", StatusOffline)
}

func TestInviteRateLimit(t *testing.T) {
	f, _ := OpenFriends("")
	s := NewService(f, verifyAny, nil)
	players := map[string]string{"host": "Host"}
	for i := 0; i < inviteMax+2; i++ {
		players[string(rune('a'+i))] = "x"
	}
	s.Met(players)
	for id := range players { // everyone "online" without real sockets
		s.conns[id] = map[*listener]struct{}{{send: make(chan []byte, 8)}: {}}
	}
	sent := 0
	for id := range players {
		if id == "host" {
			continue
		}
		if s.Invite("host", "Host", id, "ROOM") == InviteOK {
			sent++
		}
	}
	if sent != inviteMax {
		t.Fatalf("sent %d invites in a minute, want cap %d", sent, inviteMax)
	}
}

func TestFriendsMoveMerges(t *testing.T) {
	f, _ := OpenFriends("")
	now := time.Now()
	f.now = func() time.Time { now = now.Add(time.Second); return now }
	_ = f.Met(map[string]string{"guest": "G", "sari": "Sari"})
	_ = f.Met(map[string]string{"acct": "A", "joko": "Joko"})
	_ = f.Met(map[string]string{"guest": "G", "acct": "A"}) // same person on two devices
	_ = f.Move("guest", "acct")
	got := map[string]bool{}
	for _, e := range f.List("acct") {
		got[e.ID] = true
	}
	if !got["sari"] || !got["joko"] || got["acct"] || got["guest"] || len(got) != 2 {
		t.Fatalf("merged list = %v", got)
	}
	if !f.Has("sari", "acct") || f.Has("sari", "guest") {
		t.Fatal("friends of the guest now point at the account")
	}
}
