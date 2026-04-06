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
	"encoding/json"
	"errors"
	"fmt"

	"weos/domain/repositories"
	"weos/infrastructure/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type BehaviorSettingsRepository struct {
	db *gorm.DB
}

func ProvideBehaviorSettingsRepository(db *gorm.DB) repositories.BehaviorSettingsRepository {
	return &BehaviorSettingsRepository{db: db}
}

func (r *BehaviorSettingsRepository) GetByAccountAndType(
	ctx context.Context, accountID, typeSlug string,
) ([]string, error) {
	var row models.BehaviorSettings
	err := r.db.WithContext(ctx).
		Where("account_id = ? AND type_slug = ?", accountID, typeSlug).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil // no override — caller should use preset defaults
	}
	if err != nil {
		return nil, err
	}
	var slugs []string
	if err := json.Unmarshal([]byte(row.EnabledBehaviors), &slugs); err != nil {
		return nil, fmt.Errorf(
			"corrupt behavior settings for account %q type %q: %w",
			accountID, typeSlug, err)
	}
	// Row exists — return the parsed list (may be empty, meaning all disabled).
	return slugs, nil
}

func (r *BehaviorSettingsRepository) SaveByAccountAndType(
	ctx context.Context, accountID, typeSlug string, enabledSlugs []string,
) error {
	if enabledSlugs == nil {
		enabledSlugs = []string{}
	}
	data, err := json.Marshal(enabledSlugs)
	if err != nil {
		return err
	}

	row := models.BehaviorSettings{
		AccountID:        accountID,
		TypeSlug:         typeSlug,
		EnabledBehaviors: string(data),
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "account_id"}, {Name: "type_slug"}},
		DoUpdates: clause.AssignmentColumns([]string{"enabled_behaviors", "updated_at"}),
	}).Create(&row).Error
}
