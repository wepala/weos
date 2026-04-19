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

	"github.com/wepala/weos/v3/domain/entities"
)

type ResourceType struct {
	ID          string `gorm:"primaryKey"`
	Name        string `gorm:"not null"`
	Slug        string `gorm:"not null;uniqueIndex"`
	Description string `gorm:"type:text"`
	Context     string `gorm:"type:text"`
	Schema      string `gorm:"type:text"`
	Status      string `gorm:"not null;default:active"`
	SequenceNo  int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time `gorm:"index"`
}

func (m ResourceType) TableName() string {
	return "resource_types"
}

func (m *ResourceType) ToResourceType() (*entities.ResourceType, error) {
	e := &entities.ResourceType{}
	err := e.Restore(
		m.ID, m.Name, m.Slug, m.Description, m.Status,
		toRawMessage(m.Context), toRawMessage(m.Schema),
		m.CreatedAt, m.SequenceNo,
	)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func FromResourceType(e *entities.ResourceType) *ResourceType {
	return &ResourceType{
		ID:          e.GetID(),
		Name:        e.Name(),
		Slug:        e.Slug(),
		Description: e.Description(),
		Context:     string(e.Context()),
		Schema:      string(e.Schema()),
		Status:      e.Status(),
		SequenceNo:  e.GetSequenceNo(),
		CreatedAt:   e.CreatedAt(),
	}
}

func toRawMessage(s string) json.RawMessage {
	if s == "" {
		return nil
	}
	return json.RawMessage(s)
}
