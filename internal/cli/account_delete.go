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
	"strings"
	"time"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/internal/config"

	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

var (
	accountDeleteConfirm      bool
	accountDeleteSkipDrain    bool
	accountDeleteDrainTimeout time.Duration
)

var accountDeleteCmd = &cobra.Command{
	Use:   "delete <account-id>",
	Short: "Erase an account and everything of its from this instance",
	Long: `Erases the account named by its id: its events, projection rows, graph
triples, files, sessions, its members' memberships, the connector codes and
tokens that granted access to it, and its feature grants. Members who belonged
to no other account go with it. This is a hard delete and cannot be undone.

It runs the same service the app's delete button runs, so an operator can
finish a deletion the person can no longer reach: a deletion that failed
part-way leaves the account locked, and running this command finishes it.

If this binary registers steps of its own inside a deletion — unlinking the
account from something outside this instance, such as a payment provider or an
identity provider — this command runs them too, and names them before it
starts. A deletion finished here is the same deletion the app runs.

The command refuses to run without --confirm, and it opens the store directly,
so it needs no running server. A server that is running keeps serving; the
account is locked before anything is removed.

Before it purges, the command waits for the background projections to reach
the head of the event log, so none of them projects the account's events after
the purge. A checkpoint row that no process advances any more — a projection
that was turned off, renamed or retired — is not waited for once it has gone
unwritten for ACCOUNT_ERASURE_DRAIN_STALE_AFTER_SECONDS. If a deletion still
times out on a row that will never move, --skip-drain purges without the wait;
use it only when you know no projection is running.`,
	Args:         cobra.ExactArgs(1),
	RunE:         runAccountDelete,
	SilenceUsage: true,
}

func init() {
	accountDeleteCmd.Flags().BoolVar(&accountDeleteConfirm, "confirm", false,
		"confirm the erasure; without it the command changes nothing")
	accountDeleteCmd.Flags().DurationVar(&accountDeleteDrainTimeout, "drain-timeout", 0,
		"how long to wait for background projections to catch up before purging (default from config)")
	accountDeleteCmd.Flags().BoolVar(&accountDeleteSkipDrain, "skip-drain", false,
		"purge without waiting for background projections; for a checkpoint row that will never move")
	accountCmd.AddCommand(accountDeleteCmd)
}

func runAccountDelete(cmd *cobra.Command, args []string) error {
	accountID := args[0]
	if accountID == "" {
		return errors.New("no account id supplied")
	}
	// Refused before the store is opened: a missing --confirm must cost
	// nothing and change nothing.
	if !accountDeleteConfirm {
		return fmt.Errorf("refusing to erase account %s without --confirm: this is a hard delete "+
			"of everything the account owns and cannot be undone", accountID)
	}
	if err := requireExplicitDSN("account delete"); err != nil {
		return err
	}

	appCfg := GetConfig().Config
	if accountDeleteDrainTimeout > 0 {
		appCfg.Worker.ErasureDrainTimeout = accountDeleteDrainTimeout
	}

	erasure, app, err := buildErasure(appCfg)
	if err != nil {
		return err
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

	// Said out loud before anything is removed: an instance whose binary
	// registered its steps into a graph this command does not carry reads as
	// "none" here, rather than erasing silently without them.
	if names := erasure.ParticipantNames(); len(names) > 0 {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Running %d registered step(s) first: %s\n",
			len(names), strings.Join(names, ", "))
	} else {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "No registered steps to run first; erasing what is on this instance only\n")
	}

	result, err := erasure.Erase(cmd.Context(), application.EraseAccountCommand{
		AccountID:   accountID,
		RequestedBy: "operator",
		SkipDrain:   accountDeleteSkipDrain,
	})
	if err != nil {
		if errors.Is(err, application.ErrAccountNotFound) {
			return fmt.Errorf("no account with id %s exists on this instance", accountID)
		}
		if errors.Is(err, application.ErrErasureInProgress) {
			return fmt.Errorf("a deletion of account %s is already running in this process; wait for it to finish", accountID)
		}
		if errors.Is(err, application.ErrErasureDrainTimeout) {
			return fmt.Errorf("account %s is locked but not erased: %v — run this command again "+
				"once the background projections have caught up, pass --drain-timeout to wait longer, "+
				"or pass --skip-drain if the named checkpoint belongs to a projection nothing runs any more", accountID, err)
		}
		return fmt.Errorf("account %s is locked but not erased: %v — run this command again to finish", accountID, err)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"Erased account %s: %d member(s) lost it, %d resource(s) and %d event(s) removed\n",
		result.AccountID, result.MembersLost, result.Resources, result.Events)
	return nil
}

// buildErasure builds the graph this command erases from: the application
// module, plus the options a downstream binary registered for every graph
// that can erase an account. Those options are what carry the binary's own
// erasure participants here — without them the command would erase with an
// empty participant group, which is indistinguishable from an instance that
// legitimately registered none.
func buildErasure(appCfg config.Config) (*application.AccountErasureService, *fx.App, error) {
	var erasure *application.AccountErasureService
	opts := []fx.Option{
		fx.NopLogger,
		application.Module(appCfg, presets.NewDefaultRegistry()),
	}
	opts = append(opts, customErasureFxOptions...)
	app := fx.New(append(opts, fx.Populate(&erasure))...)
	if err := app.Err(); err != nil {
		return nil, nil, fmt.Errorf("could not build the erasure: %w", err)
	}
	return erasure, app, nil
}
