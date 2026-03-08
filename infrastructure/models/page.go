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
	"weos/pkg/identity"
)

type Page struct {
	ID          string     `gorm:"primaryKey"`
	WebsiteID   string     `gorm:"index;not null"`
	Name        string     `gorm:"not null"`
	Slug        string     `gorm:"not null"`
	Description string     `gorm:"type:text"`
	Template    string     `gorm:"type:text"`
	Position    int        `gorm:"not null;default:0"`
	Status      string     `gorm:"not null;default:draft"`
	SequenceNo  int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time `gorm:"index"`
}

func (m Page) TableName() string {
	return "pages"
}

func (m *Page) ToPage() (*entities.Page, error) {
	e := &entities.Page{}
	err := e.Restore(
		m.ID, m.Name, m.Slug, m.Description,
		m.Template, m.Status, m.Position, m.CreatedAt, m.SequenceNo,
	)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func FromPage(e *entities.Page) *Page {
	websiteSlug := identity.ExtractWebsiteSlug(e.GetID())
	return &Page{
		ID:          e.GetID(),
		WebsiteID:   "urn:" + websiteSlug,
		Name:        e.Name(),
		Slug:        e.Slug(),
		Description: e.Description(),
		Template:    e.Template(),
		Position:    e.Position(),
		Status:      e.Status(),
		SequenceNo:  e.GetSequenceNo(),
		CreatedAt:   e.CreatedAt(),
	}
}
