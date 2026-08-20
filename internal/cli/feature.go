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
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/fx"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
)

var featureCmd = &cobra.Command{
	Use:   "feature",
	Short: "Inspect and change which features are on for this instance",
	Long: `Features are capabilities an instance can turn on or off without a redeploy.

A feature is declared in code or by a preset; this command changes whether it is
on. An instance-level setting is final for everyone: no account override and no
individual grant reaches past an explicit off.

Reset is not the same as disable. Disabling turns a feature off for everyone;
resetting removes the instance setting so the feature returns to the default it
was declared with, which an account or a grant can still turn on.

This command sees only the features its OWN environment declares — from the
binary, from installed presets, and from FEATURES. Run it with the same
environment as the server, or the two will disagree about which features exist:
a key the server declares and your shell does not is refused here, and a key
your shell declares and the server does not is written and then ignored.

Account overrides and individual grants are not visible from the command line,
which has no signed-in caller to resolve them for. Use the API or the admin UI
to see those.`,
	SilenceUsage: true,
}

var featureListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List every declared feature, its state, and where that state came from",
	Args:         cobra.NoArgs,
	RunE:         runFeatureList,
	SilenceUsage: true,
}

var featureEnableCmd = &cobra.Command{
	Use:          "enable <key>",
	Short:        "Turn a feature on for the whole instance",
	Args:         cobra.ExactArgs(1),
	RunE:         runFeatureSet(true),
	SilenceUsage: true,
}

var featureDisableCmd = &cobra.Command{
	Use:          "disable <key>",
	Short:        "Turn a feature off for the whole instance, for everyone",
	Args:         cobra.ExactArgs(1),
	RunE:         runFeatureSet(false),
	SilenceUsage: true,
}

var featureResetCmd = &cobra.Command{
	Use:   "reset <key>",
	Short: "Remove the instance setting so the feature returns to its declared default",
	Long: `Removes the instance-level setting for a feature.

This is not the same as disabling it. A disabled feature is off for everyone and
nothing below can turn it back on. A reset feature returns to the default it was
declared with, which an account admin or an individual grant can still turn on.`,
	Args:         cobra.ExactArgs(1),
	RunE:         runFeatureReset,
	SilenceUsage: true,
}

func init() {
	featureCmd.AddCommand(featureListCmd, featureEnableCmd, featureDisableCmd, featureResetCmd)
	rootCmd.AddCommand(featureCmd)
}

func runFeatureList(cmd *cobra.Command, _ []string) error {
	return withFeatureAdmin(cmd, func(ctx context.Context, admin *application.FeatureAdminService) error {
		statuses, err := admin.Listing(ctx)
		if err != nil {
			return fmt.Errorf("could not read the features: %w", err)
		}
		if len(statuses) == 0 {
			cmd.Println("No features are declared on this instance.")
			return nil
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(w, "KEY\tNAME\tSTATE\tSOURCE"); err != nil {
			return err
		}
		for _, s := range statuses {
			if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				s.Key, s.DisplayName, stateWord(s.Enabled), sourceWord(s.Source)); err != nil {
				return err
			}
		}
		return w.Flush()
	})
}

func runFeatureSet(enabled bool) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		key := args[0]
		return withFeatureAdmin(cmd, func(ctx context.Context, admin *application.FeatureAdminService) error {
			if err := admin.SetInstance(ctx, key, enabled, entities.FeatureChangeSourceCLI); err != nil {
				return err
			}
			cmd.Printf("Feature %q is now %s for this instance.\n", key, stateWord(enabled))
			printCacheAgeNotice(cmd)
			return nil
		})
	}
}

func runFeatureReset(cmd *cobra.Command, args []string) error {
	key := args[0]
	return withFeatureAdmin(cmd, func(ctx context.Context, admin *application.FeatureAdminService) error {
		if err := admin.ResetInstance(ctx, key, entities.FeatureChangeSourceCLI); err != nil {
			return err
		}
		cmd.Printf("Feature %q returns to the default it was declared with.\n", key)
		printCacheAgeNotice(cmd)
		return nil
	})
}

// printCacheAgeNotice tells the operator the change will not be instant.
//
// A running server resolves a caller's features once and reads from memory
// afterwards, and this command is a different process — it cannot reach into
// that server's memory. So the flip lands when the running instance's cached
// sets reach their maximum age. Saying so here is the difference between a
// documented delay and an operator concluding the command did not work.
func printCacheAgeNotice(cmd *cobra.Command) {
	age := GetConfig().Features.CacheMaxAge
	if age <= 0 {
		age = 15 * time.Minute
	}
	// Named as this command's own setting, not the server's. They are separate
	// processes reading separate environments, and stating a number this
	// process cannot know would be worse than saying nothing.
	cmd.Printf("A running instance picks this up when its cached feature sets expire "+
		"(FEATURE_CACHE_MAX_AGE_SECONDS; %s here, but the server's own setting is what applies).\n", age)
}

func stateWord(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

// sourceWord renders the deciding layer for an operator. "default" is the one
// that needs expanding: it means nobody has set anything, which is a different
// situation from someone having set it to the same value, and the two call for
// different actions.
func sourceWord(source string) string {
	switch source {
	case "instance":
		return "instance override"
	case "account":
		return "account override"
	case "grant":
		return "grant"
	default:
		return "declared default"
	}
}

// withFeatureAdmin boots the application, hands the callback the admin service,
// and shuts down again.
//
// The context is marked as local transport. Whoever can run this binary against
// the database can already change anything in it directly, so the permission
// check that guards the HTTP surfaces has nothing to add here — the operating
// system already made the decision. The marker is applied here and not in
// StartContainer on purpose: it also switches knowledge-graph routing, which
// no other command should inherit from this one.
func withFeatureAdmin(cmd *cobra.Command, fn func(context.Context, *application.FeatureAdminService) error) error {
	if err := requireExplicitDSN("feature"); err != nil {
		return err
	}

	// Quiet unless asked. Booting the module logs every built-in resource
	// type, projection and behavior registration at info — useful when a
	// server starts, noise that buries the answer when a one-shot command
	// prints a table. --verbose still turns it all back on.
	appCfg := GetConfig().Config
	if !verbose {
		appCfg.LogLevel = "error"
	}

	var admin *application.FeatureAdminService
	app := fx.New(
		fx.NopLogger,
		application.Module(appCfg, presets.NewDefaultRegistry()),
		fx.Populate(&admin),
	)

	// Generous, for the same reason account create is: starting the module runs
	// migrations and projection setup against a possibly cold database, and a
	// timeout here reports nothing useful.
	startCtx, startCancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("could not start: %w", rootCause(err))
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer stopCancel()
		_ = app.Stop(stopCtx)
	}()

	return fn(application.WithLocalTransport(cmd.Context()), admin)
}
