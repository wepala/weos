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

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	gormprov "github.com/wepala/weos/v3/infrastructure/database/gorm"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authmodels "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/models"
	"gorm.io/gorm"
)

// This file pins the account adoption (wm-c7xehr): an account with no
// account_members row can never be signed in to, so what it owns is out of
// reach of every account-scoped path, and even its creator's own deletes are
// refused unless that agent still has a credential. `weos maintenance
// adopt-account` moves each of its resources to a named account with one
// Resource.AccountAdopted, compare-and-set on the source account.

const (
	orphanAccountID   = "acct-test-orphaned"
	orphanAccountName = "Old local twin"
	orphanWriter      = "agent-test-orphan-writer"
)

type adoptFixture struct {
	orphanTask1, orphanTask2, orphanProject string // in the orphaned account
	othersTask                              string // in the other agent's account
	ownersTask                              string // in the target account already
}

// seedOrphan creates an account with no members, and resources in it written
// by an agent that is a member of nothing — the shape the pre-cutover twin
// left behind. It also writes one resource in each seeded account.
func (env *assignOwnerEnv) seedOrphan(t *testing.T) adoptFixture {
	t.Helper()
	account, err := new(authentities.Account).With(orphanAccountID, orphanAccountName, authentities.AccountTypePersonal)
	if err != nil {
		t.Fatalf("orphaned account: %v", err)
	}
	if err := env.accountRepo.Save(context.Background(), account); err != nil {
		t.Fatalf("save the orphaned account: %v", err)
	}
	return adoptFixture{
		orphanTask1:   env.create(t, orphanWriter, orphanAccountID, "task", "Reconcile May statement"),
		orphanTask2:   env.create(t, orphanWriter, orphanAccountID, "task", "Reconcile June statement"),
		orphanProject: env.create(t, orphanWriter, orphanAccountID, "project", "Bank imports"),
		othersTask:    env.create(t, env.otherAgent, env.otherAccount, "task", "Someone else's task"),
		ownersTask:    env.create(t, env.ownerAgent, env.ownerAccount, "task", "Owned task"),
	}
}

func (env *assignOwnerEnv) adoptCommand(dryRun bool) application.AdoptAccountCommand {
	return application.AdoptAccountCommand{
		FromAccountID: orphanAccountID, AccountID: env.ownerAccount, AgentID: env.ownerAgent, DryRun: dryRun,
	}
}

func (env *assignOwnerEnv) adoptedEvents(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := env.db.Table("events").Where("event_type = ?", "Resource.AccountAdopted").Count(&n).Error; err != nil {
		t.Fatalf("count adoption events: %v", err)
	}
	return n
}

func (env *assignOwnerEnv) requireAdopted(t *testing.T, f adoptFixture, when string) {
	t.Helper()
	env.requireOwner(t, "task", f.orphanTask1, env.ownerAccount, when)
	env.requireOwner(t, "task", f.orphanTask2, env.ownerAccount, when)
	env.requireOwner(t, "project", f.orphanProject, env.ownerAccount, when)
	env.requireOwner(t, "task", f.othersTask, env.otherAccount, when+" (another account's resource)")
	env.requireOwner(t, "task", f.ownersTask, env.ownerAccount, when+" (the target's own resource)")
}

func TestAdoptAccount_PreviewCountsPerTypeAndWritesNothing(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seedOrphan(t)
	before := env.eventCount(t)

	report, err := env.adoption.AdoptAccount(context.Background(), env.adoptCommand(true))
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !report.DryRun {
		t.Error("the report does not say it was a preview")
	}
	if report.FromAccountName != orphanAccountName || report.AccountName != env.accountName(t, env.ownerAccount) {
		t.Errorf("names = (%q, %q), want the two accounts' names", report.FromAccountName, report.AccountName)
	}
	if report.FromMembers != 0 || report.AgentRole == "" {
		t.Errorf("member check = %d members, agent role %q", report.FromMembers, report.AgentRole)
	}
	if got := report.Types["task"]; got.ToMove != 2 || got.Moved != 0 {
		t.Errorf("task counts = %+v, want 2 to move and 0 moved", got)
	}
	if got := report.Types["project"]; got.ToMove != 1 || got.Moved != 0 {
		t.Errorf("project counts = %+v, want 1 to move and 0 moved", got)
	}
	if report.ToMove() != 3 {
		t.Errorf("total to move = %d, want 3", report.ToMove())
	}
	if after := env.eventCount(t); after != before {
		t.Errorf("a preview wrote %d event(s)", after-before)
	}
	env.requireOwner(t, "task", f.orphanTask1, orphanAccountID, "after the preview")
}

func TestAdoptAccount_MovesEveryResourceOfTheOrphanedAccountAndNothingElse(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seedOrphan(t)

	report, err := env.adoption.AdoptAccount(context.Background(), env.adoptCommand(false))
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if report.DryRun {
		t.Error("a real run reports itself as a preview")
	}
	if got := report.Types["task"]; got.ToMove != 2 || got.Moved != 2 {
		t.Errorf("task counts = %+v, want 2 to move and 2 moved", got)
	}
	if got := report.Types["project"]; got.ToMove != 1 || got.Moved != 1 {
		t.Errorf("project counts = %+v, want 1 to move and 1 moved", got)
	}
	if len(report.NeedsReproject) != 0 || len(report.ForeignRows) != 0 {
		t.Errorf("left %+v for a reproject and %+v with a foreign row", report.NeedsReproject, report.ForeignRows)
	}
	if n := env.adoptedEvents(t); n != 3 {
		t.Errorf("stored %d Resource.AccountAdopted event(s), want 3", n)
	}
	if report.Granted() != 3 {
		t.Errorf("granted read on %d resource(s), want 3", report.Granted())
	}
	env.requireAdopted(t, f, "after the move")
	// The source account is not deleted.
	if a, err := env.accountRepo.FindByID(context.Background(), orphanAccountID); err != nil || a == nil {
		t.Errorf("the source account is gone after the move (%v)", err)
	}
}

