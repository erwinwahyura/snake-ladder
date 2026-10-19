package identity

import (
	"math/rand/v2"
	"sync"
	"time"
)

// Transfers hands an identity to another device with a short code, instead
// of a permanent link that would work for anyone who ever saw it.
// A code lasts TransferTTL, works once, and a new one replaces the old.
type Transfers struct {
	mu    sync.Mutex
	codes map[string]transfer
	byPID map[string]string // token → its current code
	now   func() time.Time
}

type transfer struct {
	token   string
	expires time.Time
}

// TransferTTL is how long a transfer code works.
const TransferTTL = 10 * time.Minute

// Six characters without look-alikes: about 10⁹ codes, and each lives ten
// minutes, so guessing one is hopeless.
const transferAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// NewTransfers returns an empty code book.
func NewTransfers() *Transfers {
	return &Transfers{codes: map[string]transfer{}, byPID: map[string]string{}, now: time.Now}
}

// Create makes a code for a (verified) token.
func (t *Transfers) Create(token string) (string, time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep()
	if old, ok := t.byPID[token]; ok {
		delete(t.codes, old)
	}
	code := newTransferCode()
	for _, taken := t.codes[code]; taken; _, taken = t.codes[code] {
		code = newTransferCode()
	}
	exp := t.now().Add(TransferTTL)
	t.codes[code] = transfer{token: token, expires: exp}
	t.byPID[token] = code
	return code, exp
}

// Claim exchanges a code for its token, once.
func (t *Transfers) Claim(code string) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep()
	tr, ok := t.codes[code]
	if !ok {
		return "", false
	}
	delete(t.codes, code)
	delete(t.byPID, tr.token)
	return tr.token, true
}

func (t *Transfers) sweep() {
	now := t.now()
	for code, tr := range t.codes {
		if now.After(tr.expires) {
			delete(t.codes, code)
			delete(t.byPID, tr.token)
		}
	}
}

func newTransferCode() string {
	b := make([]byte, 6)
	for i := range b {
		b[i] = transferAlphabet[rand.IntN(len(transferAlphabet))]
	}
	return string(b)
}
