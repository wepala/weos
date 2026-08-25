package application

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// testFactContext / testFactSchema mirror the memory preset's fact type. The
// application package cannot import presets/memory (it imports application),
// so the shape is replicated here; the memory preset's own tests pin the real
// artifact.
const testFactContext = `{"@vocab":"https://schema.org/",` +
	`"mem":"https://weos.io/vocab/memory#","@type":"mem:Fact",` +
	`"statement":"https://schema.org/text","about":"https://schema.org/about",` +
	`"wasDerivedFrom":"http://www.w3.org/ns/prov#wasDerivedFrom",` +
	`"wasRevisionOf":"http://www.w3.org/ns/prov#wasRevisionOf",` +
	`"invalidatedAtTime":"http://www.w3.org/ns/prov#invalidatedAtTime"}`

const testFactSchema = `{"type":"object","properties":{` +
	`"statement":{"type":"string"},"about":{"type":"string"},` +
	`"wasDerivedFrom":{"type":"array","items":{"type":"string"}},` +
	`"wasRevisionOf":{"type":"string","x-resource-type":"fact","x-display-property":"statement"},` +
	`"invalidatedAtTime":{"type":"string","format":"date-time"}},"required":["statement"]}`

// testEligible is the allowlist the handler tests run with: "note" mirrors
// the memory preset's own episodic input declaration.
func testEligible() map[string]bool {
	return map[string]bool{"note": true}
}

type fakeExtractor struct {
	calls      int
	lastObs    entities.EpisodeObservation
	lastFacts  []entities.ExistingFact
	candidates []entities.FactCandidate
	err        error
}

func (f *fakeExtractor) ExtractFacts(
	_ context.Context, obs entities.EpisodeObservation, related []entities.ExistingFact,
) ([]entities.FactCandidate, error) {
	f.calls++
	f.lastObs = obs
	f.lastFacts = related
	return f.candidates, f.err
}

type fakeFactStore struct {
	// facts holds existing fact resources keyed by the `about` field value
	// ListByField filters on.
	facts   map[string][]*entities.Resource
	creates []CreateResourceCommand
	updates []UpdateResourceCommand
	order   []string
}

func (f *fakeFactStore) Create(_ context.Context, cmd CreateResourceCommand) (*entities.Resource, error) {
	f.creates = append(f.creates, cmd)
	f.order = append(f.order, "create")
	return nil, nil
}

func (f *fakeFactStore) Update(_ context.Context, cmd UpdateResourceCommand) (*entities.Resource, error) {
	f.updates = append(f.updates, cmd)
	f.order = append(f.order, "update")
	return nil, nil
}

func (f *fakeFactStore) ListByField(
	_ context.Context, _, _, fieldValue string,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	return repositories.PaginatedResponse[*entities.Resource]{Data: f.facts[fieldValue]}, nil
}

// testFact builds a fact resource whose data went through the real
// BuildResourceGraph, mirroring what ResourceService persists.
func testFact(t *testing.T, id string, flat map[string]any) *entities.Resource {
	t.Helper()
	raw, err := json.Marshal(flat)
	if err != nil {
		t.Fatalf("marshal fact data: %v", err)
	}
	refProps := ExtractReferenceProperties(
		json.RawMessage(testFactSchema), json.RawMessage(testFactContext))
	graph, err := BuildResourceGraph(raw, refProps, id, "Fact", json.RawMessage(testFactContext))
	if err != nil {
		t.Fatalf("build fact graph: %v", err)
	}
	res, err := new(entities.Resource).With(id, "fact", graph, "", "")
	if err != nil {
		t.Fatalf("build fact resource: %v", err)
	}
	return res
}

func factTypeRepo(t *testing.T) *kgTypeRepo {
	t.Helper()
	rt, err := new(entities.ResourceType).With("Fact", "fact", "test fact type",
		json.RawMessage(testFactContext), json.RawMessage(testFactSchema))
	if err != nil {
		t.Fatalf("build fact type: %v", err)
	}
	return &kgTypeRepo{rt: rt}
}