func TestAdoptAccount_ARerunMovesNothing(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seedOrphan(t)
	if _, err := env.adoption.AdoptAccount(context.Background(), env.adoptCommand(false)); err != nil {
		t.Fatalf("move: %v", err)
	}
	before := env.eventCount(t)

	report, err := env.adoption.AdoptAccount(context.Background(), env.adoptCommand(false))
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if report.ToMove() != 0 || report.Moved() != 0 || report.RowsToMove() != 0 {
		t.Errorf("second run = %d to move, %d moved, %d rows to move; want 0",
			report.ToMove(), report.Moved(), report.RowsToMove())
	}
	if after := env.eventCount(t); after != before {
		t.Errorf("a second run wrote %d event(s)", after-before)
	}
	env.requireAdopted(t, f, "after the second run")
}

func TestAdoptAccount_RefusesAnAccountWithAMemberAndEveryBadArgument(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seedOrphan(t)
	before := env.eventCount(t)
	ctx := context.Background()

	for _, c := range []struct {
		name string
		cmd  application.AdoptAccountCommand
		want error
	}{
		{"an unknown source", application.AdoptAccountCommand{FromAccountID: "acct-test-missing",
			AccountID: env.ownerAccount, AgentID: env.ownerAgent}, application.ErrAdoptSourceNotFound},
		{"an unknown target", application.AdoptAccountCommand{FromAccountID: orphanAccountID,
			AccountID: "acct-test-missing", AgentID: env.ownerAgent}, application.ErrAdoptTargetNotFound},
		{"the same account", application.AdoptAccountCommand{FromAccountID: orphanAccountID,
			AccountID: orphanAccountID, AgentID: env.ownerAgent}, application.ErrAdoptSameAccount},
		{"an agent that is not in the target", application.AdoptAccountCommand{FromAccountID: orphanAccountID,
			AccountID: env.ownerAccount, AgentID: env.otherAgent}, application.ErrAdoptAgentNotAdmin},
		// The other account has its agent as a member, so it is not orphaned.
		{"a source with a member", application.AdoptAccountCommand{FromAccountID: env.otherAccount,
			AccountID: env.ownerAccount, AgentID: env.ownerAgent}, application.ErrAdoptSourceHasMembers},
	} {
		for _, dryRun := range []bool{true, false} {
			c.cmd.DryRun = dryRun
			if _, err := env.adoption.AdoptAccount(ctx, c.cmd); !errors.Is(err, c.want) {
				t.Errorf("%s (preview %v): err = %v, want %v", c.name, dryRun, err, c.want)
			}
		}
	}
	// A plain member of the target is not enough: the move hands the target
	// everything the source owns.
	if err := env.accountRepo.SaveMember(ctx, env.ownerAccount, env.otherAgent, authentities.RoleMember); err != nil {
		t.Fatal(err)
	}
	_, err := env.adoption.AdoptAccount(ctx, application.AdoptAccountCommand{
		FromAccountID: orphanAccountID, AccountID: env.ownerAccount, AgentID: env.otherAgent,
	})
	if !errors.Is(err, application.ErrAdoptAgentNotAdmin) {
		t.Errorf("a plain member: err = %v, want ErrAdoptAgentNotAdmin", err)
	}
	for _, cmd := range []application.AdoptAccountCommand{
		{AccountID: env.ownerAccount, AgentID: env.ownerAgent},
		{FromAccountID: orphanAccountID, AgentID: env.ownerAgent},
		{FromAccountID: orphanAccountID, AccountID: env.ownerAccount},
	} {
		if _, err := env.adoption.AdoptAccount(ctx, cmd); err == nil {
			t.Errorf("command %+v with a missing value succeeded", cmd)
		}
	}
	if after := env.eventCount(t); after != before {
		t.Errorf("a refused run wrote %d event(s)", after-before)
	}
	env.requireOwner(t, "task", f.orphanTask1, orphanAccountID, "after the refused runs")
	env.requireOwner(t, "task", f.othersTask, env.otherAccount, "after the refused runs")
}

// A rebuild of the read model from the event stream keeps the new owner:
// once over the existing rows, and once from empty tables.
func TestAdoptAccount_ARebuildFromEventsKeepsTheNewOwner(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seedOrphan(t)
	if _, err := env.adoption.AdoptAccount(context.Background(), env.adoptCommand(false)); err != nil {
		t.Fatalf("move: %v", err)
	}
	tables := []string{"resources", env.projMgr.TableName("task"), env.projMgr.TableName("project")}
	env.stop()

	env.reproject(t, "rebuild over existing rows")
	env.requireAdopted(t, f, "rebuild over existing rows")

	for _, table := range tables {
		if err := env.db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("empty %s: %v", table, err)
		}
	}
	env.reproject(t, "rebuild from empty tables")
	env.requireAdopted(t, f, "rebuild from empty tables")
}

