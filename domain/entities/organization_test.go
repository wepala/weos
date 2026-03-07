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

func TestOrganization_With(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		inputName string
		inputSlug string
		wantErr   bool
		errSubstr string
		validate  func(*testing.T, *Organization)
	}{
		{
			name:      "happy path - valid organization",
			inputName: "Acme Corp",
			inputSlug: "acme-corp",
			wantErr:   false,
			validate: func(t *testing.T, e *Organization) {
				t.Helper()
				if e.GetID() != "urn:org:acme-corp" {
					t.Fatalf("got id %q, want %q", e.GetID(), "urn:org:acme-corp")
				}
				if e.Name() != "Acme Corp" {
					t.Fatalf("got name %q, want %q", e.Name(), "Acme Corp")
				}
				if e.Slug() != "acme-corp" {
					t.Fatalf("got slug %q, want %q", e.Slug(), "acme-corp")
				}
				if e.Status() != "active" {
					t.Fatalf("got status %q, want %q", e.Status(), "active")
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
			inputName: "Org",
			inputSlug: "",
			wantErr:   true,
			errSubstr: "slug cannot be empty",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entity, err := new(Organization).With(tt.inputName, tt.inputSlug)
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

func TestOrganization_ApplyEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	setupOrg := func(t *testing.T, id string) *Organization {
		t.Helper()
		e := &Organization{}
		e.BaseEntity = ddd.NewBaseEntity(id)
		return e
	}

	tests := []struct {
		name      string
		setup     func(*testing.T) *Organization
		event     domain.EventEnvelope[any]
		wantErr   bool
		errSubstr string
		validate  func(*testing.T, *Organization)
	}{
		{
			name: "OrganizationCreated restores fields",
			setup: func(t *testing.T) *Organization {
				return setupOrg(t, "urn:org:test")
			},
			event: newTestEnvelope(
				OrganizationCreated{
					Name:      "Test Org",
					Slug:      "test",
					Timestamp: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
				},
				"urn:org:test", "Organization.Created", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *Organization) {
				t.Helper()
				if e.Name() != "Test Org" {
					t.Fatalf("got name %q, want %q", e.Name(), "Test Org")
				}
				if e.Slug() != "test" {
					t.Fatalf("got slug %q, want %q", e.Slug(), "test")
				}
				if e.Status() != "active" {
					t.Fatalf("got status %q, want %q", e.Status(), "active")
				}
			},
		},
		{
			name: "OrganizationUpdated updates fields",
			setup: func(t *testing.T) *Organization {
				o := setupOrg(t, "urn:org:update-test")
				o.name = "Old Name"
				return o
			},
			event: newTestEnvelope(
				OrganizationUpdated{
					Name:        "New Name",
					Slug:        "update-test",
					Description: "A desc",
					URL:         "https://example.com",
					LogoURL:     "/logo.png",
					Status:      "active",
					Timestamp:   time.Now(),
				},
				"urn:org:update-test", "Organization.Updated", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *Organization) {
				t.Helper()
				if e.Name() != "New Name" {
					t.Fatalf("got name %q, want %q", e.Name(), "New Name")
				}
				if e.URL() != "https://example.com" {
					t.Fatalf("got url %q, want %q", e.URL(), "https://example.com")
				}
			},
		},
		{
			name: "OrganizationDeleted sets archived",
			setup: func(t *testing.T) *Organization {
				o := setupOrg(t, "urn:org:del-test")
				o.status = "active"
				return o
			},
			event: newTestEnvelope(
				OrganizationDeleted{Timestamp: time.Now()},
				"urn:org:del-test", "Organization.Deleted", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *Organization) {
				t.Helper()
				if e.Status() != "archived" {
					t.Fatalf("got status %q, want %q", e.Status(), "archived")
				}
			},
		},
		{
			name: "unknown event type returns error",
			setup: func(t *testing.T) *Organization {
				return setupOrg(t, "urn:org:unknown")
			},
			event:     newTestEnvelope(struct{}{}, "urn:org:unknown", "UnknownEvent", 1),
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

func TestOrganizationCreated_EventType(t *testing.T) {
	t.Parallel()
	if got := (OrganizationCreated{}).EventType(); got != "Organization.Created" {
		t.Fatalf("EventType() = %q, want %q", got, "Organization.Created")
	}
}

func TestOrganizationUpdated_EventType(t *testing.T) {
	t.Parallel()
	if got := (OrganizationUpdated{}).EventType(); got != "Organization.Updated" {
		t.Fatalf("EventType() = %q, want %q", got, "Organization.Updated")
	}
}

func TestOrganizationDeleted_EventType(t *testing.T) {
	t.Parallel()
	if got := (OrganizationDeleted{}).EventType(); got != "Organization.Deleted" {
		t.Fatalf("EventType() = %q, want %q", got, "Organization.Deleted")
	}
}
