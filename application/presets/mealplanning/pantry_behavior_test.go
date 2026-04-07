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

package mealplanning

import (
	"context"
	"encoding/json"
	"testing"

	"weos/application"
	"weos/domain/entities"
	"weos/domain/repositories"
)

// stubResourceSvc is a minimal ResourceService stub for behavior tests.
// It stores pre-seeded flat resources and records Update/Delete calls.
type stubResourceSvc struct {
	// listFlatData holds per-typeSlug pre-seeded results for ListFlatWithFilters.
	// When listFlatFilter is set for a type, it's called to filter results.
	listFlatData map[string][]map[string]any
	// getByIDData holds pre-seeded resources returned by GetByID.
	getByIDData map[string]*entities.Resource
	// getByIDErr holds canned errors for GetByID.
	getByIDErr map[string]error
	// listFlatErr holds canned errors for ListFlatWithFilters.
	listFlatErr map[string]error
	// updateErr forces Update to fail when set.
	updateErr error
	// deleteErr forces Delete to fail when set.
	deleteErr error
	// updates records all Update calls.
	updates []application.UpdateResourceCommand
	// creates records all Create calls.
	creates []application.CreateResourceCommand
	// deletes records all Delete calls.
	deletes []application.DeleteResourceCommand
	// createErr forces Create to fail when set.
	createErr error
}

func newStubResourceSvc() *stubResourceSvc {
	return &stubResourceSvc{
		listFlatData: make(map[string][]map[string]any),
		getByIDData:  make(map[string]*entities.Resource),
		getByIDErr:   make(map[string]error),
		listFlatErr:  make(map[string]error),
	}
}

func (s *stubResourceSvc) Create(
	_ context.Context, cmd application.CreateResourceCommand,
) (*entities.Resource, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	s.creates = append(s.creates, cmd)
	return nil, nil //nolint:nilnil
}

func (s *stubResourceSvc) GetByID(
	_ context.Context, id string,
) (*entities.Resource, error) {
	if err, ok := s.getByIDErr[id]; ok {
		return nil, err
	}
	if r, ok := s.getByIDData[id]; ok {
		return r, nil
	}
	return nil, nil //nolint:nilnil
}

func (s *stubResourceSvc) List(
	context.Context, string, string, int, repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	return repositories.PaginatedResponse[*entities.Resource]{}, nil
}

func (s *stubResourceSvc) ListFlat(
	context.Context, string, string, int, repositories.SortOptions,
) (repositories.PaginatedResponse[map[string]any], error) {
	return repositories.PaginatedResponse[map[string]any]{}, nil
}

func (s *stubResourceSvc) ListByField(
	context.Context, string, string, string,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	return repositories.PaginatedResponse[*entities.Resource]{}, nil
}

func (s *stubResourceSvc) ListWithFilters(
	context.Context, string, []repositories.FilterCondition,
	string, int, repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	return repositories.PaginatedResponse[*entities.Resource]{}, nil
}

func (s *stubResourceSvc) ListFlatWithFilters(
	_ context.Context, typeSlug string, filters []repositories.FilterCondition,
	_ string, _ int, _ repositories.SortOptions,
) (repositories.PaginatedResponse[map[string]any], error) {
	if err, ok := s.listFlatErr[typeSlug]; ok {
		return repositories.PaginatedResponse[map[string]any]{}, err
	}
	data := s.listFlatData[typeSlug]
	// Apply simple eq filters so tests can pre-seed a superset and
	// behaviors see the filtered subset.
	if len(filters) > 0 {
		filtered := make([]map[string]any, 0, len(data))
		for _, row := range data {
			if matchesAllFilters(row, filters) {
				filtered = append(filtered, row)
			}
		}
		data = filtered
	}
	return repositories.PaginatedResponse[map[string]any]{Data: data}, nil
}

func matchesAllFilters(row map[string]any, filters []repositories.FilterCondition) bool {
	for _, f := range filters {
		if f.Operator != "eq" {
			continue
		}
		v, ok := row[f.Field]
		if !ok {
			return false
		}
		// Coerce to string for comparison since FilterCondition.Value is string.
		switch val := v.(type) {
		case string:
			if val != f.Value {
				return false
			}
		case bool:
			if (val && f.Value != "true") || (!val && f.Value != "false") {
				return false
			}
		default:
			// No other coercions needed for these tests.
			return false
		}
	}
	return true
}

