package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"go.uber.org/fx"
)

// fakeKGStore captures every call so tests can assert what the projector did.
type fakeKGStore struct {
	mu              sync.Mutex
	active          bool
	addCalls        [][]repositories.Triple
	removeCalls     [][]repositories.Triple
	subjectsRemoved []string
	loadedFormats   []string
	updates         []string
	clearCalls      int
	emptyResp       bool
}

func (f *fakeKGStore) Active() bool { return f.active }

func (f *fakeKGStore) AddTriples(_ context.Context, t []repositories.Triple) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addCalls = append(f.addCalls, t)
	return nil
}

func (f *fakeKGStore) RemoveTriples(_ context.Context, t []repositories.Triple) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removeCalls = append(f.removeCalls, t)
	return nil
}

func (f *fakeKGStore) RemoveSubject(_ context.Context, s string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subjectsRemoved = append(f.subjectsRemoved, s)
	return nil
}

func (f *fakeKGStore) Query(_ context.Context, _ string) (repositories.KGQueryResult, error) {
	return repositories.KGQueryResult{}, nil
}

func (f *fakeKGStore) Update(_ context.Context, q string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, q)
	return nil
}

func (f *fakeKGStore) LoadOntology(_ context.Context, format string, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loadedFormats = append(f.loadedFormats, format)
	return nil
}

func (f *fakeKGStore) Clear(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clearCalls++
	return nil
}

func (f *fakeKGStore) IsEmpty(_ context.Context) (bool, error) { return f.emptyResp, nil }

// recordingLifecycle records hooks without invoking them — keeps the unit
// test from triggering the goroutine backfill.
type recordingLifecycle struct {
	hooks []fx.Hook
}

func (r *recordingLifecycle) Append(h fx.Hook) { r.hooks = append(r.hooks, h) }

// fakeEventStore returns canned envelopes for GetEventsByTransactionID. Other
// methods panic — Resource.Published is the only path that touches the store
// during projection.
type fakeEventStore struct {
	tx     map[string][]domain.EventEnvelope[any]
	getErr error
}

func (f *fakeEventStore) Append(_ context.Context, _ string, _ int, _ ...domain.EventEnvelope[any]) error {
	panic("not used")
}
func (f *fakeEventStore) GetEvents(_ context.Context, _ string) ([]domain.EventEnvelope[any], error) {
	panic("not used")
}
func (f *fakeEventStore) GetEventsFromVersion(_ context.Context, _ string, _ int) ([]domain.EventEnvelope[any], error) {
	panic("not used")
}
func (f *fakeEventStore) GetEventsRange(_ context.Context, _ string, _, _ int) ([]domain.EventEnvelope[any], error) {
	panic("not used")
}
func (f *fakeEventStore) GetEventByID(_ context.Context, _ string) (domain.EventEnvelope[any], error) {
	panic("not used")
}
func (f *fakeEventStore) GetEventsByTransactionID(_ context.Context, txID string) ([]domain.EventEnvelope[any], error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.tx[txID], nil
}
func (f *fakeEventStore) Close() error                                               { return nil }
func (f *fakeEventStore) GetCurrentVersion(_ context.Context, _ string) (int, error) { return 0, nil }

func newProjectorParams(store repositories.KnowledgeGraphStore) SubscribeKnowledgeGraphHandlersParams {
	return SubscribeKnowledgeGraphHandlersParams{
		Lifecycle:  &recordingLifecycle{},
		Dispatcher: domain.NewEventDispatcher(),
		Store:      store,
		Logger:     noopLogger{},
	}
}

func TestSubscribeKnowledgeGraphHandlers_InactiveStoreIsNoop(t *testing.T) {
	t.Parallel()
	params := newProjectorParams(&fakeKGStore{active: false})

	if err := SubscribeKnowledgeGraphHandlers(params); err != nil {
		t.Fatalf("SubscribeKnowledgeGraphHandlers(inactive): %v", err)
	}
	// With no subscriptions registered, dispatching an event must succeed
	// silently — and the lifecycle must not have a backfill hook attached.
	lc := params.Lifecycle.(*recordingLifecycle)
	if len(lc.hooks) != 0 {
		t.Errorf("inactive store should not register backfill hook; got %d", len(lc.hooks))
	}
}

