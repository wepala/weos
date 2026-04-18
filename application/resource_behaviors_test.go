package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/pkg/jsonld"

	"github.com/akeemphilbert/pericarp/pkg/auth"
)

// stubTypeRepo implements ResourceTypeRepository for testing behavior hierarchy.
type stubTypeRepo struct {
	types map[string]*entities.ResourceType
}

func (r *stubTypeRepo) FindBySlug(_ context.Context, slug string) (*entities.ResourceType, error) {
	if rt, ok := r.types[slug]; ok {
		return rt, nil
	}
	return nil, errors.New("not found")
}

func (r *stubTypeRepo) Save(context.Context, *entities.ResourceType) error { return nil }
func (r *stubTypeRepo) FindByID(context.Context, string) (*entities.ResourceType, error) {
	return nil, errors.New("not implemented")
}
func (r *stubTypeRepo) FindAll(
	_ context.Context, _ string, _ int,
) (repositories.PaginatedResponse[*entities.ResourceType], error) {
	return repositories.PaginatedResponse[*entities.ResourceType]{}, nil
}
func (r *stubTypeRepo) Update(context.Context, *entities.ResourceType) error { return nil }
func (r *stubTypeRepo) Delete(context.Context, string) error                 { return nil }

// noopLogger implements entities.Logger as a no-op.
type noopLogger struct{}

func (noopLogger) Info(context.Context, string, ...any)  {}
func (noopLogger) Warn(context.Context, string, ...any)  {}
func (noopLogger) Error(context.Context, string, ...any) {}

// trackBehavior records calls for assertion.
type trackBehavior struct {
	entities.DefaultBehavior
	label string
	calls *[]string
}

func (b *trackBehavior) BeforeCreate(
	_ context.Context, data json.RawMessage, _ *entities.ResourceType,
) (json.RawMessage, error) {
	*b.calls = append(*b.calls, b.label+".BeforeCreate")
	return data, nil
}

func (b *trackBehavior) AfterCreate(_ context.Context, _ *entities.Resource) error {
	*b.calls = append(*b.calls, b.label+".AfterCreate")
	return nil
}

func makeTestRT(slug string, ctx json.RawMessage) *entities.ResourceType {
	rt := &entities.ResourceType{}
	schema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)
	if _, err := rt.With("Test "+slug, slug, "test", ctx, schema); err != nil {
		panic(err)
	}
	return rt
}

func vfContext(extra string) json.RawMessage {
	ctx := `{"@vocab":"https://valueflows.org/"`
	if extra != "" {
		ctx += "," + extra
	}
	ctx += "}"
	return json.RawMessage(ctx)
}

func subClassOf(parent string) string {
	return `"rdfs:subClassOf":"` + parent + `"`
}

func TestBehaviorFor_NoInheritance(t *testing.T) {
	var calls []string
	rt := makeTestRT("person", json.RawMessage(`{"@vocab":"https://schema.org/"}`))

	svc := &resourceService{
		typeRepo:  &stubTypeRepo{types: map[string]*entities.ResourceType{"person": rt}},
		logger:    noopLogger{},
		behaviors: ResourceBehaviorRegistry{"person": &trackBehavior{label: "person", calls: &calls}},
	}

	behavior := svc.behaviorFor(context.Background(), rt)
	_, _ = behavior.BeforeCreate(context.Background(), json.RawMessage(`{"name":"test"}`), rt)

	if len(calls) != 1 || calls[0] != "person.BeforeCreate" {
		t.Errorf("expected [person.BeforeCreate], got %v", calls)
	}
}

func TestBehaviorFor_SingleInheritance(t *testing.T) {
	var calls []string
	commitmentRT := makeTestRT("commitment", vfContext(""))
	invoiceRT := makeTestRT("invoice", vfContext(subClassOf("commitment")))

	repo := &stubTypeRepo{types: map[string]*entities.ResourceType{
		"invoice":    invoiceRT,
		"commitment": commitmentRT,
	}}

	svc := &resourceService{
		typeRepo: repo,
		logger:   noopLogger{},
		behaviors: ResourceBehaviorRegistry{
			"invoice":    &trackBehavior{label: "invoice", calls: &calls},
			"commitment": &trackBehavior{label: "commitment", calls: &calls},
		},
	}

	behavior := svc.behaviorFor(context.Background(), invoiceRT)
	_, _ = behavior.BeforeCreate(context.Background(), json.RawMessage(`{"name":"test"}`), invoiceRT)

	if len(calls) != 2 || calls[0] != "invoice.BeforeCreate" || calls[1] != "commitment.BeforeCreate" {
		t.Errorf("expected [invoice.BeforeCreate, commitment.BeforeCreate], got %v", calls)
	}
}

