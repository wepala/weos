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

// Organization represents an organization aggregate root.
// Ontology source: org:Organization / schema:Organization
type Organization struct {
	*ddd.BaseEntity
	name        string
	slug        string
	description string
	url         string
	logoURL     string
	status      string
	createdAt   time.Time
}

func (e *Organization) With(name, slug string) (*Organization, error) {
	if name == "" {
		return nil, fmt.Errorf("name cannot be empty")
	}
	if slug == "" {
		return nil, fmt.Errorf("slug cannot be empty")
	}

	entityID := identity.NewOrganization(slug)
	e.BaseEntity = ddd.NewBaseEntity(entityID)
	e.name = name
	e.slug = slug
	e.status = "active"
	e.createdAt = time.Now()

	event := new(OrganizationCreated).With(name, slug)
	if err := e.BaseEntity.RecordEvent(event, event.EventType()); err != nil {
		return nil, fmt.Errorf("failed to record OrganizationCreated event: %w", err)
	}

	return e, nil
}

func (e *Organization) Name() string         { return e.name }
func (e *Organization) Slug() string         { return e.slug }
func (e *Organization) Description() string  { return e.description }
func (e *Organization) URL() string          { return e.url }
func (e *Organization) LogoURL() string      { return e.logoURL }
func (e *Organization) Status() string       { return e.status }
func (e *Organization) CreatedAt() time.Time { return e.createdAt }

func (e *Organization) Update(
	name, slug, description, url, logoURL, status string,
) error {
	e.name = name
	e.slug = slug
	e.description = description
	e.url = url
	e.logoURL = logoURL
	e.status = status
	event := OrganizationUpdated{}.With(name, slug, description, url, logoURL, status)
	return e.BaseEntity.RecordEvent(event, event.EventType())
}

func (e *Organization) MarkDeleted() error {
	e.status = "archived"
	event := OrganizationDeleted{}.With()
	return e.BaseEntity.RecordEvent(event, event.EventType())
}

func (e *Organization) Restore(
	id, name, slug, description, url, logoURL, status string,
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
	e.url = url
	e.logoURL = logoURL
	e.status = status
	e.createdAt = createdAt
	return nil
}

func (e *Organization) ApplyEvent(
	ctx context.Context, envelope domain.EventEnvelope[any],
) error {
	if err := e.BaseEntity.ApplyEvent(ctx, envelope); err != nil {
		return fmt.Errorf("base entity apply event failed: %w", err)
	}

	switch payload := envelope.Payload.(type) {
	case OrganizationCreated:
		e.name = payload.Name
		e.slug = payload.Slug
		e.status = "active"
		e.createdAt = payload.Timestamp
		return nil
	case OrganizationUpdated:
		e.name = payload.Name
		e.slug = payload.Slug
		e.description = payload.Description
		e.url = payload.URL
		e.logoURL = payload.LogoURL
		e.status = payload.Status
		return nil
	case OrganizationDeleted:
		e.status = "archived"
		return nil
	default:
		return fmt.Errorf("unknown event type: %T", envelope.Payload)
	}
}
