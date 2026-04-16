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
	"net/http"
	"strings"
	"testing"
)

func okHandler(BehaviorServices) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}
}

func TestPresetRegistry_AddRejectsHandlerWithEmptyPath(t *testing.T) {
	t.Parallel()
	r := NewPresetRegistry()
	err := r.Add(PresetDefinition{
		Name: "p",
		Handlers: []PresetHTTPHandler{
			{Method: http.MethodGet, Path: "", Factory: okHandler},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "path is empty") {
		t.Fatalf("expected empty-path error, got %v", err)
	}
}

func TestPresetRegistry_AddRejectsHandlerWithoutLeadingSlash(t *testing.T) {
	t.Parallel()
	r := NewPresetRegistry()
	err := r.Add(PresetDefinition{
		Name: "p",
		Handlers: []PresetHTTPHandler{
			{Method: http.MethodGet, Path: "leads/upload", Factory: okHandler},
		},
	})
	if err == nil || !strings.Contains(err.Error(), `must start with "/"`) {
		t.Fatalf("expected leading-slash error, got %v", err)
	}
}

func TestPresetRegistry_AddRejectsHandlerPathWithAPIPrefix(t *testing.T) {
	t.Parallel()
	// Preset paths are documented as relative to the /api group. A preset
	// that prefixes its path with "/api/" would silently mount at
	// "/api/api/..." — reject it up front so the failure is loud.
	cases := []string{"/api", "/api/", "/api/leads", "/api/leads/upload"}
	for _, p := range cases {
		p := p
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			r := NewPresetRegistry()
			err := r.Add(PresetDefinition{
				Name: "p",
				Handlers: []PresetHTTPHandler{
					{Method: http.MethodGet, Path: p, Factory: okHandler},
				},
			})
			if err == nil || !strings.Contains(err.Error(), `must be relative to "/api"`) {
				t.Fatalf("expected /api-prefix error for %q, got %v", p, err)
			}
		})
	}
}

