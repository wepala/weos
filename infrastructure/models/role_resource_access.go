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

import "time"

// RoleResourceAccess stores the role→resource-type access configuration as JSON.
// The Access column is a JSON object mapping role names to their per-resource-type
// allowed ODRL actions: {"instructor": {"enrollment": ["read","modify"], "invoice": ["read"]}}.
type RoleResourceAccess struct {
	ID        uint   `gorm:"primaryKey;autoIncrement"`
	Access    string `gorm:"type:text"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (RoleResourceAccess) TableName() string {
	return "role_resource_access"
}
