package room

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/erwinwahyura/snake-ladder/internal/stats"
)

// fastTiming keeps animations near zero so tests run quickly.
func fastTiming() Timing {
	return Timing{
		TurnTimeout:   time.Second,
		AbsentTimeout: 20 * time.Millisecond,
		Dice:          time.Millisecond,
		Step:          time.Millisecond,
		Jump:          time.Millisecond,
		Settle:        time.Millisecond,
		BotThink:      2 * time.Millisecond,
		MatchWait:     time.Hour, // tests that need the clock set their own
		IdleRoomTTL:   time.Hour,
		IdleCheck:     time.Hour,
	}
}

type scriptedDice struct {
	mu    sync.Mutex
	rolls []int
}

func (d *scriptedDice) next() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.rolls) == 0 {
		return 1
	}
	v := d.rolls[0]
	d.rolls = d.rolls[1:]
	return v
}

type testClient struct {
	t    *testing.T
	conn *websocket.Conn
	msgs chan map[string]any
}

func dial(t *testing.T, srv *httptest.Server, params url.Values) *testClient {
	t.Helper()
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?" + params.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	tc := &testClient{t: t, conn: conn, msgs: make(chan map[string]any, 256)}
	go func() {
		defer close(tc.msgs)
		for {
			_, b, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			tc.msgs <- m
		}
	}()
	t.Cleanup(func() { conn.CloseNow() })
	return tc
}

func (tc *testClient) sendMsg(v any) {
	tc.t.Helper()
	b, _ := json.Marshal(v)
	if err := tc.conn.Write(context.Background(), websocket.MessageText, b); err != nil {
		tc.t.Fatal(err)
	}
}

// waitFor returns the first message of type typ for which ok returns true.
func (tc *testClient) waitFor(typ string, ok func(map[string]any) bool) map[string]any {
	tc.t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case m, open := <-tc.msgs:
			if !open {
				tc.t.Fatalf("connection closed while waiting for %s", typ)
			}
			if m["t"] == typ && (ok == nil || ok(m)) {
				return m
			}
		case <-timeout:
			tc.t.Fatalf("timed out waiting for %s", typ)
		}
	}
}

func phaseIs(p string) func(map[string]any) bool {
	return func(m map[string]any) bool { return m["phase"] == p }
}

func newTestServer(t *testing.T, dice *scriptedDice, rec stats.Recorder) (*httptest.Server, *Hub) {
	t.Helper()
	h := NewHub(Config{Timing: fastTiming(), Dice: dice.next, Recorder: rec})
	srv := httptest.NewServer(h.Handler(nil))
	t.Cleanup(func() { h.Close(); srv.Close() })
	return srv, h
}