// The reason for the command: before the move nobody but a write with no
// identity can delete a resource of the orphaned account. After it, the
// target account's owner can.
func TestAdoptAccount_TheTargetsOwnerCanDeleteAMovedResource(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seedOrphan(t)
	ownerCtx := auth.ContextWithAgent(context.Background(), &auth.Identity{
		AgentID: env.ownerAgent, AccountIDs: []string{env.ownerAccount}, ActiveAccountID: env.ownerAccount,
	})

	err := env.resources.Delete(ownerCtx, application.DeleteResourceCommand{ID: f.orphanTask1})
	if !errors.Is(err, entities.ErrAccessDenied) {
		t.Fatalf("delete before the move: err = %v, want ErrAccessDenied", err)
	}
	if _, err := env.adoption.AdoptAccount(context.Background(), env.adoptCommand(false)); err != nil {
		t.Fatalf("move: %v", err)
	}
	if err := env.resources.Delete(ownerCtx, application.DeleteResourceCommand{ID: f.orphanTask1}); err != nil {
		t.Fatalf("delete after the move: %v", err)
	}
	if _, err := env.resourceRepo.FindByID(context.Background(), f.orphanTask1); err == nil {
		t.Fatal("the moved resource is still there after its delete")
	}
	// The other agent still cannot delete what moved to the owner's account.
	otherCtx := auth.ContextWithAgent(context.Background(), &auth.Identity{
		AgentID: env.otherAgent, AccountIDs: []string{env.otherAccount}, ActiveAccountID: env.otherAccount,
	})
	if err := env.resources.Delete(otherCtx, application.DeleteResourceCommand{ID: f.orphanTask2}); !errors.Is(err,
		entities.ErrAccessDenied) {
		t.Fatalf("delete by another account's agent: err = %v, want ErrAccessDenied", err)
	}
}

// A move whose projection write did not reach a row leaves the record in the
// target and the row in the source. A re-run moves the row, with no second
// event.
func TestAdoptAccount_ARowLeftInTheSourceIsMovedByARerun(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seedOrphan(t)
	if _, err := env.adoption.AdoptAccount(context.Background(), env.adoptCommand(false)); err != nil {
		t.Fatalf("move: %v", err)
	}
	taskTable := env.projMgr.TableName("task")
	if err := env.db.Table(taskTable).Where("id = ?", f.orphanTask2).
		Update("account_id", orphanAccountID).Error; err != nil {
		t.Fatal(err)
	}
	events := env.adoptedEvents(t)

	plan, err := env.adoption.AdoptAccount(context.Background(), env.adoptCommand(true))
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if got := plan.Types["task"]; got.ToMove != 0 || got.RowsToMove != 1 {
		t.Errorf("preview task counts = %+v, want 0 to move and 1 row to move", got)
	}
	report, err := env.adoption.AdoptAccount(context.Background(), env.adoptCommand(false))
	if err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if got := report.Types["task"]; got.RowsMoved != 1 || got.Moved != 0 {
		t.Errorf("re-run task counts = %+v, want 1 row moved and no resource moved", got)
	}
	if n := env.adoptedEvents(t); n != events {
		t.Errorf("the re-run stored %d more adoption event(s), want none", n-events)
	}
	env.requireAdopted(t, f, "after the re-run")
}

// Copilot 4580310679: a run interrupted after the record write leaves the
// record in the target and each projection row as it was before the move. A
// row that was ownerless is still ownerless, and a row that was missing is
// still missing, so neither is in the source account any more. A re-run must
// still find both, check that the stored history holds the move, and fill or
// write the rows with no second event. The target's own resources with an
// ownerless or missing row are not the run's business and are not touched.
func TestAdoptAccount_ARerunRepairsOwnerlessAndMissingRowsAfterAnInterruptedRun(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seedOrphan(t)
	ctx := context.Background()
	if _, err := env.adoption.AdoptAccount(ctx, env.adoptCommand(false)); err != nil {
		t.Fatalf("move: %v", err)
	}
	// The state a run stopped right after the record write leaves, for a
	// task whose row had no owner and one that had no row at all.
	taskTable := env.projMgr.TableName("task")
	if err := env.db.Table(taskTable).Where("id = ?", f.orphanTask1).Update("account_id", nil).Error; err != nil {
		t.Fatal(err)
	}
	if err := env.db.Table(taskTable).Where("id = ?", f.orphanTask2).Delete(map[string]any{}).Error; err != nil {
		t.Fatal(err)
	}
	// The target's own task, ownerless and missing in turn: no move from
	// the source is in its history.
	ownersBlank := env.create(t, env.ownerAgent, env.ownerAccount, "task", "Owned and blank")
	if err := env.db.Table(taskTable).Where("id = ?", ownersBlank).Update("account_id", "").Error; err != nil {
		t.Fatal(err)
	}
	if err := env.db.Table(taskTable).Where("id = ?", f.ownersTask).Delete(map[string]any{}).Error; err != nil {
		t.Fatal(err)
	}
	events := env.eventCount(t)

	plan, err := env.adoption.AdoptAccount(ctx, env.adoptCommand(true))
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if got := plan.Types["task"]; got.ToMove != 0 || got.RowsToMove != 2 {
		t.Errorf("preview task counts = %+v, want 0 to move and 2 rows to repair", got)
	}
	report, err := env.adoption.AdoptAccount(ctx, env.adoptCommand(false))
	if err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if got := report.Types["task"]; got.RowsMoved != 2 || got.Moved != 0 || got.NeedsReproject != 0 {
		t.Errorf("re-run task counts = %+v, want 2 rows repaired and nothing else", got)
	}
	if n := env.eventCount(t); n != events {
		t.Errorf("the re-run stored %d event(s), want none", n-events)
	}
	env.requireOwner(t, "task", f.orphanTask1, env.ownerAccount, "the ownerless row after the re-run")
	env.requireOwner(t, "task", f.orphanTask2, env.ownerAccount, "the missing row after the re-run")
	if _, projection := env.owners(t, "task", ownersBlank); projection != "" {
		t.Errorf("the target's own ownerless row was given %q", projection)
	}
	var n int64
	if err := env.db.Table(taskTable).Where("id = ?", f.ownersTask).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("the target's own missing row was written")
	}
}