func TestBehaviorFor_ParentBehaviorOnly(t *testing.T) {
	var calls []string
	commitmentRT := makeTestRT("commitment", vfContext(""))
	leaseRT := makeTestRT("lease", vfContext(subClassOf("commitment")))

	repo := &stubTypeRepo{types: map[string]*entities.ResourceType{
		"lease":      leaseRT,
		"commitment": commitmentRT,
	}}

	svc := &resourceService{
		typeRepo: repo,
		logger:   noopLogger{},
		behaviors: ResourceBehaviorRegistry{
			"commitment": &trackBehavior{label: "commitment", calls: &calls},
		},
	}

	behavior := svc.behaviorFor(context.Background(), leaseRT)
	_, _ = behavior.BeforeCreate(context.Background(), json.RawMessage(`{"name":"test"}`), leaseRT)

	if len(calls) != 1 || calls[0] != "commitment.BeforeCreate" {
		t.Errorf("expected [commitment.BeforeCreate], got %v", calls)
	}
}

func TestBehaviorFor_ChainOfThree(t *testing.T) {
	var calls []string
	actionRT := makeTestRT("action", vfContext(""))
	commitmentRT := makeTestRT("commitment", vfContext(subClassOf("action")))
	invoiceRT := makeTestRT("invoice", vfContext(subClassOf("commitment")))

	repo := &stubTypeRepo{types: map[string]*entities.ResourceType{
		"invoice":    invoiceRT,
		"commitment": commitmentRT,
		"action":     actionRT,
	}}

	svc := &resourceService{
		typeRepo: repo,
		logger:   noopLogger{},
		behaviors: ResourceBehaviorRegistry{
			"invoice":    &trackBehavior{label: "invoice", calls: &calls},
			"commitment": &trackBehavior{label: "commitment", calls: &calls},
			"action":     &trackBehavior{label: "action", calls: &calls},
		},
	}

	behavior := svc.behaviorFor(context.Background(), invoiceRT)
	_, _ = behavior.BeforeCreate(context.Background(), json.RawMessage(`{"name":"test"}`), invoiceRT)

	expected := []string{"invoice.BeforeCreate", "commitment.BeforeCreate", "action.BeforeCreate"}
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %v", calls)
	}
	for i, want := range expected {
		if calls[i] != want {
			t.Errorf("call[%d] = %q, want %q", i, calls[i], want)
		}
	}
}

func TestBehaviorFor_CircularReference(t *testing.T) {
	aRT := makeTestRT("type-a", json.RawMessage(`{"rdfs:subClassOf":"type-b"}`))
	bRT := makeTestRT("type-b", json.RawMessage(`{"rdfs:subClassOf":"type-a"}`))

	repo := &stubTypeRepo{types: map[string]*entities.ResourceType{
		"type-a": aRT,
		"type-b": bRT,
	}}

	var calls []string
	svc := &resourceService{
		typeRepo: repo,
		logger:   noopLogger{},
		behaviors: ResourceBehaviorRegistry{
			"type-a": &trackBehavior{label: "a", calls: &calls},
			"type-b": &trackBehavior{label: "b", calls: &calls},
		},
	}

	// Should not infinite loop — visited set breaks the cycle
	behavior := svc.behaviorFor(context.Background(), aRT)
	_, _ = behavior.BeforeCreate(context.Background(), json.RawMessage(`{"name":"test"}`), aRT)

	// a fires, then b fires (b's parent is a, but a is already visited)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls (cycle broken), got %v", calls)
	}
}

func TestBehaviorFor_ParentNotFound(t *testing.T) {
	var calls []string
	invoiceRT := makeTestRT("invoice", json.RawMessage(`{"rdfs:subClassOf":"nonexistent"}`))

	repo := &stubTypeRepo{types: map[string]*entities.ResourceType{
		"invoice": invoiceRT,
	}}

	svc := &resourceService{
		typeRepo: repo,
		logger:   noopLogger{},
		behaviors: ResourceBehaviorRegistry{
			"invoice": &trackBehavior{label: "invoice", calls: &calls},
		},
	}

	behavior := svc.behaviorFor(context.Background(), invoiceRT)
	_, _ = behavior.BeforeCreate(context.Background(), json.RawMessage(`{"name":"test"}`), invoiceRT)

	if len(calls) != 1 || calls[0] != "invoice.BeforeCreate" {
		t.Errorf("expected [invoice.BeforeCreate], got %v", calls)
	}
}

