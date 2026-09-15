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
	"testing"
	"time"

	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authmodels "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/models"
	esinfra "github.com/akeemphilbert/pericarp/pkg/eventsourcing/infrastructure"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/wepala/weos/v3/domain/repositories"
)

// auditStore is a store holding pericarp's auth tables and the event log, the
// tables the membership audit reads.
type auditStore struct {
	t        *testing.T
	db       *gorm.DB
	position int64
}

func newAuditStore(t *testing.T) *auditStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), gormConfig())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&authmodels.AccountModel{}, &authmodels.AgentModel{}, &authmodels.CredentialModel{},
		&authmodels.AccountMemberModel{}, &authmodels.InviteModel{}, &esinfra.GormEventModel{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &auditStore{t: t, db: db}
}

func (s *auditStore) account(id string) {
	s.t.Helper()
	if err := s.db.Create(&authmodels.AccountModel{ID: id, Name: id, AccountType: "personal", Active: true, CreatedAt: time.Now()}).Error; err != nil {
		s.t.Fatalf("seed account %s: %v", id, err)
	}
}

func (s *auditStore) row(accountID, agentID, roleID string, writtenAt time.Time) {
	s.t.Helper()
	if err := s.db.Create(&authmodels.AccountMemberModel{
		AccountID: accountID, AgentID: agentID, RoleID: roleID, CreatedAt: writtenAt,
	}).Error; err != nil {
		s.t.Fatalf("seed membership %s in %s: %v", agentID, accountID, err)
	}
}

// event appends an event to accountID's history, the way a unit of work
// records a membership change on the account aggregate.
func (s *auditStore) event(accountID string, seq int, eventType, agentID, roleID string) {
	s.t.Helper()
	s.position++
	payload := esinfra.JSONB{"subject": accountID, "object": agentID}
	if roleID != "" {
		payload["role"] = roleID
	}
	if err := s.db.Create(&esinfra.GormEventModel{
		ID: fmt.Sprintf("%s-event-%d", accountID, seq), AggregateID: accountID, EventType: eventType,
		SequenceNo: seq, Position: s.position, Payload: payload, CreatedAt: time.Now(),
	}).Error; err != nil {
		s.t.Fatalf("seed event %s #%d: %v", accountID, seq, err)
	}
}

func (s *auditStore) invite(id, accountID, email, inviteeAgentID, roleID string) {
	s.t.Helper()
	if err := s.db.Create(&authmodels.InviteModel{
		ID: id, AccountID: accountID, Email: email, RoleID: roleID,
		InviterAgentID: "agent-ops", InviteeAgentID: inviteeAgentID, Status: "pending",
		ExpiresAt: time.Now().Add(72 * time.Hour), CreatedAt: time.Now(),
	}).Error; err != nil {
		s.t.Fatalf("seed invite %s: %v", id, err)
	}
}

func (s *auditStore) credential(agentID, email string) {
	s.t.Helper()
	if err := s.db.Create(&authmodels.CredentialModel{
		ID: agentID + "-cred", AgentID: agentID, Provider: "password", ProviderUserID: agentID,
		Email: email, Active: true, CreatedAt: time.Now(),
	}).Error; err != nil {
		s.t.Fatalf("seed credential of %s: %v", agentID, err)
	}
}

// membershipRows is every membership on the store, for proving the audit wrote
// nothing.
func (s *auditStore) membershipRows() []authmodels.AccountMemberModel {
	s.t.Helper()
	var rows []authmodels.AccountMemberModel
	if err := s.db.Order("account_id, agent_id").Find(&rows).Error; err != nil {
		s.t.Fatalf("read memberships: %v", err)
	}
	return rows
}

