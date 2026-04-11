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

package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"weos/api/handlers"
	"weos/application"
	"weos/domain/entities"
	"weos/domain/repositories"

	"github.com/labstack/echo/v4"
)

// stubResourceSvc captures just enough of ResourceService to drive Get.
// Methods not used here panic via the embedded interface if invoked, which
// makes accidental cross-calls (e.g. falling through to GetByID when we
// expect the flat path to handle everything) visibly fail.
type stubResourceSvc struct {
	application.ResourceService

	flatRow map[string]any
	flatErr error
	flatHit int

	byIDEntity *entities.Resource
	byIDErr    error
	byIDHit    int
}

func (s *stubResourceSvc) GetFlat(_ context.Context, _, _ string) (map[string]any, error) {
	s.flatHit++
	return s.flatRow, s.flatErr
}

func (s *stubResourceSvc) GetByID(_ context.Context, _ string) (*entities.Resource, error) {
	s.byIDHit++
	return s.byIDEntity, s.byIDErr
}

// stubTypeSvc returns a fixed resource type from GetBySlug and panics on
// anything else. Only the type-existence check is exercised by Get.
type stubTypeSvc struct {
	application.ResourceTypeService

	rt      *entities.ResourceType
	slugErr error
}

func (s *stubTypeSvc) GetBySlug(_ context.Context, _ string) (*entities.ResourceType, error) {
	if s.slugErr != nil {
		return nil, s.slugErr
	}
	return s.rt, nil
}

// noopHandlerLogger swallows log calls for handler tests that don't assert on logging.
type noopHandlerLogger struct{}

func (noopHandlerLogger) Info(_ context.Context, _ string, _ ...any)  {}
func (noopHandlerLogger) Warn(_ context.Context, _ string, _ ...any)  {}
func (noopHandlerLogger) Error(_ context.Context, _ string, _ ...any) {}

// recordingHandlerLogger captures Error calls so tests can assert that 500
// branches actually log the underlying cause.
type recordingHandlerLogger struct {
	noopHandlerLogger
	errors []string
}

func (l *recordingHandlerLogger) Error(_ context.Context, msg string, _ ...any) {
	l.errors = append(l.errors, msg)
}

// newHandler builds a ResourceHandler with the given stubs and a noop logger.
func newHandler(t *testing.T, svc application.ResourceService) *handlers.ResourceHandler {
	t.Helper()
	return handlers.NewResourceHandler(svc, &stubTypeSvc{rt: makeTestCourseType(t)}, noopHandlerLogger{})
}

func makeTestCourseType(t *testing.T) *entities.ResourceType {
	t.Helper()
	rt := &entities.ResourceType{}
	if err := rt.Restore(
		"urn:type:course", "Course", "course", "",
		"active", json.RawMessage(`{}`), json.RawMessage(`{}`),
		time.Unix(0, 0), 1,
	); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	return rt
}

func makeTestCourseEntity(t *testing.T, id string) *entities.Resource {
	t.Helper()
	e := &entities.Resource{}
	err := e.Restore(
		id, "course", "active",
		json.RawMessage(`{"@graph":[{"@id":"`+id+`","@type":"Course","name":"Intro"}]}`),
		"", "", time.Unix(0, 0), 1,
	)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	return e
}

// newGetRequest constructs an echo Context targeting ResourceHandler.Get with
// the typeSlug/id route params pre-populated.
func newGetRequest(t *testing.T, accept string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/resources/course/urn:course:abc", nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("typeSlug", "id")
	c.SetParamValues("course", "urn:course:abc")
	return c, rec
}

// TestResourceHandler_Get_FlatPathSuccess — non-JSON-LD request returns the
// flat row (200) and does NOT fall through to GetByID.
func TestResourceHandler_Get_FlatPathSuccess(t *testing.T) {
	t.Parallel()
	svc := &stubResourceSvc{flatRow: map[string]any{"id": "urn:course:abc", "name": "Intro"}}
	h := newHandler(t, svc)

	c, rec := newGetRequest(t, "application/json")
	if err := h.Get(c); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if svc.flatHit != 1 {
		t.Errorf("flatHit = %d, want 1", svc.flatHit)
	}
	if svc.byIDHit != 0 {
		t.Errorf("byIDHit = %d, want 0 (flat path must not fall through on success)", svc.byIDHit)
	}
}

// TestResourceHandler_Get_FlatPathAccessDenied — ErrAccessDenied surfaces as
// 403 without falling through to GetByID (which would silently re-deny).
func TestResourceHandler_Get_FlatPathAccessDenied(t *testing.T) {
	t.Parallel()
	svc := &stubResourceSvc{flatErr: entities.ErrAccessDenied}
	h := newHandler(t, svc)

	c, rec := newGetRequest(t, "application/json")
	if err := h.Get(c); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403", rec.Code)
	}
	if svc.byIDHit != 0 {
		t.Errorf("byIDHit = %d, want 0 (must not fall through on access denied)", svc.byIDHit)
	}
}