func TestBehaviorFor_NilRT(t *testing.T) {
	svc := &resourceService{
		logger:    noopLogger{},
		behaviors: make(ResourceBehaviorRegistry),
	}

	behavior := svc.behaviorFor(context.Background(), nil)
	data, err := behavior.BeforeCreate(context.Background(), json.RawMessage(`{"name":"test"}`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != `{"name":"test"}` {
		t.Errorf("expected pass-through, got %s", data)
	}
}

func TestSubClassOf_UsedByBehaviorFor(t *testing.T) {
	// Verify the jsonld.SubClassOf function is correctly used
	ctx := vfContext(subClassOf("commitment"))
	parent := jsonld.SubClassOf(ctx)
	if parent != "commitment" {
		t.Errorf("SubClassOf = %q, want %q", parent, "commitment")
	}
}

// --- Stub BehaviorSettingsRepository for tests ---

type stubBehaviorSettings struct {
	data map[string][]string // key = accountID+"|"+typeSlug
	err  error
}

func (s *stubBehaviorSettings) GetByAccountAndType(
	_ context.Context, accountID, typeSlug string,
) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	slugs, ok := s.data[accountID+"|"+typeSlug]
	if !ok {
		return nil, nil
	}
	return slugs, nil
}

func (s *stubBehaviorSettings) SaveByAccountAndType(
	_ context.Context, accountID, typeSlug string, slugs []string,
) error {
	if s.err != nil {
		return s.err
	}
	if s.data == nil {
		s.data = make(map[string][]string)
	}
	s.data[accountID+"|"+typeSlug] = slugs
	return nil
}

func withAccount(ctx context.Context, agentID, accountID string) context.Context {
	return auth.ContextWithAgent(ctx, &auth.Identity{
		AgentID:         agentID,
		AccountIDs:      []string{accountID},
		ActiveAccountID: accountID,
	})
}

// --- Account-scoped behavior filtering tests ---

func TestBehaviorFor_AccountOverrideDisablesBehavior(t *testing.T) {
	var calls []string
	rt := makeTestRT("person", json.RawMessage(`{"@vocab":"https://schema.org/"}`))

	settings := &stubBehaviorSettings{
		data: map[string][]string{
			"acct1|person": {}, // empty list = all disabled
		},
	}

	svc := &resourceService{
		typeRepo:         &stubTypeRepo{types: map[string]*entities.ResourceType{"person": rt}},
		logger:           noopLogger{},
		behaviors:        ResourceBehaviorRegistry{"person": &trackBehavior{label: "person", calls: &calls}},
		behaviorMeta:     BehaviorMetaRegistry{"person": entities.BehaviorMeta{Slug: "person", Default: true, Manageable: true}},
		behaviorSettings: settings,
	}

	ctx := withAccount(context.Background(), "agent1", "acct1")
	behavior := svc.behaviorFor(ctx, rt)
	_, _ = behavior.BeforeCreate(ctx, json.RawMessage(`{"name":"test"}`), rt)

	if len(calls) != 0 {
		t.Errorf("expected no calls (behavior disabled by account), got %v", calls)
	}
}

func TestBehaviorFor_NoAccountOverrideUsesPresetDefaults(t *testing.T) {
	var calls []string
	rt := makeTestRT("person", json.RawMessage(`{"@vocab":"https://schema.org/"}`))

	settings := &stubBehaviorSettings{} // no overrides stored

	svc := &resourceService{
		typeRepo:         &stubTypeRepo{types: map[string]*entities.ResourceType{"person": rt}},
		logger:           noopLogger{},
		behaviors:        ResourceBehaviorRegistry{"person": &trackBehavior{label: "person", calls: &calls}},
		behaviorMeta:     BehaviorMetaRegistry{"person": entities.BehaviorMeta{Slug: "person", Default: true, Manageable: true}},
		behaviorSettings: settings,
	}

	ctx := withAccount(context.Background(), "agent1", "acct1")
	behavior := svc.behaviorFor(ctx, rt)
	_, _ = behavior.BeforeCreate(ctx, json.RawMessage(`{"name":"test"}`), rt)

	if len(calls) != 1 || calls[0] != "person.BeforeCreate" {
		t.Errorf("expected [person.BeforeCreate] (default enabled), got %v", calls)
	}
}

func TestBehaviorFor_PresetDefaultFalseDisablesBehavior(t *testing.T) {
	var calls []string
	rt := makeTestRT("person", json.RawMessage(`{"@vocab":"https://schema.org/"}`))

	svc := &resourceService{
		typeRepo:         &stubTypeRepo{types: map[string]*entities.ResourceType{"person": rt}},
		logger:           noopLogger{},
		behaviors:        ResourceBehaviorRegistry{"person": &trackBehavior{label: "person", calls: &calls}},
		behaviorMeta:     BehaviorMetaRegistry{"person": entities.BehaviorMeta{Slug: "person", Default: false}},
		behaviorSettings: &stubBehaviorSettings{},
	}

	ctx := withAccount(context.Background(), "agent1", "acct1")
	behavior := svc.behaviorFor(ctx, rt)
	_, _ = behavior.BeforeCreate(ctx, json.RawMessage(`{"name":"test"}`), rt)

	if len(calls) != 0 {
		t.Errorf("expected no calls (default=false, no override), got %v", calls)
	}
}

func TestBehaviorFor_SettingsErrorFallsBackToDefaults(t *testing.T) {
	var calls []string
	rt := makeTestRT("person", json.RawMessage(`{"@vocab":"https://schema.org/"}`))

	settings := &stubBehaviorSettings{err: errors.New("db down")}

	svc := &resourceService{
		typeRepo:         &stubTypeRepo{types: map[string]*entities.ResourceType{"person": rt}},
		logger:           noopLogger{},
		behaviors:        ResourceBehaviorRegistry{"person": &trackBehavior{label: "person", calls: &calls}},
		behaviorMeta:     BehaviorMetaRegistry{"person": entities.BehaviorMeta{Slug: "person", Default: true, Manageable: true}},
		behaviorSettings: settings,
	}

	ctx := withAccount(context.Background(), "agent1", "acct1")
	behavior := svc.behaviorFor(ctx, rt)
	_, _ = behavior.BeforeCreate(ctx, json.RawMessage(`{"name":"test"}`), rt)

	// Should fall back to defaults (Default: true), so behavior fires
	if len(calls) != 1 || calls[0] != "person.BeforeCreate" {
		t.Errorf("expected [person.BeforeCreate] (fallback to defaults), got %v", calls)
	}
}

func TestBehaviorFor_NilSettingsRepo(t *testing.T) {
	var calls []string
	rt := makeTestRT("person", json.RawMessage(`{"@vocab":"https://schema.org/"}`))

	svc := &resourceService{
		typeRepo:     &stubTypeRepo{types: map[string]*entities.ResourceType{"person": rt}},
		logger:       noopLogger{},
		behaviors:    ResourceBehaviorRegistry{"person": &trackBehavior{label: "person", calls: &calls}},
		behaviorMeta: BehaviorMetaRegistry{"person": entities.BehaviorMeta{Slug: "person", Default: true, Manageable: true}},
		// behaviorSettings intentionally nil
	}

	ctx := withAccount(context.Background(), "agent1", "acct1")
	// Should not panic — nil guard in resolveEnabledBehaviors
	behavior := svc.behaviorFor(ctx, rt)
	_, _ = behavior.BeforeCreate(ctx, json.RawMessage(`{"name":"test"}`), rt)

	if len(calls) != 1 || calls[0] != "person.BeforeCreate" {
		t.Errorf("expected [person.BeforeCreate] (nil settings repo), got %v", calls)
	}
}

func TestBehaviorFor_InheritanceWithAccountOverride(t *testing.T) {
	var calls []string
	commitmentRT := makeTestRT("commitment", vfContext(""))
	invoiceRT := makeTestRT("invoice", vfContext(subClassOf("commitment")))

	repo := &stubTypeRepo{types: map[string]*entities.ResourceType{
		"invoice":    invoiceRT,
		"commitment": commitmentRT,
	}}

	// Account override enables invoice but not commitment
	settings := &stubBehaviorSettings{
		data: map[string][]string{
			"acct1|invoice": {"invoice"}, // only invoice enabled
		},
	}

	svc := &resourceService{
		typeRepo: repo,
		logger:   noopLogger{},
		behaviors: ResourceBehaviorRegistry{
			"invoice":    &trackBehavior{label: "invoice", calls: &calls},
			"commitment": &trackBehavior{label: "commitment", calls: &calls},
		},
		behaviorMeta: BehaviorMetaRegistry{
			"invoice":    entities.BehaviorMeta{Slug: "invoice", Default: true, Manageable: true},
			"commitment": entities.BehaviorMeta{Slug: "commitment", Default: true, Manageable: true},
		},
		behaviorSettings: settings,
	}

	ctx := withAccount(context.Background(), "agent1", "acct1")
	behavior := svc.behaviorFor(ctx, invoiceRT)
	_, _ = behavior.BeforeCreate(ctx, json.RawMessage(`{"name":"test"}`), invoiceRT)

	// Only invoice should fire; commitment is excluded from override list
	if len(calls) != 1 || calls[0] != "invoice.BeforeCreate" {
		t.Errorf("expected [invoice.BeforeCreate], got %v", calls)
	}
}

func TestBehaviorFor_NonManageableNotAffectedByOverride(t *testing.T) {
	var calls []string
	rt := makeTestRT("person", json.RawMessage(`{"@vocab":"https://schema.org/"}`))

	settings := &stubBehaviorSettings{
		data: map[string][]string{
			"acct1|person": {},
		},
	}

	svc := &resourceService{
		typeRepo:         &stubTypeRepo{types: map[string]*entities.ResourceType{"person": rt}},
		logger:           noopLogger{},
		behaviors:        ResourceBehaviorRegistry{"person": &trackBehavior{label: "person", calls: &calls}},
		behaviorMeta:     BehaviorMetaRegistry{"person": entities.BehaviorMeta{Slug: "person", Default: true, Manageable: false}},
		behaviorSettings: settings,
	}

	ctx := withAccount(context.Background(), "agent1", "acct1")
	behavior := svc.behaviorFor(ctx, rt)
	_, _ = behavior.BeforeCreate(ctx, json.RawMessage(`{"name":"test"}`), rt)

	if len(calls) != 1 || calls[0] != "person.BeforeCreate" {
		t.Errorf("expected [person.BeforeCreate] (non-manageable, ignores override), got %v", calls)
	}
}

// serviceAwareBehavior records the services it received at construction.
type serviceAwareBehavior struct {
	entities.DefaultBehavior
	services BehaviorServices
}

// stubResourceRepo embeds the interface so it satisfies the type without
// implementing methods. Method calls on the nil embedded field panic — these
// stubs are only used for identity checks, not invocation.
type stubResourceRepo struct {
	repositories.ResourceRepository
}
type stubTripleRepo struct{ repositories.TripleRepository }

// recordingLogger captures Warn calls for assertion in merge tests.
type recordingLogger struct {
	noopLogger
	warns []string
}

func (l *recordingLogger) Warn(_ context.Context, msg string, _ ...any) {
	l.warns = append(l.warns, msg)
}

func TestStaticBehavior_IgnoresServices(t *testing.T) {
	t.Parallel()
	want := &serviceAwareBehavior{}
	factory := StaticBehavior(want)
	got := factory(BehaviorServices{})
	if got != want {
		t.Fatalf("StaticBehavior should return the same instance regardless of services")
	}
}

func TestStaticBehavior_PanicsOnNil(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("StaticBehavior(nil) should panic")
		}
	}()
	_ = StaticBehavior(nil)
}

