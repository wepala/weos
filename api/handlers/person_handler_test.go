package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/api/handlers"
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/labstack/echo/v4"
)

// stubPersonSvc captures calls so tests can assert handler behavior.
type stubPersonSvc struct {
	application.ResourceService

	listFilters []repositories.FilterCondition
	listResult  repositories.PaginatedResponse[*entities.Resource]
	listErr     error

	getByIDEntity *entities.Resource
	getByIDErr    error

	updateData   json.RawMessage
	updateEntity *entities.Resource
	updateErr    error
}

func (s *stubPersonSvc) List(
	_ context.Context, _ string, _ string, _ int, _ repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	return s.listResult, s.listErr
}

func (s *stubPersonSvc) ListWithFilters(
	_ context.Context, _ string, filters []repositories.FilterCondition,
	_ string, _ int, _ repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	s.listFilters = filters
	return s.listResult, s.listErr
}

func (s *stubPersonSvc) GetByID(_ context.Context, _ string) (*entities.Resource, error) {
	return s.getByIDEntity, s.getByIDErr
}

func (s *stubPersonSvc) Update(_ context.Context, cmd application.UpdateResourceCommand) (*entities.Resource, error) {
	s.updateData = cmd.Data
	return s.updateEntity, s.updateErr
}

func makeTestPersonEntity(t *testing.T, id string) *entities.Resource {
	t.Helper()
	e := &entities.Resource{}
	err := e.Restore(
		id, "person", "active",
		json.RawMessage(`{"@graph":[{"@id":"`+id+`","@type":"Person","givenName":"Jane"}]}`),
		"", "", time.Unix(0, 0), 1,
	)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	return e
}

func TestPersonHandler_List_FilterFieldMapping(t *testing.T) {
	t.Parallel()
	svc := &stubPersonSvc{
		listResult: repositories.PaginatedResponse[*entities.Resource]{
			Data: []*entities.Resource{makeTestPersonEntity(t, "urn:person:1")},
		},
	}
	h := handlers.NewPersonHandler(svc)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet,
		"/api/persons?_filter[given_name][eq]=Jane&_filter[family_name][eq]=Doe", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.List(c); err != nil {
		t.Fatalf("List: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if len(svc.listFilters) != 2 {
		t.Fatalf("listFilters len = %d, want 2", len(svc.listFilters))
	}

	want := map[string]string{
		"givenName":  "Jane",
		"familyName": "Doe",
	}
	for _, f := range svc.listFilters {
		expected, ok := want[f.Field]
		if !ok {
			t.Errorf("unexpected filter field %q (snake_case not translated?)", f.Field)
			continue
		}
		if f.Value != expected {
			t.Errorf("filter %q value = %q, want %q", f.Field, f.Value, expected)
		}
	}
}

func TestPersonHandler_List_UnmappedFilterPassedThrough(t *testing.T) {
	t.Parallel()
	svc := &stubPersonSvc{
		listResult: repositories.PaginatedResponse[*entities.Resource]{
			Data: []*entities.Resource{makeTestPersonEntity(t, "urn:person:1")},
		},
	}
	h := handlers.NewPersonHandler(svc)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet,
		"/api/persons?_filter[customField][eq]=val", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.List(c); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(svc.listFilters) != 1 {
		t.Fatalf("listFilters len = %d, want 1", len(svc.listFilters))
	}
	if svc.listFilters[0].Field != "customField" {
		t.Errorf("field = %q, want %q (unmapped fields should pass through)", svc.listFilters[0].Field, "customField")
	}
}

func makePersonWithStatus(t *testing.T, id, status string) *entities.Resource {
	t.Helper()
	e := &entities.Resource{}
	data := `{"@graph":[{"@id":"` + id + `","@type":"Person","givenName":"Jane","status":"` + status + `"}]}`
	if err := e.Restore(id, "person", "active", json.RawMessage(data), "", "", time.Unix(0, 0), 1); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	return e
}

func TestPersonHandler_Update_OmitStatusPreservesExisting(t *testing.T) {
	t.Parallel()
	existing := makePersonWithStatus(t, "urn:person:1", "active")
	updated := makePersonWithStatus(t, "urn:person:1", "active")
	svc := &stubPersonSvc{
		getByIDEntity: existing,
		updateEntity:  updated,
	}
	h := handlers.NewPersonHandler(svc)

	e := echo.New()
	body := `{"given_name":"Jane","family_name":"Doe","email":"j@example.com","avatar_url":""}`
	req := httptest.NewRequest(http.MethodPut, "/api/persons/urn:person:1",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("urn:person:1")

	if err := h.Update(c); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}

	var sent map[string]any
	if err := json.Unmarshal(svc.updateData, &sent); err != nil {
		t.Fatalf("unmarshal updateData: %v", err)
	}
	if sent["status"] != "active" {
		t.Errorf("status = %v, want 'active' (should be carried forward from existing)", sent["status"])
	}
}

func TestPersonHandler_Update_OmitStatusFallsBackToEntityColumn(t *testing.T) {
	t.Parallel()
	// Simulate a legacy record: status lives in the entity column, but the
	// JSON data payload does NOT include a status field.
	existing := &entities.Resource{}
	legacyData := `{"@graph":[{"@id":"urn:person:1","@type":"Person","givenName":"Jane"}]}`
	if err := existing.Restore(
		"urn:person:1", "person", "active",
		json.RawMessage(legacyData), "", "", time.Unix(0, 0), 1,
	); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	updated := makePersonWithStatus(t, "urn:person:1", "active")
	svc := &stubPersonSvc{
		getByIDEntity: existing,
		updateEntity:  updated,
	}
	h := handlers.NewPersonHandler(svc)

	e := echo.New()
	body := `{"given_name":"Jane","family_name":"Doe","email":"j@example.com","avatar_url":""}`
	req := httptest.NewRequest(http.MethodPut, "/api/persons/urn:person:1",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("urn:person:1")

	if err := h.Update(c); err != nil {
		t.Fatalf("Update: %v", err)
	}

	var sent map[string]any
	if err := json.Unmarshal(svc.updateData, &sent); err != nil {
		t.Fatalf("unmarshal updateData: %v", err)
	}
	if sent["status"] != "active" {
		t.Errorf("status = %v, want 'active' (should fall back to entity column for legacy records)", sent["status"])
	}
}

func TestPersonHandler_Update_ProvidedStatusUpdates(t *testing.T) {
	t.Parallel()
	updated := makePersonWithStatus(t, "urn:person:1", "inactive")
	svc := &stubPersonSvc{
		updateEntity: updated,
	}
	h := handlers.NewPersonHandler(svc)

	e := echo.New()
	body := `{"given_name":"Jane","family_name":"Doe","email":"j@example.com","avatar_url":"","status":"inactive"}`
	req := httptest.NewRequest(http.MethodPut, "/api/persons/urn:person:1",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("urn:person:1")

	if err := h.Update(c); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}

	var sent map[string]any
	if err := json.Unmarshal(svc.updateData, &sent); err != nil {
		t.Fatalf("unmarshal updateData: %v", err)
	}
	if sent["status"] != "inactive" {
		t.Errorf("status = %v, want 'inactive'", sent["status"])
	}
}
