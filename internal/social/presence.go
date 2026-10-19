package social

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Status of a friend, as shown next to their name.
const (
	StatusOffline = ""
	StatusOnline  = "online"  // app open, not at a table
	StatusPlaying = "playing" // seated at a table
)

// Service ties the friend list to who is online and delivers invites.
type Service struct {
	Friends *Friends
	// Verify turns a signed token into a player ID.
	Verify func(token string) (string, bool)
	// Listed reports whether a table is open to join from Meja terbuka.
	Listed func(code string) bool

	mu       sync.Mutex
	conns    map[string]map[*listener]struct{} // player ID → open presence sockets
	seats    map[string]map[string]bool        // player ID → table codes they sit at
	lastSent map[[2]string]time.Time           // (from, to) → last invite
	sentLog  map[string][]time.Time            // from → recent invites
	now      func() time.Time
}

type listener struct{ send chan []byte }

// NewService wires a friend store to presence tracking.
func NewService(f *Friends, verify func(string) (string, bool), listed func(string) bool) *Service {
	return &Service{
		Friends: f, Verify: verify, Listed: listed,
		conns:    map[string]map[*listener]struct{}{},
		seats:    map[string]map[string]bool{},
		lastSent: map[[2]string]time.Time{},
		sentLog:  map[string][]time.Time{},
		now:      time.Now,
	}
}

// Met is called by a room when a game starts.
func (s *Service) Met(players map[string]string) {
	if err := s.Friends.Met(players); err != nil {
		slog.Error("record friends", "err", err)
	}
}

// Seated is called by a room when a person sits down at, or leaves, a table.
func (s *Service) Seated(id, code string, on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if on {
		if s.seats[id] == nil {
			s.seats[id] = map[string]bool{}
		}
		s.seats[id][code] = true
		return
	}
	delete(s.seats[id], code)
	if len(s.seats[id]) == 0 {
		delete(s.seats, id)
	}
}

// FriendView is a friend as the client sees them.
type FriendView struct {
	Entry
	Status string `json:"status"`
	Table  string `json:"table,omitempty"` // a public table they're waiting at
}

// List is someone's friends with live status, online first.
func (s *Service) List(id string) []FriendView {
	entries := s.Friends.List(id)
	out := make([]FriendView, 0, len(entries))
	tables := make([][]string, len(entries))
	s.mu.Lock()
	for i, e := range entries {
		v := FriendView{Entry: e}
		switch {
		case len(s.seats[e.ID]) > 0:
			v.Status = StatusPlaying
			for code := range s.seats[e.ID] {
				tables[i] = append(tables[i], code)
			}
		case len(s.conns[e.ID]) > 0:
			v.Status = StatusOnline
		}
		out = append(out, v)
	}
	s.mu.Unlock()
	// Asked outside our lock: Listed takes the room hub's lock.
	for i, codes := range tables {
		for _, code := range codes {
			if s.Listed != nil && s.Listed(code) {
				out[i].Table = code
			}
		}
	}
	rank := map[string]int{StatusOnline: 0, StatusPlaying: 1, StatusOffline: 2}
	// Stable sort keeps "most recently played" order within each group.
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Status] < rank[out[j].Status] })
	return out
}

// Invite errors, sent back to the inviter.
const (
	InviteOK         = ""
	InviteNotFriend  = "not_friend"
	InviteOffline    = "friend_offline"
	InviteTooSoon    = "invite_too_soon"
	InviteRateLimit  = "invite_rate_limit"
	inviteSameFriend = 20 * time.Second
	inviteWindow     = time.Minute
	inviteMax        = 10
)

type inviteMsg struct {
	T    string `json:"t"`
	From string `json:"from"`
	Room string `json:"room"`
}

// Invite asks a friend to come to a table. Only friends can be invited,
// only while they have the app open, and not too often.
func (s *Service) Invite(fromID, fromName, toID, code string) string {
	if !s.Friends.Has(fromID, toID) {
		return InviteNotFriend
	}
	b, _ := json.Marshal(inviteMsg{T: "invite", From: fromName, Room: code})
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if now.Sub(s.lastSent[[2]string{fromID, toID}]) < inviteSameFriend {
		return InviteTooSoon
	}
	recent := s.sentLog[fromID][:0]
	for _, t := range s.sentLog[fromID] {
		if now.Sub(t) < inviteWindow {
			recent = append(recent, t)
		}
	}
	if len(recent) >= inviteMax {
		s.sentLog[fromID] = recent
		return InviteRateLimit
	}
	if len(s.conns[toID]) == 0 {
		return InviteOffline
	}
	for l := range s.conns[toID] {
		select {
		case l.send <- b:
		default: // a stuck socket just misses this one
		}
	}
	s.lastSent[[2]string{fromID, toID}] = now
	s.sentLog[fromID] = append(recent, now)
	return InviteOK
}

// Handler serves /ws/presence?pid=TOKEN. The socket only receives: while
// it is open the player shows as online and can be invited.
func (s *Service) Handler(originPatterns []string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, ok := s.Verify(req.URL.Query().Get("pid"))
		if !ok {
			http.Error(w, "unknown player", http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{OriginPatterns: originPatterns})
		if err != nil {
			return
		}
		conn.SetReadLimit(256)
		l := &listener{send: make(chan []byte, 8)}
		s.mu.Lock()
		if s.conns[id] == nil {
			s.conns[id] = map[*listener]struct{}{}
		}
		s.conns[id][l] = struct{}{}
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			delete(s.conns[id], l)
			if len(s.conns[id]) == 0 {
				delete(s.conns, id)
			}
			s.mu.Unlock()
			conn.CloseNow()
		}()

		ctx, cancel := context.WithCancel(req.Context())
		defer cancel()
		go func() { // reads only to notice the client going away
			defer cancel()
			for {
				if _, _, err := conn.Read(ctx); err != nil {
					return
				}
			}
		}()
		ping := time.NewTicker(25 * time.Second)
		defer ping.Stop()
		for {
			select {
			case b := <-l.send:
				wctx, c := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Write(wctx, websocket.MessageText, b)
				c()
				if err != nil {
					return
				}
			case <-ping.C:
				pctx, c := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Ping(pctx)
				c()
				if err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}
}
