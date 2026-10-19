package room

import (
	crand "crypto/rand"
	"errors"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/erwinwahyura/snake-ladder/internal/game"
	"github.com/erwinwahyura/snake-ladder/internal/stats"
)

// Timing controls turn timeouts and how long the server waits for clients
// to finish animating a move before the next roll is accepted.
type Timing struct {
	TurnTimeout   time.Duration // a connected player must roll within this
	AbsentTimeout time.Duration // auto-roll delay for a disconnected player
	Dice          time.Duration // dice tumble animation
	Step          time.Duration // one tile hop
	Jump          time.Duration // snake slide or ladder climb
	Settle        time.Duration // pause after a move before the next turn
	BotThink      time.Duration // a bot waits this long (plus up to half again) before rolling
	IdleRoomTTL   time.Duration // empty rooms are removed after this
	IdleCheck     time.Duration // how often to look for idle rooms
}

// DefaultTiming is tuned for the web client's animations.
func DefaultTiming() Timing {
	return Timing{
		TurnTimeout:   20 * time.Second,
		AbsentTimeout: 2 * time.Second,
		Dice:          1100 * time.Millisecond,
		Step:          260 * time.Millisecond,
		Jump:          1000 * time.Millisecond,
		Settle:        400 * time.Millisecond,
		BotThink:      900 * time.Millisecond,
		IdleRoomTTL:   10 * time.Minute,
		IdleCheck:     time.Minute,
	}
}

// Config is shared by every room in a hub.
type Config struct {
	Timing   Timing
	Board    game.Board
	Dice     func() int // returns 1..6; defaults to a fair die
	MaxRooms int
	Recorder stats.Recorder // finished games go here; nil disables the leaderboard
	// Verify turns a client's signed player ID into the stable ID the
	// leaderboard stores. Nil accepts any well-formed ID (tests).
	Verify func(token string) (string, bool)
}

// ErrTooManyRooms is returned when the hub is at capacity.
var ErrTooManyRooms = errors.New("too many rooms")

// Hub owns all live rooms.
type Hub struct {
	cfg   Config
	mu    sync.Mutex
	rooms map[string]*Room
}

// NewHub returns a hub, filling unset config with defaults.
func NewHub(cfg Config) *Hub {
	if cfg.Board.Jumps == nil {
		cfg.Board = game.Classic()
	}
	if cfg.Dice == nil {
		cfg.Dice = func() int { return rand.IntN(6) + 1 }
	}
	if cfg.MaxRooms == 0 {
		cfg.MaxRooms = 1000
	}
	if cfg.Timing == (Timing{}) {
		cfg.Timing = DefaultTiming()
	}
	return &Hub{cfg: cfg, rooms: map[string]*Room{}}
}

// Create makes a room with a fresh code and starts its loop.
func (h *Hub) Create() (*Room, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.rooms) >= h.cfg.MaxRooms {
		return nil, ErrTooManyRooms
	}
	code := newCode()
	for h.rooms[code] != nil {
		code = newCode()
	}
	r := newRoom(code, h)
	h.rooms[code] = r
	go r.run()
	return r, nil
}

// Get finds a room by code, case-insensitively.
func (h *Hub) Get(code string) *Room {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.rooms[strings.ToUpper(strings.TrimSpace(code))]
}

// Len is the number of live rooms.
func (h *Hub) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.rooms)
}

// Close stops every room and disconnects its clients.
func (h *Hub) Close() {
	h.mu.Lock()
	rooms := make([]*Room, 0, len(h.rooms))
	for _, r := range h.rooms {
		rooms = append(rooms, r)
	}
	h.mu.Unlock()
	for _, r := range rooms {
		r.stop()
	}
}

// Public lists open public tables that someone could join right now:
// fullest first (a game starts sooner), then newest.
func (h *Hub) Public(limit int) []Listing {
	h.mu.Lock()
	out := []Listing{}
	for _, r := range h.rooms {
		if l := r.listing.Load(); l != nil {
			out = append(out, *l)
		}
	}
	h.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Humans != out[j].Humans {
			return out[i].Humans > out[j].Humans
		}
		return out[i].created.After(out[j].created)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (h *Hub) remove(code string) {
	h.mu.Lock()
	delete(h.rooms, code)
	h.mu.Unlock()
}

// Room codes avoid look-alike characters (0/O, 1/I) so they survive being
// read out loud or typed from a WhatsApp message.
const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newCode() string {
	b := make([]byte, 4)
	for i := range b {
		b[i] = codeAlphabet[rand.IntN(len(codeAlphabet))]
	}
	return string(b)
}

func newToken() string {
	return crand.Text() // 26 base32 chars from crypto/rand
}
