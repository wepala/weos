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

func TestPerson_With(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		inputGivenName  string
		inputFamilyName string
		inputEmail      string
		wantErr         bool
		errSubstr       string
		validate        func(*testing.T, *Person)
	}{
		{
			name:            "happy path - valid person",
			inputGivenName:  "John",
			inputFamilyName: "Doe",
			inputEmail:      "john@example.com",
			wantErr:         false,
			validate: func(t *testing.T, e *Person) {
				t.Helper()
				if !strings.HasPrefix(e.GetID(), "urn:person:") {
					t.Fatalf("got id %q, want prefix %q", e.GetID(), "urn:person:")
				}
				if e.GivenName() != "John" {
					t.Fatalf("got givenName %q, want %q", e.GivenName(), "John")
				}
				if e.FamilyName() != "Doe" {
					t.Fatalf("got familyName %q, want %q", e.FamilyName(), "Doe")
				}
				if e.Name() != "John Doe" {
					t.Fatalf("got name %q, want %q", e.Name(), "John Doe")
				}
				if e.Email() != "john@example.com" {
					t.Fatalf("got email %q, want %q", e.Email(), "john@example.com")
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
			name:            "invalid - empty givenName",
			inputGivenName:  "",
			inputFamilyName: "Doe",
			inputEmail:      "john@example.com",
			wantErr:         true,
			errSubstr:       "givenName cannot be empty",
		},
		{
			name:            "invalid - empty familyName",
			inputGivenName:  "John",
			inputFamilyName: "",
			inputEmail:      "john@example.com",
			wantErr:         true,
			errSubstr:       "familyName cannot be empty",
		},
		{
			name:            "valid - no email",
			inputGivenName:  "John",
			inputFamilyName: "Doe",
			inputEmail:      "",
			wantErr:         false,
			validate: func(t *testing.T, e *Person) {
				t.Helper()
				if e.Email() != "" {
					t.Fatalf("got email %q, want empty", e.Email())
				}
				if e.Status() != "active" {
					t.Fatalf("got status %q, want %q", e.Status(), "active")
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entity, err := new(Person).With(
				tt.inputGivenName, tt.inputFamilyName, tt.inputEmail)
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

func TestPerson_ApplyEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	setupPerson := func(t *testing.T, id string) *Person {
		t.Helper()
		e := &Person{}
		e.BaseEntity = ddd.NewBaseEntity(id)
		return e
	}

	tests := []struct {
		name      string
		setup     func(*testing.T) *Person
		event     domain.EventEnvelope[any]
		wantErr   bool
		errSubstr string
		validate  func(*testing.T, *Person)
	}{
		{
			name: "PersonCreated restores fields",
			setup: func(t *testing.T) *Person {
				return setupPerson(t, "urn:person:abc123")
			},
			event: newTestEnvelope(
				PersonCreated{
					GivenName:  "Jane",
					FamilyName: "Smith",
					Email:      "jane@example.com",
					Timestamp:  time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
				},
				"urn:person:abc123", "Person.Created", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *Person) {
				t.Helper()
				if e.GivenName() != "Jane" {
					t.Fatalf("got givenName %q, want %q", e.GivenName(), "Jane")
				}
				if e.FamilyName() != "Smith" {
					t.Fatalf("got familyName %q, want %q", e.FamilyName(), "Smith")
				}
				if e.Status() != "active" {
					t.Fatalf("got status %q, want %q", e.Status(), "active")
				}
			},
		},
		{
			name: "PersonUpdated updates fields",
			setup: func(t *testing.T) *Person {
				p := setupPerson(t, "urn:person:def456")
				p.givenName = "Old"
				return p
			},
			event: newTestEnvelope(
				PersonUpdated{
					GivenName:  "New",
					FamilyName: "Name",
					Email:      "new@example.com",
					AvatarURL:  "/avatar.png",
					Status:     "active",
					Timestamp:  time.Now(),
				},
				"urn:person:def456", "Person.Updated", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *Person) {
				t.Helper()
				if e.GivenName() != "New" {
					t.Fatalf("got givenName %q, want %q", e.GivenName(), "New")
				}
				if e.AvatarURL() != "/avatar.png" {
					t.Fatalf("got avatarURL %q, want %q", e.AvatarURL(), "/avatar.png")
				}
			},
		},
		{
			name: "PersonDeleted sets archived",
			setup: func(t *testing.T) *Person {
				p := setupPerson(t, "urn:person:ghi789")
				p.status = "active"
				return p
			},
			event: newTestEnvelope(
				PersonDeleted{Timestamp: time.Now()},
				"urn:person:ghi789", "Person.Deleted", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *Person) {
				t.Helper()
				if e.Status() != "archived" {
					t.Fatalf("got status %q, want %q", e.Status(), "archived")
				}
			},
		},
		{
			name: "PersonOrganizationLinked acknowledged",
			setup: func(t *testing.T) *Person {
				return setupPerson(t, "urn:person:jkl012")
			},
			event: newTestEnvelope(
				PersonOrganizationLinked{
					BasicTripleEvent: domain.BasicTripleEvent{
						Subject:   "urn:person:jkl012",
						Predicate: PredicateMemberOf,
						Object:    "urn:org:acme",
					},
					Timestamp: time.Now(),
				},
				"urn:person:jkl012", "Person.OrganizationLinked", 1,
			),
			wantErr: false,
		},
		{
			name: "unknown event type returns error",
			setup: func(t *testing.T) *Person {
				return setupPerson(t, "urn:person:xyz")
			},
			event:     newTestEnvelope(struct{}{}, "urn:person:xyz", "Unknown", 1),
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

func TestPersonCreated_EventType(t *testing.T) {
	t.Parallel()
	if got := (PersonCreated{}).EventType(); got != "Person.Created" {
		t.Fatalf("EventType() = %q, want %q", got, "Person.Created")
	}
}

func TestPersonUpdated_EventType(t *testing.T) {
	t.Parallel()
	if got := (PersonUpdated{}).EventType(); got != "Person.Updated" {
		t.Fatalf("EventType() = %q, want %q", got, "Person.Updated")
	}
}

func TestPersonDeleted_EventType(t *testing.T) {
	t.Parallel()
	if got := (PersonDeleted{}).EventType(); got != "Person.Deleted" {
		t.Fatalf("EventType() = %q, want %q", got, "Person.Deleted")
	}
}
