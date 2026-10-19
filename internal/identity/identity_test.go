package identity

import (
	"path/filepath"
	"strings"
	"testing"
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

	bad := []string{"", "abc", id, id + ".", id + ".AAAAAAAAAAAAAAAAAAAAAA", "x" + tok[1:], tok + "x"}
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
