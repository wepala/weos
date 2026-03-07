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

// Template represents an HTML template within a theme.
// Ontology source: schema:WebPageElement (template component)
type Template struct {
	*ddd.BaseEntity
	name        string
	slug        string
	description string
	filePath    string
	status      string
	createdAt   time.Time
}

func (e *Template) With(name, slug, themeSlug string) (*Template, error) {
	if name == "" {
		return nil, fmt.Errorf("name cannot be empty")
	}
	if slug == "" {
		return nil, fmt.Errorf("slug cannot be empty")
	}
	if themeSlug == "" {
		return nil, fmt.Errorf("themeSlug cannot be empty")
	}

	entityID := identity.NewTemplate(themeSlug, slug)
	e.BaseEntity = ddd.NewBaseEntity(entityID)
	e.name = name
	e.slug = slug
	e.status = "draft"
	e.createdAt = time.Now()

	event := new(TemplateCreated).With(name, slug)
	if err := e.BaseEntity.RecordEvent(event, event.EventType()); err != nil {
		return nil, fmt.Errorf("failed to record TemplateCreated event: %w", err)
	}

	return e, nil
}

func (e *Template) Name() string        { return e.name }
func (e *Template) Slug() string        { return e.slug }
func (e *Template) Description() string { return e.description }
func (e *Template) FilePath() string    { return e.filePath }
func (e *Template) Status() string      { return e.status }
func (e *Template) CreatedAt() time.Time { return e.createdAt }

func (e *Template) LinkToTheme(
	ctx context.Context, themeID string, logger Logger,
) error {
	if themeID == "" {
		return fmt.Errorf("themeID cannot be empty")
	}
	event := TemplateThemeLinked{}.With(e.GetID(), themeID)
	if err := e.BaseEntity.RecordEvent(event, event.EventType()); err != nil {
		return fmt.Errorf("failed to record TemplateThemeLinked event: %w", err)
	}
	logger.Info(ctx, "template linked to theme",
		"templateID", e.GetID(), "themeID", themeID)
	return nil
}

func (e *Template) Restore(
	id, name, slug, description, filePath, status string,
	createdAt time.Time,
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
	e.description = description
	e.filePath = filePath
	e.status = status
	e.createdAt = createdAt
	return nil
}

func (e *Template) ApplyEvent(
	ctx context.Context, envelope domain.EventEnvelope[any],
) error {
	if err := e.BaseEntity.ApplyEvent(ctx, envelope); err != nil {
		return fmt.Errorf("base entity apply event failed: %w", err)
	}

	switch payload := envelope.Payload.(type) {
	case TemplateCreated:
		e.name = payload.Name
		e.slug = payload.Slug
		e.status = "draft"
		e.createdAt = payload.Timestamp
		return nil
	case TemplateUpdated:
		e.name = payload.Name
		e.slug = payload.Slug
		e.description = payload.Description
		e.filePath = payload.FilePath
		e.status = payload.Status
		return nil
	case TemplateDeleted:
		e.status = "archived"
		return nil
	case TemplateThemeLinked:
		_ = payload
		return nil
	default:
		return fmt.Errorf("unknown event type: %T", envelope.Payload)
	}
}