func TestPresetRegistry_BehaviorsInjectsAllServices(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	registry.MustAdd(PresetDefinition{
		Name:  "p",
		Types: []PresetResourceType{NewPresetType("Thing", "thing", "desc", "", "")},
		Behaviors: map[string]BehaviorFactory{
			"thing": func(s BehaviorServices) entities.ResourceBehavior {
				return &serviceAwareBehavior{services: s}
			},
		},
	})

	resources := &stubResourceRepo{}
	triples := &stubTripleRepo{}
	types := &stubTypeRepo{}
	logger := noopLogger{}
	services := BehaviorServices{
		Resources:     resources,
		Triples:       triples,
		ResourceTypes: types,
		Logger:        logger,
	}
	merged, err := registry.Behaviors(services)
	if err != nil {
		t.Fatalf("Behaviors() returned error: %v", err)
	}

	b, ok := merged["thing"].(*serviceAwareBehavior)
	if !ok {
		t.Fatalf("expected serviceAwareBehavior, got %T", merged["thing"])
	}
	if b.services.Resources != resources {
		t.Errorf("Resources field not propagated")
	}
	if b.services.Triples != triples {
		t.Errorf("Triples field not propagated")
	}
	if b.services.ResourceTypes != types {
		t.Errorf("ResourceTypes field not propagated")
	}
	if b.services.Logger != logger {
		t.Errorf("Logger field not propagated")
	}
}

