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

func TestProvidePresetHTTPHandlers_RejectsNilDeps(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	writer := newLazyResourceWriter()
	cases := []struct {
		name    string
		invoke  func() error
		wantSub string
	}{
		{"nil registry", func() error {
			_, err := ProvidePresetHTTPHandlers(
				nil, &stubResourceRepo{}, &stubTripleRepo{}, &stubTypeRepo{}, noopLogger{}, writer,
			)
			return err
		}, "PresetRegistry"},
		{"nil resources", func() error {
			_, err := ProvidePresetHTTPHandlers(
				registry, nil, &stubTripleRepo{}, &stubTypeRepo{}, noopLogger{}, writer,
			)
			return err
		}, "Resources"},
		{"nil triples", func() error {
			_, err := ProvidePresetHTTPHandlers(
				registry, &stubResourceRepo{}, nil, &stubTypeRepo{}, noopLogger{}, writer,
			)
			return err
		}, "Triples"},
		{"nil resourceTypes", func() error {
			_, err := ProvidePresetHTTPHandlers(
				registry, &stubResourceRepo{}, &stubTripleRepo{}, nil, noopLogger{}, writer,
			)
			return err
		}, "ResourceTypes"},
		{"nil logger", func() error {
			_, err := ProvidePresetHTTPHandlers(
				registry, &stubResourceRepo{}, &stubTripleRepo{}, &stubTypeRepo{}, nil, writer,
			)
			return err
		}, "Logger"},
		{"nil writer", func() error {
			_, err := ProvidePresetHTTPHandlers(
				registry, &stubResourceRepo{}, &stubTripleRepo{}, &stubTypeRepo{}, noopLogger{}, nil,
			)
			return err
		}, "lazyResourceWriter"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.invoke()
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not name dependency %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestProvidePresetHTTPHandlers_RejectsTypedNilDeps(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	writer := newLazyResourceWriter()
	var typedNilResources *stubResourceRepo
	var typedNilTriples *stubTripleRepo
	var typedNilTypes *stubTypeRepo
	if _, err := ProvidePresetHTTPHandlers(
		registry, typedNilResources, &stubTripleRepo{}, &stubTypeRepo{}, noopLogger{}, writer,
	); err == nil {
		t.Error("expected error for typed-nil Resources")
	}
	if _, err := ProvidePresetHTTPHandlers(
		registry, &stubResourceRepo{}, typedNilTriples, &stubTypeRepo{}, noopLogger{}, writer,
	); err == nil {
		t.Error("expected error for typed-nil Triples")
	}
	if _, err := ProvidePresetHTTPHandlers(
		registry, &stubResourceRepo{}, &stubTripleRepo{}, typedNilTypes, noopLogger{}, writer,
	); err == nil {
		t.Error("expected error for typed-nil ResourceTypes")
	}
}

func TestProvidePresetHTTPHandlers_PropagatesRegistryError(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	registry.MustAdd(PresetDefinition{
		Name:     "first",
		Handlers: []PresetHTTPHandler{{Method: http.MethodPost, Path: "/x", Factory: okHandler}},
	})
	registry.MustAdd(PresetDefinition{
		Name:     "second",
		Handlers: []PresetHTTPHandler{{Method: http.MethodPost, Path: "/x", Factory: okHandler}},
	})
	_, err := ProvidePresetHTTPHandlers(
		registry, &stubResourceRepo{}, &stubTripleRepo{}, &stubTypeRepo{},
		noopLogger{}, newLazyResourceWriter(),
	)
	if err == nil || !strings.Contains(err.Error(), "POST /x") {
		t.Fatalf("expected collision error from registry, got %v", err)
	}
}

func TestProvidePresetHTTPHandlers_BuildsBehaviorServicesCorrectly(t *testing.T) {
	t.Parallel()
	resources := &stubResourceRepo{}
	triples := &stubTripleRepo{}
	types := &stubTypeRepo{}
	logger := noopLogger{}
	writer := newLazyResourceWriter()

	registry := NewPresetRegistry()
	var seen BehaviorServices
	registry.MustAdd(PresetDefinition{
		Name: "p",
		Handlers: []PresetHTTPHandler{{
			Method: http.MethodGet, Path: "/x",
			Factory: func(s BehaviorServices) http.HandlerFunc {
				seen = s
				return func(http.ResponseWriter, *http.Request) {}
			},
		}},
	})

	if _, err := ProvidePresetHTTPHandlers(registry, resources, triples, types, logger, writer); err != nil {
		t.Fatalf("ProvidePresetHTTPHandlers: %v", err)
	}
	// Each field must come from the matching argument — guards against future
	// refactors that drop or substitute values inside the BehaviorServices literal.
	if seen.Resources != resources {
		t.Errorf("Resources misrouted")
	}
	if seen.Triples != triples {
		t.Errorf("Triples misrouted")
	}
	if seen.ResourceTypes != types {
		t.Errorf("ResourceTypes misrouted")
	}
	if seen.Logger != logger {
		t.Errorf("Logger misrouted")
	}
	if seen.Writer != writer {
		t.Errorf("Writer misrouted")
	}
}

func TestProvidePresetHTTPHandlers_EmptyRegistryReturnsEmpty(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	mounted, err := ProvidePresetHTTPHandlers(
		registry, &stubResourceRepo{}, &stubTripleRepo{}, &stubTypeRepo{},
		noopLogger{}, newLazyResourceWriter(),
	)
	if err != nil {
		t.Fatalf("empty registry should not error: %v", err)
	}
	if len(mounted) != 0 {
		t.Errorf("expected empty mounted list, got %d", len(mounted))
	}
}
