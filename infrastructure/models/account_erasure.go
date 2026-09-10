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

// AccountErasure marks an account whose erasure has begun and not finished.
//
// The row exists from the moment the account is locked until the transaction
// that deletes the account row, which deletes this one too. While it exists
// the account is inactive in pericarp's terms, exactly like a suspended one;
// this row is what tells the two apart, so a sign-in can offer to finish the
// deletion and every other refusal can say the deletion is unfinished.
type AccountErasure struct {
	AccountID   string `gorm:"primaryKey;type:varchar(255)"`
	RequestedBy string `gorm:"type:varchar(255)"`
	StartedAt   time.Time
}

func (AccountErasure) TableName() string {
	return "account_erasures"
}
