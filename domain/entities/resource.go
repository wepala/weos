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

// Resource represents a JSON-LD resource instance of a ResourceType.
// Ontology source: rdfs:Resource
type Resource struct {
	*ddd.BaseEntity
	typeSlug  string
	data      json.RawMessage
	status    string
	createdAt time.Time
}

func (e *Resource) With(
	typeSlug string, data, ldContext json.RawMessage, typeName string,
) (*Resource, error) {
	if typeSlug == "" {
		return nil, fmt.Errorf("type slug cannot be empty")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	entityID := identity.NewResource(typeSlug)
	e.BaseEntity = ddd.NewBaseEntity(entityID)
	e.typeSlug = typeSlug
	e.status = "active"
	e.createdAt = time.Now()

	enriched, err := injectJSONLD(data, entityID, typeName, ldContext)
	if err != nil {
		return nil, fmt.Errorf("failed to inject JSON-LD fields: %w", err)
	}
	e.data = enriched

	event := new(ResourceCreated).With(typeSlug, e.data)
	if err := e.BaseEntity.RecordEvent(event, event.EventType()); err != nil {
		return nil, fmt.Errorf("failed to record ResourceCreated event: %w", err)
	}

	return e, nil
}

func (e *Resource) Update(data json.RawMessage) error {
	e.data = data
	event := ResourceUpdated{}.With(data)
	return e.BaseEntity.RecordEvent(event, event.EventType())
}

func (e *Resource) MarkDeleted() error {
	e.status = "archived"
	event := ResourceDeleted{}.With()
	return e.BaseEntity.RecordEvent(event, event.EventType())
}

func (e *Resource) TypeSlug() string      { return e.typeSlug }
func (e *Resource) Data() json.RawMessage { return e.data }
func (e *Resource) Status() string        { return e.status }
func (e *Resource) CreatedAt() time.Time  { return e.createdAt }

func (e *Resource) Restore(
	id, typeSlug, status string,
	data json.RawMessage,
	createdAt time.Time, sequenceNo int,
) error {
	if id == "" {
		return fmt.Errorf("id cannot be empty")
	}
	e.BaseEntity = ddd.RestoreBaseEntity(id, sequenceNo)
	e.typeSlug = typeSlug
	e.data = data
	e.status = status
	e.createdAt = createdAt
	return nil
}

func (e *Resource) ApplyEvent(
	ctx context.Context, envelope domain.EventEnvelope[any],
) error {
	if err := e.BaseEntity.ApplyEvent(ctx, envelope); err != nil {
		return fmt.Errorf("base entity apply event failed: %w", err)
	}

	switch payload := envelope.Payload.(type) {
	case ResourceCreated:
		e.typeSlug = payload.TypeSlug
		e.data = payload.Data
		e.status = "active"
		e.createdAt = payload.Timestamp
		return nil
	case ResourceUpdated:
		e.data = payload.Data
		return nil
	case ResourceDeleted:
		e.status = "archived"
		return nil
	default:
		return fmt.Errorf("unknown event type: %T", envelope.Payload)
	}
}

// injectJSONLD parses data, injects @id, @type, and @context, then re-marshals.
func injectJSONLD(
	data json.RawMessage, id, typeName string, ldContext json.RawMessage,
) (json.RawMessage, error) {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("data must be a JSON object: %w", err)
	}
	m["@id"] = id
	m["@type"] = typeName
	if len(ldContext) > 0 {
		var ctxVal interface{}
		if err := json.Unmarshal(ldContext, &ctxVal); err == nil {
			m["@context"] = ctxVal
		}
	}
	return json.Marshal(m)
}

// InjectJSONLDForUpdate is the exported version of injectJSONLD for use in service updates.
func InjectJSONLDForUpdate(
	data json.RawMessage, id, typeName string, ldContext json.RawMessage,
) (json.RawMessage, error) {
	return injectJSONLD(data, id, typeName, ldContext)
}

// SimplifyJSONLD converts JSON-LD data to plain JSON by mapping @id→id, @type→type,
// and removing @context.
func SimplifyJSONLD(data json.RawMessage) (json.RawMessage, error) {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return data, err
	}
	if v, ok := m["@id"]; ok {
		m["id"] = v
		delete(m, "@id")
	}
	if v, ok := m["@type"]; ok {
		m["type"] = v
		delete(m, "@type")
	}
	delete(m, "@context")
	return json.Marshal(m)
}
