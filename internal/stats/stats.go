// Package stats keeps the all-time leaderboard in a small JSON file.
//
// Players have no accounts: each browser makes a random player ID and
// keeps it in localStorage. That is enough for a game among friends, and
// it means a rename keeps your record. See docs/REVIEW.md for the limits.
package stats

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// Record is one player's lifetime numbers.
type Record struct {
	ID         string    `json:"-"`
	Name       string    `json:"name"`
	Points     int       `json:"points"` // from finishing places, see game.Placements
	Games      int       `json:"games"`
	Wins       int       `json:"wins"`
	BestRolls  int       `json:"bestRolls"` // fewest rolls in a won game, 0 if never won
	Rolls      int       `json:"rolls"`
	Ladders    int       `json:"ladders"`
	Snakes     int       `json:"snakes"`
	BotGames   int       `json:"botGames"` // games against bots only, kept apart from the ranking
	BotWins    int       `json:"botWins"`
	LastPlayed time.Time `json:"lastPlayed"`
}

// Result is one player's line from a finished game.
type Result struct {
	PlayerID string
	Name     string
	Won      bool
	Rolls    int
	Ladders  int
	Snakes   int
	Points   int  // for the finishing place
	VsBots   bool // the only other players were bots
}

// Recorder is what the room layer needs; Store implements it.
type Recorder interface {
	Record(results []Result) error
}

// Entry is a leaderboard row as sent to clients.
type Entry struct {
	Rank int `json:"rank"`
	Record
	WinRate float64 `json:"winRate"`
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// ValidID reports whether a client-supplied player ID is acceptable.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// Store is a mutex-guarded map persisted to one JSON file.
type Store struct {
	path string
	mu   sync.Mutex
	recs map[string]*Record
	now  func() time.Time
}

// Open loads the file at path, or starts empty if it does not exist.
// An empty path keeps everything in memory (used in tests).
func Open(path string) (*Store, error) {
	s := &Store{path: path, recs: map[string]*Record{}, now: time.Now}
	if path == "" {
		return s, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var file map[string]*Record
	if err := json.Unmarshal(b, &file); err != nil {
		return nil, err
	}
	for id, r := range file {
		r.ID = id
		s.recs[id] = r
	}
	return s, nil
}

// Record adds a finished game. Players without a valid ID are skipped.
func (s *Store) Record(results []Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for _, res := range results {
		if !ValidID(res.PlayerID) {
			continue
		}
		r := s.recs[res.PlayerID]
		if r == nil {
			r = &Record{ID: res.PlayerID}
			s.recs[res.PlayerID] = r
		}
		r.Name = res.Name
		r.LastPlayed = now
		if res.VsBots {
			r.BotGames++
			if res.Won {
				r.BotWins++
			}
			continue
		}
		r.Games++
		r.Points += res.Points
		r.Rolls += res.Rolls
		r.Ladders += res.Ladders
		r.Snakes += res.Snakes
		r.LastPlayed = now
		if res.Won {
			r.Wins++
			if r.BestRolls == 0 || res.Rolls < r.BestRolls {
				r.BestRolls = res.Rolls
			}
		}
	}
	return s.save()
}

// Top returns the best n players: most points, then most wins, then best
// win rate, then fewest rolls in a winning game.
func (s *Store) Top(n int) []Entry {
	out := s.ranked()
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// Rank returns one player's leaderboard row, wherever they are in it.
// Someone who has only played bots gets Rank 0 and their bot record.
func (s *Store) Rank(id string) (Entry, bool) {
	for _, e := range s.ranked() {
		if e.ID == id {
			return e, true
		}
	}
	if r, ok := s.Get(id); ok {
		return Entry{Record: r}, true
	}
	return Entry{}, false
}

func (s *Store) ranked() []Entry {
	s.mu.Lock()
	out := make([]Entry, 0, len(s.recs))
	for _, r := range s.recs {
		if r.Games == 0 {
			continue // bot games only: not on the ranking
		}
		e := Entry{Record: *r}
		if r.Games > 0 {
			e.WinRate = float64(r.Wins) / float64(r.Games)
		}
		out = append(out, e)
	}
	s.mu.Unlock()

	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Points != b.Points {
			return a.Points > b.Points
		}
		if a.Wins != b.Wins {
			return a.Wins > b.Wins
		}
		if a.WinRate != b.WinRate {
			return a.WinRate > b.WinRate
		}
		if a.BestRolls != b.BestRolls {
			return a.BestRolls != 0 && (b.BestRolls == 0 || a.BestRolls < b.BestRolls)
		}
		if !a.LastPlayed.Equal(b.LastPlayed) {
			return a.LastPlayed.After(b.LastPlayed)
		}
		return a.ID < b.ID
	})
	for i := range out {
		out[i].Rank = i + 1
	}
	return out
}

// Get returns one player's record, if any.
func (s *Store) Get(id string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.recs[id]
	if !ok {
		return Record{}, false
	}
	return *r, true
}

// save writes the whole file atomically: temp file, fsync, rename.
// Called with s.mu held.
func (s *Store) save() error {
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.recs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".leaderboard-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}
