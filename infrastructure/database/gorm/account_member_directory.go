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

package gorm

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/wepala/weos/v3/domain/repositories"
)

// AccountMemberDirectory lists the people of one account for the users routes.
type AccountMemberDirectory struct {
	db *gorm.DB
}

func ProvideAccountMemberDirectory(db *gorm.DB) repositories.AccountMemberDirectory {
	return &AccountMemberDirectory{db: db}
}

func (d *AccountMemberDirectory) ListMembers(
	ctx context.Context, accountID string,
) ([]repositories.AccountMembership, error) {
	if accountID == "" {
		return nil, nil
	}
	var members []repositories.AccountMembership
	// Queried by table name, as AccountMemberQuery is: account_members is
	// pericarp's projection, and core reads it without owning its model.
	err := d.db.WithContext(ctx).
		Table("account_members").
		Select("agent_id, role_id").
		Where("account_id = ?", accountID).
		Order("agent_id ASC").
		Scan(&members).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list the members of account %q: %w", accountID, err)
	}
	return members, nil
}