func TestPresetRegistry_AddRejectsHandlerWithUnknownMethod(t *testing.T) {
	t.Parallel()
	r := NewPresetRegistry()
	err := r.Add(PresetDefinition{
		Name: "p",
		Handlers: []PresetHTTPHandler{
			{Method: "GIT", Path: "/x", Factory: okHandler},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "not a known HTTP verb") {
		t.Fatalf("expected unknown-method error, got %v", err)
	}
}

func TestPresetRegistry_AddRejectsLowercaseMethod(t *testing.T) {
	t.Parallel()
	r := NewPresetRegistry()
	err := r.Add(PresetDefinition{
		Name: "p",
		Handlers: []PresetHTTPHandler{
			{Method: "get", Path: "/x", Factory: okHandler},
		},
	})
	if err == nil {
		t.Fatal("expected lowercase method to be rejected (allowlist is uppercase only)")
	}
}

func TestPresetRegistry_AddRejectsNilHandlerFactory(t *testing.T) {
	t.Parallel()
	r := NewPresetRegistry()
	err := r.Add(PresetDefinition{
		Name: "p",
		Handlers: []PresetHTTPHandler{
			{Method: http.MethodGet, Path: "/x", Factory: nil},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "factory is nil") {
		t.Fatalf("expected nil-factory error, got %v", err)
	}
}

func TestPresetRegistry_AddRejectsIntraPresetDuplicateRoute(t *testing.T) {
	t.Parallel()
	r := NewPresetRegistry()
	err := r.Add(PresetDefinition{
		Name: "p",
		Handlers: []PresetHTTPHandler{
			{Method: http.MethodGet, Path: "/x", Factory: okHandler},
			{Method: http.MethodGet, Path: "/x", Factory: okHandler},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("expected duplicate-route error, got %v", err)
	}
}

func TestPresetRegistry_AddAcceptsAllStandardMethods(t *testing.T) {
	t.Parallel()
	for _, m := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodOptions, http.MethodHead,
	} {
		r := NewPresetRegistry()
		err := r.Add(PresetDefinition{
			Name:     "p-" + m,
			Handlers: []PresetHTTPHandler{{Method: m, Path: "/x", Factory: okHandler}},
		})
		if err != nil {
			t.Errorf("method %q should be accepted, got %v", m, err)
		}
	}
}

func TestPresetRegistry_HandlersCloneIsolatesCallerMutations(t *testing.T) {
	t.Parallel()
	r := NewPresetRegistry()
	r.MustAdd(PresetDefinition{
		Name: "p",
		Handlers: []PresetHTTPHandler{
			{Method: http.MethodGet, Path: "/x", Factory: okHandler, Protected: true},
		},
	})
	got, ok := r.Get("p")
	if !ok {
		t.Fatal("expected to find preset 'p'")
	}
	got.Handlers[0].Path = "/mutated"
	got.Handlers = append(got.Handlers, PresetHTTPHandler{
		Method: http.MethodPost, Path: "/extra", Factory: okHandler,
	})

	again, _ := r.Get("p")
	if len(again.Handlers) != 1 {
		t.Fatalf("clone mutation leaked: registry has %d handlers, want 1", len(again.Handlers))
	}
	if again.Handlers[0].Path != "/x" {
		t.Fatalf("clone mutation leaked: path is %q, want %q", again.Handlers[0].Path, "/x")
	}
}

func TestPresetRegistry_HandlersAggregatorReturnsAllRoutes(t *testing.T) {
	t.Parallel()
	r := NewPresetRegistry()
	r.MustAdd(PresetDefinition{
		Name: "alpha",
		Handlers: []PresetHTTPHandler{
			{Method: http.MethodGet, Path: "/a", Factory: okHandler, Protected: false},
			{Method: http.MethodPost, Path: "/a", Factory: okHandler, Protected: true},
		},
	})
	r.MustAdd(PresetDefinition{
		Name: "beta",
		Handlers: []PresetHTTPHandler{
			{Method: http.MethodGet, Path: "/b", Factory: okHandler, Protected: true},
		},
	})

	mounted, err := r.Handlers(BehaviorServices{})
	if err != nil {
		t.Fatalf("Handlers() returned error: %v", err)
	}
	if len(mounted) != 3 {
		t.Fatalf("expected 3 mounted handlers, got %d", len(mounted))
	}
	// Alphabetical by source: alpha first (in declared order), then beta.
	want := []struct{ src, method, path string }{
		{"alpha", http.MethodGet, "/a"},
		{"alpha", http.MethodPost, "/a"},
		{"beta", http.MethodGet, "/b"},
	}
	for i, w := range want {
		if mounted[i].Source != w.src ||
			mounted[i].Method != w.method ||
			mounted[i].Path != w.path {
			t.Errorf("mounted[%d] = {%s %s %s}, want {%s %s %s}",
				i, mounted[i].Source, mounted[i].Method, mounted[i].Path,
				w.src, w.method, w.path)
		}
		if mounted[i].Handler == nil {
			t.Errorf("mounted[%d].Handler is nil", i)
		}
	}
}

func TestPresetRegistry_HandlersDetectsCrossPresetCollision(t *testing.T) {
	t.Parallel()
	r := NewPresetRegistry()
	r.MustAdd(PresetDefinition{
		Name:     "first",
		Handlers: []PresetHTTPHandler{{Method: http.MethodPost, Path: "/x", Factory: okHandler}},
	})
	r.MustAdd(PresetDefinition{
		Name:     "second",
		Handlers: []PresetHTTPHandler{{Method: http.MethodPost, Path: "/x", Factory: okHandler}},
	})

	_, err := r.Handlers(BehaviorServices{})
	if err == nil {
		t.Fatal("expected cross-preset collision error")
	}
	if !strings.Contains(err.Error(), "POST /x") {
		t.Errorf("error should name the colliding route, got %v", err)
	}
	if !strings.Contains(err.Error(), "first") || !strings.Contains(err.Error(), "second") {
		t.Errorf("error should name both colliding presets, got %v", err)
	}
}

func TestPresetRegistry_HandlersAllowsSamePathDifferentMethodsAcrossPresets(t *testing.T) {
	t.Parallel()
	r := NewPresetRegistry()
	r.MustAdd(PresetDefinition{
		Name:     "alpha",
		Handlers: []PresetHTTPHandler{{Method: http.MethodGet, Path: "/x", Factory: okHandler}},
	})
	r.MustAdd(PresetDefinition{
		Name:     "beta",
		Handlers: []PresetHTTPHandler{{Method: http.MethodPost, Path: "/x", Factory: okHandler}},
	})
	mounted, err := r.Handlers(BehaviorServices{})
	if err != nil {
		t.Fatalf("same-path different-method should not collide: %v", err)
	}
	if len(mounted) != 2 {
		t.Fatalf("expected 2 mounted handlers, got %d", len(mounted))
	}
}

func TestPresetRegistry_HandlersRejectsNilReturningFactory(t *testing.T) {
	t.Parallel()
	r := NewPresetRegistry()
	r.MustAdd(PresetDefinition{
		Name: "p",
		Handlers: []PresetHTTPHandler{{
			Method: http.MethodGet, Path: "/x",
			Factory: func(BehaviorServices) http.HandlerFunc { return nil },
		}},
	})
	_, err := r.Handlers(BehaviorServices{})
	if err == nil || !strings.Contains(err.Error(), "returned nil") {
		t.Fatalf("expected nil-return error, got %v", err)
	}
}

func TestPresetRegistry_HandlersFactoryInvokedOncePerHandler(t *testing.T) {
	t.Parallel()
	calls := 0
	r := NewPresetRegistry()
	r.MustAdd(PresetDefinition{
		Name: "p",
		Handlers: []PresetHTTPHandler{
			{
				Method: http.MethodGet, Path: "/x",
				Factory: func(BehaviorServices) http.HandlerFunc {
					calls++
					return func(http.ResponseWriter, *http.Request) {}
				},
			},
			{
				Method: http.MethodPost, Path: "/y",
				Factory: func(BehaviorServices) http.HandlerFunc {
					calls++
					return func(http.ResponseWriter, *http.Request) {}
				},
			},
		},
	})
	if _, err := r.Handlers(BehaviorServices{}); err != nil {
		t.Fatalf("Handlers() returned error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected each factory invoked once (2 total), got %d", calls)
	}
}

func TestPresetRegistry_HandlersInjectsServices(t *testing.T) {
	t.Parallel()
	r := NewPresetRegistry()
	var got BehaviorServices
	r.MustAdd(PresetDefinition{
		Name: "p",
		Handlers: []PresetHTTPHandler{{
			Method: http.MethodGet, Path: "/x",
			Factory: func(s BehaviorServices) http.HandlerFunc {
				got = s
				return func(http.ResponseWriter, *http.Request) {}
			},
		}},
	})
	want := BehaviorServices{
		Resources:     &stubResourceRepo{},
		Triples:       &stubTripleRepo{},
		ResourceTypes: &stubTypeRepo{},
		Logger:        noopLogger{},
		Writer:        newLazyResourceWriter(),
	}
	if _, err := r.Handlers(want); err != nil {
		t.Fatalf("Handlers() returned error: %v", err)
	}
	if got.Resources != want.Resources {
		t.Errorf("Resources not propagated")
	}
	if got.Triples != want.Triples {
		t.Errorf("Triples not propagated")
	}
	if got.ResourceTypes != want.ResourceTypes {
		t.Errorf("ResourceTypes not propagated")
	}
	if got.Logger != want.Logger {
		t.Errorf("Logger not propagated")
	}
	if got.Writer != want.Writer {
		t.Errorf("Writer not propagated")
	}
}

func TestPresetRegistry_HandlersReturnsEmptyForRegistryWithNoHandlers(t *testing.T) {
	t.Parallel()
	r := NewPresetRegistry()
	r.MustAdd(PresetDefinition{Name: "p"})
	mounted, err := r.Handlers(BehaviorServices{})
	if err != nil {
		t.Fatalf("Handlers() returned error: %v", err)
	}
	if len(mounted) != 0 {
		t.Fatalf("expected 0 mounted handlers, got %d", len(mounted))
	}
}
