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
	"github.com/wepala/weos/v3/infrastructure/logging"
	storageprovider "github.com/wepala/weos/v3/infrastructure/storage/provider"
	"github.com/wepala/weos/v3/internal/config"

	authgorm "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/database/gorm"

	"github.com/spf13/cobra"
	"go.uber.org/fx"
	"go.uber.org/zap"
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
  - the --from account exists, has no members and has no pending invitation
    that has not expired (accepting one would make a member);
  - the --account account exists and is not the --from account;
  - the --agent agent is an owner or admin of the --account account;
  - the --from account has no uploaded file in its folder and no live
    resource that names one. The command moves resources, not files: the
    file route serves a file in an account's folder only to that account,
    and erasing the account deletes the folder, so a moved resource would
    name a file its new account can never read. Moving uploads is not
    supported yet.
A run with --write reads the member, invitation and upload checks again
before it moves anything.

It never touches a resource of any other account. Each move is
compare-and-set on the --from account, so a resource that left it is
skipped, and a second run moves nothing. The --from account is never deleted
by this command. A move cannot be undone; only a database restore reverses a
run.

PREVIEW BY DEFAULT: without --write the command names both accounts, shows
the member, invitation and upload checks, counts the resources to move per type, and
writes nothing. Only --write moves.

The preview also lists each agent that created the --from account's
resources: how many, when it last wrote one, and whether it still has a
credential. An agent keeps access to what it created with no membership at
all, so a creator that can still sign in, or that wrote recently, means the
account may still be in use. The command does not refuse on this; read it
before you add --write.

Procedure:

  1. Upgrade the instance to this release. The preview (no flag) may run
     while the instance serves: it opens the database read-only and starts
     nothing, so it writes nothing.
  2. Preview first: run with no flag. Read both account names, the member
     count of the --from account and the counts per type.
  3. Stop the instance for the real run, and back up the database. A run
     with --write opens the store directly, and a serving instance competes
     with it for the database (on SQLite, for the write lock).
  4. Run again with --write. It prints its progress every 100 resources and
     when each type is done; a run stopped part-way is safe to re-run.
  5. Start the instance. The search index and the knowledge graph catch up
     with the moves in the background at start: wait until memory_search
     stops reporting that its index is behind before you judge the result.

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
against the same database, then run this command again. A failed write, or a run
stopped after a record write, can leave a projection row of a moved record in
the --from account, with no owner, or missing; a failed write stops the run
and names the resource. Fix the cause and run again: the run finds the row,
checks that the resource's history holds the move, moves, fills or writes the
row, and records no second event. A resource of the --from account
with a projection row a third account holds is listed and never moved, and
the run exits non-zero: no write this command makes repairs that row, so it
needs investigation first.

What a run with --write changes besides the owner: every moved row's
updated_at is set to the time of the run, so lists sorted by last update lead
with the moved resources. The --agent agent gets a read grant on each moved
resource: lists and memory_search show a caller what it created or holds a
grant on, and a moved resource keeps its creator. Other members of the
--account account still do not see the moved resources in lists or search.
A grant the agent already holds is kept, so a re-run grants nothing twice.
The knowledge graph carries each moved resource's relations into the
--account account's graph.

Erasing the --from account afterwards ("weos account delete") leaves every
moved resource and its history: erasure skips a resource whose history moved
it out of the account. Erase it only with this release or later. An older
binary's erasure deletes the history of every moved resource, because each
one's Resource.Created still names the --from account, and the next
"weos worker reproject" then removes them from the --account account.

Never serve a binary older than this release after a run with --write. Such
a binary does not know Resource.AccountAdopted, and it fails to load a moved
resource's history with "unknown event type: ...": updating or deleting a
moved resource, removing an import that holds one, and "weos worker
reproject" all fail. Its background workers also skip the event, so the
knowledge graph and the search index keep the resources under the --from
account. If one served, upgrade to this release, run
"weos worker checkpoint reset oxigraph" and
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
		// The preview reads the file storage too, to count the source
		// account's uploads; it creates nothing there.
		files, closeFiles, err := storageprovider.NewFileService(appCfg.Storage, logging.NewZapLogger(zap.NewNop()))
		if err != nil {
			return fmt.Errorf("open the file storage: %w", err)
		}
		// The client was only read through, so a failed close loses nothing.
		defer func() { _ = closeFiles() }()
		svc = application.NewAccountAdoptionPlanner(gormdb.ProvideAccountResourceFinder(tx),
			authgorm.NewAccountRepository(tx), gormdb.ProvideAccountUsageReader(tx), files)
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
	case errors.Is(err, application.ErrAdoptSourceHasInvites):
		return fmt.Errorf("account %s has %d pending invitation(s); accepting one makes a member. adopt-account "+
			"moves only out of an account nobody can use: revoke the invitations first", from, plan.FromInvites)
	case errors.Is(err, application.ErrAdoptSourceHasUploads):
		return fmt.Errorf("account %s has %d uploaded file(s) in its folder and %d resource(s) that name one. "+
			"adopt-account moves resources, not files: a moved resource would name a file only %s can read, "+
			"and erasing %s deletes the files. Moving uploads is not supported yet; nothing was moved",
			from, plan.FromFiles, plan.FromFileReferences, from, from)
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
	_, _ = fmt.Fprintf(out, "progress is printed every %d resources and when each type is done. "+
		"A run stopped part-way is safe to re-run: each resource commits on its own.\n", adoptProgressEvery)
	report, err := svc.ApplyAdoptAccountWithProgress(cmd.Context(), plan, adoptAccountProgressPrinter(out))
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