func TestSubscribeKnowledgeGraphHandlers_ProjectsTripleEvents(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true, emptyResp: false}
	params := newProjectorParams(store)

	if err := SubscribeKnowledgeGraphHandlers(params); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	ctx := context.Background()
	created := entities.TripleCreated{}.With("urn:product:1", "https://schema.org/name", "Widget")
	envCreated := domain.EventEnvelope[any]{
		AggregateID: "urn:product:1",
		EventType:   created.EventType(),
		Payload:     created,
		Created:     time.Now(),
	}
	if err := params.Dispatcher.Dispatch(ctx, envCreated); err != nil {
		t.Fatalf("dispatch Triple.Created: %v", err)
	}
	if len(store.addCalls) != 1 || len(store.addCalls[0]) != 1 {
		t.Fatalf("expected one AddTriples call with one triple; got %+v", store.addCalls)
	}
	got := store.addCalls[0][0]
	if got.Subject != "urn:product:1" || got.Predicate != "https://schema.org/name" || got.Object != "Widget" {
		t.Errorf("triple mismatch: %+v", got)
	}

	deleted := entities.TripleDeleted{}.With("urn:product:1", "https://schema.org/name", "Widget")
	envDeleted := domain.EventEnvelope[any]{
		AggregateID: "urn:product:1",
		EventType:   deleted.EventType(),
		Payload:     deleted,
		Created:     time.Now(),
	}
	if err := params.Dispatcher.Dispatch(ctx, envDeleted); err != nil {
		t.Fatalf("dispatch Triple.Deleted: %v", err)
	}
	if len(store.removeCalls) != 1 {
		t.Fatalf("expected one RemoveTriples call; got %d", len(store.removeCalls))
	}
}

func TestSubscribeKnowledgeGraphHandlers_RemovesSubjectOnResourceDeleted(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true, emptyResp: false}
	params := newProjectorParams(store)

	if err := SubscribeKnowledgeGraphHandlers(params); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	deleted := entities.ResourceDeleted{}.With()
	env := domain.EventEnvelope[any]{
		AggregateID: "urn:product:42",
		EventType:   deleted.EventType(),
		Payload:     deleted,
		Created:     time.Now(),
	}
	if err := params.Dispatcher.Dispatch(context.Background(), env); err != nil {
		t.Fatalf("dispatch Resource.Deleted: %v", err)
	}
	if len(store.subjectsRemoved) != 1 || store.subjectsRemoved[0] != "urn:product:42" {
		t.Errorf("subject removed = %v, want [urn:product:42]", store.subjectsRemoved)
	}
}

func TestSubscribeKnowledgeGraphHandlers_LoadsOntologyOnTypeCreated(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true, emptyResp: false}
	params := newProjectorParams(store)

	if err := SubscribeKnowledgeGraphHandlers(params); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	payload := entities.ResourceTypeCreated{
		Slug:    "product",
		Context: []byte(`{"@vocab":"https://schema.org/"}`),
	}
	env := domain.EventEnvelope[any]{
		AggregateID: "urn:type:product",
		EventType:   payload.EventType(),
		Payload:     payload,
		Created:     time.Now(),
	}
	if err := params.Dispatcher.Dispatch(context.Background(), env); err != nil {
		t.Fatalf("dispatch ResourceType.Created: %v", err)
	}
	if len(store.loadedFormats) != 1 || store.loadedFormats[0] != "application/ld+json" {
		t.Errorf("loaded formats = %v, want [application/ld+json]", store.loadedFormats)
	}
	// Explicit ontology triples (rdf:type rdfs:Class + rdfs:label) must be
	// emitted alongside the JSON-LD load.
	if len(store.addCalls) != 1 {
		t.Fatalf("expected 1 AddTriples call for explicit ontology triples; got %d", len(store.addCalls))
	}
	// The class is declared under the IRI resources are typed with — the
	// context @type (here absent, so the slug) expanded against @vocab —
	// NOT urn:type:product, which no resource carries.
	hasClassTriple := false
	for _, t := range store.addCalls[0] {
		if t.Subject == "https://schema.org/product" &&
			t.Predicate == "http://www.w3.org/1999/02/22-rdf-syntax-ns#type" &&
			t.Object == "http://www.w3.org/2000/01/rdf-schema#Class" {
			hasClassTriple = true
			break
		}
	}
	if !hasClassTriple {
		t.Errorf("missing rdf:type rdfs:Class triple: %+v", store.addCalls[0])
	}
}

func TestProjectResourceTypeOntology_ClearsClassSubjectBeforeReload(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true}
	rawCtx := []byte(`{"@vocab":"https://schema.org/","@type":"Product"}`)

	projectResourceTypeOntology(context.Background(), "Product", "product", rawCtx, store, noopLogger{})

	// The class subject (the IRI resources are typed with) must be cleared
	// before reload so a changed subClassOf can't leave stale triples behind.
	found := false
	for _, s := range store.subjectsRemoved {
		if s == "https://schema.org/Product" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected RemoveSubject(https://schema.org/Product) before reload; got %v", store.subjectsRemoved)
	}
}

