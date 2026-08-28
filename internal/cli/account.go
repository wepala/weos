package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wepala/weos/v3/api/handlers"
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

// passwordEnvVar is the channel an entrypoint should use. A flag would put the
// password in the process table, and shells remember what was typed at them;
// the environment is the one channel a deployment already has a private way to
// fill (a sensitive workspace variable, a secret mount).
//
//nolint:gosec // G101: this is the name of an environment variable, not a credential.
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
	Args:         cobra.NoArgs,
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
	// Refuse rather than pick. The realistic mix-up is an operator shelling
	// into a container whose entrypoint exported the variable, typing
	// --password for a second account, and silently getting the entrypoint's
	// password instead of the one they typed — discovered at first sign-in.
	offered := 0
	//nolint:forbidigo // the password channel is deliberately not in config.Config
	for _, supplied := range []bool{accountPasswordStdin, os.Getenv(passwordEnvVar) != "", accountPassword != ""} {
		if supplied {
			offered++
		}
	}
	if offered > 1 {
		return "", fmt.Errorf(
			"more than one password supplied: choose one of %s, --password-stdin or --password",
			passwordEnvVar)
	}

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
	if password := os.Getenv(passwordEnvVar); password != "" { //nolint:forbidigo // secret never enters config.Config
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

	if err := requireExplicitDSN("account create"); err != nil {
		return err
	}

	appCfg := GetConfig().Config

	var authService authapp.AuthenticationService
	var credentialRepo authrepos.CredentialRepository
	var passwordRepo authrepos.PasswordCredentialRepository

	// Built directly rather than through StartContainer (internal/cli/di.go),
	// which exposes only the resource services and installs a signal handler a
	// one-shot command does not want. The full module is deliberate: it is what
	// makes an account minted here indistinguishable from a registered one.
	app := fx.New(
		fx.NopLogger,
		application.Module(appCfg, presets.NewDefaultRegistry()),
		fx.Populate(&authService, &credentialRepo, &passwordRepo),
	)

	// Generously longer than fx's default. Starting the module runs the
	// migrations, the built-in resource types and the projection tables, not
	// just a connection — against a cold database that can outlast 15s, and a
	// timeout here crash-loops a container rather than reporting anything
	// useful.
	startCtx, startCancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		// Reported as the root cause rather than the whole chain: fx describes
		// a failure by naming every constructor it could not build, which is
		// several hundred characters of dependency graph wrapped around one
		// sentence about what actually broke. In a container log that buries
		// the answer.
		//
		// Not called a database failure, though the database is the usual
		// cause: any wiring failure arrives here, and branding them all as the
		// store sends an operator looking in the wrong place. The DSN is
		// scrubbed because drivers routinely echo the connection string back,
		// and a Postgres one carries a password.
		return fmt.Errorf("failed to start (check DATABASE_DSN): %s",
			redactDSN(rootCause(err).Error(), appCfg.DatabaseDSN))
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer stopCancel()
		_ = app.Stop(stopCtx)
	}()

	// Look the email up across every provider before registering, because
	// RegisterPassword's own guard only looks for a *password* credential. An
	// email that already signs in through Google sails past it and gets a
	// second, unlinked agent and personal account — silently, on some later
	// boot, once OAuth is added for an address this command already owns. This
	// runs on every start forever, so "later" is not hypothetical.
	normalized := strings.ToLower(email)
	existing, lookupErr := credentialRepo.FindByEmail(cmd.Context(), normalized)
	if lookupErr != nil {
		return fmt.Errorf("failed to check whether %s already has an account: %w", email, rootCause(lookupErr))
	}
	if len(existing) > 0 {
		if err := verifyUsable(cmd.Context(), passwordRepo, existing, email); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(),
			"Account already exists for %s (via %s); leaving it unchanged\n",
			email, strings.Join(providersOf(existing), ", "))
		return nil
	}

	displayName := handlers.DefaultDisplayName(email, accountDisplayName)

	_, _, _, err = authService.RegisterPassword(cmd.Context(), email, displayName, password)
	report, failed := describeRegisterOutcome(email, password, err)
	if failed {
		return errors.New(report)
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), report)
	return nil
}

// verifyUsable makes sure "already exists" means "and can be signed in to".
//
// Registering writes the credential row and the password row as two separate,
// untransacted saves. A crash or a full disk between them leaves an email that
// looks registered and has no password: the first run stops the boot correctly,
// but every run after it finds the credential, reports "already exists", and
// exits 0 — so the instance comes up reporting itself provisioned forever while
// nobody can sign in, and no later run can repair it, because they all take
// this same branch.
//
// Checking only for the row's presence keeps the deliberate no-reset behavior
// intact: a rotated password still changes nothing. What it refuses to do is
// call a half-written account "already there".
func verifyUsable(
	ctx context.Context,
	passwordRepo authrepos.PasswordCredentialRepository,
	existing []*authentities.Credential,
	email string,
) error {
	for _, credential := range existing {
		if credential.Provider() != authentities.ProviderPassword {
			continue
		}
		stored, err := passwordRepo.FindByCredentialID(ctx, credential.GetID())
		if err != nil {
			return fmt.Errorf("failed to check the stored password for %s: %w", email, rootCause(err))
		}
		if stored == nil {
			return fmt.Errorf(
				"%s has a password credential with no stored password — the account was "+
					"half-written by an earlier interrupted run and nobody can sign in to it. "+
					"Remove the credential for %s and run this again", email, email)
		}
	}
	return nil
}

// providersOf names the ways an email can already sign in, so the message says
// "via google" rather than leaving an operator to wonder why an account they
// never created with this command is reported as already there.
func providersOf(credentials []*authentities.Credential) []string {
	seen := map[string]bool{}
	providers := []string{}
	for _, credential := range credentials {
		provider := credential.Provider()
		if provider == "" || seen[provider] {
			continue
		}
		seen[provider] = true
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	return providers
}

// redactDSN keeps a connection string out of a message that a driver may have
// built from one. Matching on the whole DSN is deliberately narrow: it removes
// what we know we configured without trying to guess at every shape a driver
// might print.
func redactDSN(message, dsn string) string {
	if dsn == "" {
		return message
	}
	return strings.ReplaceAll(message, dsn, "[DATABASE_DSN]")
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
		s = strings.ReplaceAll(s, password, "[redacted]")
		// Go code formats values with %q as readily as %v, and the escaped
		// rendering of a password containing a quote or a backslash is a
		// different string from the password — so a plain substring replace
		// walks straight past it, leaving the secret perfectly readable.
		if escaped := strings.Trim(strconv.Quote(password), `"`); escaped != password {
			s = strings.ReplaceAll(s, escaped, "[redacted]")
		}
		return s
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
