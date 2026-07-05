package application

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// fakeKGStore captures every call so tests can assert what the projector did.
// loadErr/addErr let a test force a store failure to check error propagation.
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
	loadErr         error
	addErr          error
}

func (f *fakeKGStore) Active() bool { return f.active }

func (f *fakeKGStore) AddTriples(_ context.Context, t []repositories.Triple) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addErr != nil {
		return f.addErr
	}
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
	if f.loadErr != nil {
		return f.loadErr
	}
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

// fakeEventStore returns canned envelopes for GetEventsByTransactionID. Other
// methods panic — Resource.Published is the only path that touches the store
// during projection.
type fakeEventStore struct {
	tx           map[string][]domain.EventEnvelope[any]
	byAggregate  map[string][]domain.EventEnvelope[any]
	getErr       error
	getEventsErr error
}

func (f *fakeEventStore) Append(_ context.Context, _ string, _ int, _ ...domain.EventEnvelope[any]) error {
	panic("not used")
}
func (f *fakeEventStore) GetEvents(_ context.Context, aggregateID string) ([]domain.EventEnvelope[any], error) {
	if f.getEventsErr != nil {
		return nil, f.getEventsErr
	}
	return f.byAggregate[aggregateID], nil
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
func (f *fakeEventStore) ReadAfter(
	_ context.Context, _ int64, _ int,
) ([]domain.EventEnvelope[any], error) {
	return nil, domain.ErrGlobalOrderingNotSupported
}
func (f *fakeEventStore) HeadPosition(_ context.Context) (int64, error) {
	return 0, domain.ErrGlobalOrderingNotSupported
}

// kgTypeRepo is a ResourceTypeRepository stub whose FindByID returns a single
// configured type — the oxigraph ResourceType handler reads the type fresh by
// id rather than parsing the event payload.
type kgTypeRepo struct {
	rt  *entities.ResourceType
	err error
}

func (r *kgTypeRepo) FindByID(_ context.Context, _ string) (*entities.ResourceType, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.rt, nil
}
func (r *kgTypeRepo) FindBySlug(_ context.Context, _ string) (*entities.ResourceType, error) {
	return r.rt, nil
}
func (r *kgTypeRepo) Save(context.Context, *entities.ResourceType) error   { return nil }
func (r *kgTypeRepo) Update(context.Context, *entities.ResourceType) error { return nil }
func (r *kgTypeRepo) Delete(context.Context, string) error                 { return nil }
func (r *kgTypeRepo) FindAll(
	_ context.Context, _ string, _ int,
) (repositories.PaginatedResponse[*entities.ResourceType], error) {
	return repositories.PaginatedResponse[*entities.ResourceType]{}, nil
}

func tripleMapEnvelope(eventType, subject, predicate, object string) domain.EventEnvelope[any] {
	return domain.EventEnvelope[any]{
		AggregateID: subject,
		EventType:   eventType,
		// Payloads arrive from the store as map[string]any with lowercase keys.
		Payload: map[string]any{"subject": subject, "predicate": predicate, "object": object},
	}
}

// newTestOxigraphHandler builds the single-tenant projector handler these tests
// exercise: the fake store wrapped as the one process store (per-account routing
// has its own e2e coverage). Returns the same handler ProvideOxigraphGroup wires.
func newTestOxigraphHandler(
	es domain.EventStore,
	store repositories.KnowledgeGraphStore,
	tr repositories.ResourceTypeRepository,
	logger entities.Logger,
) func(context.Context, domain.EventEnvelope[any]) error {
	return newOxigraphProjector(es, repositories.NewSingleKnowledgeGraphStores(store), tr, logger).handle
}

func TestProvideOxigraphGroup_InactiveReturnsNil(t *testing.T) {
	t.Parallel()
	groups := ProvideOxigraphGroup(OxigraphGroupParams{
		Stores: repositories.NewSingleKnowledgeGraphStores(&fakeKGStore{active: false}),
		Logger: noopLogger{},
	})
	if groups != nil {
		t.Fatalf("inactive store should contribute no group, got %d", len(groups))
	}
}

func TestProvideOxigraphGroup_ActiveReturnsGroup(t *testing.T) {
	t.Parallel()
	groups := ProvideOxigraphGroup(OxigraphGroupParams{
		EventStore: &fakeEventStore{},
		Stores:     repositories.NewSingleKnowledgeGraphStores(&fakeKGStore{active: true}),
		TypeRepo:   &kgTypeRepo{},
		Logger:     noopLogger{},
	})
	if len(groups) != 1 || groups[0].Name != "oxigraph" || groups[0].Handler == nil {
		t.Fatalf("expected one 'oxigraph' group with a handler, got %+v", groups)
	}
}

func TestOxigraphHandler_ProjectsTripleEvents(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true}
	h := newTestOxigraphHandler(&fakeEventStore{}, store, &kgTypeRepo{}, noopLogger{})
	ctx := context.Background()

	if err := h(ctx, tripleMapEnvelope("Triple.Created", "urn:product:1", "https://schema.org/name", "Widget")); err != nil {
		t.Fatalf("Triple.Created: %v", err)
	}
	if len(store.addCalls) != 1 || len(store.addCalls[0]) != 1 {
		t.Fatalf("expected one AddTriples call with one triple; got %+v", store.addCalls)
	}
	got := store.addCalls[0][0]
	if got.Subject != "urn:product:1" || got.Predicate != "https://schema.org/name" || got.Object != "Widget" {
		t.Errorf("triple mismatch: %+v", got)
	}

	if err := h(ctx, tripleMapEnvelope("Triple.Deleted", "urn:product:1", "https://schema.org/name", "Widget")); err != nil {
		t.Fatalf("Triple.Deleted: %v", err)
	}
	if len(store.removeCalls) != 1 {
		t.Fatalf("expected one RemoveTriples call; got %d", len(store.removeCalls))
	}
}

