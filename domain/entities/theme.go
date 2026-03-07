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

// Theme represents a theme aggregate root.
// Ontology source: schema:WebSite (visual theme/skin)
type Theme struct {
	*ddd.BaseEntity
	name         string
	slug         string
	description  string
	version      string
	thumbnailURL string
	status       string
	createdAt    time.Time
}

func (e *Theme) With(name, slug string) (*Theme, error) {
	if name == "" {
		return nil, fmt.Errorf("name cannot be empty")
	}
	if slug == "" {
		return nil, fmt.Errorf("slug cannot be empty")
	}

	entityID := identity.NewTheme(slug)
	e.BaseEntity = ddd.NewBaseEntity(entityID)
	e.name = name
	e.slug = slug
	e.status = "draft"
	e.createdAt = time.Now()

	event := new(ThemeCreated).With(name, slug)
	if err := e.BaseEntity.RecordEvent(event, event.EventType()); err != nil {
		return nil, fmt.Errorf("failed to record ThemeCreated event: %w", err)
	}

	return e, nil
}

func (e *Theme) Name() string         { return e.name }
func (e *Theme) Slug() string         { return e.slug }
func (e *Theme) Description() string  { return e.description }
func (e *Theme) Version() string      { return e.version }
func (e *Theme) ThumbnailURL() string { return e.thumbnailURL }
func (e *Theme) Status() string       { return e.status }
func (e *Theme) CreatedAt() time.Time { return e.createdAt }

func (e *Theme) LinkToWebsite(
	ctx context.Context, websiteID string, logger Logger,
) error {
	if websiteID == "" {
		return fmt.Errorf("websiteID cannot be empty")
	}
	event := ThemeWebsiteLinked{}.With(e.GetID(), websiteID)
	if err := e.BaseEntity.RecordEvent(event, event.EventType()); err != nil {
		return fmt.Errorf("failed to record ThemeWebsiteLinked event: %w", err)
	}
	logger.Info(ctx, "theme linked to website",
		"themeID", e.GetID(), "websiteID", websiteID)
	return nil
}

func (e *Theme) LinkToAuthor(
	ctx context.Context, authorID string, logger Logger,
) error {
	if authorID == "" {
		return fmt.Errorf("authorID cannot be empty")
	}
	event := ThemeAuthorLinked{}.With(e.GetID(), authorID)
	if err := e.BaseEntity.RecordEvent(event, event.EventType()); err != nil {
		return fmt.Errorf("failed to record ThemeAuthorLinked event: %w", err)
	}
	logger.Info(ctx, "theme linked to author",
		"themeID", e.GetID(), "authorID", authorID)
	return nil
}

func (e *Theme) Restore(
	id, name, slug, description, version, thumbnailURL, status string,
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
	e.version = version
	e.thumbnailURL = thumbnailURL
	e.status = status
	e.createdAt = createdAt
	return nil
}

func (e *Theme) ApplyEvent(
	ctx context.Context, envelope domain.EventEnvelope[any],
) error {
	if err := e.BaseEntity.ApplyEvent(ctx, envelope); err != nil {
		return fmt.Errorf("base entity apply event failed: %w", err)
	}

	switch payload := envelope.Payload.(type) {
	case ThemeCreated:
		e.name = payload.Name
		e.slug = payload.Slug
		e.status = "draft"
		e.createdAt = payload.Timestamp
		return nil
	case ThemeUpdated:
		e.name = payload.Name
		e.slug = payload.Slug
		e.description = payload.Description
		e.version = payload.Version
		e.thumbnailURL = payload.ThumbnailURL
		e.status = payload.Status
		return nil
	case ThemeDeleted:
		e.status = "archived"
		return nil
	case ThemeWebsiteLinked:
		_ = payload
		return nil
	case ThemeAuthorLinked:
		_ = payload
		return nil
	default:
		return fmt.Errorf("unknown event type: %T", envelope.Payload)
	}
}
