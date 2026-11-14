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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	gormprov "github.com/wepala/weos/v3/infrastructure/database/gorm"
	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/internal/testpg"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

// This file pins the owner backfill (wm-4kdhro): rows written while every
// session ran with an empty active account carry no owner, and
// `weos maintenance assign-owner` gives them one through the event stream, so
// the owner survives a rebuild of the read model from events.

type assignOwnerEnv struct {
	cfg        config.Config
	app        *fx.App
	db         *gorm.DB
	projMgr    repositories.ProjectionManager
	eventStore domain.EventStore
	resources  application.ResourceService
	backfill   *application.OwnerBackfillService

	// The backfill's own dependencies, so a test can build the service over
	// a wrapped event store.
	resourceRepo repositories.ResourceRepository
	accountRepo  authrepos.AccountRepository
	dispatcher   *domain.EventDispatcher
	logger       entities.Logger

	ownerAgent, ownerAccount string
	otherAgent, otherAccount string
}

func newAssignOwnerEnv(t *testing.T) *assignOwnerEnv {
	t.Helper()
	env := &assignOwnerEnv{cfg: config.Default()}
	env.cfg.DatabaseDSN = filepath.Join(t.TempDir(), "assign_owner.db")
	// The owner upsert is dialect-specific SQL, so the same tests run on
	// PostgreSQL when a server is named.
	if admin := os.Getenv("TEST_POSTGRES_DSN"); admin != "" {
		dsn, err := testpg.FreshDatabase(t, admin, "weos_assign_owner")
		if err != nil {
			t.Fatalf("fresh postgres database: %v", err)
		}
		env.cfg.DatabaseDSN = dsn
	}
	env.cfg.LogLevel = "error"

	var authService authapp.AuthenticationService
	var types application.ResourceTypeService
	env.app = fx.New(
		fx.NopLogger,
		application.Module(env.cfg, presets.NewDefaultRegistry()),
		fx.Populate(&authService, &types, &env.resources, &env.backfill),
		fx.Populate(&env.db, &env.projMgr, &env.eventStore),
		fx.Populate(&env.resourceRepo, &env.accountRepo, &env.dispatcher, &env.logger),
	)
	ctx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := env.app.Start(ctx); err != nil {
		t.Fatalf("start app: %v", err)
	}
	t.Cleanup(env.stop)

	bg := context.Background()
	owner, _, ownerAccount, err := authService.FindOrCreateAgent(bg, authapp.UserInfo{
		ProviderUserID: "owner-001", Email: "owner@example.com", DisplayName: "Owner", Provider: "dev",
	})
	if err != nil || ownerAccount == nil {
		t.Fatalf("seed owner: %v (account %v)", err, ownerAccount)
	}
	other, _, otherAccount, err := authService.FindOrCreateAgent(bg, authapp.UserInfo{
		ProviderUserID: "other-001", Email: "other@example.com", DisplayName: "Other", Provider: "dev",
	})
	if err != nil || otherAccount == nil {
		t.Fatalf("seed other: %v (account %v)", err, otherAccount)
	}
	env.ownerAgent, env.ownerAccount = owner.GetID(), ownerAccount.GetID()
	env.otherAgent, env.otherAccount = other.GetID(), otherAccount.GetID()

	if _, err := types.InstallPreset(bg, "tasks", true); err != nil {
		t.Fatalf("install tasks preset: %v", err)
	}
	return env
}

func (env *assignOwnerEnv) stop() {
	if env.app == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	_ = env.app.Stop(ctx)
	env.app = nil
}

// create writes a resource as agent, with activeAccount as the session's
// active account — "" reproduces the session bug that left rows ownerless.
func (env *assignOwnerEnv) create(t *testing.T, agent, activeAccount, typeSlug, name string) string {
	t.Helper()
	var accounts []string
	if activeAccount != "" {
		accounts = []string{activeAccount}
	}
	ctx := auth.ContextWithAgent(context.Background(), &auth.Identity{
		AgentID: agent, AccountIDs: accounts, ActiveAccountID: activeAccount,
	})
	r, err := env.resources.Create(ctx, application.CreateResourceCommand{
		TypeSlug: typeSlug, Data: json.RawMessage(fmt.Sprintf(`{"name":%q}`, name)),
	})
	if err != nil {
		t.Fatalf("create %s %q: %v", typeSlug, name, err)
	}
	return r.GetID()
}

// owners reads a resource's owner from the canonical record and from its
// type's projection table.
func (env *assignOwnerEnv) owners(t *testing.T, typeSlug, id string) (canonical, projection string) {
	t.Helper()
	var row struct{ AccountID string }
	if err := env.db.Table("resources").Select("account_id").Where("id = ?", id).Scan(&row).Error; err != nil {
		t.Fatalf("read canonical owner of %s: %v", id, err)
	}
	canonical = row.AccountID
	var proj struct{ AccountID *string }
	if err := env.db.Table(env.projMgr.TableName(typeSlug)).Select("account_id").
		Where("id = ?", id).Scan(&proj).Error; err != nil {
		t.Fatalf("read projection owner of %s: %v", id, err)
	}
	if proj.AccountID != nil {
		projection = *proj.AccountID
	}
	return canonical, projection
}

func (env *assignOwnerEnv) requireOwner(t *testing.T, typeSlug, id, want, when string) {
	t.Helper()
	canonical, projection := env.owners(t, typeSlug, id)
	if canonical != want || projection != want {
		t.Fatalf("%s: owner of %s = canonical %q, projection %q; want %q in both",
			when, id, canonical, projection, want)
	}
}

func (env *assignOwnerEnv) eventCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := env.db.Table("events").Count(&n).Error; err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}

