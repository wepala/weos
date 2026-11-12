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

package lexical

import (
	"reflect"
	"testing"
)

func TestContentWords(t *testing.T) {
	cases := []struct {
		query string
		want  []string
	}{
		{"the invoice for the Harbor Legal lease", []string{"invoice", "harbor", "legal", "lease"}},
		{"what is it about", nil},
		{"", nil},
		{"   ", nil},
		{"can't", nil},
		{"my child can't log in", []string{"child", "log"}},
		{"he doesn't, you're, they'll, I'm, we've, she'd", nil},
		{"Harbor & invoice", []string{"harbor", "invoice"}},
		{"Harbor | invoice", []string{"harbor", "invoice"}},
		{"Harbor:*", []string{"harbor"}},
		{"!Harbor", []string{"harbor"}},
		{"& | ! :* \" ( ) <->", nil},
		{`"Harbor" NEAR invoice`, []string{"harbor", "near", "invoice"}},
		{"follow-up", []string{"follow-up"}},
		{"e-mail", []string{"e-mail"}},
		{"Dana's", []string{"dana"}},
		{"dana@example.com", []string{"dana@example.com"}},
		{"see docs.example.com.", []string{"see", "docs.example.com"}},
		{"-Harbor- --invoice", []string{"harbor", "invoice"}},
		{"Harbor HARBOR harbor invoice", []string{"harbor", "invoice"}},
		{"x Q3", []string{"q3"}},
		{"José", []string{"josé"}},
		{"one two three four five six seven eight nine ten", []string{
			"one", "two", "three", "four", "five", "six", "seven", "eight"}},
		// Greetings and thanks say nothing about what a question is about, so
		// they never spend the cap.
		{"Hi, hello, hey! Please help: thanks, thank you", []string{"help"}},
		{"hi please thanks one two three four five six seven eight nine", []string{
			"one", "two", "three", "four", "five", "six", "seven", "eight"}},
	}
	for _, c := range cases {
		if got := ContentWords(c.query); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ContentWords(%q) = %q, want %q", c.query, got, c.want)
		}
	}
}

func TestStopListCoversModalsNegationsAndFragments(t *testing.T) {
	for _, w := range []string{"can", "cannot", "could", "will", "would", "should", "do", "does", "did",
		"not", "no", "t", "s", "d", "ll", "re", "ve", "m",
		"please", "thanks", "thank", "hello", "hi", "hey"} {
		if !IsStopWord(w) {
			t.Errorf("%q is not a stop word", w)
		}
	}
	for _, w := range []string{"harbor", "legal", "invoice", "lease", "dunmore", "settlement", "balance",
		"child", "log", "dana", "tablet"} {
		if IsStopWord(w) {
			t.Errorf("%q is a stop word", w)
		}
	}
}

func TestFloor(t *testing.T) {
	cases := []struct{ minMatch, n, want int }{
		{0, 3, 1}, {-2, 3, 1}, {1, 3, 1}, {2, 3, 2}, {3, 3, 3}, {5, 2, 2}, {5, 0, 0},
	}
	for _, c := range cases {
		if got := Floor(c.minMatch, c.n); got != c.want {
			t.Errorf("Floor(%d, %d) = %d, want %d", c.minMatch, c.n, got, c.want)
		}
	}
}

func TestTokensReadsTextAsUnicode61(t *testing.T) {
	got := Tokens("Follow-up with Dana's office at dana@example.com — José, v3.2")
	want := []string{"follow", "up", "with", "dana", "s", "office", "at", "dana", "example", "com",
		"jose", "v3", "2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokens = %q, want %q", got, want)
	}
}

func TestCoverageCountsDistinctWordsHeld(t *testing.T) {
	c := NewCoverage(ContentWords("the invoice for the Harbor Legal lease"))
	cases := []struct {
		text string
		want int
	}{
		{"Chase the Harbor invoice Harbor Legal invoice chase for the June lease", 4},
		{"Harbor Legal onboarding Welcome pack for the new Legal client", 2},
		{"Dunmore invoice backlog Invoice chase, invoice reminder and invoice escalation", 1},
		{"Order the office chairs Two more chairs for the studio", 0},
	}
	for _, tc := range cases {
		if got := c.Count(tc.text); got != tc.want {
			t.Errorf("Count(%q) = %d, want %d", tc.text, got, tc.want)
		}
	}
}

func TestCoverageMatchesAJoinedWordAsAPhrase(t *testing.T) {
	c := NewCoverage([]string{"follow-up", "dana@example.com", "jose"})
	if got := c.Count("Follow-up with Dana's office at dana@example.com, for José"); got != 3 {
		t.Errorf("Count = %d, want 3", got)
	}
	if got := c.Count("follow the example of dana, up at com"); got != 0 {
		t.Errorf("Count over the parts out of order = %d, want 0", got)
	}
}
