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
	"time"

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

var _ repositories.AccountUsageReader = (*AccountMemberDirectory)(nil)

// ProvideAccountUsageReader reads an account's members, pending invitations
// and the creators of its resources over db. The account adoption
// (wm-c7xehr) moves resources only out of an account nobody can use.
func ProvideAccountUsageReader(db *gorm.DB) repositories.AccountUsageReader {
	return &AccountMemberDirectory{db: db}
}

// CountAccountMembers returns how many account_members rows name accountID.
func (d *AccountMemberDirectory) CountAccountMembers(ctx context.Context, accountID string) (int, error) {
	var n int64
	if err := d.db.WithContext(ctx).Table("account_members").Where("account_id = ?", accountID).
		Count(&n).Error; err != nil {
		return 0, fmt.Errorf("count the members of %s: %w", accountID, err)
	}
	return int(n), nil
}

// CountPendingInvites returns how many invitations into accountID are still
// pending and expire after now.
func (d *AccountMemberDirectory) CountPendingInvites(ctx context.Context, accountID string, now time.Time) (int, error) {
	var n int64
	if err := d.db.WithContext(ctx).Table("invites").
		Where("account_id = ? AND status = ? AND expires_at > ?", accountID, "pending", now).
		Count(&n).Error; err != nil {
		return 0, fmt.Errorf("count the pending invitations of %s: %w", accountID, err)
	}
	return int(n), nil
}

// AccountCreators returns each agent that created a live resource of
// accountID, with how many, the newest creation time and whether it still
// holds an active credential.
func (d *AccountMemberDirectory) AccountCreators(
	ctx context.Context, accountID string,
) ([]repositories.AccountCreator, error) {
	db := d.db.WithContext(ctx)
	var rows []struct {
		AgentID   string
		Resources int
	}
	// The newest creation time is read per agent below rather than with MAX:
	// SQLite returns MAX over a timestamp column as text.
	if err := db.Table("resources").
		Select("created_by AS agent_id, COUNT(*) AS resources").
		Where("account_id = ? AND deleted_at IS NULL", accountID).
		Group("created_by").Order("created_by").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list the creators of the resources of %s: %w", accountID, err)
	}
	out := make([]repositories.AccountCreator, 0, len(rows))
	for _, row := range rows {
		c := repositories.AccountCreator{AgentID: row.AgentID, Resources: row.Resources}
		var newest struct{ CreatedAt time.Time }
		if err := db.Table("resources").Select("created_at").
			Where("account_id = ? AND deleted_at IS NULL AND created_by = ?", accountID, row.AgentID).
			Order("created_at DESC").Limit(1).Scan(&newest).Error; err != nil {
			return nil, fmt.Errorf("read the newest resource %s created in %s: %w", row.AgentID, accountID, err)
		}
		c.NewestCreatedAt = newest.CreatedAt
		if row.AgentID != "" {
			var creds int64
			if err := db.Table("credentials").Where("agent_id = ? AND active = ?", row.AgentID, true).
				Count(&creds).Error; err != nil {
				return nil, fmt.Errorf("read the credentials of %s: %w", row.AgentID, err)
			}
			c.HasCredential = creds > 0
		}
		out = append(out, c)
	}
	return out, nil
}