func TestFullGame(t *testing.T) {
	// Seat 0 rolls a 4 (ladder 4->14), seat 1 rolls 5 (stays at 5).
	dice := &scriptedDice{rolls: []int{4, 5}}
	store, _ := stats.Open("")
	srv, h := newTestServer(t, dice, store)

	host := dial(t, srv, url.Values{"name": {"Budi"}, "pid": {"player-budi"}})
	w := host.waitFor("welcome", nil)
	code := w["room"].(string)
	if w["seat"].(float64) != 0 || w["token"] == "" {
		t.Fatalf("host welcome %v", w)
	}
	if h.Get(strings.ToLower(code)) == nil {
		t.Fatal("room lookup should be case-insensitive")
	}

	guest := dial(t, srv, url.Values{"room": {code}, "name": {"Sari"}, "pid": {"player-sari"}})
	if gw := guest.waitFor("welcome", nil); gw["seat"].(float64) != 1 {
		t.Fatalf("guest welcome %v", gw)
	}

	guest.sendMsg(map[string]any{"t": "start"})
	if e := guest.waitFor("error", nil); e["code"] != "not_host" {
		t.Fatalf("guest start: %v", e)
	}

	host.sendMsg(map[string]any{"t": "rules", "rules": map[string]any{"overshoot": "bounce", "sixAgain": false}})
	host.waitFor("state", func(m map[string]any) bool {
		return m["rules"].(map[string]any)["overshoot"] == "bounce"
	})

	host.sendMsg(map[string]any{"t": "start"})
	host.waitFor("state", phaseIs(PhasePlaying))

	guest.sendMsg(map[string]any{"t": "roll"})
	if e := guest.waitFor("error", nil); e["code"] != "not_your_turn" {
		t.Fatalf("out of turn roll: %v", e)
	}

	host.sendMsg(map[string]any{"t": "roll"})
	mv := guest.waitFor("moved", nil)["move"].(map[string]any)
	if mv["seat"].(float64) != 0 || mv["to"].(float64) != 14 || mv["kind"] != "ladder" {
		t.Fatalf("move = %v", mv)
	}

	time.Sleep(20 * time.Millisecond) // let the animation window pass
	guest.sendMsg(map[string]any{"t": "roll"})
	mv = host.waitFor("moved", func(m map[string]any) bool {
		return m["move"].(map[string]any)["seat"].(float64) == 1
	})["move"].(map[string]any)
	if mv["to"].(float64) != 5 {
		t.Fatalf("guest move = %v", mv)
	}
}

func TestReconnectKeepsSeat(t *testing.T) {
	srv, _ := newTestServer(t, &scriptedDice{}, nil)
	a := dial(t, srv, url.Values{"name": {"A"}})
	w := a.waitFor("welcome", nil)
	code, token := w["room"].(string), w["token"].(string)
	b := dial(t, srv, url.Values{"room": {code}, "name": {"B"}})
	b.waitFor("welcome", nil)

	a.sendMsg(map[string]any{"t": "start"})
	a.waitFor("state", phaseIs(PhasePlaying))
	a.conn.Close(websocket.StatusNormalClosure, "")

	// B sees A as disconnected, then A comes back with the token.
	b.waitFor("state", func(m map[string]any) bool {
		for _, p := range m["players"].([]any) {
			p := p.(map[string]any)
			if p["seat"].(float64) == 0 && p["connected"] == false {
				return true
			}
		}
		return false
	})
	a2 := dial(t, srv, url.Values{"room": {code}, "token": {token}})
	if w := a2.waitFor("welcome", nil); w["seat"].(float64) != 0 {
		t.Fatalf("reconnect seat = %v", w["seat"])
	}

	// A newcomer mid-game becomes a spectator.
	c := dial(t, srv, url.Values{"room": {code}, "name": {"C"}})
	if w := c.waitFor("welcome", nil); w["seat"].(float64) != -1 {
		t.Fatalf("mid-game join should spectate, got seat %v", w["seat"])
	}
}

func TestAbsentPlayerAutoRollsAndGameIsRecorded(t *testing.T) {
	dice := &scriptedDice{rolls: []int{6}}
	store, _ := stats.Open("")
	srv, h := newTestServer(t, dice, store)

	a := dial(t, srv, url.Values{"name": {"A"}, "pid": {"player-aaa"}})
	code := a.waitFor("welcome", nil)["room"].(string)
	b := dial(t, srv, url.Values{"room": {code}, "name": {"B"}, "pid": {"player-bbb"}})
	b.waitFor("welcome", nil)

	a.sendMsg(map[string]any{"t": "rules", "rules": map[string]any{"overshoot": "stay", "sixAgain": false}})
	a.sendMsg(map[string]any{"t": "start"})
	a.waitFor("state", phaseIs(PhasePlaying))

	// Put seat 0 six away from the finish, then let the timer roll for
	// them after they disconnect.
	r := h.Get(code)
	done := make(chan struct{})
	r.post(funcEv(func() { r.game.Pos[0] = 94; close(done) }))
	<-done
	a.conn.Close(websocket.StatusNormalClosure, "")

	end := b.waitFor("state", phaseIs(PhaseFinished))
	if end["winner"].(float64) != 0 || end["ranked"] != true {
		t.Fatalf("end state %v", end)
	}
	if rec, ok := store.Get("player-aaa"); !ok || rec.Wins != 1 || rec.Games != 1 || rec.Points != 10 {
		t.Fatalf("leaderboard A = %+v %v", rec, ok)
	}
	if rec, _ := store.Get("player-bbb"); rec.Games != 1 || rec.Wins != 0 || rec.Points != 3 {
		t.Fatalf("leaderboard B = %+v", rec)
	}
}

