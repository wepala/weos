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
	"testing"
	"time"

	"weos/domain/entities"
	"weos/domain/repositories"
)

// getFlatStubRepo implements the subset of ResourceRepository that
// ResourceService.GetFlat exercises: FindByID (via GetByID) and FindFlatByID.
// Other methods panic via the embedded interface — we want the test to fail
// loudly if GetFlat ever calls something unexpected.
type getFlatStubRepo struct {
	repositories.ResourceRepository

	findByIDResource *entities.Resource
	findByIDErr      error
	findByIDCalls    int

	findFlatRow    map[string]any
	findFlatErr    error
	findFlatCalls  int
	findFlatArgIDs []string
}

func (r *getFlatStubRepo) FindByID(_ context.Context, _ string) (*entities.Resource, error) {
	r.findByIDCalls++
	return r.findByIDResource, r.findByIDErr
}

func (r *getFlatStubRepo) FindFlatByID(_ context.Context, _, id string) (map[string]any, error) {
	r.findFlatCalls++
	r.findFlatArgIDs = append(r.findFlatArgIDs, id)
	return r.findFlatRow, r.findFlatErr
}

// restoredResource builds an *entities.Resource for tests via Restore,
// bypassing event sourcing so tests don't need to drive a full UoW.
func restoredResource(t *testing.T, id, typeSlug string) *entities.Resource {
	t.Helper()
	e := &entities.Resource{}
	if err := e.Restore(
		id, typeSlug, "active",
		json.RawMessage(`{"name":"x"}`),
		"", "", // no createdBy/accountID → access check passes in system ctx
		time.Unix(0, 0), 1,
	); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	return e
}

// TestResourceService_GetFlat_Success walks the happy path: FindByID approves
// access, typeSlug matches, the flat row comes back.
func TestResourceService_GetFlat_Success(t *testing.T) {
	t.Parallel()
	repo := &getFlatStubRepo{
		findByIDResource: restoredResource(t, "urn:course:abc", "course"),
		findFlatRow:      map[string]any{"id": "urn:course:abc", "name": "Intro"},
	}
	svc := &resourceService{repo: repo, logger: noopLogger{}}

	row, err := svc.GetFlat(context.Background(), "course", "urn:course:abc")
	if err != nil {
		t.Fatalf("GetFlat: %v", err)
	}
	if row["name"] != "Intro" {
		t.Errorf("row[name] = %v, want Intro", row["name"])
	}
	if repo.findFlatCalls != 1 {
		t.Errorf("FindFlatByID calls = %d, want 1", repo.findFlatCalls)
	}
}

// TestResourceService_GetFlat_AccessDeniedBlocksRepoCall is the IDR-prevention
// regression test: when GetByID denies access, FindFlatByID must NOT be called.
// A future refactor that swapped the order would silently leak flat rows.
func TestResourceService_GetFlat_AccessDeniedBlocksRepoCall(t *testing.T) {
	t.Parallel()
	repo := &getFlatStubRepo{findByIDErr: entities.ErrAccessDenied}
	svc := &resourceService{repo: repo, logger: noopLogger{}}

	_, err := svc.GetFlat(context.Background(), "course", "urn:course:abc")
	if !errors.Is(err, entities.ErrAccessDenied) {
		t.Fatalf("err = %v, want ErrAccessDenied", err)
	}
	if repo.findFlatCalls != 0 {
		t.Errorf("FindFlatByID called %d times after access denied — must be 0", repo.findFlatCalls)
	}
}

// TestResourceService_GetFlat_TypeSlugMismatchRejects verifies the cross-type
// guard: a URL like /resources/course/<product-id> must NOT return the product
// row via the course projection table. Without this check, GetByID would
// approve access (based on the product's ACL) and FindFlatByID would query
// the wrong projection, potentially leaking data if IDs collided across types.
func TestResourceService_GetFlat_TypeSlugMismatchRejects(t *testing.T) {
	t.Parallel()
	repo := &getFlatStubRepo{
		findByIDResource: restoredResource(t, "urn:product:xyz", "product"),
	}
	svc := &resourceService{repo: repo, logger: noopLogger{}}

	_, err := svc.GetFlat(context.Background(), "course", "urn:product:xyz")
	if !errors.Is(err, repositories.ErrNotFound) {
		t.Fatalf("err = %v, want repositories.ErrNotFound", err)
	}
	if repo.findFlatCalls != 0 {
		t.Errorf("FindFlatByID called %d times on type mismatch — must be 0", repo.findFlatCalls)
	}
}

// TestResourceService_GetFlat_NoProjectionTablePropagates verifies that
// ErrNoProjectionTable from the repo surfaces to the caller unchanged, so
// the handler can fall back to the canonical entity path.
func TestResourceService_GetFlat_NoProjectionTablePropagates(t *testing.T) {
	t.Parallel()
	repo := &getFlatStubRepo{
		findByIDResource: restoredResource(t, "urn:course:abc", "course"),
		findFlatErr:      repositories.ErrNoProjectionTable,
	}
	svc := &resourceService{repo: repo, logger: noopLogger{}}

	_, err := svc.GetFlat(context.Background(), "course", "urn:course:abc")
	if !errors.Is(err, repositories.ErrNoProjectionTable) {
		t.Errorf("err = %v, want wrapping ErrNoProjectionTable", err)
	}
}
