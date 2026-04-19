package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// stubProjMgr records which ProjectionManager methods were called.
type stubProjMgr struct {
	ensuredSlugs   []string
	ensureTableErr error
	tables         map[string]bool
	ancestorMap    map[string][]string
	reverseRefs    map[string][]repositories.ReverseReference
}

func (s *stubProjMgr) EnsureTable(_ context.Context, slug string, _, _ json.RawMessage) error {
	s.ensuredSlugs = append(s.ensuredSlugs, slug)
	return s.ensureTableErr
}

func (s *stubProjMgr) HasProjectionTable(slug string) bool {
	return s.tables[slug]
}

func (s *stubProjMgr) TableName(slug string) string     { return slug + "_table" }
func (s *stubProjMgr) Context(_ string) json.RawMessage { return nil }

func (s *stubProjMgr) EnsureExistingTables(_ context.Context) error { return nil }

func (s *stubProjMgr) UpdateColumn(context.Context, string, string, string, any) error {
	return nil
}

func (s *stubProjMgr) UpdateColumnByFK(context.Context, string, string, string, string, any) error {
	return nil
}

func (s *stubProjMgr) ReverseReferences(slug string) []repositories.ReverseReference {
	return s.reverseRefs[slug]
}

func (s *stubProjMgr) ForwardReferences(_ string) []repositories.ForwardReference {
	return nil
}

func (s *stubProjMgr) AncestorSlugs(slug string) []string {
	return s.ancestorMap[slug]
}

func (s *stubProjMgr) HasColumn(_, _ string) bool { return false }

func makeRT(slug, ctxJSON string) *entities.ResourceType {
	rt := &entities.ResourceType{}
	_ = rt.Restore("id-"+slug, slug, slug, "desc", "active",
		json.RawMessage(ctxJSON), nil, rt.CreatedAt(), 1)
	return rt
}

func TestEnsureProjection_AbstractType(t *testing.T) {
	t.Parallel()
	pm := &stubProjMgr{}
	repo := &stubTypeRepo{types: map[string]*entities.ResourceType{}}
	ctx := context.Background()

	abstractCtx := json.RawMessage(`{"@vocab":"https://schema.org/","weos:abstract":true}`)
	err := ensureProjection(ctx, repo, pm, "instrument", nil, abstractCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pm.ensuredSlugs) != 1 || pm.ensuredSlugs[0] != "instrument" {
		t.Fatalf("expected EnsureTable for 'instrument', got %v", pm.ensuredSlugs)
	}
}

