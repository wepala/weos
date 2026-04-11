package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"weos/domain/entities"
	"weos/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// --- stubs ---

// installTestTypeRepo is a ResourceTypeRepository backed by an in-memory map.
type installTestTypeRepo struct {
	types map[string]*entities.ResourceType
}

func newInstallTestTypeRepo() *installTestTypeRepo {
	return &installTestTypeRepo{types: make(map[string]*entities.ResourceType)}
}

func (r *installTestTypeRepo) FindBySlug(_ context.Context, slug string) (*entities.ResourceType, error) {
	if rt, ok := r.types[slug]; ok {
		return rt, nil
	}
	return nil, fmt.Errorf("resource type %q: %w", slug, repositories.ErrNotFound)
}

func (r *installTestTypeRepo) Save(_ context.Context, e *entities.ResourceType) error {
	r.types[e.Slug()] = e
	return nil
}

func (r *installTestTypeRepo) FindByID(_ context.Context, id string) (*entities.ResourceType, error) {
	for _, rt := range r.types {
		if rt.GetID() == id {
			return rt, nil
		}
	}
	return nil, fmt.Errorf("resource type %q: %w", id, repositories.ErrNotFound)
}

func (r *installTestTypeRepo) FindAll(
	_ context.Context, _ string, _ int,
) (repositories.PaginatedResponse[*entities.ResourceType], error) {
	return repositories.PaginatedResponse[*entities.ResourceType]{}, nil
}

func (r *installTestTypeRepo) Update(_ context.Context, _ *entities.ResourceType) error { return nil }
func (r *installTestTypeRepo) Delete(_ context.Context, _ string) error                 { return nil }

// stubEventStore is a minimal in-memory event store for testing.
type stubEventStore struct{}

func (s *stubEventStore) Append(
	_ context.Context, _ string, _ int, _ ...domain.EventEnvelope[any],
) error {
	return nil
}
func (s *stubEventStore) GetEvents(
	_ context.Context, _ string,
) ([]domain.EventEnvelope[any], error) {
	return nil, nil
}
func (s *stubEventStore) GetEventsFromVersion(
	_ context.Context, _ string, _ int,
) ([]domain.EventEnvelope[any], error) {
	return nil, nil
}
func (s *stubEventStore) GetEventsRange(
	_ context.Context, _ string, _, _ int,
) ([]domain.EventEnvelope[any], error) {
	return nil, nil
}
func (s *stubEventStore) GetEventByID(
	_ context.Context, _ string,
) (domain.EventEnvelope[any], error) {
	return domain.EventEnvelope[any]{}, nil
}
func (s *stubEventStore) GetEventsByTransactionID(
	_ context.Context, _ string,
) ([]domain.EventEnvelope[any], error) {
	return nil, nil
}
func (s *stubEventStore) GetCurrentVersion(_ context.Context, _ string) (int, error) { return 0, nil }
func (s *stubEventStore) Close() error                                               { return nil }

// fakeResourceSvc records Create calls and can be configured to fail.
type fakeResourceSvc struct {
	created  []CreateResourceCommand
	calls    int    // total Create calls (including failures)
	failAt   int    // call index at which to fail (-1 = never)
	failSlug string // only fail for this slug
}

func newFakeResourceSvc() *fakeResourceSvc {
	return &fakeResourceSvc{failAt: -1}
}

func (f *fakeResourceSvc) Create(
	_ context.Context, cmd CreateResourceCommand,
) (*entities.Resource, error) {
	idx := f.calls
	f.calls++
	if f.failSlug == cmd.TypeSlug && idx == f.failAt {
		return nil, errors.New("seed failure")
	}
	f.created = append(f.created, cmd)
	return nil, nil //nolint:nilnil // stub
}

