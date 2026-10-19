package room

import (
	"math/rand/v2"
	"time"

	"github.com/erwinwahyura/snake-ladder/internal/game"
)

// Table setup: how many seats, who can find the table, and bots.

// Listing is a public table as shown under "Meja terbuka".
type Listing struct {
	Code    string     `json:"code"`
	Host    string     `json:"host"`
	Size    int        `json:"size"`
	Humans  int        `json:"humans"`
	Bots    int        `json:"bots"` // a person joining takes a bot's seat
	Rules   game.Rules `json:"rules"`
	created time.Time
}

// botNames are picked in order, skipping any already at the table.
var botNames = []string{"Bot Ucok", "Bot Siti", "Bot Joko", "Bot Dewi", "Bot Asep", "Bot Rina"}

// openSeats are the seat numbers in use for the table size. Two players
// sit opposite each other, like at a real table.
func openSeatsFor(size int) []int {
	switch size {
	case 2:
		return []int{0, 2}
	case 3:
		return []int{0, 1, 2}
	default:
		return []int{0, 1, 2, 3}
	}
}

func (r *Room) openSeats() []int { return openSeatsFor(r.size) }

func (r *Room) isHost(c *client) bool {
	return c.seat >= 0 && r.seats[c.seat] != nil && r.seats[c.seat].host
}

// setupAllowed reports whether the host may change the table now.
func (r *Room) setupAllowed(c *client) bool {
	switch {
	case !r.isHost(c):
		r.sendErr(c, "not_host")
	case r.phase == PhasePlaying:
		r.sendErr(c, "already_playing")
	default:
		return true
	}
	return false
}

func (r *Room) onConfig(c *client, size *int, public *bool) {
	if !r.setupAllowed(c) {
		return
	}
	if size != nil {
		if *size < 2 || *size > Seats {
			r.sendErr(c, "bad_size")
			return
		}
		keep := map[int]bool{}
		for _, i := range openSeatsFor(*size) {
			keep[i] = true
		}
		// A connected person in a seat that would disappear blocks the
		// change; bots and people who already left just make room.
		for i, s := range r.seats {
			if s != nil && !keep[i] && !s.bot && s.client != nil {
				r.sendErr(c, "seats_taken")
				return
			}
		}
		for i := range r.seats {
			if !keep[i] {
				r.seats[i] = nil
			}
		}
		r.size = *size
	}
	if public != nil {
		r.public = *public
	}
	r.broadcastState()
}

func (r *Room) onBot(c *client, action string, seat *int) {
	if !r.setupAllowed(c) {
		return
	}
	switch action {
	case "add":
		if !r.addBot() {
			r.sendErr(c, "no_free_seat")
			return
		}
	case "fill":
		for r.addBot() {
		}
	case "remove":
		if seat == nil || *seat < 0 || *seat >= Seats || r.seats[*seat] == nil || !r.seats[*seat].bot {
			r.sendErr(c, "bad_seat")
			return
		}
		r.seats[*seat] = nil
	default:
		r.sendErr(c, "bad_message")
		return
	}
	r.broadcastState()
}

// addBot seats a bot in the first empty seat. It reports false when the
// table is full.
func (r *Room) addBot() bool {
	for _, i := range r.openSeats() {
		if r.seats[i] == nil {
			r.seats[i] = &seat{name: r.nextBotName(), bot: true}
			return true
		}
	}
	return false
}

func (r *Room) nextBotName() string {
	used := map[string]bool{}
	for _, s := range r.seats {
		if s != nil {
			used[s.name] = true
		}
	}
	for _, n := range botNames {
		if !used[n] {
			return n
		}
	}
	return "Bot"
}

// botThink is how long a bot "thinks" before rolling, with some jitter so
// a table of bots doesn't tick like a metronome.
func (r *Room) botThink() time.Duration {
	t := r.cfg.Timing.BotThink
	if t <= 0 {
		return 0
	}
	return t + time.Duration(rand.Int64N(int64(t)/2+1))
}

// publishListing updates the room's public entry. A table is listed only
// while it is public, not mid-game, and has a seat a person could take.
func (r *Room) publishListing() {
	if !r.public || r.delisted || r.phase == PhasePlaying {
		r.listing.Store(nil)
		return
	}
	l := Listing{Code: r.code, Size: r.size, Rules: r.rules, created: r.created}
	free := 0
	for _, i := range r.openSeats() {
		s := r.seats[i]
		switch {
		case s == nil:
			free++
		case s.bot:
			l.Bots++
		case s.client != nil:
			l.Humans++
			if s.host {
				l.Host = s.name
			}
		default:
			free++ // left the lobby; a newcomer reclaims it
		}
	}
	if l.Humans == 0 || free+l.Bots == 0 {
		r.listing.Store(nil)
		return
	}
	r.listing.Store(&l)
}
