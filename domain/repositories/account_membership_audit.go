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

package repositories

import (
	"context"
	"time"
)

// AccountMembershipAudit reads, and never writes, what an operator needs to
// find the account memberships nothing on record explains (wm-vycbd).
//
// Before wm-govvg, the users routes saved every role change into the
// instance's first account, for any person, whatever account the caller acted
// in. Those rows outlive the fix, and the scoped routes honor them. A sign-up
// records its creator joining the account in the account's own event history,
// and accepting an invite records the same; the old route wrote the row and
// recorded nothing. So a membership the history does not record and no invite
// in the account names is one to review, and so is one whose role is not the
// role its joining recorded.
type AccountMembershipAudit interface {
	// FirstAccountID returns the id of the instance's first account — the
	// account the old users route wrote every role into — or "" when the
	// instance has no account.
	FirstAccountID(ctx context.Context) (string, error)

	// UnexplainedMemberships reports the memberships of accountID that its
	// history and its invites do not explain, oldest write first.
	UnexplainedMemberships(ctx context.Context, accountID string) (*MembershipAuditReport, error)
}

// MembershipAuditReport is what the audit found in one account.
type MembershipAuditReport struct {
	AccountID    string
	AccountFound bool
	// HistoryFound is false when the event log holds nothing for the account.
	// A sign-up then cannot be told from a row the old route wrote, so every
	// membership no invite explains is listed.
	HistoryFound bool
	// Memberships is how many membership rows the account has.
	Memberships int
	Unexplained []UnexplainedMembership
}

// UnexplainedMembership is one membership row the audit lists.
type UnexplainedMembership struct {
	AgentID string
	RoleID  string
	// WrittenAt is the row's created_at. pericarp rewrites it on every role
	// save, so it is when the row was last written.
	WrittenAt time.Time
	// RecordedRoleID is the role the person's joining recorded — in the
	// account's history or on an invite — when the row now carries another.
	// It is "" when nothing on record ties the person to the account.
	RecordedRoleID string
}
