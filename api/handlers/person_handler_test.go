package handlers_test

import (
	"context"
	"encoding/json"
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

// stubPersonSvc captures calls to ListWithFilters so tests can assert
// that the handler translates filter field names correctly.
type stubPersonSvc struct {
	application.ResourceService

	listFilters []repositories.FilterCondition
	listResult  repositories.PaginatedResponse[*entities.Resource]
	listErr     error
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
