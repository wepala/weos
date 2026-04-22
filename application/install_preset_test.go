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

// testPresetSingle lets update-path tests vary exactly one field at a time.
func testPresetSingle(name, slug, desc, ctx, schema string) PresetDefinition {
	return PresetDefinition{
		Name: "single",
		Types: []PresetResourceType{{
			Name:        name,
			Slug:        slug,
			Description: desc,
			Context:     json.RawMessage(ctx),
			Schema:      json.RawMessage(schema),
		}},
	}
}

// countingUpdateSub lets tests assert the unchanged-path emits zero events.
func countingUpdateSub(t *testing.T, d *domain.EventDispatcher) *int {
	t.Helper()
	count := 0
	err := domain.Subscribe(d, "ResourceType.Updated",
		func(_ context.Context, _ domain.EventEnvelope[entities.ResourceTypeUpdated]) error {
			count++
			return nil
		},
	)
	if err != nil {
		t.Fatalf("Subscribe ResourceType.Updated: %v", err)
	}
	return &count
}

func TestInstallPreset_UpdateNoOpWhenUnchanged(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	registry.MustAdd(testPresetSingle(
		"Product", "product", "A product",
		`{"@vocab":"https://schema.org/"}`,
		`{"type":"object","properties":{"name":{"type":"string"}}}`,
	))

	repo := newInstallTestTypeRepo()
	rSvc := newFakeResourceSvc()

	// First install creates the type.
	svc := makeInstallTestService(repo, rSvc, registry)
	first, err := svc.InstallPreset(context.Background(), "single", true)
	if err != nil {
		t.Fatalf("first install: %v", err)
	}
	if len(first.Created) != 1 {
		t.Fatalf("expected 1 created on first install, got %+v", first.Created)
	}

	// Second install with update=true against the same preset should no-op.
	// Build a fresh service so its dispatcher only sees events from this run.
	es := &stubEventStore{}
	d := domain.NewEventDispatcher()
	pm := &stubProjMgr{}
	if err := SubscribeResourceTypeHandlers(d, repo, pm, noopLogger{}); err != nil {
		t.Fatalf("SubscribeResourceTypeHandlers: %v", err)
	}
	updateCount := countingUpdateSub(t, d)
	svc2 := &resourceTypeService{
		repo: repo, projMgr: pm, eventStore: es, dispatcher: d,
		registry: registry, logger: noopLogger{}, resourceSvc: rSvc,
	}

	second, err := svc2.InstallPreset(context.Background(), "single", true)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if len(second.Unchanged) != 1 || second.Unchanged[0] != "product" {
		t.Fatalf("expected Unchanged=[product], got %+v", second.Unchanged)
	}
	if len(second.Updated) != 0 {
		t.Fatalf("expected no Updated entries, got %+v", second.Updated)
	}
	if len(second.Created) != 0 {
		t.Fatalf("expected no Created entries, got %+v", second.Created)
	}
	if *updateCount != 0 {
		t.Fatalf("expected zero ResourceTypeUpdated events, got %d", *updateCount)
	}

	// Whitespace/key-order-only differences must still no-op end-to-end.
	registry2 := NewPresetRegistry()
	registry2.MustAdd(testPresetSingle(
		"Product", "product", "A product",
		`{ "@vocab" : "https://schema.org/" }`,
		"{\n  \"properties\":{\"name\":{\"type\":\"string\"}},\n  \"type\":\"object\"\n}",
	))
	svc3 := &resourceTypeService{
		repo: repo, projMgr: pm, eventStore: es, dispatcher: d,
		registry: registry2, logger: noopLogger{}, resourceSvc: rSvc,
	}
	third, err := svc3.InstallPreset(context.Background(), "single", true)
	if err != nil {
		t.Fatalf("third install: %v", err)
	}
	if len(third.Unchanged) != 1 {
		t.Fatalf("whitespace/key-order differences should not trigger update, got %+v", third)
	}
	if *updateCount != 0 {
		t.Fatalf("expected still zero events after formatting-only diff, got %d", *updateCount)
	}
}