func TestOxigraphHandler_RemovesSubjectOnResourceDeleted(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true}
	h := newTestOxigraphHandler(&fakeEventStore{}, store, &kgTypeRepo{}, noopLogger{})

	env := domain.EventEnvelope[any]{AggregateID: "urn:product:42", EventType: "Resource.Deleted"}
	if err := h(context.Background(), env); err != nil {
		t.Fatalf("Resource.Deleted: %v", err)
	}
	if len(store.subjectsRemoved) != 1 || store.subjectsRemoved[0] != "urn:product:42" {
		t.Errorf("subject removed = %v, want [urn:product:42]", store.subjectsRemoved)
	}
}

func TestOxigraphHandler_LoadsOntologyOnTypeCreated(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true}
	rt := makeRT("product", `{"@vocab":"https://schema.org/"}`)
	h := newTestOxigraphHandler(&fakeEventStore{}, store, &kgTypeRepo{rt: rt}, noopLogger{})

	env := domain.EventEnvelope[any]{AggregateID: "urn:type:product", EventType: "ResourceType.Created"}
	if err := h(context.Background(), env); err != nil {
		t.Fatalf("ResourceType.Created: %v", err)
	}
	if len(store.loadedFormats) != 1 || store.loadedFormats[0] != "application/ld+json" {
		t.Errorf("loaded formats = %v, want [application/ld+json]", store.loadedFormats)
	}
	// The class is declared under the IRI resources are typed with (the slug
	// expanded against @vocab), not urn:type:product.
	hasClassTriple := false
	for _, call := range store.addCalls {
		for _, tr := range call {
			if tr.Subject == "https://schema.org/product" &&
				tr.Predicate == "http://www.w3.org/1999/02/22-rdf-syntax-ns#type" &&
				tr.Object == "http://www.w3.org/2000/01/rdf-schema#Class" {
				hasClassTriple = true
			}
		}
	}
	if !hasClassTriple {
		t.Errorf("missing rdf:type rdfs:Class triple: %+v", store.addCalls)
	}
}

// TestOxigraphHandler_PropagatesStoreError proves a store failure surfaces as an
// error so the subscriber retries/parks the event (rather than being swallowed
// as it was on the old synchronous path).
func TestOxigraphHandler_PropagatesStoreError(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true, addErr: errors.New("oxigraph down")}
	h := newTestOxigraphHandler(&fakeEventStore{}, store, &kgTypeRepo{}, noopLogger{})

	err := h(context.Background(), tripleMapEnvelope("Triple.Created", "s", "p", "o"))
	if err == nil {
		t.Fatalf("expected store error to propagate so the subscriber retries")
	}
}

// TestOxigraphHandler_ProjectsResourcePublished exercises the Resource.Published
// branch through the handler (not just projectResourcePublished directly): it
// must load the transaction, project it, and surface a store failure.
func TestOxigraphHandler_ProjectsResourcePublished(t *testing.T) {
	t.Parallel()
	es := &fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{
		"tx-1": {{
			AggregateID: "urn:product:1",
			EventType:   "Resource.Created",
			SequenceNo:  1,
			Payload: map[string]any{
				"TypeSlug": "product",
				"Data":     map[string]any{"@id": "urn:product:1", "name": "Widget"},
			},
		}},
	}}
	store := &fakeKGStore{active: true}
	h := newTestOxigraphHandler(es, store, &kgTypeRepo{}, noopLogger{})

	env := domain.EventEnvelope[any]{
		AggregateID: "urn:product:1", EventType: "Resource.Published", TransactionID: "tx-1", SequenceNo: 1,
	}
	if err := h(context.Background(), env); err != nil {
		t.Fatalf("Resource.Published: %v", err)
	}
	if len(store.loadedFormats) != 1 {
		t.Fatalf("expected the published resource to be loaded into the graph")
	}

	// A store failure on the same path must surface so the subscriber retries.
	failStore := &fakeKGStore{active: true, loadErr: errors.New("oxigraph down")}
	hf := newTestOxigraphHandler(es, failStore, &kgTypeRepo{}, noopLogger{})
	if err := hf(context.Background(), env); err == nil {
		t.Fatalf("expected LoadOntology failure to propagate")
	}
}

