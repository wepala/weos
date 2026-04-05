package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"weos/domain/entities"
	"weos/domain/repositories"
)

// stubProjMgr records which ProjectionManager methods were called.
type stubProjMgr struct {
	ensuredSlugs   []string
	subtypeCalls   []subtypeCall
	ensureTableErr error
	registerSubErr error
	tables         map[string]bool
	parentMap      map[string]string
	reverseRefs    map[string][]repositories.ReverseReference
}

type subtypeCall struct {
	childSlug, parentSlug string
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

func (s *stubProjMgr) RegisterSubtype(
	_ context.Context, childSlug, parentSlug string, _, _ json.RawMessage,
) error {
	s.subtypeCalls = append(s.subtypeCalls, subtypeCall{childSlug, parentSlug})
	return s.registerSubErr
}

func (s *stubProjMgr) IsSubtype(slug string) bool {
	_, ok := s.parentMap[slug]
	return ok
}

func (s *stubProjMgr) ParentSlug(slug string) string {
	return s.parentMap[slug]
}

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
	if len(pm.subtypeCalls) != 0 {
		t.Fatalf("expected no RegisterSubtype calls, got %v", pm.subtypeCalls)
	}
}

func TestEnsureProjection_ConcreteWithAbstractParent(t *testing.T) {
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
	if len(pm.subtypeCalls) != 1 {
		t.Fatalf("expected 1 RegisterSubtype call, got %d", len(pm.subtypeCalls))
	}
	if pm.subtypeCalls[0].childSlug != "loan" || pm.subtypeCalls[0].parentSlug != "instrument" {
		t.Fatalf("unexpected call: %v", pm.subtypeCalls[0])
	}
	// EnsureTable is called for the parent first to handle out-of-order event replay.
	if len(pm.ensuredSlugs) != 1 || pm.ensuredSlugs[0] != "instrument" {
		t.Fatalf("expected EnsureTable for parent 'instrument', got %v", pm.ensuredSlugs)
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
	if len(pm.ensuredSlugs) != 1 || pm.ensuredSlugs[0] != "car" {
		t.Fatalf("expected EnsureTable for 'car', got %v", pm.ensuredSlugs)
	}
	if len(pm.subtypeCalls) != 0 {
		t.Fatalf("expected no RegisterSubtype calls, got %v", pm.subtypeCalls)
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

func TestEnsureProjection_ParentNotFound_FallsBackToStandalone(t *testing.T) {
	t.Parallel()
	pm := &stubProjMgr{}
	// Simulate "not found" as the real repo does: wrapping repositories.ErrNotFound.
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
	if !errors.Is(err, infraErr) {
		t.Fatalf("expected wrapped infra error, got: %v", err)
	}
}

// infraErrorRepo returns an infrastructure error that is NOT repositories.ErrNotFound.
type infraErrorRepo struct {
	stubTypeRepo
	err error
}

func (r *infraErrorRepo) FindBySlug(_ context.Context, _ string) (*entities.ResourceType, error) {
	return nil, r.err
}

func TestEnsureProjection_GormNotFoundError_FallsBack(t *testing.T) {
	t.Parallel()
	pm := &stubProjMgr{}
	// Wrap repositories.ErrNotFound like the real repo does.
	notFoundRepo := &infraErrorRepo{
		err: repositories.ErrNotFound,
	}
	ctx := context.Background()

	childCtx := json.RawMessage(`{"@vocab":"https://schema.org/","rdfs:subClassOf":"parent"}`)
	err := ensureProjection(ctx, notFoundRepo, pm, "child", nil, childCtx)
	if err != nil {
		t.Fatalf("expected no error for not-found parent, got: %v", err)
	}
	if len(pm.ensuredSlugs) != 1 || pm.ensuredSlugs[0] != "child" {
		t.Fatalf("expected fallback to EnsureTable, got: %v", pm.ensuredSlugs)
	}
}