// episode returns a Resource.Published envelope plus the transaction events
// behind it, for a resource of the given type. Payloads are map[string]any
// with PascalCase keys — background subscribers always see store-deserialized
// maps, never the original typed structs (see buildStateFromTransaction).
func episode(typeSlug, aggregateID, txID, createdEventID string) (
	domain.EventEnvelope[any], []domain.EventEnvelope[any],
) {
	created := domain.EventEnvelope[any]{
		ID:            createdEventID,
		EventType:     "Resource.Created",
		AggregateID:   aggregateID,
		SequenceNo:    1,
		TransactionID: txID,
		Payload: map[string]any{
			"TypeSlug": typeSlug,
			"Data":     map[string]any{"@id": aggregateID, "name": "episodic thing"},
		},
	}
	published := domain.EventEnvelope[any]{
		ID:            createdEventID + "-pub",
		EventType:     "Resource.Published",
		AggregateID:   aggregateID,
		SequenceNo:    2,
		TransactionID: txID,
		Payload:       map[string]any{"TypeSlug": typeSlug},
	}
	return published, []domain.EventEnvelope[any]{created, published}
}

func TestConsolidationHandler_RecordsFactFromEpisode(t *testing.T) {
	t.Parallel()

	published, tx := episode("note", "urn:note:1", "tx1", "ev1")
	extractor := &fakeExtractor{candidates: []entities.FactCandidate{
		{Statement: "Akeem keeps meeting notes in WeOS", Confidence: 0.8},
	}}
	store := &fakeFactStore{}
	h := consolidationHandler(&fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{"tx1": tx}},
		extractor, store, factTypeRepo(t), testEligible(), noopLogger{})

	if err := h(context.Background(), published); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if extractor.calls != 1 {
		t.Fatalf("extractor calls = %d, want 1", extractor.calls)
	}
	if got := extractor.lastObs.EventIDs; len(got) != 1 || got[0] != "urn:event:ev1" {
		t.Errorf("observation event IDs = %v, want [urn:event:ev1]", got)
	}
	if len(store.creates) != 1 {
		t.Fatalf("creates = %d, want 1", len(store.creates))
	}
	var data map[string]any
	if err := json.Unmarshal(store.creates[0].Data, &data); err != nil {
		t.Fatalf("unmarshal created fact: %v", err)
	}
	if data["statement"] != "Akeem keeps meeting notes in WeOS" {
		t.Errorf("statement = %v", data["statement"])
	}
	if data["about"] != "urn:note:1" {
		t.Errorf("about = %v, want the observed resource URN", data["about"])
	}
	derived, _ := data["wasDerivedFrom"].([]any)
	if len(derived) != 1 || derived[0] != "urn:event:ev1" {
		t.Errorf("wasDerivedFrom = %v, want [urn:event:ev1]", data["wasDerivedFrom"])
	}
	if _, has := data["wasRevisionOf"]; has {
		t.Error("wasRevisionOf must be absent when nothing is superseded")
	}
}

func TestConsolidationHandler_SkipsMemoryTypes(t *testing.T) {
	t.Parallel()

	// The allowlist here (incorrectly) declares the memory types eligible;
	// the hard invariant must still keep the subscriber off its own output.
	published, tx := episode("fact", "urn:fact:1", "tx1", "ev1")
	extractor := &fakeExtractor{}
	h := consolidationHandler(&fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{"tx1": tx}},
		extractor, &fakeFactStore{}, factTypeRepo(t),
		map[string]bool{"fact": true, "playbook": true}, noopLogger{})

	if err := h(context.Background(), published); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if extractor.calls != 0 {
		t.Errorf("extractor called %d times for a fact episode, want 0 (no self-loop)", extractor.calls)
	}
}

func TestConsolidationHandler_SkipsUndeclaredTypes(t *testing.T) {
	t.Parallel()

	// Structured domain data is already semantic memory — an event for a type
	// no preset declared must never reach the extractor (no LLM call).
	published, tx := episode("transaction", "urn:transaction:1", "tx1", "ev1")
	extractor := &fakeExtractor{candidates: []entities.FactCandidate{{Statement: "should never happen"}}}
	store := &fakeFactStore{}
	h := consolidationHandler(&fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{"tx1": tx}},
		extractor, store, factTypeRepo(t), testEligible(), noopLogger{})

	if err := h(context.Background(), published); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if extractor.calls != 0 {
		t.Errorf("extractor called %d times for an undeclared type, want 0", extractor.calls)
	}
	if len(store.creates) != 0 {
		t.Errorf("creates = %d, want 0", len(store.creates))
	}
}

