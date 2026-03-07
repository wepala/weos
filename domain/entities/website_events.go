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

import "time"

type WebsiteCreated struct {
	Name      string
	URL       string
	Slug      string
	Timestamp time.Time
}

func (e *WebsiteCreated) With(name, url, slug string) WebsiteCreated {
	return WebsiteCreated{
		Name:      name,
		URL:       url,
		Slug:      slug,
		Timestamp: time.Now(),
	}
}

func (e WebsiteCreated) EventType() string {
	return "Website.Created"
}

type WebsiteUpdated struct {
	Name        string
	URL         string
	Slug        string
	Description string
	Language    string
	Status      string
	Timestamp   time.Time
}

func (e WebsiteUpdated) With(
	name, url, slug, description, language, status string,
) WebsiteUpdated {
	return WebsiteUpdated{
		Name:        name,
		URL:         url,
		Slug:        slug,
		Description: description,
		Language:    language,
		Status:      status,
		Timestamp:   time.Now(),
	}
}

func (e WebsiteUpdated) EventType() string {
	return "Website.Updated"
}

type WebsiteDeleted struct {
	Timestamp time.Time
}

func (e WebsiteDeleted) With() WebsiteDeleted {
	return WebsiteDeleted{Timestamp: time.Now()}
}

func (e WebsiteDeleted) EventType() string {
	return "Website.Deleted"
}

const WebsiteEventPattern = "Website.%"
