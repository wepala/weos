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

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FeatureSettingsRepository stores the instance and account override layers.
type FeatureSettingsRepository struct {
	db *gorm.DB
}

func ProvideFeatureSettingsRepository(db *gorm.DB) repositories.FeatureSettingsRepository {
	return &FeatureSettingsRepository{db: db}
}

func (r *FeatureSettingsRepository) InstanceOverrides(ctx context.Context) (map[string]bool, error) {
	return r.overridesFor(ctx, repositories.FeatureScopeInstance, "")
}

func (r *FeatureSettingsRepository) AccountOverrides(
	ctx context.Context, accountID string,
) (map[string]bool, error) {
	if accountID == "" {
		// No account means no account layer. Returning empty rather than
		// querying with an empty scope avoids matching instance rows, whose
		// ScopeID is also empty.
		return map[string]bool{}, nil
	}
	return r.overridesFor(ctx, repositories.FeatureScopeAccount, accountID)
}

func (r *FeatureSettingsRepository) overridesFor(
	ctx context.Context, scopeType, scopeID string,
) (map[string]bool, error) {
	var rows []models.FeatureSetting
	if err := r.db.WithContext(ctx).
		Where("scope_type = ? AND scope_id = ?", scopeType, scopeID).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to load %s feature overrides: %w", scopeType, err)
	}
	out := make(map[string]bool, len(rows))
	for _, row := range rows {
		out[row.FeatureKey] = row.Enabled
	}
	return out, nil
}

func (r *FeatureSettingsRepository) SetOverride(
	ctx context.Context, scopeType, scopeID, featureKey string, enabled bool,
) error {
	row := models.FeatureSetting{
		ScopeType:  scopeType,
		ScopeID:    scopeID,
		FeatureKey: featureKey,
		Enabled:    enabled,
	}
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "scope_type"}, {Name: "scope_id"}, {Name: "feature_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"enabled", "updated_at"}),
	}).Create(&row).Error
	if err != nil {
		return fmt.Errorf("failed to set feature override %q at %s scope: %w", featureKey, scopeType, err)
	}
	return nil
}

func (r *FeatureSettingsRepository) ClearOverride(
	ctx context.Context, scopeType, scopeID, featureKey string,
) error {
	// Deleting a row that is not there is not an error: the caller wanted this
	// layer to say nothing about the feature, and it already does.
	err := r.db.WithContext(ctx).
		Where("scope_type = ? AND scope_id = ? AND feature_key = ?", scopeType, scopeID, featureKey).
		Delete(&models.FeatureSetting{}).Error
	if err != nil {
		return fmt.Errorf("failed to clear feature override %q at %s scope: %w", featureKey, scopeType, err)
	}
	return nil
}

// FeatureGrantRepository stores the bottom layer — grants to an agent or a
// role, always within an account.
type FeatureGrantRepository struct {
	db *gorm.DB
}

func ProvideFeatureGrantRepository(db *gorm.DB) repositories.FeatureGrantRepository {
	return &FeatureGrantRepository{db: db}
}

func (r *FeatureGrantRepository) GrantsFor(
	ctx context.Context, accountID, agentID, roleID string,
) ([]entities.FeatureGrantRecord, error) {
	if accountID == "" || agentID == "" {
		// Grants are account-scoped and subject-bound. With neither, there is
		// nothing a grant could match, and querying would risk matching rows
		// on empty strings.
		return nil, nil
	}

	query := r.db.WithContext(ctx).
		Model(&models.FeatureGrant{}).
		Where("account_id = ?", accountID)

	// One query for both legs — the grant made to this person directly, and
	// the one carried by whatever role they hold in this account. No filter on
	// the window: a grant that has not started is a boundary the resolver
	// needs, and one that has closed is a row a listing needs.
	if roleID != "" {
		query = query.Where(
			r.db.Where("subject_type = ? AND subject_id = ?", repositories.FeatureSubjectAgent, agentID).
				Or("subject_type = ? AND subject_id = ?", repositories.FeatureSubjectRole, roleID),
		)
	} else {
		query = query.Where("subject_type = ? AND subject_id = ?", repositories.FeatureSubjectAgent, agentID)
	}

	var rows []models.FeatureGrant
	if err := query.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to load feature grants for agent %q: %w", agentID, err)
	}
	return recordsFrom(rows), nil
}

