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

// stubResourceSvc is the backing data store for behavior tests. It records
// write calls and holds pre-seeded data for reads. Access it through the
// stubWriter and stubRepo wrappers that implement ResourceWriter and
// ResourceRepository respectively.
type stubResourceSvc struct {
	// listFlatData holds per-typeSlug pre-seeded results for FindAllByTypeFlatWithFilters.
	listFlatData map[string][]map[string]any
	// getByIDData holds pre-seeded resources returned by FindByID.
	getByIDData map[string]*entities.Resource
	// getByIDErr holds canned errors for FindByID.
	getByIDErr map[string]error
	// listFlatErr holds canned errors for FindAllByTypeFlatWithFilters.
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

// testBehaviorServices builds an application.BehaviorServices from the stub.
func testBehaviorServices(stub *stubResourceSvc) application.BehaviorServices {
	return application.BehaviorServices{
		Resources: &stubRepo{stub},
		Logger:    noopLogger{},
		Writer:    &stubWriter{stub},
	}
}

// -- stubWriter implements application.ResourceWriter -------------------------

type stubWriter struct{ s *stubResourceSvc }

func (w *stubWriter) Create(
	_ context.Context, cmd application.CreateResourceCommand,
) (*entities.Resource, error) {
	if w.s.createErr != nil {
		return nil, w.s.createErr
	}
	w.s.creates = append(w.s.creates, cmd)
	return nil, nil //nolint:nilnil
}

func (w *stubWriter) Update(
	_ context.Context, cmd application.UpdateResourceCommand,
) (*entities.Resource, error) {
	if w.s.updateErr != nil {
		return nil, w.s.updateErr
	}
	w.s.updates = append(w.s.updates, cmd)
	return nil, nil //nolint:nilnil
}

func (w *stubWriter) Delete(
	_ context.Context, cmd application.DeleteResourceCommand,
) error {
	if w.s.deleteErr != nil {
		return w.s.deleteErr
	}
	w.s.deletes = append(w.s.deletes, cmd)
	return nil
}

// -- stubRepo implements repositories.ResourceRepository ----------------------

type stubRepo struct{ s *stubResourceSvc }

func (*stubRepo) Save(context.Context, *entities.Resource) error { return nil }

func (r *stubRepo) FindByID(
	_ context.Context, id string,
) (*entities.Resource, error) {
	if err, ok := r.s.getByIDErr[id]; ok {
		return nil, err
	}
	if res, ok := r.s.getByIDData[id]; ok {
		return res, nil
	}
	return nil, nil //nolint:nilnil
}

func (*stubRepo) FindAllByType(
	context.Context, string, string, int,
	repositories.SortOptions, *repositories.VisibilityScope,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	return repositories.PaginatedResponse[*entities.Resource]{}, nil
}

func (*stubRepo) FindAllByTypeAndField(
	context.Context, string, string, string,
) ([]*entities.Resource, error) {
	return nil, nil
}

func (*stubRepo) FindAllByTypeWithFilters(
	context.Context, string, []repositories.FilterCondition,
	string, int, repositories.SortOptions, *repositories.VisibilityScope,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	return repositories.PaginatedResponse[*entities.Resource]{}, nil
}

func (*stubRepo) Update(context.Context, *entities.Resource) error { return nil }

func (*stubRepo) UpdateData(context.Context, string, json.RawMessage, int) error { return nil }

func (*stubRepo) Delete(context.Context, string) error { return nil }

func (*stubRepo) FindAllByTypeFlat(
	context.Context, string, string, int,
	repositories.SortOptions, *repositories.VisibilityScope,
) (repositories.PaginatedResponse[map[string]any], error) {
	return repositories.PaginatedResponse[map[string]any]{}, nil
}

func (r *stubRepo) FindAllByTypeFlatWithFilters(
	_ context.Context, typeSlug string, filters []repositories.FilterCondition,
	_ string, _ int, _ repositories.SortOptions, _ *repositories.VisibilityScope,
) (repositories.PaginatedResponse[map[string]any], error) {
	if err, ok := r.s.listFlatErr[typeSlug]; ok {
		return repositories.PaginatedResponse[map[string]any]{}, err
	}
	data := r.s.listFlatData[typeSlug]
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
			// Accept both "true"/"false" and "1"/"0" to match the portable
			// boolean representation used by behavior code.
			trueVals := map[string]bool{"true": true, "1": true}
			falseVals := map[string]bool{"false": true, "0": true}
			if val && !trueVals[f.Value] {
				return false
			}
			if !val && !falseVals[f.Value] {
				return false
			}
		default:
			// No other coercions needed for these tests.
			return false
		}
	}
	return true
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
	b := newEnforceSingleDefaultBehavior(testBehaviorServices(stub))

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
	b := newEnforceSingleDefaultBehavior(testBehaviorServices(stub))

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
	// Behavior with nil writer must not panic.
	b := newEnforceSingleDefaultBehavior(application.BehaviorServices{})
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