func TestEnsureProjection_ConcreteWithParent_EnsuresBothTables(t *testing.T) {
	t.Parallel()
	pm := &stubProjMgr{}
	parentRT := makeRT("instrument", `{"@vocab":"https://schema.org/","weos:abstract":true}`)
	repo := &stubTypeRepo{types: map[string]*entities.ResourceType{
		"instrument": parentRT,
	}}
	ctx := context.Background()

	childCtx := json.RawMessage(`{"@vocab":"https://schema.org/","rdfs:subClassOf":"instrument"}`)
	err := ensureProjection(ctx, repo, pm, "loan", nil, childCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both child and parent should get EnsureTable calls.
	if len(pm.ensuredSlugs) != 2 {
		t.Fatalf("expected 2 EnsureTable calls, got %v", pm.ensuredSlugs)
	}
	if pm.ensuredSlugs[0] != "loan" {
		t.Fatalf("first EnsureTable should be for child 'loan', got %q", pm.ensuredSlugs[0])
	}
	if pm.ensuredSlugs[1] != "instrument" {
		t.Fatalf("second EnsureTable should be for parent 'instrument', got %q", pm.ensuredSlugs[1])
	}
}

func TestEnsureProjection_ConcreteWithNonAbstractParent(t *testing.T) {
	t.Parallel()
	pm := &stubProjMgr{}
	parentRT := makeRT("vehicle", `{"@vocab":"https://schema.org/"}`)
	repo := &stubTypeRepo{types: map[string]*entities.ResourceType{
		"vehicle": parentRT,
	}}
	ctx := context.Background()

	childCtx := json.RawMessage(`{"@vocab":"https://schema.org/","rdfs:subClassOf":"vehicle"}`)
	err := ensureProjection(ctx, repo, pm, "car", nil, childCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both child and non-abstract parent get EnsureTable.
	if len(pm.ensuredSlugs) != 2 {
		t.Fatalf("expected 2 EnsureTable calls, got %v", pm.ensuredSlugs)
	}
}

func TestEnsureProjection_ConcreteNoParent(t *testing.T) {
	t.Parallel()
	pm := &stubProjMgr{}
	repo := &stubTypeRepo{types: map[string]*entities.ResourceType{}}
	ctx := context.Background()

	simpleCtx := json.RawMessage(`{"@vocab":"https://schema.org/"}`)
	err := ensureProjection(ctx, repo, pm, "product", nil, simpleCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pm.ensuredSlugs) != 1 || pm.ensuredSlugs[0] != "product" {
		t.Fatalf("expected EnsureTable for 'product', got %v", pm.ensuredSlugs)
	}
}

func TestEnsureProjection_ParentNotFound_ChildStillGetTable(t *testing.T) {
	t.Parallel()
	pm := &stubProjMgr{}
	notFoundRepo := &infraErrorRepo{err: repositories.ErrNotFound}
	ctx := context.Background()

	childCtx := json.RawMessage(`{"@vocab":"https://schema.org/","rdfs:subClassOf":"missing-parent"}`)
	err := ensureProjection(ctx, notFoundRepo, pm, "orphan", nil, childCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pm.ensuredSlugs) != 1 || pm.ensuredSlugs[0] != "orphan" {
		t.Fatalf("expected EnsureTable for 'orphan', got %v", pm.ensuredSlugs)
	}
}

func TestEnsureProjection_InfraError_PropagatesError(t *testing.T) {
	t.Parallel()
	pm := &stubProjMgr{}
	infraErr := errors.New("connection refused")
	infraRepo := &infraErrorRepo{err: infraErr}
	ctx := context.Background()

	childCtx := json.RawMessage(`{"@vocab":"https://schema.org/","rdfs:subClassOf":"parent"}`)
	err := ensureProjection(ctx, infraRepo, pm, "child", nil, childCtx)
	if err == nil {
		t.Fatal("expected error to be propagated")
	}
	// Child's own table should still have been ensured before the parent lookup failed.
	if len(pm.ensuredSlugs) != 1 || pm.ensuredSlugs[0] != "child" {
		t.Fatalf("expected EnsureTable for 'child' before error, got %v", pm.ensuredSlugs)
	}
}

// infraErrorRepo returns an infrastructure error for FindBySlug.
type infraErrorRepo struct {
	stubTypeRepo
	err error
}

func (r *infraErrorRepo) FindBySlug(_ context.Context, _ string) (*entities.ResourceType, error) {
	return nil, r.err
}

// TestBuildStateFromTransaction_TripleReplayDoesNotDuplicate pins the
// interaction between the issue #8 fix and projection replay. Resource.Created
// now already carries an edges node (populated by BuildResourceGraph); the
// subsequent Triple.Created event replays the same edge via AddEdgeToGraph.
// The replay must be idempotent: the final state.Data must contain a single
// ref (not a two-element array) so that extractEdgeColumns can read the FK.
func TestBuildStateFromTransaction_TripleReplayDoesNotDuplicate(t *testing.T) {
	t.Parallel()

	const aggregateID = "urn:enrollment:test-1"
	const predicate = "https://schema.org/participant"
	const objectID = "urn:student:stu-1"

	// Build a Resource.Created payload whose Data already has the edge —
	// mirrors what resource_service now produces after the fix.
	graphWithEdge := map[string]any{
		"@graph": []any{
			map[string]any{
				"@id":            aggregateID,
				"@type":          "Enrollment",
				"paymentCadence": "monthly",
			},
			map[string]any{
				"@id":     aggregateID,
				predicate: map[string]any{"@id": objectID},
			},
		},
	}
	graphBytes, err := json.Marshal(graphWithEdge)
	if err != nil {
		t.Fatalf("marshal graph: %v", err)
	}

	createdPayload := map[string]any{
		"TypeSlug":  "enrollment",
		"Data":      json.RawMessage(graphBytes),
		"CreatedBy": "test-agent",
		"AccountID": "test-account",
		"Timestamp": "2026-04-13T12:00:00Z",
	}
	triplePayload := map[string]any{
		"predicate": predicate,
		"object":    objectID,
	}

	txEvents := []domain.EventEnvelope[any]{
		{
			AggregateID: aggregateID,
			EventType:   "Resource.Created",
			SequenceNo:  1,
			Payload:     createdPayload,
		},
		{
			AggregateID: aggregateID,
			EventType:   "Triple.Created",
			SequenceNo:  2,
			Payload:     triplePayload,
		},
	}

	state := buildStateFromTransaction(
		context.Background(), txEvents, aggregateID, 0, noopLogger{},
	)

	// Decode and assert the edge is a single map, not a two-entry array.
	var doc map[string]any
	if err := json.Unmarshal(state.Data, &doc); err != nil {
		t.Fatalf("unmarshal state.Data: %v", err)
	}
	graphArr, ok := doc["@graph"].([]any)
	if !ok || len(graphArr) < 2 {
		t.Fatalf("expected @graph with edges node, got %v", doc["@graph"])
	}
	edges, ok := graphArr[1].(map[string]any)
	if !ok {
		t.Fatalf("edges node is %T, want map", graphArr[1])
	}
	ref, ok := edges[predicate].(map[string]any)
	if !ok {
		t.Fatalf("predicate value is %T after replay, want a single {@id} map (not an array); this means idempotency regressed",
			edges[predicate])
	}
	if got, _ := ref["@id"].(string); got != objectID {
		t.Errorf("edge @id = %q, want %q", got, objectID)
	}
}
