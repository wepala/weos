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

package application

import (
	"context"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
)

// The match mode and the floor reach the index as the caller gave them,
// beside the clamped limit and the normalized type filter.
func TestLexicalSearch_PassesTheMatchModeToTheIndex(t *testing.T) {
	t.Parallel()

	index := newFakeLexicalIndex(true)
	s := NewLexicalSearch(index, nil)
	_, _, err := s.Search(context.Background(), repositories.LexicalQuery{
		Text: "Harbor invoice", Limit: 500, TypeSlugs: []string{" Task "},
		Match: repositories.MatchAny, MinMatch: 2,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	got := index.queries[0]
	if got.Match != repositories.MatchAny || got.MinMatch != 2 || got.Limit != maxRecallLimit ||
		strings.Join(got.TypeSlugs, ",") != "task" || got.Text != "Harbor invoice" {
		t.Errorf("index query = %+v", got)
	}
}

// labelsByWord answers label search with a fixed answer per query word.
type labelsByWord struct {
	fakeKGLabelSearch
	byWord  map[string][]string
	queries []string
}

func (l *labelsByWord) SearchEntities(
	_ context.Context, q, _ string, _ int,
) ([]repositories.KGTerm, error) {
	l.queries = append(l.queries, q)
	var terms []repositories.KGTerm
	for _, id := range l.byWord[q] {
		terms = append(terms, repositories.KGTerm{Value: id})
	}
	return terms, nil
}

func TestLexicalSearch_AnyModeOnGraphLabelsRanksByCoverage(t *testing.T) {
	t.Parallel()

	kg := &labelsByWord{
		fakeKGLabelSearch: fakeKGLabelSearch{active: true},
		byWord: map[string][]string{
			"harbor":  {"urn:task:onboarding", "urn:task:chase"},
			"legal":   {"urn:task:onboarding", "urn:task:chase"},
			"invoice": {"urn:task:lease", "urn:task:chase", "urn:task:chase"},
		},
	}
	s := NewLexicalSearch(newFakeLexicalIndex(false), kg)

	hits, mode, err := s.Search(context.Background(), repositories.LexicalQuery{
		Text: "the Harbor Legal invoice", Limit: 10, Match: repositories.MatchAny,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if mode != LexicalModeGraphLabels {
		t.Errorf("mode = %q", mode)
	}
	if got := strings.Join(kg.queries, ","); got != "harbor,legal,invoice" {
		t.Errorf("label searches = %s, want one per content word", got)
	}
	if got := hitList(hits); got != "urn:task:chase,urn:task:onboarding,urn:task:lease" {
		t.Errorf("hits = %s, want ranked by coverage", got)
	}

	hits, _, err = s.Search(context.Background(), repositories.LexicalQuery{
		Text: "Harbor Legal invoice", Limit: 10, Match: repositories.MatchAny, MinMatch: 3,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := hitList(hits); got != "urn:task:chase" {
		t.Errorf("MinMatch 3 hits = %s, want the resource holding every word", got)
	}

	hits, _, err = s.Search(context.Background(), repositories.LexicalQuery{
		Text: "Harbor Legal invoice", Limit: 1, Match: repositories.MatchAny,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := hitList(hits); got != "urn:task:chase" {
		t.Errorf("limit 1 hits = %s, want the best-covering resource", got)
	}
}

func TestLexicalSearch_AnyModeOnGraphLabelsWithNoContentWordsMatchesNothing(t *testing.T) {
	t.Parallel()

	kg := &labelsByWord{fakeKGLabelSearch: fakeKGLabelSearch{active: true}}
	s := NewLexicalSearch(newFakeLexicalIndex(false), kg)
	hits, _, err := s.Search(context.Background(), repositories.LexicalQuery{
		Text: "what is it about", Limit: 10, Match: repositories.MatchAny,
	})
	if err != nil || len(hits) != 0 || len(kg.queries) != 0 {
		t.Errorf("hits = %v, err = %v, label searches = %v; want none of each", hits, err, kg.queries)
	}
}

// rankedAnyWordGraph is a graph that counts coverage in one query
// (AnyWordLabelSearch). Its per-word label search fails the test if called.
type rankedAnyWordGraph struct {
	fakeKGLabelSearch
	t        *testing.T
	answer   []string
	calls    int
	words    []string
	minMatch int
	types    []string
	limit    int
}

func (g *rankedAnyWordGraph) SearchEntities(
	_ context.Context, q, _ string, _ int,
) ([]repositories.KGTerm, error) {
	g.t.Errorf("per-word label search %q ran; want the one coverage query", q)
	return nil, nil
}

func (g *rankedAnyWordGraph) SearchEntitiesAnyWord(
	_ context.Context, words []string, minMatch int, types []string, limit int,
) ([]repositories.KGTerm, error) {
	g.calls++
	g.words, g.minMatch, g.types, g.limit = words, minMatch, types, limit
	terms := make([]repositories.KGTerm, 0, len(g.answer))
	for _, id := range g.answer {
		terms = append(terms, repositories.KGTerm{Value: id})
	}
	return terms, nil
}

// A graph that ranks by coverage in one query gets the content words, the
// floor and the type filter in that query, before its limit, so a resource
// past one word's many matches is still counted for every word it holds
// (Copilot on PR #583). Its order is kept, and the caller's limit applies.
func TestLexicalSearch_AnyModeOnGraphLabelsCountsCoverageInOneQuery(t *testing.T) {
	t.Parallel()

	kg := &rankedAnyWordGraph{
		fakeKGLabelSearch: fakeKGLabelSearch{active: true},
		t:                 t,
		answer:            []string{"urn:task:chase", "urn:task:lease", "urn:task:onboarding"},
	}
	s := NewLexicalSearch(newFakeLexicalIndex(false), kg)

	hits, mode, err := s.Search(context.Background(), repositories.LexicalQuery{
		Text: "the Harbor invoice", Limit: 2, TypeSlugs: []string{" Task "},
		Match: repositories.MatchAny, MinMatch: 5,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if mode != LexicalModeGraphLabels {
		t.Errorf("mode = %q", mode)
	}
	if kg.calls != 1 || strings.Join(kg.words, ",") != "harbor,invoice" || kg.minMatch != 2 ||
		strings.Join(kg.types, ",") != "task" || kg.limit != maxRecallLimit {
		t.Errorf("coverage query: calls=%d words=%v minMatch=%d types=%v limit=%d; "+
			"want 1 call for [harbor invoice], floor 2, [task], limit %d",
			kg.calls, kg.words, kg.minMatch, kg.types, kg.limit, maxRecallLimit)
	}
	if got := hitList(hits); got != "urn:task:chase,urn:task:lease" {
		t.Errorf("hits = %s, want the graph's order cut to the limit", got)
	}
}

func TestParseMatchMode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want repositories.MatchMode
	}{
		{"", repositories.MatchAll}, {"all", repositories.MatchAll}, {" ANY ", repositories.MatchAny},
	}
	for _, c := range cases {
		got, err := repositories.ParseMatchMode(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseMatchMode(%q) = %v, %v; want %v", c.in, got, err, c.want)
		}
	}
	if _, err := repositories.ParseMatchMode("some"); err == nil {
		t.Error("ParseMatchMode(\"some\") succeeded, want an error")
	}
}

func hitList(hits []repositories.LexicalHit) string {
	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.ID)
	}
	return strings.Join(ids, ",")
}