// TestOxigraphHandler_ResourceTypeBranches covers the deliberate skip-vs-retry
// decision when reading the type: a since-deleted type (ErrNotFound) is skipped
// (return nil), while any other repo error propagates so the subscriber retries.
func TestOxigraphHandler_ResourceTypeBranches(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true}
	env := domain.EventEnvelope[any]{AggregateID: "urn:type:gone", EventType: "ResourceType.Created"}

	skip := newTestOxigraphHandler(&fakeEventStore{}, store, &kgTypeRepo{err: repositories.ErrNotFound}, noopLogger{})
	if err := skip(context.Background(), env); err != nil {
		t.Fatalf("deleted type should be skipped without error, got %v", err)
	}
	if len(store.loadedFormats) != 0 {
		t.Fatalf("deleted type must not be projected")
	}

	retry := newTestOxigraphHandler(&fakeEventStore{}, store, &kgTypeRepo{err: errors.New("db down")}, noopLogger{})
	if err := retry(context.Background(), env); err == nil {
		t.Fatalf("a repo error other than ErrNotFound must propagate so the subscriber retries")
	}
}

// TestOxigraphHandler_IgnoresUnrelatedEvents confirms the handler is a no-op for
// event types it does not project (the subscriber feeds it every event).
func TestOxigraphHandler_IgnoresUnrelatedEvents(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true}
	h := newTestOxigraphHandler(&fakeEventStore{}, store, &kgTypeRepo{}, noopLogger{})
	if err := h(context.Background(), domain.EventEnvelope[any]{EventType: "Something.Else"}); err != nil {
		t.Fatalf("unrelated event should be ignored: %v", err)
	}
	if len(store.addCalls)+len(store.loadedFormats)+len(store.subjectsRemoved) != 0 {
		t.Errorf("unrelated event must produce no store writes")
	}
}

func TestProjectResourceTypeOntology_ClearsClassSubjectBeforeReload(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true}
	rawCtx := []byte(`{"@vocab":"https://schema.org/","@type":"Product"}`)

	if err := projectResourceTypeOntology(context.Background(), "Product", "product", rawCtx, store, noopLogger{}); err != nil {
		t.Fatalf("projectResourceTypeOntology: %v", err)
	}

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

	if err := emitExplicitOntologyTriples(context.Background(), "Product", "product", rawCtx, store, noopLogger{}); err != nil {
		t.Fatalf("emitExplicitOntologyTriples: %v", err)
	}
	if len(store.addCalls) != 1 {
		t.Fatalf("expected one AddTriples call; got %d", len(store.addCalls))
	}
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

func resourcePublishedEnv(aggregateID, txID string, seq int) domain.EventEnvelope[any] {
	return domain.EventEnvelope[any]{
		AggregateID:   aggregateID,
		EventType:     "Resource.Published",
		TransactionID: txID,
		SequenceNo:    seq,
	}
}

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

	if err := projectResourcePublished(
		context.Background(), resourcePublishedEnv("urn:product:1", "tx-1", 1), es, store, noopLogger{},
	); err != nil {
		t.Fatalf("projectResourcePublished: %v", err)
	}

	if len(store.subjectsRemoved) != 1 || store.subjectsRemoved[0] != "urn:product:1" {
		t.Errorf("subject remove order wrong: %v", store.subjectsRemoved)
	}
	if len(store.loadedFormats) != 1 || store.loadedFormats[0] != "application/ld+json" {
		t.Errorf("loaded formats = %v, want [application/ld+json]", store.loadedFormats)
	}
}

func TestProjectResourcePublished_EmptyTransactionIDReturnsError(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true}
	es := &fakeEventStore{}

	err := projectResourcePublished(
		context.Background(), resourcePublishedEnv("urn:product:1", "", 0), es, store, noopLogger{},
	)
	if err == nil {
		t.Fatalf("empty transaction id should return an error")
	}
	if len(store.loadedFormats) != 0 || len(store.subjectsRemoved) != 0 {
		t.Errorf("empty transaction id should produce no writes")
	}
}

func TestProjectResourcePublished_TransactionLoadErrorReturns(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true}
	es := &fakeEventStore{getErr: errors.New("event store down")}

	err := projectResourcePublished(
		context.Background(), resourcePublishedEnv("urn:product:1", "tx-1", 0), es, store, noopLogger{},
	)
	if err == nil {
		t.Fatalf("event store error should propagate")
	}
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

	if err := projectResourcePublished(
		context.Background(), resourcePublishedEnv("urn:product:1", "tx-1", 0), es, store, noopLogger{},
	); err != nil {
		t.Fatalf("delete should short-circuit without error: %v", err)
	}
	if len(store.loadedFormats) != 0 {
		t.Errorf("delete envelope must not LoadOntology; got %v", store.loadedFormats)
	}
}
