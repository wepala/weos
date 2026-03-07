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

// Website represents a website aggregate root.
// Ontology source: schema:WebSite
type Website struct {
	*ddd.BaseEntity
	name        string
	slug        string
	url         string
	description string
	language    string
	status      string
	createdAt   time.Time
}

func (e *Website) With(name, url, slug string) (*Website, error) {
	if name == "" {
		return nil, fmt.Errorf("name cannot be empty")
	}
	if url == "" {
		return nil, fmt.Errorf("url cannot be empty")
	}
	if slug == "" {
		return nil, fmt.Errorf("slug cannot be empty")
	}

	entityID := identity.NewWebsite(slug)
	e.BaseEntity = ddd.NewBaseEntity(entityID)
	e.name = name
	e.slug = slug
	e.url = url
	e.language = "en"
	e.status = "draft"
	e.createdAt = time.Now()

	event := new(WebsiteCreated).With(name, url, slug)
	if err := e.BaseEntity.RecordEvent(event, event.EventType()); err != nil {
		return nil, fmt.Errorf("failed to record WebsiteCreated event: %w", err)
	}

	return e, nil
}

func (e *Website) Name() string        { return e.name }
func (e *Website) Slug() string        { return e.slug }
func (e *Website) URL() string         { return e.url }
func (e *Website) Description() string { return e.description }
func (e *Website) Language() string     { return e.language }
func (e *Website) Status() string      { return e.status }
func (e *Website) CreatedAt() time.Time { return e.createdAt }

func (e *Website) Restore(
	id, name, slug, url, description, language, status string, createdAt time.Time,
) error {
	if id == "" {
		return fmt.Errorf("id cannot be empty")
	}
	if name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	e.BaseEntity = ddd.NewBaseEntity(id)
	e.name = name
	e.slug = slug
	e.url = url
	e.description = description
	e.language = language
	e.status = status
	e.createdAt = createdAt
	return nil
}

func (e *Website) ApplyEvent(
	ctx context.Context, envelope domain.EventEnvelope[any],
) error {
	if err := e.BaseEntity.ApplyEvent(ctx, envelope); err != nil {
		return fmt.Errorf("base entity apply event failed: %w", err)
	}

	switch payload := envelope.Payload.(type) {
	case WebsiteCreated:
		e.name = payload.Name
		e.slug = payload.Slug
		e.url = payload.URL
		e.language = "en"
		e.status = "draft"
		e.createdAt = payload.Timestamp
		return nil
	case WebsiteUpdated:
		e.name = payload.Name
		e.url = payload.URL
		e.slug = payload.Slug
		e.description = payload.Description
		e.language = payload.Language
		e.status = payload.Status
		return nil
	case WebsiteDeleted:
		e.status = "archived"
		return nil
	default:
		return fmt.Errorf("unknown event type: %T", envelope.Payload)
	}
}
