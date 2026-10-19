package stats

import (
	"path/filepath"
	"testing"
)

func TestRecordAndRank(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lb.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	game := func(winner string, rolls map[string]int) {
		t.Helper()
		var rs []Result
		for id, n := range rolls {
			pts := 3
			if id == winner {
				pts = 10
			}
			rs = append(rs, Result{PlayerID: id, Name: "name-" + id[:4], Won: id == winner, Rolls: n, Points: pts})
		}
		if err := s.Record(rs); err != nil {
			t.Fatal(err)
		}
	}
	game("aaaaaaaa", map[string]int{"aaaaaaaa": 30, "bbbbbbbb": 28})
	game("bbbbbbbb", map[string]int{"aaaaaaaa": 25, "bbbbbbbb": 40})
	game("aaaaaaaa", map[string]int{"aaaaaaaa": 22, "bbbbbbbb": 31, "cccccccc": 30})
	if err := s.Record([]Result{{PlayerID: "bad id!", Won: true}}); err != nil {
		t.Fatal(err)
	}

	top := s.Top(10)
	if len(top) != 3 {
		t.Fatalf("want 3 players, got %d", len(top))
	}
	if top[0].Name != "name-aaaa" || top[0].Wins != 2 || top[0].BestRolls != 22 || top[0].Games != 3 || top[0].Points != 23 {
		t.Fatalf("leader = %+v", top[0])
	}
	if top[1].Name != "name-bbbb" || top[2].Wins != 0 || top[2].BestRolls != 0 {
		t.Fatalf("order = %+v", top)
	}

	if me, ok := s.Rank("cccccccc"); !ok || me.Rank != 3 {
		t.Fatalf("Rank(c) = %+v %v", me, ok)
	}
	if _, ok := s.Rank("nobody00"); ok {
		t.Fatal("unknown player should not rank")
	}

	// Reload from disk.
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := s2.Get("aaaaaaaa"); !ok || r.Wins != 2 {
		t.Fatalf("reloaded = %+v %v", r, ok)
	}
}

func TestBotGamesKeptApart(t *testing.T) {
	s, _ := Open("")
	_ = s.Record([]Result{{PlayerID: "solo-player", Name: "Solo", Won: true, Rolls: 15, VsBots: true}})
	_ = s.Record([]Result{{PlayerID: "solo-player", Name: "Solo", Won: false, Rolls: 40, VsBots: true}})
	if top := s.Top(10); len(top) != 0 {
		t.Fatalf("bot-only player must not be ranked: %+v", top)
	}
	me, ok := s.Rank("solo-player")
	if !ok || me.Rank != 0 || me.BotGames != 2 || me.BotWins != 1 || me.Wins != 0 || me.BestRolls != 0 {
		t.Fatalf("bot record = %+v %v", me, ok)
	}
}
