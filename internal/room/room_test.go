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
	if rec, ok := store.Get("player-aaa"); !ok || rec.Wins != 1 || rec.Games != 1 {
		t.Fatalf("leaderboard A = %+v %v", rec, ok)
	}
	if rec, _ := store.Get("player-bbb"); rec.Games != 1 || rec.Wins != 0 {
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
