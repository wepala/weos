// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

// Package lexical holds the rules both lexical index engines share for an
// any-word search: what a content word of a query is, and how many of a
// query's content words a text holds. It has no engine in it; each engine
// matches the words this package hands it in its own query language.
package lexical

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// MaxContentWords caps how many content words one any-word search matches.
// It is the support hub's maxQueryTerms (services/weos-support
// support/answers.go), so the hub and core score the same words of a long
// question.
const MaxContentWords = 8

// ContentWords returns the content words of a query, in the order they
// first appear: the words a person wrote, lower-cased, with stop words and
// single letters dropped, each once, at most MaxContentWords.
//
// A word is a run of letters and digits. A single '-', '.' or '@' between two
// letters or digits joins the run, so "follow-up", "dana@example.com" and
// "v3.2" are each one word, matched by each engine as its own tokenizer reads
// it. An apostrophe always splits, so "can't" is "can" + "t" — the split both
// engines make when they index a text — and both fragments are stop words, so
// a contraction is dropped whole rather than shortened to "can". Every other
// character (whitespace, quotes, and every query operator of either engine)
// separates words and is never part of one, so the words are safe to hand to
// an engine as plain text.
//
// A query with no content words returns nil. An any-word search over nil
// matches nothing; it never matches everything.
func ContentWords(query string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range words(strings.ToLower(query)) {
		if len([]rune(w)) < 2 || stopWords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
		if len(out) == MaxContentWords {
			break
		}
	}
	return out
}

// IsStopWord reports whether a lower-case word is on the stop list.
func IsStopWord(word string) bool { return stopWords[word] }

// Floor is the fewest content words a hit must hold for a MinMatch over a
// query of n content words: MinMatch, with 0 (and anything below it) meaning
// one word, capped at n so a floor above the query's word count asks for
// every word rather than for the impossible.
func Floor(minMatch, n int) int {
	if minMatch < 1 {
		minMatch = 1
	}
	if minMatch > n {
		minMatch = n
	}
	return minMatch
}

// isWordRune reports whether r belongs inside a word. Nonspacing marks are
// kept with the letter they decorate.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Co, r)
}

// isJoiner reports whether r joins two runs of letters or digits into one word.
func isJoiner(r rune) bool { return r == '-' || r == '.' || r == '@' }

// words splits text into words by the rule ContentWords states.
func words(text string) []string {
	rs := []rune(text)
	var out []string
	start := -1
	for i, r := range rs {
		switch {
		case isWordRune(r):
			if start < 0 {
				start = i
			}
		case start >= 0 && isJoiner(r) && i+1 < len(rs) && isWordRune(rs[i+1]) && isWordRune(rs[i-1]):
			// A single joiner between two word characters stays in the word.
		default:
			if start >= 0 {
				out = append(out, string(rs[start:i]))
				start = -1
			}
		}
	}
	if start >= 0 {
		out = append(out, string(rs[start:]))
	}
	return out
}

// Tokens splits text the way SQLite FTS5's default unicode61 tokenizer does:
// lower-cased, diacritics removed, at every character that is not a letter,
// a number or a private-use character. Coverage uses it to read a stored text
// as FTS5 indexed it.
func Tokens(text string) []string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(text)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.FieldsFunc(b.String(), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.Is(unicode.Co, r)
	})
}

// Coverage counts how many of a query's content words a text holds. A word
// is held when its tokens appear in the text's tokens one after another — the
// rule an FTS5 phrase follows — so a joined word such as "follow-up" is held
// by "Follow-up" and not by a text that only says "follow".
type Coverage struct {
	words [][]string
}

// NewCoverage prepares the content words for counting.
func NewCoverage(contentWords []string) *Coverage {
	c := &Coverage{}
	for _, w := range contentWords {
		if t := Tokens(w); len(t) > 0 {
			c.words = append(c.words, t)
		}
	}
	return c
}

// Count returns the number of distinct content words the text holds.
func (c *Coverage) Count(text string) int {
	tokens := Tokens(text)
	held := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		held[t] = true
	}
	n := 0
	for _, w := range c.words {
		if len(w) == 1 {
			if held[w[0]] {
				n++
			}
			continue
		}
		if holdsPhrase(tokens, w) {
			n++
		}
	}
	return n
}

// holdsPhrase reports whether phrase appears in tokens as consecutive tokens.
func holdsPhrase(tokens, phrase []string) bool {
	for i := 0; i+len(phrase) <= len(tokens); i++ {
		match := true
		for j, p := range phrase {
			if tokens[i+j] != p {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
