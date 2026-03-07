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

package entities

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/akeemphilbert/pericarp/pkg/ddd"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

func TestTemplate_With(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		inputName      string
		inputSlug      string
		inputThemeSlug string
		wantErr        bool
		errSubstr      string
		validate       func(*testing.T, *Template)
	}{
		{
			name:           "happy path - valid template",
			inputName:      "Home Page",
			inputSlug:      "home",
			inputThemeSlug: "test-theme",
			wantErr:        false,
			validate: func(t *testing.T, e *Template) {
				t.Helper()
				if !strings.HasPrefix(e.GetID(), "urn:theme:test-theme:template:") {
					t.Fatalf("got id %q, want prefix %q",
						e.GetID(), "urn:theme:test-theme:template:")
				}
				if !strings.HasSuffix(e.GetID(), ":home") {
					t.Fatalf("got id %q, want suffix %q", e.GetID(), ":home")
				}
				if e.Name() != "Home Page" {
					t.Fatalf("got name %q, want %q", e.Name(), "Home Page")
				}
				if e.Slug() != "home" {
					t.Fatalf("got slug %q, want %q", e.Slug(), "home")
				}
				if e.Status() != "draft" {
					t.Fatalf("got status %q, want %q", e.Status(), "draft")
				}
				if e.CreatedAt().IsZero() {
					t.Fatal("expected non-zero CreatedAt")
				}
			},
		},
		{
			name:           "invalid - empty name",
			inputName:      "",
			inputSlug:      "home",
			inputThemeSlug: "test-theme",
			wantErr:        true,
			errSubstr:      "name cannot be empty",
		},
		{
			name:           "invalid - empty slug",
			inputName:      "Home",
			inputSlug:      "",
			inputThemeSlug: "test-theme",
			wantErr:        true,
			errSubstr:      "slug cannot be empty",
		},
		{
			name:           "invalid - empty themeSlug",
			inputName:      "Home",
			inputSlug:      "home",
			inputThemeSlug: "",
			wantErr:        true,
			errSubstr:      "themeSlug cannot be empty",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entity, err := new(Template).With(
				tt.inputName, tt.inputSlug, tt.inputThemeSlug)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSubstr != "" && !contains(err.Error(), tt.errSubstr) {
					t.Fatalf("error should contain %q, got: %v",
						tt.errSubstr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if entity == nil {
				t.Fatal("expected non-nil entity")
			}
			if tt.validate != nil {
				tt.validate(t, entity)
			}
		})
	}
}

func TestTemplate_ApplyEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	setupTemplate := func(t *testing.T, id string) *Template {
		t.Helper()
		e := &Template{}
		e.BaseEntity = ddd.NewBaseEntity(id)
		return e
	}

	tests := []struct {
		name      string
		setup     func(*testing.T) *Template
		event     domain.EventEnvelope[any]
		wantErr   bool
		errSubstr string
		validate  func(*testing.T, *Template)
	}{
		{
			name: "TemplateCreated restores fields",
			setup: func(t *testing.T) *Template {
				return setupTemplate(t, "urn:theme:t:template:abc:home")
			},
			event: newTestEnvelope(
				TemplateCreated{
					Name:      "Home",
					Slug:      "home",
					Timestamp: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
				},
				"urn:theme:t:template:abc:home", "Template.Created", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *Template) {
				t.Helper()
				if e.Name() != "Home" {
					t.Fatalf("got name %q, want %q", e.Name(), "Home")
				}
				if e.Slug() != "home" {
					t.Fatalf("got slug %q, want %q", e.Slug(), "home")
				}
				if e.Status() != "draft" {
					t.Fatalf("got status %q, want %q", e.Status(), "draft")
				}
			},
		},
		{
			name: "TemplateUpdated updates fields",
			setup: func(t *testing.T) *Template {
				tpl := setupTemplate(t, "urn:theme:t:template:def:about")
				tpl.name = "Old"
				return tpl
			},
			event: newTestEnvelope(
				TemplateUpdated{
					Name: "About", Slug: "about",
					Description: "About page", FilePath: "about.html",
					Status: "active", Timestamp: time.Now(),
				},
				"urn:theme:t:template:def:about", "Template.Updated", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *Template) {
				t.Helper()
				if e.Name() != "About" {
					t.Fatalf("got name %q, want %q", e.Name(), "About")
				}
				if e.FilePath() != "about.html" {
					t.Fatalf("got filePath %q, want %q",
						e.FilePath(), "about.html")
				}
			},
		},
		{
			name: "TemplateDeleted sets archived",
			setup: func(t *testing.T) *Template {
				tpl := setupTemplate(t, "urn:theme:t:template:ghi:del")
				tpl.status = "active"
				return tpl
			},
			event: newTestEnvelope(
				TemplateDeleted{Timestamp: time.Now()},
				"urn:theme:t:template:ghi:del", "Template.Deleted", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *Template) {
				t.Helper()
				if e.Status() != "archived" {
					t.Fatalf("got status %q, want %q", e.Status(), "archived")
				}
			},
		},
		{
			name: "TemplateThemeLinked acknowledged",
			setup: func(t *testing.T) *Template {
				return setupTemplate(t, "urn:theme:t:template:jkl:link")
			},
			event: newTestEnvelope(
				TemplateThemeLinked{
					BasicTripleEvent: domain.BasicTripleEvent{
						Subject:   "urn:theme:t:template:jkl:link",
						Predicate: PredicateBelongsTo,
						Object:    "urn:theme:t",
					},
					Timestamp: time.Now(),
				},
				"urn:theme:t:template:jkl:link", "Template.ThemeLinked", 1,
			),
			wantErr: false,
		},
		{
			name: "unknown event type returns error",
			setup: func(t *testing.T) *Template {
				return setupTemplate(t, "urn:theme:t:template:x:y")
			},
			event:     newTestEnvelope(struct{}{}, "urn:theme:t:template:x:y", "Unknown", 1),
			wantErr:   true,
			errSubstr: "unknown event type",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entity := tt.setup(t)
			err := entity.ApplyEvent(ctx, tt.event)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSubstr != "" && !contains(err.Error(), tt.errSubstr) {
					t.Fatalf("error should contain %q, got: %v",
						tt.errSubstr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.validate != nil {
				tt.validate(t, entity)
			}
		})
	}
}

func TestTemplateCreated_EventType(t *testing.T) {
	t.Parallel()
	if got := (TemplateCreated{}).EventType(); got != "Template.Created" {
		t.Fatalf("EventType() = %q, want %q", got, "Template.Created")
	}
}

func TestTemplateUpdated_EventType(t *testing.T) {
	t.Parallel()
	if got := (TemplateUpdated{}).EventType(); got != "Template.Updated" {
		t.Fatalf("EventType() = %q, want %q", got, "Template.Updated")
	}
}

func TestTemplateDeleted_EventType(t *testing.T) {
	t.Parallel()
	if got := (TemplateDeleted{}).EventType(); got != "Template.Deleted" {
		t.Fatalf("EventType() = %q, want %q", got, "Template.Deleted")
	}
}