// TestResourceHandler_Get_FlatPathNoProjection_FallsThrough — the "no projection
// table" sentinel is a legitimate fall-through to GetByID; verify the canonical
// path then serves the response.
func TestResourceHandler_Get_FlatPathNoProjection_FallsThrough(t *testing.T) {
	t.Parallel()
	svc := &stubResourceSvc{
		flatErr:    repositories.ErrNoProjectionTable,
		byIDEntity: makeTestCourseEntity(t, "urn:course:abc"),
	}
	h := newHandler(t, svc)

	c, rec := newGetRequest(t, "application/json")
	if err := h.Get(c); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if svc.byIDHit != 1 {
		t.Errorf("byIDHit = %d, want 1 (fall-through should happen)", svc.byIDHit)
	}
}

// TestResourceHandler_Get_FlatPathNotFound_FallsThrough — a missing flat row
// should also fall through (the row may exist in the canonical table during
// a migration window).
func TestResourceHandler_Get_FlatPathNotFound_FallsThrough(t *testing.T) {
	t.Parallel()
	svc := &stubResourceSvc{
		flatErr:    repositories.ErrNotFound,
		byIDEntity: makeTestCourseEntity(t, "urn:course:abc"),
	}
	h := newHandler(t, svc)

	c, rec := newGetRequest(t, "application/json")
	if err := h.Get(c); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if svc.byIDHit != 1 {
		t.Errorf("byIDHit = %d, want 1", svc.byIDHit)
	}
}

// TestResourceHandler_Get_FlatPathUnexpectedError_Returns500 — real DB errors
// (anything not matching the known sentinels) must return 500 rather than
// silently falling through and being masked as 404 by the canonical path.
// Also verifies the underlying error is logged so operators have context.
func TestResourceHandler_Get_FlatPathUnexpectedError_Returns500(t *testing.T) {
	t.Parallel()
	svc := &stubResourceSvc{flatErr: errors.New("connection reset by peer")}
	logger := &recordingHandlerLogger{}
	h := handlers.NewResourceHandler(svc, &stubTypeSvc{rt: makeTestCourseType(t)}, logger)

	c, rec := newGetRequest(t, "application/json")
	if err := h.Get(c); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500 (real DB errors must not be masked)", rec.Code)
	}
	if svc.byIDHit != 0 {
		t.Errorf("byIDHit = %d, want 0 (must not fall through on unexpected error)", svc.byIDHit)
	}
	if len(logger.errors) != 1 {
		t.Errorf("logger.Error calls = %d, want 1 (operators must see DB error context)", len(logger.errors))
	}
}

// TestResourceHandler_Get_JSONLDBypassesFlat — JSON-LD clients get the
// canonical entity, never the flat row.
func TestResourceHandler_Get_JSONLDBypassesFlat(t *testing.T) {
	t.Parallel()
	svc := &stubResourceSvc{byIDEntity: makeTestCourseEntity(t, "urn:course:abc")}
	h := newHandler(t, svc)

	c, rec := newGetRequest(t, "application/ld+json")
	if err := h.Get(c); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if svc.flatHit != 0 {
		t.Errorf("flatHit = %d, want 0 (JSON-LD requests must not call GetFlat)", svc.flatHit)
	}
	if svc.byIDHit != 1 {
		t.Errorf("byIDHit = %d, want 1", svc.byIDHit)
	}
}

// TestResourceHandler_Get_CanonicalPathNotFoundReturns404 — after a
// legitimate fall-through, if GetByID also returns ErrNotFound, respond 404.
func TestResourceHandler_Get_CanonicalPathNotFoundReturns404(t *testing.T) {
	t.Parallel()
	svc := &stubResourceSvc{
		flatErr: repositories.ErrNoProjectionTable,
		byIDErr: repositories.ErrNotFound,
	}
	h := newHandler(t, svc)

	c, rec := newGetRequest(t, "application/json")
	if err := h.Get(c); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rec.Code)
	}
}

// TestResourceHandler_Get_CanonicalPathUnexpectedErrorReturns500 — distinct
// from not-found: real errors on the canonical path must not be masked as 404.
// Also verifies the error is logged so operators have context.
func TestResourceHandler_Get_CanonicalPathUnexpectedErrorReturns500(t *testing.T) {
	t.Parallel()
	svc := &stubResourceSvc{
		flatErr: repositories.ErrNoProjectionTable,
		byIDErr: errors.New("disk full"),
	}
	logger := &recordingHandlerLogger{}
	h := handlers.NewResourceHandler(svc, &stubTypeSvc{rt: makeTestCourseType(t)}, logger)

	c, rec := newGetRequest(t, "application/json")
	if err := h.Get(c); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", rec.Code)
	}
	if len(logger.errors) != 1 {
		t.Errorf("logger.Error calls = %d, want 1 (operators must see DB error context)", len(logger.errors))
	}
}
