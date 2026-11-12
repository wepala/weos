package application

import (
	"context"
	"errors"
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
	// queries records each search's whole query.
	queries []repositories.LexicalQuery
	// searchTypes records the type filter of each search.
	searchTypes [][]string
	// owners records the owner each resource was indexed with.
	owners map[string]repositories.LexicalOwner
	// scopes records the visibility scope of each search.
	scopes []*repositories.VisibilityScope
	hits   []repositories.LexicalHit
	engine string
	// ownerErr fails every Owner lookup; ownerLookups counts them.
	ownerErr     error
	ownerLookups int
	// lag and lagErr are what Lag reports.
	lag    int64
	lagErr error
}

func newFakeLexicalIndex(active bool) *fakeLexicalIndex {
	return &fakeLexicalIndex{
		active: active, indexed: map[string]string{}, types: map[string]string{},
		owners: map[string]repositories.LexicalOwner{},
	}
}

func (f *fakeLexicalIndex) Active() bool { return f.active }
func (f *fakeLexicalIndex) Engine() string {
	if !f.active {
		return ""
	}
	if f.engine == "" {
		return repositories.LexicalEngineFTS5
	}
	return f.engine
}
func (f *fakeLexicalIndex) Index(
	_ context.Context, id, typeSlug, content string, owner repositories.LexicalOwner,
) error {
	f.indexed[id] = content
	f.types[id] = typeSlug
	f.owners[id] = owner
	return nil
}
func (f *fakeLexicalIndex) Remove(_ context.Context, id string) error {
	f.removed = append(f.removed, id)
	delete(f.indexed, id)
	return nil
}
func (f *fakeLexicalIndex) Owner(_ context.Context, id string) (repositories.LexicalOwner, bool, error) {
	f.ownerLookups++
	if f.ownerErr != nil {
		return repositories.LexicalOwner{}, false, f.ownerErr
	}
	if _, ok := f.indexed[id]; !ok {
		return repositories.LexicalOwner{}, false, nil
	}
	return f.owners[id], true, nil
}
func (f *fakeLexicalIndex) Search(
	_ context.Context, q repositories.LexicalQuery, scope *repositories.VisibilityScope,
) ([]repositories.LexicalHit, error) {
	f.searches = append(f.searches, q.Text)
	f.queries = append(f.queries, q)
	f.scopes = append(f.scopes, scope)
	f.searchTypes = append(f.searchTypes, q.TypeSlugs)
	return f.hits, nil
}
func (f *fakeLexicalIndex) Clear(context.Context) error { return nil }
func (f *fakeLexicalIndex) Lag(context.Context) (int64, error) {
	return f.lag, f.lagErr
}

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

