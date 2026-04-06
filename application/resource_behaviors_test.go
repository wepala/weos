package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"weos/domain/entities"
	"weos/domain/repositories"
	"weos/pkg/jsonld"

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
