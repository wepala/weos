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
	"time"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

const PredicateBelongsTo = "belongsTo"

type PageCreated struct {
	Name      string
	Slug      string
	Timestamp time.Time
}

func (e *PageCreated) With(name, slug string) PageCreated {
	return PageCreated{
		Name:      name,
		Slug:      slug,
		Timestamp: time.Now(),
	}
}

func (e PageCreated) EventType() string {
	return "Page.Created"
}

type PageUpdated struct {
	Name        string
	Slug        string
	Description string
	Template    string
	Position    int
	Status      string
	Timestamp   time.Time
}

func (e PageUpdated) With(
	name, slug, description, template, status string, position int,
) PageUpdated {
	return PageUpdated{
		Name:        name,
		Slug:        slug,
		Description: description,
		Template:    template,
		Position:    position,
		Status:      status,
		Timestamp:   time.Now(),
	}
}

func (e PageUpdated) EventType() string {
	return "Page.Updated"
}

type PageDeleted struct {
	Timestamp time.Time
}

func (e PageDeleted) With() PageDeleted {
	return PageDeleted{Timestamp: time.Now()}
}

func (e PageDeleted) EventType() string {
	return "Page.Deleted"
}

type PageWebsiteLinked struct {
	domain.BasicTripleEvent
	Timestamp time.Time
}

func (e PageWebsiteLinked) With(
	pageID, websiteID string,
) PageWebsiteLinked {
	return PageWebsiteLinked{
		BasicTripleEvent: domain.BasicTripleEvent{
			Subject:   pageID,
			Predicate: PredicateBelongsTo,
			Object:    websiteID,
		},
		Timestamp: time.Now(),
	}
}

func (e PageWebsiteLinked) EventType() string {
	return "Page.WebsiteLinked"
}

const PageEventPattern = "Page.%"