// A projection row of a third account under a record of the orphaned
// account is listed, never moved and never counted as moved.
func TestAdoptAccount_ARowOfAThirdAccountIsListedAndNotMoved(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seedOrphan(t)
	taskTable := env.projMgr.TableName("task")
	if err := env.db.Table(taskTable).Where("id = ?", f.orphanTask2).
		Update("account_id", env.otherAccount).Error; err != nil {
		t.Fatal(err)
	}

	report, err := env.adoption.AdoptAccount(context.Background(), env.adoptCommand(false))
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if got := report.Types["task"]; got.Moved != 1 || got.ForeignRows != 1 {
		t.Errorf("task counts = %+v, want 1 moved and 1 with a foreign row", got)
	}
	if len(report.ForeignRows) != 1 || report.ForeignRows[0].ID != f.orphanTask2 ||
		report.ForeignRows[0].RowAccountID != env.otherAccount {
		t.Fatalf("foreign rows = %+v, want %s with a row of the other account", report.ForeignRows, f.orphanTask2)
	}
	canonical, projection := env.owners(t, "task", f.orphanTask2)
	if canonical != orphanAccountID || projection != env.otherAccount {
		t.Errorf("owners of the listed resource = (%q, %q), want both left as they were", canonical, projection)
	}
	env.requireOwner(t, "task", f.orphanTask1, env.ownerAccount, "the other task")
}

