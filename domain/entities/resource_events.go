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

type ResourceCreated struct {
	TypeSlug  string
	Data      json.RawMessage
	Timestamp time.Time
}

func (e *ResourceCreated) With(typeSlug string, data json.RawMessage) ResourceCreated {
	return ResourceCreated{
		TypeSlug:  typeSlug,
		Data:      data,
		Timestamp: time.Now(),
	}
}

func (e ResourceCreated) EventType() string {
	return "Resource.Created"
}

type ResourceUpdated struct {
	Data      json.RawMessage
	Timestamp time.Time
}

func (e ResourceUpdated) With(data json.RawMessage) ResourceUpdated {
	return ResourceUpdated{
		Data:      data,
		Timestamp: time.Now(),
	}
}

func (e ResourceUpdated) EventType() string {
	return "Resource.Updated"
}

type ResourceDeleted struct {
	Timestamp time.Time
}

func (e ResourceDeleted) With() ResourceDeleted {
	return ResourceDeleted{Timestamp: time.Now()}
}

func (e ResourceDeleted) EventType() string {
	return "Resource.Deleted"
}

const ResourceEventPattern = "Resource.%"
