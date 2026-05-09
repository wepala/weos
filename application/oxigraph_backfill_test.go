package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
)

// stubTypeRepoForBackfill is a minimal ResourceTypeRepository fake that
// returns a fixed list of types from FindAll. Other methods panic — backfill
// only uses FindAll.
type stubTypeRepoForBackfill struct {
	types []*entities.ResourceType
	err   error
}

func (s *stubTypeRepoForBackfill) Save(_ context.Context, _ *entities.ResourceType) error {
	panic("not used")
}
func (s *stubTypeRepoForBackfill) FindByID(_ context.Context, _ string) (*entities.ResourceType, error) {
	panic("not used")
}
func (s *stubTypeRepoForBackfill) FindBySlug(_ context.Context, _ string) (*entities.ResourceType, error) {
	panic("not used")
}
func (s *stubTypeRepoForBackfill) FindAll(_ context.Context, _ string, _ int) (repositories.PaginatedResponse[*entities.ResourceType], error) {
	if s.err != nil {
		return repositories.PaginatedResponse[*entities.ResourceType]{}, s.err
	}
	return repositories.PaginatedResponse[*entities.ResourceType]{Data: s.types, HasMore: false}, nil
}
func (s *stubTypeRepoForBackfill) Update(_ context.Context, _ *entities.ResourceType) error {
	panic("not used")
}
func (s *stubTypeRepoForBackfill) Delete(_ context.Context, _ string) error { panic("not used") }

// stubResourceRepoForBackfill returns the supplied resources from
// FindAllByType, keyed by type slug. Other methods panic.
type stubResourceRepoForBackfill struct {
	byType map[string][]*entities.Resource
}

func (s *stubResourceRepoForBackfill) Save(_ context.Context, _ *entities.Resource) error {
	panic("not used")
}
func (s *stubResourceRepoForBackfill) FindByID(_ context.Context, _ string) (*entities.Resource, error) {
	panic("not used")
}
func (s *stubResourceRepoForBackfill) FindAllByType(_ context.Context, slug, _ string, _ int, _ repositories.SortOptions, _ *repositories.VisibilityScope) (repositories.PaginatedResponse[*entities.Resource], error) {
	return repositories.PaginatedResponse[*entities.Resource]{Data: s.byType[slug], HasMore: false}, nil
}
func (s *stubResourceRepoForBackfill) FindAllByTypeAndField(_ context.Context, _, _, _ string) ([]*entities.Resource, error) {
	panic("not used")
}
func (s *stubResourceRepoForBackfill) FindAllByTypeWithFilters(_ context.Context, _ string, _ []repositories.FilterCondition, _ string, _ int, _ repositories.SortOptions, _ *repositories.VisibilityScope) (repositories.PaginatedResponse[*entities.Resource], error) {
	panic("not used")
}
func (s *stubResourceRepoForBackfill) Update(_ context.Context, _ *entities.Resource) error {
	panic("not used")
}
func (s *stubResourceRepoForBackfill) UpdateData(_ context.Context, _ string, _ json.RawMessage, _ int) error {
	panic("not used")
}
func (s *stubResourceRepoForBackfill) Delete(_ context.Context, _ string) error {
	panic("not used")
}
func (s *stubResourceRepoForBackfill) FindAllByTypeFlat(_ context.Context, _, _ string, _ int, _ repositories.SortOptions, _ *repositories.VisibilityScope) (repositories.PaginatedResponse[map[string]any], error) {
	panic("not used")
}
func (s *stubResourceRepoForBackfill) FindAllByTypeFlatWithFilters(_ context.Context, _ string, _ []repositories.FilterCondition, _ string, _ int, _ repositories.SortOptions, _ *repositories.VisibilityScope) (repositories.PaginatedResponse[map[string]any], error) {
	panic("not used")
}
func (s *stubResourceRepoForBackfill) FindFlatByID(_ context.Context, _, _ string) (map[string]any, error) {
	panic("not used")
}

// failingKGStore extends fakeKGStore with a configurable LoadOntology error so
// we can test the consecutive-failure abort path.
type failingKGStore struct {
	fakeKGStore
	loadErr error
}

func (f *failingKGStore) LoadOntology(_ context.Context, format string, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loadedFormats = append(f.loadedFormats, format)
	if f.loadErr != nil {
		return f.loadErr
	}
	return nil
}

