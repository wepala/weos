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
//
// It reads pericarp's account_members, agents and credentials tables by name,
// as AccountMemberQuery does: core reads those projections without owning the
// models that define them. A page is two statements whatever its size — the
// memberships joined to the person records, then the credentials of the people
// on the page — where loading each member through pericarp's repositories cost
// two statements per member (wm-g7284).
type AccountMemberDirectory struct {
	db *gorm.DB
}

func ProvideAccountMemberDirectory(db *gorm.DB) repositories.AccountMemberDirectory {
	return &AccountMemberDirectory{db: db}
}

// memberRow is one membership and, when the person record exists, that record.
type memberRow struct {
	AgentID  string
	RoleID   string
	PersonID *string
	Name     *string
	Status   *string
}

type credentialEmailRow struct {
	AgentID string
	Email   string
}

func (d *AccountMemberDirectory) ListMembers(
	ctx context.Context, accountID, cursor string, limit int,
) (*repositories.AccountMemberPage, error) {
	page := &repositories.AccountMemberPage{}
	if accountID == "" {
		return page, nil
	}
	if limit <= 0 {
		limit = repositories.DefaultMemberPageSize
	}
	if limit > repositories.MaxMemberPageSize {
		limit = repositories.MaxMemberPageSize
	}

	var rows []memberRow
	query := d.db.WithContext(ctx).
		Table("account_members").
		Select("account_members.agent_id AS agent_id, account_members.role_id AS role_id, "+
			"agents.id AS person_id, agents.name AS name, agents.status AS status").
		Joins("LEFT JOIN agents ON agents.id = account_members.agent_id").
		Where("account_members.account_id = ?", accountID)
	if cursor != "" {
		query = query.Where("account_members.agent_id > ?", cursor)
	}
	// One row past the page says whether another page follows.
	if err := query.Order("account_members.agent_id ASC").Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list the members of account %q: %w", accountID, err)
	}
	if len(rows) > limit {
		rows = rows[:limit]
		page.HasMore = true
	}

	people := make([]string, 0, len(rows))
	for i := range rows {
		if rows[i].PersonID != nil {
			people = append(people, rows[i].AgentID)
		}
	}
	emails := make(map[string]string, len(people))
	if len(people) > 0 {
		var credentials []credentialEmailRow
		err := d.db.WithContext(ctx).
			Table("credentials").
			Select("agent_id, email").
			Where("agent_id IN ? AND email <> ?", people, "").
			Order("created_at ASC, id ASC").
			Scan(&credentials).Error
		if err != nil {
			return nil, fmt.Errorf("failed to read the emails of the members of account %q: %w", accountID, err)
		}
		for _, c := range credentials {
			if _, seen := emails[c.AgentID]; !seen {
				emails[c.AgentID] = c.Email
			}
		}
	}

	page.Members = make([]repositories.AccountMember, 0, len(rows))
	for i := range rows {
		member := repositories.AccountMember{AgentID: rows[i].AgentID, RoleID: rows[i].RoleID}
		if rows[i].PersonID != nil {
			member.HasRecord = true
			member.Name = derefString(rows[i].Name)
			member.Status = derefString(rows[i].Status)
			member.Email = emails[rows[i].AgentID]
		}
		page.Members = append(page.Members, member)
	}
	if page.HasMore {
		page.Cursor = rows[len(rows)-1].AgentID
	}
	return page, nil
}

func (d *AccountMemberDirectory) CountMembersWithRole(ctx context.Context, accountID, roleID string) (int, error) {
	if accountID == "" || roleID == "" {
		return 0, nil
	}
	var count int64
	err := d.db.WithContext(ctx).
		Table("account_members").
		Where("account_id = ? AND role_id = ?", accountID, roleID).
		Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to count the holders of role %q in account %q: %w", roleID, accountID, err)
	}
	return int(count), nil
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
