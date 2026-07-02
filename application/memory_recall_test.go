package application

import (
	"context"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
)

type fakeKGQuery struct {
	active    bool
	lastQuery string
	result    repositories.KGQueryResult
	err       error
	KnowledgeGraphService
}

func (f *fakeKGQuery) Active() bool { return f.active }

func (f *fakeKGQuery) Query(_ context.Context, sparql string) (repositories.KGQueryResult, error) {
	f.lastQuery = sparql
	return f.result, f.err
}

type fakeWorkingMemory struct {
	facts     []RecalledFact
	lastAbout string
}

func (f *fakeWorkingMemory) RecentFacts(_ context.Context, about string, _ int) ([]RecalledFact, error) {
	f.lastAbout = about
	return f.facts, nil
}

func term(v string) repositories.KGTerm { return repositories.KGTerm{Value: v} }

func TestMemoryRecall_MergesWorkingAndGraphWithSupersessionFilter(t *testing.T) {
	t.Parallel()

	kg := &fakeKGQuery{active: true, result: repositories.KGQueryResult{
		Vars: []string{"fact", "statement"},
		Bindings: []map[string]repositories.KGTerm{
			{"fact": term("urn:fact:graph1"), "statement": term("graph fact"),
				"generatedAt": term("2026-06-01T00:00:00Z"), "confidence": term("0.8")},
			{"fact": term("urn:fact:shared"), "statement": term("stale copy")},
		},
	}}
	wm := &fakeWorkingMemory{facts: []RecalledFact{
		{ID: "urn:fact:shared", Statement: "fresh copy", Source: "working"},
		{ID: "urn:fact:new", Statement: "just written", Source: "working"},
	}}
	recall := NewMemoryRecall(kg, wm)

	facts, err := recall.Recall(context.Background(), RecallQuery{})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if !strings.Contains(kg.lastQuery, "FILTER NOT EXISTS { ?fact <http://www.w3.org/ns/prov#invalidatedAtTime>") {
		t.Errorf("supersession filter missing from recall SPARQL:\n%s", kg.lastQuery)
	}
	if len(facts) != 3 {
		t.Fatalf("facts = %d, want 3 (deduped)", len(facts))
	}
	byID := map[string]RecalledFact{}
	for _, f := range facts {
		byID[f.ID] = f
	}
	if byID["urn:fact:shared"].Statement != "fresh copy" {
		t.Errorf("shared fact = %q, want the working-set copy", byID["urn:fact:shared"].Statement)
	}
	if _, ok := byID["urn:fact:new"]; !ok {
		t.Error("just-written fact missing — same-turn visibility broken")
	}
	if g := byID["urn:fact:graph1"]; g.Confidence != 0.8 || g.Source != "graph" {
		t.Errorf("graph fact = %+v", g)
	}
}

func TestMemoryRecall_FiltersApplyToBothStores(t *testing.T) {
	t.Parallel()

	kg := &fakeKGQuery{active: true}
	wm := &fakeWorkingMemory{facts: []RecalledFact{
		{ID: "urn:fact:1", Statement: "Akeem bases PRs on v3", About: "urn:person:akeem"},
		{ID: "urn:fact:2", Statement: "unrelated", About: "urn:org:wepala"},
	}}
	recall := NewMemoryRecall(kg, wm)

	facts, err := recall.Recall(context.Background(),
		RecallQuery{About: "urn:person:akeem", Keyword: "PRs"})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(facts) != 1 || facts[0].ID != "urn:fact:1" {
		t.Fatalf("facts = %+v, want only the matching working fact", facts)
	}
	if wm.lastAbout != "urn:person:akeem" {
		t.Errorf("working-set about = %q — the filter must be pushed into the projection query", wm.lastAbout)
	}
	if !strings.Contains(kg.lastQuery, `FILTER(STR(?about) = "urn:person:akeem")`) {
		t.Errorf("about filter missing from SPARQL:\n%s", kg.lastQuery)
	}
	if !strings.Contains(kg.lastQuery, `CONTAINS(LCASE(STR(?statement)), "prs")`) {
		t.Errorf("keyword filter missing from SPARQL:\n%s", kg.lastQuery)
	}
}

func TestMemoryRecall_ProvenanceOnRequest(t *testing.T) {
	t.Parallel()

	kg := &fakeKGQuery{active: true, result: repositories.KGQueryResult{
		Bindings: []map[string]repositories.KGTerm{
			{"fact": term("urn:fact:1"), "statement": term("s"),
				"sources": term("urn:event:a urn:event:b")},
		},
	}}
	recall := NewMemoryRecall(kg, &fakeWorkingMemory{})

	facts, err := recall.Recall(context.Background(), RecallQuery{IncludeProvenance: true})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if !strings.Contains(kg.lastQuery, "GROUP_CONCAT(DISTINCT ?src") ||
		!strings.Contains(kg.lastQuery, "GROUP BY ?fact") {
		t.Errorf("provenance aggregation missing from SPARQL:\n%s", kg.lastQuery)
	}
	if len(facts) != 1 || len(facts[0].DerivedFrom) != 2 {
		t.Fatalf("facts = %+v, want one fact with two source events", facts)
	}
	if facts[0].DerivedFrom[0] != "urn:event:a" {
		t.Errorf("DerivedFrom = %v", facts[0].DerivedFrom)
	}
}

func TestMemoryRecall_WorksWithoutGraph(t *testing.T) {
	t.Parallel()

	wm := &fakeWorkingMemory{facts: []RecalledFact{{ID: "urn:fact:1", Statement: "s"}}}

	for name, kg := range map[string]KnowledgeGraphService{
		"nil service":    nil,
		"inactive store": &fakeKGQuery{active: false},
	} {
		recall := NewMemoryRecall(kg, wm)
		facts, err := recall.Recall(context.Background(), RecallQuery{})
		if err != nil {
			t.Fatalf("%s: Recall must degrade to working-only, got: %v", name, err)
		}
		if len(facts) != 1 {
			t.Errorf("%s: facts = %d, want 1", name, len(facts))
		}
	}
}

func TestMemoryRecall_EscapesSPARQLInjection(t *testing.T) {
	t.Parallel()

	kg := &fakeKGQuery{active: true}
	recall := NewMemoryRecall(kg, &fakeWorkingMemory{})

	_, err := recall.Recall(context.Background(),
		RecallQuery{About: `urn:x") . ?s ?p ?o . FILTER("a" = "a`})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if strings.Contains(kg.lastQuery, `urn:x") .`) {
		t.Errorf("quote not escaped — SPARQL injection possible:\n%s", kg.lastQuery)
	}
	if !strings.Contains(kg.lastQuery, `urn:x\"`) {
		t.Errorf("expected escaped quote in query:\n%s", kg.lastQuery)
	}
}