func (f *fakeResourceSvc) GetByID(context.Context, string) (*entities.Resource, error) {
	return nil, nil //nolint:nilnil
}
func (f *fakeResourceSvc) List(
	context.Context, string, string, int, repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	return repositories.PaginatedResponse[*entities.Resource]{}, nil
}
func (f *fakeResourceSvc) ListFlat(
	context.Context, string, string, int, repositories.SortOptions,
) (repositories.PaginatedResponse[map[string]any], error) {
	return repositories.PaginatedResponse[map[string]any]{}, nil
}
func (f *fakeResourceSvc) GetFlat(context.Context, string, string) (map[string]any, error) {
	return nil, nil
}
func (f *fakeResourceSvc) ListByField(
	context.Context, string, string, string,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	return repositories.PaginatedResponse[*entities.Resource]{}, nil
}
func (f *fakeResourceSvc) ListWithFilters(
	context.Context, string, []repositories.FilterCondition, string, int, repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	return repositories.PaginatedResponse[*entities.Resource]{}, nil
}
func (f *fakeResourceSvc) ListFlatWithFilters(
	context.Context, string, []repositories.FilterCondition, string, int, repositories.SortOptions,
) (repositories.PaginatedResponse[map[string]any], error) {
	return repositories.PaginatedResponse[map[string]any]{}, nil
}
func (f *fakeResourceSvc) Update(context.Context, UpdateResourceCommand) (*entities.Resource, error) {
	return nil, nil //nolint:nilnil
}
func (f *fakeResourceSvc) Delete(context.Context, DeleteResourceCommand) error { return nil }

// --- helpers ---

func makeInstallTestService(
	repo repositories.ResourceTypeRepository,
	resourceSvc *fakeResourceSvc,
	registry *PresetRegistry,
) ResourceTypeService {
	es := &stubEventStore{}
	d := domain.NewEventDispatcher()
	pm := &stubProjMgr{}
	// Wire up event handlers so created types are projected to the repo.
	if err := SubscribeResourceTypeHandlers(d, repo, pm, noopLogger{}); err != nil {
		panic(fmt.Sprintf("SubscribeResourceTypeHandlers failed in test setup: %v", err))
	}
	return &resourceTypeService{
		repo:        repo,
		projMgr:     pm,
		eventStore:  es,
		dispatcher:  d,
		registry:    registry,
		logger:      noopLogger{},
		resourceSvc: resourceSvc,
	}
}

func testPresetWithFixtures() PresetDefinition {
	return PresetDefinition{
		Name:        "test-fixtures",
		Description: "Preset for fixture seeding tests",
		Types: []PresetResourceType{
			{
				Name:        "Measure",
				Slug:        "measure",
				Description: "Unit of measure",
				Context:     json.RawMessage(`{"@vocab":"https://schema.org/"}`),
				Schema: json.RawMessage(`{
					"type":"object",
					"properties":{"name":{"type":"string"},"abbr":{"type":"string"}},
					"required":["name"]
				}`),
				Fixtures: []json.RawMessage{
					json.RawMessage(`{"name":"Cup","abbr":"cup"}`),
					json.RawMessage(`{"name":"Tablespoon","abbr":"tbsp"}`),
					json.RawMessage(`{"name":"Gram","abbr":"g"}`),
				},
			},
			{
				Name:        "Tag",
				Slug:        "tag",
				Description: "Simple tag",
				Context:     json.RawMessage(`{"@vocab":"https://schema.org/"}`),
				Schema: json.RawMessage(`{
					"type":"object",
					"properties":{"name":{"type":"string"}},
					"required":["name"]
				}`),
			},
		},
	}
}

// --- tests ---

func TestInstallPreset_SeedsFixturesOnCreate(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	registry.MustAdd(testPresetWithFixtures())
	repo := newInstallTestTypeRepo()
	rSvc := newFakeResourceSvc()
	svc := makeInstallTestService(repo, rSvc, registry)

	result, err := svc.InstallPreset(context.Background(), "test-fixtures", false)
	if err != nil {
		t.Fatalf("InstallPreset failed: %v", err)
	}

	// Both types should be created.
	if len(result.Created) != 2 {
		t.Fatalf("expected 2 created types, got %d: %v", len(result.Created), result.Created)
	}

	// 3 fixtures should be seeded for "measure", 0 for "tag".
	if result.Seeded == nil {
		t.Fatal("expected non-nil Seeded map")
	}
	if result.Seeded["measure"] != 3 {
		t.Fatalf("expected 3 seeded for measure, got %d", result.Seeded["measure"])
	}
	if _, ok := result.Seeded["tag"]; ok {
		t.Fatal("tag has no fixtures, should not appear in Seeded")
	}

	// Verify the resource service received correct commands.
	if len(rSvc.created) != 3 {
		t.Fatalf("expected 3 resource creates, got %d", len(rSvc.created))
	}
	for _, cmd := range rSvc.created {
		if cmd.TypeSlug != "measure" {
			t.Fatalf("expected TypeSlug 'measure', got %q", cmd.TypeSlug)
		}
	}
}

