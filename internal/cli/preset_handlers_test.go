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

package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"weos/application"

	"github.com/labstack/echo/v4"
)

type silentLogger struct{}

func (silentLogger) Info(context.Context, string, ...any)  {}
func (silentLogger) Warn(context.Context, string, ...any)  {}
func (silentLogger) Error(context.Context, string, ...any) {}

// fakeAuth stands in for the protected group's auth middleware. It only
// inspects the Authorization header (sessions are out of scope here); the
// goal is to prove that protected preset handlers run behind some 401 gate.
func fakeAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if c.Request().Header.Get("Authorization") == "" {
			return c.NoContent(http.StatusUnauthorized)
		}
		return next(c)
	}
}

// buildTestServer wires a minimal Echo router that mirrors the structure of
// serve.go: an /api group plus a /api `protected` subgroup with auth applied.
// Then it mounts the supplied preset handlers via the same helper serve.go uses.
func buildTestServer(t *testing.T, registry *application.PresetRegistry) *echo.Echo {
	t.Helper()
	mounted, err := registry.Handlers(application.BehaviorServices{})
	if err != nil {
		t.Fatalf("registry.Handlers: %v", err)
	}
	e := echo.New()
	api := e.Group("/api")
	protected := api.Group("")
	protected.Use(fakeAuth)
	mountPresetHandlers(api, protected, mounted, silentLogger{})
	return e
}

func TestServe_MountsPublicPresetHandlerWithoutAuth(t *testing.T) {
	t.Parallel()
	registry := application.NewPresetRegistry()
	registry.MustAdd(application.PresetDefinition{
		Name: "p",
		Handlers: []application.PresetHTTPHandler{{
			Method: http.MethodGet, Path: "/test/ping", Protected: false,
			Factory: func(application.BehaviorServices) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("pong"))
				}
			},
		}},
	})
	srv := buildTestServer(t, registry)

	req := httptest.NewRequest(http.MethodGet, "/api/test/ping", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("public handler: got %d, want 200", rec.Code)
	}
	if rec.Body.String() != "pong" {
		t.Errorf("public handler body: got %q, want %q", rec.Body.String(), "pong")
	}
}

func TestServe_ProtectedPresetHandlerRejectsUnauthenticated(t *testing.T) {
	t.Parallel()
	registry := application.NewPresetRegistry()
	registry.MustAdd(application.PresetDefinition{
		Name: "p",
		Handlers: []application.PresetHTTPHandler{{
			Method: http.MethodPost, Path: "/test/secure", Protected: true,
			Factory: func(application.BehaviorServices) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusCreated)
				}
			},
		}},
	})
	srv := buildTestServer(t, registry)

	req := httptest.NewRequest(http.MethodPost, "/api/test/secure", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("protected handler without auth: got %d, want 401", rec.Code)
	}
}

func TestServe_ProtectedPresetHandlerRunsWhenAuthenticated(t *testing.T) {
	t.Parallel()
	registry := application.NewPresetRegistry()
	registry.MustAdd(application.PresetDefinition{
		Name: "p",
		Handlers: []application.PresetHTTPHandler{{
			Method: http.MethodPost, Path: "/test/secure", Protected: true,
			Factory: func(application.BehaviorServices) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusCreated)
				}
			},
		}},
	})
	srv := buildTestServer(t, registry)

	req := httptest.NewRequest(http.MethodPost, "/api/test/secure", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("protected handler with auth: got %d, want 201", rec.Code)
	}
}

func TestServe_MountsHandlersFromMultiplePresets(t *testing.T) {
	t.Parallel()
	registry := application.NewPresetRegistry()
	// Public handler from one preset; protected handler from another. This
	// exercises both group-selection branches AND multi-preset aggregation in
	// a single test, so a regression that only mounts the first preset (or
	// only one group) shows up here.
	registry.MustAdd(application.PresetDefinition{
		Name: "alpha",
		Handlers: []application.PresetHTTPHandler{{
			Method: http.MethodGet, Path: "/alpha", Protected: false,
			Factory: func(application.BehaviorServices) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte("a"))
				}
			},
		}},
	})
	registry.MustAdd(application.PresetDefinition{
		Name: "beta",
		Handlers: []application.PresetHTTPHandler{{
			Method: http.MethodGet, Path: "/beta", Protected: true,
			Factory: func(application.BehaviorServices) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte("b"))
				}
			},
		}},
	})
	srv := buildTestServer(t, registry)

	tests := []struct {
		name, path, body, authHeader string
		wantCode                     int
	}{
		{"public alpha no auth", "/api/alpha", "a", "", http.StatusOK},
		{"protected beta no auth", "/api/beta", "", "", http.StatusUnauthorized},
		{"protected beta with auth", "/api/beta", "b", "Bearer t", http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Errorf("%s: got %d, want %d", tc.path, rec.Code, tc.wantCode)
			}
			if tc.body != "" && rec.Body.String() != tc.body {
				t.Errorf("%s body: got %q, want %q", tc.path, rec.Body.String(), tc.body)
			}
		})
	}
}

// TestServe_PresetHandlerNotShadowedByDynamicResourceRoute locks in the
// architectural invariant called out in serve.go: preset handlers must mount
// before the dynamic /:typeSlug catch-all. If a future change reorders
// registration, the catch-all silently swallows preset POSTs.
func TestServe_PresetHandlerNotShadowedByDynamicResourceRoute(t *testing.T) {
	t.Parallel()
	registry := application.NewPresetRegistry()
	registry.MustAdd(application.PresetDefinition{
		Name: "p",
		Handlers: []application.PresetHTTPHandler{{
			Method: http.MethodPost, Path: "/leads", Protected: true,
			Factory: func(application.BehaviorServices) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte("preset"))
				}
			},
		}},
	})
	mounted, err := registry.Handlers(application.BehaviorServices{})
	if err != nil {
		t.Fatalf("registry.Handlers: %v", err)
	}
	e := echo.New()
	api := e.Group("/api")
	protected := api.Group("")
	protected.Use(fakeAuth)
	// Mount the preset routes FIRST, then the catch-all — same order as serve.go.
	mountPresetHandlers(api, protected, mounted, silentLogger{})
	protected.POST("/:typeSlug", func(c echo.Context) error {
		return c.String(http.StatusOK, "catchall:"+c.Param("typeSlug"))
	})

	req := httptest.NewRequest(http.MethodPost, "/api/leads", nil)
	req.Header.Set("Authorization", "Bearer t")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated || rec.Body.String() != "preset" {
		t.Fatalf("preset handler was shadowed by catch-all: got status=%d body=%q, want 201 \"preset\"",
			rec.Code, rec.Body.String())
	}
}