func TestRunKGBackfill_SkipsWhenGraphPopulated(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true, emptyResp: false}
	params := SubscribeKnowledgeGraphHandlersParams{
		Store:        store,
		ResourceRepo: &stubResourceRepoForBackfill{},
		TypeRepo:     &stubTypeRepoForBackfill{},
		Logger:       noopLogger{},
	}

	runKGBackfill(context.Background(), params, backfillCtxOpts{rebuild: false})

	if store.clearCalls != 0 {
		t.Errorf("non-rebuild + populated graph should not Clear; got %d", store.clearCalls)
	}
	if len(store.loadedFormats) != 0 {
		t.Errorf("non-rebuild + populated graph should not load; got %v", store.loadedFormats)
	}
}

func TestRunKGBackfill_RebuildClearsAndReloads(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true, emptyResp: false}
	rt := makeRT("product", `{"@vocab":"https://schema.org/"}`)
	params := SubscribeKnowledgeGraphHandlersParams{
		Store:        store,
		ResourceRepo: &stubResourceRepoForBackfill{},
		TypeRepo:     &stubTypeRepoForBackfill{types: []*entities.ResourceType{rt}},
		Logger:       noopLogger{},
	}

	runKGBackfill(context.Background(), params, backfillCtxOpts{rebuild: true})

	if store.clearCalls != 1 {
		t.Errorf("rebuild should Clear exactly once; got %d", store.clearCalls)
	}
	// After clearing we should at least push the type's ontology context.
	if len(store.loadedFormats) == 0 {
		t.Errorf("rebuild should push at least one ontology load")
	}
}

func TestRunKGBackfill_AbortsAfterTooManyConsecutiveFailures(t *testing.T) {
	t.Parallel()
	store := &failingKGStore{
		fakeKGStore: fakeKGStore{active: true, emptyResp: true},
		loadErr:     errors.New("oxigraph down"),
	}
	rt := makeRT("product", `{}`)

	// Build N+1 resources so abort can trigger before the last one.
	resources := make([]*entities.Resource, backfillFailureAbortThreshold+5)
	for i := range resources {
		r := &entities.Resource{}
		_ = r.Restore(
			"urn:product:"+string(rune('a'+i)),
			"product",
			"active",
			[]byte(`{"@id":"urn:product:a","name":"x"}`),
			"agent",
			"acct",
			rt.CreatedAt(),
			1,
		)
		resources[i] = r
	}

	params := SubscribeKnowledgeGraphHandlersParams{
		Store:        store,
		ResourceRepo: &stubResourceRepoForBackfill{byType: map[string][]*entities.Resource{"product": resources}},
		TypeRepo:     &stubTypeRepoForBackfill{types: []*entities.ResourceType{rt}},
		Logger:       noopLogger{},
	}

	runKGBackfill(context.Background(), params, backfillCtxOpts{rebuild: false})

	// One ontology load + at most threshold resource loads before abort.
	// (Each failure increments loadedFormats because LoadOntology records
	// before returning the error.) Total <= threshold + 1 (ontology pre-load)
	// + 1 (the failing one that hits the threshold).
	maxExpected := backfillFailureAbortThreshold + 2
	if len(store.loadedFormats) > maxExpected {
		t.Errorf("expected abort after %d failures; got %d loads",
			backfillFailureAbortThreshold, len(store.loadedFormats))
	}
}

func TestRunKGBackfill_RespectsContextCancellation(t *testing.T) {
	t.Parallel()
	store := &fakeKGStore{active: true, emptyResp: true}
	rt := makeRT("product", `{}`)
	params := SubscribeKnowledgeGraphHandlersParams{
		Store:        store,
		ResourceRepo: &stubResourceRepoForBackfill{},
		TypeRepo:     &stubTypeRepoForBackfill{types: []*entities.ResourceType{rt}},
		Logger:       noopLogger{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before we run

	runKGBackfill(ctx, params, backfillCtxOpts{rebuild: false})

	// IsEmpty fires before the ctx check, but after that everything should
	// short-circuit. The clearer signal: no ontology load should happen for
	// rt.
	if len(store.loadedFormats) != 0 {
		t.Errorf("cancelled ctx should suppress backfill writes; got %v", store.loadedFormats)
	}
}