func TestInstallPreset_SkipsFixturesForExistingTypes(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	registry.MustAdd(testPresetWithFixtures())
	repo := newInstallTestTypeRepo()
	rSvc := newFakeResourceSvc()
	svc := makeInstallTestService(repo, rSvc, registry)

	// First install creates types + fixtures.
	_, err := svc.InstallPreset(context.Background(), "test-fixtures", false)
	if err != nil {
		t.Fatalf("first install failed: %v", err)
	}

	// Second install should skip all types -- no new fixtures.
	rSvc2 := newFakeResourceSvc()
	svc2 := makeInstallTestService(repo, rSvc2, registry)
	result, err := svc2.InstallPreset(context.Background(), "test-fixtures", false)
	if err != nil {
		t.Fatalf("second install failed: %v", err)
	}
	if len(result.Skipped) != 2 {
		t.Fatalf("expected 2 skipped, got %d", len(result.Skipped))
	}
	if len(rSvc2.created) != 0 {
		t.Fatalf("expected 0 resource creates on second install, got %d", len(rSvc2.created))
	}
}

func TestInstallPreset_FixtureFailureIsNonFatal(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	registry.MustAdd(testPresetWithFixtures())
	repo := newInstallTestTypeRepo()
	rSvc := newFakeResourceSvc()
	rSvc.failAt = 1 // fail on second fixture
	rSvc.failSlug = "measure"
	svc := makeInstallTestService(repo, rSvc, registry)

	result, err := svc.InstallPreset(context.Background(), "test-fixtures", false)
	if err != nil {
		t.Fatalf("InstallPreset should not fail on fixture error, got: %v", err)
	}

	// Both types should still be created.
	if len(result.Created) != 2 {
		t.Fatalf("expected 2 created types, got %d", len(result.Created))
	}

	// 2 of 3 fixtures should have been seeded (index 0 and 2 succeed, 1 fails).
	if result.Seeded["measure"] != 2 {
		t.Fatalf("expected 2 seeded for measure (1 failed), got %d", result.Seeded["measure"])
	}
}

func TestInstallPreset_GetBySlugNonNotFoundError(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	registry.MustAdd(testPresetWithFixtures())

	// Use a repo that returns a non-not-found error.
	dbErr := errors.New("database connection refused")
	repo := &failingTypeRepo{err: dbErr}
	rSvc := newFakeResourceSvc()
	svc := makeInstallTestService(repo, rSvc, registry)

	result, err := svc.InstallPreset(context.Background(), "test-fixtures", false)
	if err == nil {
		t.Fatal("expected error for non-not-found GetBySlug failure")
	}
	if !errors.Is(err, dbErr) {
		t.Fatalf("expected wrapped database error, got: %v", err)
	}
	// Result should be returned (possibly partial) even on error.
	if result == nil {
		t.Fatal("expected non-nil result even on error")
	}
}

// failingTypeRepo always returns a non-not-found error from FindBySlug.
type failingTypeRepo struct {
	err error
}

func (r *failingTypeRepo) FindBySlug(context.Context, string) (*entities.ResourceType, error) {
	return nil, r.err
}
func (r *failingTypeRepo) Save(context.Context, *entities.ResourceType) error { return nil }
func (r *failingTypeRepo) FindByID(context.Context, string) (*entities.ResourceType, error) {
	return nil, r.err
}
func (r *failingTypeRepo) FindAll(
	context.Context, string, int,
) (repositories.PaginatedResponse[*entities.ResourceType], error) {
	return repositories.PaginatedResponse[*entities.ResourceType]{}, nil
}
func (r *failingTypeRepo) Update(context.Context, *entities.ResourceType) error { return nil }
func (r *failingTypeRepo) Delete(context.Context, string) error                 { return nil }
