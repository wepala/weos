package application

import (
	"context"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

type fakeLexicalIndex struct {
	active   bool
	indexed  map[string]string // id → content
	types    map[string]string // id → typeSlug
	removed  []string
	searches []string
	hits     []repositories.LexicalHit
}

func newFakeLexicalIndex(active bool) *fakeLexicalIndex {
	return &fakeLexicalIndex{active: active, indexed: map[string]string{}, types: map[string]string{}}
}

func (f *fakeLexicalIndex) Active() bool { return f.active }
func (f *fakeLexicalIndex) Index(_ context.Context, id, typeSlug, content string) error {
	f.indexed[id] = content
	f.types[id] = typeSlug
	return nil
}
func (f *fakeLexicalIndex) Remove(_ context.Context, id string) error {
	f.removed = append(f.removed, id)
	delete(f.indexed, id)
	return nil
}
func (f *fakeLexicalIndex) Search(_ context.Context, q string, _ int) ([]repositories.LexicalHit, error) {
	f.searches = append(f.searches, q)
	return f.hits, nil
}
func (f *fakeLexicalIndex) Clear(context.Context) error { return nil }

func TestLexicalHandler_IndexesPublishedResourceText(t *testing.T) {
	t.Parallel()

	published, tx := episode("note", "urn:note:1", "tx1", "ev1")
	index := newFakeLexicalIndex(true)
	h := lexicalHandler(&fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{"tx1": tx}},
		index, noopLogger{})

	if err := h(context.Background(), published); err != nil {
		t.Fatalf("handler: %v", err)
	}
	content, ok := index.indexed["urn:note:1"]
	if !ok {
		t.Fatal("resource not indexed")
	}
	if !strings.Contains(content, "episodic thing") {
		t.Errorf("content = %q, want the resource's text literal", content)
	}
	if index.types["urn:note:1"] != "note" {
		t.Errorf("typeSlug = %q, want note", index.types["urn:note:1"])
	}
}

func TestLexicalHandler_RemovesSupersededFactsAndDeletes(t *testing.T) {
	t.Parallel()

	// A superseded fact (invalidatedAtTime set) must leave the index.
	invalidated := domain.EventEnvelope[any]{
		ID: "ev2", EventType: "Resource.Updated", AggregateID: "urn:fact:old",
		SequenceNo: 3, TransactionID: "tx2",
		Payload: map[string]any{"Data": map[string]any{
			"@id": "urn:fact:old", "statement": "old belief",
			"invalidatedAtTime": "2026-07-02T00:00:00Z",
		}},
	}
	published := domain.EventEnvelope[any]{
		ID: "ev2-pub", EventType: "Resource.Published", AggregateID: "urn:fact:old",
		SequenceNo: 4, TransactionID: "tx2",
		Payload: map[string]any{"TypeSlug": "fact"},
	}
	index := newFakeLexicalIndex(true)
	index.indexed["urn:fact:old"] = "old belief"
	h := lexicalHandler(&fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{
		"tx2": {invalidated, published},
	}}, index, noopLogger{})

	if err := h(context.Background(), published); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if _, still := index.indexed["urn:fact:old"]; still {
		t.Error("superseded fact still indexed — lexical recall would surface it")
	}

	// Deletes drop the row too.
	if err := h(context.Background(), domain.EventEnvelope[any]{
		EventType: "Resource.Deleted", AggregateID: "urn:note:9",
	}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if index.removed[len(index.removed)-1] != "urn:note:9" {
		t.Errorf("removed = %v, want urn:note:9 last", index.removed)
	}
}

func TestProvideLexicalGroup_InactiveIndexRegistersNothing(t *testing.T) {
	t.Parallel()

	groups := ProvideLexicalGroup(LexicalGroupParams{
		Index:  newFakeLexicalIndex(false),
		Logger: noopLogger{},
	})
	if groups != nil {
		t.Fatalf("expected no group for an inactive index, got %+v", groups)
	}
}

func TestLexicalContent_DeterministicAndTextOnly(t *testing.T) {
	t.Parallel()

	node := map[string]any{
		"@id":       "urn:x",
		"@type":     "Note",
		"name":      "Quarterly review",
		"tags":      []any{"finance", "q3"},
		"count":     3.0,
		"published": true,
		"empty":     "",
	}
	got := lexicalContent(node)
	want := "Quarterly review finance q3"
	if got != want {
		t.Errorf("content = %q, want %q (sorted keys, strings only, no @-keys)", got, want)
	}
}