type assignOwnerFixture struct {
	ownerless1, ownerless2 string // tasks, ownerless, created by the owner agent
	ownerlessProject       string // a project, ownerless, created by the owner agent
	othersOwnerless        string // a task, ownerless, created by another agent
	alreadyOwned           string // a task created by the owner agent in its account
}

func (env *assignOwnerEnv) seed(t *testing.T) assignOwnerFixture {
	t.Helper()
	return assignOwnerFixture{
		ownerless1:       env.create(t, env.ownerAgent, "", "task", "Reconcile May statement"),
		ownerless2:       env.create(t, env.ownerAgent, "", "task", "Reconcile June statement"),
		ownerlessProject: env.create(t, env.ownerAgent, "", "project", "Bank imports"),
		othersOwnerless:  env.create(t, env.otherAgent, "", "task", "Someone else's task"),
		alreadyOwned:     env.create(t, env.ownerAgent, env.ownerAccount, "task", "Owned task"),
	}
}

func TestAssignOwner_DryRunCountsPerTypeAndChangesNothing(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seed(t)
	before := env.eventCount(t)

	report, err := env.backfill.AssignOwner(context.Background(), application.AssignOwnerCommand{
		AccountID: env.ownerAccount, CreatedBy: env.ownerAgent, DryRun: true,
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !report.DryRun {
		t.Error("report does not say it was a dry run")
	}
	if got := report.Types["task"]; got.Ownerless != 2 || got.Assigned != 0 {
		t.Errorf("task counts = %+v, want 2 ownerless and 0 assigned", got)
	}
	if got := report.Types["project"]; got.Ownerless != 1 || got.Assigned != 0 {
		t.Errorf("project counts = %+v, want 1 ownerless and 0 assigned", got)
	}
	if report.Ownerless() != 3 || report.Assigned() != 0 {
		t.Errorf("totals = %d ownerless, %d assigned; want 3 and 0", report.Ownerless(), report.Assigned())
	}
	if after := env.eventCount(t); after != before {
		t.Errorf("a dry run wrote %d event(s)", after-before)
	}
	env.requireOwner(t, "task", f.ownerless1, "", "after the dry run")
	env.requireOwner(t, "project", f.ownerlessProject, "", "after the dry run")
}

func TestAssignOwner_AssignsOnlyOwnerlessRowsOfTheGivenCreator(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seed(t)

	report, err := env.backfill.AssignOwner(context.Background(), application.AssignOwnerCommand{
		AccountID: env.ownerAccount, CreatedBy: env.ownerAgent,
	})
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if report.DryRun {
		t.Error("a real run reports itself as a dry run")
	}
	if got := report.Types["task"]; got.Ownerless != 2 || got.Assigned != 2 {
		t.Errorf("task counts = %+v, want 2 ownerless and 2 assigned", got)
	}
	if got := report.Types["project"]; got.Ownerless != 1 || got.Assigned != 1 {
		t.Errorf("project counts = %+v, want 1 ownerless and 1 assigned", got)
	}

	env.requireOwner(t, "task", f.ownerless1, env.ownerAccount, "after the run")
	env.requireOwner(t, "task", f.ownerless2, env.ownerAccount, "after the run")
	env.requireOwner(t, "project", f.ownerlessProject, env.ownerAccount, "after the run")
	env.requireOwner(t, "task", f.othersOwnerless, "", "another creator's row")
	env.requireOwner(t, "task", f.alreadyOwned, env.ownerAccount, "an already-owned row")

	// The run is idempotent: nothing is left to assign.
	again, err := env.backfill.AssignOwner(context.Background(), application.AssignOwnerCommand{
		AccountID: env.ownerAccount, CreatedBy: env.ownerAgent,
	})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if again.Ownerless() != 0 || again.Assigned() != 0 {
		t.Errorf("second run found %d and assigned %d, want 0 and 0", again.Ownerless(), again.Assigned())
	}

	// The assignment advanced the aggregate's version in the read model, so a
	// later update through the normal write path still commits.
	ctx := auth.ContextWithAgent(context.Background(), &auth.Identity{
		AgentID: env.ownerAgent, AccountIDs: []string{env.ownerAccount}, ActiveAccountID: env.ownerAccount,
	})
	if _, err := env.resources.Update(ctx, application.UpdateResourceCommand{
		ID: f.ownerless1, Data: json.RawMessage(`{"name":"Reconcile May statement again"}`),
	}); err != nil {
		t.Fatalf("update after the assignment: %v", err)
	}
	env.requireOwner(t, "task", f.ownerless1, env.ownerAccount, "after a later update")
}

func TestAssignOwner_RefusesAnUnknownAccountOrANonMember(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seed(t)
	before := env.eventCount(t)

	_, err := env.backfill.AssignOwner(context.Background(), application.AssignOwnerCommand{
		AccountID: "no-such-account", CreatedBy: env.ownerAgent,
	})
	if !errors.Is(err, application.ErrOwnerAccountNotFound) {
		t.Errorf("unknown account: err = %v, want ErrOwnerAccountNotFound", err)
	}
	_, err = env.backfill.AssignOwner(context.Background(), application.AssignOwnerCommand{
		AccountID: env.otherAccount, CreatedBy: env.ownerAgent,
	})
	if !errors.Is(err, application.ErrOwnerAgentNotMember) {
		t.Errorf("non-member: err = %v, want ErrOwnerAgentNotMember", err)
	}
	for _, missing := range []application.AssignOwnerCommand{
		{CreatedBy: env.ownerAgent}, {AccountID: env.ownerAccount},
	} {
		if _, err := env.backfill.AssignOwner(context.Background(), missing); err == nil {
			t.Errorf("command %+v with a missing value succeeded", missing)
		}
	}
	if after := env.eventCount(t); after != before {
		t.Errorf("a refused run wrote %d event(s)", after-before)
	}
	env.requireOwner(t, "task", f.ownerless1, "", "after the refused runs")
}

// A rebuild of the read model from the event stream keeps the assigned
// owner: once over the existing rows, and once from empty tables.
func TestAssignOwner_ARebuildFromEventsKeepsTheOwner(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seed(t)
	if _, err := env.backfill.AssignOwner(context.Background(), application.AssignOwnerCommand{
		AccountID: env.ownerAccount, CreatedBy: env.ownerAgent,
	}); err != nil {
		t.Fatalf("assign: %v", err)
	}
	taskTable := env.projMgr.TableName("task")
	projectTable := env.projMgr.TableName("project")
	env.stop()

	reproject := func(when string) {
		t.Helper()
		env.reproject(t, when)
		for _, c := range []struct{ slug, id, want string }{
			{"task", f.ownerless1, env.ownerAccount},
			{"task", f.ownerless2, env.ownerAccount},
			{"project", f.ownerlessProject, env.ownerAccount},
			{"task", f.othersOwnerless, ""},
			{"task", f.alreadyOwned, env.ownerAccount},
		} {
			env.requireOwner(t, c.slug, c.id, c.want, when)
		}
	}

	// Over existing rows whose owner was lost: a hand edit of the read model
	// is exactly what a rebuild must undo.
	for _, table := range []string{"resources", taskTable, projectTable} {
		if err := env.db.Exec("UPDATE " + table + " SET account_id = ''").Error; err != nil {
			t.Fatalf("blank owners in %s: %v", table, err)
		}
	}
	reproject("rebuild over existing rows")

	// From empty read-model tables.
	for _, table := range []string{"resources", taskTable, projectTable} {
		if err := env.db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("empty %s: %v", table, err)
		}
	}
	reproject("rebuild from empty tables")
}

// reproject runs `weos worker reproject` in process, against a stopped
// instance's store, and leaves env.db open on the store for reads.
func (env *assignOwnerEnv) reproject(t *testing.T, when string) {
	t.Helper()
	var rt application.ReprojectRuntime
	app := fx.New(fx.NopLogger, application.ReprojectModule(env.cfg), fx.Populate(&rt, &env.db))
	ctx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("%s: start reproject: %v", when, err)
	}
	defer func() { _ = app.Stop(context.Background()) }()
	if _, err := application.Reproject(context.Background(), rt, application.ReprojectOptions{}); err != nil {
		t.Fatalf("%s: reproject: %v", when, err)
	}
}

