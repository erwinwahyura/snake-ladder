package game

import "testing"

func TestApply(t *testing.T) {
	b := Classic()
	tests := []struct {
		name       string
		pos, dice  int
		landed, to int
		kind       string
		bounced    bool
		won        bool
	}{
		{"enter board", 0, 3, 3, 3, KindNone, false, false},
		{"ladder from start", 0, 4, 4, 14, KindLadder, false, false},
		{"snake", 12, 5, 17, 7, KindSnake, false, false},
		{"plain step", 40, 2, 42, 42, KindNone, false, false},
		{"exact win", 97, 3, 100, 100, KindNone, false, true},
		{"ladder to 99 is not a win", 77, 3, 80, 99, KindLadder, false, false},
	}
	stay := Rules{Overshoot: OvershootStay}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Apply(b, stay, tt.pos, tt.dice)
			if m.Landed != tt.landed || m.To != tt.to || m.Kind != tt.kind || m.Bounced != tt.bounced || m.Won != tt.won {
				t.Fatalf("Apply(%d,%d) = %+v", tt.pos, tt.dice, m)
			}
		})
	}
}

func TestOvershoot(t *testing.T) {
	b := Classic()
	m := Apply(b, Rules{Overshoot: OvershootStay}, 97, 5)
	if !m.Stuck || m.To != 97 || m.Steps() != 0 {
		t.Fatalf("stay rule: %+v", m)
	}
	m = Apply(b, Rules{Overshoot: OvershootBounce}, 97, 5) // 102 -> 98, snake to 79
	if !m.Bounced || m.Landed != 98 || m.To != 79 || m.Kind != KindSnake {
		t.Fatalf("bounce rule into snake: %+v", m)
	}
	m = Apply(b, Rules{Overshoot: OvershootBounce}, 99, 4)
	if m.To != 97 || m.Steps() != 4 {
		t.Fatalf("bounce plain: %+v", m)
	}
}

func TestSixAgain(t *testing.T) {
	g := New(Classic(), Rules{Overshoot: OvershootStay, SixAgain: true}, []int{0, 1})
	m, _ := g.Roll(6)
	if !m.Again || g.Current() != 0 {
		t.Fatalf("6 should roll again: %+v current=%d", m, g.Current())
	}
	m, _ = g.Roll(2)
	if m.Again || g.Current() != 1 {
		t.Fatalf("turn should pass after non-6: %+v current=%d", m, g.Current())
	}
	if g.Stats[0].Rolls != 2 {
		t.Fatalf("rolls = %d", g.Stats[0].Rolls)
	}
}

func TestClassicBoardIsSane(t *testing.T) {
	b := Classic()
	for from, to := range b.Jumps {
		if from <= 1 || from >= Size || to < 1 || to > Size || from == to {
			t.Errorf("bad jump %d->%d", from, to)
		}
		if _, chained := b.Jumps[to]; chained {
			t.Errorf("jump %d->%d ends on another jump", from, to)
		}
	}
	if len(b.Ladders())+len(b.Snakes()) != len(b.Jumps) {
		t.Fatal("ladders+snakes must cover all jumps")
	}
}

func TestTurnsAndWin(t *testing.T) {
	g := New(Classic(), Rules{Overshoot: OvershootStay}, []int{0, 2, 3})
	if g.Current() != 0 {
		t.Fatalf("first turn = %d", g.Current())
	}
	if _, err := g.Roll(7); err != ErrBadDice {
		t.Fatalf("want ErrBadDice, got %v", err)
	}
	mustRoll := func(d int) Move {
		t.Helper()
		m, err := g.Roll(d)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	if m := mustRoll(2); m.Seat != 0 || g.Pos[0] != 2 {
		t.Fatalf("seat 0 move %+v", m)
	}
	if g.Current() != 2 {
		t.Fatalf("turn should pass to seat 2, got %d", g.Current())
	}
	mustRoll(1)
	mustRoll(1)
	if g.Current() != 0 {
		t.Fatalf("turn should wrap to seat 0, got %d", g.Current())
	}

	g.Pos[0] = 96
	m := mustRoll(4)
	if !m.Won || g.Winner != 0 || g.Current() != -1 {
		t.Fatalf("expected win, got %+v winner=%d", m, g.Winner)
	}
	if _, err := g.Roll(1); err != ErrFinished {
		t.Fatalf("want ErrFinished, got %v", err)
	}
}
