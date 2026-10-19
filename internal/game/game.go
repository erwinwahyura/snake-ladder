// Package game holds the pure rules of ular tangga (snakes and ladders).
// It has no I/O and no clocks: the room layer decides when to roll and
// supplies the dice value.
package game

import (
	"errors"
	"sort"
)

// Size is the number of tiles on the board. Pawns start off-board at 0.
const Size = 100

// Board maps the start of every snake or ladder to where it ends.
// A jump to a higher tile is a ladder; a jump to a lower tile is a snake.
type Board struct {
	Jumps map[int]int
}

// Jump is one snake or ladder, for sending to clients.
type Jump struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// Classic returns the familiar layout from the Milton Bradley board.
func Classic() Board {
	return Board{Jumps: map[int]int{
		// ladders
		4: 14, 9: 31, 21: 42, 28: 84, 51: 67, 72: 91, 80: 99,
		// snakes
		17: 7, 54: 34, 62: 19, 64: 60, 87: 24, 93: 73, 95: 75, 98: 79,
	}}
}

// Ladders lists the ladders, sorted by start tile.
func (b Board) Ladders() []Jump { return b.list(func(from, to int) bool { return to > from }) }

// Snakes lists the snakes, sorted by head tile.
func (b Board) Snakes() []Jump { return b.list(func(from, to int) bool { return to < from }) }

func (b Board) list(keep func(from, to int) bool) []Jump {
	out := []Jump{}
	for from, to := range b.Jumps {
		if keep(from, to) {
			out = append(out, Jump{From: from, To: to})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].From < out[j].From })
	return out
}

// Kind of jump at the end of a move.
const (
	KindNone   = ""
	KindLadder = "ladder"
	KindSnake  = "snake"
)

// Move describes one roll from start to finish so clients can animate it.
type Move struct {
	Seat    int    `json:"seat"`
	Dice    int    `json:"dice"`
	From    int    `json:"from"`
	Landed  int    `json:"landed"`  // tile reached by walking, after any bounce
	To      int    `json:"to"`      // final tile after a snake or ladder
	Kind    string `json:"kind"`    // "", "ladder" or "snake"
	Bounced bool   `json:"bounced"` // overshot 100 and walked back
	Stuck   bool   `json:"stuck"`   // overshot 100 and stayed put
	Again   bool   `json:"again"`   // same player rolls again
	Won     bool   `json:"won"`
}

// Steps is how many tiles the pawn walks, used for animation timing.
func (m Move) Steps() int {
	if m.Stuck {
		return 0
	}
	return m.Dice
}

// Overshoot rules: what happens when a roll would go past 100.
const (
	OvershootStay   = "stay"   // pawn does not move (classic exact-roll rule)
	OvershootBounce = "bounce" // pawn walks to 100 and back by the excess
)

// Rules are the house rules a room plays with.
type Rules struct {
	Overshoot string `json:"overshoot"` // OvershootStay or OvershootBounce
	SixAgain  bool   `json:"sixAgain"`  // rolling a 6 gives another roll
}

// DefaultRules: exact roll to finish (no bounce) and a 6 rolls again,
// the way it is usually played at home in Indonesia.
func DefaultRules() Rules { return Rules{Overshoot: OvershootStay, SixAgain: true} }

// Valid reports whether the rules are known values.
func (r Rules) Valid() bool { return r.Overshoot == OvershootStay || r.Overshoot == OvershootBounce }

// Apply works out where a pawn on pos ends up after rolling dice.
func Apply(b Board, r Rules, pos, dice int) Move {
	m := Move{Dice: dice, From: pos}
	landed := pos + dice
	if landed > Size {
		if r.Overshoot == OvershootBounce {
			landed = Size - (landed - Size)
			m.Bounced = true
		} else {
			m.Stuck = true
			m.Landed, m.To = pos, pos
			return m
		}
	}
	m.Landed, m.To = landed, landed
	if to, ok := b.Jumps[landed]; ok {
		m.To = to
		if to > landed {
			m.Kind = KindLadder
		} else {
			m.Kind = KindSnake
		}
	}
	m.Won = m.To == Size
	return m
}

// Errors returned by Game.Roll.
var (
	ErrFinished = errors.New("game is finished")
	ErrBadDice  = errors.New("dice must be 1..6")
)

// Game is the state of one match. Seats are the table positions (0..3)
// taking part, in turn order.
type Game struct {
	Board  Board
	Rules  Rules
	Seats  []int
	Pos    map[int]int
	Stats  map[int]*Stats
	turn   int // index into Seats
	Winner int // seat, or -1
}

// Stats are per-seat counters for one game, fed into the leaderboard.
type Stats struct {
	Rolls   int `json:"rolls"`
	Ladders int `json:"ladders"`
	Snakes  int `json:"snakes"`
}

// New starts a game for the given seats, in turn order.
func New(b Board, r Rules, seats []int) *Game {
	g := &Game{Board: b, Rules: r, Seats: append([]int(nil), seats...), Pos: map[int]int{}, Stats: map[int]*Stats{}, Winner: -1}
	for _, s := range seats {
		g.Pos[s] = 0
		g.Stats[s] = &Stats{}
	}
	return g
}

// Current is the seat whose turn it is, or -1 once the game is won.
func (g *Game) Current() int {
	if g.Winner >= 0 || len(g.Seats) == 0 {
		return -1
	}
	return g.Seats[g.turn]
}

// Roll applies a dice value for the current seat and advances the turn.
func (g *Game) Roll(dice int) (Move, error) {
	if g.Winner >= 0 {
		return Move{}, ErrFinished
	}
	if dice < 1 || dice > 6 {
		return Move{}, ErrBadDice
	}
	seat := g.Current()
	m := Apply(g.Board, g.Rules, g.Pos[seat], dice)
	m.Seat = seat
	g.Pos[seat] = m.To
	st := g.Stats[seat]
	st.Rolls++
	switch m.Kind {
	case KindLadder:
		st.Ladders++
	case KindSnake:
		st.Snakes++
	}
	switch {
	case m.Won:
		g.Winner = seat
	case g.Rules.SixAgain && dice == 6:
		m.Again = true
	default:
		g.turn = (g.turn + 1) % len(g.Seats)
	}
	return m, nil
}
