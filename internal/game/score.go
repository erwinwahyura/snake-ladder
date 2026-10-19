package game

import "sort"

// Points for each finishing place, by number of players at the table.
// A win is worth 10 at any size. The other places earn less, but always
// something, so finishing a game is never worth nothing. Snakes and
// ladders is pure luck, so there are no bonuses for style or speed.
var pointsTable = map[int][]int{
	2: {10, 3},
	3: {10, 5, 2},
	4: {10, 6, 3, 1},
}

// Place is one seat's finishing place (1 = winner) and the points it earns.
type Place struct {
	Seat   int
	Place  int
	Points int
}

// Placements ranks every seat when the game ends: the winner first, then
// by how far along the board each pawn got. Equal squares share a place.
func (g *Game) Placements() []Place {
	seats := append([]int(nil), g.Seats...)
	sort.SliceStable(seats, func(i, j int) bool {
		a, b := seats[i], seats[j]
		if (a == g.Winner) != (b == g.Winner) {
			return a == g.Winner
		}
		return g.Pos[a] > g.Pos[b]
	})
	table := pointsTable[len(seats)]
	out := make([]Place, len(seats))
	for i, s := range seats {
		place := i + 1
		if i > 0 && s != g.Winner && g.Pos[s] == g.Pos[seats[i-1]] && seats[i-1] != g.Winner {
			place = out[i-1].Place
		}
		pts := 0
		if place-1 < len(table) {
			pts = table[place-1]
		}
		out[i] = Place{Seat: s, Place: place, Points: pts}
	}
	return out
}
