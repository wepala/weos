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

type TemplateCreated struct {
	Name      string
	Slug      string
	Timestamp time.Time
}

func (e *TemplateCreated) With(name, slug string) TemplateCreated {
	return TemplateCreated{
		Name:      name,
		Slug:      slug,
		Timestamp: time.Now(),
	}
}

func (e TemplateCreated) EventType() string {
	return "Template.Created"
}

type TemplateUpdated struct {
	Name        string
	Slug        string
	Description string
	FilePath    string
	Status      string
	Timestamp   time.Time
}

func (e TemplateUpdated) With(
	name, slug, description, filePath, status string,
) TemplateUpdated {
	return TemplateUpdated{
		Name:        name,
		Slug:        slug,
		Description: description,
		FilePath:    filePath,
		Status:      status,
		Timestamp:   time.Now(),
	}
}

func (e TemplateUpdated) EventType() string {
	return "Template.Updated"
}

type TemplateDeleted struct {
	Timestamp time.Time
}

func (e TemplateDeleted) With() TemplateDeleted {
	return TemplateDeleted{Timestamp: time.Now()}
}

func (e TemplateDeleted) EventType() string {
	return "Template.Deleted"
}

type TemplateThemeLinked struct {
	domain.BasicTripleEvent
	Timestamp time.Time
}

func (e TemplateThemeLinked) With(
	templateID, themeID string,
) TemplateThemeLinked {
	return TemplateThemeLinked{
		BasicTripleEvent: domain.BasicTripleEvent{
			Subject:   templateID,
			Predicate: PredicateBelongsTo,
			Object:    themeID,
		},
		Timestamp: time.Now(),
	}
}

func (e TemplateThemeLinked) EventType() string {
	return "Template.ThemeLinked"
}

const TemplateEventPattern = "Template.%"
