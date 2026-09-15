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
	"fmt"
	"time"

	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	"gorm.io/gorm"

	"github.com/wepala/weos/v3/domain/repositories"
)

// AccountMembershipAudit answers the operator membership audit from pericarp's
// accounts, account_members, invites and credentials tables and the event log.
// It only reads, by table name, as the other ports over pericarp's tables do.
type AccountMembershipAudit struct {
	db *gorm.DB
}

func ProvideAccountMembershipAudit(db *gorm.DB) repositories.AccountMembershipAudit {
	return &AccountMembershipAudit{db: db}
}

func (a *AccountMembershipAudit) FirstAccountID(ctx context.Context) (string, error) {
	var ids []string
	// Ordered as pericarp's AccountRepository.FindAll orders accounts, which is
	// how the old users route chose the first one.
	if err := a.db.WithContext(ctx).Table("accounts").Order("id ASC").Limit(1).Pluck("id", &ids).Error; err != nil {
		return "", fmt.Errorf("failed to find the instance's first account: %w", err)
	}
	if len(ids) == 0 {
		return "", nil
	}
	return ids[0], nil
}

type auditMemberRow struct {
	AgentID   string
	RoleID    string
	CreatedAt time.Time
}

// auditEventRow holds the payload as text, which both SQLite's text column and
// Postgres's jsonb column scan into.
type auditEventRow struct {
	EventType string
	Payload   string
}

type auditInviteRow struct {
	Email          string
	InviteeAgentID string
	RoleID         string
}

// auditInvitedRole is the role the latest invite to an address gave, and that
// invite's place in the account's invites, oldest first.
type auditInvitedRole struct {
	role string
	sent int
}

func (a *AccountMembershipAudit) UnexplainedMemberships(
	ctx context.Context, accountID string,
) (*repositories.MembershipAuditReport, error) {
	report := &repositories.MembershipAuditReport{AccountID: accountID}
	db := a.db.WithContext(ctx)

	var accounts int64
	if err := db.Table("accounts").Where("id = ?", accountID).Count(&accounts).Error; err != nil {
		return nil, fmt.Errorf("failed to find account %q: %w", accountID, err)
	}
	report.AccountFound = accounts > 0

	var rows []auditMemberRow
	if err := db.Table("account_members").
		Select("agent_id, role_id, created_at").
		Where("account_id = ?", accountID).
		Order("created_at ASC, agent_id ASC").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to read the memberships of account %q: %w", accountID, err)
	}
	report.Memberships = len(rows)
	if len(rows) == 0 {
		return report, nil
	}

	// The account's own history, in order: who joined with which role, whose
	// role it changed, and who left.
	var events []auditEventRow
	if err := db.Table("events").
		Select("event_type, payload").
		Where("aggregate_id = ?", accountID).
		Order("sequence_no ASC").
		Scan(&events).Error; err != nil {
		return nil, fmt.Errorf("failed to read the history of account %q: %w", accountID, err)
	}
	report.HistoryFound = len(events) > 0
	recorded := map[string]string{}
	for _, e := range events {
		switch e.EventType {
		case authentities.EventTypeAccountMemberAdded,
			authentities.EventTypeAccountMemberRoleChanged,
			authentities.EventTypeAccountMemberRemoved:
		default:
			continue
		}
		// The payload is the event as JSON: the member is its object, and an
		// added or changed membership carries the role.
		var change struct {
			Object string `json:"object"`
			Role   string `json:"role"`
		}
		if err := json.Unmarshal([]byte(e.Payload), &change); err != nil {
			return nil, fmt.Errorf("failed to read a membership event of account %q: %w", accountID, err)
		}
		if change.Object == "" {
			continue
		}
		if e.EventType == authentities.EventTypeAccountMemberRemoved {
			delete(recorded, change.Object)
		} else {
			recorded[change.Object] = change.Role
		}
	}

	// The account's invites, by the person who accepted one and by the address
	// one was sent to. A later invite's role wins.
	var invites []auditInviteRow
	if err := db.Table("invites").
		Select("email, invitee_agent_id, role_id").
		Where("account_id = ?", accountID).
		Order("created_at ASC, id ASC").
		Scan(&invites).Error; err != nil {
		return nil, fmt.Errorf("failed to read the invites of account %q: %w", accountID, err)
	}
	invitedAgent := map[string]string{}
	invitedEmail := map[string]auditInvitedRole{}
	for i, inv := range invites {
		if inv.InviteeAgentID != "" {
			invitedAgent[inv.InviteeAgentID] = inv.RoleID
		}
		if key := auditEmailKey(inv.Email); key != "" {
			invitedEmail[key] = auditInvitedRole{role: inv.RoleID, sent: i}
		}
	}

	// An address only matters for a person nothing else explains, so only
	// their credentials are read.
	var byEmail []string
	for _, r := range rows {
		if _, ok := recorded[r.AgentID]; ok {
			continue
		}
		if _, ok := invitedAgent[r.AgentID]; ok {
			continue
		}
		byEmail = append(byEmail, r.AgentID)
	}
	emailRole := map[string]string{}
	if len(byEmail) > 0 && len(invitedEmail) > 0 {
		var credentials []credentialEmailRow
		if err := db.Table("credentials").
			Select("agent_id, email").
			Where("agent_id IN ?", byEmail).
			Scan(&credentials).Error; err != nil {
			return nil, fmt.Errorf("failed to read the credentials of the members of account %q: %w", accountID, err)
		}
		// A person with credentials for several invited addresses takes the
		// role of the latest of those invites, as one address invited twice
		// does. The credentials are read back in no set order, so the invite
		// order decides, not the order of the loop.
		latest := map[string]int{}
		for _, c := range credentials {
			inv, ok := invitedEmail[auditEmailKey(c.Email)]
			if !ok {
				continue
			}
			if sent, seen := latest[c.AgentID]; seen && sent > inv.sent {
				continue
			}
			latest[c.AgentID] = inv.sent
			emailRole[c.AgentID] = inv.role
		}
	}

	for _, r := range rows {
		role, explained := recorded[r.AgentID]
		if !explained {
			role, explained = invitedAgent[r.AgentID]
		}
		if !explained {
			role, explained = emailRole[r.AgentID]
		}
		switch {
		case !explained:
			report.Unexplained = append(report.Unexplained, repositories.UnexplainedMembership{
				AgentID: r.AgentID, RoleID: r.RoleID, WrittenAt: r.CreatedAt,
			})
		case role != "" && role != r.RoleID:
			report.Unexplained = append(report.Unexplained, repositories.UnexplainedMembership{
				AgentID: r.AgentID, RoleID: r.RoleID, WrittenAt: r.CreatedAt, RecordedRoleID: role,
			})
		}
	}
	return report, nil
}

// auditEmailKey is an address as the audit compares it: under owner binding's
// one rule, repositories.FoldCredentialEmail. A Unicode fold is looser than any
// path that writes a membership, so it would count as explained a membership
// whose address only folds to the invited one.
func auditEmailKey(email string) string {
	return repositories.FoldCredentialEmail(email)
}