// The operator command, as the shipped binary runs it against a stopped
// instance's store.
func TestAdoptAccount_TheCommandPreviewsThenMovesWithWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the weos binary")
	}
	env := newAssignOwnerEnv(t)
	f := env.seedOrphan(t)
	ownerName := env.accountName(t, env.ownerAccount)

	help, _, code := env.runWeos(t, "maintenance", "adopt-account", "--help")
	if code != 0 || !strings.Contains(help, "Stop the instance for the real run") {
		t.Fatalf("adopt-account --help (exit %d) does not say to stop the instance:\n%s", code, help)
	}
	env.stop()
	db, err := gorm.Open(gormprov.DialectorForDSN(env.cfg.DatabaseDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	env.db = db

	args := []string{"maintenance", "adopt-account", "--from", orphanAccountID,
		"--account", env.ownerAccount, "--agent", env.ownerAgent}
	stdout, stderr, code := env.runWeos(t, args...)
	if code != 0 {
		t.Fatalf("preview exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	for _, want := range []string{"DRY RUN", orphanAccountName, ownerName, "0 members", "0 uploaded files",
		"project", "1 to move", "task", "2 to move", "total: 3 resource(s) would be moved"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("preview output lacks %q:\n%s", want, stdout)
		}
	}
	env.requireOwner(t, "task", f.orphanTask1, orphanAccountID, "after the command's preview")

	stdout, stderr, code = env.runWeos(t, append(args, "--write")...)
	if code != 0 {
		t.Fatalf("real run exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	for _, want := range []string{"total: 3 of 3 resource(s) moved", "safe to re-run", "project 1 of 1",
		"task 2 of 2", "granted " + env.ownerAgent + " read on 3 resource(s)", "updated_at"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("real run output lacks %q:\n%s", want, stdout)
		}
	}
	env.requireAdopted(t, f, "after the command")

	// An account with a member is refused, non-zero, and names the rule.
	_, stderr, code = env.runWeos(t, "maintenance", "adopt-account", "--from", env.otherAccount,
		"--account", env.ownerAccount, "--agent", env.ownerAgent, "--write")
	if code == 0 || !strings.Contains(stderr, "has 1 member") {
		t.Errorf("a source with a member exited %d, stderr:\n%s", code, stderr)
	}
	env.requireOwner(t, "task", f.othersTask, env.otherAccount, "after the refused command")
}

func (env *assignOwnerEnv) streamLength(t *testing.T, aggregateID string) int64 {
	t.Helper()
	var n int64
	if err := env.db.Table("events").Where("aggregate_id = ?", aggregateID).Count(&n).Error; err != nil {
		t.Fatalf("count events of %s: %v", aggregateID, err)
	}
	return n
}

// wm-myzy4t: once the orphaned account is empty, the operator's next step is
// to erase it. Every moved resource's Resource.Created still names that
// account, and the erasure used to delete every stream that named it. The
// moved resources, their whole history and a rebuild from it all survive.
func TestAdoptAccount_ErasingTheEmptiedSourceKeepsWhatMoved(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seedOrphan(t)
	ctx := context.Background()
	if _, err := env.adoption.AdoptAccount(ctx, env.adoptCommand(false)); err != nil {
		t.Fatalf("move: %v", err)
	}
	moved := map[string]string{f.orphanTask1: "task", f.orphanTask2: "task", f.orphanProject: "project"}
	streams := map[string]int64{}
	for id := range moved {
		streams[id] = env.streamLength(t, id)
	}

	if _, err := env.erasure.Erase(ctx, application.EraseAccountCommand{
		AccountID: orphanAccountID, RequestedBy: env.ownerAgent, SkipDrain: true,
	}); err != nil {
		t.Fatalf("erase the emptied source: %v", err)
	}
	if a, err := env.accountRepo.FindByID(ctx, orphanAccountID); err == nil && a != nil {
		t.Fatal("the source account is still there after its erasure")
	}
	for id, typeSlug := range moved {
		if got := env.streamLength(t, id); got != streams[id] {
			t.Errorf("%s has %d of its %d events after the source was erased", id, got, streams[id])
		}
		env.requireOwner(t, typeSlug, id, env.ownerAccount, "after the source was erased")
	}
	env.requireOwner(t, "task", f.othersTask, env.otherAccount, "after the source was erased")

	env.stop()
	env.reproject(t, "rebuild after the source was erased")
	env.requireAdopted(t, f, "rebuild after the source was erased")
}

// display reads the display column of a reference on a resource's row in its
// type's projection table.
func (env *assignOwnerEnv) display(t *testing.T, typeSlug, id, column string) string {
	t.Helper()
	var row struct{ Label *string }
	if err := env.db.Table(env.projMgr.TableName(typeSlug)).Select(column+" AS label").
		Where("id = ?", id).Scan(&row).Error; err != nil {
		t.Fatalf("read %s of %s: %v", column, id, err)
	}
	if row.Label == nil {
		return ""
	}
	return *row.Label
}

// wm-xuskm9: the run moves resources in type and id order, so a resource
// often moves while a resource it references is still in the source
// account — a note whose parent note has a higher id, a chore whose task's
// type sorts later. The move must keep the label of that reference, and so
// must every move after it.
func TestAdoptAccount_KeepsTheLabelsOfReferencesToResourcesNotMovedYet(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seedOrphan(t)
	ctx := context.Background()
	for slug, schema := range map[string]string{
		"note": `{"type":"object","properties":{"name":{"type":"string"},` +
			`"parent":{"type":"string","x-resource-type":"note","x-display-property":"name"}},"required":["name"]}`,
		"chore": `{"type":"object","properties":{"name":{"type":"string"},` +
			`"task":{"type":"string","x-resource-type":"task","x-display-property":"name"}},"required":["name"]}`,
	} {
		if _, err := env.types.Create(ctx, application.CreateResourceTypeCommand{
			Name: slug, Slug: slug, Description: "A type with a reference, for the adoption",
			Context: json.RawMessage(`{"@vocab":"https://schema.org/"}`), Schema: json.RawMessage(schema),
		}); err != nil {
			t.Fatalf("create type %s: %v", slug, err)
		}
	}
	// Two notes; the one with the lower id names the other as its parent.
	a := env.create(t, orphanWriter, orphanAccountID, "note", "Statement notes")
	b := env.create(t, orphanWriter, orphanAccountID, "note", "Banking notes")
	lo, hi := a, b
	if hi < lo {
		lo, hi = hi, lo
	}
	writer := auth.ContextWithAgent(ctx, &auth.Identity{
		AgentID: orphanWriter, AccountIDs: []string{orphanAccountID}, ActiveAccountID: orphanAccountID,
	})
	hiName := map[string]string{a: "Statement notes", b: "Banking notes"}[hi]
	loName := map[string]string{a: "Statement notes", b: "Banking notes"}[lo]
	if _, err := env.resources.Update(writer, application.UpdateResourceCommand{
		ID: lo, Data: json.RawMessage(fmt.Sprintf(`{"name":%q,"parent":%q}`, loName, hi)),
	}); err != nil {
		t.Fatalf("give the note a parent: %v", err)
	}
	chore := env.create(t, orphanWriter, orphanAccountID, "chore", "File the May statement")
	if _, err := env.resources.Update(writer, application.UpdateResourceCommand{
		ID: chore, Data: json.RawMessage(fmt.Sprintf(`{"name":"File the May statement","task":%q}`, f.orphanTask1)),
	}); err != nil {
		t.Fatalf("give the chore a task: %v", err)
	}
	if got := env.display(t, "note", lo, "parent_display"); got != hiName {
		t.Fatalf("before the move the note's parent label = %q, want %q", got, hiName)
	}
	if got := env.display(t, "chore", chore, "task_display"); got != "Reconcile May statement" {
		t.Fatalf("before the move the chore's task label = %q", got)
	}

	if _, err := env.adoption.AdoptAccount(ctx, env.adoptCommand(false)); err != nil {
		t.Fatalf("move: %v", err)
	}
	env.requireOwner(t, "note", lo, env.ownerAccount, "after the move")
	env.requireOwner(t, "chore", chore, env.ownerAccount, "after the move")
	if got := env.display(t, "note", lo, "parent_display"); got != hiName {
		t.Errorf("after the move the note's parent label = %q, want %q", got, hiName)
	}
	if got := env.display(t, "chore", chore, "task_display"); got != "Reconcile May statement" {
		t.Errorf("after the move the chore's task label = %q, want %q", got, "Reconcile May statement")
	}
}

// invite writes an invitation into account, as pericarp stores one.
func (env *assignOwnerEnv) invite(t *testing.T, id, account, status string, expiresAt time.Time) {
	t.Helper()
	if err := env.db.Create(&authmodels.InviteModel{
		ID: id, AccountID: account, Email: id + "@example.com", RoleID: authentities.RoleMember,
		InviterAgentID: orphanWriter, InviteeAgentID: "agent-test-" + id, Status: status, ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("invite %s: %v", id, err)
	}
}

// wm-m0ci05: an account with no member but a pending, unexpired invitation
// is still somebody's: accepting the invitation makes a member. It is refused,
// in the preview and in a real run. An expired, accepted or revoked
// invitation does not hold the account.
func TestAdoptAccount_RefusesASourceWithAPendingInvite(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seedOrphan(t)
	ctx := context.Background()
	env.invite(t, "inv-expired", orphanAccountID, "pending", time.Now().Add(-time.Hour))
	env.invite(t, "inv-accepted", orphanAccountID, "accepted", time.Now().Add(time.Hour))
	env.invite(t, "inv-revoked", orphanAccountID, "revoked", time.Now().Add(time.Hour))
	plan, err := env.adoption.AdoptAccount(ctx, env.adoptCommand(true))
	if err != nil {
		t.Fatalf("preview with only spent invitations: %v", err)
	}
	if plan.FromInvites != 0 {
		t.Errorf("pending invitations = %d, want 0 (expired, accepted and revoked do not count)", plan.FromInvites)
	}

	env.invite(t, "inv-pending", orphanAccountID, "pending", time.Now().Add(24*time.Hour))
	before := env.eventCount(t)
	for _, dryRun := range []bool{true, false} {
		cmd := env.adoptCommand(dryRun)
		report, err := env.adoption.AdoptAccount(ctx, cmd)
		if !errors.Is(err, application.ErrAdoptSourceHasInvites) {
			t.Errorf("preview %v: err = %v, want ErrAdoptSourceHasInvites", dryRun, err)
		}
		if report.FromInvites != 1 {
			t.Errorf("preview %v: pending invitations = %d, want 1", dryRun, report.FromInvites)
		}
	}
	if after := env.eventCount(t); after != before {
		t.Errorf("a refused run wrote %d event(s)", after-before)
	}
	env.requireOwner(t, "task", f.orphanTask1, orphanAccountID, "after the refused run")
}

// wm-m0ci05: the checks are read again when the real run starts. A member or
// a pending invitation that arrived after the preview refuses the run, and
// nothing moves.
func TestAdoptAccount_TheRunChecksTheSourceAgainBeforeItMovesAnything(t *testing.T) {
	ctx := context.Background()
	for name, arrive := range map[string]func(env *assignOwnerEnv, t *testing.T) error{
		"a member": func(env *assignOwnerEnv, _ *testing.T) error {
			return env.accountRepo.SaveMember(ctx, orphanAccountID, env.otherAgent, authentities.RoleMember)
		},
		"a pending invitation": func(env *assignOwnerEnv, t *testing.T) error {
			env.invite(t, "inv-late", orphanAccountID, "pending", time.Now().Add(time.Hour))
			return nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			env := newAssignOwnerEnv(t)
			f := env.seedOrphan(t)
			plan, err := env.adoption.PlanAdoptAccount(ctx, env.adoptCommand(false))
			if err != nil {
				t.Fatalf("plan: %v", err)
			}
			if err := arrive(env, t); err != nil {
				t.Fatal(err)
			}
			before := env.eventCount(t)
			_, err = env.adoption.ApplyAdoptAccount(ctx, plan)
			if !errors.Is(err, application.ErrAdoptSourceHasMembers) && !errors.Is(err, application.ErrAdoptSourceHasInvites) {
				t.Fatalf("apply after %s arrived: err = %v, want a refusal", name, err)
			}
			if after := env.eventCount(t); after != before {
				t.Errorf("the refused run wrote %d event(s)", after-before)
			}
			env.requireOwner(t, "task", f.orphanTask1, orphanAccountID, "after the refused run")
		})
	}
}

// wm-m0ci05: an account with no member can still be in use through the
// creator rule: whoever created its resources keeps access to them. The
// preview names each creator, whether it can still sign in, how many of the
// resources it wrote and when it last wrote one.
func TestAdoptAccount_ThePreviewNamesWhoCreatedTheSourcesResources(t *testing.T) {
	env := newAssignOwnerEnv(t)
	env.seedOrphan(t)
	// The owner agent also wrote into the orphaned account, and can sign in.
	env.create(t, env.ownerAgent, orphanAccountID, "task", "Recent task")

	plan, err := env.adoption.AdoptAccount(context.Background(), env.adoptCommand(true))
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	got := map[string]repositories.AccountCreator{}
	for _, c := range plan.FromCreators {
		got[c.AgentID] = c
	}
	if len(got) != 2 {
		t.Fatalf("creators = %+v, want the orphan's writer and the owner", plan.FromCreators)
	}
	if c := got[orphanWriter]; c.Resources != 3 || c.HasCredential || c.NewestCreatedAt.IsZero() {
		t.Errorf("orphan writer = %+v, want 3 resources, no credential and a newest time", c)
	}
	if c := got[env.ownerAgent]; c.Resources != 1 || !c.HasCredential || c.NewestCreatedAt.IsZero() {
		t.Errorf("owner = %+v, want 1 resource, a credential and a newest time", c)
	}
}

// wm-ebgqob (1): the move is only worth anything if the target account's
// owner then finds the moved resources — in a list and in memory_search —
// and not only by id. Lists and the lexical index scope by the creator or a
// read grant, and a moved resource keeps the orphan's creator.
func TestAdoptAccount_TheTargetsOwnerListsAndSearchesWhatMoved(t *testing.T) {
	env := newAssignOwnerEnvWithWorkers(t)
	f := env.seedOrphan(t)
	ctx := context.Background()
	if _, err := env.adoption.AdoptAccount(ctx, env.adoptCommand(false)); err != nil {
		t.Fatalf("move: %v", err)
	}
	if err := waitForSubscriber(ctx, env.workers, lexicalSubscriber); err != nil {
		t.Fatal(err)
	}
	owner := auth.ContextWithAgent(ctx, &auth.Identity{
		AgentID: env.ownerAgent, AccountIDs: []string{env.ownerAccount}, ActiveAccountID: env.ownerAccount,
	})

	listed := map[string]bool{}
	for _, typeSlug := range []string{"task", "project"} {
		page, err := env.resources.List(owner, typeSlug, "", 100, repositories.SortOptions{})
		if err != nil {
			t.Fatalf("list %s: %v", typeSlug, err)
		}
		for _, r := range page.Data {
			listed[r.GetID()] = true
		}
	}
	for _, id := range []string{f.orphanTask1, f.orphanTask2, f.orphanProject} {
		if !listed[id] {
			t.Errorf("the target's owner does not list moved resource %s", id)
		}
	}
	if listed[f.othersTask] {
		t.Errorf("the target's owner lists another account's resource %s", f.othersTask)
	}

	hits, _, err := env.search.Search(owner, repositories.LexicalQuery{Text: "Reconcile statement", Limit: 10})
	if err != nil {
		t.Fatalf("memory_search: %v", err)
	}
	found := map[string]bool{}
	for _, h := range hits {
		found[h.ID] = true
	}
	for _, id := range []string{f.orphanTask1, f.orphanTask2} {
		if !found[id] {
			t.Errorf("memory_search by the target's owner does not find moved resource %s (hits %+v)", id, hits)
		}
	}

	// A re-run changes nothing, grants included.
	var grants int64
	if err := env.db.Table("resource_permissions").Count(&grants).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := env.adoption.AdoptAccount(ctx, env.adoptCommand(false)); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	var again int64
	if err := env.db.Table("resource_permissions").Count(&again).Error; err != nil {
		t.Fatal(err)
	}
	if again != grants {
		t.Errorf("a re-run changed the grants: %d before, %d after", grants, again)
	}
}

// wm-ebgqob (2): the run reports its progress per resource, with the totals
// a printer needs to say how far it is, overall and per type.
func TestAdoptAccount_TheRunReportsItsProgress(t *testing.T) {
	env := newAssignOwnerEnv(t)
	env.seedOrphan(t)
	ctx := context.Background()
	plan, err := env.adoption.PlanAdoptAccount(ctx, env.adoptCommand(false))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	var seen []application.AdoptAccountProgress
	if _, err := env.adoption.ApplyAdoptAccountWithProgress(ctx, plan, func(p application.AdoptAccountProgress) {
		seen = append(seen, p)
	}); err != nil {
		t.Fatalf("move: %v", err)
	}
	want := []application.AdoptAccountProgress{
		{Done: 1, Total: 3, TypeSlug: "project", TypeDone: 1, TypeTotal: 1},
		{Done: 2, Total: 3, TypeSlug: "task", TypeDone: 1, TypeTotal: 2},
		{Done: 3, Total: 3, TypeSlug: "task", TypeDone: 2, TypeTotal: 2},
	}
	if len(seen) != len(want) {
		t.Fatalf("progress = %+v, want %+v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("progress[%d] = %+v, want %+v", i, seen[i], want[i])
		}
	}
}

// wm-ebgqob (1): a grant the agent already holds without read keeps its
// actions and gains read; one with read is left as it is.
func TestAdoptAccount_AnExistingGrantKeepsItsActions(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seedOrphan(t)
	ctx := context.Background()
	for id, actions := range map[string]string{f.orphanTask1: `["modify"]`, f.orphanTask2: `["read","delete"]`} {
		if err := env.db.Exec("INSERT INTO resource_permissions (id, resource_id, agent_id, actions, granted_by, granted_at) "+
			"VALUES (?, ?, ?, ?, ?, ?)", "perm-"+id, id, env.ownerAgent, actions, "agent-test-someone", time.Now()).Error; err != nil {
			t.Fatal(err)
		}
	}
	report, err := env.adoption.AdoptAccount(ctx, env.adoptCommand(false))
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if report.Granted() != 2 {
		t.Errorf("granted %d, want 2 (the project, and read added to the modify grant)", report.Granted())
	}
	read := func(id string) string {
		var row struct{ Actions string }
		if err := env.db.Table("resource_permissions").Select("actions").
			Where("resource_id = ? AND agent_id = ?", id, env.ownerAgent).Scan(&row).Error; err != nil {
			t.Fatal(err)
		}
		return row.Actions
	}
	if got := read(f.orphanTask1); !strings.Contains(got, "modify") || !strings.Contains(got, "read") {
		t.Errorf("grant on %s = %s, want modify kept and read added", f.orphanTask1, got)
	}
	if got := read(f.orphanTask2); !strings.Contains(got, "delete") {
		t.Errorf("grant on %s = %s, want it left as it was", f.orphanTask2, got)
	}
}

// storeFile writes a file into account's upload folder, where the local
// storage backend writes an upload.
func (env *assignOwnerEnv) storeFile(t *testing.T, account, name string) {
	t.Helper()
	dir := filepath.Join(env.cfg.Storage.LocalPath, "accounts", account, "uploads")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("receipt"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// uploadURL is the URL the local storage backend hands out for a file in
// account's upload folder.
func uploadURL(account, name string) string {
	return "/api/uploads/files/accounts/" + account + "/uploads/" + name
}

// createNaming writes a task in account whose description names url.
func (env *assignOwnerEnv) createNaming(t *testing.T, agent, account, url string) string {
	t.Helper()
	ctx := auth.ContextWithAgent(context.Background(), &auth.Identity{
		AgentID: agent, AccountIDs: []string{account}, ActiveAccountID: account,
	})
	data, err := json.Marshal(map[string]string{"name": "Receipt for May", "description": "See " + url})
	if err != nil {
		t.Fatal(err)
	}
	r, err := env.resources.Create(ctx, application.CreateResourceCommand{TypeSlug: "task", Data: data})
	if err != nil {
		t.Fatalf("create a task naming %s: %v", url, err)
	}
	return r.GetID()
}

// Copilot 4580310631: the run moves resources, not files. A file in an
// account's upload folder is served only to that account, and erasing the
// account deletes the folder, so a moved resource would name a file its new
// account can never read. Until the files move too (wm-lhc55a), a source
// account with a stored file, or with a resource naming one of its files, is
// refused — in the preview and in a real run — and nothing moves.
func TestAdoptAccount_RefusesASourceWithUploadedFiles(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]struct {
		seed             func(env *assignOwnerEnv, t *testing.T)
		files, resources int
	}{
		"a file in its upload folder": {
			seed:  func(env *assignOwnerEnv, t *testing.T) { env.storeFile(t, orphanAccountID, "2abc-receipt.jpg") },
			files: 1,
		},
		"a resource naming one of its files": {
			seed: func(env *assignOwnerEnv, t *testing.T) {
				env.createNaming(t, orphanWriter, orphanAccountID, uploadURL(orphanAccountID, "2abc-receipt.jpg"))
			},
			resources: 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			env := newAssignOwnerEnv(t)
			f := env.seedOrphan(t)
			c.seed(env, t)
			before := env.eventCount(t)

			for _, dryRun := range []bool{true, false} {
				report, err := env.adoption.AdoptAccount(ctx, env.adoptCommand(dryRun))
				if !errors.Is(err, application.ErrAdoptSourceHasUploads) {
					t.Fatalf("dry run %v: err = %v, want the uploads refusal", dryRun, err)
				}
				if report.FromFiles != c.files || report.FromFileReferences != c.resources {
					t.Errorf("dry run %v: counted %d file(s) and %d resource(s) naming one, want %d and %d",
						dryRun, report.FromFiles, report.FromFileReferences, c.files, c.resources)
				}
				for _, want := range []string{fmt.Sprint(c.files) + " uploaded file",
					fmt.Sprint(c.resources) + " resource", "not supported yet"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("dry run %v: the refusal %q does not say %q", dryRun, err, want)
					}
				}
			}
			if after := env.eventCount(t); after != before {
				t.Errorf("the refused runs wrote %d event(s)", after-before)
			}
			env.requireOwner(t, "task", f.orphanTask1, orphanAccountID, "after the refused runs")
		})
	}
}