func (r *FeatureGrantRepository) ListByFeature(
	ctx context.Context, accountID, featureKey string,
) ([]entities.FeatureGrantRecord, error) {
	if accountID == "" || featureKey == "" {
		return nil, nil
	}
	var rows []models.FeatureGrant
	err := r.db.WithContext(ctx).
		Where("account_id = ? AND feature_key = ?", accountID, featureKey).
		Order("subject_type ASC, subject_id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list grants on feature %q: %w", featureKey, err)
	}
	return recordsFrom(rows), nil
}

func recordsFrom(rows []models.FeatureGrant) []entities.FeatureGrantRecord {
	out := make([]entities.FeatureGrantRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, entities.FeatureGrantRecord{
			SubjectType:    row.SubjectType,
			SubjectID:      row.SubjectID,
			AccountID:      row.AccountID,
			FeatureKey:     row.FeatureKey,
			ValidFrom:      row.ValidFrom,
			ValidThrough:   row.ValidThrough,
			GrantedByID:    row.GrantedByID,
			GrantedByEmail: row.GrantedByEmail,
			Source:         row.Source,
			CreatedAt:      row.CreatedAt,
		})
	}
	return out
}

func (r *FeatureGrantRepository) Grant(
	ctx context.Context, record entities.FeatureGrantRecord,
) error {
	row := models.FeatureGrant{
		SubjectType:    record.SubjectType,
		SubjectID:      record.SubjectID,
		AccountID:      record.AccountID,
		FeatureKey:     record.FeatureKey,
		ValidFrom:      record.ValidFrom,
		ValidThrough:   record.ValidThrough,
		GrantedByID:    record.GrantedByID,
		GrantedByEmail: record.GrantedByEmail,
		Source:         record.Source,
	}
	// Granting twice leaves one grant. The window and provenance are replaced
	// rather than kept: re-granting with a new window is how an admin changes
	// one, and the record should say who last did that.
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "subject_type"}, {Name: "subject_id"}, {Name: "account_id"}, {Name: "feature_key"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"valid_from", "valid_through", "granted_by_id", "granted_by_email", "source", "updated_at",
		}),
	}).Create(&row).Error
	if err != nil {
		return fmt.Errorf("failed to grant feature %q to %s %q: %w",
			record.FeatureKey, record.SubjectType, record.SubjectID, err)
	}
	return nil
}

func (r *FeatureGrantRepository) Revoke(
	ctx context.Context, subjectType, subjectID, accountID, featureKey string,
) (bool, error) {
	res := r.db.WithContext(ctx).
		Where("subject_type = ? AND subject_id = ? AND account_id = ? AND feature_key = ?",
			subjectType, subjectID, accountID, featureKey).
		Delete(&models.FeatureGrant{})
	if res.Error != nil {
		return false, fmt.Errorf("failed to revoke feature %q from %s %q: %w",
			featureKey, subjectType, subjectID, res.Error)
	}
	// Reported rather than assumed, so a caller can tell a real revocation
	// from a no-op and record only what happened.
	return res.RowsAffected > 0, nil
}

// AccountMemberQuery answers "who holds this role in this account?" — the
// account-first direction pericarp's AccountRepository does not offer.
type AccountMemberQuery struct {
	db *gorm.DB
}

func ProvideAccountMemberQuery(db *gorm.DB) repositories.AccountMemberQuery {
	return &AccountMemberQuery{db: db}
}

func (q *AccountMemberQuery) ListMemberIDsByRole(
	ctx context.Context, accountID, roleID string,
) ([]string, error) {
	if accountID == "" || roleID == "" {
		return nil, nil
	}
	var agentIDs []string
	// Queried by table name rather than through a model: account_members is
	// pericarp's projection, and core reads it without taking ownership of the
	// struct that defines it.
	err := q.db.WithContext(ctx).
		Table("account_members").
		Where("account_id = ? AND role_id = ?", accountID, roleID).
		Pluck("agent_id", &agentIDs).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list members holding role %q in account %q: %w", roleID, accountID, err)
	}
	return agentIDs, nil
}