// runWeos runs the real weos binary against the env's store.
func (env *assignOwnerEnv) runWeos(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	binary, err := weosBinary()
	if err != nil {
		t.Fatalf("build weos: %v", err)
	}
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(), "DATABASE_DSN="+env.cfg.DatabaseDSN, "LOG_LEVEL=error")
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if runErr := cmd.Run(); runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			t.Fatalf("run weos: %v", runErr)
		}
		exitCode = exitErr.ExitCode()
	}
	return out.String(), errOut.String(), exitCode
}

// The operator command, as the shipped binary runs it against a stopped
// instance's store.
func TestAssignOwner_TheCommandPreviewsThenAssignsWithWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the weos binary")
	}
	env := newAssignOwnerEnv(t)
	f := env.seed(t)

	// The help says to stop the instance for a real run. This test follows
	// that rule, so it checks the help still states it.
	help, _, code := env.runWeos(t, "maintenance", "assign-owner", "--help")
	if code != 0 || !strings.Contains(help, "Stop the instance for the real run") {
		t.Fatalf("assign-owner --help (exit %d) no longer says to stop the instance:\n%s", code, help)
	}
	// The instance is stopped, as the help asks; the test reads the store
	// through a plain connection of its own.
	env.stop()
	db, err := gorm.Open(gormprov.DialectorForDSN(env.cfg.DatabaseDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	env.db = db

	args := []string{"maintenance", "assign-owner", "--account", env.ownerAccount, "--created-by", env.ownerAgent}
	stdout, stderr, code := env.runWeos(t, args...)
	if code != 0 {
		t.Fatalf("preview exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	for _, want := range []string{"DRY RUN", "project", "1 ownerless, would assign 1",
		"task", "2 ownerless, would assign 2", "total: 3 ownerless resource(s) would be assigned"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("preview output lacks %q:\n%s", want, stdout)
		}
	}
	env.requireOwner(t, "task", f.ownerless1, "", "after the command's preview")

	stdout, stderr, code = env.runWeos(t, append(args, "--write")...)
	if code != 0 {
		t.Fatalf("real run exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "total: 3 of 3 ownerless resource(s) assigned") {
		t.Errorf("real run output lacks the total:\n%s", stdout)
	}
	env.requireOwner(t, "task", f.ownerless1, env.ownerAccount, "after the command")
	env.requireOwner(t, "project", f.ownerlessProject, env.ownerAccount, "after the command")
	env.requireOwner(t, "task", f.othersOwnerless, "", "another creator's row after the command")

	// A creator who is not a member of the account is refused, non-zero.
	_, stderr, code = env.runWeos(t, "maintenance", "assign-owner",
		"--account", env.otherAccount, "--created-by", env.ownerAgent, "--write")
	if code == 0 || !strings.Contains(stderr, "is not a member of account") {
		t.Errorf("non-member run exited %d, stderr:\n%s", code, stderr)
	}
}

func (env *assignOwnerEnv) isPostgres() bool {
	return strings.HasPrefix(env.cfg.DatabaseDSN, "postgres")
}

// failOwnerWrites makes every write that gives the canonical record of id an
// owner fail, as a busy database or a lost connection would fail the
// synchronous projection write after the event was stored.
func (env *assignOwnerEnv) failOwnerWrites(t *testing.T, id string) (restore func()) {
	t.Helper()
	if env.isPostgres() {
		t.Skip("the failing-write trigger is SQLite only")
	}
	trigger := fmt.Sprintf(`CREATE TRIGGER fail_owner_write BEFORE UPDATE ON resources
		WHEN NEW.id = '%s' AND NEW.account_id <> ''
		BEGIN SELECT RAISE(ABORT, 'simulated projection write failure'); END`, id)
	if err := env.db.Exec(trigger).Error; err != nil {
		t.Fatalf("install the failing-write trigger: %v", err)
	}
	return func() {
		if err := env.db.Exec("DROP TRIGGER fail_owner_write").Error; err != nil {
			t.Fatalf("drop the failing-write trigger: %v", err)
		}
	}
}

// ownedCount is how many of ids carry want as their canonical owner.
func (env *assignOwnerEnv) ownedCount(t *testing.T, want string, ids ...string) int {
	t.Helper()
	var n int64
	if err := env.db.Table("resources").Where("id IN ? AND account_id = ?", ids, want).
		Count(&n).Error; err != nil {
		t.Fatalf("count owned rows: %v", err)
	}
	return int(n)
}

// A projection write that fails after the event was stored must not be
// reported as an assignment: the row is still ownerless.
func TestAssignOwner_AFailedProjectionWriteIsNotCountedAsAssigned(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seed(t)
	restore := env.failOwnerWrites(t, f.ownerless1)

	report, err := env.backfill.AssignOwner(context.Background(), application.AssignOwnerCommand{
		AccountID: env.ownerAccount, CreatedBy: env.ownerAgent,
	})
	if err == nil {
		t.Fatalf("a run whose projection write failed returned no error; report %+v", report)
	}
	for _, want := range []string{f.ownerless1, "weos worker reproject"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
	all := []string{f.ownerless1, f.ownerless2, f.ownerlessProject}
	if owned := env.ownedCount(t, env.ownerAccount, all...); report.Assigned() != owned {
		t.Errorf("report says %d assigned, but %d row(s) carry the owner", report.Assigned(), owned)
	}
	env.requireOwner(t, "task", f.ownerless1, "", "after the failed write")
	restore()

	// The event is stored and the row is a version behind it. A re-run must
	// not stall on that row: it skips it, names it for a reproject, and
	// assigns everything else.
	again, err := env.backfill.AssignOwner(context.Background(), application.AssignOwnerCommand{
		AccountID: env.ownerAccount, CreatedBy: env.ownerAgent,
	})
	if err != nil {
		t.Fatalf("a re-run stalled on the stale row: %v", err)
	}
	if len(again.NeedsReproject) != 1 || again.NeedsReproject[0].ID != f.ownerless1 {
		t.Fatalf("re-run lists %+v for a reproject, want only %s", again.NeedsReproject, f.ownerless1)
	}
	if reason := again.NeedsReproject[0].Reason; !strings.Contains(reason, "Resource.AccountAssigned") {
		t.Errorf("reason %q does not say the assignment is already in the event store", reason)
	}
	if got := again.Types["task"].NeedsReproject; got != 1 {
		t.Errorf("task count needing a reproject = %d, want 1", got)
	}
	if owned := env.ownedCount(t, env.ownerAccount, f.ownerless2, f.ownerlessProject); owned != 2 {
		t.Errorf("%d of the 2 healthy rows carry the owner after the re-run", owned)
	}
	if owned := env.ownedCount(t, env.ownerAccount, all...); again.Assigned()+report.Assigned() != owned {
		t.Errorf("the runs report %d assigned, but %d row(s) carry the owner",
			again.Assigned()+report.Assigned(), owned)
	}

	// The advice holds: a reproject replays the stored assignment.
	env.stop()
	env.reproject(t, "the advised reproject")
	env.requireOwner(t, "task", f.ownerless1, env.ownerAccount, "after the advised reproject")
}

// failTypeRowOwnerWrites makes every write that gives id's row in typeSlug's
// projection table an owner fail, while the canonical record's write goes
// through: the two are written one after the other, not in one transaction.
func (env *assignOwnerEnv) failTypeRowOwnerWrites(t *testing.T, typeSlug, id string) (restore func()) {
	t.Helper()
	if env.isPostgres() {
		t.Skip("the failing-write trigger is SQLite only")
	}
	trigger := fmt.Sprintf(`CREATE TRIGGER fail_type_row_owner_write BEFORE UPDATE ON %s
		WHEN NEW.id = '%s' AND NEW.account_id <> ''
		BEGIN SELECT RAISE(ABORT, 'simulated type projection row write failure'); END`,
		env.projMgr.TableName(typeSlug), id)
	if err := env.db.Exec(trigger).Error; err != nil {
		t.Fatalf("install the failing type-row trigger: %v", err)
	}
	return func() {
		if err := env.db.Exec("DROP TRIGGER fail_type_row_owner_write").Error; err != nil {
			t.Fatalf("drop the failing type-row trigger: %v", err)
		}
	}
}

// assignedEvents is how many Resource.AccountAssigned events the store holds
// for id.
func (env *assignOwnerEnv) assignedEvents(t *testing.T, id string) int64 {
	t.Helper()
	var n int64
	if err := env.db.Table("events").Where("aggregate_id = ? AND event_type = ?", id, "Resource.AccountAssigned").
		Count(&n).Error; err != nil {
		t.Fatalf("count the assignments of %s: %v", id, err)
	}
	return n
}

// wm-mttt4k: the record takes the owner and the type's projection row does
// not. The run must not count the resource as assigned, and a re-run must fill
// the row from the record without a second event.
func TestAssignOwner_ATypeRowThatDidNotTakeTheOwnerIsFilledByARerun(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seed(t)
	restore := env.failTypeRowOwnerWrites(t, "task", f.ownerless1)
	cmd := application.AssignOwnerCommand{AccountID: env.ownerAccount, CreatedBy: env.ownerAgent}

	report, err := env.backfill.AssignOwner(context.Background(), cmd)
	if err == nil {
		t.Fatalf("a run whose type projection row stayed ownerless returned no error; report %+v", report)
	}
	for _, want := range []string{f.ownerless1, "projection row", "run this command again"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}
	canonical, projection := env.owners(t, "task", f.ownerless1)
	if canonical != env.ownerAccount || projection != "" {
		t.Fatalf("after the failed type-row write: canonical %q, type row %q; want the owner, then none",
			canonical, projection)
	}
	fully := 0
	for _, r := range []struct{ slug, id string }{
		{"task", f.ownerless1}, {"task", f.ownerless2}, {"project", f.ownerlessProject},
	} {
		if c, p := env.owners(t, r.slug, r.id); c == env.ownerAccount && p == env.ownerAccount {
			fully++
		}
	}
	if report.Assigned() != fully {
		t.Errorf("report says %d assigned, but %d resource(s) carry the owner in the record and the type row",
			report.Assigned(), fully)
	}
	restore()

	// The preview selects the resource and says its record has the owner.
	preview, err := env.backfill.AssignOwner(context.Background(), application.AssignOwnerCommand{
		AccountID: env.ownerAccount, CreatedBy: env.ownerAgent, DryRun: true,
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	var listed *application.AssignOwnerResource
	for i := range preview.Resources {
		if preview.Resources[i].ID == f.ownerless1 {
			listed = &preview.Resources[i]
		}
	}
	if listed == nil || !listed.RecordOwned {
		t.Fatalf("the preview lists %+v; want %s marked as a record that has the owner", preview.Resources, f.ownerless1)
	}
	// The command's preview reads the store read-only, with no projection
	// manager, and must still find and mark the row.
	if !testing.Short() {
		stdout, stderr, code := env.runWeos(t, "maintenance", "assign-owner",
			"--account", env.ownerAccount, "--created-by", env.ownerAgent)
		want := f.ownerless1 + "  (record has the owner; projection row to fill)"
		if code != 0 || !strings.Contains(stdout, want) {
			t.Errorf("the command's preview (exit %d) lacks %q:\n%s\nstderr:\n%s", code, want, stdout, stderr)
		}
	}

	again, err := env.backfill.AssignOwner(context.Background(), cmd)
	if err != nil {
		t.Fatalf("the re-run: %v", err)
	}
	// The fill stores no event, so it is counted apart (wm-y49sxb): the
	// assigned counts match the events, and the one row is reported filled.
	if got := report.Assigned() + again.Assigned(); got != 3-1 {
		t.Errorf("the runs report %d assigned, want 2: one event per resource, and none for the fill", got)
	}
	if again.Filled() != 1 || again.ToFill() != 1 {
		t.Errorf("the re-run reports %d of %d row(s) filled, want 1 of 1", again.Filled(), again.ToFill())
	}
	env.requireOwner(t, "task", f.ownerless1, env.ownerAccount, "after the re-run")
	env.requireOwner(t, "task", f.ownerless2, env.ownerAccount, "after the re-run")
	env.requireOwner(t, "project", f.ownerlessProject, env.ownerAccount, "after the re-run")
	if n := env.assignedEvents(t, f.ownerless1); n != 1 {
		t.Errorf("the store holds %d assignment(s) of %s, want the one the first run stored", n, f.ownerless1)
	}

	// A rebuild keeps the owner the filled row now carries.
	env.stop()
	env.reproject(t, "a rebuild after the re-run")
	env.requireOwner(t, "task", f.ownerless1, env.ownerAccount, "after a rebuild")
}

// conflictingStore refuses every append to one aggregate with a version
// conflict, as a write that landed between the run's read and its append
// would.
type conflictingStore struct {
	domain.EventStore
	aggregateID string
}

func (s conflictingStore) Append(
	ctx context.Context, aggregateID string, expected int, events ...domain.EventEnvelope[any],
) error {
	if aggregateID == s.aggregateID {
		return fmt.Errorf("%w: expected version %d, got %d", domain.ErrConcurrencyConflict, expected, expected+1)
	}
	return s.EventStore.Append(ctx, aggregateID, expected, events...)
}

func TestAssignOwner_ASequenceConflictSkipsTheResourceAndContinues(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seed(t)
	svc := application.ProvideOwnerBackfillService(application.OwnerBackfillParams{
		Repo: env.resourceRepo, Accounts: env.accountRepo,
		EventStore: conflictingStore{EventStore: env.eventStore, aggregateID: f.ownerless1},
		Dispatcher: env.dispatcher, Logger: env.logger,
	})

	report, err := svc.AssignOwner(context.Background(), application.AssignOwnerCommand{
		AccountID: env.ownerAccount, CreatedBy: env.ownerAgent,
	})
	if err != nil {
		t.Fatalf("a sequence conflict stopped the run: %v", err)
	}
	if len(report.NeedsReproject) != 1 || report.NeedsReproject[0].ID != f.ownerless1 {
		t.Fatalf("run lists %+v for a reproject, want only %s", report.NeedsReproject, f.ownerless1)
	}
	if reason := report.NeedsReproject[0].Reason; !strings.Contains(reason, "conflict") {
		t.Errorf("reason %q does not name the conflict", reason)
	}
	if report.Assigned() != 2 {
		t.Errorf("assigned %d, want the 2 resources after and around the conflict", report.Assigned())
	}
	env.requireOwner(t, "task", f.ownerless1, "", "the conflicting row")
	env.requireOwner(t, "task", f.ownerless2, env.ownerAccount, "a row after the conflict")
	env.requireOwner(t, "project", f.ownerlessProject, env.ownerAccount, "a row before the conflict")
}

// setCreatedAt moves a resource's creation time, as if it had been written
// then.
func (env *assignOwnerEnv) setCreatedAt(t *testing.T, id string, at time.Time) {
	t.Helper()
	if err := env.db.Table("resources").Where("id = ?", id).Update("created_at", at).Error; err != nil {
		t.Fatalf("set created_at of %s: %v", id, err)
	}
}

func (env *assignOwnerEnv) accountName(t *testing.T, id string) string {
	t.Helper()
	account, err := env.accountRepo.FindByID(context.Background(), id)
	if err != nil || account == nil {
		t.Fatalf("load account %s: %v", id, err)
	}
	return account.Name()
}

// joinOtherAccount makes the owner agent a member of the other account too,
// so the agent belongs to two accounts.
func (env *assignOwnerEnv) joinOtherAccount(t *testing.T) {
	t.Helper()
	if err := env.accountRepo.SaveMember(context.Background(), env.otherAccount, env.ownerAgent,
		authentities.RoleMember); err != nil {
		t.Fatalf("add the owner agent to the other account: %v", err)
	}
}

var (
	augustFifth  = time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	julyLast     = time.Date(2026, 7, 31, 23, 0, 0, 0, time.UTC)
	windowFrom   = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	windowTo     = time.Date(2026, 8, 10, 23, 59, 59, 999999999, time.UTC)
	augustTenthL = time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC)
)

func TestAssignOwner_DryRunListsEachResourceAndTheAccountName(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seed(t)
	env.setCreatedAt(t, f.ownerless1, augustFifth)

	report, err := env.backfill.AssignOwner(context.Background(), application.AssignOwnerCommand{
		AccountID: env.ownerAccount, CreatedBy: env.ownerAgent, DryRun: true,
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if want := env.accountName(t, env.ownerAccount); want == "" || report.AccountName != want {
		t.Errorf("account name = %q, want %q", report.AccountName, want)
	}
	byID := map[string]application.AssignOwnerResource{}
	for _, r := range report.Resources {
		byID[r.ID] = r
	}
	if len(report.Resources) != 3 {
		t.Fatalf("dry run lists %d resource(s), want 3: %+v", len(report.Resources), report.Resources)
	}
	for id, typeSlug := range map[string]string{
		f.ownerless1: "task", f.ownerless2: "task", f.ownerlessProject: "project",
	} {
		r, ok := byID[id]
		if !ok {
			t.Errorf("dry run does not list %s", id)
			continue
		}
		if r.TypeSlug != typeSlug || r.CreatedAt.IsZero() {
			t.Errorf("%s listed as %+v, want type %s and a creation time", id, r, typeSlug)
		}
	}
	if got := byID[f.ownerless1].CreatedAt; !got.Equal(augustFifth) {
		t.Errorf("created_at of %s = %v, want %v", f.ownerless1, got, augustFifth)
	}
}

func TestAssignOwner_TheCreationWindowNarrowsTheSelection(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seed(t)
	env.setCreatedAt(t, f.ownerless1, augustFifth)
	env.setCreatedAt(t, f.ownerlessProject, julyLast)
	// f.ownerless2 keeps today's creation time, after the window.

	cmd := application.AssignOwnerCommand{
		AccountID: env.ownerAccount, CreatedBy: env.ownerAgent,
		CreatedFrom: windowFrom, CreatedTo: windowTo, DryRun: true,
	}
	preview, err := env.backfill.AssignOwner(context.Background(), cmd)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if preview.Ownerless() != 1 || len(preview.Resources) != 1 || preview.Resources[0].ID != f.ownerless1 {
		t.Fatalf("window selects %+v, want only %s", preview.Resources, f.ownerless1)
	}

	cmd.DryRun = false
	report, err := env.backfill.AssignOwner(context.Background(), cmd)
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if report.Assigned() != 1 {
		t.Errorf("assigned %d, want 1", report.Assigned())
	}
	env.requireOwner(t, "task", f.ownerless1, env.ownerAccount, "inside the window")
	env.requireOwner(t, "task", f.ownerless2, "", "after the window")
	env.requireOwner(t, "project", f.ownerlessProject, "", "before the window")

	// An open end: from the window's start, with no end, takes today's row too.
	open, err := env.backfill.AssignOwner(context.Background(), application.AssignOwnerCommand{
		AccountID: env.ownerAccount, CreatedBy: env.ownerAgent, CreatedFrom: windowFrom, DryRun: true,
	})
	if err != nil {
		t.Fatalf("open-ended dry run: %v", err)
	}
	if open.Ownerless() != 1 || open.Resources[0].ID != f.ownerless2 {
		t.Errorf("open-ended window selects %+v, want only %s", open.Resources, f.ownerless2)
	}

	if _, err := env.backfill.AssignOwner(context.Background(), application.AssignOwnerCommand{
		AccountID: env.ownerAccount, CreatedBy: env.ownerAgent, CreatedFrom: windowTo, CreatedTo: windowFrom,
		DryRun: true,
	}); err == nil {
		t.Error("a window that ends before it starts was accepted")
	}
}

func TestAssignOwner_AnAgentInTwoAccountsNeedsTheAccountConfirmed(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seed(t)
	env.joinOtherAccount(t)
	before := env.eventCount(t)

	// A dry run is allowed, and it says which accounts the agent is in.
	preview, err := env.backfill.AssignOwner(context.Background(), application.AssignOwnerCommand{
		AccountID: env.ownerAccount, CreatedBy: env.ownerAgent, DryRun: true,
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if len(preview.MemberOf) != 2 {
		t.Errorf("dry run names %d account(s) for the agent, want 2: %+v", len(preview.MemberOf), preview.MemberOf)
	}

	for _, confirm := range []string{"", env.otherAccount} {
		_, err := env.backfill.AssignOwner(context.Background(), application.AssignOwnerCommand{
			AccountID: env.ownerAccount, CreatedBy: env.ownerAgent, ConfirmAccount: confirm,
		})
		if !errors.Is(err, application.ErrOwnerAccountNotConfirmed) {
			t.Errorf("confirm %q: err = %v, want ErrOwnerAccountNotConfirmed", confirm, err)
		}
	}
	if after := env.eventCount(t); after != before {
		t.Fatalf("an unconfirmed run wrote %d event(s)", after-before)
	}
	env.requireOwner(t, "task", f.ownerless1, "", "after the unconfirmed runs")

	report, err := env.backfill.AssignOwner(context.Background(), application.AssignOwnerCommand{
		AccountID: env.ownerAccount, CreatedBy: env.ownerAgent, ConfirmAccount: env.ownerAccount,
	})
	if err != nil {
		t.Fatalf("confirmed run: %v", err)
	}
	if report.Assigned() != 3 {
		t.Errorf("confirmed run assigned %d, want 3", report.Assigned())
	}
	env.requireOwner(t, "task", f.ownerless1, env.ownerAccount, "after the confirmed run")
}

// The operator command narrows by dates, lists each row, names the account,
// and asks for the account again when the agent is in two.
func TestAssignOwner_TheCommandNarrowsListsAndConfirms(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the weos binary")
	}
	env := newAssignOwnerEnv(t)
	f := env.seed(t)
	// Late on the window's last day: a date for --created-to takes that
	// whole day.
	env.setCreatedAt(t, f.ownerless1, augustTenthL)
	env.setCreatedAt(t, f.ownerlessProject, julyLast)
	env.joinOtherAccount(t)
	name := env.accountName(t, env.ownerAccount)
	env.stop()
	db, err := gorm.Open(gormprov.DialectorForDSN(env.cfg.DatabaseDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	env.db = db

	base := []string{"maintenance", "assign-owner", "--account", env.ownerAccount, "--created-by", env.ownerAgent,
		"--created-from", "2026-08-01", "--created-to", "2026-08-10"}
	stdout, stderr, code := env.runWeos(t, base...)
	if code != 0 {
		t.Fatalf("preview exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	for _, want := range []string{
		f.ownerless1, "2026-08-10T18:00:00Z", "total: 1 ownerless resource(s) would be assigned",
		fmt.Sprintf("account %s (%s)", name, env.ownerAccount), "member of 2 accounts", "--confirm-account",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("preview output lacks %q:\n%s", want, stdout)
		}
	}
	for _, unwanted := range []string{f.ownerless2, f.ownerlessProject} {
		if strings.Contains(stdout, unwanted) {
			t.Errorf("preview lists %s, outside the window:\n%s", unwanted, stdout)
		}
	}

	_, stderr, code = env.runWeos(t, append(base, "--write")...)
	if code == 0 || !strings.Contains(stderr, "--confirm-account") {
		t.Errorf("an unconfirmed real run exited %d, stderr:\n%s", code, stderr)
	}
	env.requireOwner(t, "task", f.ownerless1, "", "after the unconfirmed command")

	stdout, stderr, code = env.runWeos(t, append(base, "--write", "--confirm-account", env.ownerAccount)...)
	if code != 0 {
		t.Fatalf("confirmed run exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	nameAt, totalAt := strings.Index(stdout, name), strings.Index(stdout, "total: 1 of 1")
	if nameAt < 0 || totalAt < 0 || nameAt > totalAt {
		t.Errorf("the real run does not name the account before its result:\n%s", stdout)
	}
	env.requireOwner(t, "task", f.ownerless1, env.ownerAccount, "after the confirmed command")
	env.requireOwner(t, "task", f.ownerless2, "", "outside the window after the command")

	_, stderr, code = env.runWeos(t, "maintenance", "assign-owner", "--account", env.ownerAccount,
		"--created-by", env.ownerAgent, "--created-from", "August 1")
	if code == 0 || !strings.Contains(stderr, "--created-from") {
		t.Errorf("a malformed date exited %d, stderr:\n%s", code, stderr)
	}
}

// A run with no flag is a preview, and writes nothing, even against a
// database whose boot would. An assignment cannot be undone, so a forgotten
// flag must cost a preview, never a write. The full application runs
// migrations and installs missing built-in presets as it starts, so a preview
// built on it would recreate a built-in type that the store lacks, and append
// that type's events, before it listed one row.
func TestAssignOwner_TheCommandWithNoFlagWritesNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the weos binary")
	}
	env := newAssignOwnerEnv(t)
	f := env.seed(t)
	env.stop()
	db, err := gorm.Open(gormprov.DialectorForDSN(env.cfg.DatabaseDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	env.db = db

	// The notification type is auto-installed: a boot of the full
	// application creates it when the store has never had it, as on a
	// database from a release before the type existed.
	if err := db.Exec("DELETE FROM resource_types WHERE slug = ?", "notification").Error; err != nil {
		t.Fatalf("remove the notification type: %v", err)
	}
	if err := db.Exec("DELETE FROM events WHERE aggregate_id = ?", "urn:type:notification").Error; err != nil {
		t.Fatalf("remove the notification type's events: %v", err)
	}
	typeCount := func() int64 {
		t.Helper()
		var n int64
		if err := db.Table("resource_types").Count(&n).Error; err != nil {
			t.Fatalf("count resource types: %v", err)
		}
		return n
	}
	typesBefore, eventsBefore := typeCount(), env.eventCount(t)

	stdout, stderr, code := env.runWeos(t, "maintenance", "assign-owner",
		"--account", env.ownerAccount, "--created-by", env.ownerAgent)
	if code != 0 {
		t.Fatalf("a run with no flag exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	for _, want := range []string{"DRY RUN", "--write", "total: 3 ownerless resource(s) would be assigned"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("a run with no flag does not read as a preview; output lacks %q:\n%s", want, stdout)
		}
	}
	if after := typeCount(); after != typesBefore {
		t.Errorf("a run with no flag changed the resource types: %d before, %d after", typesBefore, after)
	}
	if after := env.eventCount(t); after != eventsBefore {
		t.Errorf("a run with no flag appended %d event(s)", after-eventsBefore)
	}
	const when = "after a run with no flag"
	env.requireOwner(t, "task", f.ownerless1, "", when)
	env.requireOwner(t, "task", f.ownerless2, "", when)
	env.requireOwner(t, "project", f.ownerlessProject, "", when)
	env.requireOwner(t, "task", f.othersOwnerless, "", when)
	env.requireOwner(t, "task", f.alreadyOwned, env.ownerAccount, when)
}

// wm-zm4sbz: a resource whose record has the owner and whose type row does
// not exist at all is missing from the type's listing. The run must not read
// clean: the preview and the run list it for a reproject, and a reproject
// writes the row.
func TestAssignOwner_AMissingTypeRowIsListedForAReproject(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seed(t)
	if err := env.db.Table(env.projMgr.TableName("task")).Where("id = ?", f.alreadyOwned).
		Delete(map[string]any{}).Error; err != nil {
		t.Fatalf("delete the type row: %v", err)
	}
	events := env.eventCount(t)

	for _, dryRun := range []bool{true, false} {
		report, err := env.backfill.AssignOwner(context.Background(), application.AssignOwnerCommand{
			AccountID: env.ownerAccount, CreatedBy: env.ownerAgent, DryRun: dryRun,
		})
		if err != nil {
			t.Fatalf("dry run %v: %v", dryRun, err)
		}
		var listed bool
		for _, s := range report.NeedsReproject {
			if s.ID == f.alreadyOwned && strings.Contains(s.Reason, env.projMgr.TableName("task")) {
				listed = true
			}
		}
		if !listed {
			t.Errorf("dry run %v: NeedsReproject = %+v, want %s naming its missing row", dryRun,
				report.NeedsReproject, f.alreadyOwned)
		}
		for _, r := range report.Resources {
			if r.ID == f.alreadyOwned {
				t.Errorf("dry run %v: the run selects %s to write", dryRun, f.alreadyOwned)
			}
		}
	}
	// The three ownerless resources were assigned, one event each, and
	// nothing was written for the missing row.
	if got := env.eventCount(t) - events; got != 3 {
		t.Errorf("the run stored %d event(s), want 3", got)
	}

	env.stop()
	env.reproject(t, "a reproject after the run")
	env.requireOwner(t, "task", f.alreadyOwned, env.ownerAccount, "after a reproject")
}

// Copilot on #586 and wm-oyoqao: a resource whose record has the owner and
// whose type row another account owns is missing from the record owner's
// listing. The upsert keeps an owner a row already has, in a reproject too,
// so the preview and the run list it apart from the ones left for a
// reproject, with the table and the account the row holds, and write nothing
// to it.
func TestAssignOwner_ATypeRowOwnedByAnotherAccountIsListedApart(t *testing.T) {
	env := newAssignOwnerEnv(t)
	f := env.seed(t)
	table := env.projMgr.TableName("task")
	if err := env.db.Table(table).Where("id = ?", f.alreadyOwned).
		Update("account_id", env.otherAccount).Error; err != nil {
		t.Fatalf("give the type row another owner: %v", err)
	}
	events := env.eventCount(t)

	for _, dryRun := range []bool{true, false} {
		report, err := env.backfill.AssignOwner(context.Background(), application.AssignOwnerCommand{
			AccountID: env.ownerAccount, CreatedBy: env.ownerAgent, DryRun: dryRun,
		})
		if err != nil {
			t.Fatalf("dry run %v: %v", dryRun, err)
		}
		var listed bool
		for _, r := range report.ForeignRows {
			if r.ID == f.alreadyOwned && r.Table == table && r.RowAccountID == env.otherAccount &&
				r.RecordAccountID == env.ownerAccount {
				listed = true
			}
		}
		if !listed {
			t.Errorf("dry run %v: ForeignRows = %+v, want %s with %s owned by %s", dryRun,
				report.ForeignRows, f.alreadyOwned, table, env.otherAccount)
		}
		for _, s := range report.NeedsReproject {
			if s.ID == f.alreadyOwned {
				t.Errorf("dry run %v: %s is listed for a reproject, which keeps the row's owner", dryRun, f.alreadyOwned)
			}
		}
		for _, r := range report.Resources {
			if r.ID == f.alreadyOwned {
				t.Errorf("dry run %v: the run selects %s to write", dryRun, f.alreadyOwned)
			}
		}
	}
	// The three ownerless resources were assigned, one event each, and
	// nothing was written for the row owned elsewhere.
	if got := env.eventCount(t) - events; got != 3 {
		t.Errorf("the run stored %d event(s), want 3", got)
	}
	if _, projection := env.owners(t, "task", f.alreadyOwned); projection != env.otherAccount {
		t.Errorf("after the run the type row is owned by %q, want it left with %s", projection, env.otherAccount)
	}

	// A reproject is no repair either: the report must not send the
	// operator to one.
	env.stop()
	env.reproject(t, "a reproject after the run")
	if _, projection := env.owners(t, "task", f.alreadyOwned); projection != env.otherAccount {
		t.Logf("a reproject moved the type row to %q", projection)
		t.Errorf("a reproject changed the row's owner from %s; the report's advice must say so", env.otherAccount)
	}
}