// A source with no file of its own passes, even when another account has
// files and a resource of the source names another account's file: only the
// source's own uploads are stranded by a move.
func TestAdoptAccount_ASourceWithNoUploadsOfItsOwnMoves(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seedOrphan(t)
	env.storeFile(t, env.ownerAccount, "2abc-owner.jpg")
	env.createNaming(t, orphanWriter, orphanAccountID, uploadURL(env.ownerAccount, "2abc-owner.jpg"))

	report, err := env.adoption.AdoptAccount(context.Background(), env.adoptCommand(false))
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if report.FromFiles != 0 || report.FromFileReferences != 0 {
		t.Errorf("counted %d file(s) and %d resource(s) naming one, want none", report.FromFiles, report.FromFileReferences)
	}
	env.requireAdopted(t, f, "after the move")
}

// The uploads check is read again when the real run starts: a file stored
// after the preview refuses the run, and nothing moves.
func TestAdoptAccount_TheRunChecksForUploadsAgainBeforeItMovesAnything(t *testing.T) {
	ctx := context.Background()
	env := newAssignOwnerEnv(t)
	f := env.seedOrphan(t)
	plan, err := env.adoption.PlanAdoptAccount(ctx, env.adoptCommand(false))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	env.storeFile(t, orphanAccountID, "2abc-late.jpg")
	before := env.eventCount(t)
	if _, err := env.adoption.ApplyAdoptAccount(ctx, plan); !errors.Is(err, application.ErrAdoptSourceHasUploads) {
		t.Fatalf("apply after a file arrived: err = %v, want the uploads refusal", err)
	}
	if after := env.eventCount(t); after != before {
		t.Errorf("the refused run wrote %d event(s)", after-before)
	}
	env.requireOwner(t, "task", f.orphanTask1, orphanAccountID, "after the refused run")
}

// The operator command refuses a source with uploads in its preview and with
// --write, names the counts and says the move of uploads is not supported.
func TestAdoptAccount_TheCommandRefusesASourceWithUploads(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the weos binary")
	}
	env := newAssignOwnerEnv(t)
	f := env.seedOrphan(t)
	env.storeFile(t, orphanAccountID, "2abc-receipt.jpg")
	env.stop()
	db, err := gorm.Open(gormprov.DialectorForDSN(env.cfg.DatabaseDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	env.db = db

	args := []string{"maintenance", "adopt-account", "--from", orphanAccountID,
		"--account", env.ownerAccount, "--agent", env.ownerAgent}
	for _, extra := range [][]string{nil, {"--write"}} {
		_, stderr, code := env.runWeos(t, append(args, extra...)...)
		if code == 0 {
			t.Fatalf("%v: a source with an uploaded file exited 0", extra)
		}
		for _, want := range []string{"1 uploaded file", "not supported yet"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("%v: stderr lacks %q:\n%s", extra, want, stderr)
			}
		}
	}
	env.requireOwner(t, "task", f.orphanTask1, orphanAccountID, "after the refused command")
}
