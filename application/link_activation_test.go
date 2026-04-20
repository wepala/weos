// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package application

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
)

// listingTypeRepo is a stubTypeRepo-like minimal fake that returns a
// predictable slice from FindAll. The existing stubTypeRepo.FindAll returns
// an empty page, which isn't useful for activation tests.
type listingTypeRepo struct {
	slugs []string
}

func (r *listingTypeRepo) Save(context.Context, *entities.ResourceType) error { return nil }
func (r *listingTypeRepo) FindByID(context.Context, string) (*entities.ResourceType, error) {
	return nil, repositories.ErrNotFound
}
func (r *listingTypeRepo) FindBySlug(_ context.Context, slug string) (*entities.ResourceType, error) {
	for _, s := range r.slugs {
		if s == slug {
			return fakeResourceType(s), nil
		}
	}
	return nil, repositories.ErrNotFound
}
func (r *listingTypeRepo) FindAll(
	_ context.Context, _ string, _ int,
) (repositories.PaginatedResponse[*entities.ResourceType], error) {
	items := make([]*entities.ResourceType, 0, len(r.slugs))
	for _, s := range r.slugs {
		items = append(items, fakeResourceType(s))
	}
	return repositories.PaginatedResponse[*entities.ResourceType]{
		Data: items, HasMore: false,
	}, nil
}
func (r *listingTypeRepo) Update(context.Context, *entities.ResourceType) error { return nil }
func (r *listingTypeRepo) Delete(context.Context, string) error                 { return nil }

func fakeResourceType(slug string) *entities.ResourceType {
	rt := &entities.ResourceType{}
	_ = rt.Restore("id-"+slug, slug, slug, "", "active",
		json.RawMessage(`{}`), json.RawMessage(`{}`), time.Now(), 1)
	return rt
}

// recordingProjMgr captures RegisterLink calls so tests can assert activation
// behavior without a real DB. A per-call error override (keyed on source slug)
// lets tests exercise the failure branch without swapping the whole fake.
type recordingProjMgr struct {
	mu     sync.Mutex
	calls  []repositories.LinkReference
	errors map[string]error // source slug → error to return
	stubProjMgr
}

func (r *recordingProjMgr) RegisterLink(_ context.Context, ref repositories.LinkReference) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, ref)
	if r.errors != nil {
		if err, ok := r.errors[ref.SourceSlug]; ok {
			return err
		}
	}
	return nil
}

func (r *recordingProjMgr) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// newTestLinkActivator panics on construction errors so test call sites stay
// readable — production wiring uses NewLinkActivator directly.
func newTestLinkActivator(
	t *testing.T, registry *LinkRegistry, pm repositories.ProjectionManager,
	repo repositories.ResourceTypeRepository,
) *LinkActivator {
	t.Helper()
	a, err := NewLinkActivator(registry, pm, repo, noopLogger{})
	if err != nil {
		t.Fatalf("NewLinkActivator: %v", err)
	}
	return a
}

func TestLinkActivator_ActivatesWhenBothEndpointsInstalled(t *testing.T) {
	t.Parallel()
	registry := NewLinkRegistry()
	_ = registry.Add(PresetLinkDefinition{
		SourceType: "invoice", TargetType: "guardian",
		PropertyName: "guardian", DisplayProperty: "name",
	})
	pm := &recordingProjMgr{}
	repo := &listingTypeRepo{slugs: []string{"invoice", "guardian"}}
	act := newTestLinkActivator(t, registry, pm, repo)

	if err := act.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if pm.callCount() != 1 {
		t.Fatalf("expected 1 RegisterLink call, got %d", pm.callCount())
	}
	got := pm.calls[0]
	if got.SourceSlug != "invoice" || got.TargetSlug != "guardian" ||
		got.PropertyName != "guardian" || got.DisplayProperty != "name" {
		t.Errorf("unexpected call args: %+v", got)
	}
}

func TestLinkActivator_SkipsWhenEndpointMissing(t *testing.T) {
	t.Parallel()
	registry := NewLinkRegistry()
	_ = registry.Add(PresetLinkDefinition{
		SourceType: "invoice", TargetType: "guardian", PropertyName: "guardian",
	})
	pm := &recordingProjMgr{}
	// Only one side installed — link must stay dormant.
	repo := &listingTypeRepo{slugs: []string{"invoice"}}
	act := newTestLinkActivator(t, registry, pm, repo)

	if err := act.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if pm.callCount() != 0 {
		t.Errorf("expected 0 RegisterLink calls with missing endpoint, got %d", pm.callCount())
	}
}

