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

type SidebarSettings struct {
	ID          uint   `gorm:"primaryKey;autoIncrement"`
	Role        string `gorm:"type:varchar(100);uniqueIndex;default:default"`
	HiddenSlugs string `gorm:"type:text"`
	MenuGroups  string `gorm:"type:text"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (SidebarSettings) TableName() string {
	return "sidebar_settings"
}
