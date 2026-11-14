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
	maintenanceAssignOwnerCmd.Flags().Bool("dry-run", false,
		"list the ownerless resources (id, type, created_at) and the counts per type, and change nothing")
	maintenanceAssignOwnerCmd.Flags().String("created-from", "",
		"only resources created at or after this time (RFC 3339, or a date YYYY-MM-DD meaning 00:00 UTC)")
	maintenanceAssignOwnerCmd.Flags().String("created-to", "",
		"only resources created at or before this time (RFC 3339, or a date YYYY-MM-DD meaning that whole day, UTC)")
	maintenanceAssignOwnerCmd.Flags().String("confirm-account", "",
		"repeat the --account id; a real run requires it when the --created-by agent is a member of more than one account")
	maintenanceCmd.AddCommand(maintenanceAssignOwnerCmd)
	rootCmd.AddCommand(maintenanceCmd)
}

func runMaintenanceAssignOwner(cmd *cobra.Command, _ []string) error {
	accountID, _ := cmd.Flags().GetString("account")
	createdBy, _ := cmd.Flags().GetString("created-by")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	confirm, _ := cmd.Flags().GetString("confirm-account")
	fromFlag, _ := cmd.Flags().GetString("created-from")
	toFlag, _ := cmd.Flags().GetString("created-to")
	if accountID == "" || createdBy == "" {
		return errors.New("assign-owner needs both --account and --created-by")
	}
	createdFrom, err := parseCreatedBound("created-from", fromFlag, false)
	if err != nil {
		return err
	}
	createdTo, err := parseCreatedBound("created-to", toFlag, true)
	if err != nil {
		return err
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

	out := cmd.OutOrStdout()
	plan, err := svc.PlanAssignOwner(cmd.Context(), application.AssignOwnerCommand{
		AccountID: accountID, CreatedBy: createdBy, DryRun: dryRun,
		CreatedFrom: createdFrom, CreatedTo: createdTo, ConfirmAccount: confirm,
	})
	switch {
	case errors.Is(err, application.ErrOwnerAccountNotFound):
		return fmt.Errorf("no account with id %s exists on this instance", accountID)
	case errors.Is(err, application.ErrOwnerAgentNotMember):
		return fmt.Errorf("agent %s is not a member of account %s, so the account cannot own what it wrote",
			createdBy, accountID)
	case errors.Is(err, application.ErrOwnerAccountNotConfirmed):
		return fmt.Errorf("%w.\nAn assignment cannot be undone. Run with --dry-run to see the account and the rows, "+
			"then repeat the account to confirm it: --confirm-account %s", err, accountID)
	case err != nil:
		return err
	}
	// The account is named before anything is written.
	printAssignOwnerTarget(out, plan)
	if dryRun {
		printAssignOwnerReport(out, plan)
		return nil
	}
	report, err := svc.ApplyAssignOwner(cmd.Context(), plan)
	// Printed even when the run stopped part-way: each resource commits on
	// its own, so the operator needs to know what already landed.
	printAssignOwnerReport(out, report)
	return assignOwnerOutcome(report, err)
}

// parseCreatedBound reads a --created-from or --created-to value: RFC 3339,
// or a date (YYYY-MM-DD) in UTC. A date as the window's end takes that whole
// day. An empty value is an open end, the zero time.
func parseCreatedBound(flag, value string, end bool) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t.UTC(), nil
	}
	day, err := time.ParseInLocation(time.DateOnly, value, time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("--%s %q is not a time: use RFC 3339 (2026-08-01T00:00:00Z) "+
			"or a date (2026-08-01)", flag, value)
	}
	if end {
		return day.Add(24*time.Hour - time.Nanosecond), nil
	}
	return day, nil
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

// printAssignOwnerTarget says what a run is about to do, and to which
// account, before anything is written.
func printAssignOwnerTarget(out io.Writer, r application.AssignOwnerReport) {
	if r.DryRun {
		_, _ = fmt.Fprintln(out, "assign-owner: DRY RUN — nothing was written. Re-run without --dry-run to apply.")
	} else {
		_, _ = fmt.Fprintln(out, "assign-owner: assigning owners through the event stream. An assignment cannot be undone.")
	}
	account := r.AccountID
	if r.AccountName != "" {
		account = fmt.Sprintf("%s (%s)", r.AccountName, r.AccountID)
	}
	_, _ = fmt.Fprintf(out, "account %s, resources created by %s\n", account, r.CreatedBy)
	if !r.CreatedFrom.IsZero() || !r.CreatedTo.IsZero() {
		_, _ = fmt.Fprintf(out, "created from %s to %s\n", windowBound(r.CreatedFrom), windowBound(r.CreatedTo))
	}
	if len(r.MemberOf) > 1 {
		names := make([]string, len(r.MemberOf))
		for i, a := range r.MemberOf {
			names[i] = fmt.Sprintf("%s (%s)", a.Name, a.ID)
		}
		_, _ = fmt.Fprintf(out, "note: %s is a member of %d accounts: %s.\n", r.CreatedBy, len(r.MemberOf),
			strings.Join(names, ", "))
		if r.DryRun {
			_, _ = fmt.Fprintf(out, "      A real run needs the account repeated: --confirm-account %s\n", r.AccountID)
		}
	}
}

func windowBound(t time.Time) string {
	if t.IsZero() {
		return "(open)"
	}
	return t.UTC().Format(time.RFC3339)
}

// printAssignOwnerReport renders what a run selected (a dry run) or did (a
// real run), one line per type. A dry run also lists every resource.
func printAssignOwnerReport(out io.Writer, r application.AssignOwnerReport) {
	if len(r.Types) == 0 {
		_, _ = fmt.Fprintln(out, "nothing to assign: the agent has no resource without an owner"+
			" (in the window, when one is given). Check that --created-by is the agent id that wrote them.")
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
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintf(out, "  %-20s  %-24s %s\n", "CREATED AT", "TYPE", "ID")
		for _, res := range r.Resources {
			_, _ = fmt.Fprintf(out, "  %-20s  %-24s %s\n", res.CreatedAt.UTC().Format(time.RFC3339), res.TypeSlug, res.ID)
		}
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