func TestPresetRegistry_AddRejectsNilFactory(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	err := registry.Add(PresetDefinition{
		Name:  "p",
		Types: []PresetResourceType{NewPresetType("T", "t", "d", "", "")},
		Behaviors: map[string]BehaviorFactory{
			"t": nil,
		},
	})
	if err == nil {
		t.Fatal("Add should reject nil factory")
	}
}

func TestPresetRegistry_BehaviorsFactoryInvokedOncePerSlug(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	calls := 0
	registry.MustAdd(PresetDefinition{
		Name:  "p",
		Types: []PresetResourceType{NewPresetType("T", "t", "d", "", "")},
		Behaviors: map[string]BehaviorFactory{
			"t": func(BehaviorServices) entities.ResourceBehavior {
				calls++
				return &serviceAwareBehavior{}
			},
		},
	})
	if _, err := registry.Behaviors(BehaviorServices{}); err != nil {
		t.Fatalf("Behaviors() returned error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected factory to be invoked exactly once, got %d", calls)
	}
}

func TestPresetRegistry_BehaviorsMultiPresetMergeLastWins(t *testing.T) {
	t.Parallel()
	winner := &serviceAwareBehavior{}
	loser := &serviceAwareBehavior{}
	registry := NewPresetRegistry()
	registry.MustAdd(PresetDefinition{
		Name:      "a-first",
		Types:     []PresetResourceType{NewPresetType("T", "t", "d", "", "")},
		Behaviors: map[string]BehaviorFactory{"t": StaticBehavior(loser)},
	})
	registry.MustAdd(PresetDefinition{
		Name:      "b-second",
		Types:     []PresetResourceType{NewPresetType("T", "t", "d", "", "")},
		Behaviors: map[string]BehaviorFactory{"t": StaticBehavior(winner)},
	})
	logger := &recordingLogger{}
	merged, err := registry.Behaviors(BehaviorServices{Logger: logger})
	if err != nil {
		t.Fatalf("Behaviors() returned error: %v", err)
	}
	if merged["t"] != winner {
		t.Fatalf("expected later preset to win the merge")
	}
	if len(logger.warns) != 1 {
		t.Fatalf("expected exactly one override warning, got %d", len(logger.warns))
	}
}

