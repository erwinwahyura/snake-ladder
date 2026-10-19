package names

import "testing"

func TestOffensive(t *testing.T) {
	bad := []string{"anjing", "ANJINGG", "4nj1ng", "a n j i n g", "a.n.j.i.n.g", "si bangsat", "FUCK", "fuuuck", "asu", "Tai lo", "jancuk", "k0nt0l", "cuk"}
	for _, n := range bad {
		if !Offensive(n) {
			t.Errorf("%q should be caught", n)
		}
	}
	good := []string{"Budi", "Sari", "Pantai Kuta", "Masuk", "Santai", "Cukup", "Tabib", "Asep", "Dika", "Bot Ucok", "Farras", "Erwin"}
	for _, n := range good {
		if Offensive(n) {
			t.Errorf("%q is a normal name", n)
		}
	}
}
