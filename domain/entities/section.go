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
	"fmt"
	"time"

	"weos/pkg/identity"

	"github.com/akeemphilbert/pericarp/pkg/ddd"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// Section represents a content section within a page.
// Ontology source: schema:WebPageElement
type Section struct {
	*ddd.BaseEntity
	name       string
	slot       string
	entityType string
	content    string
	position   int
	createdAt  time.Time
}

func (e *Section) With(name, slot, websiteSlug, pageSlug string) (*Section, error) {
	if name == "" {
		return nil, fmt.Errorf("name cannot be empty")
	}
	if slot == "" {
		return nil, fmt.Errorf("slot cannot be empty")
	}
	if websiteSlug == "" {
		return nil, fmt.Errorf("websiteSlug cannot be empty")
	}
	if pageSlug == "" {
		return nil, fmt.Errorf("pageSlug cannot be empty")
	}

	entityID := identity.NewSection(websiteSlug, pageSlug)
	e.BaseEntity = ddd.NewBaseEntity(entityID)
	e.name = name
	e.slot = slot
	e.createdAt = time.Now()

	event := new(SectionCreated).With(name, slot)
	if err := e.BaseEntity.RecordEvent(event, event.EventType()); err != nil {
		return nil, fmt.Errorf("failed to record SectionCreated event: %w", err)
	}

	return e, nil
}

func (e *Section) Name() string        { return e.name }
func (e *Section) Slot() string        { return e.slot }
func (e *Section) EntityType() string  { return e.entityType }
func (e *Section) Content() string     { return e.content }
func (e *Section) Position() int       { return e.position }
func (e *Section) CreatedAt() time.Time { return e.createdAt }

func (e *Section) LinkToPage(
	ctx context.Context, pageID string, logger Logger,
) error {
	if pageID == "" {
		return fmt.Errorf("pageID cannot be empty")
	}
	event := SectionPageLinked{}.With(e.GetID(), pageID)
	if err := e.BaseEntity.RecordEvent(event, event.EventType()); err != nil {
		return fmt.Errorf("failed to record SectionPageLinked event: %w", err)
	}
	logger.Info(ctx, "section linked to page",
		"sectionID", e.GetID(), "pageID", pageID)
	return nil
}

func (e *Section) Restore(
	id, name, slot, entityType, content string,
	position int, createdAt time.Time,
) error {
	if id == "" {
		return fmt.Errorf("id cannot be empty")
	}
	if name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	e.BaseEntity = ddd.NewBaseEntity(id)
	e.name = name
	e.slot = slot
	e.entityType = entityType
	e.content = content
	e.position = position
	e.createdAt = createdAt
	return nil
}

func (e *Section) ApplyEvent(
	ctx context.Context, envelope domain.EventEnvelope[any],
) error {
	if err := e.BaseEntity.ApplyEvent(ctx, envelope); err != nil {
		return fmt.Errorf("base entity apply event failed: %w", err)
	}

	switch payload := envelope.Payload.(type) {
	case SectionCreated:
		e.name = payload.Name
		e.slot = payload.Slot
		e.createdAt = payload.Timestamp
		return nil
	case SectionUpdated:
		e.name = payload.Name
		e.slot = payload.Slot
		e.entityType = payload.EntityType
		e.content = payload.Content
		e.position = payload.Position
		return nil
	case SectionDeleted:
		return nil
	case SectionPageLinked:
		_ = payload
		return nil
	default:
		return fmt.Errorf("unknown event type: %T", envelope.Payload)
	}
}