func TestProvideResourceBehaviorRegistry_RejectsTypedNilDeps(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	writer := newLazyResourceWriter()
	var typedNilResources *stubResourceRepo
	var typedNilTriples *stubTripleRepo
	var typedNilTypes *stubTypeRepo
	if _, err := ProvideResourceBehaviorRegistry(
		registry, typedNilResources, &stubTripleRepo{}, &stubTypeRepo{}, noopLogger{}, writer,
	); err == nil {
		t.Error("expected error for typed-nil Resources")
	}
	if _, err := ProvideResourceBehaviorRegistry(
		registry, &stubResourceRepo{}, typedNilTriples, &stubTypeRepo{}, noopLogger{}, writer,
	); err == nil {
		t.Error("expected error for typed-nil Triples")
	}
	if _, err := ProvideResourceBehaviorRegistry(
		registry, &stubResourceRepo{}, &stubTripleRepo{}, typedNilTypes, noopLogger{}, writer,
	); err == nil {
		t.Error("expected error for typed-nil ResourceTypes")
	}
}

func TestProvideResourceBehaviorRegistry_RejectsNilDeps(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	writer := newLazyResourceWriter()
	if _, err := ProvideResourceBehaviorRegistry(registry, nil, &stubTripleRepo{}, &stubTypeRepo{}, noopLogger{}, writer); err == nil {
		t.Error("expected error when Resources is nil")
	}
	if _, err := ProvideResourceBehaviorRegistry(registry, &stubResourceRepo{}, nil, &stubTypeRepo{}, noopLogger{}, writer); err == nil {
		t.Error("expected error when Triples is nil")
	}
	if _, err := ProvideResourceBehaviorRegistry(registry, &stubResourceRepo{}, &stubTripleRepo{}, nil, noopLogger{}, writer); err == nil {
		t.Error("expected error when ResourceTypes is nil")
	}
	if _, err := ProvideResourceBehaviorRegistry(registry, &stubResourceRepo{}, &stubTripleRepo{}, &stubTypeRepo{}, nil, writer); err == nil {
		t.Error("expected error when Logger is nil")
	}
	if _, err := ProvideResourceBehaviorRegistry(registry, &stubResourceRepo{}, &stubTripleRepo{}, &stubTypeRepo{}, noopLogger{}, nil); err == nil {
		t.Error("expected error when Writer is nil")
	}
}

