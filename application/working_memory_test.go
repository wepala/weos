package application

import (
	"context"
	"fmt"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
)

type fakeFlatLister struct {
	rows        []map[string]any
	err         error
	lastLimit   int
	lastSort    repositories.SortOptions
	lastFilters []repositories.FilterCondition
}

func (f *fakeFlatLister) ListFlat(
	_ context.Context, _, _ string, limit int, sort repositories.SortOptions,
) (repositories.PaginatedResponse[map[string]any], error) {
	f.lastLimit = limit
	f.lastSort = sort
	f.lastFilters = nil
	return repositories.PaginatedResponse[map[string]any]{Data: f.rows}, f.err
}

func (f *fakeFlatLister) ListFlatWithFilters(
	_ context.Context, _ string, filters []repositories.FilterCondition,
	_ string, limit int, sort repositories.SortOptions,
) (repositories.PaginatedResponse[map[string]any], error) {
	f.lastLimit = limit
	f.lastSort = sort
	f.lastFilters = filters
	return repositories.PaginatedResponse[map[string]any]{Data: f.rows}, f.err
}

func TestWorkingMemory_RecentFactsExcludesSuperseded(t *testing.T) {
	t.Parallel()

	lister := &fakeFlatLister{rows: []map[string]any{
		{"id": "urn:fact:new", "statement": "current belief", "confidence": 0.9,
			"wasRevisionOf": "urn:fact:old"},
		{"id": "urn:fact:old", "statement": "outdated belief",
			"invalidatedAtTime": "2026-07-01T00:00:00Z"},
	}}
	wm := &workingMemory{facts: lister}

	facts, err := wm.RecentFacts(context.Background(), "", 0)
	if err != nil {
		t.Fatalf("RecentFacts: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("facts = %d, want 1 (superseded excluded)", len(facts))
	}
	f := facts[0]
	if f.ID != "urn:fact:new" || f.Statement != "current belief" {
		t.Errorf("fact = %+v", f)
	}
	if f.Confidence != 0.9 || f.WasRevisionOf != "urn:fact:old" {
		t.Errorf("fact fields = %+v", f)
	}
	if f.Source != "working" {
		t.Errorf("Source = %q, want working", f.Source)
	}
	if lister.lastLimit != defaultWorkingSetSize {
		t.Errorf("limit = %d, want default %d", lister.lastLimit, defaultWorkingSetSize)
	}
	if lister.lastSort.SortBy != "updatedAt" || lister.lastSort.SortOrder != "desc" {
		t.Errorf("sort = %+v, want updatedAt desc (freshest first)", lister.lastSort)
	}
}

func TestWorkingMemory_NoProjectionTableIsEmptyNotError(t *testing.T) {
	t.Parallel()

	lister := &fakeFlatLister{err: fmt.Errorf("list: %w", repositories.ErrNoProjectionTable)}
	wm := &workingMemory{facts: lister}

	facts, err := wm.RecentFacts(context.Background(), "", 5)
	if err != nil {
		t.Fatalf("RecentFacts must treat a missing fact table as empty, got: %v", err)
	}
	if len(facts) != 0 {
		t.Errorf("facts = %d, want 0", len(facts))
	}
}

func TestWorkingMemory_AboutFilterRunsInsideTheQuery(t *testing.T) {
	t.Parallel()

	lister := &fakeFlatLister{rows: []map[string]any{
		{"id": "urn:fact:1", "statement": "about akeem", "about": "urn:person:akeem"},
	}}
	wm := &workingMemory{facts: lister}

	facts, err := wm.RecentFacts(context.Background(), "urn:person:akeem", 5)
	if err != nil {
		t.Fatalf("RecentFacts: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("facts = %d, want 1", len(facts))
	}
	// The filter must reach the projection query — filtering after the limit
	// would silently drop matches once other facts crowd the window.
	if len(lister.lastFilters) != 1 {
		t.Fatalf("filters = %+v, want the about eq condition pushed into the query", lister.lastFilters)
	}
	f := lister.lastFilters[0]
	if f.Field != "about" || f.Operator != "eq" || f.Value != "urn:person:akeem" {
		t.Errorf("filter = %+v", f)
	}
}

func TestMergeRecalledFacts_DedupsByURNWorkingWins(t *testing.T) {
	t.Parallel()

	working := []RecalledFact{
		{ID: "urn:fact:1", Statement: "fresh from projection", Source: "working"},
		{ID: "urn:fact:2", Statement: "not yet in graph", Source: "working"},
	}
	graph := []RecalledFact{
		{ID: "urn:fact:1", Statement: "stale graph copy", Source: "graph"},
		{ID: "urn:fact:3", Statement: "graph only", Source: "graph"},
	}

	merged := MergeRecalledFacts(working, graph)
	if len(merged) != 3 {
		t.Fatalf("merged = %d, want 3 (no double entries)", len(merged))
	}
	byID := map[string]RecalledFact{}
	for _, f := range merged {
		byID[f.ID] = f
	}
	if byID["urn:fact:1"].Statement != "fresh from projection" {
		t.Errorf("duplicate resolved to %q, want the working-set copy", byID["urn:fact:1"].Statement)
	}
	if _, ok := byID["urn:fact:2"]; !ok {
		t.Error("working-only fact missing — same-turn visibility broken")
	}
	if _, ok := byID["urn:fact:3"]; !ok {
		t.Error("graph-only fact missing")
	}
}
