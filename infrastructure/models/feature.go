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

// FeatureSetting is one explicit override of a feature flag, at either the
// instance or the account scope.
//
// A row's existence is the whole of its meaning: no row means the layer says
// nothing and resolution passes through it, so the tri-state (unset / on /
// off) needs no nullable column and no sentinel value, and cannot be
// mis-stored. Clearing an override deletes the row.
type FeatureSetting struct {
	ID uint `gorm:"primaryKey;autoIncrement"`
	// ScopeType is "instance" or "account".
	ScopeType string `gorm:"type:varchar(32);not null;uniqueIndex:idx_feature_scope_key"`
	// ScopeID is the account ID, and empty for the instance scope — there is
	// one instance, so it needs no identifier.
	ScopeID    string `gorm:"type:varchar(255);not null;default:'';uniqueIndex:idx_feature_scope_key"`
	FeatureKey string `gorm:"type:varchar(64);not null;uniqueIndex:idx_feature_scope_key"`
	Enabled    bool   `gorm:"not null"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (FeatureSetting) TableName() string {
	return "feature_settings"
}

// FeatureGrant is a right to a feature held by one agent or by one role.
//
// There is no enabled column, deliberately: a grant only ever turns a feature
// on. Saying "off" is an override's job, and a grant row meaning "off" would
// contradict the resolution rule that a grant can never lower what an upper
// layer decided. Revoking deletes the row.
//
// Separate from FeatureSetting because the lifecycles diverge: #483 adds
// validity windows and provenance here, which would be permanently null on
// every instance-scoped override row.
type FeatureGrant struct {
	ID uint `gorm:"primaryKey;autoIncrement"`
	// SubjectType is "agent" or "role".
	SubjectType string `gorm:"type:varchar(32);not null;uniqueIndex:idx_feature_grant_subject"`
	// SubjectID is the agent ID or the role ID.
	SubjectID string `gorm:"type:varchar(255);not null;uniqueIndex:idx_feature_grant_subject"`
	// AccountID scopes the grant. Roles are per-account memberships, and a
	// person's access is granted within an account, so both subject kinds
	// carry one.
	AccountID  string `gorm:"type:varchar(255);not null;uniqueIndex:idx_feature_grant_subject;index:idx_feature_grant_account"`
	FeatureKey string `gorm:"type:varchar(64);not null;uniqueIndex:idx_feature_grant_subject"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (FeatureGrant) TableName() string {
	return "feature_grants"
}