func TestInstallPreset_UpdateWhenFieldChanges(t *testing.T) {
	t.Parallel()

	base := func() PresetDefinition {
		return testPresetSingle(
			"Product", "product", "A product",
			`{"@vocab":"https://schema.org/"}`,
			`{"type":"object","properties":{"name":{"type":"string"}}}`,
		)
	}

	mutations := map[string]func(p *PresetResourceType){
		"name":        func(p *PresetResourceType) { p.Name = "Widget" },
		"description": func(p *PresetResourceType) { p.Description = "A widget" },
		"context": func(p *PresetResourceType) {
			p.Context = json.RawMessage(`{"@vocab":"https://example.com/"}`)
		},
		"schema": func(p *PresetResourceType) {
			p.Schema = json.RawMessage(`{"type":"object","properties":{"sku":{"type":"string"}}}`)
		},
	}

	for field, mutate := range mutations {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			// Seed: create the resource type via a first install.
			seedReg := NewPresetRegistry()
			seedReg.MustAdd(base())
			repo := newInstallTestTypeRepo()
			rSvc := newFakeResourceSvc()
			if _, err := makeInstallTestService(repo, rSvc, seedReg).
				InstallPreset(context.Background(), "single", false); err != nil {
				t.Fatalf("seed install: %v", err)
			}

			// Registry whose preset mutates just one field.
			mutated := base()
			mutate(&mutated.Types[0])
			reg := NewPresetRegistry()
			reg.MustAdd(mutated)

			es := &stubEventStore{}
			d := domain.NewEventDispatcher()
			pm := &stubProjMgr{}
			if err := SubscribeResourceTypeHandlers(d, repo, pm, noopLogger{}); err != nil {
				t.Fatalf("SubscribeResourceTypeHandlers: %v", err)
			}
			updateCount := countingUpdateSub(t, d)
			svc := &resourceTypeService{
				repo: repo, projMgr: pm, eventStore: es, dispatcher: d,
				registry: reg, logger: noopLogger{}, resourceSvc: rSvc,
			}

			res, err := svc.InstallPreset(context.Background(), "single", true)
			if err != nil {
				t.Fatalf("install with mutated %s: %v", field, err)
			}
			if len(res.Updated) != 1 || res.Updated[0] != "product" {
				t.Fatalf("expected Updated=[product] when %s changed, got %+v", field, res)
			}
			if len(res.Unchanged) != 0 {
				t.Fatalf("expected empty Unchanged when %s changed, got %+v", field, res.Unchanged)
			}
			if *updateCount != 1 {
				t.Fatalf("expected exactly 1 ResourceTypeUpdated event when %s changed, got %d",
					field, *updateCount)
			}
		})
	}
}