func (s *stubResourceSvc) Update(
	_ context.Context, cmd application.UpdateResourceCommand,
) (*entities.Resource, error) {
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	s.updates = append(s.updates, cmd)
	return nil, nil //nolint:nilnil
}

func (s *stubResourceSvc) Delete(
	_ context.Context, cmd application.DeleteResourceCommand,
) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.deletes = append(s.deletes, cmd)
	return nil
}

// makeTestResource constructs a minimal Resource entity for behavior tests.
func makeTestResource(t *testing.T, id, typeSlug string, data map[string]any) *entities.Resource {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("failed to marshal test data: %v", err)
	}
	r, err := new(entities.Resource).With(id, typeSlug, raw, "", "")
	if err != nil {
		t.Fatalf("failed to create test resource: %v", err)
	}
	return r
}

func TestEnforceSingleDefault_NonDefaultIsNoop(t *testing.T) {
	t.Parallel()
	stub := newStubResourceSvc()
	b := NewEnforceSingleDefaultBehavior()
	b.SetDependencies(entities.BehaviorDependencies{
		ResourceSvc: application.ResourceService(stub),
		Logger:      noopLogger{},
	})

	// Pre-seed an existing default pantry that should NOT be touched.
	stub.listFlatData["pantry"] = []map[string]any{
		{"id": "pantry-other", "name": "Other", "isDefault": true},
	}

	// The new pantry is NOT marked as default.
	resource := makeTestResource(t, "pantry-new", "pantry", map[string]any{
		"name":      "New",
		"isDefault": false,
	})

	if err := b.AfterCreate(context.Background(), resource); err != nil {
		t.Fatalf("AfterCreate returned error: %v", err)
	}

	if len(stub.updates) != 0 {
		t.Fatalf("expected no updates, got %d", len(stub.updates))
	}
}

func TestEnforceSingleDefault_UnsetsOthers(t *testing.T) {
	t.Parallel()
	stub := newStubResourceSvc()
	b := NewEnforceSingleDefaultBehavior()
	b.SetDependencies(entities.BehaviorDependencies{
		ResourceSvc: application.ResourceService(stub),
		Logger:      noopLogger{},
	})

	// Two existing default pantries; the new one is also default.
	stub.listFlatData["pantry"] = []map[string]any{
		{"id": "pantry-a", "name": "A", "isDefault": true},
		{"id": "pantry-b", "name": "B", "isDefault": true},
		{"id": "pantry-new", "name": "New", "isDefault": true},
	}

	resource := makeTestResource(t, "pantry-new", "pantry", map[string]any{
		"name":      "New",
		"isDefault": true,
	})

	if err := b.AfterCreate(context.Background(), resource); err != nil {
		t.Fatalf("AfterCreate returned error: %v", err)
	}

	// Expect updates for pantry-a and pantry-b but NOT pantry-new.
	if len(stub.updates) != 2 {
		t.Fatalf("expected 2 updates, got %d", len(stub.updates))
	}
	updatedIDs := map[string]bool{}
	for _, u := range stub.updates {
		updatedIDs[u.ID] = true
	}
	if !updatedIDs["pantry-a"] || !updatedIDs["pantry-b"] {
		t.Fatalf("expected updates for pantry-a and pantry-b, got %v", updatedIDs)
	}
	if updatedIDs["pantry-new"] {
		t.Fatal("should not update the resource being enforced")
	}

	// Verify each update clears isDefault.
	for _, u := range stub.updates {
		var m map[string]any
		if err := json.Unmarshal(u.Data, &m); err != nil {
			t.Fatalf("invalid update data: %v", err)
		}
		if v, _ := m["isDefault"].(bool); v {
			t.Fatalf("expected isDefault false in update for %s", u.ID)
		}
	}
}

func TestEnforceSingleDefault_NilServiceSafe(t *testing.T) {
	t.Parallel()
	// Behavior with no injected service must not panic.
	b := NewEnforceSingleDefaultBehavior()
	resource := makeTestResource(t, "pantry-new", "pantry", map[string]any{
		"name":      "New",
		"isDefault": true,
	})
	if err := b.AfterCreate(context.Background(), resource); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// noopLogger is a no-op entities.Logger for tests.
type noopLogger struct{}

func (noopLogger) Info(context.Context, string, ...any)  {}
func (noopLogger) Warn(context.Context, string, ...any)  {}
func (noopLogger) Error(context.Context, string, ...any) {}
