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
	for _, want := range []string{"DRY RUN", orphanAccountName, ownerName, "0 members",
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
	if !strings.Contains(stdout, "total: 3 of 3 resource(s) moved") {
		t.Errorf("real run output lacks the total:\n%s", stdout)
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
