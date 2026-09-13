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
	"errors"
	"fmt"

	"github.com/wepala/weos/v3/domain/repositories"

	"gorm.io/gorm"
)

// CredentialRowDeleter implements repositories.CredentialRowDeleter over
// pericarp's credentials table.
type CredentialRowDeleter struct {
	db *gorm.DB
}

// ProvideCredentialRowDeleter builds the deleter.
func ProvideCredentialRowDeleter(db *gorm.DB) repositories.CredentialRowDeleter {
	return &CredentialRowDeleter{db: db}
}

// DeleteCredentialRow implements repositories.CredentialRowDeleter. The table
// is addressed by name, as the account purger addresses it: credentials is
// pericarp's projection, and core writes to it without owning its model.
func (d *CredentialRowDeleter) DeleteCredentialRow(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("no credential id to delete")
	}
	if err := d.db.WithContext(ctx).Table("credentials").Where("id = ?", id).Delete(map[string]any{}).Error; err != nil {
		return fmt.Errorf("failed to delete the credential row: %w", err)
	}
	return nil
}
