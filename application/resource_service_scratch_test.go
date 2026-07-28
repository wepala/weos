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
	"testing"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// scratchRoundtripRepo is a ResourceRepository whose only live method is
// FindByID (the sole repo call Update makes before commit). Everything else
// panics via the embedded interface so an unexpected call fails loudly.
type scratchRoundtripRepo struct {
	repositories.ResourceRepository
	entity *entities.Resource
}

func (r *scratchRoundtripRepo) FindByID(_ context.Context, _ string) (*entities.Resource, error) {
	return r.entity, nil
}

// scratchRoundtripTripleRepo returns no existing triples so reconcileTriples on
// the update path is a clean no-op.
type scratchRoundtripTripleRepo struct {
	repositories.TripleRepository
}

func (r *scratchRoundtripTripleRepo) FindBySubject(
	_ context.Context, _ string,
) ([]repositories.Triple, error) {
	return nil, nil
}

// scratchBehavior stashes a sentinel in the per-call scratch during its Before
// hook and records, in its After hook, whether the same sentinel came back —
// proving the scratch seeded at the top of Create/Update is the one every hook
// of that call shares. Create and Update use distinct sentinels so a rail can't
// accidentally pass by reading the other rail's value.
type scratchBehavior struct {
	entities.DefaultBehavior

	createSentinel string
	updateSentinel string

	afterCreateSaw string
	afterCreateOK  bool
	afterUpdateSaw string
	afterUpdateOK  bool
}

func (b *scratchBehavior) BeforeCreate(
	ctx context.Context, data json.RawMessage, _ *entities.ResourceType,
) (json.RawMessage, error) {
	if s := ScratchFromContext(ctx); s != nil {
		s.Set("sentinel", b.createSentinel)
	}
	return data, nil
}

func (b *scratchBehavior) AfterCreate(ctx context.Context, _ *entities.Resource) error {
	if s := ScratchFromContext(ctx); s != nil {
		v, ok := s.Get("sentinel")
		b.afterCreateOK = ok
		if sv, isStr := v.(string); isStr {
			b.afterCreateSaw = sv
		}
	}
	return nil
}

func (b *scratchBehavior) BeforeUpdate(
	ctx context.Context, _ *entities.Resource, data json.RawMessage, _ *entities.ResourceType,
) (json.RawMessage, error) {
	if s := ScratchFromContext(ctx); s != nil {
		s.Set("sentinel", b.updateSentinel)
	}
	return data, nil
}

func (b *scratchBehavior) AfterUpdate(ctx context.Context, _ *entities.Resource) error {
	if s := ScratchFromContext(ctx); s != nil {
		v, ok := s.Get("sentinel")
		b.afterUpdateOK = ok
		if sv, isStr := v.(string); isStr {
			b.afterUpdateSaw = sv
		}
	}
	return nil
}

// TestResourceService_ScratchRoundtripAcrossHooks drives the REAL Create and
// Update rails end-to-end (behavior hooks → schema validation → UoW commit) and
// asserts a behavior's Before hook and After hook of the same call see one
// shared scratch — the contract BehaviorScratch exists to provide.
func TestResourceService_ScratchRoundtripAcrossHooks(t *testing.T) {
	t.Parallel()

	rt := makeTestRT("scratch-thing", json.RawMessage(`{"@vocab":"https://schema.org/"}`))
	behavior := &scratchBehavior{
		createSentinel: "from-before-create",
		updateSentinel: "from-before-update",
	}

	svc := &resourceService{
		repo: &scratchRoundtripRepo{
			entity: restoredResource(t, "urn:scratch-thing:1", "scratch-thing"),
		},
		typeRepo:   &stubTypeRepo{types: map[string]*entities.ResourceType{"scratch-thing": rt}},
		tripleRepo: &scratchRoundtripTripleRepo{},
		eventStore: &stubEventStore{},
		dispatcher: domain.NewEventDispatcher(),
		logger:     noopLogger{},
		behaviors:  ResourceBehaviorRegistry{"scratch-thing": behavior},
	}

	ctx := context.Background()

	if _, err := svc.Create(ctx, CreateResourceCommand{
		TypeSlug: "scratch-thing",
		Data:     json.RawMessage(`{"name":"test"}`),
	}); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if !behavior.afterCreateOK {
		t.Fatal("AfterCreate saw no scratch value — the create rail's hooks did not share a scratch")
	}
	if behavior.afterCreateSaw != behavior.createSentinel {
		t.Errorf("AfterCreate saw %q, want the BeforeCreate sentinel %q",
			behavior.afterCreateSaw, behavior.createSentinel)
	}

	if _, err := svc.Update(ctx, UpdateResourceCommand{
		ID:   "urn:scratch-thing:1",
		Data: json.RawMessage(`{"name":"updated"}`),
	}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if !behavior.afterUpdateOK {
		t.Fatal("AfterUpdate saw no scratch value — the update rail's hooks did not share a scratch")
	}
	if behavior.afterUpdateSaw != behavior.updateSentinel {
		t.Errorf("AfterUpdate saw %q, want the BeforeUpdate sentinel %q",
			behavior.afterUpdateSaw, behavior.updateSentinel)
	}
}
