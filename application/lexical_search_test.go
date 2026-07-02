package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
)

type fakeKGLabelSearch struct {
	KnowledgeGraphService
	active      bool
	terms       []repositories.KGTerm
	lastQ       string
	invalidated []string // fact IRIs the supersession-check query reports
	lastSPARQL  string
}

func (f *fakeKGLabelSearch) Active() bool { return f.active }
func (f *fakeKGLabelSearch) SearchEntities(
	_ context.Context, q, _ string, _ int,
) ([]repositories.KGTerm, error) {
	f.lastQ = q
	return f.terms, nil
}

func (f *fakeKGLabelSearch) Query(
	_ context.Context, sparql string,
) (repositories.KGQueryResult, error) {
	f.lastSPARQL = sparql
	res := repositories.KGQueryResult{}
	for _, id := range f.invalidated {
		res.Bindings = append(res.Bindings,
			map[string]repositories.KGTerm{"f": {Value: id}})
	}
	return res, nil
}

func TestLexicalSearch_FTS5First(t *testing.T) {
	t.Parallel()

	index := newFakeLexicalIndex(true)
	index.hits = []repositories.LexicalHit{{ID: "urn:fact:1", TypeSlug: "fact", Snippet: "…akeem…"}}
	s := NewLexicalSearch(index, &fakeKGLabelSearch{active: true})

	hits, mode, err := s.Search(context.Background(), "akeem", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if mode != LexicalModeFTS5 {
		t.Errorf("mode = %q, want fts5", mode)
	}
	if len(hits) != 1 || hits[0].ID != "urn:fact:1" {
		t.Errorf("hits = %+v", hits)
	}
}

func TestLexicalSearch_DegradesToGraphLabels(t *testing.T) {
	t.Parallel()

	kg := &fakeKGLabelSearch{active: true, terms: []repositories.KGTerm{{Value: "urn:person:akeem"}}}
	s := NewLexicalSearch(newFakeLexicalIndex(false), kg)

	hits, mode, err := s.Search(context.Background(), "akeem", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if mode != LexicalModeGraphLabels {
		t.Errorf("mode = %q, want graph-labels", mode)
	}
	if len(hits) != 1 || hits[0].ID != "urn:person:akeem" {
		t.Errorf("hits = %+v", hits)
	}
	if kg.lastQ != "akeem" {
		t.Errorf("kg query = %q", kg.lastQ)
	}
}

func TestLexicalSearch_FallbackDropsSupersededFacts(t *testing.T) {
	t.Parallel()

	kg := &fakeKGLabelSearch{
		active: true,
		terms: []repositories.KGTerm{
			{Value: "urn:fact:dead"},
			{Value: "urn:fact:live"},
			{Value: "urn:person:akeem"},
		},
		invalidated: []string{"urn:fact:dead"},
	}
	s := NewLexicalSearch(newFakeLexicalIndex(false), kg)

	hits, mode, err := s.Search(context.Background(), "akeem", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if mode != LexicalModeGraphLabels {
		t.Errorf("mode = %q", mode)
	}
	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.ID)
	}
	if len(ids) != 2 || ids[0] != "urn:fact:live" || ids[1] != "urn:person:akeem" {
		t.Errorf("hits = %v, want the superseded fact dropped", ids)
	}
	if !strings.Contains(kg.lastSPARQL, "urn:fact:dead") ||
		!strings.Contains(kg.lastSPARQL, "http://www.w3.org/ns/prov#invalidatedAtTime") {
		t.Errorf("supersession check query = %q", kg.lastSPARQL)
	}
}

func TestLexicalSearch_UnavailableWhenNoBackend(t *testing.T) {
	t.Parallel()

	s := NewLexicalSearch(newFakeLexicalIndex(false), nil)
	_, _, err := s.Search(context.Background(), "akeem", 5)
	if !errors.Is(err, ErrLexicalUnavailable) {
		t.Fatalf("err = %v, want ErrLexicalUnavailable", err)
	}
}