// wm-vycbd. The audit lists the memberships of an account that neither the
// account's history nor an invite in that account explains, and a membership
// whose role is not the role its sign-up or invite gave it. It changes nothing.
func TestAccountMembershipAuditListsMembershipsNoHistoryOrInviteExplains(t *testing.T) {
	ctx := context.Background()
	s := newAuditStore(t)
	base := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	at := func(hours int) time.Time { return base.Add(time.Duration(hours) * time.Hour) }

	s.account("acct-1harbor")
	s.account("acct-2cedar")
	// The creator's sign-up and two people who joined through the history.
	s.event("acct-1harbor", 1, authentities.EventTypeAccountCreated, "", "")
	s.event("acct-1harbor", 2, authentities.EventTypeAccountMemberAdded, "agent-ops", authentities.RoleOwner)
	s.event("acct-1harbor", 3, authentities.EventTypeAccountMemberAdded, "agent-clerk", authentities.RoleMember)
	s.event("acct-1harbor", 4, authentities.EventTypeAccountMemberAdded, "agent-leaver", authentities.RoleMember)
	s.event("acct-1harbor", 5, authentities.EventTypeAccountMemberRemoved, "agent-leaver", "")

	s.row("acct-1harbor", "agent-ops", authentities.RoleOwner, at(0))      // the creator, as signed up
	s.row("acct-1harbor", "agent-stranger", authentities.RoleOwner, at(1)) // no tie at all
	s.row("acct-1harbor", "agent-invitee", authentities.RoleMember, at(2)) // an invite names their email
	s.row("acct-1harbor", "agent-clerk", authentities.RoleAdmin, at(3))    // joined as member, now admin
	s.row("acct-1harbor", "agent-leaver", authentities.RoleAdmin, at(4))   // removed, then written again
	s.row("acct-1harbor", "agent-promoted", authentities.RoleAdmin, at(5)) // invited as member, now admin
	s.row("acct-2cedar", "agent-stranger-2", authentities.RoleOwner, at(6))

	s.credential("agent-invitee", "Invitee@LanternHomes.example")
	s.invite("invite-1", "acct-1harbor", "invitee@lanternhomes.example", "", authentities.RoleMember)
	s.invite("invite-2", "acct-1harbor", "someone-else@lanternhomes.example", "agent-promoted", authentities.RoleMember)
	s.invite("invite-3", "acct-2cedar", "stranger@harborlegal.example", "agent-stranger", authentities.RoleOwner)

	before := s.membershipRows()
	audit := ProvideAccountMembershipAudit(s.db)

	first, err := audit.FirstAccountID(ctx)
	if err != nil {
		t.Fatalf("FirstAccountID: %v", err)
	}
	if first != "acct-1harbor" {
		t.Fatalf("FirstAccountID = %q, want acct-1harbor, the lowest id, as the old users route chose it", first)
	}

	report, err := audit.UnexplainedMemberships(ctx, "acct-1harbor")
	if err != nil {
		t.Fatalf("UnexplainedMemberships: %v", err)
	}
	if !report.AccountFound || !report.HistoryFound || report.Memberships != 6 {
		t.Errorf("report found=%v history=%v memberships=%d; want true, true, 6",
			report.AccountFound, report.HistoryFound, report.Memberships)
	}
	want := []repositories.UnexplainedMembership{
		{AgentID: "agent-stranger", RoleID: authentities.RoleOwner, WrittenAt: at(1)},
		{AgentID: "agent-clerk", RoleID: authentities.RoleAdmin, WrittenAt: at(3), RecordedRoleID: authentities.RoleMember},
		{AgentID: "agent-leaver", RoleID: authentities.RoleAdmin, WrittenAt: at(4)},
		{AgentID: "agent-promoted", RoleID: authentities.RoleAdmin, WrittenAt: at(5), RecordedRoleID: authentities.RoleMember},
	}
	if len(report.Unexplained) != len(want) {
		t.Fatalf("the audit listed %+v, want %+v", report.Unexplained, want)
	}
	for i, w := range want {
		got := report.Unexplained[i]
		if got.AgentID != w.AgentID || got.RoleID != w.RoleID || got.RecordedRoleID != w.RecordedRoleID || !got.WrittenAt.Equal(w.WrittenAt) {
			t.Errorf("listed %d = %+v, want %+v", i, got, w)
		}
	}

	after := s.membershipRows()
	if fmt.Sprint(after) != fmt.Sprint(before) {
		t.Errorf("the audit changed the memberships:\nbefore %v\nafter  %v", before, after)
	}
}

// wm-vycbd. An account with no recorded history cannot tell a sign-up from a
// role the old route wrote, so the report says so, and lists every membership
// no invite explains.
func TestAccountMembershipAuditSaysWhenTheAccountHasNoHistory(t *testing.T) {
	ctx := context.Background()
	s := newAuditStore(t)
	s.account("acct-bare")
	s.row("acct-bare", "agent-ops", authentities.RoleOwner, time.Now())
	s.row("acct-bare", "agent-invitee", authentities.RoleMember, time.Now())
	s.invite("invite-1", "acct-bare", "invitee@lanternhomes.example", "agent-invitee", authentities.RoleMember)

	report, err := ProvideAccountMembershipAudit(s.db).UnexplainedMemberships(ctx, "acct-bare")
	if err != nil {
		t.Fatalf("UnexplainedMemberships: %v", err)
	}
	if report.HistoryFound {
		t.Error("an account with no events reported a history")
	}
	if len(report.Unexplained) != 1 || report.Unexplained[0].AgentID != "agent-ops" {
		t.Errorf("the audit listed %+v, want only agent-ops", report.Unexplained)
	}
}

// wm-vycbd. An account that does not exist, and an instance with no account,
// are reported, not failed.
func TestAccountMembershipAuditReportsAMissingAccount(t *testing.T) {
	ctx := context.Background()
	s := newAuditStore(t)
	audit := ProvideAccountMembershipAudit(s.db)

	first, err := audit.FirstAccountID(ctx)
	if err != nil || first != "" {
		t.Errorf("FirstAccountID on an empty instance = %q, %v; want \"\", nil", first, err)
	}
	report, err := audit.UnexplainedMemberships(ctx, "acct-nobody")
	if err != nil {
		t.Fatalf("UnexplainedMemberships: %v", err)
	}
	if report.AccountFound || len(report.Unexplained) != 0 {
		t.Errorf("a missing account reported found=%v listed=%+v", report.AccountFound, report.Unexplained)
	}
}