func TestEmitExplicitOntologyTriples_IncludesSubClassOfWhenDeclared(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true}
	rawCtx := []byte(`{"@vocab":"https://schema.org/","rdfs:subClassOf":"thing"}`)

	emitExplicitOntologyTriples(context.Background(), "Product", "product", rawCtx, store, noopLogger{})

	if len(store.addCalls) != 1 {
		t.Fatalf("expected one AddTriples call; got %d", len(store.addCalls))
	}
	// Parent is expanded against the same context so the subClassOf chain links
	// to the parent's real class IRI (https://schema.org/thing), not a slug URN.
	hasSubClass := false
	for _, tr := range store.addCalls[0] {
		if tr.Predicate == "http://www.w3.org/2000/01/rdf-schema#subClassOf" &&
			tr.Object == "https://schema.org/thing" {
			hasSubClass = true
			break
		}
	}
	if !hasSubClass {
		t.Errorf("expected rdfs:subClassOf triple targeting https://schema.org/thing; got %+v", store.addCalls[0])
	}
}

func TestSubscribeKnowledgeGraphHandlers_RegistersBackfillHookWhenActive(t *testing.T) {
	t.Parallel()
	params := newProjectorParams(&fakeKGStore{active: true})

	if err := SubscribeKnowledgeGraphHandlers(params); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	lc := params.Lifecycle.(*recordingLifecycle)
	if len(lc.hooks) != 1 {
		t.Fatalf("expected exactly 1 lifecycle hook (backfill); got %d", len(lc.hooks))
	}
}

// projectResourcePublished is the most complex projector path; without these
// tests a regression would silently corrupt the graph (stale data after edits,
// or extra writes after deletes that the GORM projector handled separately).

func TestProjectResourcePublished_LoadsDataAfterClearingPriorSubject(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true}
	es := &fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{
		"tx-1": {{
			AggregateID: "urn:product:1",
			EventType:   "Resource.Created",
			SequenceNo:  1,
			Payload: map[string]any{
				"TypeSlug":  "product",
				"Data":      map[string]any{"@id": "urn:product:1", "name": "Widget"},
				"CreatedBy": "agent-1",
			},
		}},
	}}

	env := domain.EventEnvelope[entities.ResourcePublished]{
		AggregateID:   "urn:product:1",
		EventType:     "Resource.Published",
		TransactionID: "tx-1",
		SequenceNo:    1,
		Payload:       entities.ResourcePublished{}.With("product"),
	}
	projectResourcePublished(context.Background(), env, es, store, noopLogger{})

	// Replace semantic: the projector must remove the prior subject before
	// loading new data, so deleted properties don't linger.
	if len(store.subjectsRemoved) != 1 || store.subjectsRemoved[0] != "urn:product:1" {
		t.Errorf("subject remove order wrong: %v", store.subjectsRemoved)
	}
	if len(store.loadedFormats) != 1 || store.loadedFormats[0] != "application/ld+json" {
		t.Errorf("loaded formats = %v, want [application/ld+json]", store.loadedFormats)
	}
}

func TestProjectResourcePublished_EmptyTransactionIDLogsAndReturns(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true}
	es := &fakeEventStore{}

	env := domain.EventEnvelope[entities.ResourcePublished]{
		AggregateID: "urn:product:1",
		EventType:   "Resource.Published",
		// TransactionID intentionally empty
	}
	projectResourcePublished(context.Background(), env, es, store, noopLogger{})

	if len(store.loadedFormats) != 0 || len(store.subjectsRemoved) != 0 {
		t.Errorf("empty transaction id should produce no writes; got loads=%v removes=%v",
			store.loadedFormats, store.subjectsRemoved)
	}
}

func TestProjectResourcePublished_TransactionLoadErrorLogsAndReturns(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true}
	es := &fakeEventStore{getErr: errors.New("event store down")}

	env := domain.EventEnvelope[entities.ResourcePublished]{
		AggregateID:   "urn:product:1",
		EventType:     "Resource.Published",
		TransactionID: "tx-1",
	}
	projectResourcePublished(context.Background(), env, es, store, noopLogger{})

	if len(store.loadedFormats) != 0 {
		t.Errorf("event store error should suppress writes; got loads=%v", store.loadedFormats)
	}
}

func TestProjectResourcePublished_DeleteShortCircuits(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true}
	es := &fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{
		"tx-1": {{
			AggregateID: "urn:product:1",
			EventType:   "Resource.Deleted",
			SequenceNo:  3,
			Payload:     map[string]any{},
		}},
	}}

	env := domain.EventEnvelope[entities.ResourcePublished]{
		AggregateID:   "urn:product:1",
		EventType:     "Resource.Published",
		TransactionID: "tx-1",
	}
	projectResourcePublished(context.Background(), env, es, store, noopLogger{})

	if len(store.loadedFormats) != 0 {
		t.Errorf("delete envelope must not LoadOntology; got %v", store.loadedFormats)
	}
}
