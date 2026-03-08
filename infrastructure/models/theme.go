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

type Theme struct {
	ID           string `gorm:"primaryKey"`
	Name         string `gorm:"not null"`
	Slug         string `gorm:"not null;uniqueIndex"`
	Description  string `gorm:"type:text"`
	Version      string `gorm:"type:text"`
	ThumbnailURL string `gorm:"type:text"`
	Status       string `gorm:"not null;default:draft"`
	SequenceNo   int
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time `gorm:"index"`
}

func (m Theme) TableName() string {
	return "themes"
}

func (m *Theme) ToTheme() (*entities.Theme, error) {
	e := &entities.Theme{}
	err := e.Restore(
		m.ID, m.Name, m.Slug, m.Description,
		m.Version, m.ThumbnailURL, m.Status, m.CreatedAt, m.SequenceNo,
	)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func FromTheme(e *entities.Theme) *Theme {
	return &Theme{
		ID:           e.GetID(),
		Name:         e.Name(),
		Slug:         e.Slug(),
		Description:  e.Description(),
		Version:      e.Version(),
		ThumbnailURL: e.ThumbnailURL(),
		Status:       e.Status(),
		SequenceNo:   e.GetSequenceNo(),
		CreatedAt:    e.CreatedAt(),
	}
}
