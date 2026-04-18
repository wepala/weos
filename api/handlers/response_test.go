package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wepala/weos/v3/api/handlers"
	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/domain/entities"

	"github.com/labstack/echo/v4"
)

// newTestContext creates an Echo context and injects the messages accumulator
// directly into the request context for handler tests.
func newTestContext(t *testing.T) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// Inject message accumulator directly into the request context.
	ctx := entities.ContextWithMessages(c.Request().Context())
	c.SetRequest(c.Request().WithContext(ctx))
	return c, rec
}

func parseJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v\nbody: %s", err, rec.Body.String())
	}
	return result
}

func TestRespond_WrapsDataInEnvelope(t *testing.T) {
	t.Parallel()
	c, rec := newTestContext(t)

	err := handlers.ExportRespond(c, http.StatusCreated, map[string]string{"id": "abc"})
	if err != nil {
		t.Fatalf("respond error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}

	result := parseJSON(t, rec)
	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %v", result)
	}
	if data["id"] != "abc" {
		t.Fatalf("expected id=abc, got %v", data["id"])
	}
	if _, exists := result["messages"]; exists {
		t.Fatal("expected messages omitted when empty")
	}
}

func TestRespond_IncludesContextMessages(t *testing.T) {
	t.Parallel()
	c, rec := newTestContext(t)

	entities.AddMessage(c.Request().Context(), entities.Message{Type: "warning", Text: "watch out"})

	err := handlers.ExportRespond(c, http.StatusOK, nil)
	if err != nil {
		t.Fatalf("respond error: %v", err)
	}

	result := parseJSON(t, rec)
	msgs, ok := result["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %v", result["messages"])
	}
	msg := msgs[0].(map[string]any)
	if msg["type"] != "warning" || msg["text"] != "watch out" {
		t.Fatalf("unexpected message: %v", msg)
	}
}

func TestRespondRaw_NoDoubleEncoding(t *testing.T) {
	t.Parallel()
	c, rec := newTestContext(t)

	raw := json.RawMessage(`{"@type":"Product","name":"Widget"}`)
	err := handlers.ExportRespondRaw(c, http.StatusOK, raw)
	if err != nil {
		t.Fatalf("respondRaw error: %v", err)
	}

	result := parseJSON(t, rec)
	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data to be an object (not double-encoded string), got %T: %v",
			result["data"], result["data"])
	}
	if data["@type"] != "Product" {
		t.Fatalf("expected @type=Product, got %v", data["@type"])
	}
}

func TestRespondPaginated_IncludesAllFields(t *testing.T) {
	t.Parallel()
	c, rec := newTestContext(t)

	entities.AddMessage(c.Request().Context(), entities.Message{Type: "info", Text: "filtered"})

	err := handlers.ExportRespondPaginated(c, http.StatusOK, []string{"a", "b"}, "cur123", true)
	if err != nil {
		t.Fatalf("respondPaginated error: %v", err)
	}

	result := parseJSON(t, rec)
	if result["cursor"] != "cur123" {
		t.Fatalf("expected cursor=cur123, got %v", result["cursor"])
	}
	if result["has_more"] != true {
		t.Fatalf("expected has_more=true, got %v", result["has_more"])
	}
	data, ok := result["data"].([]any)
	if !ok || len(data) != 2 {
		t.Fatalf("expected 2-item data array, got %v", result["data"])
	}
	msgs, ok := result["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %v", result["messages"])
	}
}

func TestRespondError_BackwardCompatFormat(t *testing.T) {
	t.Parallel()
	c, rec := newTestContext(t)

	err := handlers.ExportRespondError(c, http.StatusBadRequest, "invalid input")
	if err != nil {
		t.Fatalf("respondError error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	result := parseJSON(t, rec)
	if result["error"] != "invalid input" {
		t.Fatalf("expected error='invalid input', got %v", result["error"])
	}
	// Messages should be omitted when no context messages were accumulated.
	if _, exists := result["messages"]; exists {
		t.Fatal("expected messages omitted when no context messages present")
	}
}

func TestRespondError_PreservesContextMessages(t *testing.T) {
	t.Parallel()
	c, rec := newTestContext(t)

	// Service added a warning before the error occurred.
	entities.AddMessage(c.Request().Context(), entities.Message{
		Type: "warning", Text: "schema missing", Field: "schema",
	})

	err := handlers.ExportRespondError(c, http.StatusBadRequest, "validation failed")
	if err != nil {
		t.Fatalf("respondError error: %v", err)
	}

	result := parseJSON(t, rec)
	if result["error"] != "validation failed" {
		t.Fatalf("expected error='validation failed', got %v", result["error"])
	}
	msgs, ok := result["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected 1 context message (not duplicated error), got %v", result["messages"])
	}
	msg := msgs[0].(map[string]any)
	if msg["type"] != "warning" {
		t.Fatalf("expected warning message from context, got %v", msg)
	}
}

func TestRespondForbidden_StatusAndMessage(t *testing.T) {
	t.Parallel()
	c, rec := newTestContext(t)

	err := handlers.ExportRespondForbidden(c)
	if err != nil {
		t.Fatalf("respondForbidden error: %v", err)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}

	result := parseJSON(t, rec)
	if result["error"] != "access denied" {
		t.Fatalf("expected error='access denied', got %v", result["error"])
	}
}

func TestEnvelope_OmitsFieldAndCodeWhenEmpty(t *testing.T) {
	t.Parallel()
	e := echo.New()
	e.Use(apimw.Messages())

	e.GET("/test", func(c echo.Context) error {
		ctx := c.Request().Context()
		entities.AddMessage(ctx, entities.Message{Type: "success", Text: "done"})
		return c.JSON(http.StatusOK, handlers.Envelope{
			Data:     nil,
			Messages: entities.GetMessages(ctx),
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var result map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	var msgs []map[string]any
	if err := json.Unmarshal(result["messages"], &msgs); err != nil {
		t.Fatalf("failed to parse messages: %v", err)
	}
	msg := msgs[0]
	if _, ok := msg["field"]; ok {
		t.Fatal("expected field to be omitted when empty")
	}
	if _, ok := msg["code"]; ok {
		t.Fatal("expected code to be omitted when empty")
	}
}
