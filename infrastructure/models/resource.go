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
	"encoding/json"
	"time"

	"weos/domain/entities"
)

type Resource struct {
	ID         string `gorm:"primaryKey"`
	TypeSlug   string `gorm:"not null;index"`
	Data       string `gorm:"type:text"`
	Status     string `gorm:"not null;default:active"`
	SequenceNo int
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  *time.Time `gorm:"index"`
}

func (m Resource) TableName() string {
	return "resources"
}

func (m *Resource) ToResource() (*entities.Resource, error) {
	e := &entities.Resource{}
	err := e.Restore(
		m.ID, m.TypeSlug, m.Status,
		json.RawMessage(m.Data),
		m.CreatedAt, m.SequenceNo,
	)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func FromResource(e *entities.Resource) *Resource {
	return &Resource{
		ID:         e.GetID(),
		TypeSlug:   e.TypeSlug(),
		Data:       string(e.Data()),
		Status:     e.Status(),
		SequenceNo: e.GetSequenceNo(),
		CreatedAt:  e.CreatedAt(),
	}
}
