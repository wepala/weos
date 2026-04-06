package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"weos/api/handlers"
	apimw "weos/api/middleware"
	"weos/domain/entities"

	"github.com/labstack/echo/v4"
)

// callWithMessages creates an Echo context with the messages middleware active,
// adds the given messages, then calls the handler and returns the response.
func callWithMessages(
	t *testing.T, msgs []entities.Message, handler echo.HandlerFunc,
) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	// Use the messages middleware to inject the accumulator.
	mw := apimw.Messages()
	wrapped := mw(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Add messages to context after middleware runs.
	err := mw(func(c echo.Context) error {
		ctx := c.Request().Context()
		for _, m := range msgs {
			entities.AddMessage(ctx, m)
		}
		return handler(c)
	})(c)
	_ = wrapped // satisfy linter
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return rec
}

func TestEnvelope_SuccessWithoutMessages(t *testing.T) {
	t.Parallel()
	e := echo.New()
	e.Use(apimw.Messages())

	e.GET("/test", func(c echo.Context) error {
		return c.JSON(http.StatusOK, handlers.Envelope{
			Data:     map[string]string{"name": "widget"},
			Messages: entities.GetMessages(c.Request().Context()),
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Data should be present.
	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %v", result)
	}
	if data["name"] != "widget" {
		t.Fatalf("expected name=widget, got %v", data["name"])
	}

	// Messages should be omitted (not null).
	if _, exists := result["messages"]; exists {
		t.Fatalf("expected messages to be omitted when empty, got %v", result["messages"])
	}
}

func TestEnvelope_SuccessWithMessages(t *testing.T) {
	t.Parallel()
	e := echo.New()
	e.Use(apimw.Messages())

	e.GET("/test", func(c echo.Context) error {
		ctx := c.Request().Context()
		entities.AddMessage(ctx, entities.Message{Type: "info", Text: "hello"})
		return c.JSON(http.StatusOK, handlers.Envelope{
			Data:     map[string]string{"id": "123"},
			Messages: entities.GetMessages(ctx),
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var result map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	msgs, ok := result["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %v", result["messages"])
	}
	msg := msgs[0].(map[string]any)
	if msg["type"] != "info" || msg["text"] != "hello" {
		t.Fatalf("unexpected message: %v", msg)
	}
}

func TestErrorEnvelope_Format(t *testing.T) {
	t.Parallel()
	e := echo.New()
	e.Use(apimw.Messages())

	e.GET("/test", func(c echo.Context) error {
		return c.JSON(http.StatusBadRequest, handlers.ErrorEnvelope{
			Error:    "invalid input",
			Messages: []entities.Message{{Type: "error", Text: "invalid input"}},
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Should have both "error" string and "messages" array.
	if result["error"] != "invalid input" {
		t.Fatalf("expected error='invalid input', got %v", result["error"])
	}
	msgs, ok := result["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected 1 message in error envelope, got %v", result["messages"])
	}
}

func TestPaginatedEnvelope_Format(t *testing.T) {
	t.Parallel()
	e := echo.New()
	e.Use(apimw.Messages())

	e.GET("/test", func(c echo.Context) error {
		return c.JSON(http.StatusOK, handlers.PaginatedEnvelope{
			Data:    []string{"a", "b"},
			Cursor:  "abc123",
			HasMore: true,
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var result map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if result["cursor"] != "abc123" {
		t.Fatalf("expected cursor=abc123, got %v", result["cursor"])
	}
	if result["has_more"] != true {
		t.Fatalf("expected has_more=true, got %v", result["has_more"])
	}
	data, ok := result["data"].([]any)
	if !ok || len(data) != 2 {
		t.Fatalf("expected 2 items in data, got %v", result["data"])
	}
	// Messages omitted when empty.
	if _, exists := result["messages"]; exists {
		t.Fatalf("expected messages omitted, got %v", result["messages"])
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