func TestGamePausesWhenEveryoneLeaves(t *testing.T) {
	srv, h := newTestServer(t, &scriptedDice{}, nil)
	a := dial(t, srv, url.Values{"name": {"A"}})
	w := a.waitFor("welcome", nil)
	code, token := w["room"].(string), w["token"].(string)
	b := dial(t, srv, url.Values{"room": {code}, "name": {"B"}})
	b.waitFor("welcome", nil)
	a.sendMsg(map[string]any{"t": "start"})
	a.waitFor("state", phaseIs(PhasePlaying))
	a.conn.Close(websocket.StatusNormalClosure, "")
	b.conn.Close(websocket.StatusNormalClosure, "")

	// AbsentTimeout is 20ms. A could get one auto-roll before B also
	// leaves, so compare two snapshots taken once both are gone.
	r := h.Get(code)
	rolls := func() int {
		got := make(chan int)
		r.post(funcEv(func() { got <- r.game.Stats[0].Rolls + r.game.Stats[1].Rolls }))
		return <-got
	}
	time.Sleep(60 * time.Millisecond)
	before := rolls()
	time.Sleep(200 * time.Millisecond)
	if after := rolls(); after != before {
		t.Fatalf("game kept playing with nobody connected: %d -> %d rolls", before, after)
	}

	// Coming back resumes the clock.
	a2 := dial(t, srv, url.Values{"room": {code}, "token": {token}})
	st := a2.waitFor("state", phaseIs(PhasePlaying))
	if st["deadline"].(float64) == 0 {
		t.Fatal("deadline should resume after reconnect")
	}
}

