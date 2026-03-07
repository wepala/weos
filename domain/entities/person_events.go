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

const PredicateMemberOf = "memberOf"

type PersonCreated struct {
	GivenName  string
	FamilyName string
	Email      string
	Timestamp  time.Time
}

func (e *PersonCreated) With(givenName, familyName, email string) PersonCreated {
	return PersonCreated{
		GivenName:  givenName,
		FamilyName: familyName,
		Email:      email,
		Timestamp:  time.Now(),
	}
}

func (e PersonCreated) EventType() string {
	return "Person.Created"
}

type PersonUpdated struct {
	GivenName  string
	FamilyName string
	Email      string
	AvatarURL  string
	Status     string
	Timestamp  time.Time
}

func (e PersonUpdated) With(
	givenName, familyName, email, avatarURL, status string,
) PersonUpdated {
	return PersonUpdated{
		GivenName:  givenName,
		FamilyName: familyName,
		Email:      email,
		AvatarURL:  avatarURL,
		Status:     status,
		Timestamp:  time.Now(),
	}
}

func (e PersonUpdated) EventType() string {
	return "Person.Updated"
}

type PersonDeleted struct {
	Timestamp time.Time
}

func (e PersonDeleted) With() PersonDeleted {
	return PersonDeleted{Timestamp: time.Now()}
}

func (e PersonDeleted) EventType() string {
	return "Person.Deleted"
}

type PersonOrganizationLinked struct {
	domain.BasicTripleEvent
	Timestamp time.Time
}

func (e PersonOrganizationLinked) With(
	personID, orgID string,
) PersonOrganizationLinked {
	return PersonOrganizationLinked{
		BasicTripleEvent: domain.BasicTripleEvent{
			Subject:   personID,
			Predicate: PredicateMemberOf,
			Object:    orgID,
		},
		Timestamp: time.Now(),
	}
}

func (e PersonOrganizationLinked) EventType() string {
	return "Person.OrganizationLinked"
}

const PersonEventPattern = "Person.%"
