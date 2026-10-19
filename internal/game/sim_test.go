package game

import (
	"math/rand/v2"
	"testing"
)

// soloRolls plays one pawn to 100 and counts the rolls.
func soloRolls(r Rules, rng *rand.Rand) int {
	g := New(Classic(), Rules{Overshoot: r.Overshoot}, []int{0})
	for n := 1; ; n++ {
		m, _ := g.Roll(rng.IntN(6) + 1)
		if m.Won {
			return n
		}
	}
}

// TestExpectedLength checks the rules against published maths: on the
// classic board with the exact-roll rule, one player needs about 39.2
// rolls on average (see docs/RESEARCH.md). If this drifts, the rules or the
// board changed.
func TestExpectedLength(t *testing.T) {
	if testing.Short() {
		t.Skip("simulation")
	}
	rng := rand.New(rand.NewPCG(1, 2))
	const games = 40000
	mean := func(r Rules) float64 {
		total := 0
		for i := 0; i < games; i++ {
			total += soloRolls(r, rng)
		}
		return float64(total) / games
	}
	stay := mean(Rules{Overshoot: OvershootStay})
	bounce := mean(Rules{Overshoot: OvershootBounce})
	t.Logf("mean solo rolls: stay=%.1f bounce=%.1f", stay, bounce)
	if stay < 38.2 || stay > 40.2 {
		t.Errorf("stay mean %.1f, want ~39.2", stay)
	}
	if bounce < stay*1.3 {
		t.Errorf("bounce mean %.1f should be clearly longer than stay %.1f on this board", bounce, stay)
	}
}
