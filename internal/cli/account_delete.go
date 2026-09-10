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
	"time"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"

	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

var (
	accountDeleteConfirm      bool
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

The command refuses to run without --confirm, and it opens the store directly,
so it needs no running server. A server that is running keeps serving; the
account is locked before anything is removed.`,
	Args:         cobra.ExactArgs(1),
	RunE:         runAccountDelete,
	SilenceUsage: true,
}

func init() {
	accountDeleteCmd.Flags().BoolVar(&accountDeleteConfirm, "confirm", false,
		"confirm the erasure; without it the command changes nothing")
	accountDeleteCmd.Flags().DurationVar(&accountDeleteDrainTimeout, "drain-timeout", 0,
		"how long to wait for background projections to catch up before purging (default from config)")
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

	var erasure *application.AccountErasureService
	app := fx.New(
		fx.NopLogger,
		application.Module(appCfg, presets.NewDefaultRegistry()),
		fx.Populate(&erasure),
	)
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

	result, err := erasure.Erase(cmd.Context(), application.EraseAccountCommand{
		AccountID:   accountID,
		RequestedBy: "operator",
	})
	if err != nil {
		if errors.Is(err, application.ErrAccountNotFound) {
			return fmt.Errorf("no account with id %s exists on this instance", accountID)
		}
		if errors.Is(err, application.ErrErasureDrainTimeout) {
			return fmt.Errorf("account %s is locked but not erased: %v — run this command again "+
				"once the background projections have caught up, or pass --drain-timeout", accountID, err)
		}
		return fmt.Errorf("account %s is locked but not erased: %v — run this command again to finish", accountID, err)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"Erased account %s: %d member(s) lost it, %d resource(s) and %d event(s) removed\n",
		result.AccountID, result.MembersLost, result.Resources, result.Events)
	return nil
}