func TestPresetRegistry_BehaviorsRejectsNilReturningFactory(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	registry.MustAdd(PresetDefinition{
		Name:  "p",
		Types: []PresetResourceType{NewPresetType("T", "t", "d", "", "")},
		Behaviors: map[string]BehaviorFactory{
			"t": func(BehaviorServices) entities.ResourceBehavior { return nil },
		},
	})
	if _, err := registry.Behaviors(BehaviorServices{}); err == nil {
		t.Fatal("expected error when factory returns nil")
	}
}

func TestProvideResourceBehaviorRegistry_RejectsNilReturningFactory(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	registry.MustAdd(PresetDefinition{
		Name:  "p",
		Types: []PresetResourceType{NewPresetType("T", "t", "d", "", "")},
		Behaviors: map[string]BehaviorFactory{
			"t": func(BehaviorServices) entities.ResourceBehavior { return nil },
		},
	})
	_, err := ProvideResourceBehaviorRegistry(
		registry, &stubResourceRepo{}, &stubTripleRepo{}, &stubTypeRepo{}, noopLogger{}, newLazyResourceWriter(),
	)
	if err == nil {
		t.Fatal("expected error when factory returns nil")
	}
}

func TestProvideResourceBehaviorRegistry_PassesThroughDeps(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	registry.MustAdd(PresetDefinition{
		Name:  "p",
		Types: []PresetResourceType{NewPresetType("T", "t", "d", "", "")},
		Behaviors: map[string]BehaviorFactory{
			"t": func(s BehaviorServices) entities.ResourceBehavior {
				return &serviceAwareBehavior{services: s}
			},
		},
	})
	resources := &stubResourceRepo{}
	triples := &stubTripleRepo{}
	types := &stubTypeRepo{}
	logger := noopLogger{}
	writer := newLazyResourceWriter()
	merged, err := ProvideResourceBehaviorRegistry(registry, resources, triples, types, logger, writer)
	if err != nil {
		t.Fatalf("ProvideResourceBehaviorRegistry returned error: %v", err)
	}
	b := merged["t"].(*serviceAwareBehavior)
	if b.services.Resources != resources || b.services.Triples != triples ||
		b.services.ResourceTypes != types || b.services.Logger != logger ||
		b.services.Writer != writer {
		t.Fatal("provider did not pass dependencies through to factory")
	}
	// Registry build-time invariant: the proxy is constructed but not yet
	// targeted. This guards the two-phase wiring contract — WireResourceWriter
	// must still be able to install the real service later.
	if writer.svc.Load() != nil {
		t.Fatal("lazyResourceWriter.svc should be nil at registry build time")
	}
}

// fakeResourceWriter records the calls made through ResourceWriter so tests
// can assert the lazy proxy forwards to the right method with the right args.
type fakeResourceWriter struct {
	createCalls []CreateResourceCommand
	updateCalls []UpdateResourceCommand
	deleteCalls []DeleteResourceCommand
	createErr   error
	updateErr   error
	deleteErr   error
	createRes   *entities.Resource
	updateRes   *entities.Resource
}

func (f *fakeResourceWriter) Create(
	_ context.Context, cmd CreateResourceCommand,
) (*entities.Resource, error) {
	f.createCalls = append(f.createCalls, cmd)
	return f.createRes, f.createErr
}

func (f *fakeResourceWriter) Update(
	_ context.Context, cmd UpdateResourceCommand,
) (*entities.Resource, error) {
	f.updateCalls = append(f.updateCalls, cmd)
	return f.updateRes, f.updateErr
}

func (f *fakeResourceWriter) Delete(_ context.Context, cmd DeleteResourceCommand) error {
	f.deleteCalls = append(f.deleteCalls, cmd)
	return f.deleteErr
}

func TestLazyResourceWriter_PreWireReturnsError(t *testing.T) {
	t.Parallel()
	w := newLazyResourceWriter()
	ctx := context.Background()

	if _, err := w.Create(ctx, CreateResourceCommand{TypeSlug: "t"}); err == nil {
		t.Error("expected error from Create before wiring")
	} else if !strings.Contains(err.Error(), "before wiring") {
		t.Errorf("Create error = %q, want contains 'before wiring'", err.Error())
	}

	if _, err := w.Update(ctx, UpdateResourceCommand{ID: "id"}); err == nil {
		t.Error("expected error from Update before wiring")
	} else if !strings.Contains(err.Error(), "before wiring") {
		t.Errorf("Update error = %q, want contains 'before wiring'", err.Error())
	}

	if err := w.Delete(ctx, DeleteResourceCommand{ID: "id"}); err == nil {
		t.Error("expected error from Delete before wiring")
	} else if !strings.Contains(err.Error(), "before wiring") {
		t.Errorf("Delete error = %q, want contains 'before wiring'", err.Error())
	}
}

