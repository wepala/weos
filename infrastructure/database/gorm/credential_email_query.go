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
	"strings"

	"github.com/wepala/weos/v3/domain/repositories"

	"gorm.io/gorm"
)

// CredentialEmailQuery implements repositories.CredentialEmailQuery over
// pericarp's credentials table.
type CredentialEmailQuery struct {
	db *gorm.DB
}

// ProvideCredentialEmailQuery builds the query.
func ProvideCredentialEmailQuery(db *gorm.DB) repositories.CredentialEmailQuery {
	return &CredentialEmailQuery{db: db}
}

// CredentialsByEmail implements repositories.CredentialEmailQuery.
//
// LOWER(TRIM(email)) cannot use pericarp's email index. The table holds a
// handful of rows per person and is read here once per first sign-in of an
// identity the instance has never seen, so a scan is the honest trade against
// adding an index to a table core does not own.
func (q *CredentialEmailQuery) CredentialsByEmail(ctx context.Context, email string) ([]repositories.CredentialEmailMatch, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if normalized == "" {
		return nil, nil
	}
	var rows []struct {
		AgentID  string
		Provider string
		Active   bool
	}
	// Queried by table name rather than through a model: credentials is
	// pericarp's projection, and core reads it without taking ownership of the
	// struct that defines it.
	err := q.db.WithContext(ctx).
		Table("credentials").
		Select("agent_id, provider, active").
		Where("LOWER(TRIM(email)) = ?", normalized).
		Order("agent_id, provider").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to find the credentials holding an email: %w", err)
	}
	matches := make([]repositories.CredentialEmailMatch, 0, len(rows))
	for _, r := range rows {
		matches = append(matches, repositories.CredentialEmailMatch{AgentID: r.AgentID, Provider: r.Provider, Active: r.Active})
	}
	return matches, nil
}
