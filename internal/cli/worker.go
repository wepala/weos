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
	"os"
	"text/tabwriter"
	"time"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"
	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Inspect and operate the background event subscribers",
	Long: `Manage the background subscriber runtime: list and replay parked
(poison) events that a handler could not process after exhausting its retries,
and reset checkpoints to rebuild a projection.

These commands do not start the background workers themselves. The parked-event
inspection/replay and a plain "checkpoint reset" only touch the checkpoint and
parked-event tables, so they are safe to run while a server is processing.
"checkpoint reset --truncate" is the exception — it clears the projection table,
which races with a live subscriber or the API still writing it; run it with the
server stopped (or the affected group otherwise idle).`,
}

var workerParkedCmd = &cobra.Command{
	Use:   "parked",
	Short: "Inspect and replay parked (poison) events",
}

var workerParkedListCmd = &cobra.Command{
	Use:   "list",
	Short: "List parked events across subscribers",
	Long: `Lists events that were parked after a handler failed on them repeatedly.
The events behind a parked event keep flowing; fix the underlying cause and
replay the event with "weos worker parked replay".`,
	RunE: runWorkerParkedList,
}

var workerParkedReplayCmd = &cobra.Command{
	Use:   "replay <subscriber> <event-id>",
	Short: "Replay a parked event (re-runs the handler; clears it on success)",
	Args:  cobra.ExactArgs(2),
	RunE:  runWorkerParkedReplay,
}

var workerCheckpointCmd = &cobra.Command{
	Use:   "checkpoint",
	Short: "Manage subscriber checkpoints",
}

var workerCheckpointResetCmd = &cobra.Command{
	Use:   "reset <subscriber>",
	Short: "Reset a subscriber's checkpoint to 0 so it replays all history",
	Long: `Resets the subscriber's checkpoint to 0; on its next run it replays the
whole event history — the one mechanism for rebuild, recovery, and backfill.
Replay is incremental and resumable (interrupting and restarting continues from
where it left off). With --truncate, the subscriber's projection is cleared
first so the replay rebuilds it from empty; because that delete races with any
live subscriber or API write to the same table, run --truncate only with the
server stopped (or the affected group otherwise idle).`,
	Args: cobra.ExactArgs(1),
	RunE: runWorkerCheckpointReset,
}

var workerReprojectCmd = &cobra.Command{
	Use:   "reproject",
	Short: "Replay the event feed through the synchronous projections (resource types, resources, triples)",
	Long: `Streams the whole event history, in global position order, through the same
synchronous projection handlers the write path uses, materializing the
resource-type, canonical-resource, and triple read models — including the
per-type projection tables.

This is the rebuild rail for projections that "checkpoint reset" cannot reach:
those handlers run inline at commit time and have no checkpoint. Its intended
use is an event store that arrived without its projections — a pericarp
export/import migration, a restored backup — where the history is complete but
every synchronous read model is empty.

Run it with the server STOPPED: a live server is writing the same tables. The
handlers are idempotent, so an interrupted run can simply be re-run (or resumed
with --after-position from the position the error message reports). Events
owned by the checkpointed background groups are skipped here — rebuild those
with "worker checkpoint reset <subscriber>" as usual.`,
	Args: cobra.NoArgs,
	RunE: runWorkerReproject,
}

func init() {
	workerParkedListCmd.Flags().String("subscriber", "", "limit to a single subscriber")
	workerParkedCmd.AddCommand(workerParkedListCmd, workerParkedReplayCmd)

	workerCheckpointResetCmd.Flags().Bool("truncate", false, "clear the subscriber's projection before replay")
	workerCheckpointCmd.AddCommand(workerCheckpointResetCmd)

	workerReprojectCmd.Flags().Int64("after-position", 0, "resume: replay only events after this global position")
	workerReprojectCmd.Flags().Int("batch-size", 500, "events read per batch")

	workerCmd.AddCommand(workerParkedCmd, workerCheckpointCmd, workerReprojectCmd)
	rootCmd.AddCommand(workerCmd)
}

