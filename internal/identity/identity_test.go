package identity

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIssueVerify(t *testing.T) {
	s, err := LoadOrCreate(filepath.Join(t.TempDir(), "secret"))
	if err != nil {
		t.Fatal(err)
	}
	tok := s.Issue()
	id, ok := s.Verify(tok)
	if !ok || len(id) != 16 || !strings.HasPrefix(tok, id+".") {
		t.Fatalf("Verify(%q) = %q %v", tok, id, ok)
	}
	if s.Issue() == tok {
		t.Fatal("IDs must be unique")
	}

	other1 := "x"
	if tok[0] == 'x' {
		other1 = "y" // must actually differ from the real first character
	}
	bad := []string{"", "abc", id, id + ".", id + ".AAAAAAAAAAAAAAAAAAAAAA", other1 + tok[1:], tok + "x"}
	for _, b := range bad {
		if _, ok := s.Verify(b); ok {
			t.Errorf("Verify(%q) should fail", b)
		}
	}

	other, _ := New([]byte(strings.Repeat("k", 32)))
	if _, ok := other.Verify(tok); ok {
		t.Fatal("a different secret must not accept the token")
	}
	if _, err := New([]byte("short")); err == nil {
		t.Fatal("short secret must be rejected")
	}
}

func TestSecretPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	a, _ := LoadOrCreate(path)
	b, _ := LoadOrCreate(path)
	if _, ok := b.Verify(a.Issue()); !ok {
		t.Fatal("reloading the same secret must keep IDs valid")
	}
}

func TestRevokePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	s, _ := LoadOrCreate(path)
	tok := s.Issue()
	id, _ := s.Verify(tok)
	if err := s.Revoke(id); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Verify(tok); ok {
		t.Fatal("revoked ID must not verify")
	}
	s2, _ := LoadOrCreate(path)
	if _, ok := s2.Verify(tok); ok {
		t.Fatal("revocation must survive a restart")
	}
	if _, ok := s2.Verify(s2.Issue()); !ok {
		t.Fatal("new IDs still work")
	}
}

func TestTransfers(t *testing.T) {
	tr := NewTransfers()
	now := time.Now()
	tr.now = func() time.Time { return now }

	code, exp := tr.Create("tok-a")
	if len(code) != 6 || !exp.After(now) {
		t.Fatalf("code %q exp %v", code, exp)
	}
	if tok, ok := tr.Claim(code); !ok || tok != "tok-a" {
		t.Fatalf("claim = %q %v", tok, ok)
	}
	if _, ok := tr.Claim(code); ok {
		t.Fatal("a code works once")
	}

	first, _ := tr.Create("tok-b")
	second, _ := tr.Create("tok-b")
	if _, ok := tr.Claim(first); ok && first != second {
		t.Fatal("a new code replaces the old one")
	}
	now = now.Add(TransferTTL + time.Second)
	if _, ok := tr.Claim(second); ok {
		t.Fatal("expired code must not work")
	}
}