func TestJSONEquivalent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		a, b json.RawMessage
		want bool
	}{
		{"both empty string", json.RawMessage(""), json.RawMessage(""), true},
		{"both nil", nil, nil, true},
		{"nil vs empty slice", nil, json.RawMessage{}, true},
		{"one empty", json.RawMessage(""), json.RawMessage(`{"a":1}`), false},
		{"null literal vs empty", json.RawMessage(`null`), json.RawMessage(""), false},
		{"null literal vs null literal", json.RawMessage(`null`), json.RawMessage(`null`), true},
		{"identical", json.RawMessage(`{"a":1}`), json.RawMessage(`{"a":1}`), true},
		{"whitespace only", json.RawMessage(`{"a":1}`), json.RawMessage(`{ "a" : 1 }`), true},
		{"key order", json.RawMessage(`{"a":1,"b":2}`), json.RawMessage(`{"b":2,"a":1}`), true},
		{"value differs", json.RawMessage(`{"a":1}`), json.RawMessage(`{"a":2}`), false},
		{"nested key order", json.RawMessage(`{"x":{"a":1,"b":2}}`), json.RawMessage(`{"x":{"b":2,"a":1}}`), true},
		{"array order matters", json.RawMessage(`[1,2]`), json.RawMessage(`[2,1]`), false},
		// Malformed JSON must never be treated as equivalent — force the
		// non-match path so downstream validation runs (see jsonEquivalent).
		{"malformed vs identical bytes", json.RawMessage(`{bad`), json.RawMessage(`{bad`), false},
		{"malformed vs valid", json.RawMessage(`{bad`), json.RawMessage(`{"a":1}`), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := jsonEquivalent(tc.a, tc.b)
			if got != tc.want {
				t.Fatalf("jsonEquivalent(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// Regression guard: update=false must route an existing matching type to
// Skipped, never to Unchanged — Unchanged is reserved for the update=true
// no-op path. Without this, a future refactor that reorders the cases in
// InstallPreset would silently change semantics.
func TestInstallPreset_UpdateFalseSkipsNotUnchanged(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	registry.MustAdd(testPresetSingle(
		"Product", "product", "A product",
		`{"@vocab":"https://schema.org/"}`,
		`{"type":"object","properties":{"name":{"type":"string"}}}`,
	))
	repo := newInstallTestTypeRepo()
	rSvc := newFakeResourceSvc()

	svc := makeInstallTestService(repo, rSvc, registry)
	if _, err := svc.InstallPreset(context.Background(), "single", false); err != nil {
		t.Fatalf("seed install: %v", err)
	}
	res, err := makeInstallTestService(repo, rSvc, registry).
		InstallPreset(context.Background(), "single", false)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if len(res.Skipped) != 1 || res.Skipped[0] != "product" {
		t.Fatalf("expected Skipped=[product] with update=false, got %+v", res)
	}
	if len(res.Unchanged) != 0 {
		t.Fatalf("update=false must never produce Unchanged, got %+v", res.Unchanged)
	}
}

// A single install must correctly distribute types across Created, Updated,
// and Unchanged in one pass — independent accumulation, no spillover.
func TestInstallPreset_MixedBatchCreatesUpdatesAndUnchanged(t *testing.T) {
	t.Parallel()

	seedReg := NewPresetRegistry()
	seedReg.MustAdd(PresetDefinition{
		Name: "mix",
		Types: []PresetResourceType{
			{
				Name: "Stable", Slug: "stable", Description: "won't change",
				Context: json.RawMessage(`{"@vocab":"https://schema.org/"}`),
				Schema:  json.RawMessage(`{"type":"object"}`),
			},
			{
				Name: "Mutating", Slug: "mutating", Description: "old desc",
				Context: json.RawMessage(`{"@vocab":"https://schema.org/"}`),
				Schema:  json.RawMessage(`{"type":"object"}`),
			},
		},
	})
	repo := newInstallTestTypeRepo()
	rSvc := newFakeResourceSvc()
	if _, err := makeInstallTestService(repo, rSvc, seedReg).
		InstallPreset(context.Background(), "mix", false); err != nil {
		t.Fatalf("seed install: %v", err)
	}

	// Second preset: same "stable", mutated "mutating", brand-new "fresh".
	reg := NewPresetRegistry()
	reg.MustAdd(PresetDefinition{
		Name: "mix",
		Types: []PresetResourceType{
			{
				Name: "Stable", Slug: "stable", Description: "won't change",
				Context: json.RawMessage(`{"@vocab":"https://schema.org/"}`),
				Schema:  json.RawMessage(`{"type":"object"}`),
			},
			{
				Name: "Mutating", Slug: "mutating", Description: "new desc",
				Context: json.RawMessage(`{"@vocab":"https://schema.org/"}`),
				Schema:  json.RawMessage(`{"type":"object"}`),
			},
			{
				Name: "Fresh", Slug: "fresh", Description: "new",
				Context: json.RawMessage(`{"@vocab":"https://schema.org/"}`),
				Schema:  json.RawMessage(`{"type":"object"}`),
			},
		},
	})
	res, err := makeInstallTestService(repo, rSvc, reg).
		InstallPreset(context.Background(), "mix", true)
	if err != nil {
		t.Fatalf("mixed install: %v", err)
	}
	if len(res.Created) != 1 || res.Created[0] != "fresh" {
		t.Fatalf("expected Created=[fresh], got %+v", res.Created)
	}
	if len(res.Updated) != 1 || res.Updated[0] != "mutating" {
		t.Fatalf("expected Updated=[mutating], got %+v", res.Updated)
	}
	if len(res.Unchanged) != 1 || res.Unchanged[0] != "stable" {
		t.Fatalf("expected Unchanged=[stable], got %+v", res.Unchanged)
	}
}

// The Unchanged path must not touch Status — the comment on
// presetMatchesResourceType says Status is carried over; pin that the
// stored Status is still intact after a no-op install.
func TestInstallPreset_UnchangedPreservesStatus(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	registry.MustAdd(testPresetSingle(
		"Product", "product", "A product",
		`{"@vocab":"https://schema.org/"}`,
		`{"type":"object"}`,
	))
	repo := newInstallTestTypeRepo()
	rSvc := newFakeResourceSvc()
	if _, err := makeInstallTestService(repo, rSvc, registry).
		InstallPreset(context.Background(), "single", false); err != nil {
		t.Fatalf("seed install: %v", err)
	}
	// Stamp a non-default status on the stored entity to prove the no-op
	// path doesn't clobber it.
	stored := repo.types["product"]
	if err := stored.Restore(
		stored.GetID(), stored.Name(), stored.Slug(), stored.Description(),
		"archived", stored.Context(), stored.Schema(),
		stored.CreatedAt(), 99,
	); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if _, err := makeInstallTestService(repo, rSvc, registry).
		InstallPreset(context.Background(), "single", true); err != nil {
		t.Fatalf("second install: %v", err)
	}
	if got := repo.types["product"].Status(); got != "archived" {
		t.Fatalf("Unchanged path must preserve Status, got %q", got)
	}
}
