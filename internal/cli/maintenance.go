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
	"time"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"

	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

var maintenanceCmd = &cobra.Command{
	Use:   "maintenance",
	Short: "One-off operator repairs of an instance's data",
}

var maintenanceAssignOwnerCmd = &cobra.Command{
	Use:   "assign-owner",
	Short: "Give an owning account to the resources one agent wrote with no account",
	Long: `Finds every resource, of every type, that has no owning account and was
created by the agent named by --created-by, and gives each one the account
named by --account as its owner.

A resource takes its owner from the session that wrote it. A session whose
active account was empty wrote resources with no owner, and account-scoped
reads never find them. This command repairs them through the event stream: it
records one Resource.AccountAssigned event per resource, through the same unit
of work as any other write, so the projection tables, the knowledge graph and
the search index all see the owner, and a rebuild from events keeps it.

It only ever fills an empty owner. A resource that has an owner is never
touched, and an owner is never moved from one account to another. The account
must exist and the agent must be a member of it.

--dry-run counts the ownerless resources per type and writes nothing. Run it
first. A real run that stops part-way has assigned what it reports; running it
again assigns the rest.

The command opens the store directly, so it needs no running server, and it
refuses to run without an explicit DATABASE_DSN or --database-dsn. Back up the
database before a real run.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runMaintenanceAssignOwner,
}

func init() {
	maintenanceAssignOwnerCmd.Flags().String("account", "", "the account id to give the resources as their owner (required)")
	maintenanceAssignOwnerCmd.Flags().String("created-by", "", "the agent id whose ownerless resources are assigned (required)")
	maintenanceAssignOwnerCmd.Flags().Bool("dry-run", false, "count the ownerless resources per type and change nothing")
	maintenanceCmd.AddCommand(maintenanceAssignOwnerCmd)
	rootCmd.AddCommand(maintenanceCmd)
}

func runMaintenanceAssignOwner(cmd *cobra.Command, _ []string) error {
	accountID, _ := cmd.Flags().GetString("account")
	createdBy, _ := cmd.Flags().GetString("created-by")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if accountID == "" || createdBy == "" {
		return errors.New("assign-owner needs both --account and --created-by")
	}
	if err := requireExplicitDSN("maintenance assign-owner"); err != nil {
		return err
	}
	appCfg := GetConfig().Config

	var svc *application.OwnerBackfillService
	app := fx.New(
		fx.NopLogger,
		application.Module(appCfg, presets.NewDefaultRegistry()),
		fx.Populate(&svc),
	)
	if err := app.Err(); err != nil {
		return fmt.Errorf("could not build the owner backfill: %w", err)
	}
	startCtx, startCancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start (check DATABASE_DSN): %s",
			redactDSN(rootCause(err).Error(), appCfg.DatabaseDSN))
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer stopCancel()
		_ = app.Stop(stopCtx)
	}()

	report, err := svc.AssignOwner(cmd.Context(), application.AssignOwnerCommand{
		AccountID: accountID, CreatedBy: createdBy, DryRun: dryRun,
	})
	switch {
	case errors.Is(err, application.ErrOwnerAccountNotFound):
		return fmt.Errorf("no account with id %s exists on this instance", accountID)
	case errors.Is(err, application.ErrOwnerAgentNotMember):
		return fmt.Errorf("agent %s is not a member of account %s, so the account cannot own what it wrote",
			createdBy, accountID)
	}
	// Printed even when the run stopped part-way: each resource commits on
	// its own, so the operator needs to know what already landed.
	printAssignOwnerReport(cmd.OutOrStdout(), report)
	return assignOwnerOutcome(report, err)
}

// assignOwnerOutcome is the command's result. A run that left resources for a
// reproject did not finish its job, so it fails even though it went on past
// them: a script must not read it as done.
func assignOwnerOutcome(r application.AssignOwnerReport, err error) error {
	if err != nil {
		return err
	}
	if n := len(r.NeedsReproject); n > 0 {
		return fmt.Errorf("%d resource(s) left for a reproject; see the list above", n)
	}
	return nil
}

// printAssignOwnerReport renders a run for the operator, one line per type.
func printAssignOwnerReport(out io.Writer, r application.AssignOwnerReport) {
	if r.DryRun {
		_, _ = fmt.Fprintln(out, "assign-owner: DRY RUN — nothing was written. Re-run without --dry-run to apply.")
	} else {
		_, _ = fmt.Fprintln(out, "assign-owner: owners assigned through the event stream.")
	}
	_, _ = fmt.Fprintf(out, "account %s, resources created by %s\n", r.AccountID, r.CreatedBy)
	if len(r.Types) == 0 {
		_, _ = fmt.Fprintln(out, "nothing to assign: the agent has no resource without an owner.")
		return
	}
	_, _ = fmt.Fprintln(out)
	for _, slug := range r.TypeSlugs() {
		c := r.Types[slug]
		if r.DryRun {
			_, _ = fmt.Fprintf(out, "  %-24s %d ownerless, would assign %d\n", slug, c.Ownerless, c.Ownerless)
			continue
		}
		_, _ = fmt.Fprintf(out, "  %-24s %d ownerless, assigned %d", slug, c.Ownerless, c.Assigned)
		if c.Skipped > 0 {
			_, _ = fmt.Fprintf(out, ", skipped %d that gained an owner during the run", c.Skipped)
		}
		if c.NeedsReproject > 0 {
			_, _ = fmt.Fprintf(out, ", %d left for a reproject", c.NeedsReproject)
		}
		_, _ = fmt.Fprintln(out)
	}
	if r.DryRun {
		_, _ = fmt.Fprintf(out, "\ntotal: %d ownerless resource(s) would be assigned\n", r.Ownerless())
		return
	}
	_, _ = fmt.Fprintf(out, "\ntotal: %d of %d ownerless resource(s) assigned", r.Assigned(), r.Ownerless())
	if len(r.NeedsReproject) > 0 {
		_, _ = fmt.Fprintf(out, ", %d left for a reproject", len(r.NeedsReproject))
	}
	_, _ = fmt.Fprintln(out)
	printNeedsReproject(out, r.NeedsReproject)
}

// printNeedsReproject lists the resources a run skipped because their read
// model is behind the event store, and the one repair for that.
func printNeedsReproject(out io.Writer, skipped []application.AssignOwnerSkip) {
	if len(skipped) == 0 {
		return
	}
	_, _ = fmt.Fprintln(out, "\nThese resources are still ownerless. Their read model is behind the event store, so")
	_, _ = fmt.Fprintln(out, "this run did not write to them:")
	for _, s := range skipped {
		_, _ = fmt.Fprintf(out, "  %s (%s): %s\n", s.ID, s.TypeSlug, s.Reason)
	}
	_, _ = fmt.Fprintln(out, "\nStop the instance and run `weos worker reproject` against the same database, then")
	_, _ = fmt.Fprintln(out, "run this command again. A reproject applies an assignment already in the event store,")
	_, _ = fmt.Fprintln(out, "and brings every other record up to its events so the next run can assign it.")
}