// A deployment whose index did not exist before (PostgreSQL before
// wm-irqzji) fills it on first start only because the group replays history
// from event 0 rather than starting at the feed head.
func TestProvideLexicalGroup_ActiveIndexReplaysFromTheStart(t *testing.T) {
	t.Parallel()

	index := newFakeLexicalIndex(true)
	index.engine = repositories.LexicalEnginePostgres
	groups := ProvideLexicalGroup(LexicalGroupParams{Index: index, Logger: noopLogger{}})
	if len(groups) != 1 || groups[0].Name != "lexical-index" {
		t.Fatalf("groups = %+v, want the lexical-index group", groups)
	}
	if groups[0].StartAtHead {
		t.Error("group must not StartAtHead — an empty index is filled by replaying all history")
	}
	if groups[0].Truncate == nil {
		t.Error("group must carry Truncate so `worker checkpoint reset lexical-index --truncate` rebuilds it")
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

// ownedEpisode is a created-and-published resource whose Resource.Created
// carries an owner, as one written by an authenticated caller does.
func ownedEpisode(aggregateID, txID, agent, account string) (
	domain.EventEnvelope[any], []domain.EventEnvelope[any],
) {
	published, tx := episode("note", aggregateID, txID, txID+"-created")
	created := tx[0].Payload.(map[string]any)
	created["CreatedBy"] = agent
	created["AccountID"] = account
	return published, tx
}

// The owner comes from the resource's own Resource.Created, never from the
// handler's context — which carries no caller during a rebuild.
func TestLexicalHandler_IndexesTheOwnerFromTheCreateEvent(t *testing.T) {
	t.Parallel()

	published, tx := ownedEpisode("urn:note:1", "tx1", "agent-harbor", "harbor")
	index := newFakeLexicalIndex(true)
	h := lexicalHandler(&fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{"tx1": tx}},
		index, noopLogger{})

	if err := h(context.Background(), published); err != nil {
		t.Fatalf("handler: %v", err)
	}
	want := repositories.LexicalOwner{CreatedBy: "agent-harbor", AccountID: "harbor"}
	if got := index.owners["urn:note:1"]; got != want {
		t.Errorf("owner = %+v, want %+v", got, want)
	}
}

// An update transaction has no Resource.Created in it. With no index row for
// the resource yet, the owner is read from the aggregate's history. Indexing
// it as ownerless would show the updated text to every caller.
func TestLexicalHandler_KeepsTheOwnerOnUpdate(t *testing.T) {
	t.Parallel()

	_, createTx := ownedEpisode("urn:note:1", "tx1", "agent-harbor", "harbor")
	updated := domain.EventEnvelope[any]{
		ID: "ev-upd", EventType: "Resource.Updated", AggregateID: "urn:note:1",
		SequenceNo: 3, TransactionID: "tx2",
		Payload: map[string]any{"Data": map[string]any{"@id": "urn:note:1", "name": "Dunmore settlement"}},
	}
	published := domain.EventEnvelope[any]{
		ID: "ev-upd-pub", EventType: "Resource.Published", AggregateID: "urn:note:1",
		SequenceNo: 4, TransactionID: "tx2",
		Payload: map[string]any{"TypeSlug": "note"},
	}
	store := &fakeEventStore{
		tx:          map[string][]domain.EventEnvelope[any]{"tx2": {updated, published}},
		byAggregate: map[string][]domain.EventEnvelope[any]{"urn:note:1": append(createTx, updated, published)},
	}
	index := newFakeLexicalIndex(true)
	if err := lexicalHandler(store, index, noopLogger{})(context.Background(), published); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if index.indexed["urn:note:1"] != "Dunmore settlement" {
		t.Fatalf("content = %q, want the updated text", index.indexed["urn:note:1"])
	}
	want := repositories.LexicalOwner{CreatedBy: "agent-harbor", AccountID: "harbor"}
	if got := index.owners["urn:note:1"]; got != want {
		t.Errorf("owner after update = %+v, want %+v", got, want)
	}
}

// A history that cannot be read is an error the subscriber retries, not an
// ownerless row.
func TestLexicalHandler_FailsWhenTheOwnerCannotBeRead(t *testing.T) {
	t.Parallel()

	updated := domain.EventEnvelope[any]{
		ID: "ev-upd", EventType: "Resource.Updated", AggregateID: "urn:note:1",
		SequenceNo: 3, TransactionID: "tx2",
		Payload: map[string]any{"Data": map[string]any{"@id": "urn:note:1", "name": "Dunmore"}},
	}
	published := domain.EventEnvelope[any]{
		ID: "ev-upd-pub", EventType: "Resource.Published", AggregateID: "urn:note:1",
		SequenceNo: 4, TransactionID: "tx2", Payload: map[string]any{"TypeSlug": "note"},
	}
	store := &fakeEventStore{
		tx:           map[string][]domain.EventEnvelope[any]{"tx2": {updated, published}},
		getEventsErr: errors.New("store unavailable"),
	}
	index := newFakeLexicalIndex(true)
	if err := lexicalHandler(store, index, noopLogger{})(context.Background(), published); err == nil {
		t.Fatal("handler succeeded, want the history error so the event is retried")
	}
	if _, indexed := index.indexed["urn:note:1"]; indexed {
		t.Error("resource indexed without its owner")
	}
}

// noteUpdate is an update transaction for urn:note:1 with new text.
func noteUpdate(text string) (domain.EventEnvelope[any], []domain.EventEnvelope[any]) {
	updated := domain.EventEnvelope[any]{
		ID: "ev-upd", EventType: "Resource.Updated", AggregateID: "urn:note:1",
		SequenceNo: 3, TransactionID: "tx2",
		Payload: map[string]any{"Data": map[string]any{"@id": "urn:note:1", "name": text}},
	}
	published := domain.EventEnvelope[any]{
		ID: "ev-upd-pub", EventType: "Resource.Published", AggregateID: "urn:note:1",
		SequenceNo: 4, TransactionID: "tx2", Payload: map[string]any{"TypeSlug": "note"},
	}
	return published, []domain.EventEnvelope[any]{updated, published}
}

// An owner never changes, so an update to a resource the index already holds
// reads the owner from its row and never loads the aggregate's history
// (wm-ehvlu3). The event store here fails every history load, so the test
// fails if the handler reaches for it.
func TestLexicalHandler_ReadsTheUpdateOwnerFromTheIndexRow(t *testing.T) {
	t.Parallel()

	published, tx := noteUpdate("Dunmore settlement")
	store := &fakeEventStore{
		tx:           map[string][]domain.EventEnvelope[any]{"tx2": tx},
		getEventsErr: errors.New("history must not be loaded when the row has the owner"),
	}
	index := newFakeLexicalIndex(true)
	harbor := repositories.LexicalOwner{CreatedBy: "agent-harbor", AccountID: "harbor"}
	if err := index.Index(context.Background(), "urn:note:1", "note", "old text", harbor); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	if err := lexicalHandler(store, index, noopLogger{})(context.Background(), published); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if index.indexed["urn:note:1"] != "Dunmore settlement" {
		t.Fatalf("content = %q, want the updated text", index.indexed["urn:note:1"])
	}
	if got := index.owners["urn:note:1"]; got != harbor {
		t.Errorf("owner after update = %+v, want the row's owner %+v", got, harbor)
	}
	if index.ownerLookups != 1 {
		t.Errorf("owner lookups = %d, want 1", index.ownerLookups)
	}
}

// A create carries its owner, so it never asks the index.
func TestLexicalHandler_CreateDoesNotLookUpTheOwner(t *testing.T) {
	t.Parallel()

	published, tx := ownedEpisode("urn:note:1", "tx1", "agent-harbor", "harbor")
	index := newFakeLexicalIndex(true)
	index.ownerErr = errors.New("a create must not look up its owner")
	h := lexicalHandler(&fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{"tx1": tx}},
		index, noopLogger{})
	if err := h(context.Background(), published); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if index.ownerLookups != 0 {
		t.Errorf("owner lookups = %d on a create, want 0", index.ownerLookups)
	}
}

// An index row that cannot be read is an error the subscriber retries — it
// is never a reason to index the resource as ownerless.
func TestLexicalHandler_FailsWhenTheIndexRowCannotBeRead(t *testing.T) {
	t.Parallel()

	published, tx := noteUpdate("Dunmore")
	store := &fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{"tx2": tx}}
	index := newFakeLexicalIndex(true)
	index.ownerErr = errors.New("index unavailable")
	if err := lexicalHandler(store, index, noopLogger{})(context.Background(), published); err == nil {
		t.Fatal("handler succeeded, want the lookup error so the event is retried")
	}
	if _, indexed := index.indexed["urn:note:1"]; indexed {
		t.Error("resource indexed without its owner")
	}
}