// updateEpisode builds an update-only transaction: Resource.Updated +
// Resource.Published, no Resource.Created. The transaction state's TypeSlug is
// empty on this path — only the published payload carries the slug.
func updateEpisode(typeSlug, aggregateID, txID, eventID string) (
	domain.EventEnvelope[any], []domain.EventEnvelope[any],
) {
	updated := domain.EventEnvelope[any]{
		ID:            eventID,
		EventType:     "Resource.Updated",
		AggregateID:   aggregateID,
		SequenceNo:    3,
		TransactionID: txID,
		Payload: map[string]any{
			"Data": map[string]any{"@id": aggregateID, "statement": "edited"},
		},
	}
	published := domain.EventEnvelope[any]{
		ID:            eventID + "-pub",
		EventType:     "Resource.Published",
		AggregateID:   aggregateID,
		SequenceNo:    4,
		TransactionID: txID,
		Payload:       map[string]any{"TypeSlug": typeSlug},
	}
	return published, []domain.EventEnvelope[any]{updated, published}
}

func TestConsolidationHandler_SkipsMemoryTypeUpdates(t *testing.T) {
	t.Parallel()

	// A supersession invalidates the predecessor via Update — that commit's
	// own Resource.Published must not loop the subscriber onto the fact,
	// even when the allowlist (incorrectly) declares the memory types.
	published, tx := updateEpisode("fact", "urn:fact:old", "tx9", "ev9")
	extractor := &fakeExtractor{candidates: []entities.FactCandidate{{Statement: "fact about a fact"}}}
	store := &fakeFactStore{}
	h := consolidationHandler(&fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{"tx9": tx}},
		extractor, store, factTypeRepo(t),
		map[string]bool{"fact": true, "playbook": true}, noopLogger{})

	if err := h(context.Background(), published); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if extractor.calls != 0 {
		t.Errorf("extractor called %d times for a fact UPDATE episode, want 0 (no self-loop)", extractor.calls)
	}
	if len(store.creates) != 0 {
		t.Errorf("creates = %d, want 0", len(store.creates))
	}
}

func TestConsolidationHandler_ConsolidatesNonMemoryUpdates(t *testing.T) {
	t.Parallel()

	published, tx := updateEpisode("note", "urn:note:1", "tx9", "ev9")
	extractor := &fakeExtractor{candidates: []entities.FactCandidate{{Statement: "note evolved"}}}
	store := &fakeFactStore{}
	h := consolidationHandler(&fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{"tx9": tx}},
		extractor, store, factTypeRepo(t), testEligible(), noopLogger{})

	if err := h(context.Background(), published); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if extractor.calls != 1 {
		t.Fatalf("extractor calls = %d, want 1 — update episodes must consolidate", extractor.calls)
	}
	if extractor.lastObs.TypeSlug != "note" {
		t.Errorf("observation TypeSlug = %q, want note (from published payload)", extractor.lastObs.TypeSlug)
	}
	if len(store.creates) != 1 {
		t.Errorf("creates = %d, want 1", len(store.creates))
	}
}

func TestConsolidationHandler_DedupCrossEntityCandidateOnReplay(t *testing.T) {
	t.Parallel()

	// A prior run recorded a fact ABOUT ANOTHER ENTITY (urn:person:x) from
	// this episode's event. The episode-level dedup (scoped to the observed
	// resource) misses it; the per-candidate check must catch it on replay.
	published, tx := episode("note", "urn:note:1", "tx1", "ev1")
	crossEntity := testFact(t, "urn:fact:person", map[string]any{
		"statement":      "the person mentioned prefers espresso",
		"about":          "urn:person:x",
		"wasDerivedFrom": []string{"urn:event:ev1"},
	})
	extractor := &fakeExtractor{candidates: []entities.FactCandidate{{
		Statement: "the person mentioned prefers espresso",
		About:     "urn:person:x",
	}}}
	store := &fakeFactStore{facts: map[string][]*entities.Resource{"urn:person:x": {crossEntity}}}
	h := consolidationHandler(&fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{"tx1": tx}},
		extractor, store, factTypeRepo(t), testEligible(), noopLogger{})

	if err := h(context.Background(), published); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if extractor.calls != 1 {
		t.Fatalf("extractor calls = %d, want 1 (episode-level dedup can't see the cross-entity fact)",
			extractor.calls)
	}
	if len(store.creates) != 0 {
		t.Errorf("creates = %d, want 0 — replay must not re-record the cross-entity fact", len(store.creates))
	}
}

