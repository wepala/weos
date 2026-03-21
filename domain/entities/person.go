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

// Person represents a person aggregate root.
// Ontology source: foaf:Person / schema:Person
type Person struct {
	*ddd.BaseEntity
	givenName  string
	familyName string
	email      string
	avatarURL  string
	status     string
	createdAt  time.Time
}

func (e *Person) With(givenName, familyName, email string) (*Person, error) {
	if givenName == "" {
		return nil, fmt.Errorf("givenName cannot be empty")
	}
	if familyName == "" {
		return nil, fmt.Errorf("familyName cannot be empty")
	}
	entityID := identity.NewPerson()
	e.BaseEntity = ddd.NewBaseEntity(entityID)
	e.givenName = givenName
	e.familyName = familyName
	e.email = email
	e.status = "active"
	e.createdAt = time.Now()

	event := new(PersonCreated).With(givenName, familyName, email)
	if err := e.BaseEntity.RecordEvent(event, event.EventType()); err != nil {
		return nil, fmt.Errorf("failed to record PersonCreated event: %w", err)
	}

	return e, nil
}

func (e *Person) GivenName() string    { return e.givenName }
func (e *Person) FamilyName() string   { return e.familyName }
func (e *Person) Name() string         { return e.givenName + " " + e.familyName }
func (e *Person) Email() string        { return e.email }
func (e *Person) AvatarURL() string    { return e.avatarURL }
func (e *Person) Status() string       { return e.status }
func (e *Person) CreatedAt() time.Time { return e.createdAt }

func (e *Person) Update(givenName, familyName, email, avatarURL, status string) error {
	e.givenName = givenName
	e.familyName = familyName
	e.email = email
	e.avatarURL = avatarURL
	e.status = status
	event := PersonUpdated{}.With(givenName, familyName, email, avatarURL, status)
	return e.BaseEntity.RecordEvent(event, event.EventType())
}

func (e *Person) MarkDeleted() error {
	e.status = "archived"
	event := PersonDeleted{}.With()
	return e.BaseEntity.RecordEvent(event, event.EventType())
}

func (e *Person) LinkToOrganization(
	ctx context.Context, orgID string, logger Logger,
) error {
	if orgID == "" {
		return fmt.Errorf("orgID cannot be empty")
	}
	event := PersonOrganizationLinked{}.With(e.GetID(), orgID)
	if err := e.BaseEntity.RecordEvent(event, event.EventType()); err != nil {
		return fmt.Errorf("failed to record PersonOrganizationLinked event: %w", err)
	}
	logger.Info(ctx, "person linked to organization",
		"personID", e.GetID(), "orgID", orgID)
	return nil
}

func (e *Person) Restore(
	id, givenName, familyName, email, avatarURL, status string,
	createdAt time.Time, sequenceNo int,
) error {
	if id == "" {
		return fmt.Errorf("id cannot be empty")
	}
	if givenName == "" {
		return fmt.Errorf("givenName cannot be empty")
	}
	e.BaseEntity = ddd.RestoreBaseEntity(id, sequenceNo)
	e.givenName = givenName
	e.familyName = familyName
	e.email = email
	e.avatarURL = avatarURL
	e.status = status
	e.createdAt = createdAt
	return nil
}

func (e *Person) ApplyEvent(
	ctx context.Context, envelope domain.EventEnvelope[any],
) error {
	if err := e.BaseEntity.ApplyEvent(ctx, envelope); err != nil {
		return fmt.Errorf("base entity apply event failed: %w", err)
	}

	switch payload := envelope.Payload.(type) {
	case PersonCreated:
		e.givenName = payload.GivenName
		e.familyName = payload.FamilyName
		e.email = payload.Email
		e.status = "active"
		e.createdAt = payload.Timestamp
		return nil
	case PersonUpdated:
		e.givenName = payload.GivenName
		e.familyName = payload.FamilyName
		e.email = payload.Email
		e.avatarURL = payload.AvatarURL
		e.status = payload.Status
		return nil
	case PersonDeleted:
		e.status = "archived"
		return nil
	case PersonOrganizationLinked:
		_ = payload
		return nil
	default:
		return fmt.Errorf("unknown event type: %T", envelope.Payload)
	}
}
