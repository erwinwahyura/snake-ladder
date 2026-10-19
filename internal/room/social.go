package room

import (
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/erwinwahyura/snake-ladder/internal/game"
)

// Friends, invites, reports and bots' reactions.

// markSeated tells the presence service a person sat down or left.
func (r *Room) markSeated(s *seat, on bool) {
	if r.cfg.Social != nil && s != nil && !s.bot && s.pid != "" {
		r.cfg.Social.Seated(s.pid, r.code, on)
	}
}

func (r *Room) onInvite(c *client, friend string) {
	switch {
	case r.cfg.Social == nil:
		r.sendErr(c, "bad_message")
	case c.seat < 0 || r.seats[c.seat] == nil || r.seats[c.seat].pid == "":
		r.sendErr(c, "not_seated")
	case r.phase == PhasePlaying:
		r.sendErr(c, "already_playing")
	default:
		s := r.seats[c.seat]
		res := r.cfg.Social.Invite(s.pid, s.name, friend, r.code)
		r.send(c, invitedMsg{T: "invited", Friend: friend, Result: res})
	}
}

// reportsToDelist distinct reports take a table off Meja terbuka. It can
// still be played and joined by code; strangers just won't find it.
const reportsToDelist = 2

func (r *Room) onReport(id string) {
	if id == "" || !r.public || r.delisted {
		return
	}
	for _, s := range r.seats {
		if s != nil && s.pid == id {
			return // reporting your own table does nothing
		}
	}
	if r.reports == nil {
		r.reports = map[string]bool{}
	}
	r.reports[id] = true
	if len(r.reports) >= reportsToDelist {
		r.delisted = true
		slog.Warn("table delisted after reports", "room", r.code, "host", r.hostName())
		r.publishListing()
	}
}

func (r *Room) hostName() string {
	if i := r.host(); i >= 0 {
		return r.seats[i].name
	}
	return ""
}

// botReact sometimes has a bot comment on a move, after it has played out
// on screen: 🐍 or 😭 when a bot is eaten, 🔥 on its ladder, 😂 when a
// person gets eaten. Rare enough to stay charming.
func (r *Room) botReact(m game.Move, after time.Duration) {
	mover := r.seats[m.Seat]
	if mover == nil || m.Kind == game.KindNone {
		return
	}
	var bots []int
	for _, i := range r.game.Seats {
		if s := r.seats[i]; s != nil && s.bot {
			bots = append(bots, i)
		}
	}
	if len(bots) == 0 {
		return
	}
	who, emoji := -1, ""
	switch {
	case mover.bot && m.Kind == game.KindSnake && rand.Float64() < 0.5:
		who, emoji = m.Seat, []string{"🐍", "😭"}[rand.IntN(2)]
	case mover.bot && m.Kind == game.KindLadder && rand.Float64() < 0.35:
		who, emoji = m.Seat, "🔥"
	case !mover.bot && m.Kind == game.KindSnake && rand.Float64() < 0.35:
		who, emoji = bots[rand.IntN(len(bots))], "😂"
	}
	if who < 0 {
		return
	}
	time.AfterFunc(after, func() {
		r.post(funcEv(func() {
			if s := r.seats[who]; s != nil && s.bot {
				r.broadcast(reactionMsg{T: "reaction", Seat: who, Emoji: emoji})
			}
		}))
	})
}
