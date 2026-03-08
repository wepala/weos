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
	"encoding/json"
	"time"
)

type ResourceTypeCreated struct {
	Name        string
	Slug        string
	Description string
	Context     json.RawMessage
	Schema      json.RawMessage
	Timestamp   time.Time
}

func (e *ResourceTypeCreated) With(
	name, slug, description string, ctx, schema json.RawMessage,
) ResourceTypeCreated {
	return ResourceTypeCreated{
		Name:        name,
		Slug:        slug,
		Description: description,
		Context:     ctx,
		Schema:      schema,
		Timestamp:   time.Now(),
	}
}

func (e ResourceTypeCreated) EventType() string {
	return "ResourceType.Created"
}

type ResourceTypeUpdated struct {
	Name        string
	Slug        string
	Description string
	Context     json.RawMessage
	Schema      json.RawMessage
	Status      string
	Timestamp   time.Time
}

func (e ResourceTypeUpdated) With(
	name, slug, description, status string,
	ctx, schema json.RawMessage,
) ResourceTypeUpdated {
	return ResourceTypeUpdated{
		Name:        name,
		Slug:        slug,
		Description: description,
		Context:     ctx,
		Schema:      schema,
		Status:      status,
		Timestamp:   time.Now(),
	}
}

func (e ResourceTypeUpdated) EventType() string {
	return "ResourceType.Updated"
}

type ResourceTypeDeleted struct {
	Timestamp time.Time
}

func (e ResourceTypeDeleted) With() ResourceTypeDeleted {
	return ResourceTypeDeleted{Timestamp: time.Now()}
}

func (e ResourceTypeDeleted) EventType() string {
	return "ResourceType.Deleted"
}

const ResourceTypeEventPattern = "ResourceType.%"
