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
	"fmt"
	"time"

	"weos/pkg/identity"

	"github.com/akeemphilbert/pericarp/pkg/ddd"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// ResourceType defines a type of resource with its JSON-LD context and optional JSON Schema.
// Ontology source: rdfs:Class
type ResourceType struct {
	*ddd.BaseEntity
	name        string
	slug        string
	description string
	context     json.RawMessage
	schema      json.RawMessage
	status      string
	createdAt   time.Time
}

func (e *ResourceType) With(
	name, slug, description string, ctx, schema json.RawMessage,
) (*ResourceType, error) {
	if name == "" {
		return nil, fmt.Errorf("name cannot be empty")
	}
	if slug == "" {
		return nil, fmt.Errorf("slug cannot be empty")
	}

	entityID := identity.NewResourceType(slug)
	e.BaseEntity = ddd.NewBaseEntity(entityID)
	e.name = name
	e.slug = slug
	e.description = description
	e.context = ctx
	e.schema = schema
	e.status = "active"
	e.createdAt = time.Now()

	event := new(ResourceTypeCreated).With(name, slug, description, ctx, schema)
	if err := e.BaseEntity.RecordEvent(event, event.EventType()); err != nil {
		return nil, fmt.Errorf("failed to record ResourceTypeCreated event: %w", err)
	}

	return e, nil
}

func (e *ResourceType) Name() string             { return e.name }
func (e *ResourceType) Slug() string             { return e.slug }
func (e *ResourceType) Description() string      { return e.description }
func (e *ResourceType) Context() json.RawMessage { return e.context }
func (e *ResourceType) Schema() json.RawMessage  { return e.schema }
func (e *ResourceType) Status() string           { return e.status }
func (e *ResourceType) CreatedAt() time.Time     { return e.createdAt }

func (e *ResourceType) Update(
	name, slug, description, status string, ctx, schema json.RawMessage,
) error {
	e.name = name
	e.slug = slug
	e.description = description
	e.context = ctx
	e.schema = schema
	e.status = status
	event := ResourceTypeUpdated{}.With(name, slug, description, status, ctx, schema)
	return e.BaseEntity.RecordEvent(event, event.EventType())
}

func (e *ResourceType) MarkDeleted() error {
	e.status = "archived"
	event := ResourceTypeDeleted{}.With()
	return e.BaseEntity.RecordEvent(event, event.EventType())
}

func (e *ResourceType) Restore(
	id, name, slug, description, status string,
	ctx, schema json.RawMessage,
	createdAt time.Time, sequenceNo int,
) error {
	if id == "" {
		return fmt.Errorf("id cannot be empty")
	}
	if name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	e.BaseEntity = ddd.RestoreBaseEntity(id, sequenceNo)
	e.name = name
	e.slug = slug
	e.description = description
	e.context = ctx
	e.schema = schema
	e.status = status
	e.createdAt = createdAt
	return nil
}

func (e *ResourceType) ApplyEvent(
	ctx context.Context, envelope domain.EventEnvelope[any],
) error {
	if err := e.BaseEntity.ApplyEvent(ctx, envelope); err != nil {
		return fmt.Errorf("base entity apply event failed: %w", err)
	}

	switch payload := envelope.Payload.(type) {
	case ResourceTypeCreated:
		e.name = payload.Name
		e.slug = payload.Slug
		e.description = payload.Description
		e.context = payload.Context
		e.schema = payload.Schema
		e.status = "active"
		e.createdAt = payload.Timestamp
		return nil
	case ResourceTypeUpdated:
		e.name = payload.Name
		e.slug = payload.Slug
		e.description = payload.Description
		e.context = payload.Context
		e.schema = payload.Schema
		e.status = payload.Status
		return nil
	case ResourceTypeDeleted:
		e.status = "archived"
		return nil
	default:
		return fmt.Errorf("unknown event type: %T", envelope.Payload)
	}
}
