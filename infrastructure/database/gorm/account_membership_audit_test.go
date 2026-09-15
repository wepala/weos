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
	s.inviteAt(id, accountID, email, inviteeAgentID, roleID, time.Now())
}

// inviteAt seeds an invite sent at a given time, for tests that depend on the
// order the invites were sent in.
func (s *auditStore) inviteAt(id, accountID, email, inviteeAgentID, roleID string, sentAt time.Time) {
	s.t.Helper()
	if err := s.db.Create(&authmodels.InviteModel{
		ID: id, AccountID: accountID, Email: email, RoleID: roleID,
		InviterAgentID: "agent-ops", InviteeAgentID: inviteeAgentID, Status: "pending",
		ExpiresAt: sentAt.Add(72 * time.Hour), CreatedAt: sentAt,
	}).Error; err != nil {
		s.t.Fatalf("seed invite %s: %v", id, err)
	}
}

func (s *auditStore) credential(agentID, email string) {
	s.t.Helper()
	s.namedCredential(agentID+"-cred", agentID, email)
}

// namedCredential seeds a credential with its own id, for a person who holds
// more than one.
func (s *auditStore) namedCredential(id, agentID, email string) {
	s.t.Helper()
	if err := s.db.Create(&authmodels.CredentialModel{
		ID: id, AgentID: agentID, Provider: "password", ProviderUserID: id,
		Email: email, Active: true, CreatedAt: time.Now(),
	}).Error; err != nil {
		s.t.Fatalf("seed credential %s of %s: %v", id, agentID, err)
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

// wm-govvg. The audit compares an invite's email to a member's credential under
// owner binding's one rule, repositories.FoldCredentialEmail: spaces trimmed and
// ASCII capitals lower-cased, nothing else. A Unicode fold is looser than any
// path that writes a membership, so it would count as explained a membership
// whose address only folds to the invited one — U+212A KELVIN SIGN lower-cases
// to "k" — and leave it off the list a person reviews.
func TestAccountMembershipAuditFoldsEmailsTheWayOwnerBindingDoes(t *testing.T) {
	ctx := context.Background()
	s := newAuditStore(t)
	base := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	at := func(hours int) time.Time { return base.Add(time.Duration(hours) * time.Hour) }

	s.account("acct-1harbor")
	s.event("acct-1harbor", 1, authentities.EventTypeAccountCreated, "", "")
	s.event("acct-1harbor", 2, authentities.EventTypeAccountMemberAdded, "agent-ops", authentities.RoleOwner)

	s.row("acct-1harbor", "agent-ops", authentities.RoleOwner, at(0))
	s.row("acct-1harbor", "agent-kelvin", authentities.RoleMember, at(1)) // folds to the invite only under Unicode
	s.row("acct-1harbor", "agent-spaced", authentities.RoleMember, at(2)) // differs in ASCII case and spaces

	s.credential("agent-kelvin", "\u212Aim@harborlegal.example")
	s.credential("agent-spaced", "  Spaced@HarborLegal.example ")
	s.invite("invite-kim", "acct-1harbor", "kim@harborlegal.example", "", authentities.RoleMember)
	s.invite("invite-spaced", "acct-1harbor", "spaced@harborlegal.example", "", authentities.RoleMember)

	report, err := ProvideAccountMembershipAudit(s.db).UnexplainedMemberships(ctx, "acct-1harbor")
	if err != nil {
		t.Fatalf("UnexplainedMemberships: %v", err)
	}
	if len(report.Unexplained) != 1 || report.Unexplained[0].AgentID != "agent-kelvin" ||
		report.Unexplained[0].RoleID != authentities.RoleMember || !report.Unexplained[0].WrittenAt.Equal(at(1)) {
		t.Fatalf("the audit listed %+v; want only agent-kelvin, as member, written at %v", report.Unexplained, at(1))
	}
}

// wm-govvg. A person can hold credentials for two addresses that were each
// invited with a different role. The later invite's role wins, as it does for
// one address invited twice, whatever order the credentials are stored in. Two
// people show both orders: one whose credential for the later invite was stored
// first, and one whose credential for the earlier invite was.
func TestAccountMembershipAuditTakesTheLaterInviteAcrossAPersonsAddresses(t *testing.T) {
	ctx := context.Background()
	s := newAuditStore(t)
	base := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	at := func(hours int) time.Time { return base.Add(time.Duration(hours) * time.Hour) }

	s.account("acct-1harbor")
	s.event("acct-1harbor", 1, authentities.EventTypeAccountCreated, "", "")
	s.event("acct-1harbor", 2, authentities.EventTypeAccountMemberAdded, "agent-ops", authentities.RoleOwner)

	s.row("acct-1harbor", "agent-ops", authentities.RoleOwner, at(0))
	s.row("acct-1harbor", "agent-avery", authentities.RoleAdmin, at(1))
	s.row("acct-1harbor", "agent-blake", authentities.RoleAdmin, at(2))

	s.inviteAt("invite-1", "acct-1harbor", "avery@harborlegal.example", "", authentities.RoleMember, at(3))
	s.inviteAt("invite-2", "acct-1harbor", "avery@cedarrealty.example", "", authentities.RoleAdmin, at(4))
	s.inviteAt("invite-3", "acct-1harbor", "blake@harborlegal.example", "", authentities.RoleMember, at(5))
	s.inviteAt("invite-4", "acct-1harbor", "blake@cedarrealty.example", "", authentities.RoleAdmin, at(6))

	// Avery's credential for the later invite is stored first; Blake's for the
	// earlier invite is.
	s.namedCredential("cred-avery-cedar", "agent-avery", "avery@cedarrealty.example")
	s.namedCredential("cred-avery-harbor", "agent-avery", "avery@harborlegal.example")
	s.namedCredential("cred-blake-harbor", "agent-blake", "blake@harborlegal.example")
	s.namedCredential("cred-blake-cedar", "agent-blake", "blake@cedarrealty.example")

	report, err := ProvideAccountMembershipAudit(s.db).UnexplainedMemberships(ctx, "acct-1harbor")
	if err != nil {
		t.Fatalf("UnexplainedMemberships: %v", err)
	}
	if len(report.Unexplained) != 0 {
		t.Fatalf("the audit listed %+v; want nothing, since each admin role is the role of the later invite", report.Unexplained)
	}
}

// wm-govvg, Copilot review 5205110975. The credentials of the members nothing
// else explains are read in batches, not in one IN list with a parameter per
// member: an account larger than SQLite's limit of 32766 parameters would make
// the audit fail instead of report. Two people whose credentials name an
// invited address sit in the first batch and in the last, so every batch's
// credentials must reach the analysis.
func TestAccountMembershipAuditReadsTheCredentialsOfALargeAccountInBatches(t *testing.T) {
	ctx := context.Background()
	s := newAuditStore(t)
	const members = 33000
	writtenAt := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)

	s.account("acct-1harbor")
	rows := make([]authmodels.AccountMemberModel, 0, members)
	for i := range members {
		rows = append(rows, authmodels.AccountMemberModel{
			AccountID: "acct-1harbor", AgentID: fmt.Sprintf("agent-%05d", i), RoleID: authentities.RoleMember, CreatedAt: writtenAt,
		})
	}
	if err := s.db.CreateInBatches(rows, 500).Error; err != nil {
		t.Fatalf("seed %d memberships: %v", members, err)
	}
	s.invite("invite-1", "acct-1harbor", "counsel@cedarrealty.example", "", authentities.RoleMember)
	first, last := "agent-00000", fmt.Sprintf("agent-%05d", members-1)
	s.namedCredential("cred-first", first, "counsel@cedarrealty.example")
	s.namedCredential("cred-last", last, "counsel@cedarrealty.example")

	report, err := ProvideAccountMembershipAudit(s.db).UnexplainedMemberships(ctx, "acct-1harbor")
	if err != nil {
		t.Fatalf("UnexplainedMemberships over %d members: %v", members, err)
	}
	if len(report.Unexplained) != members-2 {
		t.Fatalf("the audit listed %d memberships; want %d, all but the two an invited address explains", len(report.Unexplained), members-2)
	}
	for _, m := range report.Unexplained {
		if m.AgentID == first || m.AgentID == last {
			t.Errorf("the audit listed %s, whose credential names the invited address", m.AgentID)
		}
	}
}
