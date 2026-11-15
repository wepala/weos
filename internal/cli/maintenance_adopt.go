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
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	gormdb "github.com/wepala/weos/v3/infrastructure/database/gorm"
	"github.com/wepala/weos/v3/internal/config"

	authgorm "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/database/gorm"

	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

var maintenanceAdoptAccountCmd = &cobra.Command{
	Use:   "adopt-account",
	Short: "Move every resource of an account with no members to another account",
	Long: `Moves the owner of every resource whose account is the one named by --from
to the account named by --account, through the event stream: one
Resource.AccountAdopted event per resource, through the same unit of work as
any other write, so the projection tables, the knowledge graph and the search
index all follow it, and a rebuild from events keeps it.

An account with no members (no account_members row) cannot be signed in to by
anyone, so nobody can list, change or delete what it owns. This is the one
command that moves a resource that has an owner, and it moves only out of
such an account. "weos maintenance assign-owner" still only fills an empty
owner.

It refuses unless:
  - the --from account exists and has no members;
  - the --account account exists and is not the --from account;
  - the --agent agent is an owner or admin of the --account account.

It never touches a resource of any other account. Each move is
compare-and-set on the --from account, so a resource that left it is
skipped, and a second run moves nothing. The --from account is never deleted
by this command. A move cannot be undone; only a database restore reverses a
run.

PREVIEW BY DEFAULT: without --write the command names both accounts, shows
the member check, counts the resources to move per type, and writes nothing.
Only --write moves.

Procedure:

  1. Upgrade the instance to this release. The preview (no flag) may run
     while the instance serves: it opens the database read-only and starts
     nothing, so it writes nothing.
  2. Preview first: run with no flag. Read both account names, the member
     count of the --from account and the counts per type.
  3. Stop the instance for the real run, and back up the database. A run
     with --write opens the store directly, and a serving instance competes
     with it for the database (on SQLite, for the write lock).
  4. Run again with --write.

Where the ids come from. Run these against the same database:

  --from     SELECT a.id, a.name, COUNT(r.id) FROM accounts a
               JOIN resources r ON r.account_id = a.id
               WHERE NOT EXISTS (SELECT 1 FROM account_members m
                                 WHERE m.account_id = a.id)
               GROUP BY a.id, a.name;
  --account  SELECT a.id, a.name, m.role_id FROM account_members m
  --agent      JOIN accounts a ON a.id = m.account_id
               WHERE m.agent_id = '<agent id>';
             or, from a person's email address:
             SELECT agent_id FROM credentials WHERE email = '<address>';

A resource is counted as moved only when its record and each of its
projection rows read back in the --account account. A run that stops part-way
has moved what it reports; running it again with --write moves the rest. A
resource whose record is behind its event stream is skipped and listed, and
the command exits non-zero: stop the instance, run "weos worker reproject"
against the same database, then run this command again. A failed write can
leave a projection row in the --from account under a record that moved; the
run then stops and names the resource. Fix the cause and run again: the run
moves the row and records no second event. A resource of the --from account
with a projection row a third account holds is listed and never moved, and
the run exits non-zero: no write this command makes repairs that row, so it
needs investigation first.

What a run with --write changes besides the owner: every moved row's
updated_at is set to the time of the run.

Erasing the --from account afterwards ("weos account delete") leaves every
moved resource and its history: erasure skips a resource whose history moved
it out of the account. Erase it only with this release or later. An older
binary's erasure deletes the history of every moved resource, because each
one's Resource.Created still names the --from account, and the next
"weos worker reproject" then removes them from the --account account.

If a binary older than this release served during or after a run with
--write, its background workers skipped the new event, so the knowledge graph
and the search index still file the resources under the --from account. With
this release, run "weos worker checkpoint reset oxigraph" and
"weos worker checkpoint reset lexical-index", then start the instance; the
workers replay the history.

The command refuses to run without an explicit DATABASE_DSN or --database-dsn.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runMaintenanceAdoptAccount,
}

func init() {
	maintenanceAdoptAccountCmd.Flags().String("from", "",
		"the account with no members whose resources are moved (required)")
	maintenanceAdoptAccountCmd.Flags().String("account", "", "the account to move the resources to (required)")
	maintenanceAdoptAccountCmd.Flags().String("agent", "",
		"an agent that is owner or admin of --account, who answers for the move (required)")
	maintenanceAdoptAccountCmd.Flags().Bool("write", false,
		"apply the move (default is a preview that counts the resources and writes nothing)")
	maintenanceCmd.AddCommand(maintenanceAdoptAccountCmd)
}

func runMaintenanceAdoptAccount(cmd *cobra.Command, _ []string) error {
	from, _ := cmd.Flags().GetString("from")
	accountID, _ := cmd.Flags().GetString("account")
	agentID, _ := cmd.Flags().GetString("agent")
	write, _ := cmd.Flags().GetBool("write")
	dryRun := !write
	if from == "" || accountID == "" || agentID == "" {
		return errors.New("adopt-account needs --from, --account and --agent")
	}
	if err := requireExplicitDSN("maintenance adopt-account"); err != nil {
		return err
	}
	appCfg := GetConfig().Config

	var svc *application.AccountAdoptionService
	if dryRun {
		tx, done, err := openReadOnlyStore(cmd.Context(), appCfg.DatabaseDSN)
		if err != nil {
			return err
		}
		defer done()
		svc = application.NewAccountAdoptionPlanner(gormdb.ProvideAccountResourceFinder(tx),
			authgorm.NewAccountRepository(tx), gormdb.ProvideAccountMemberCounter(tx))
	} else {
		started, stop, err := startAccountAdoption(cmd.Context(), appCfg)
		if err != nil {
			return err
		}
		defer stop()
		svc = started
	}

	out := cmd.OutOrStdout()
	plan, err := svc.PlanAdoptAccount(cmd.Context(), application.AdoptAccountCommand{
		FromAccountID: from, AccountID: accountID, AgentID: agentID, DryRun: dryRun,
	})
	switch {
	case errors.Is(err, application.ErrAdoptSourceNotFound):
		return fmt.Errorf("no account with id %s exists on this instance", from)
	case errors.Is(err, application.ErrAdoptSourceHasMembers):
		return fmt.Errorf("account %s has %d member(s), so someone can still use it. adopt-account moves only "+
			"out of an account with no members", from, plan.FromMembers)
	case errors.Is(err, application.ErrAdoptTargetNotFound):
		return fmt.Errorf("no account with id %s exists on this instance", accountID)
	case errors.Is(err, application.ErrAdoptSameAccount):
		return fmt.Errorf("--from and --account are the same account (%s)", accountID)
	case errors.Is(err, application.ErrAdoptAgentNotAdmin):
		return fmt.Errorf("agent %s is not an owner or admin of account %s (%w)", agentID, accountID, err)
	case err != nil:
		return err
	}
	// Both accounts are named before anything is written.
	printAdoptAccountTarget(out, plan)
	if dryRun {
		printAdoptAccountReport(out, plan)
		return nil
	}
	report, err := svc.ApplyAdoptAccount(cmd.Context(), plan)
	// Printed even when the run stopped part-way: each resource commits on
	// its own, so the operator needs to know what already landed.
	printAdoptAccountReport(out, report)
	return adoptAccountOutcome(report, err)
}

// startAccountAdoption starts the application against the store for a real
// run, and returns the service with the function that stops it.
func startAccountAdoption(ctx context.Context, appCfg config.Config) (*application.AccountAdoptionService, func(), error) {
	var svc *application.AccountAdoptionService
	app := fx.New(
		fx.NopLogger,
		application.Module(appCfg, presets.NewDefaultRegistry()),
		fx.Populate(&svc),
	)
	if err := app.Err(); err != nil {
		return nil, nil, fmt.Errorf("could not build the account adoption: %w", err)
	}
	startCtx, startCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		return nil, nil, fmt.Errorf("failed to start (check DATABASE_DSN): %s",
			redactDSN(rootCause(err).Error(), appCfg.DatabaseDSN))
	}
	return svc, func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer stopCancel()
		// Each move committed on its own before this point, so a failed
		// shutdown loses nothing, and the process is about to exit.
		_ = app.Stop(stopCtx)
	}, nil
}

// adoptAccountOutcome is the command's result. A run that left resources for
// a reproject, or met a projection row of a third account, did not finish its
// job, so it fails even though it went on past them.
func adoptAccountOutcome(r application.AdoptAccountReport, err error) error {
	if err != nil {
		return err
	}
	var left []string
	if n := len(r.NeedsReproject); n > 0 {
		left = append(left, fmt.Sprintf("%d resource(s) left for a reproject", n))
	}
	if n := len(r.ForeignRows); n > 0 {
		left = append(left, fmt.Sprintf("%d resource(s) with a projection row of a third account, not moved", n))
	}
	if len(left) > 0 {
		return fmt.Errorf("%s; see the lists above", strings.Join(left, ", "))
	}
	return nil
}

// printAdoptAccountTarget names both accounts and the checks before
// anything is written.
func printAdoptAccountTarget(out io.Writer, r application.AdoptAccountReport) {
	if r.DryRun {
		_, _ = fmt.Fprintln(out, "adopt-account: DRY RUN — nothing was written. Re-run with --write to apply.")
	} else {
		_, _ = fmt.Fprintln(out, "adopt-account: moving resources through the event stream. A move cannot be undone.")
	}
	_, _ = fmt.Fprintf(out, "from %s (%s): %d members\n", r.FromAccountName, r.FromAccountID, r.FromMembers)
	_, _ = fmt.Fprintf(out, "to   %s (%s): %s is %s\n", r.AccountName, r.AccountID, r.AgentID, r.AgentRole)
}

// adoptOneEventEach says what a move stores, beside the count of them.
const adoptOneEventEach = "one Resource.AccountAdopted event each"

// printAdoptAccountReport renders what a run selected (a preview) or did (a
// run with --write), one line per type, then the resources it left.
func printAdoptAccountReport(out io.Writer, r application.AdoptAccountReport) {
	if len(r.Types) == 0 {
		_, _ = fmt.Fprintln(out, "nothing to move: the account owns no live resource.")
		return
	}
	_, _ = fmt.Fprintln(out)
	missing := 0
	for _, res := range r.Resources {
		if res.MissingTable != "" {
			missing++
		}
	}
	for _, slug := range r.TypeSlugs() {
		c := r.Types[slug]
		if r.DryRun {
			_, _ = fmt.Fprintf(out, "  %-24s %d to move", slug, c.ToMove)
			if c.RowsToMove > 0 {
				_, _ = fmt.Fprintf(out, "; %d row(s) left in the source by an earlier move", c.RowsToMove)
			}
		} else {
			_, _ = fmt.Fprintf(out, "  %-24s %d to move, moved %d", slug, c.ToMove, c.Moved)
			if c.RowsToMove > 0 {
				_, _ = fmt.Fprintf(out, "; %d row(s) left in the source, moved %d", c.RowsToMove, c.RowsMoved)
			}
			if c.Skipped > 0 {
				_, _ = fmt.Fprintf(out, ", skipped %d that left the source account during the run", c.Skipped)
			}
		}
		if c.NeedsReproject > 0 {
			_, _ = fmt.Fprintf(out, ", %d left for a reproject", c.NeedsReproject)
		}
		if c.ForeignRows > 0 {
			_, _ = fmt.Fprintf(out, ", %d with a projection row of a third account", c.ForeignRows)
		}
		_, _ = fmt.Fprintln(out)
	}
	if r.DryRun {
		_, _ = fmt.Fprintf(out, "\ntotal: %d resource(s) would be moved (%s)\n", r.ToMove(), adoptOneEventEach)
		if n := r.RowsToMove(); n > 0 {
			_, _ = fmt.Fprintf(out, "       %d resource(s) with a projection row still in the source to move (no event)\n", n)
		}
		if missing > 0 {
			_, _ = fmt.Fprintf(out, "       %d of them have no row in one of their projection tables; the move writes it\n",
				missing)
		}
	} else {
		_, _ = fmt.Fprintf(out, "\ntotal: %d of %d resource(s) moved", r.Moved(), r.ToMove())
		if len(r.NeedsReproject) > 0 {
			_, _ = fmt.Fprintf(out, ", %d left for a reproject", len(r.NeedsReproject))
		}
		_, _ = fmt.Fprintf(out, " (%s)\n", adoptOneEventEach)
		if n := r.RowsToMove(); n > 0 {
			_, _ = fmt.Fprintf(out, "       %d of %d projection row(s) left in the source moved (no event)\n",
				r.RowsMoved(), n)
		}
	}
	if len(r.NeedsReproject) > 0 {
		_, _ = fmt.Fprintln(out, "\nThese resources are left for a reproject. Some may already have a stored move: a run")
		_, _ = fmt.Fprintln(out, "stores the event first and then reads the projection back.")
		for _, s := range r.NeedsReproject {
			_, _ = fmt.Fprintf(out, "  %s (%s): %s\n", s.ID, s.TypeSlug, s.Reason)
		}
		_, _ = fmt.Fprintln(out, "\nStop the instance and run `weos worker reproject` against the same database, then")
		_, _ = fmt.Fprintln(out, "run this command again.")
	}
	if len(r.ForeignRows) > 0 {
		_, _ = fmt.Fprintln(out, "\nThese resources have a projection row a third account holds. They are never moved:")
		_, _ = fmt.Fprintln(out, "no write this command makes changes that row. Each one needs investigation first:")
		for _, f := range r.ForeignRows {
			_, _ = fmt.Fprintf(out, "  %s (%s): %s\n", f.ID, f.TypeSlug, f.Reason)
		}
	}
}
