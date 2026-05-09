package application

import (
	"context"
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