func TestConsolidationHandler_DedupOnReplay(t *testing.T) {
	t.Parallel()

	published, tx := episode("note", "urn:note:1", "tx1", "ev1")
	existing := testFact(t, "urn:fact:prior", map[string]any{
		"statement":      "already distilled",
		"about":          "urn:note:1",
		"wasDerivedFrom": []string{"urn:event:ev1"},
	})
	extractor := &fakeExtractor{candidates: []entities.FactCandidate{{Statement: "dup"}}}
	store := &fakeFactStore{facts: map[string][]*entities.Resource{"urn:note:1": {existing}}}
	h := consolidationHandler(&fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{"tx1": tx}},
		extractor, store, factTypeRepo(t), testEligible(), noopLogger{})

	if err := h(context.Background(), published); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if extractor.calls != 0 {
		t.Errorf("extractor called %d times on replay, want 0 (dedup on wasDerivedFrom)", extractor.calls)
	}
	if len(store.creates) != 0 {
		t.Errorf("creates = %d on replay, want 0", len(store.creates))
	}
}

func TestConsolidationHandler_SupersedesInvalidatesPredecessorFirst(t *testing.T) {
	t.Parallel()

	published, tx := episode("note", "urn:note:1", "tx1", "ev2")
	old := testFact(t, "urn:fact:old", map[string]any{
		"statement":      "Akeem works Mondays from home",
		"about":          "urn:note:1",
		"wasDerivedFrom": []string{"urn:event:ev1"},
	})
	extractor := &fakeExtractor{candidates: []entities.FactCandidate{{
		Statement:    "Akeem works Mondays from the office",
		Confidence:   0.9,
		SupersedesID: "urn:fact:old",
	}}}
	store := &fakeFactStore{facts: map[string][]*entities.Resource{"urn:note:1": {old}}}
	h := consolidationHandler(&fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{"tx1": tx}},
		extractor, store, factTypeRepo(t), testEligible(), noopLogger{})

	if err := h(context.Background(), published); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if got := strings.Join(store.order, ","); got != "update,create" {
		t.Fatalf("write order = %s, want update,create (invalidate predecessor first)", got)
	}
	if store.updates[0].ID != "urn:fact:old" {
		t.Errorf("updated ID = %s, want urn:fact:old", store.updates[0].ID)
	}
	var oldData map[string]any
	if err := json.Unmarshal(store.updates[0].Data, &oldData); err != nil {
		t.Fatalf("unmarshal invalidation update: %v", err)
	}
	if oldData["invalidatedAtTime"] == nil || oldData["invalidatedAtTime"] == "" {
		t.Error("invalidatedAtTime not set on superseded fact")
	}
	if oldData["statement"] != "Akeem works Mondays from home" {
		t.Error("invalidation update dropped existing fields — Update is full-replace")
	}
	var newData map[string]any
	if err := json.Unmarshal(store.creates[0].Data, &newData); err != nil {
		t.Fatalf("unmarshal new fact: %v", err)
	}
	if newData["wasRevisionOf"] != "urn:fact:old" {
		t.Errorf("wasRevisionOf = %v, want urn:fact:old", newData["wasRevisionOf"])
	}
}

func TestConsolidationHandler_ClampsOutOfRangeConfidence(t *testing.T) {
	t.Parallel()

	published, tx := episode("note", "urn:note:1", "tx1", "ev1")
	extractor := &fakeExtractor{candidates: []entities.FactCandidate{
		{Statement: "over-confident", Confidence: 1.7},
		{Statement: "under-confident", Confidence: -0.4},
	}}
	store := &fakeFactStore{}
	h := consolidationHandler(&fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{"tx1": tx}},
		extractor, store, factTypeRepo(t), testEligible(), noopLogger{})

	if err := h(context.Background(), published); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(store.creates) != 2 {
		t.Fatalf("creates = %d, want 2", len(store.creates))
	}
	var over, under map[string]any
	if err := json.Unmarshal(store.creates[0].Data, &over); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := json.Unmarshal(store.creates[1].Data, &under); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got, _ := over["confidence"].(float64); got != 1 {
		t.Errorf("confidence = %v, want clamped to 1 — out-of-range fails schema validation on every retry",
			over["confidence"])
	}
	if _, has := under["confidence"]; has {
		t.Errorf("negative confidence must be omitted, got %v", under["confidence"])
	}
}

