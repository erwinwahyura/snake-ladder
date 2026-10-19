package room

import (
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/erwinwahyura/snake-ladder/internal/game"
	"github.com/erwinwahyura/snake-ladder/internal/stats"
)

// Seats is the number of places at the table.
const Seats = 4

// Room is one table. All of its state is owned by the run goroutine;
// other goroutines talk to it only through post().
type Room struct {
	code  string
	hub   *Hub
	cfg   Config
	inbox chan any
	quit  chan struct{}
	done  chan struct{}

	// owned by run()
	seats      [Seats]*seat
	spectators map[*client]struct{}
	phase      string
	rules      game.Rules
	game       *game.Game
	busyUntil  time.Time
	deadline   time.Time
	turnTimer  *time.Timer
	lastActive time.Time
}

type seat struct {
	pid    string // stable per-browser player ID, for the leaderboard
	name   string
	token  string
	host   bool
	client *client // nil while disconnected
}

type joinEv struct {
	c                *client
	name, token, pid string
}

type leaveEv struct{ c *client }

// funcEv runs a function on the room goroutine (used by tests).
type funcEv func()

type msgEv struct {
	c   *client
	msg inMsg
}

func newRoom(code string, h *Hub) *Room {
	t := time.NewTimer(time.Hour)
	t.Stop()
	return &Room{
		code:       code,
		hub:        h,
		cfg:        h.cfg,
		inbox:      make(chan any, 64),
		quit:       make(chan struct{}),
		done:       make(chan struct{}),
		spectators: map[*client]struct{}{},
		phase:      PhaseLobby,
		rules:      game.DefaultRules(),
		turnTimer:  t,
		lastActive: time.Now(),
	}
}

// Code is the room's join code.
func (r *Room) Code() string { return r.code }

// post delivers an event to the room loop. It reports false if the room
// has already shut down.
func (r *Room) post(ev any) bool {
	select {
	case r.inbox <- ev:
		return true
	case <-r.done:
		return false
	}
}

func (r *Room) stop() {
	select {
	case <-r.quit:
	default:
		close(r.quit)
	}
	<-r.done
}

func (r *Room) run() {
	idle := time.NewTicker(r.cfg.Timing.IdleCheck)
	defer func() {
		idle.Stop()
		r.turnTimer.Stop()
		for _, s := range r.seats {
			if s != nil && s.client != nil {
				r.detach(s.client)
			}
		}
		for c := range r.spectators {
			r.detach(c)
		}
		r.hub.remove(r.code)
		close(r.done)
	}()

	for {
		select {
		case ev := <-r.inbox:
			r.lastActive = time.Now()
			switch ev := ev.(type) {
			case joinEv:
				r.onJoin(ev)
			case leaveEv:
				r.onDisconnect(ev.c)
			case msgEv:
				r.onMessage(ev.c, ev.msg)
			case funcEv:
				ev()
			}
		case <-r.turnTimer.C:
			r.onTurnTimeout()
		case <-idle.C:
			if r.connected() == 0 && time.Since(r.lastActive) > r.cfg.Timing.IdleRoomTTL {
				slog.Info("room closed (idle)", "room", r.code)
				return
			}
		case <-r.quit:
			return
		}
	}
}

// --- events ---

func (r *Room) onJoin(ev joinEv) {
	c := ev.c
	c.seat = -1

	// Reconnect: the token proves this browser held the seat before.
	if ev.token != "" {
		for i, s := range r.seats {
			if s != nil && s.token == ev.token {
				if s.client != nil {
					r.detach(s.client) // the same player opened a second tab
				}
				s.client, c.seat = c, i
				if n := cleanName(ev.name, i); strings.TrimSpace(ev.name) != "" {
					s.name = n
				}
				r.welcome(c, s.token)
				r.rescheduleTurn()
				r.broadcastState()
				return
			}
		}
	}

	if r.phase != PhasePlaying {
		if i := r.freeSeat(); i >= 0 {
			s := &seat{pid: ev.pid, name: cleanName(ev.name, i), token: newToken(), client: c}
			if r.host() < 0 {
				s.host = true
			}
			r.seats[i], c.seat = s, i
			r.welcome(c, s.token)
			r.broadcastState()
			return
		}
	}

	r.spectators[c] = struct{}{}
	r.welcome(c, "")
	r.broadcastState()
}

