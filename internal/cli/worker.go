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
(poison) events that a handler could not process after exhausting its retries.

These commands do not start the background workers — they operate on the shared
checkpoint and parked-event tables, so they are safe to run while a server is
processing.`,
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

func init() {
	workerParkedListCmd.Flags().String("subscriber", "", "limit to a single subscriber")
	workerParkedCmd.AddCommand(workerParkedListCmd, workerParkedReplayCmd)
	workerCmd.AddCommand(workerParkedCmd)
	rootCmd.AddCommand(workerCmd)
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
