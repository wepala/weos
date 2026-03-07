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

func TestWebsite_With(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		inputName string
		inputURL  string
		inputSlug string
		wantErr   bool
		errSubstr string
		validate  func(*testing.T, *Website)
	}{
		{
			name:      "happy path - valid website",
			inputName: "My Site",
			inputURL:  "https://example.com",
			inputSlug: "my-site",
			wantErr:   false,
			validate: func(t *testing.T, e *Website) {
				t.Helper()
				if e.GetID() != "urn:my-site" {
					t.Fatalf("got id %q, want %q", e.GetID(), "urn:my-site")
				}
				if e.Name() != "My Site" {
					t.Fatalf("got name %q, want %q", e.Name(), "My Site")
				}
				if e.Slug() != "my-site" {
					t.Fatalf("got slug %q, want %q", e.Slug(), "my-site")
				}
				if e.URL() != "https://example.com" {
					t.Fatalf("got url %q, want %q", e.URL(), "https://example.com")
				}
				if e.Language() != "en" {
					t.Fatalf("got language %q, want %q", e.Language(), "en")
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
			inputURL:  "https://example.com",
			inputSlug: "my-site",
			wantErr:   true,
			errSubstr: "name cannot be empty",
		},
		{
			name:      "invalid - empty url",
			inputName: "My Site",
			inputURL:  "",
			inputSlug: "my-site",
			wantErr:   true,
			errSubstr: "url cannot be empty",
		},
		{
			name:      "invalid - empty slug",
			inputName: "My Site",
			inputURL:  "https://example.com",
			inputSlug: "",
			wantErr:   true,
			errSubstr: "slug cannot be empty",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entity, err := new(Website).With(tt.inputName, tt.inputURL, tt.inputSlug)
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

func TestWebsite_ApplyEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	setupWebsite := func(t *testing.T, id string) *Website {
		t.Helper()
		e := &Website{}
		e.BaseEntity = ddd.NewBaseEntity(id)
		return e
	}

	tests := []struct {
		name      string
		setup     func(*testing.T) *Website
		event     domain.EventEnvelope[any]
		wantErr   bool
		errSubstr string
		validate  func(*testing.T, *Website)
	}{
		{
			name: "WebsiteCreated restores fields",
			setup: func(t *testing.T) *Website {
				return setupWebsite(t, "urn:test-site")
			},
			event: newTestEnvelope(
				WebsiteCreated{
					Name:      "Test Site",
					URL:       "https://test.com",
					Slug:      "test-site",
					Timestamp: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
				},
				"urn:test-site", "Website.Created", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *Website) {
				t.Helper()
				if e.Name() != "Test Site" {
					t.Fatalf("got name %q, want %q", e.Name(), "Test Site")
				}
				if e.URL() != "https://test.com" {
					t.Fatalf("got url %q, want %q", e.URL(), "https://test.com")
				}
				if e.Slug() != "test-site" {
					t.Fatalf("got slug %q, want %q", e.Slug(), "test-site")
				}
			},
		},
		{
			name: "WebsiteUpdated updates fields",
			setup: func(t *testing.T) *Website {
				w := setupWebsite(t, "urn:test-456")
				w.name = "Old Name"
				return w
			},
			event: newTestEnvelope(
				WebsiteUpdated{
					Name: "New Name", URL: "https://new.com",
					Slug:        "test-456",
					Description: "A description", Language: "fr",
					Status: "published", Timestamp: time.Now(),
				},
				"urn:test-456", "Website.Updated", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *Website) {
				t.Helper()
				if e.Name() != "New Name" {
					t.Fatalf("got name %q, want %q", e.Name(), "New Name")
				}
			},
		},
		{
			name: "WebsiteDeleted sets archived",
			setup: func(t *testing.T) *Website {
				w := setupWebsite(t, "urn:test-789")
				w.status = "published"
				return w
			},
			event: newTestEnvelope(
				WebsiteDeleted{Timestamp: time.Now()},
				"urn:test-789", "Website.Deleted", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *Website) {
				t.Helper()
				if e.Status() != "archived" {
					t.Fatalf("got status %q, want %q", e.Status(), "archived")
				}
			},
		},
		{
			name: "unknown event type returns error",
			setup: func(t *testing.T) *Website {
				return setupWebsite(t, "urn:test-unknown")
			},
			event:     newTestEnvelope(struct{}{}, "urn:test-unknown", "UnknownEvent", 1),
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

func TestWebsiteCreated_EventType(t *testing.T) {
	t.Parallel()
	if got := (WebsiteCreated{}).EventType(); got != "Website.Created" {
		t.Fatalf("EventType() = %q, want %q", got, "Website.Created")
	}
}

func TestWebsiteUpdated_EventType(t *testing.T) {
	t.Parallel()
	if got := (WebsiteUpdated{}).EventType(); got != "Website.Updated" {
		t.Fatalf("EventType() = %q, want %q", got, "Website.Updated")
	}
}

func TestWebsiteDeleted_EventType(t *testing.T) {
	t.Parallel()
	if got := (WebsiteDeleted{}).EventType(); got != "Website.Deleted" {
		t.Fatalf("EventType() = %q, want %q", got, "Website.Deleted")
	}
}

func newTestEnvelope(
	payload any, aggregateID, eventType string, sequenceNo int,
) domain.EventEnvelope[any] {
	typed := domain.NewEventEnvelope(payload, aggregateID, eventType, sequenceNo)
	return domain.EventEnvelope[any]{
		ID:          typed.ID,
		AggregateID: typed.AggregateID,
		EventType:   typed.EventType,
		SequenceNo:  typed.SequenceNo,
		Payload:     typed.Payload,
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
