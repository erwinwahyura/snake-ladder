// Package social keeps "Teman main" (people you have shared a table with),
// knows who is online, and delivers invites to them.
//
// There are no accounts or friend requests: sitting down to a game with
// someone makes you each other's teman. That matches how the game is
// played (with people you already know, via a WhatsApp link) and keeps the
// list to people who have already chosen to play together.
package social

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// MaxFriends caps each list; the least recently played drop off.
const MaxFriends = 50

// Friend is one entry in someone's list.
type Friend struct {
	Name       string    `json:"name"`
	LastPlayed time.Time `json:"lastPlayed"`
}

// Friends is a JSON-file store: player ID → friend ID → Friend.
type Friends struct {
	path string
	mu   sync.Mutex
	m    map[string]map[string]*Friend
	now  func() time.Time
}

// OpenFriends loads the store; an empty path keeps it in memory.
func OpenFriends(path string) (*Friends, error) {
	f := &Friends{path: path, m: map[string]map[string]*Friend{}, now: time.Now}
	if path == "" {
		return f, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &f.m); err != nil {
		return nil, err
	}
	return f, nil
}

// Met records that these players (ID → name) sat down to a game together.
func (f *Friends) Met(players map[string]string) error {
	if len(players) < 2 {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	for a := range players {
		for b, name := range players {
			if a == b {
				continue
			}
			list := f.m[a]
			if list == nil {
				list = map[string]*Friend{}
				f.m[a] = list
			}
			list[b] = &Friend{Name: name, LastPlayed: now}
			f.trim(list)
		}
	}
	return f.save()
}

func (f *Friends) trim(list map[string]*Friend) {
	for len(list) > MaxFriends {
		var oldest string
		for id, fr := range list {
			if oldest == "" || fr.LastPlayed.Before(list[oldest].LastPlayed) {
				oldest = id
			}
		}
		delete(list, oldest)
	}
}

// Entry is a friend with their ID, newest first from List.
type Entry struct {
	ID string `json:"id"`
	Friend
}

// List returns someone's friends, most recently played first.
func (f *Friends) List(id string) []Entry {
	f.mu.Lock()
	out := make([]Entry, 0, len(f.m[id]))
	for fid, fr := range f.m[id] {
		out = append(out, Entry{ID: fid, Friend: *fr})
	}
	f.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].LastPlayed.After(out[j].LastPlayed) })
	return out
}

// Has reports whether fid is in id's list.
func (f *Friends) Has(id, fid string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.m[id][fid]
	return ok
}

// Remove takes fid off id's list (one direction only: they keep you).
func (f *Friends) Remove(id, fid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.m[id][fid]; !ok {
		return nil
	}
	delete(f.m[id], fid)
	return f.save()
}

// Move renames a player everywhere after "Ganti kunci".
func (f *Friends) Move(from, to string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if list, ok := f.m[from]; ok {
		f.m[to] = list
		delete(f.m, from)
	}
	for _, list := range f.m {
		if fr, ok := list[from]; ok {
			list[to] = fr
			delete(list, from)
		}
	}
	return f.save()
}

// save writes atomically; called with f.mu held.
func (f *Friends) save() error {
	if f.path == "" {
		return nil
	}
	b, err := json.Marshal(f.m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.path), ".friends-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.path)
}