func (r *Room) onDisconnect(c *client) {
	if c.detached {
		return
	}
	if _, ok := r.spectators[c]; ok {
		delete(r.spectators, c)
		r.detach(c)
		r.broadcastState()
		return
	}
	if c.seat < 0 || r.seats[c.seat] == nil || r.seats[c.seat].client != c {
		r.detach(c)
		return
	}
	s := r.seats[c.seat]
	s.client = nil
	r.detach(c)
	if s.host {
		r.passHost(c.seat)
	}
	r.rescheduleTurn()
	r.broadcastState()
}

func (r *Room) onMessage(c *client, m inMsg) {
	if c.detached {
		return
	}
	switch m.T {
	case msgStart:
		r.onStart(c)
	case msgRoll:
		r.onRoll(c)
	case msgLeave:
		r.onLeave(c)
	case msgRules:
		r.onRules(c, m.Rules)
	case msgReact:
		r.onReact(c, m.Emoji)
	case msgKick:
		r.onKick(c, m.Seat)
	default:
		r.sendErr(c, "bad_message")
	}
}

func (r *Room) onStart(c *client) {
	if c.seat < 0 || !r.seats[c.seat].host {
		r.sendErr(c, "not_host")
		return
	}
	if r.phase == PhasePlaying {
		r.sendErr(c, "already_playing")
		return
	}
	// Seats whose player wandered off before the start are freed up.
	var order []int
	for i, s := range r.seats {
		switch {
		case s == nil:
		case s.client == nil:
			r.seats[i] = nil
		default:
			order = append(order, i)
		}
	}
	r.game = game.New(r.cfg.Board, r.rules, order)
	r.phase = PhasePlaying
	r.busyUntil = time.Now()
	r.scheduleTurn()
	r.broadcastState()
}

func (r *Room) onRules(c *client, rules *game.Rules) {
	switch {
	case c.seat < 0 || !r.seats[c.seat].host:
		r.sendErr(c, "not_host")
	case r.phase == PhasePlaying:
		r.sendErr(c, "already_playing")
	case rules == nil || !rules.Valid():
		r.sendErr(c, "bad_rules")
	default:
		r.rules = *rules
		r.broadcastState()
	}
}

// onKick lets the host clear a seat whose player has gone, so a stale
// lobby doesn't wait for someone who closed the tab. Connected players
// can't be kicked: that would just be rude.
func (r *Room) onKick(c *client, target *int) {
	switch {
	case c.seat < 0 || !r.seats[c.seat].host:
		r.sendErr(c, "not_host")
	case r.phase == PhasePlaying:
		r.sendErr(c, "already_playing")
	case target == nil || *target < 0 || *target >= Seats || r.seats[*target] == nil:
		r.sendErr(c, "bad_seat")
	case r.seats[*target].client != nil:
		r.sendErr(c, "still_connected")
	default:
		r.seats[*target] = nil
		r.broadcastState()
	}
}

// reactCooldown stops one player flooding the table with emoji.
const reactCooldown = 1200 * time.Millisecond

func (r *Room) onReact(c *client, emoji string) {
	switch {
	case c.seat < 0:
		r.sendErr(c, "not_seated")
	case !validReaction(emoji):
		r.sendErr(c, "bad_reaction")
	case time.Since(c.lastReact) < reactCooldown:
		// Silently ignore: the button is already throttled client-side.
	default:
		c.lastReact = time.Now()
		r.broadcast(reactionMsg{T: "reaction", Seat: c.seat, Emoji: emoji})
	}
}

func (r *Room) onRoll(c *client) {
	switch {
	case r.phase != PhasePlaying:
		r.sendErr(c, "not_playing")
	case c.seat < 0 || c.seat != r.game.Current():
		r.sendErr(c, "not_your_turn")
	case time.Now().Before(r.busyUntil):
		r.sendErr(c, "still_moving")
	default:
		r.roll(false)
	}
}

func (r *Room) onLeave(c *client) {
	if c.seat < 0 {
		return
	}
	if r.phase == PhasePlaying {
		// Mid-game the seat stays and auto-rolls; the token still works.
		r.onDisconnect(c)
		return
	}
	i := c.seat
	wasHost := r.seats[i].host
	r.seats[i] = nil
	c.seat = -1
	r.spectators[c] = struct{}{}
	if wasHost {
		r.passHost(i)
	}
	r.welcome(c, "")
	r.broadcastState()
}

