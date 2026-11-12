package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/auth"
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

// rankedLabelGraph answers label search the way the graph does: in its own
// order, cut at the limit. Its type-scoped search applies the type before the
// limit, as the SPARQL FILTER does.
type rankedLabelGraph struct {
	KnowledgeGraphService
	terms      []repositories.KGTerm
	typedCalls int
}

func (g *rankedLabelGraph) Active() bool { return true }

func (g *rankedLabelGraph) SearchEntities(
	_ context.Context, _, _ string, limit int,
) ([]repositories.KGTerm, error) {
	if len(g.terms) > limit {
		return g.terms[:limit], nil
	}
	return g.terms, nil
}

func (g *rankedLabelGraph) SearchEntitiesOfTypes(
	_ context.Context, _ string, typeSlugs []string, limit int,
) ([]repositories.KGTerm, error) {
	g.typedCalls++
	var out []repositories.KGTerm
	for _, t := range g.terms {
		if urnHasType(t.Value, typeSlugs) && len(out) < limit {
			out = append(out, t)
		}
	}
	return out, nil
}

func (g *rankedLabelGraph) Query(context.Context, string) (repositories.KGQueryResult, error) {
	return repositories.KGQueryResult{}, nil
}

// More than the graph's maximum of other types' labels rank before the one
// matching task. The type filter must reach the graph query, before its
// LIMIT, or the task is cut off and a task-only search finds nothing
// (Copilot review 5869602749).
func TestLexicalSearch_FallbackFindsATypePastTheGraphMaximum(t *testing.T) {
	t.Parallel()

	var terms []repositories.KGTerm
	for i := 0; i < maxRecallLimit+20; i++ {
		terms = append(terms, repositories.KGTerm{Value: fmt.Sprintf("urn:project:p%03d", i)})
	}
	terms = append(terms, repositories.KGTerm{Value: "urn:task:t1"})
	kg := &rankedLabelGraph{terms: terms}
	s := NewLexicalSearch(newFakeLexicalIndex(false), kg)

	hits, mode, err := s.Search(context.Background(), "invoice", 5, "task")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if mode != LexicalModeGraphLabels {
		t.Errorf("mode = %q", mode)
	}
	if len(hits) != 1 || hits[0].ID != "urn:task:t1" {
		t.Errorf("hits = %+v, want the task past %d project labels", hits, maxRecallLimit+20)
	}
	if kg.typedCalls != 1 {
		t.Errorf("type-scoped label search ran %d times, want 1", kg.typedCalls)
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

// The caller's identity becomes the index query's scope, so the index can
// apply it before its LIMIT; a caller with no identity (CLI, stdio, a
// background job) searches unscoped, as it reads every resource.
func TestLexicalSearch_PassesTheCallersScopeToTheIndex(t *testing.T) {
	t.Parallel()

	index := newFakeLexicalIndex(true)
	s := NewLexicalSearch(index, nil)

	caller := auth.ContextWithAgent(context.Background(), &auth.Identity{
		AgentID: "agent-cedar", AccountIDs: []string{"cedar"}, ActiveAccountID: "cedar",
	})
	if _, _, err := s.Search(caller, "Dunmore", 5); err != nil {
		t.Fatalf("scoped Search: %v", err)
	}
	if _, _, err := s.Search(context.Background(), "Dunmore", 5); err != nil {
		t.Fatalf("system Search: %v", err)
	}
	if len(index.scopes) != 2 {
		t.Fatalf("index searched %d times, want 2", len(index.scopes))
	}
	got := index.scopes[0]
	if got == nil || got.AgentID != "agent-cedar" || got.AccountID != "cedar" || got.IsAdmin {
		t.Errorf("scope = %+v, want agent-cedar in cedar, not admin", got)
	}
	if index.scopes[1] != nil {
		t.Errorf("system caller scope = %+v, want nil (unscoped)", index.scopes[1])
	}
}

// --- The rebuilding notice (wm-4db1v2) ---

// While the full-text index is behind the event history the answer carries a
// notice saying it may be partial, next to the mode.
func TestLexicalRebuildNotice_SaysWhenTheIndexIsBehind(t *testing.T) {
	t.Parallel()
	index := newFakeLexicalIndex(true)
	index.lag = 120
	s := NewLexicalSearch(index, nil)
	_, mode, err := s.Search(context.Background(), "akeem", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	notice := LexicalRebuildNotice(context.Background(), s, mode)
	if !strings.Contains(notice, "rebuilding") || !strings.Contains(notice, "120 events behind") ||
		!strings.Contains(notice, "incomplete") {
		t.Errorf("notice = %q, want it to say the index is rebuilding, 120 events behind, answer incomplete", notice)
	}
}

func TestLexicalRebuildNotice_NoneWhenCaughtUp(t *testing.T) {
	t.Parallel()
	s := NewLexicalSearch(newFakeLexicalIndex(true), nil)
	if notice := LexicalRebuildNotice(context.Background(), s, LexicalModeFTS5); notice != "" {
		t.Errorf("notice = %q for a caught-up index, want none", notice)
	}
}

// The degraded path reads the graph, not the index, so the index's lag says
// nothing about its answer.
func TestLexicalRebuildNotice_NoneOnGraphLabels(t *testing.T) {
	t.Parallel()
	index := newFakeLexicalIndex(false)
	index.lag = 120
	s := NewLexicalSearch(index, &fakeKGLabelSearch{active: true})
	if notice := LexicalRebuildNotice(context.Background(), s, LexicalModeGraphLabels); notice != "" {
		t.Errorf("notice = %q on graph-labels, want none", notice)
	}
}

// A lag that cannot be read is not reported as "complete": the notice says
// the answer may be partial.
func TestLexicalRebuildNotice_SaysWhenProgressCannotBeRead(t *testing.T) {
	t.Parallel()
	index := newFakeLexicalIndex(true)
	index.lagErr = errors.New("no checkpoint table")
	s := NewLexicalSearch(index, nil)
	notice := LexicalRebuildNotice(context.Background(), s, LexicalModeFTS5)
	if !strings.Contains(notice, "may be incomplete") {
		t.Errorf("notice = %q, want it to say the answer may be incomplete", notice)
	}
	if strings.Contains(notice, "no checkpoint table") {
		t.Errorf("notice = %q quotes the database error to the caller", notice)
	}
}

// A LexicalSearch that cannot report progress (one written outside core)
// gives no notice rather than a guess.
func TestLexicalRebuildNotice_NoneWithoutProgress(t *testing.T) {
	t.Parallel()
	var s LexicalSearch = searchWithoutProgress{}
	if notice := LexicalRebuildNotice(context.Background(), s, LexicalModeFTS5); notice != "" {
		t.Errorf("notice = %q, want none", notice)
	}
}

type searchWithoutProgress struct{}

func (searchWithoutProgress) Search(
	context.Context, string, int, ...string,
) ([]repositories.LexicalHit, string, error) {
	return nil, LexicalModeFTS5, nil
}
