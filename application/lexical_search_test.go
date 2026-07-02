package application

import (
	"context"
	"errors"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
)

type fakeKGLabelSearch struct {
	KnowledgeGraphService
	active bool
	terms  []repositories.KGTerm
	lastQ  string
}

func (f *fakeKGLabelSearch) Active() bool { return f.active }
func (f *fakeKGLabelSearch) SearchEntities(
	_ context.Context, q, _ string, _ int,
) ([]repositories.KGTerm, error) {
	f.lastQ = q
	return f.terms, nil
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

func TestLexicalSearch_UnavailableWhenNoBackend(t *testing.T) {
	t.Parallel()

	s := NewLexicalSearch(newFakeLexicalIndex(false), nil)
	_, _, err := s.Search(context.Background(), "akeem", 5)
	if !errors.Is(err, ErrLexicalUnavailable) {
		t.Fatalf("err = %v, want ErrLexicalUnavailable", err)
	}
}