func (r *Room) onTurnTimeout() {
	if r.phase != PhasePlaying || r.game.Current() < 0 {
		return
	}
	if wait := time.Until(r.busyUntil); wait > 0 {
		r.turnTimer.Reset(wait)
		return
	}
	r.roll(true)
}

// --- game flow ---

func (r *Room) roll(auto bool) {
	m, err := r.game.Roll(r.cfg.Dice())
	if err != nil {
		slog.Error("roll", "room", r.code, "err", err)
		return
	}
	anim := r.animFor(m)
	r.busyUntil = time.Now().Add(anim)
	r.broadcast(movedMsg{T: "moved", Move: m, AnimMs: anim.Milliseconds(), Auto: auto})
	if m.Won {
		r.phase = PhaseFinished
		r.turnTimer.Stop()
		r.deadline = time.Time{}
		r.record()
	} else {
		r.scheduleTurn()
	}
	r.broadcastState()
}

// ranked: only games with at least two players count, so nobody farms
// wins by playing alone.
func (r *Room) ranked() bool { return r.game != nil && len(r.game.Seats) >= 2 }

func (r *Room) results() []resultView {
	out := make([]resultView, 0, len(r.game.Seats))
	for _, i := range r.game.Seats {
		st := r.game.Stats[i]
		rv := resultView{Seat: i, Pos: r.game.Pos[i], Rolls: st.Rolls, Ladders: st.Ladders, Snakes: st.Snakes, Won: r.game.Winner == i}
		if s := r.seats[i]; s != nil {
			rv.Name = s.name
		}
		out = append(out, rv)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Won != out[b].Won {
			return out[a].Won
		}
		return out[a].Pos > out[b].Pos
	})
	return out
}

func (r *Room) record() {
	if r.cfg.Recorder == nil || !r.ranked() {
		return
	}
	var rs []stats.Result
	for _, i := range r.game.Seats {
		s, st := r.seats[i], r.game.Stats[i]
		if s == nil {
			continue
		}
		rs = append(rs, stats.Result{PlayerID: s.pid, Name: s.name, Won: r.game.Winner == i, Rolls: st.Rolls, Ladders: st.Ladders, Snakes: st.Snakes})
	}
	if err := r.cfg.Recorder.Record(rs); err != nil {
		slog.Error("record leaderboard", "room", r.code, "err", err)
	}
}

func (r *Room) animFor(m game.Move) time.Duration {
	t := r.cfg.Timing
	d := t.Dice + time.Duration(m.Steps())*t.Step + t.Settle
	if m.Kind != game.KindNone {
		d += t.Jump
	}
	return d
}

// scheduleTurn sets the auto-roll deadline for whoever's turn it is.
func (r *Room) scheduleTurn() {
	cur := r.game.Current()
	if cur < 0 {
		return
	}
	// Nobody seated is watching: pause rather than auto-play the whole game
	// (and hand someone a leaderboard win) while everyone is away.
	if r.connectedSeats() == 0 {
		r.turnTimer.Stop()
		r.deadline = time.Time{}
		return
	}
	wait := r.cfg.Timing.TurnTimeout
	if s := r.seats[cur]; s == nil || s.client == nil {
		wait = r.cfg.Timing.AbsentTimeout
	}
	start := r.busyUntil
	if now := time.Now(); start.Before(now) {
		start = now
	}
	r.deadline = start.Add(wait)
	r.turnTimer.Reset(time.Until(r.deadline))
}

// rescheduleTurn re-evaluates the deadline when the current player
// connects or disconnects.
func (r *Room) rescheduleTurn() {
	if r.phase == PhasePlaying && r.game.Current() >= 0 {
		r.scheduleTurn()
	}
}

// --- helpers ---

func (r *Room) freeSeat() int {
	for i, s := range r.seats {
		if s == nil {
			return i
		}
	}
	// Reclaim a seat whose player left the lobby without reconnecting.
	for i, s := range r.seats {
		if s.client == nil {
			wasHost := s.host
			r.seats[i] = nil
			if wasHost {
				r.passHost(i)
			}
			return i
		}
	}
	return -1
}

