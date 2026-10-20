package identity

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Accounts links sign-in providers to player IDs. Only the provider's
// stable subject ID is stored: no email, no name.
type Accounts struct {
	path   string
	mu     sync.Mutex
	google map[string]string // Google "sub" → player ID
	byID   map[string]string // player ID → Google "sub"
}

type accountsFile struct {
	Google map[string]string `json:"google"`
}

// ErrAlreadyLinked: this player ID is linked to a different Google account.
var ErrAlreadyLinked = errors.New("player already linked to another account")

// OpenAccounts loads the store; an empty path keeps it in memory.
func OpenAccounts(path string) (*Accounts, error) {
	a := &Accounts{path: path, google: map[string]string{}, byID: map[string]string{}}
	if path == "" {
		return a, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	var f accountsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	for sub, id := range f.Google {
		a.google[sub] = id
		a.byID[id] = sub
	}
	return a, nil
}

// Google returns the player ID linked to a Google account.
func (a *Accounts) Google(sub string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id, ok := a.google[sub]
	return id, ok
}

// Linked reports whether a player ID has a sign-in attached.
func (a *Accounts) Linked(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.byID[id]
	return ok
}

// LinkGoogle attaches a Google account to a player ID.
func (a *Accounts) LinkGoogle(sub, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if other, ok := a.byID[id]; ok && other != sub {
		return ErrAlreadyLinked
	}
	if old, ok := a.google[sub]; ok {
		delete(a.byID, old)
	}
	a.google[sub] = id
	a.byID[id] = sub
	return a.save()
}

// Move follows a player to a new ID ("Ganti kunci").
func (a *Accounts) Move(from, to string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	sub, ok := a.byID[from]
	if !ok {
		return nil
	}
	delete(a.byID, from)
	a.byID[to] = sub
	a.google[sub] = to
	return a.save()
}

func (a *Accounts) save() error {
	if a.path == "" {
		return nil
	}
	b, err := json.Marshal(accountsFile{Google: a.google})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(a.path), ".accounts-*")
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
	return os.Rename(tmp.Name(), a.path)
}
