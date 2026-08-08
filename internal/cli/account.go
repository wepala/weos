package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/wepala/weos/v3/api/handlers"
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

// passwordEnvVar is the channel an entrypoint should use. A flag would put the
// password in the process table, and shells remember what was typed at them;
// the environment is the one channel a deployment already has a private way to
// fill (a sensitive workspace variable, a secret mount).
const passwordEnvVar = "WEOS_ACCOUNT_PASSWORD"

var (
	accountEmail         string
	accountPassword      string
	accountDisplayName   string
	accountPasswordStdin bool
)

var accountCmd = &cobra.Command{
	Use:   "account",
	Short: "Manage accounts",
}

var accountCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a password account without going through an HTTP route",
	Long: `Creates an account that can sign in with an email and a password, using the
same path the register endpoint uses, so an account made here and one that
registered over HTTP are the same thing afterwards.

This exists so an instance can be provisioned without mounting a registration
route that strangers can also reach. It opens the store directly and needs no
running server.

Supply the password through ` + passwordEnvVar + ` or --password-stdin. Both keep
it out of the process table and out of shell history; --password does not, and
is meant for interactive use only.

Running it again for an email that already exists reports that and succeeds, so
it is safe on every restart and redeploy. It will not change an existing
account's password.`,
	RunE:         runAccountCreate,
	SilenceUsage: true,
}

func init() {
	accountCreateCmd.Flags().StringVar(&accountEmail, "email", "", "email address to create the account for (required)")
	accountCreateCmd.Flags().StringVar(&accountPassword, "password", "",
		"password (visible in the process table — prefer "+passwordEnvVar+" or --password-stdin)")
	accountCreateCmd.Flags().StringVar(&accountDisplayName, "display-name", "",
		"name to present the account under (defaults to the email's local part)")
	accountCreateCmd.Flags().BoolVar(&accountPasswordStdin, "password-stdin", false, "read the password from standard input")
	_ = accountCreateCmd.MarkFlagRequired("email")

	accountCmd.AddCommand(accountCreateCmd)
	rootCmd.AddCommand(accountCmd)
}

// resolveAccountPassword takes the password from the first channel that offers
// one, preferring the channels that don't leak it. It is a separate step from
// creating the account so that "no password anywhere" fails before the store is
// opened, rather than part-way through provisioning.
func resolveAccountPassword(stdin io.Reader) (string, error) {
	if accountPasswordStdin {
		raw, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("failed to read the password from standard input: %w", err)
		}
		// A password is whatever was typed, minus the newline the shell adds.
		password := strings.TrimRight(string(raw), "\r\n")
		if password == "" {
			return "", errors.New("no password on standard input")
		}
		return password, nil
	}
	if password := os.Getenv(passwordEnvVar); password != "" {
		return password, nil
	}
	if accountPassword != "" {
		return accountPassword, nil
	}
	return "", fmt.Errorf(
		"no password supplied: set %s, pass --password-stdin, or use --password", passwordEnvVar)
}

func runAccountCreate(cmd *cobra.Command, _ []string) error {
	email := strings.TrimSpace(accountEmail)
	if email == "" {
		return errors.New("no email supplied: pass --email")
	}

	// Resolved first, so a missing password costs nothing and creates nothing.
	// Note that the password is never put into an error, a log line or any
	// output from here on — an operator reading a failure report, or a verbose
	// log, must not find it there.
	password, err := resolveAccountPassword(cmd.InOrStdin())
	if err != nil {
		return err
	}

	appCfg := GetConfig().Config

	var authService authapp.AuthenticationService

	app := fx.New(
		fx.NopLogger,
		application.Module(appCfg, presets.NewDefaultRegistry()),
		fx.Populate(&authService),
	)

	startCtx, startCancel := context.WithTimeout(cmd.Context(), fx.DefaultTimeout)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		// Named as a database problem on purpose: this runs from an entrypoint
		// under `set -e`, and the operator needs to know the boot stopped
		// because the store would not open. The DSN is left out — it can carry
		// credentials of its own.
		//
		// Reported as the root cause rather than the whole chain: fx describes
		// a failure by naming every constructor it could not build, which is
		// several hundred characters of dependency graph wrapped around one
		// sentence about the file. In a container log that buries the answer.
		return fmt.Errorf("failed to open the database: %w", rootCause(err))
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer stopCancel()
		_ = app.Stop(stopCtx)
	}()

	displayName := handlers.DefaultDisplayName(email, accountDisplayName)

	_, _, _, err = authService.RegisterPassword(cmd.Context(), email, displayName, password)
	report, failed := describeRegisterOutcome(email, password, err)
	if failed {
		return errors.New(report)
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), report)
	return nil
}

// rootCause unwraps to the innermost error, which is the sentence that says
// what actually went wrong. Everything above it is the wiring that noticed.
func rootCause(err error) error {
	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			return err
		}
		err = unwrapped
	}
}

// describeRegisterOutcome turns the result of registering into the one thing
// the caller acts on — a message, and whether the boot should stop.
//
// It is separate from the command so the mapping can be checked directly. The
// password-support branch in particular is not reachable through the real
// binary today (application/module.go wires the password-credential repository
// unconditionally, so the service always has one), and a test that faked an
// unreachable state would only prove the fake works.
//
// It takes the password only to keep it out: the last branch reports whatever a
// lower layer said went wrong, and that text is not ours to trust. Redacting
// here — at the one place that turns an outcome into words — makes "the
// password is never echoed" something the code enforces rather than something
// every layer beneath has to remember.
func describeRegisterOutcome(email, password string, err error) (report string, failed bool) {
	redact := func(s string) string {
		if password == "" {
			return s
		}
		return strings.ReplaceAll(s, password, "[redacted]")
	}

	switch {
	case err == nil:
		return fmt.Sprintf("Created account for %s", email), false

	case errors.Is(err, authapp.ErrEmailAlreadyTaken):
		// The normal state of every restart after the first. Succeeding here is
		// what makes the command safe to run on every boot; it deliberately
		// does not touch the existing account's password, so a changed variable
		// never silently locks anyone out or lets anyone back in.
		return fmt.Sprintf("Account already exists for %s; leaving it unchanged", email), false

	case errors.Is(err, authapp.ErrPasswordSupportNotConfigured):
		return "failed to create the account: password support is not configured on this instance", true

	default:
		return redact(fmt.Sprintf("failed to create the account: %v", err)), true
	}
}
