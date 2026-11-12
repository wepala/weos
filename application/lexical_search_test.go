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
	lastClass   string
	lastLimit   int
	invalidated []string // fact IRIs the supersession-check query reports
	lastSPARQL  string
}

func (f *fakeKGLabelSearch) Active() bool { return f.active }
func (f *fakeKGLabelSearch) SearchEntities(
	_ context.Context, q, classIRI string, limit int,
) ([]repositories.KGTerm, error) {
	f.lastQ = q
	f.lastClass = classIRI
	f.lastLimit = limit
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

func TestLexicalSearch_ReportsTheIndexEngineAsMode(t *testing.T) {
	t.Parallel()

	index := newFakeLexicalIndex(true)
	index.engine = repositories.LexicalEnginePostgres
	s := NewLexicalSearch(index, nil)

	_, mode, err := s.Search(context.Background(), "akeem", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if mode != LexicalModePostgresFTS {
		t.Errorf("mode = %q, want %q", mode, LexicalModePostgresFTS)
	}
	if !IsFullTextMode(mode) || !IsFullTextMode(LexicalModeFTS5) || IsFullTextMode(LexicalModeGraphLabels) {
		t.Errorf("IsFullTextMode misclassifies the modes")
	}
}

func TestLexicalSearch_PassesTheTypeFilterToTheIndex(t *testing.T) {
	t.Parallel()

	index := newFakeLexicalIndex(true)
	s := NewLexicalSearch(index, nil)

	if _, _, err := s.Search(context.Background(), "invoice", 5, "task", " "); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(index.searchTypes) != 1 || len(index.searchTypes[0]) != 1 || index.searchTypes[0][0] != "task" {
		t.Errorf("index type filter = %v, want [task] with the blank dropped", index.searchTypes)
	}
}

func TestLexicalSearch_FallbackNarrowsToTheType(t *testing.T) {
	t.Parallel()

	var terms []repositories.KGTerm
	for i := 0; i < 30; i++ {
		terms = append(terms, repositories.KGTerm{Value: "urn:project:p" + strings.Repeat("x", i)})
	}
	terms = append(terms, repositories.KGTerm{Value: "urn:task:t1"}, repositories.KGTerm{Value: "urn:taskboard:b1"})
	kg := &fakeKGLabelSearch{active: true, terms: terms}
	s := NewLexicalSearch(newFakeLexicalIndex(false), kg)

	hits, mode, err := s.Search(context.Background(), "invoice", 5, "task")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if mode != LexicalModeGraphLabels {
		t.Errorf("mode = %q", mode)
	}
	if len(hits) != 1 || hits[0].ID != "urn:task:t1" {
		t.Errorf("hits = %+v, want only the task", hits)
	}
	if kg.lastLimit != maxRecallLimit {
		t.Errorf("label search limit = %d, want %d so other types cannot spend it", kg.lastLimit, maxRecallLimit)
	}
}

func TestLexicalSearch_FallbackKeepsTheLimitUnnarrowed(t *testing.T) {
	t.Parallel()

	kg := &fakeKGLabelSearch{active: true}
	s := NewLexicalSearch(newFakeLexicalIndex(false), kg)
	if _, _, err := s.Search(context.Background(), "invoice", 7); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if kg.lastLimit != 7 {
		t.Errorf("label search limit = %d, want the caller's 7", kg.lastLimit)
	}
}

func TestLexicalSearch_NormalizesTheTypeFilter(t *testing.T) {
	t.Parallel()

	index := newFakeLexicalIndex(true)
	s := NewLexicalSearch(index, nil)
	if _, _, err := s.Search(context.Background(), "invoice", 5, "Task ", " NOTE"); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(index.searchTypes) != 1 || strings.Join(index.searchTypes[0], ",") != "task,note" {
		t.Errorf("index type filter = %v, want [task note] trimmed and lowercased", index.searchTypes)
	}

	kg := &fakeKGLabelSearch{active: true, terms: []repositories.KGTerm{
		{Value: "urn:project:p1"}, {Value: "urn:task:t1"},
	}}
	fallback := NewLexicalSearch(newFakeLexicalIndex(false), kg)
	hits, _, err := fallback.Search(context.Background(), "invoice", 5, " Task")
	if err != nil {
		t.Fatalf("fallback Search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "urn:task:t1" {
		t.Errorf("fallback hits = %+v, want only the task for the filter \" Task\"", hits)
	}
}