func TestReactions(t *testing.T) {
	srv, _ := newTestServer(t, &scriptedDice{}, nil)
	a := dial(t, srv, url.Values{"name": {"A"}})
	code := a.waitFor("welcome", nil)["room"].(string)
	b := dial(t, srv, url.Values{"room": {code}, "name": {"B"}})
	b.waitFor("welcome", nil)

	a.sendMsg(map[string]any{"t": "react", "emoji": "🐍"})
	got := b.waitFor("reaction", nil)
	if got["seat"].(float64) != 0 || got["emoji"] != "🐍" {
		t.Fatalf("reaction = %v", got)
	}
	a.sendMsg(map[string]any{"t": "react", "emoji": "<script>"})
	if e := a.waitFor("error", nil); e["code"] != "bad_reaction" {
		t.Fatalf("want bad_reaction, got %v", e)
	}

	// Within the cooldown a second reaction is dropped; B must not see it.
	a.sendMsg(map[string]any{"t": "react", "emoji": "😂"})
	a.sendMsg(map[string]any{"t": "start"}) // marker: anything before it was relayed already
	deadline := time.After(3 * time.Second)
	for {
		select {
		case m := <-b.msgs:
			if m["t"] == "reaction" {
				t.Fatalf("cooldown not applied: %v", m)
			}
			if m["t"] == "state" && m["phase"] == PhasePlaying {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for start")
		}
	}
}

func TestHostRemovesAwayPlayer(t *testing.T) {
	srv, _ := newTestServer(t, &scriptedDice{}, nil)
	a := dial(t, srv, url.Values{"name": {"A"}})
	code := a.waitFor("welcome", nil)["room"].(string)
	b := dial(t, srv, url.Values{"room": {code}, "name": {"B"}})
	b.waitFor("welcome", nil)

	a.sendMsg(map[string]any{"t": "kick", "seat": 1})
	if e := a.waitFor("error", nil); e["code"] != "still_connected" {
		t.Fatalf("kicking a connected player: %v", e)
	}
	b.sendMsg(map[string]any{"t": "kick", "seat": 0})
	if e := b.waitFor("error", nil); e["code"] != "not_host" {
		t.Fatalf("non-host kick: %v", e)
	}

	b.conn.Close(websocket.StatusNormalClosure, "")
	a.waitFor("state", func(m map[string]any) bool {
		for _, p := range m["players"].([]any) {
			if p := p.(map[string]any); p["seat"].(float64) == 1 && p["connected"] == false {
				return true
			}
		}
		return false
	})
	a.sendMsg(map[string]any{"t": "kick", "seat": 1})
	a.waitFor("state", func(m map[string]any) bool { return len(m["players"].([]any)) == 1 })
}

func players(m map[string]any) []map[string]any {
	var out []map[string]any
	for _, p := range m["players"].([]any) {
		out = append(out, p.(map[string]any))
	}
	return out
}

func TestTableSizeAndVisibility(t *testing.T) {
	srv, h := newTestServer(t, &scriptedDice{}, nil)
	a := dial(t, srv, url.Values{"name": {"Budi"}})
	code := a.waitFor("welcome", nil)["room"].(string)
	if l := h.Public(10); len(l) != 0 {
		t.Fatalf("new rooms are private: %+v", l)
	}

	a.sendMsg(map[string]any{"t": "config", "size": 2, "public": true})
	a.waitFor("state", func(m map[string]any) bool { return m["size"].(float64) == 2 && m["public"] == true })
	l := h.Public(10)
	if len(l) != 1 || l[0].Code != code || l[0].Host != "Budi" || l[0].Size != 2 || l[0].Humans != 1 {
		t.Fatalf("listing = %+v", l)
	}

	// Two seats sit opposite each other: the guest gets seat 2.
	b := dial(t, srv, url.Values{"room": {code}, "name": {"Sari"}})
	if w := b.waitFor("welcome", nil); w["seat"].(float64) != 2 {
		t.Fatalf("2-seat table should seat the guest opposite, got %v", w["seat"])
	}
	a.waitFor("state", func(m map[string]any) bool { return len(players(m)) == 2 })
	if l := h.Public(10); len(l) != 0 {
		t.Fatalf("a full table must leave the list: %+v", l)
	}
	c := dial(t, srv, url.Values{"room": {code}, "name": {"C"}})
	if w := c.waitFor("welcome", nil); w["seat"].(float64) != -1 {
		t.Fatal("third person at a 2-seat table should spectate")
	}

	// Growing to 4 is fine; shrinking below a connected person is refused.
	a.sendMsg(map[string]any{"t": "config", "size": 4})
	a.waitFor("state", func(m map[string]any) bool { return m["size"].(float64) == 4 })
	a.sendMsg(map[string]any{"t": "config", "size": 3}) // seat 3 empty: ok
	a.waitFor("state", func(m map[string]any) bool { return m["size"].(float64) == 3 })
	a.sendMsg(map[string]any{"t": "config", "size": 7})
	if e := a.waitFor("error", nil); e["code"] != "bad_size" {
		t.Fatalf("bad size: %v", e)
	}
	b.sendMsg(map[string]any{"t": "config", "size": 2})
	if e := b.waitFor("error", nil); e["code"] != "not_host" {
		t.Fatalf("guest config: %v", e)
	}
}

func TestBotsPlayAndPeopleReplaceThem(t *testing.T) {
	dice := &scriptedDice{rolls: []int{2, 3, 4, 5, 1, 2, 3}}
	store, _ := stats.Open("")
	srv, h := newTestServer(t, dice, store)
	a := dial(t, srv, url.Values{"name": {"Budi"}, "pid": {"player-budi"}})
	code := a.waitFor("welcome", nil)["room"].(string)

	a.sendMsg(map[string]any{"t": "config", "public": true})
	a.sendMsg(map[string]any{"t": "bot", "action": "fill"})
	st := a.waitFor("state", func(m map[string]any) bool { return len(players(m)) == 4 })
	bots := 0
	for _, p := range players(st) {
		if p["bot"] == true {
			bots++
			if !strings.HasPrefix(p["name"].(string), "Bot ") || p["connected"] != true {
				t.Fatalf("bot view = %v", p)
			}
		}
	}
	if bots != 3 {
		t.Fatalf("want 3 bots, got %d", bots)
	}
	if l := h.Public(10); len(l) != 1 || l[0].Bots != 3 {
		t.Fatalf("a table with bots stays joinable: %+v", h.Public(10))
	}
	a.sendMsg(map[string]any{"t": "bot", "action": "add"})
	if e := a.waitFor("error", nil); e["code"] != "no_free_seat" {
		t.Fatalf("add to full table: %v", e)
	}

	// A person joining takes a bot's seat.
	b := dial(t, srv, url.Values{"room": {code}, "name": {"Sari"}, "pid": {"player-sari"}})
	if w := b.waitFor("welcome", nil); w["seat"].(float64) < 1 {
		t.Fatalf("joiner should replace a bot, got seat %v", w["seat"])
	}
	a.waitFor("state", func(m map[string]any) bool {
		n := 0
		for _, p := range players(m) {
			if p["bot"] == true {
				n++
			}
		}
		return n == 2 && len(players(m)) == 4
	})
	b.conn.Close(websocket.StatusNormalClosure, "")
	a.waitFor("state", func(m map[string]any) bool {
		for _, p := range players(m) {
			if p["name"] == "Sari" && p["connected"] == false {
				return true
			}
		}
		return false
	})

	// Bots roll on their own; Budi rolls his turns. Sari (gone before the
	// start) loses her seat, so it's Budi vs 2 bots: recorded as a bot game.
	a.sendMsg(map[string]any{"t": "start"})
	a.waitFor("state", phaseIs(PhasePlaying))
	r := h.Get(code)
	sawBotMove := false
	deadline := time.After(3 * time.Second)
	for !sawBotMove {
		select {
		case m := <-a.msgs:
			if m["t"] == "moved" {
				mv := m["move"].(map[string]any)
				if mv["seat"].(float64) != 0 {
					if m["auto"] == true {
						t.Fatal("a bot's roll is not a timeout auto-roll")
					}
					sawBotMove = true
				}
			}
			if m["t"] == "state" && m["turn"] == float64(0) && m["phase"] == PhasePlaying {
				a.sendMsg(map[string]any{"t": "roll"})
			}
		case <-deadline:
			t.Fatal("bots never rolled")
		}
	}

	// Finish quickly: put Budi one step from 100 and let him roll a 1...
	done := make(chan struct{})
	r.post(funcEv(func() {
		r.game.Pos[0] = 99
		dice.mu.Lock()
		dice.rolls = []int{1, 1, 1, 1}
		dice.mu.Unlock()
		close(done)
	}))
	<-done
	end := time.After(5 * time.Second)
	for {
		select {
		case m := <-a.msgs:
			if m["t"] == "state" && m["phase"] == PhasePlaying && m["turn"] == float64(0) {
				a.sendMsg(map[string]any{"t": "roll"})
			}
			if m["t"] == "state" && m["phase"] == PhaseFinished {
				if m["ranked"] == true {
					t.Fatal("one person vs bots is not ranked")
				}
				rec, ok := store.Get("player-budi")
				if !ok || rec.BotGames != 1 || rec.Games != 0 {
					t.Fatalf("bot game record = %+v %v", rec, ok)
				}
				if _, ok := store.Get("player-sari"); ok {
					t.Fatal("Sari left before the start and must not be recorded")
				}
				return
			}
		case <-end:
			t.Fatal("game did not finish")
		}
	}
}

type fakeSocial struct {
	mu      sync.Mutex
	met     []map[string]string
	seated  map[string]bool // id@code → on
	invites []string
}

func (f *fakeSocial) Met(p map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.met = append(f.met, p)
}

func (f *fakeSocial) Seated(id, code string, on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.seated == nil {
		f.seated = map[string]bool{}
	}
	f.seated[id+"@"+code] = on
}

func (f *fakeSocial) Invite(from, name, to, code string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invites = append(f.invites, from+">"+to+"@"+code)
	if to == "offline-friend" {
		return "friend_offline"
	}
	return ""
}

func (f *fakeSocial) isSeated(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seated[key]
}

func TestSocialHooksInvitesAndReports(t *testing.T) {
	soc := &fakeSocial{}
	h := NewHub(Config{Timing: fastTiming(), Dice: (&scriptedDice{}).next, Social: soc})
	srv := httptest.NewServer(h.Handler(nil))
	t.Cleanup(func() { h.Close(); srv.Close() })

	a := dial(t, srv, url.Values{"name": {"Budi"}, "pid": {"player-budi"}})
	code := a.waitFor("welcome", nil)["room"].(string)
	b := dial(t, srv, url.Values{"room": {code}, "name": {"Sari"}, "pid": {"player-sari"}})
	b.waitFor("welcome", nil)
	a.waitFor("state", func(m map[string]any) bool { return len(players(m)) == 2 })
	if !soc.isSeated("player-budi@"+code) || !soc.isSeated("player-sari@"+code) {
		t.Fatalf("both should be seated: %v", soc.seated)
	}

	a.sendMsg(map[string]any{"t": "invite", "friend": "player-joko"})
	if m := a.waitFor("invited", nil); m["friend"] != "player-joko" || m["result"] != "" {
		t.Fatalf("invited = %v", m)
	}
	a.sendMsg(map[string]any{"t": "invite", "friend": "offline-friend"})
	if m := a.waitFor("invited", nil); m["result"] != "friend_offline" {
		t.Fatalf("offline invite = %v", m)
	}

	// Public table, reported by two strangers: off the list. The host
	// reporting their own table does nothing.
	a.sendMsg(map[string]any{"t": "config", "public": true})
	a.waitFor("state", func(m map[string]any) bool { return m["public"] == true })
	h.Report(code, "player-budi")
	h.Report(code, "stranger-1")
	h.Report(code, "stranger-1") // same person twice counts once
	sync1 := make(chan struct{})
	h.Get(code).post(funcEv(func() { close(sync1) }))
	<-sync1
	if !h.Listed(code) {
		t.Fatal("one distinct report must not delist")
	}
	h.Report(code, "stranger-2")
	sync2 := make(chan struct{})
	h.Get(code).post(funcEv(func() { close(sync2) }))
	<-sync2
	if h.Listed(code) {
		t.Fatal("two distinct reports delist the table")
	}

	a.sendMsg(map[string]any{"t": "start"})
	a.waitFor("state", phaseIs(PhasePlaying))
	soc.mu.Lock()
	if len(soc.met) != 1 || soc.met[0]["player-budi"] != "Budi" || soc.met[0]["player-sari"] != "Sari" {
		t.Fatalf("met = %v", soc.met)
	}
	soc.mu.Unlock()
	a.sendMsg(map[string]any{"t": "invite", "friend": "player-joko"})
	if e := a.waitFor("error", nil); e["code"] != "already_playing" {
		t.Fatalf("invite mid-game: %v", e)
	}

	b.conn.Close(websocket.StatusNormalClosure, "")
	for i := 0; soc.isSeated("player-sari@" + code); i++ {
		if i > 100 {
			t.Fatal("Sari should be unseated after disconnecting")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRudeNamesReplaced(t *testing.T) {
	srv, _ := newTestServer(t, &scriptedDice{}, nil)
	a := dial(t, srv, url.Values{"name": {"4nj1ng"}})
	a.waitFor("welcome", nil)
	st := a.waitFor("state", nil)
	if n := players(st)[0]["name"]; n != "Pemain 1" {
		t.Fatalf("rude name shown as %q", n)
	}
}

func newMatchServer(t *testing.T, wait time.Duration) (*httptest.Server, *Hub) {
	t.Helper()
	tm := fastTiming()
	tm.MatchWait = wait
	h := NewHub(Config{Timing: tm, Dice: (&scriptedDice{}).next})
	srv := httptest.NewServer(h.Handler(nil))
	t.Cleanup(func() { h.Close(); srv.Close() })
	return srv, h
}

func TestQuickMatchGroupsPeopleAndStartsWhenFull(t *testing.T) {
	srv, h := newMatchServer(t, time.Hour)
	var code string
	var cs []*testClient
	for i, name := range []string{"A", "B", "C", "D"} {
		c := dial(t, srv, url.Values{"match": {"1"}, "name": {name}})
		w := c.waitFor("welcome", nil)
		if i == 0 {
			code = w["room"].(string)
			st := c.waitFor("state", nil)
			if st["match"] != true || st["size"].(float64) != 4 || st["matchAt"] == nil {
				t.Fatalf("match table state = %v", st)
			}
			for _, p := range players(st) {
				if p["host"] == true {
					t.Fatal("a Cari lawan table has no host")
				}
			}
		} else if w["room"] != code {
			t.Fatalf("%s went to %v, want %s", name, w["room"], code)
		}
		cs = append(cs, c)
		// Wait until the table shows them, so the next search finds it listed.
		if i > 0 {
			cs[0].waitFor("state", func(m map[string]any) bool { return len(players(m)) == i+1 })
		}
	}
	// Four people: starts at once, long before the hour-long clock.
	st := cs[0].waitFor("state", phaseIs(PhasePlaying))
	for _, p := range players(st) {
		if p["bot"] == true {
			t.Fatal("a full table of people needs no bots")
		}
	}
	if len(h.Public(10)) != 0 {
		t.Fatal("a running match is not listed")
	}
	// The next searcher gets a fresh table.
	e := dial(t, srv, url.Values{"match": {"1"}, "name": {"E"}})
	if w := e.waitFor("welcome", nil); w["room"] == code {
		t.Fatal("new searcher must not land in a running game")
	}
	// Nobody can reconfigure a hostless table.
	e.sendMsg(map[string]any{"t": "config", "size": 2})
	if m := e.waitFor("error", nil); m["code"] != "not_host" {
		t.Fatalf("config on match table: %v", m)
	}
}

func TestQuickMatchFillsWithBotsWhenTimeIsUp(t *testing.T) {
	srv, _ := newMatchServer(t, 80*time.Millisecond)
	a := dial(t, srv, url.Values{"match": {"1"}, "name": {"A"}})
	a.waitFor("welcome", nil)
	st := a.waitFor("state", phaseIs(PhasePlaying))
	bots := 0
	for _, p := range players(st) {
		if p["bot"] == true {
			bots++
		}
	}
	if bots != 3 {
		t.Fatalf("want 3 bots after the wait, got %d (%v)", bots, players(st))
	}
}

func TestQuickMatchWithNobodyLeftDoesNotStart(t *testing.T) {
	srv, h := newMatchServer(t, 60*time.Millisecond)
	a := dial(t, srv, url.Values{"match": {"1"}, "name": {"A"}})
	code := a.waitFor("welcome", nil)["room"].(string)
	a.conn.Close(websocket.StatusNormalClosure, "")
	time.Sleep(200 * time.Millisecond)
	r := h.Get(code)
	got := make(chan string)
	r.post(funcEv(func() { got <- r.phase }))
	if p := <-got; p != PhaseLobby {
		t.Fatalf("empty match table started anyway: %s", p)
	}
}
