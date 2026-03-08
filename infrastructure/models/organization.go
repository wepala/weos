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

type Organization struct {
	ID          string `gorm:"primaryKey"`
	Name        string `gorm:"not null"`
	Slug        string `gorm:"not null;uniqueIndex"`
	Description string `gorm:"type:text"`
	URL         string `gorm:"type:text"`
	LogoURL     string `gorm:"type:text"`
	Status      string `gorm:"not null;default:active"`
	SequenceNo  int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time `gorm:"index"`
}

func (m Organization) TableName() string {
	return "organizations"
}

func (m *Organization) ToOrganization() (*entities.Organization, error) {
	e := &entities.Organization{}
	err := e.Restore(
		m.ID, m.Name, m.Slug, m.Description,
		m.URL, m.LogoURL, m.Status, m.CreatedAt, m.SequenceNo,
	)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func FromOrganization(e *entities.Organization) *Organization {
	return &Organization{
		ID:          e.GetID(),
		Name:        e.Name(),
		Slug:        e.Slug(),
		Description: e.Description(),
		URL:         e.URL(),
		LogoURL:     e.LogoURL(),
		Status:      e.Status(),
		SequenceNo:  e.GetSequenceNo(),
		CreatedAt:   e.CreatedAt(),
	}
}
