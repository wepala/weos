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

package models

import (
	"time"

	"weos/domain/entities"
)

type Person struct {
	ID         string     `gorm:"primaryKey"`
	GivenName  string     `gorm:"not null"`
	FamilyName string     `gorm:"not null"`
	Email      string     `gorm:"not null;uniqueIndex"`
	AvatarURL  string     `gorm:"type:text"`
	Status     string     `gorm:"not null;default:active"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  *time.Time `gorm:"index"`
}

func (m Person) TableName() string {
	return "persons"
}

func (m *Person) ToPerson() (*entities.Person, error) {
	e := &entities.Person{}
	err := e.Restore(
		m.ID, m.GivenName, m.FamilyName, m.Email,
		m.AvatarURL, m.Status, m.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func FromPerson(e *entities.Person) *Person {
	return &Person{
		ID:         e.GetID(),
		GivenName:  e.GivenName(),
		FamilyName: e.FamilyName(),
		Email:      e.Email(),
		AvatarURL:  e.AvatarURL(),
		Status:     e.Status(),
		CreatedAt:  e.CreatedAt(),
	}
}
