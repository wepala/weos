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

type SectionCreated struct {
	Name      string
	Slot      string
	Timestamp time.Time
}

func (e *SectionCreated) With(name, slot string) SectionCreated {
	return SectionCreated{
		Name:      name,
		Slot:      slot,
		Timestamp: time.Now(),
	}
}

func (e SectionCreated) EventType() string {
	return "Section.Created"
}

type SectionUpdated struct {
	Name       string
	Slot       string
	EntityType string
	Content    string
	Position   int
	Timestamp  time.Time
}

func (e SectionUpdated) With(
	name, slot, entityType, content string, position int,
) SectionUpdated {
	return SectionUpdated{
		Name:       name,
		Slot:       slot,
		EntityType: entityType,
		Content:    content,
		Position:   position,
		Timestamp:  time.Now(),
	}
}

func (e SectionUpdated) EventType() string {
	return "Section.Updated"
}

type SectionDeleted struct {
	Timestamp time.Time
}

func (e SectionDeleted) With() SectionDeleted {
	return SectionDeleted{Timestamp: time.Now()}
}

func (e SectionDeleted) EventType() string {
	return "Section.Deleted"
}

type SectionPageLinked struct {
	domain.BasicTripleEvent
	Timestamp time.Time
}

func (e SectionPageLinked) With(
	sectionID, pageID string,
) SectionPageLinked {
	return SectionPageLinked{
		BasicTripleEvent: domain.BasicTripleEvent{
			Subject:   sectionID,
			Predicate: PredicateBelongsTo,
			Object:    pageID,
		},
		Timestamp: time.Now(),
	}
}

func (e SectionPageLinked) EventType() string {
	return "Section.PageLinked"
}

const SectionEventPattern = "Section.%"
