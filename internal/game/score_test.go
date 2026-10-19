package game

import "testing"

func TestPlacements(t *testing.T) {
	g := New(Classic(), DefaultRules(), []int{0, 1, 2, 3})
	g.Pos = map[int]int{0: 40, 1: 100, 2: 75, 3: 40}
	g.Winner = 1
	got := g.Placements()
	want := []Place{{1, 1, 10}, {2, 2, 6}, {0, 3, 3}, {3, 3, 3}} // 0 and 3 tie on 40
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("placements = %+v, want %+v", got, want)
		}
	}

	g2 := New(Classic(), DefaultRules(), []int{0, 2})
	g2.Pos = map[int]int{0: 100, 2: 99}
	g2.Winner = 0
	if p := g2.Placements(); p[0].Points != 10 || p[1].Points != 3 {
		t.Fatalf("2-player points = %+v", p)
	}
}
