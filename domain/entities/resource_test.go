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

func TestResource_With(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		id        string
		typeSlug  string
		data      json.RawMessage
		wantErr   bool
		errSubstr string
		validate  func(*testing.T, *Resource)
	}{
		{
			name:     "happy path - valid resource with graph data",
			id:       "urn:products:abc123",
			typeSlug: "products",
			data:     json.RawMessage(`{"@graph":[{"@id":"urn:products:abc123","@type":"Product","name":"Widget"}]}`),
			wantErr:  false,
			validate: func(t *testing.T, e *Resource) {
				t.Helper()
				if e.GetID() != "urn:products:abc123" {
					t.Fatalf("got id %q, want %q", e.GetID(), "urn:products:abc123")
				}
				if e.TypeSlug() != "products" {
					t.Fatalf("got typeSlug %q", e.TypeSlug())
				}
				if e.Status() != "active" {
					t.Fatalf("got status %q", e.Status())
				}
			},
		},
		{
			name:      "invalid - empty id",
			id:        "",
			typeSlug:  "products",
			data:      json.RawMessage(`{"name":"Widget"}`),
			wantErr:   true,
			errSubstr: "id cannot be empty",
		},
		{
			name:      "invalid - empty type slug",
			id:        "urn:products:abc",
			typeSlug:  "",
			data:      json.RawMessage(`{"name":"Widget"}`),
			wantErr:   true,
			errSubstr: "type slug cannot be empty",
		},
		{
			name:      "invalid - empty data",
			id:        "urn:products:abc",
			typeSlug:  "products",
			data:      nil,
			wantErr:   true,
			errSubstr: "data cannot be empty",
		},
		{
			name:      "invalid - non-valid JSON",
			id:        "urn:products:abc",
			typeSlug:  "products",
			data:      json.RawMessage(`not json`),
			wantErr:   true,
			errSubstr: "data must be valid JSON",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entity, err := new(Resource).With(tt.id, tt.typeSlug, tt.data)
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

func TestResource_Restore(t *testing.T) {
	t.Parallel()

	e := &Resource{}
	data := json.RawMessage(`{"@id":"urn:products:abc","@type":"Product","name":"Widget"}`)
	err := e.Restore("urn:products:abc", "products", "active", data,
		time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC), 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.GetID() != "urn:products:abc" {
		t.Fatalf("got id %q", e.GetID())
	}
	if e.TypeSlug() != "products" {
		t.Fatalf("got typeSlug %q", e.TypeSlug())
	}
	if e.GetSequenceNo() != 3 {
		t.Fatalf("got sequenceNo %d, want 3", e.GetSequenceNo())
	}
}

func TestResource_RestoreErrors(t *testing.T) {
	t.Parallel()

	if err := new(Resource).Restore("", "p", "active", nil, time.Now(), 0); err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestResource_ApplyEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	setupRes := func(t *testing.T, id string) *Resource {
		t.Helper()
		e := &Resource{}
		e.BaseEntity = ddd.NewBaseEntity(id)
		return e
	}

	tests := []struct {
		name      string
		setup     func(*testing.T) *Resource
		event     domain.EventEnvelope[any]
		wantErr   bool
		errSubstr string
		validate  func(*testing.T, *Resource)
	}{
		{
			name: "ResourceCreated restores fields",
			setup: func(t *testing.T) *Resource {
				return setupRes(t, "urn:products:abc")
			},
			event: newTestEnvelope(
				ResourceCreated{
					TypeSlug:  "products",
					Data:      json.RawMessage(`{"name":"Widget"}`),
					Timestamp: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
				},
				"urn:products:abc", "Resource.Created", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *Resource) {
				t.Helper()
				if e.TypeSlug() != "products" {
					t.Fatalf("got typeSlug %q", e.TypeSlug())
				}
				if e.Status() != "active" {
					t.Fatalf("got status %q", e.Status())
				}
			},
		},
		{
			name: "ResourceUpdated updates data",
			setup: func(t *testing.T) *Resource {
				r := setupRes(t, "urn:products:abc")
				r.data = json.RawMessage(`{"name":"Old"}`)
				return r
			},
			event: newTestEnvelope(
				ResourceUpdated{
					Data:      json.RawMessage(`{"name":"New"}`),
					Timestamp: time.Now(),
				},
				"urn:products:abc", "Resource.Updated", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *Resource) {
				t.Helper()
				if string(e.Data()) != `{"name":"New"}` {
					t.Fatalf("got data %q", string(e.Data()))
				}
			},
		},
		{
			name: "ResourceDeleted sets archived",
			setup: func(t *testing.T) *Resource {
				r := setupRes(t, "urn:products:del")
				r.status = "active"
				return r
			},
			event: newTestEnvelope(
				ResourceDeleted{Timestamp: time.Now()},
				"urn:products:del", "Resource.Deleted", 1,
			),
			wantErr: false,
			validate: func(t *testing.T, e *Resource) {
				t.Helper()
				if e.Status() != "archived" {
					t.Fatalf("got status %q, want %q", e.Status(), "archived")
				}
			},
		},
		{
			name: "unknown event type returns error",
			setup: func(t *testing.T) *Resource {
				return setupRes(t, "urn:products:unk")
			},
			event:     newTestEnvelope(struct{}{}, "urn:products:unk", "UnknownEvent", 1),
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

func TestSimplifyJSONLD(t *testing.T) {
	t.Parallel()

	input := json.RawMessage(`{"@id":"urn:products:abc","@type":"Product","@context":"https://schema.org","name":"Widget"}`)
	result, err := SimplifyJSONLD(input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if m["id"] != "urn:products:abc" {
		t.Fatalf("id = %v, want urn:products:abc", m["id"])
	}
	if m["type"] != "Product" {
		t.Fatalf("type = %v, want Product", m["type"])
	}
	if _, ok := m["@context"]; ok {
		t.Fatal("expected @context to be removed")
	}
	if _, ok := m["@id"]; ok {
		t.Fatal("expected @id to be removed")
	}
	if _, ok := m["@type"]; ok {
		t.Fatal("expected @type to be removed")
	}
	if m["name"] != "Widget" {
		t.Fatalf("name = %v, want Widget", m["name"])
	}
}

func TestInjectJSONLDForUpdate(t *testing.T) {
	t.Parallel()

	data := json.RawMessage(`{"name":"Updated Widget"}`)
	result, err := InjectJSONLDForUpdate(data, "urn:products:abc", "Product",
		json.RawMessage(`"https://schema.org"`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if m["@id"] != "urn:products:abc" {
		t.Fatalf("@id = %v", m["@id"])
	}
	if m["@type"] != "Product" {
		t.Fatalf("@type = %v", m["@type"])
	}
	if m["name"] != "Updated Widget" {
		t.Fatalf("name = %v", m["name"])
	}
}

func TestResourceCreated_EventType(t *testing.T) {
	t.Parallel()
	if got := (ResourceCreated{}).EventType(); got != "Resource.Created" {
		t.Fatalf("EventType() = %q, want %q", got, "Resource.Created")
	}
}

func TestResourceUpdated_EventType(t *testing.T) {
	t.Parallel()
	if got := (ResourceUpdated{}).EventType(); got != "Resource.Updated" {
		t.Fatalf("EventType() = %q, want %q", got, "Resource.Updated")
	}
}

func TestResourceDeleted_EventType(t *testing.T) {
	t.Parallel()
	if got := (ResourceDeleted{}).EventType(); got != "Resource.Deleted" {
		t.Fatalf("EventType() = %q, want %q", got, "Resource.Deleted")
	}
}
