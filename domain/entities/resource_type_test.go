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
	"encoding/json"
	"testing"
	"time"

	"github.com/akeemphilbert/pericarp/pkg/ddd"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

func TestResourceType_With(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		inputName string
		inputSlug string
		inputCtx  json.RawMessage
		wantErr   bool
		errSubstr string
		validate  func(*testing.T, *ResourceType)
	}{
		{
			name:      "happy path - valid resource type",
			inputName: "Product",
			inputSlug: "products",
			inputCtx:  json.RawMessage(`"https://schema.org"`),
			wantErr:   false,
			validate: func(t *testing.T, e *ResourceType) {
				t.Helper()
				if e.GetID() != "urn:type:products" {
					t.Fatalf("got id %q, want %q", e.GetID(), "urn:type:products")
				}
				if e.Name() != "Product" {
					t.Fatalf("got name %q, want %q", e.Name(), "Product")
				}
				if e.Slug() != "products" {
					t.Fatalf("got slug %q, want %q", e.Slug(), "products")
				}
				if e.Status() != "active" {
					t.Fatalf("got status %q, want %q", e.Status(), "active")
				}
				if e.CreatedAt().IsZero() {
					t.Fatal("expected non-zero CreatedAt")
				}
				if string(e.Context()) != `"https://schema.org"` {
					t.Fatalf("got context %q, want %q", string(e.Context()), `"https://schema.org"`)
				}
			},
		},
		{
			name:      "valid without context",
			inputName: "Event",
			inputSlug: "events",
			inputCtx:  nil,
			wantErr:   false,
			validate: func(t *testing.T, e *ResourceType) {
				t.Helper()
				if e.Context() != nil {
					t.Fatalf("expected nil context, got %q", string(e.Context()))
				}
			},
		},
		{
			name:      "happy path - with description and schema",
			inputName: "Article",
			inputSlug: "article",
			inputCtx:  json.RawMessage(`"https://schema.org"`),
			wantErr:   false,
			validate: func(t *testing.T, e *ResourceType) {
				t.Helper()
				// Create with description + schema via direct constructor call
				rt, err := new(ResourceType).With(
					"Article", "article", "A written composition",
					json.RawMessage(`"https://schema.org"`),
					json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}}}`),
				)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if rt.Description() != "A written composition" {
					t.Fatalf("got description %q, want %q", rt.Description(), "A written composition")
				}
				if string(rt.Schema()) != `{"type":"object","properties":{"title":{"type":"string"}}}` {
					t.Fatalf("got schema %q", string(rt.Schema()))
				}
			},
		},
		{
			name:      "invalid - empty name",
			inputName: "",
			inputSlug: "products",
			wantErr:   true,
			errSubstr: "name cannot be empty",
		},
		{
			name:      "invalid - empty slug",
			inputName: "Product",
			inputSlug: "",
			wantErr:   true,
			errSubstr: "slug cannot be empty",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entity, err := new(ResourceType).With(tt.inputName, tt.inputSlug, "", tt.inputCtx, nil)
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
			if entity == nil {
				t.Fatal("expected non-nil entity")
			}
			if tt.validate != nil {
				tt.validate(t, entity)
			}
		})
	}
}

func TestResourceType_Restore(t *testing.T) {
	t.Parallel()

	e := &ResourceType{}
	err := e.Restore(
		"urn:type:products", "Product", "products", "A product type", "active",
		json.RawMessage(`"https://schema.org"`),
		json.RawMessage(`{"type":"object"}`),
		time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC), 5,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.GetID() != "urn:type:products" {
		t.Fatalf("got id %q, want %q", e.GetID(), "urn:type:products")
	}
	if e.Name() != "Product" {
		t.Fatalf("got name %q", e.Name())
	}
	if e.Description() != "A product type" {
		t.Fatalf("got description %q", e.Description())
	}
	if string(e.Schema()) != `{"type":"object"}` {
		t.Fatalf("got schema %q", string(e.Schema()))
	}
	if e.GetSequenceNo() != 5 {
		t.Fatalf("got sequenceNo %d, want 5", e.GetSequenceNo())
	}
}

func TestResourceType_RestoreErrors(t *testing.T) {
	t.Parallel()

	if err := new(ResourceType).Restore("", "n", "s", "", "active", nil, nil, time.Now(), 0); err == nil {
		t.Fatal("expected error for empty id")
	}
	if err := new(ResourceType).Restore("id", "", "s", "", "active", nil, nil, time.Now(), 0); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestResourceType_ApplyEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	setupRT := func(t *testing.T, id string) *ResourceType {
		t.Helper()
		e := &ResourceType{}
		e.BaseEntity = ddd.NewBaseEntity(id)
		return e
	}

	tests := []struct {
		name      string
		setup     func(*testing.T) *ResourceType
		event     domain.EventEnvelope[any]
		wantErr   bool
		errSubstr string
		validate  func(*testing.T, *ResourceType)
	}{
		{
			name: "ResourceTypeCreated restores fields",
			setup: func(t *testing.T) *ResourceType {
				return setupRT(t, "urn:type:products")
			},
			event: newTestEnvelope(
				ResourceTypeCreated{
					Name:      "Product",
					Slug:      "products",
					Context:   json.RawMessage(`"https://schema.org"`),
					Timestamp: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
				},
				"urn:type:products", "ResourceType.Created", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *ResourceType) {
				t.Helper()
				if e.Name() != "Product" {
					t.Fatalf("got name %q, want %q", e.Name(), "Product")
				}
				if e.Status() != "active" {
					t.Fatalf("got status %q, want %q", e.Status(), "active")
				}
			},
		},
		{
			name: "ResourceTypeUpdated updates fields",
			setup: func(t *testing.T) *ResourceType {
				rt := setupRT(t, "urn:type:products")
				rt.name = "Old"
				return rt
			},
			event: newTestEnvelope(
				ResourceTypeUpdated{
					Name:        "New Product",
					Slug:        "products",
					Description: "Updated desc",
					Status:      "active",
					Timestamp:   time.Now(),
				},
				"urn:type:products", "ResourceType.Updated", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *ResourceType) {
				t.Helper()
				if e.Name() != "New Product" {
					t.Fatalf("got name %q", e.Name())
				}
				if e.Description() != "Updated desc" {
					t.Fatalf("got description %q", e.Description())
				}
			},
		},
		{
			name: "ResourceTypeDeleted sets archived",
			setup: func(t *testing.T) *ResourceType {
				rt := setupRT(t, "urn:type:del")
				rt.status = "active"
				return rt
			},
			event: newTestEnvelope(
				ResourceTypeDeleted{Timestamp: time.Now()},
				"urn:type:del", "ResourceType.Deleted", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *ResourceType) {
				t.Helper()
				if e.Status() != "archived" {
					t.Fatalf("got status %q, want %q", e.Status(), "archived")
				}
			},
		},
		{
			name: "unknown event type returns error",
			setup: func(t *testing.T) *ResourceType {
				return setupRT(t, "urn:type:unknown")
			},
			event:     newTestEnvelope(struct{}{}, "urn:type:unknown", "UnknownEvent", 1),
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

func TestResourceTypeCreated_EventType(t *testing.T) {
	t.Parallel()
	if got := (ResourceTypeCreated{}).EventType(); got != "ResourceType.Created" {
		t.Fatalf("EventType() = %q, want %q", got, "ResourceType.Created")
	}
}

func TestResourceTypeUpdated_EventType(t *testing.T) {
	t.Parallel()
	if got := (ResourceTypeUpdated{}).EventType(); got != "ResourceType.Updated" {
		t.Fatalf("EventType() = %q, want %q", got, "ResourceType.Updated")
	}
}

func TestResourceTypeDeleted_EventType(t *testing.T) {
	t.Parallel()
	if got := (ResourceTypeDeleted{}).EventType(); got != "ResourceType.Deleted" {
		t.Fatalf("EventType() = %q, want %q", got, "ResourceType.Deleted")
	}
}
