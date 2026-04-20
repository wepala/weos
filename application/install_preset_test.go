package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

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
	items := make([]*entities.ResourceType, 0, len(r.types))
	for _, rt := range r.types {
		items = append(items, rt)
	}
	return repositories.PaginatedResponse[*entities.ResourceType]{
		Data: items, HasMore: false,
	}, nil
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

// End-to-end: a preset declaring both Types and Links → InstallPreset
// creates the types, then the LinkActivator reconciles and asks the
// ProjectionManager to RegisterLink for any link whose endpoints are both
// installed. Covers the "finance-education integration" use case from #328.
func TestInstallPreset_ActivatesCrossPresetLinkViaActivator(t *testing.T) {
	t.Parallel()
	// Two separate presets — one per type — plus a third "integration" preset
	// that declares the link between them. Exactly the decoupling the issue
	// describes: neither finance nor education knows about the other, and the
	// link lives in a package that depends on both.
	financePreset := PresetDefinition{
		Name: "finance",
		Types: []PresetResourceType{
			{
				Name: "Invoice", Slug: "invoice",
				Context: json.RawMessage(`{"@vocab":"https://schema.org/"}`),
				Schema:  json.RawMessage(`{"type":"object","properties":{"amount":{"type":"number"}}}`),
			},
		},
	}
	educationPreset := PresetDefinition{
		Name: "education",
		Types: []PresetResourceType{
			{
				Name: "Guardian", Slug: "guardian",
				Context: json.RawMessage(`{"@vocab":"https://schema.org/"}`),
				Schema:  json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`),
			},
		},
	}
	integrationPreset := PresetDefinition{
		Name:  "finance-education",
		Types: nil, // no types of its own — only the link.
		Links: []PresetLinkDefinition{
			{
				Name: "invoice-guardian", SourceType: "invoice", TargetType: "guardian",
				PropertyName: "guardian", DisplayProperty: "name",
			},
		},
	}

	registry := NewPresetRegistry()
	registry.MustAdd(financePreset)
	registry.MustAdd(educationPreset)
	registry.MustAdd(integrationPreset)

	// Build a link registry seeded from the presets — mirrors buildLinkRegistry.
	links := NewLinkRegistry()
	for _, def := range registry.List() {
		for _, l := range def.Links {
			if err := links.Add(l); err != nil {
				t.Fatalf("Add link: %v", err)
			}
		}
	}

	repo := newInstallTestTypeRepo()
	pm := &recordingProjMgr{}
	activator, err := NewLinkActivator(links, pm, repo, noopLogger{})
	if err != nil {
		t.Fatalf("NewLinkActivator: %v", err)
	}

	// Helper builds a resourceTypeService wired with the activator and a
	// dispatcher that projects Save events back into repo.
	makeSvc := func() ResourceTypeService {
		es := &stubEventStore{}
		d := domain.NewEventDispatcher()
		projMgrStub := &stubProjMgr{}
		if err := SubscribeResourceTypeHandlers(d, repo, projMgrStub, noopLogger{}); err != nil {
			t.Fatalf("SubscribeResourceTypeHandlers: %v", err)
		}
		return &resourceTypeService{
			repo: repo, projMgr: projMgrStub, eventStore: es, dispatcher: d,
			registry: registry, logger: noopLogger{}, resourceSvc: newFakeResourceSvc(),
			linkActivator: activator,
		}
	}

	ctx := context.Background()

	// Install finance first — guardian isn't installed yet, so the link stays
	// dormant even though finance's install triggers a reconcile.
	svc := makeSvc()
	if _, err := svc.InstallPreset(ctx, "finance", false); err != nil {
		t.Fatalf("install finance: %v", err)
	}
	if pm.callCount() != 0 {
		t.Fatalf("expected dormant link after finance only, got %d RegisterLink calls", pm.callCount())
	}

	// Installing education completes both endpoints — the reconcile during
	// education's install should activate the invoice→guardian link.
	if _, err := svc.InstallPreset(ctx, "education", false); err != nil {
		t.Fatalf("install education: %v", err)
	}
	if pm.callCount() != 1 {
		t.Fatalf("expected 1 RegisterLink call after both presets installed, got %d", pm.callCount())
	}
	call := pm.calls[0]
	if call.SourceSlug != "invoice" || call.TargetSlug != "guardian" ||
		call.PropertyName != "guardian" || call.DisplayProperty != "name" {
		t.Errorf("unexpected link call: %+v", call)
	}

	// Installing the integration preset (which adds zero new types) runs a
	// reconcile that finds the link already active; RegisterLink is called
	// again but the ProjectionManager side dedups — covered elsewhere.
	if _, err := svc.InstallPreset(ctx, "finance-education", false); err != nil {
		t.Fatalf("install finance-education: %v", err)
	}
}

// Reconcile failures inside InstallPreset must not be silently swallowed —
// they surface on the result's Warnings slice so API/CLI callers can tell
// the admin the install was partial.
func TestInstallPreset_ReconcileFailureSurfacesAsWarning(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	registry.MustAdd(PresetDefinition{
		Name: "finance",
		Types: []PresetResourceType{{
			Name: "Invoice", Slug: "invoice",
			Context: json.RawMessage(`{"@vocab":"https://schema.org/"}`),
			Schema:  json.RawMessage(`{"type":"object","properties":{"amount":{"type":"number"}}}`),
		}},
		Links: []PresetLinkDefinition{{
			SourceType: "invoice", TargetType: "invoice",
			PropertyName: "parent", DisplayProperty: "name",
		}},
	})
	links := NewLinkRegistry()
	for _, def := range registry.List() {
		for _, l := range def.Links {
			_ = links.Add(l)
		}
	}

	repo := newInstallTestTypeRepo()
	// Force every RegisterLink call to fail.
	pm := &recordingProjMgr{errors: map[string]error{"invoice": errors.New("boom")}}
	activator, err := NewLinkActivator(links, pm, repo, noopLogger{})
	if err != nil {
		t.Fatalf("NewLinkActivator: %v", err)
	}

	es := &stubEventStore{}
	d := domain.NewEventDispatcher()
	projMgrStub := &stubProjMgr{}
	if err := SubscribeResourceTypeHandlers(d, repo, projMgrStub, noopLogger{}); err != nil {
		t.Fatalf("SubscribeResourceTypeHandlers: %v", err)
	}
	svc := &resourceTypeService{
		repo: repo, projMgr: projMgrStub, eventStore: es, dispatcher: d,
		registry: registry, logger: noopLogger{}, resourceSvc: newFakeResourceSvc(),
		linkActivator: activator,
	}

	result, err := svc.InstallPreset(context.Background(), "finance", false)
	if err != nil {
		t.Fatalf("InstallPreset itself should not error: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Fatalf("expected reconcile failure to surface as a warning, got none")
	}
	if len(result.Created) != 1 {
		t.Errorf("expected the type to still be created, got %+v", result.Created)
	}
}
