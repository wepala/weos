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

func TestPage_With(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		inputName        string
		inputSlug        string
		inputWebsiteSlug string
		wantErr          bool
		errSubstr        string
		validate         func(*testing.T, *Page)
	}{
		{
			name:             "happy path - valid page",
			inputName:        "Home",
			inputSlug:        "home",
			inputWebsiteSlug: "ak33m",
			wantErr:          false,
			validate: func(t *testing.T, e *Page) {
				t.Helper()
				if e.GetID() == "" {
					t.Fatal("expected non-empty ID")
				}
				if !strings.HasPrefix(e.GetID(), "urn:ak33m:page:") {
					t.Fatalf("got id %q, want prefix %q", e.GetID(), "urn:ak33m:page:")
				}
				if !strings.HasSuffix(e.GetID(), ":home") {
					t.Fatalf("got id %q, want suffix %q", e.GetID(), ":home")
				}
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
			name:             "invalid - empty name",
			inputName:        "",
			inputSlug:        "home",
			inputWebsiteSlug: "ak33m",
			wantErr:          true,
			errSubstr:        "name cannot be empty",
		},
		{
			name:             "invalid - empty slug",
			inputName:        "Home",
			inputSlug:        "",
			inputWebsiteSlug: "ak33m",
			wantErr:          true,
			errSubstr:        "slug cannot be empty",
		},
		{
			name:             "invalid - empty websiteSlug",
			inputName:        "Home",
			inputSlug:        "home",
			inputWebsiteSlug: "",
			wantErr:          true,
			errSubstr:        "websiteSlug cannot be empty",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entity, err := new(Page).With(tt.inputName, tt.inputSlug, tt.inputWebsiteSlug)
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

func TestPage_ApplyEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	setupPage := func(t *testing.T, id string) *Page {
		t.Helper()
		e := &Page{}
		e.BaseEntity = ddd.NewBaseEntity(id)
		return e
	}

	tests := []struct {
		name      string
		setup     func(*testing.T) *Page
		event     domain.EventEnvelope[any]
		wantErr   bool
		errSubstr string
		validate  func(*testing.T, *Page)
	}{
		{
			name: "PageCreated restores fields",
			setup: func(t *testing.T) *Page {
				return setupPage(t, "urn:ak33m:page:abc123:home")
			},
			event: newTestEnvelope(
				PageCreated{
					Name: "Home", Slug: "home",
					Timestamp: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
				},
				"urn:ak33m:page:abc123:home", "Page.Created", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *Page) {
				t.Helper()
				if e.Name() != "Home" {
					t.Fatalf("got name %q, want %q", e.Name(), "Home")
				}
				if e.Slug() != "home" {
					t.Fatalf("got slug %q, want %q", e.Slug(), "home")
				}
			},
		},
		{
			name: "PageWebsiteLinked is acknowledged",
			setup: func(t *testing.T) *Page {
				return setupPage(t, "urn:ak33m:page:abc456:about")
			},
			event: newTestEnvelope(
				PageWebsiteLinked{
					BasicTripleEvent: domain.BasicTripleEvent{
						Subject: "urn:ak33m:page:abc456:about", Predicate: PredicateBelongsTo,
						Object: "urn:ak33m",
					},
					Timestamp: time.Now(),
				},
				"urn:ak33m:page:abc456:about", "Page.WebsiteLinked", 1,
			),
			wantErr: false,
		},
		{
			name: "unknown event type returns error",
			setup: func(t *testing.T) *Page {
				return setupPage(t, "urn:ak33m:page:abc789:contact")
			},
			event:     newTestEnvelope(struct{}{}, "urn:ak33m:page:abc789:contact", "UnknownEvent", 1),
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

func TestPageCreated_EventType(t *testing.T) {
	t.Parallel()
	if got := (PageCreated{}).EventType(); got != "Page.Created" {
		t.Fatalf("EventType() = %q, want %q", got, "Page.Created")
	}
}

func TestPageWebsiteLinked_EventType(t *testing.T) {
	t.Parallel()
	if got := (PageWebsiteLinked{}).EventType(); got != "Page.WebsiteLinked" {
		t.Fatalf("EventType() = %q, want %q", got, "Page.WebsiteLinked")
	}
}