// runWorkerReproject drives application.Reproject over a deliberately NARROW
// fx graph (application.ReprojectModule), not withWorkerManager's full
// application.Module: the full module runs ensureBuiltInResourceTypes at
// start, which decides type existence from the projection tables — empty on
// exactly the freshly-imported store reproject exists to fix — and would
// append duplicate built-in ResourceType.Created events (fresh IDs) into the
// imported history before replay ever ran (#443).
func runWorkerReproject(cmd *cobra.Command, _ []string) error {
	after, _ := cmd.Flags().GetInt64("after-position")
	batch, _ := cmd.Flags().GetInt("batch-size")
	appCfg := GetConfig().Config

	var rt application.ReprojectRuntime
	app := fx.New(
		fx.NopLogger,
		application.ReprojectModule(appCfg),
		fx.Populate(&rt),
	)

	startCtx, startCancel := context.WithTimeout(cmd.Context(), fx.DefaultTimeout)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start reproject runtime: %w", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer stopCancel()
		_ = app.Stop(stopCtx)
	}()

	res, err := application.Reproject(cmd.Context(), rt, application.ReprojectOptions{
		AfterPosition: after,
		BatchSize:     batch,
	})
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout,
		"reprojected %d events (%d skipped) through positions %d..%d\n",
		res.Dispatched, res.Skipped, after, res.LastPosition)
	return nil
}

// withWorkerManager builds the application graph, hands the worker Manager to
// fn, then shuts down. WorkerConfig.RunInProcess is left false here, so the
// background subscribers are constructed (and thus available for inspection)
// but never started for these short-lived commands.
func withWorkerManager(cmd *cobra.Command, fn func(ctx context.Context, m *application.Manager) error) error {
	appCfg := GetConfig().Config
	// Inspect/operate only: never start background workers from the CLI, even
	// on a host that sets WORKER_RUN_IN_PROCESS for its server process.
	appCfg.Worker.RunInProcess = false

	var manager *application.Manager
	app := fx.New(
		fx.NopLogger,
		application.Module(appCfg, presets.NewDefaultRegistry()),
		fx.Populate(&manager),
	)

	startCtx, startCancel := context.WithTimeout(cmd.Context(), fx.DefaultTimeout)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start application: %w", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer stopCancel()
		_ = app.Stop(stopCtx)
	}()

	return fn(cmd.Context(), manager)
}

func runWorkerParkedList(cmd *cobra.Command, _ []string) error {
	subscriber, _ := cmd.Flags().GetString("subscriber")
	return withWorkerManager(cmd, func(ctx context.Context, m *application.Manager) error {
		parked, err := m.ParkedEvents(ctx, subscriber)
		if err != nil {
			return err
		}
		if len(parked) == 0 {
			_, _ = fmt.Fprintln(os.Stdout, "No parked events.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "SUBSCRIBER\tEVENT ID\tTYPE\tPOSITION\tATTEMPTS\tPARKED AT\tERROR")
		for _, p := range parked {
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
				p.Subscriber, p.EventID, p.EventType, p.Position, p.Attempts,
				p.ParkedAt.Format(time.RFC3339), p.Error)
		}
		_ = w.Flush()
		_, _ = fmt.Fprintf(os.Stdout, "\n%d parked event(s).\n", len(parked))
		return nil
	})
}

func runWorkerParkedReplay(cmd *cobra.Command, args []string) error {
	subscriber, eventID := args[0], args[1]
	return withWorkerManager(cmd, func(ctx context.Context, m *application.Manager) error {
		if err := m.ReplayParked(ctx, subscriber, eventID); err != nil {
			// Only the handler-failure case actually leaves the row parked;
			// a not-parked id (typo, or already cleared) has no row to retry.
			if errors.Is(err, subscriptions.ErrEventNotParked) {
				return fmt.Errorf("no parked event %s for subscriber %s: %w", eventID, subscriber, err)
			}
			return fmt.Errorf("replay failed (event left parked): %w", err)
		}
		_, _ = fmt.Fprintf(os.Stdout, "Replayed event %s on subscriber %s; parked row cleared.\n",
			eventID, subscriber)
		return nil
	})
}

func runWorkerCheckpointReset(cmd *cobra.Command, args []string) error {
	subscriber := args[0]
	truncate, _ := cmd.Flags().GetBool("truncate")
	return withWorkerManager(cmd, func(ctx context.Context, m *application.Manager) error {
		if err := m.ResetCheckpoint(ctx, subscriber, truncate); err != nil {
			return err
		}
		msg := "checkpoint reset to 0"
		if truncate {
			msg = "projection cleared and " + msg
		}
		_, _ = fmt.Fprintf(os.Stdout,
			"Subscriber %s: %s. It will replay history on its next run.\n", subscriber, msg)
		return nil
	})
}
