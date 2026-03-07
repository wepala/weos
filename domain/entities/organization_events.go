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

type OrganizationCreated struct {
	Name      string
	Slug      string
	Timestamp time.Time
}

func (e *OrganizationCreated) With(name, slug string) OrganizationCreated {
	return OrganizationCreated{
		Name:      name,
		Slug:      slug,
		Timestamp: time.Now(),
	}
}

func (e OrganizationCreated) EventType() string {
	return "Organization.Created"
}

type OrganizationUpdated struct {
	Name        string
	Slug        string
	Description string
	URL         string
	LogoURL     string
	Status      string
	Timestamp   time.Time
}

func (e OrganizationUpdated) With(
	name, slug, description, url, logoURL, status string,
) OrganizationUpdated {
	return OrganizationUpdated{
		Name:        name,
		Slug:        slug,
		Description: description,
		URL:         url,
		LogoURL:     logoURL,
		Status:      status,
		Timestamp:   time.Now(),
	}
}

func (e OrganizationUpdated) EventType() string {
	return "Organization.Updated"
}

type OrganizationDeleted struct {
	Timestamp time.Time
}

func (e OrganizationDeleted) With() OrganizationDeleted {
	return OrganizationDeleted{Timestamp: time.Now()}
}

func (e OrganizationDeleted) EventType() string {
	return "Organization.Deleted"
}

const OrganizationEventPattern = "Organization.%"