func TestConsolidationHandler_IgnoresHallucinatedSupersedesID(t *testing.T) {
	t.Parallel()

	published, tx := episode("note", "urn:note:1", "tx1", "ev1")
	extractor := &fakeExtractor{candidates: []entities.FactCandidate{{
		Statement:    "some new fact",
		SupersedesID: "urn:fact:never-shown-to-model",
	}}}
	store := &fakeFactStore{}
	h := consolidationHandler(&fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{"tx1": tx}},
		extractor, store, factTypeRepo(t), testEligible(), noopLogger{})

	if err := h(context.Background(), published); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(store.updates) != 0 {
		t.Errorf("updates = %d, want 0 — hallucinated URN must not invalidate anything", len(store.updates))
	}
	var data map[string]any
	if err := json.Unmarshal(store.creates[0].Data, &data); err != nil {
		t.Fatalf("unmarshal created fact: %v", err)
	}
	if _, has := data["wasRevisionOf"]; has {
		t.Error("wasRevisionOf must be dropped for an unknown supersedesId")
	}
}

func TestConsolidationHandler_ExcludesSupersededFromExtractorContext(t *testing.T) {
	t.Parallel()

	published, tx := episode("note", "urn:note:1", "tx1", "ev3")
	invalidated := testFact(t, "urn:fact:dead", map[string]any{
		"statement":         "outdated",
		"about":             "urn:note:1",
		"wasDerivedFrom":    []string{"urn:event:ev1"},
		"invalidatedAtTime": "2026-01-01T00:00:00Z",
	})
	active := testFact(t, "urn:fact:live", map[string]any{
		"statement":      "current",
		"about":          "urn:note:1",
		"wasDerivedFrom": []string{"urn:event:ev2"},
	})
	extractor := &fakeExtractor{}
	store := &fakeFactStore{facts: map[string][]*entities.Resource{"urn:note:1": {invalidated, active}}}
	h := consolidationHandler(&fakeEventStore{tx: map[string][]domain.EventEnvelope[any]{"tx1": tx}},
		extractor, store, factTypeRepo(t), testEligible(), noopLogger{})

	if err := h(context.Background(), published); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(extractor.lastFacts) != 1 || extractor.lastFacts[0].ID != "urn:fact:live" {
		t.Errorf("extractor context = %+v, want only the non-superseded fact", extractor.lastFacts)
	}
}

func TestProvideConsolidationGroup_NoExtractorRegistersNothing(t *testing.T) {
	t.Parallel()

	groups := ProvideConsolidationGroup(ConsolidationGroupParams{
		Extractor: nil,
		Registry:  NewPresetRegistry(),
		Logger:    noopLogger{},
	})
	if groups != nil {
		t.Fatalf("expected no group without an extractor, got %+v", groups)
	}
}

func TestProvideConsolidationGroup_NoDeclaredTypesRegistersNothing(t *testing.T) {
	t.Parallel()

	registry := NewPresetRegistry()
	registry.MustAdd(PresetDefinition{Name: "structured", Types: []PresetResourceType{
		NewPresetType("Transaction", "transaction", "typed domain data", "", ""),
	}})
	groups := ProvideConsolidationGroup(ConsolidationGroupParams{
		Extractor: &fakeExtractor{},
		Registry:  registry,
		Logger:    noopLogger{},
	})
	if groups != nil {
		t.Fatalf("expected no group when no preset declares eligible types, got %+v", groups)
	}
}

func TestProvideConsolidationGroup_DeclaredTypesRegisterStartAtHeadGroup(t *testing.T) {
	t.Parallel()

	registry := NewPresetRegistry()
	registry.MustAdd(PresetDefinition{Name: "memoryish", Consolidates: []string{"note"}})
	groups := ProvideConsolidationGroup(ConsolidationGroupParams{
		Extractor: &fakeExtractor{},
		Registry:  registry,
		Logger:    noopLogger{},
	})
	if len(groups) != 1 || groups[0].Name != "consolidation" {
		t.Fatalf("groups = %+v, want one consolidation group", groups)
	}
	if !groups[0].StartAtHead {
		t.Error("consolidation group must start at the feed head — installing memory " +
			"on an instance with history must not trigger a backfill LLM pass")
	}
}

func TestConsolidationEligible_StripsMemoryTypes(t *testing.T) {
	t.Parallel()

	registry := NewPresetRegistry()
	registry.MustAdd(PresetDefinition{Name: "a", Consolidates: []string{"note", "fact", ""}})
	registry.MustAdd(PresetDefinition{Name: "b", Consolidates: []string{"playbook", "transcript"}})
	eligible := registry.ConsolidationEligible()
	want := map[string]bool{"note": true, "transcript": true}
	if len(eligible) != len(want) {
		t.Fatalf("eligible = %v, want %v", eligible, want)
	}
	for slug := range want {
		if !eligible[slug] {
			t.Errorf("eligible missing %q: %v", slug, eligible)
		}
	}
}
