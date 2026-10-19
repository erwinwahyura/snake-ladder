// Package names checks player names before they are shown to strangers,
// for example in the public table list.
//
// It catches common Indonesian and English swear words, including the
// usual disguises: l33t digits, repeated letters, and spaces or dots
// between letters. It is a small, readable list, not a complete filter:
// the report button in Meja terbuka covers what it misses.
package names

import (
	"strings"
	"unicode"
)

// blocked are matched anywhere in the squashed name.
var blocked = []string{
	// Indonesian
	"anjing", "anjeng", "anjir", "bangsat", "bajingan", "kontol", "memek", "ngentot", "entot",
	"jancok", "jancuk", "cuk", "goblok", "tolol", "pepek", "perek", "lonte", "pelacur",
	"kampret", "titit", "bego", "idiot", "keparat", "brengsek", "setan",
	// English
	"fuck", "shit", "bitch", "cunt", "dick", "pussy", "whore", "slut", "asshole", "bastard",
	"nigger", "nigga", "faggot", "retard",
}

// blockedWords are too short to match inside other words ("tai" is in
// "pantai", "asu" in "masuk"), so they must be a whole word on their own.
var blockedWords = []string{"asu", "tai", "babi", "kntl", "mmk", "fck", "ngt", "jnck", "bgst"}

var leet = map[rune]rune{'0': 'o', '1': 'i', '3': 'e', '4': 'a', '5': 's', '7': 't', '8': 'b', '@': 'a', '$': 's', '!': 'i'}

// Offensive reports whether a name contains a blocked word.
func Offensive(name string) bool {
	words, squashed := normalize(name)
	for _, b := range blocked {
		if b == "cuk" { // only as a word: "cuk" sits inside "cukup"
			for _, w := range words {
				if w == b {
					return true
				}
			}
			continue
		}
		if strings.Contains(squashed, b) {
			return true
		}
	}
	for _, w := range words {
		for _, b := range blockedWords {
			if w == b {
				return true
			}
		}
	}
	return false
}

// normalize lowercases, undoes l33t, drops repeated letters, and returns
// both the separate words and everything squashed together (which catches
// "a n j i n g" and "a.n.j.i.n.g").
func normalize(s string) (words []string, squashed string) {
	var cur, all strings.Builder
	var last rune
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	for _, r := range strings.ToLower(s) {
		if m, ok := leet[r]; ok {
			r = m
		}
		if !unicode.IsLetter(r) {
			flush()
			last = 0
			continue
		}
		if r == last {
			continue
		}
		last = r
		cur.WriteRune(r)
		all.WriteRune(r)
	}
	flush()
	return words, all.String()
}