func TestLazyResourceWriter_ForwardsAfterSetTarget(t *testing.T) {
	t.Parallel()
	w := newLazyResourceWriter()
	fake := &fakeResourceWriter{}
	if err := w.SetTarget(fake); err != nil {
		t.Fatalf("SetTarget returned error: %v", err)
	}

	ctx := context.Background()
	createCmd := CreateResourceCommand{TypeSlug: "t", Data: json.RawMessage(`{"x":1}`)}
	if _, err := w.Create(ctx, createCmd); err != nil {
		t.Fatalf("Create after wiring returned error: %v", err)
	}
	updateCmd := UpdateResourceCommand{ID: "id-1", Data: json.RawMessage(`{"x":2}`)}
	if _, err := w.Update(ctx, updateCmd); err != nil {
		t.Fatalf("Update after wiring returned error: %v", err)
	}
	deleteCmd := DeleteResourceCommand{ID: "id-2"}
	if err := w.Delete(ctx, deleteCmd); err != nil {
		t.Fatalf("Delete after wiring returned error: %v", err)
	}

	// Each method must forward to its matching target method with the exact
	// command value — catches a wiring regression like Update calling Create.
	if len(fake.createCalls) != 1 || fake.createCalls[0].TypeSlug != "t" {
		t.Errorf("Create forwarding: got %+v, want one call with TypeSlug=t", fake.createCalls)
	}
	if len(fake.updateCalls) != 1 || fake.updateCalls[0].ID != "id-1" {
		t.Errorf("Update forwarding: got %+v, want one call with ID=id-1", fake.updateCalls)
	}
	if len(fake.deleteCalls) != 1 || fake.deleteCalls[0].ID != "id-2" {
		t.Errorf("Delete forwarding: got %+v, want one call with ID=id-2", fake.deleteCalls)
	}
}

func TestLazyResourceWriter_SetTargetRejectsNil(t *testing.T) {
	t.Parallel()
	w := newLazyResourceWriter()
	if err := w.SetTarget(nil); err == nil {
		t.Error("SetTarget(nil) = nil error, want rejection")
	}
	// Typed-nil interface: a nil *fakeResourceWriter wrapped in the ResourceWriter
	// interface is != nil but still unusable. isNilInterface must catch it.
	var typedNil *fakeResourceWriter
	if err := w.SetTarget(typedNil); err == nil {
		t.Error("SetTarget(typed-nil) = nil error, want rejection")
	}
}

func TestLazyResourceWriter_SetTargetRejectsDoubleSet(t *testing.T) {
	t.Parallel()
	w := newLazyResourceWriter()
	if err := w.SetTarget(&fakeResourceWriter{}); err != nil {
		t.Fatalf("first SetTarget returned error: %v", err)
	}
	if err := w.SetTarget(&fakeResourceWriter{}); err == nil {
		t.Error("second SetTarget = nil error, want 'already wired'")
	}
}

func TestLazyResourceWriter_SetTargetRejectsSelf(t *testing.T) {
	t.Parallel()
	w := newLazyResourceWriter()
	// Targeting self would infinite-loop on the first forwarded call.
	if err := w.SetTarget(w); err == nil {
		t.Error("SetTarget(self) = nil error, want refusal")
	}
}

func TestWireResourceWriter_RejectsNilService(t *testing.T) {
	t.Parallel()
	w := newLazyResourceWriter()
	// A nil ResourceService interface must cause Fx to fail startup, not
	// silently install a nil target that panics at the first hook.
	if err := WireResourceWriter(w, nil); err == nil {
		t.Error("WireResourceWriter with nil svc = nil error, want rejection")
	}
}

func TestWireResourceWriter_RejectsNilWriter(t *testing.T) {
	t.Parallel()
	// A nil *lazyResourceWriter must fail startup rather than nil-deref.
	if err := WireResourceWriter(nil, nil); err == nil {
		t.Error("WireResourceWriter with nil writer = nil error, want rejection")
	}
}
