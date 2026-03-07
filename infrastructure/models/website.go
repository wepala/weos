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

type Website struct {
	ID          string     `gorm:"primaryKey"`
	Name        string     `gorm:"not null"`
	Slug        string     `gorm:"not null;uniqueIndex"`
	URL         string     `gorm:"not null"`
	Description string     `gorm:"type:text"`
	Language    string     `gorm:"not null;default:en"`
	Status      string     `gorm:"not null;default:draft"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time `gorm:"index"`
}

func (m Website) TableName() string {
	return "websites"
}

func (m *Website) ToWebsite() (*entities.Website, error) {
	e := &entities.Website{}
	err := e.Restore(
		m.ID, m.Name, m.Slug, m.URL, m.Description,
		m.Language, m.Status, m.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func FromWebsite(e *entities.Website) *Website {
	return &Website{
		ID:          e.GetID(),
		Name:        e.Name(),
		Slug:        e.Slug(),
		URL:         e.URL(),
		Description: e.Description(),
		Language:    e.Language(),
		Status:      e.Status(),
		CreatedAt:   e.CreatedAt(),
	}
}
