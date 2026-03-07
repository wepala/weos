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

func TestSection_With(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		inputName        string
		inputSlot        string
		inputWebsiteSlug string
		inputPageSlug    string
		wantErr          bool
		errSubstr        string
		validate         func(*testing.T, *Section)
	}{
		{
			name:             "happy path - valid section",
			inputName:        "Hero",
			inputSlot:        "hero.headline",
			inputWebsiteSlug: "ak33m",
			inputPageSlug:    "home",
			wantErr:          false,
			validate: func(t *testing.T, e *Section) {
				t.Helper()
				if e.GetID() == "" {
					t.Fatal("expected non-empty ID")
				}
				if !strings.HasPrefix(e.GetID(), "urn:ak33m:home:section:") {
					t.Fatalf("got id %q, want prefix %q", e.GetID(), "urn:ak33m:home:section:")
				}
				if e.Name() != "Hero" {
					t.Fatalf("got name %q, want %q", e.Name(), "Hero")
				}
				if e.Slot() != "hero.headline" {
					t.Fatalf("got slot %q, want %q", e.Slot(), "hero.headline")
				}
			},
		},
		{
			name:             "invalid - empty name",
			inputName:        "",
			inputSlot:        "hero.headline",
			inputWebsiteSlug: "ak33m",
			inputPageSlug:    "home",
			wantErr:          true,
			errSubstr:        "name cannot be empty",
		},
		{
			name:             "invalid - empty slot",
			inputName:        "Hero",
			inputSlot:        "",
			inputWebsiteSlug: "ak33m",
			inputPageSlug:    "home",
			wantErr:          true,
			errSubstr:        "slot cannot be empty",
		},
		{
			name:             "invalid - empty websiteSlug",
			inputName:        "Hero",
			inputSlot:        "hero.headline",
			inputWebsiteSlug: "",
			inputPageSlug:    "home",
			wantErr:          true,
			errSubstr:        "websiteSlug cannot be empty",
		},
		{
			name:             "invalid - empty pageSlug",
			inputName:        "Hero",
			inputSlot:        "hero.headline",
			inputWebsiteSlug: "ak33m",
			inputPageSlug:    "",
			wantErr:          true,
			errSubstr:        "pageSlug cannot be empty",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entity, err := new(Section).With(
				tt.inputName, tt.inputSlot,
				tt.inputWebsiteSlug, tt.inputPageSlug,
			)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSubstr != "" && !contains(err.Error(), tt.errSubstr) {
					t.Fatalf("error should contain %q, got: %v", tt.errSubstr, err)
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

func TestSection_ApplyEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	setupSection := func(t *testing.T, id string) *Section {
		t.Helper()
		e := &Section{}
		e.BaseEntity = ddd.NewBaseEntity(id)
		return e
	}

	tests := []struct {
		name      string
		setup     func(*testing.T) *Section
		event     domain.EventEnvelope[any]
		wantErr   bool
		errSubstr string
		validate  func(*testing.T, *Section)
	}{
		{
			name: "SectionCreated restores fields",
			setup: func(t *testing.T) *Section {
				return setupSection(t, "urn:ak33m:home:section:abc123")
			},
			event: newTestEnvelope(
				SectionCreated{
					Name: "Hero", Slot: "hero.headline",
					Timestamp: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
				},
				"urn:ak33m:home:section:abc123", "Section.Created", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *Section) {
				t.Helper()
				if e.Name() != "Hero" {
					t.Fatalf("got name %q, want %q", e.Name(), "Hero")
				}
				if e.Slot() != "hero.headline" {
					t.Fatalf("got slot %q, want %q", e.Slot(), "hero.headline")
				}
			},
		},
		{
			name: "SectionPageLinked is acknowledged",
			setup: func(t *testing.T) *Section {
				return setupSection(t, "urn:ak33m:home:section:abc456")
			},
			event: newTestEnvelope(
				SectionPageLinked{
					BasicTripleEvent: domain.BasicTripleEvent{
						Subject: "urn:ak33m:home:section:abc456", Predicate: PredicateBelongsTo,
						Object: "urn:ak33m:page:xyz:home",
					},
					Timestamp: time.Now(),
				},
				"urn:ak33m:home:section:abc456", "Section.PageLinked", 1,
			),
			wantErr: false,
		},
		{
			name: "unknown event type returns error",
			setup: func(t *testing.T) *Section {
				return setupSection(t, "urn:ak33m:home:section:abc789")
			},
			event:     newTestEnvelope(struct{}{}, "urn:ak33m:home:section:abc789", "UnknownEvent", 1),
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
					t.Fatalf("error should contain %q, got: %v", tt.errSubstr, err)
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

func TestSectionCreated_EventType(t *testing.T) {
	t.Parallel()
	if got := (SectionCreated{}).EventType(); got != "Section.Created" {
		t.Fatalf("EventType() = %q, want %q", got, "Section.Created")
	}
}

func TestSectionPageLinked_EventType(t *testing.T) {
	t.Parallel()
	if got := (SectionPageLinked{}).EventType(); got != "Section.PageLinked" {
		t.Fatalf("EventType() = %q, want %q", got, "Section.PageLinked")
	}
}
