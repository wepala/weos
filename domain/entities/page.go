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

// Page represents a web page within a website.
// Ontology source: schema:WebPage
type Page struct {
	*ddd.BaseEntity
	name        string
	slug        string
	description string
	template    string
	position    int
	status      string
	createdAt   time.Time
}

func (e *Page) With(name, slug, websiteSlug string) (*Page, error) {
	if name == "" {
		return nil, fmt.Errorf("name cannot be empty")
	}
	if slug == "" {
		return nil, fmt.Errorf("slug cannot be empty")
	}
	if websiteSlug == "" {
		return nil, fmt.Errorf("websiteSlug cannot be empty")
	}

	entityID := identity.NewPage(websiteSlug, slug)
	e.BaseEntity = ddd.NewBaseEntity(entityID)
	e.name = name
	e.slug = slug
	e.status = "draft"
	e.createdAt = time.Now()

	event := new(PageCreated).With(name, slug)
	if err := e.BaseEntity.RecordEvent(event, event.EventType()); err != nil {
		return nil, fmt.Errorf("failed to record PageCreated event: %w", err)
	}

	return e, nil
}

func (e *Page) Name() string        { return e.name }
func (e *Page) Slug() string        { return e.slug }
func (e *Page) Description() string { return e.description }
func (e *Page) Template() string    { return e.template }
func (e *Page) Position() int       { return e.position }
func (e *Page) Status() string      { return e.status }
func (e *Page) CreatedAt() time.Time { return e.createdAt }

func (e *Page) LinkToWebsite(
	ctx context.Context, websiteID string, logger Logger,
) error {
	if websiteID == "" {
		return fmt.Errorf("websiteID cannot be empty")
	}
	event := PageWebsiteLinked{}.With(e.GetID(), websiteID)
	if err := e.BaseEntity.RecordEvent(event, event.EventType()); err != nil {
		return fmt.Errorf("failed to record PageWebsiteLinked event: %w", err)
	}
	logger.Info(ctx, "page linked to website",
		"pageID", e.GetID(), "websiteID", websiteID)
	return nil
}

func (e *Page) Restore(
	id, name, slug, description, template, status string,
	position int, createdAt time.Time, sequenceNo int,
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
	e.template = template
	e.position = position
	e.status = status
	e.createdAt = createdAt
	return nil
}

func (e *Page) ApplyEvent(
	ctx context.Context, envelope domain.EventEnvelope[any],
) error {
	if err := e.BaseEntity.ApplyEvent(ctx, envelope); err != nil {
		return fmt.Errorf("base entity apply event failed: %w", err)
	}

	switch payload := envelope.Payload.(type) {
	case PageCreated:
		e.name = payload.Name
		e.slug = payload.Slug
		e.status = "draft"
		e.createdAt = payload.Timestamp
		return nil
	case PageUpdated:
		e.name = payload.Name
		e.slug = payload.Slug
		e.description = payload.Description
		e.template = payload.Template
		e.position = payload.Position
		e.status = payload.Status
		return nil
	case PageDeleted:
		e.status = "archived"
		return nil
	case PageWebsiteLinked:
		_ = payload
		return nil
	default:
		return fmt.Errorf("unknown event type: %T", envelope.Payload)
	}
}
