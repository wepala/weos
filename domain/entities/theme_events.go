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

const (
	PredicateAppliedTo  = "appliedTo"
	PredicateAuthoredBy = "authoredBy"
)

type ThemeCreated struct {
	Name      string
	Slug      string
	Timestamp time.Time
}

func (e *ThemeCreated) With(name, slug string) ThemeCreated {
	return ThemeCreated{
		Name:      name,
		Slug:      slug,
		Timestamp: time.Now(),
	}
}

func (e ThemeCreated) EventType() string {
	return "Theme.Created"
}

type ThemeUpdated struct {
	Name         string
	Slug         string
	Description  string
	Version      string
	ThumbnailURL string
	Status       string
	Timestamp    time.Time
}

func (e ThemeUpdated) With(
	name, slug, description, version, thumbnailURL, status string,
) ThemeUpdated {
	return ThemeUpdated{
		Name:         name,
		Slug:         slug,
		Description:  description,
		Version:      version,
		ThumbnailURL: thumbnailURL,
		Status:       status,
		Timestamp:    time.Now(),
	}
}

func (e ThemeUpdated) EventType() string {
	return "Theme.Updated"
}

type ThemeDeleted struct {
	Timestamp time.Time
}

func (e ThemeDeleted) With() ThemeDeleted {
	return ThemeDeleted{Timestamp: time.Now()}
}

func (e ThemeDeleted) EventType() string {
	return "Theme.Deleted"
}

type ThemeWebsiteLinked struct {
	domain.BasicTripleEvent
	Timestamp time.Time
}

func (e ThemeWebsiteLinked) With(
	themeID, websiteID string,
) ThemeWebsiteLinked {
	return ThemeWebsiteLinked{
		BasicTripleEvent: domain.BasicTripleEvent{
			Subject:   themeID,
			Predicate: PredicateAppliedTo,
			Object:    websiteID,
		},
		Timestamp: time.Now(),
	}
}

func (e ThemeWebsiteLinked) EventType() string {
	return "Theme.WebsiteLinked"
}

type ThemeAuthorLinked struct {
	domain.BasicTripleEvent
	Timestamp time.Time
}

func (e ThemeAuthorLinked) With(
	themeID, authorID string,
) ThemeAuthorLinked {
	return ThemeAuthorLinked{
		BasicTripleEvent: domain.BasicTripleEvent{
			Subject:   themeID,
			Predicate: PredicateAuthoredBy,
			Object:    authorID,
		},
		Timestamp: time.Now(),
	}
}

func (e ThemeAuthorLinked) EventType() string {
	return "Theme.AuthorLinked"
}

const ThemeEventPattern = "Theme.%"
