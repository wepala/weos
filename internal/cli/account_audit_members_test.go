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

package cli

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/internal/config"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/glebarez/sqlite"
	"go.uber.org/fx"
	gormlib "gorm.io/gorm"
)

// wm-vycbd. Before wm-govvg, the users routes saved role changes into the
// instance's first account for anybody. The operator audit lists those rows and
// changes nothing. These tests run the real command, in-process, against a
// store seeded through the real module: registrations record their history the
// way a production sign-up does, and the old route's rows are written the way
// it wrote them, with SaveMember and no event.

type auditPerson struct {
	email     string
	agentID   string
	accountID string
}

func membershipSnapshot(t *testing.T, dsn string) string {
	t.Helper()
	db, err := gormlib.Open(sqlite.Open(dsn), &gormlib.Config{})
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()
	var rows []struct{ AccountID, AgentID, RoleID string }
	if err := db.Table("account_members").Select("account_id, agent_id, role_id").
		Order("account_id, agent_id").Scan(&rows).Error; err != nil {
		t.Fatalf("read the memberships: %v", err)
	}
	return fmt.Sprint(rows)
}

func TestAccountAuditMembersListsTheFirstAccountsUnexplainedMemberships(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "weos.db")
	uploads := filepath.Join(dir, "uploads")
	t.Setenv("STORAGE_LOCAL_PATH", uploads)

	appCfg := config.Default()
	appCfg.DatabaseDSN = dsn
	appCfg.Storage.LocalPath = uploads
	appCfg.LogLevel = "error"
	var authService authapp.AuthenticationService
	var accounts authrepos.AccountRepository
	var db *gormlib.DB
	app := fx.New(fx.NopLogger, application.Module(appCfg, presets.NewDefaultRegistry()),
		fx.Populate(&authService, &accounts, &db))
	startCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		t.Fatalf("start the module: %v", err)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer stopCancel()
		_ = app.Stop(stopCtx)
	}
	t.Cleanup(stop)

	ctx := context.Background()
	var people []auditPerson
	for _, email := range []string{
		"ops@harborlegal.example", "counsel@cedarrealty.example",
		"clerk@lanternhomes.example", "paralegal@brightwaterlaw.example",
	} {
		agent, _, account, err := authService.RegisterPassword(ctx, email, strings.SplitN(email, "@", 2)[0], "correct-horse-battery-staple")
		if err != nil {
			t.Fatalf("register %s: %v", email, err)
		}
		people = append(people, auditPerson{email: email, agentID: agent.GetID(), accountID: account.GetID()})
	}
	first, err := accounts.FindAll(ctx, "", 1)
	if err != nil || len(first.Data) == 0 {
		t.Fatalf("find the first account: %v", err)
	}
	firstID := first.Data[0].GetID()
	var owner auditPerson
	var others []auditPerson
	for _, p := range people {
		if p.accountID == firstID {
			owner = p
		} else {
			others = append(others, p)
		}
	}
	if owner.agentID == "" || len(others) != 3 {
		t.Fatalf("none of the registrations created the first account %s", firstID)
	}
	stranger, invitee, promoted := others[0], others[1], others[2]

	// The rows the old users route left in the first account.
	for _, write := range []struct {
		who  auditPerson
		role string
	}{
		{stranger, authentities.RoleAdmin},
		{invitee, authentities.RoleMember},
		{promoted, authentities.RoleAdmin},
	} {
		if err := accounts.SaveMember(ctx, firstID, write.who.agentID, write.role); err != nil {
			t.Fatalf("write the old route's row for %s: %v", write.who.email, err)
		}
	}
	// An invite sent to the invitee's address, and one accepted by the
	// promoted person as a plain member.
	for _, inv := range []struct{ id, email, invitee string }{
		{"invite-brightwater", strings.ToUpper(invitee.email), ""},
		{"invite-promoted", "someone-else@lanternhomes.example", promoted.agentID},
	} {
		if err := db.Exec(`INSERT INTO invites
			(id, account_id, email, role_id, inviter_agent_id, invitee_agent_id, status, expires_at, accepted_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?)`,
			inv.id, firstID, inv.email, authentities.RoleMember, owner.agentID, inv.invitee,
			time.Now().Add(72*time.Hour), time.Time{}, time.Now()).Error; err != nil {
			t.Fatalf("seed invite %s: %v", inv.id, err)
		}
	}
	stop()
	before := membershipSnapshot(t, dsn)

	var out, errOut bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	rootCmd.SetArgs([]string{"account", "audit-members", "--database-dsn", dsn})
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		databaseDSN = ""
		cfg = nil
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("account audit-members failed: %v\nstdout:\n%s\nstderr:\n%s", err, out.String(), errOut.String())
	}
	said := out.String()

	lineFor := func(agentID string) string {
		for _, line := range strings.Split(said, "\n") {
			if strings.Contains(line, agentID) {
				return line
			}
		}
		return ""
	}
	if !strings.Contains(said, firstID) {
		t.Errorf("the audit does not name the account it read, %s:\n%s", firstID, said)
	}
	if line := lineFor(stranger.agentID); !strings.Contains(line, authentities.RoleAdmin) || !strings.Contains(line, "no sign-up or invite") {
		t.Errorf("the stranger's row is listed as %q; want their admin role and no sign-up or invite:\n%s", line, said)
	}
	if line := lineFor(promoted.agentID); !strings.Contains(line, authentities.RoleAdmin) || !strings.Contains(line, "recorded member") {
		t.Errorf("the promoted person's row is listed as %q; want admin against the recorded member role:\n%s", line, said)
	}
	for _, p := range []auditPerson{owner, invitee} {
		if line := lineFor(p.agentID); line != "" {
			t.Errorf("%s, whom a sign-up or an invite explains, is listed: %q", p.email, line)
		}
	}
	if strings.Contains(said, "@") || strings.Contains(errOut.String(), "@") {
		t.Errorf("the audit printed an email address:\nstdout:\n%s\nstderr:\n%s", said, errOut.String())
	}
	if after := membershipSnapshot(t, dsn); after != before {
		t.Errorf("the audit changed the memberships:\nbefore %s\nafter  %s", before, after)
	}
}

// wm-vycbd. The command's help says it changes nothing and tells an operator
// how to remove a row it lists.
func TestAccountAuditMembersHelpSaysHowToRemoveARow(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"account", "audit-members"})
	if err != nil || cmd == nil || cmd.Name() != "audit-members" {
		t.Fatalf("weos account audit-members is not a command (found %v, %v)", cmd, err)
	}
	for _, want := range []string{"DELETE FROM account_members", "changes nothing", "account_id", "agent_id"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("the help does not say %q:\n%s", want, cmd.Long)
		}
	}
}