// printAdoptAccountNext says what a real run changed besides the owner, and
// what to do next (wm-ebgqob).
func printAdoptAccountNext(out io.Writer, r application.AdoptAccountReport) {
	_, _ = fmt.Fprintf(out, "       granted %s read on %d resource(s): lists and memory_search show a caller what\n"+
		"       it created or holds a grant on, and a moved resource keeps its creator. Other members of\n"+
		"       %s do not see the moved resources in lists or search.\n", r.AgentID, r.Granted(), r.AccountName)
	_, _ = fmt.Fprintln(out, "\nThe updated_at of every moved row is now the time of this run, so lists sorted by")
	_, _ = fmt.Fprintln(out, "last update lead with the moved resources.")
	_, _ = fmt.Fprintln(out, "Next: start the instance. The search index and the knowledge graph catch up with the")
	_, _ = fmt.Fprintln(out, "moves in the background at start; until memory_search stops reporting that its index")
	_, _ = fmt.Fprintln(out, "is behind, search can still show the moved resources under the old account or miss them.")
}

// printAdoptAccountTarget names both accounts and the checks before
// anything is written.
func printAdoptAccountTarget(out io.Writer, r application.AdoptAccountReport) {
	if r.DryRun {
		_, _ = fmt.Fprintln(out, "adopt-account: DRY RUN — nothing was written. Re-run with --write to apply.")
	} else {
		_, _ = fmt.Fprintln(out, "adopt-account: moving resources through the event stream. A move cannot be undone.")
	}
	_, _ = fmt.Fprintf(out, "from %s (%s): %d members, %d pending invitations, %d uploaded files, "+
		"%d resources naming one\n", r.FromAccountName, r.FromAccountID, r.FromMembers, r.FromInvites,
		r.FromFiles, r.FromFileReferences)
	// An agent keeps access to what it created with no membership, so the
	// creators say whether anyone is still using the account (wm-m0ci05).
	live := 0
	for _, c := range r.FromCreators {
		agent, signIn := c.AgentID, "no credential"
		if agent == "" {
			agent = "(no creator)"
		}
		if c.HasCredential {
			signIn = "HAS A CREDENTIAL"
			live++
		}
		_, _ = fmt.Fprintf(out, "     created by %s: %d resource(s), newest %s, %s\n",
			agent, c.Resources, c.NewestCreatedAt.UTC().Format(time.RFC3339), signIn)
	}
	if live > 0 {
		_, _ = fmt.Fprintf(out, "     WARNING: %d creator(s) can still sign in and reach what they created here. "+
			"Make sure the account is abandoned before you run with --write.\n", live)
	}
	_, _ = fmt.Fprintf(out, "to   %s (%s): %s is %s\n", r.AccountName, r.AccountID, r.AgentID, r.AgentRole)
}

// adoptProgressEvery is how many resources a real run handles between two
// progress lines; a line is also printed when each type is done.
const adoptProgressEvery = 100

// adoptAccountProgressPrinter prints a real run's progress, so an operator
// watching a run of thousands of resources can tell working from stuck
// (wm-ebgqob). Each line says a stopped run is safe to re-run.
func adoptAccountProgressPrinter(out io.Writer) func(application.AdoptAccountProgress) {
	return func(p application.AdoptAccountProgress) {
		if p.Done%adoptProgressEvery != 0 && p.TypeDone != p.TypeTotal {
			return
		}
		_, _ = fmt.Fprintf(out, "  progress: %d of %d resource(s) handled; %s %d of %d "+
			"(safe to re-run if stopped)\n", p.Done, p.Total, p.TypeSlug, p.TypeDone, p.TypeTotal)
	}
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
		if res.MissingTable != "" && !res.RecordMoved {
			missing++
		}
	}
	for _, slug := range r.TypeSlugs() {
		c := r.Types[slug]
		if r.DryRun {
			_, _ = fmt.Fprintf(out, "  %-24s %d to move", slug, c.ToMove)
			if c.RowsToMove > 0 {
				_, _ = fmt.Fprintf(out, "; %d row(s) an earlier move left unfinished", c.RowsToMove)
			}
		} else {
			_, _ = fmt.Fprintf(out, "  %-24s %d to move, moved %d", slug, c.ToMove, c.Moved)
			if c.RowsToMove > 0 {
				_, _ = fmt.Fprintf(out, "; %d row(s) an earlier move left unfinished, finished %d", c.RowsToMove, c.RowsMoved)
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
			_, _ = fmt.Fprintf(out, "       %d resource(s) whose move left a projection row in the source, ownerless "+
				"or missing, to finish (no event)\n", n)
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
			_, _ = fmt.Fprintf(out, "       %d of %d resource(s) with an unfinished projection row finished (no event)\n",
				r.RowsMoved(), n)
		}
		printAdoptAccountNext(out, r)
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
