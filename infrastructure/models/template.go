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

type Template struct {
	ID          string `gorm:"primaryKey"`
	ThemeID     string `gorm:"index;not null"`
	Name        string `gorm:"not null"`
	Slug        string `gorm:"not null"`
	Description string `gorm:"type:text"`
	FilePath    string `gorm:"type:text"`
	Status      string `gorm:"not null;default:draft"`
	SequenceNo  int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time `gorm:"index"`
}

func (m Template) TableName() string {
	return "templates"
}

func (m *Template) ToTemplate() (*entities.Template, error) {
	e := &entities.Template{}
	err := e.Restore(
		m.ID, m.Name, m.Slug, m.Description,
		m.FilePath, m.Status, m.CreatedAt, m.SequenceNo,
	)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func FromTemplate(e *entities.Template, themeID string) *Template {
	return &Template{
		ID:          e.GetID(),
		ThemeID:     themeID,
		Name:        e.Name(),
		Slug:        e.Slug(),
		Description: e.Description(),
		FilePath:    e.FilePath(),
		Status:      e.Status(),
		SequenceNo:  e.GetSequenceNo(),
		CreatedAt:   e.CreatedAt(),
	}
}
