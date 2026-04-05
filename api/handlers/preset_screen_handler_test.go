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
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"weos/api/handlers"
	"weos/application"

	"github.com/labstack/echo/v4"
)

func screenRegistry() *application.PresetRegistry {
	r := application.NewPresetRegistry()
	r.MustAdd(application.PresetDefinition{
		Name: "tasks",
		Types: []application.PresetResourceType{
			{Name: "Task", Slug: "task"},
		},
		Screens: fstest.MapFS{
			"task/Checklist.mjs": {Data: []byte(`export const meta={name:"Checklist",label:"Checklist"};export default {}`)},
		},
	})
	r.MustAdd(application.PresetDefinition{
		Name: "core",
		Types: []application.PresetResourceType{
			{Name: "Person", Slug: "person"},
		},
		// No Screens
	})
	return r
}

func TestPresetScreenHandler_Serve_Success(t *testing.T) {
	t.Parallel()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/resource-types/presets/tasks/screens/task/Checklist.mjs", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("name", "*")
	c.SetParamValues("tasks", "/task/Checklist.mjs")

	h := handlers.NewPresetScreenHandler(screenRegistry())
	err := h.Serve(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/javascript; charset=utf-8" {
		t.Fatalf("expected application/javascript content type, got %q", ct)
	}
	cc := rec.Header().Get("Cache-Control")
	if cc != "private, max-age=3600" {
		t.Fatalf("expected private cache control, got %q", cc)
	}
	body := rec.Body.String()
	expected := `export const meta={name:"Checklist",label:"Checklist"};export default {}`
	if body != expected {
		t.Fatalf("expected body %q, got %q", expected, body)
	}
}

func TestPresetScreenHandler_Serve_PresetNotFound(t *testing.T) {
	t.Parallel()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/resource-types/presets/nonexistent/screens/foo.mjs", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("name", "*")
	c.SetParamValues("nonexistent", "/foo.mjs")

	h := handlers.NewPresetScreenHandler(screenRegistry())
	err := h.Serve(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestPresetScreenHandler_Serve_NoScreens(t *testing.T) {
	t.Parallel()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/resource-types/presets/core/screens/person/List.mjs", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("name", "*")
	c.SetParamValues("core", "/person/List.mjs")

	h := handlers.NewPresetScreenHandler(screenRegistry())
	err := h.Serve(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestPresetScreenHandler_Serve_FileNotFound(t *testing.T) {
	t.Parallel()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/resource-types/presets/tasks/screens/task/Missing.mjs", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("name", "*")
	c.SetParamValues("tasks", "/task/Missing.mjs")

	h := handlers.NewPresetScreenHandler(screenRegistry())
	err := h.Serve(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestPresetScreenHandler_Serve_PathTraversal(t *testing.T) {
	t.Parallel()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/resource-types/presets/tasks/screens/../../etc/passwd", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("name", "*")
	c.SetParamValues("tasks", "/../../etc/passwd")

	h := handlers.NewPresetScreenHandler(screenRegistry())
	err := h.Serve(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Path traversal rejected: either by .mjs suffix check (400) or fs.FS contract (404).
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 404 or 400 for path traversal, got %d", rec.Code)
	}
}

func TestPresetScreenHandler_Serve_NonMjsRejected(t *testing.T) {
	t.Parallel()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/resource-types/presets/tasks/screens/task/config.json", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("name", "*")
	c.SetParamValues("tasks", "/task/config.json")

	h := handlers.NewPresetScreenHandler(screenRegistry())
	err := h.Serve(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-.mjs file, got %d", rec.Code)
	}
}

func TestPresetScreenHandler_Serve_NestedPathRejected(t *testing.T) {
	t.Parallel()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/resource-types/presets/tasks/screens/task/sub/deep.mjs", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("name", "*")
	c.SetParamValues("tasks", "/task/sub/deep.mjs")

	h := handlers.NewPresetScreenHandler(screenRegistry())
	err := h.Serve(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for nested path, got %d", rec.Code)
	}
}

func TestPresetScreenHandler_Serve_EmptyPath(t *testing.T) {
	t.Parallel()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/resource-types/presets/tasks/screens/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("name", "*")
	c.SetParamValues("tasks", "")

	h := handlers.NewPresetScreenHandler(screenRegistry())
	err := h.Serve(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
