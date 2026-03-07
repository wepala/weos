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
	"testing"
	"time"

	"github.com/akeemphilbert/pericarp/pkg/ddd"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

func TestTheme_With(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		inputName string
		inputSlug string
		wantErr   bool
		errSubstr string
		validate  func(*testing.T, *Theme)
	}{
		{
			name:      "happy path - valid theme",
			inputName: "Default Theme",
			inputSlug: "default-theme",
			wantErr:   false,
			validate: func(t *testing.T, e *Theme) {
				t.Helper()
				if e.GetID() != "urn:theme:default-theme" {
					t.Fatalf("got id %q, want %q", e.GetID(), "urn:theme:default-theme")
				}
				if e.Name() != "Default Theme" {
					t.Fatalf("got name %q, want %q", e.Name(), "Default Theme")
				}
				if e.Slug() != "default-theme" {
					t.Fatalf("got slug %q, want %q", e.Slug(), "default-theme")
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
			name:      "invalid - empty name",
			inputName: "",
			inputSlug: "some-slug",
			wantErr:   true,
			errSubstr: "name cannot be empty",
		},
		{
			name:      "invalid - empty slug",
			inputName: "Theme",
			inputSlug: "",
			wantErr:   true,
			errSubstr: "slug cannot be empty",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entity, err := new(Theme).With(tt.inputName, tt.inputSlug)
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

func TestTheme_ApplyEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	setupTheme := func(t *testing.T, id string) *Theme {
		t.Helper()
		e := &Theme{}
		e.BaseEntity = ddd.NewBaseEntity(id)
		return e
	}

	tests := []struct {
		name      string
		setup     func(*testing.T) *Theme
		event     domain.EventEnvelope[any]
		wantErr   bool
		errSubstr string
		validate  func(*testing.T, *Theme)
	}{
		{
			name: "ThemeCreated restores fields",
			setup: func(t *testing.T) *Theme {
				return setupTheme(t, "urn:theme:test")
			},
			event: newTestEnvelope(
				ThemeCreated{
					Name:      "Test Theme",
					Slug:      "test",
					Timestamp: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
				},
				"urn:theme:test", "Theme.Created", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *Theme) {
				t.Helper()
				if e.Name() != "Test Theme" {
					t.Fatalf("got name %q, want %q", e.Name(), "Test Theme")
				}
				if e.Slug() != "test" {
					t.Fatalf("got slug %q, want %q", e.Slug(), "test")
				}
				if e.Status() != "draft" {
					t.Fatalf("got status %q, want %q", e.Status(), "draft")
				}
			},
		},
		{
			name: "ThemeUpdated updates fields",
			setup: func(t *testing.T) *Theme {
				th := setupTheme(t, "urn:theme:update-test")
				th.name = "Old Name"
				return th
			},
			event: newTestEnvelope(
				ThemeUpdated{
					Name: "New Name", Slug: "update-test",
					Description: "A desc", Version: "2.0",
					ThumbnailURL: "/img.png", Status: "active",
					Timestamp: time.Now(),
				},
				"urn:theme:update-test", "Theme.Updated", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *Theme) {
				t.Helper()
				if e.Name() != "New Name" {
					t.Fatalf("got name %q, want %q", e.Name(), "New Name")
				}
				if e.Version() != "2.0" {
					t.Fatalf("got version %q, want %q", e.Version(), "2.0")
				}
			},
		},
		{
			name: "ThemeDeleted sets archived",
			setup: func(t *testing.T) *Theme {
				th := setupTheme(t, "urn:theme:del-test")
				th.status = "active"
				return th
			},
			event: newTestEnvelope(
				ThemeDeleted{Timestamp: time.Now()},
				"urn:theme:del-test", "Theme.Deleted", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *Theme) {
				t.Helper()
				if e.Status() != "archived" {
					t.Fatalf("got status %q, want %q", e.Status(), "archived")
				}
			},
		},
		{
			name: "ThemeWebsiteLinked acknowledged",
			setup: func(t *testing.T) *Theme {
				return setupTheme(t, "urn:theme:link-test")
			},
			event: newTestEnvelope(
				ThemeWebsiteLinked{
					BasicTripleEvent: domain.BasicTripleEvent{
						Subject:   "urn:theme:link-test",
						Predicate: PredicateAppliedTo,
						Object:    "urn:my-site",
					},
					Timestamp: time.Now(),
				},
				"urn:theme:link-test", "Theme.WebsiteLinked", 1,
			),
			wantErr: false,
		},
		{
			name: "ThemeAuthorLinked acknowledged",
			setup: func(t *testing.T) *Theme {
				return setupTheme(t, "urn:theme:author-test")
			},
			event: newTestEnvelope(
				ThemeAuthorLinked{
					BasicTripleEvent: domain.BasicTripleEvent{
						Subject:   "urn:theme:author-test",
						Predicate: PredicateAuthoredBy,
						Object:    "urn:person:john",
					},
					Timestamp: time.Now(),
				},
				"urn:theme:author-test", "Theme.AuthorLinked", 1,
			),
			wantErr: false,
		},
		{
			name: "unknown event type returns error",
			setup: func(t *testing.T) *Theme {
				return setupTheme(t, "urn:theme:unknown")
			},
			event:     newTestEnvelope(struct{}{}, "urn:theme:unknown", "UnknownEvent", 1),
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

func TestThemeCreated_EventType(t *testing.T) {
	t.Parallel()
	if got := (ThemeCreated{}).EventType(); got != "Theme.Created" {
		t.Fatalf("EventType() = %q, want %q", got, "Theme.Created")
	}
}

func TestThemeUpdated_EventType(t *testing.T) {
	t.Parallel()
	if got := (ThemeUpdated{}).EventType(); got != "Theme.Updated" {
		t.Fatalf("EventType() = %q, want %q", got, "Theme.Updated")
	}
}

func TestThemeDeleted_EventType(t *testing.T) {
	t.Parallel()
	if got := (ThemeDeleted{}).EventType(); got != "Theme.Deleted" {
		t.Fatalf("EventType() = %q, want %q", got, "Theme.Deleted")
	}
}
