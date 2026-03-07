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

type Section struct {
	ID         string     `gorm:"primaryKey"`
	PageID     string     `gorm:"index;not null"`
	Name       string     `gorm:"not null"`
	Slot       string     `gorm:"not null"`
	EntityType string     `gorm:"type:text"`
	Content    string     `gorm:"type:text"`
	Position   int        `gorm:"not null;default:0"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  *time.Time `gorm:"index"`
}

func (m Section) TableName() string {
	return "sections"
}

func (m *Section) ToSection() (*entities.Section, error) {
	e := &entities.Section{}
	err := e.Restore(
		m.ID, m.Name, m.Slot, m.EntityType,
		m.Content, m.Position, m.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func FromSection(e *entities.Section, pageID string) *Section {
	return &Section{
		ID:         e.GetID(),
		PageID:     pageID,
		Name:       e.Name(),
		Slot:       e.Slot(),
		EntityType: e.EntityType(),
		Content:    e.Content(),
		Position:   e.Position(),
		CreatedAt:  e.CreatedAt(),
	}
}