func TestLinkActivator_ActivatesLateInstalledTarget(t *testing.T) {
	t.Parallel()
	registry := NewLinkRegistry()
	_ = registry.Add(PresetLinkDefinition{
		SourceType: "invoice", TargetType: "guardian", PropertyName: "guardian",
	})
	pm := &recordingProjMgr{}
	repo := &listingTypeRepo{slugs: []string{"invoice"}} // target missing
	act := newTestLinkActivator(t, registry, pm, repo)

	// First reconcile: dormant.
	_ = act.Reconcile(context.Background())
	if pm.callCount() != 0 {
		t.Fatalf("expected dormant, got %d calls", pm.callCount())
	}

	// Target installed — reconcile again.
	repo.slugs = append(repo.slugs, "guardian")
	if err := act.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if pm.callCount() != 1 {
		t.Errorf("expected 1 activation after late install, got %d", pm.callCount())
	}
}

func TestLinkActivator_ReconcileIsSafeToRepeat(t *testing.T) {
	t.Parallel()
	registry := NewLinkRegistry()
	_ = registry.Add(PresetLinkDefinition{
		SourceType: "invoice", TargetType: "guardian", PropertyName: "guardian",
	})
	pm := &recordingProjMgr{}
	repo := &listingTypeRepo{slugs: []string{"invoice", "guardian"}}
	act := newTestLinkActivator(t, registry, pm, repo)

	// Each reconcile calls RegisterLink; the underlying ProjectionManager is
	// what dedups — LinkActivator doesn't need to track prior activations.
	// Dedup correctness is covered by TestRegisterLink_Idempotent in the
	// projection_manager_test.go suite.
	if err := act.Reconcile(context.Background()); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	if err := act.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if pm.callCount() != 2 {
		t.Errorf("expected 2 calls (one per reconcile), got %d", pm.callCount())
	}
}

// Per-link RegisterLink failures must be logged, counted, and not stop
// subsequent links from being attempted. Reconcile returns an aggregate
// error so callers can escalate; siblings still get a chance to activate.
func TestLinkActivator_PerLinkErrorIsSurfacedAndSiblingsProceed(t *testing.T) {
	t.Parallel()
	registry := NewLinkRegistry()
	_ = registry.Add(PresetLinkDefinition{
		SourceType: "bad", TargetType: "guardian", PropertyName: "guardian",
	})
	_ = registry.Add(PresetLinkDefinition{
		SourceType: "good", TargetType: "guardian", PropertyName: "guardian",
	})

	pm := &recordingProjMgr{errors: map[string]error{
		"bad": errors.New("alter table failed"),
	}}
	repo := &listingTypeRepo{slugs: []string{"bad", "good", "guardian"}}
	act := newTestLinkActivator(t, registry, pm, repo)

	err := act.Reconcile(context.Background())
	if err == nil {
		t.Fatalf("expected aggregate error when a link fails, got nil")
	}
	// Sibling activation still attempted: both links were dispatched to the
	// projection manager.
	if pm.callCount() != 2 {
		t.Errorf("expected 2 RegisterLink attempts despite failure, got %d", pm.callCount())
	}
}

func TestNewLinkActivator_RejectsMissingDependencies(t *testing.T) {
	t.Parallel()
	registry := NewLinkRegistry()
	pm := &recordingProjMgr{}
	repo := &listingTypeRepo{}
	logger := noopLogger{}

	cases := []struct {
		name string
		r    *LinkRegistry
		p    repositories.ProjectionManager
		t    repositories.ResourceTypeRepository
		l    entities.Logger
	}{
		{"nil registry", nil, pm, repo, logger},
		{"nil projection manager", registry, nil, repo, logger},
		{"nil type repo", registry, pm, nil, logger},
		{"nil logger", registry, pm, repo, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewLinkActivator(tc.r, tc.p, tc.t, tc.l); err == nil {
				t.Errorf("%s: expected error, got nil", tc.name)
			}
		})
	}
}
