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
	initGrantCommands()
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

var (
	grantEmail   string
	grantRole    string
	grantAccount string
	grantFrom    string
	grantUntil   string
)

var featureGrantCmd = &cobra.Command{
	Use:   "grant <key>",
	Short: "Give a feature to one person or to a role within an account",
	Long: `Gives a feature to one person or to a role.

A grant only ever turns a feature ON. It cannot rescue a feature an account or
the instance has switched off — those are above it and an explicit off is final.

The window is optional. A grant with none is live from the moment it is made;
one with --valid-from starts on its own, and one with --valid-until ends on its
own, with nothing to run and nobody to sign in again.

The account must be named on any instance with more than one, because a grant
lands in exactly one account and guessing which would be worse than asking.`,
	Args:         cobra.ExactArgs(1),
	RunE:         runFeatureGrant,
	SilenceUsage: true,
}

var featureRevokeCmd = &cobra.Command{
	Use:          "revoke <key>",
	Short:        "Take a feature back from one person or from a role",
	Args:         cobra.ExactArgs(1),
	RunE:         runFeatureRevoke,
	SilenceUsage: true,
}

var featureGrantsCmd = &cobra.Command{
	Use:          "grants <key>",
	Short:        "List who holds a feature, with each grant's window and who made it",
	Args:         cobra.ExactArgs(1),
	RunE:         runFeatureGrants,
	SilenceUsage: true,
}

func initGrantCommands() {
	for _, c := range []*cobra.Command{featureGrantCmd, featureRevokeCmd} {
		c.Flags().StringVar(&grantEmail, "email", "", "the person to grant to (give exactly one of --email or --role)")
		c.Flags().StringVar(&grantRole, "role", "", "the role to grant to (owner, admin or member)")
		c.Flags().StringVar(&grantAccount, "account", "", "the account id the grant lands in")
	}
	featureGrantCmd.Flags().StringVar(&grantFrom, "valid-from", "", "RFC3339 time the grant starts (default: immediately)")
	featureGrantCmd.Flags().StringVar(&grantUntil, "valid-until", "", "RFC3339 time the grant ends (default: indefinitely)")
	featureGrantsCmd.Flags().StringVar(&grantAccount, "account", "", "the account id to list grants in")
	featureCmd.AddCommand(featureGrantCmd, featureRevokeCmd, featureGrantsCmd)
}

func runFeatureGrant(cmd *cobra.Command, args []string) error {
	key := args[0]
	from, err := parseGrantFlag("--valid-from", grantFrom)
	if err != nil {
		return err
	}
	until, err := parseGrantFlag("--valid-until", grantUntil)
	if err != nil {
		return err
	}
	return withFeatureAdmin(cmd, func(ctx context.Context, admin *application.FeatureAdminService) error {
		account, err := resolveGrantAccount(ctx, admin)
		if err != nil {
			return err
		}
		if err := admin.Grant(ctx, application.GrantRequest{
			Key: key, Email: grantEmail, Role: grantRole,
			ValidFrom: from, ValidThrough: until,
			AccountID: account, Source: entities.FeatureChangeSourceCLI,
		}); err != nil {
			return err
		}
		cmd.Printf("Granted %q to %s in account %s.\n", key, grantSubjectWord(), account)
		// Echo what was actually stored. A grant dated to the wrong year is
		// accepted and simply never applies, and a success line alone would
		// let an operator walk away from a typo. Printing the window and its
		// status catches it at the keyboard.
		if views, err := admin.GrantsOn(ctx, key, account); err == nil {
			for _, v := range views {
				if v.Email == grantEmail || (grantRole != "" && v.Role == grantRole) {
					cmd.Printf("  window: %s   status: %s\n", windowWord(v), v.Status)
					break
				}
			}
		}
		printCacheAgeNotice(cmd)
		return nil
	})
}

func runFeatureRevoke(cmd *cobra.Command, args []string) error {
	key := args[0]
	return withFeatureAdmin(cmd, func(ctx context.Context, admin *application.FeatureAdminService) error {
		account, err := resolveGrantAccount(ctx, admin)
		if err != nil {
			return err
		}
		if err := admin.RevokeGrant(ctx, application.RevokeRequest{
			Key: key, Email: grantEmail, Role: grantRole,
			AccountID: account, Source: entities.FeatureChangeSourceCLI,
		}); err != nil {
			return err
		}
		cmd.Printf("Took %q back from %s in account %s.\n", key, grantSubjectWord(), account)
		printCacheAgeNotice(cmd)
		return nil
	})
}

func runFeatureGrants(cmd *cobra.Command, args []string) error {
	key := args[0]
	return withFeatureAdmin(cmd, func(ctx context.Context, admin *application.FeatureAdminService) error {
		account, err := resolveGrantAccount(ctx, admin)
		if err != nil {
			return err
		}
		views, err := admin.GrantsOn(ctx, key, account)
		if err != nil {
			return err
		}
		if len(views) == 0 {
			cmd.Printf("Nobody holds %q in account %s.\n", key, account)
			return nil
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(w, "SUBJECT\tWINDOW\tSTATUS\tGRANTED BY"); err != nil {
			return err
		}
		for _, v := range views {
			subject := v.Email
			if v.SubjectType == "role" {
				subject = "role:" + v.Role
			}
			if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				subject, windowWord(v), v.Status, orDash(v.GrantedBy)); err != nil {
				return err
			}
		}
		return w.Flush()
	})
}

// resolveGrantAccount names the account a grant acts in. The command line has
// no session to take one from, so it is either given or inferred — and only
// inferred when there is exactly one account, which on this system means a
// single-person instance.
func resolveGrantAccount(ctx context.Context, admin *application.FeatureAdminService) (string, error) {
	if grantAccount != "" {
		return grantAccount, nil
	}
	return admin.DefaultGrantAccount(ctx)
}

func parseGrantFlag(flag, value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("%s must be an RFC3339 time like 2026-08-21T09:00:00Z: %w", flag, err)
	}
	return &t, nil
}

func grantSubjectWord() string {
	if grantRole != "" {
		return "role " + grantRole
	}
	return grantEmail
}

func windowWord(v application.GrantView) string {
	switch {
	case v.ValidFrom != nil && v.ValidThrough != nil:
		return v.ValidFrom.Format(time.RFC3339) + " to " + v.ValidThrough.Format(time.RFC3339)
	case v.ValidFrom != nil:
		return "from " + v.ValidFrom.Format(time.RFC3339)
	case v.ValidThrough != nil:
		return "until " + v.ValidThrough.Format(time.RFC3339)
	default:
		return "—"
	}
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