func (r *Room) host() int {
	for i, s := range r.seats {
		if s != nil && s.host {
			return i
		}
	}
	return -1
}

// passHost moves the host role from seat `from` to the next connected
// player, if there is one.
func (r *Room) passHost(from int) {
	for k := 1; k <= Seats; k++ {
		i := (from + k) % Seats
		if s := r.seats[i]; s != nil && s.client != nil {
			if f := r.seats[from]; f != nil {
				f.host = false
			}
			s.host = true
			return
		}
	}
}

func (r *Room) connectedSeats() int {
	n := 0
	for _, s := range r.seats {
		if s != nil && s.client != nil {
			n++
		}
	}
	return n
}

func (r *Room) connected() int {
	n := len(r.spectators)
	for _, s := range r.seats {
		if s != nil && s.client != nil {
			n++
		}
	}
	return n
}

func (r *Room) state() stateMsg {
	st := stateMsg{
		T:          "state",
		Room:       r.code,
		Phase:      r.phase,
		Rules:      r.rules,
		Players:    []playerView{},
		Turn:       -1,
		Winner:     -1,
		Now:        time.Now().UnixMilli(),
		Spectators: len(r.spectators),
	}
	for i, s := range r.seats {
		if s == nil {
			continue
		}
		p := playerView{Seat: i, Name: s.name, Connected: s.client != nil, Host: s.host}
		if r.game != nil {
			if pos, ok := r.game.Pos[i]; ok && r.phase != PhaseLobby {
				p.Pos, p.Playing = pos, true
			}
		}
		st.Players = append(st.Players, p)
	}
	sort.Slice(st.Players, func(a, b int) bool { return st.Players[a].Seat < st.Players[b].Seat })
	if r.game != nil && r.phase != PhaseLobby {
		st.Turn = r.game.Current()
		st.Winner = r.game.Winner
	}
	if r.phase == PhasePlaying && !r.deadline.IsZero() {
		st.Deadline = r.deadline.UnixMilli()
	}
	if r.game != nil {
		st.Ranked = r.ranked()
	} else {
		st.Ranked = r.connectedSeats() >= 2
	}
	if r.phase == PhaseFinished {
		st.Results = r.results()
	}
	return st
}

func (r *Room) welcome(c *client, token string) {
	t := r.cfg.Timing
	r.send(c, welcomeMsg{
		T:     "welcome",
		Room:  r.code,
		Seat:  c.seat,
		Token: token,
		Board: boardView{Ladders: r.cfg.Board.Ladders(), Snakes: r.cfg.Board.Snakes()},
		Timing: timingView{
			DiceMs: t.Dice.Milliseconds(),
			StepMs: t.Step.Milliseconds(),
			JumpMs: t.Jump.Milliseconds(),
		},
	})
}

func (r *Room) broadcastState() { r.broadcast(r.state()) }

func (r *Room) broadcast(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("marshal", "err", err)
		return
	}
	for _, s := range r.seats {
		if s != nil && s.client != nil {
			r.sendRaw(s.client, b)
		}
	}
	for c := range r.spectators {
		r.sendRaw(c, b)
	}
}

func (r *Room) send(c *client, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("marshal", "err", err)
		return
	}
	r.sendRaw(c, b)
}

func (r *Room) sendErr(c *client, code string) { r.send(c, errorMsg{T: "error", Code: code}) }

// sendRaw never blocks the room: a client too slow to drain its buffer
// is dropped and can reconnect with its token.
func (r *Room) sendRaw(c *client, b []byte) {
	if c.detached {
		return
	}
	select {
	case c.send <- b:
	default:
		slog.Warn("dropping slow client", "room", r.code, "seat", c.seat)
		if c.seat >= 0 && r.seats[c.seat] != nil && r.seats[c.seat].client == c {
			r.seats[c.seat].client = nil
		}
		delete(r.spectators, c)
		r.detach(c)
	}
}

// detach closes the client's outbound queue, which makes its writer close
// the socket. Only the room goroutine sends on or closes c.send.
func (r *Room) detach(c *client) {
	if !c.detached {
		c.detached = true
		close(c.send)
	}
}

func cleanName(name string, seat int) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	if rs := []rune(name); len(rs) > 16 {
		name = string(rs[:16])
	}
	if name == "" {
		name = "Pemain " + string(rune('1'+seat))
	}
	return name
}
