package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/wepala/weos/v3/api/handlers"
	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"github.com/cucumber/godog"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
)

// TestAccountCreateCLI runs the acceptance scenarios for story #137 (epic
// #135): an operator mints an account from the command line rather than through
// an HTTP route that strangers can also reach.
//
// These scenarios run the real binary as a real process. That is not
// incidental: what they assert is process behavior — the exit code an
// entrypoint under `set -e` reads, what reaches stdout and stderr, a password
// arriving on standard input, and a password that must not appear in the
// argument list. Calling the command in-process would leave every one of those
// claims about something other than the thing that ships.
func TestAccountCreateCLI(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "account-create-cli",
		ScenarioInitializer: initAccountCreateScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/account_create_cli.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("account create CLI acceptance scenarios failed")
	}
}

var (
	buildOnce   sync.Once
	builtBinary string
	buildDir    string
	buildErr    error
)

// TestMain removes the directory the binary was built into. Without it every
// run leaves an ~85 MB copy behind, which is invisible on an ephemeral CI
// runner and accumulates on a developer's machine.
func TestMain(m *testing.M) {
	code := m.Run()
	if buildDir != "" {
		_ = os.RemoveAll(buildDir)
	}
	os.Exit(code)
}

// weosBinary builds the command under test once for the whole suite.
func weosBinary() (string, error) {
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "weos-account-cli-bin-")
		if err != nil {
			buildErr = err
			return
		}
		buildDir = dir
		path := filepath.Join(dir, "weos")
		cmd := exec.Command("go", "build", "-o", path, "./cmd/weos")
		cmd.Dir = "../.."
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("failed to build the weos binary: %w\n%s", err, out)
			return
		}
		builtBinary = path
	})
	return builtBinary, buildErr
}

// commandRun is what an entrypoint script can observe about one invocation.
type commandRun struct {
	exitCode int
	stdout   string
	stderr   string
	args     []string
}

// said is everything the command printed, which is what the "appears nowhere"
// assertions search.
func (r commandRun) said() string { return r.stdout + r.stderr }

type accountCLIWorld struct {
	dsn     string
	tmpDir  string
	verbose bool

	lastRun      commandRun
	lastPassword string
	workDir      string

	// A booted instance, for the scenarios that check the account works.
	app    *fx.App
	server *httptest.Server

	authService authapp.AuthenticationService
	credRepo    authrepos.CredentialRepository
	agentRepo   authrepos.AgentRepository
	accountRepo authrepos.AccountRepository
	sessionMgr  session.SessionManager
	logger      entities.Logger

	resourceService     application.ResourceService
	resourceTypeService application.ResourceTypeService

	signIns map[string]int
}

func initAccountCreateScenario(sc *godog.ScenarioContext) {
	w := &accountCLIWorld{}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a WeOS store with no accounts, and no server running against it$`, w.anEmptyStore)
	sc.Step(`^a WeOS store where the operator has already created the account "([^"]*)" with password "([^"]*)"$`,
		w.aStoreWithAccount)
	sc.Step(`^a WeOS store whose database cannot be opened$`, w.anUnopenableStore)
	sc.Step(`^a WeOS store where "([^"]*)" already signs in through Google$`, w.anOAuthAccount)
	sc.Step(`^a WeOS instance with no database configured$`, w.noDatabaseConfigured)
	sc.Step(`^a WeOS store that already holds the account "([^"]*)" and a task named "([^"]*)"$`, w.aStoreWithAccountAndTask)
	sc.Step(`^no server is running against it$`, w.noServerRunning)
	sc.Step(`^the operator has asked for verbose logging$`, w.verboseLogging)
	sc.Step(`^the operator has created an account for "([^"]*)" with password "([^"]*)"$`, w.operatorCreatedAccount)

	sc.Step(`^the operator creates an account for "([^"]*)" with the password supplied through the environment$`,
		func(email string) error { return w.createViaEnv(email, aPassword, "") })
	sc.Step(`^the operator creates an account for "([^"]*)" with password "([^"]*)" supplied through the environment$`,
		func(email, password string) error { return w.createViaEnv(email, password, "") })
	sc.Step(`^the operator creates an account for "([^"]*)" with password "([^"]*)"$`,
		func(email, password string) error { return w.createViaEnv(email, password, "") })
	sc.Step(`^the operator creates an account for "([^"]*)" with password "([^"]*)" again$`,
		func(email, password string) error { return w.createViaEnv(email, password, "") })
	sc.Step(`^the operator creates an account for "([^"]*)" with no display name given$`,
		func(email string) error { return w.createViaEnv(email, aPassword, "") })
	sc.Step(`^the operator creates an account for "([^"]*)" with the display name "([^"]*)"$`,
		func(email, displayName string) error { return w.createViaEnv(email, aPassword, displayName) })
	sc.Step(`^the operator creates an account for "([^"]*)" with no password supplied through any channel$`, w.createWithNoPassword)
	sc.Step(`^the operator creates an account for "([^"]*)" with the password supplied on standard input$`, w.createViaStdin)
	sc.Step(`^the operator creates an account for "([^"]*)" with the password given on the command line$`, w.createViaFlag)

	sc.Step(`^the instance is started with password sign-in enabled$`, func() error { return w.startInstance(false) })
	sc.Step(`^the instance is started with password sign-in and account registration enabled$`,
		func() error { return w.startInstance(true) })
	sc.Step(`^"([^"]*)" registers with password "([^"]*)"$`, w.registersOverHTTP)
	sc.Step(`^"([^"]*)" signs in with password "([^"]*)"$`, w.cliSignIn)
	sc.Step(`^both "([^"]*)" and "([^"]*)" sign in with password "([^"]*)"$`, w.bothSignIn)

	sc.Step(`^the command exits successfully$`, w.exitedSuccessfully)
	sc.Step(`^the command exits with a failure$`, w.exitedWithFailure)
	sc.Step(`^the command reports that the account already exists$`, w.reportedAlreadyExists)
	sc.Step(`^the failure names the database as the reason$`, func() error { return w.failureNames("database") })
	sc.Step(`^the failure says no database was specified and names how to supply one$`, w.failureNamesNoDatabase)
	sc.Step(`^the command reports that the account already exists, naming Google as how it signs in$`, w.reportedExistsViaGoogle)
	sc.Step(`^no account is created in any store$`, w.noStoreWasWritten)
	sc.Step(`^the command leaves no database behind in the directory it ran from$`, w.noStrayDatabase)
	sc.Step(`^the store holds exactly one account for "([^"]*)"$`, w.storeHoldsExactlyOne)
	sc.Step(`^no account exists for "([^"]*)"$`, w.noAccountExists)
	sc.Step(`^the account for "([^"]*)" is presented as "([^"]*)"$`, w.accountPresentedAs)
	sc.Step(`^the password appears nowhere in what the command printed or logged$`, w.passwordNowhereInOutput)
	sc.Step(`^the password was never given as a command-line argument$`, w.passwordNotAnArgument)
	sc.Step(`^"([^"]*)" can sign in with password "([^"]*)"$`, w.canSignIn)
	sc.Step(`^"([^"]*)" cannot sign in with password "([^"]*)"$`, w.cannotSignInCLI)
	sc.Step(`^"([^"]*)" can sign in with the password that was supplied$`,
		func(email string) error { return w.canSignIn(email, w.lastPassword) })
	sc.Step(`^"([^"]*)" can sign in with password "([^"]*)" once the instance is started$`, w.canSignIn)
	sc.Step(`^the sign-in succeeds$`, w.signInSucceeded)
	sc.Step(`^both sign-ins succeed$`, w.bothSignInsSucceeded)
	sc.Step(`^"([^"]*)" holds an authenticated session$`, w.cliHoldsSession)
	sc.Step(`^each of them owns a personal account$`, w.eachOwnsPersonalAccount)
	sc.Step(`^the store still holds the account "([^"]*)" and the task named "([^"]*)"$`, w.storeStillHolds)
}

// --- Store setup ---

func (w *accountCLIWorld) anEmptyStore() error {
	dir, err := os.MkdirTemp("", "weos-account-cli-")
	if err != nil {
		return err
	}
	w.tmpDir = dir
	w.dsn = filepath.Join(dir, "store.db")
	w.signIns = map[string]int{}
	return nil
}

// anUnopenableStore points the command at a path inside a directory that does
// not exist, which is the shape of a misconfigured DSN in a deployment.
func (w *accountCLIWorld) anUnopenableStore() error {
	if err := w.anEmptyStore(); err != nil {
		return err
	}
	w.dsn = filepath.Join(w.tmpDir, "no-such-directory", "store.db")
	return nil
}

func (w *accountCLIWorld) aStoreWithAccount(email, password string) error {
	if err := w.anEmptyStore(); err != nil {
		return err
	}
	return w.operatorCreatedAccount(email, password)
}

// operatorCreatedAccount seeds through the command itself, so the "second run"
// scenarios start from a state the command actually produced.
func (w *accountCLIWorld) operatorCreatedAccount(email, password string) error {
	if err := w.createViaEnv(email, password, ""); err != nil {
		return err
	}
	return w.exitedSuccessfully()
}

func (w *accountCLIWorld) aStoreWithAccountAndTask(email, taskName string) error {
	if err := w.anEmptyStore(); err != nil {
		return err
	}
	if err := w.operatorCreatedAccount(email, aPassword); err != nil {
		return err
	}
	if err := w.startInstance(false); err != nil {
		return err
	}
	if err := w.seedTask(taskName); err != nil {
		return err
	}
	// The scenario says no server is running when the command runs, so the one
	// used for seeding is stopped again.
	w.stopInstance()
	return nil
}

func (w *accountCLIWorld) noServerRunning() error {
	w.stopInstance()
	return nil
}

func (w *accountCLIWorld) verboseLogging() error {
	w.verbose = true
	return nil
}

// anOAuthAccount seeds an account the way the OAuth callback would, so the
// scenario starts from an email that signs in without any password at all.
func (w *accountCLIWorld) anOAuthAccount(email string) error {
	if err := w.anEmptyStore(); err != nil {
		return err
	}
	if err := w.startInstance(false); err != nil {
		return err
	}
	if _, _, _, err := w.authService.FindOrCreateAgent(context.Background(), authapp.UserInfo{
		ProviderUserID: "google-" + email,
		Email:          email,
		DisplayName:    "Ops",
		Provider:       "google",
	}); err != nil {
		return fmt.Errorf("could not seed the Google account for %q: %w", email, err)
	}
	w.stopInstance()
	return nil
}

// noDatabaseConfigured runs the command from an empty directory with no DSN in
// the environment, which is how a deployment that lost its setting would look.
func (w *accountCLIWorld) noDatabaseConfigured() error {
	dir, err := os.MkdirTemp("", "weos-account-cli-nodsn-")
	if err != nil {
		return err
	}
	w.tmpDir = dir
	w.dsn = ""
	w.workDir = dir
	w.signIns = map[string]int{}
	return nil
}

// --- Running the command ---

func (w *accountCLIWorld) run(args []string, env []string, stdin string) error {
	binary, err := weosBinary()
	if err != nil {
		return err
	}
	if w.verbose {
		args = append(args, "--verbose")
	}
	cmd := exec.Command(binary, args...)
	if w.workDir != "" {
		cmd.Dir = w.workDir
	}
	dsnEnv := "DATABASE_DSN=" + w.dsn
	if w.dsn == "" {
		// Unset rather than empty: an absent setting is the case under test.
		dsnEnv = "DATABASE_DSN="
	}
	cmd.Env = append(os.Environ(),
		append([]string{
			dsnEnv,
			// Left unset by default; a scenario that asks for verbose logging
			// turns it up so the "never repeats the password" claim is made
			// against the noisiest setting, not the quietest.
			"LOG_LEVEL=" + map[bool]string{true: "debug", false: "error"}[w.verbose],
		}, env...)...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			return fmt.Errorf("failed to run the command: %w", runErr)
		}
		exitCode = exitErr.ExitCode()
	}
	w.lastRun = commandRun{
		exitCode: exitCode,
		stdout:   stdout.String(),
		stderr:   stderr.String(),
		args:     args,
	}
	return nil
}

func (w *accountCLIWorld) createArgs(email, displayName string) []string {
	args := []string{"account", "create", "--email", email}
	if displayName != "" {
		args = append(args, "--display-name", displayName)
	}
	return args
}

func (w *accountCLIWorld) createViaEnv(email, password, displayName string) error {
	w.lastPassword = password
	return w.run(w.createArgs(email, displayName), []string{"WEOS_ACCOUNT_PASSWORD=" + password}, "")
}

func (w *accountCLIWorld) createViaStdin(email string) error {
	w.lastPassword = aPassword
	args := append(w.createArgs(email, ""), "--password-stdin")
	return w.run(args, nil, aPassword+"\n")
}

// createViaFlag is the interactive path. It is supported because an operator at
// a terminal wants it, and it is deliberately not what the entrypoint uses:
// anything on the command line is visible in the process table.
func (w *accountCLIWorld) createViaFlag(email string) error {
	w.lastPassword = aPassword
	return w.run(append(w.createArgs(email, ""), "--password", aPassword), nil, "")
}

func (w *accountCLIWorld) createWithNoPassword(email string) error {
	w.lastPassword = ""
	return w.run(w.createArgs(email, ""), []string{"WEOS_ACCOUNT_PASSWORD="}, "")
}

// --- Command assertions ---

func (w *accountCLIWorld) exitedSuccessfully() error {
	if w.lastRun.exitCode != 0 {
		return fmt.Errorf("expected the command to exit 0, got %d: %s", w.lastRun.exitCode, w.lastRun.said())
	}
	return nil
}

func (w *accountCLIWorld) exitedWithFailure() error {
	if w.lastRun.exitCode == 0 {
		return fmt.Errorf("expected the command to exit non-zero, it exited 0: %s", w.lastRun.said())
	}
	return nil
}

func (w *accountCLIWorld) reportedAlreadyExists() error {
	if !strings.Contains(strings.ToLower(w.lastRun.said()), "already exists") {
		return fmt.Errorf("expected the command to report the account already exists, it said: %s", w.lastRun.said())
	}
	return nil
}

func (w *accountCLIWorld) failureNames(subject string) error {
	if !strings.Contains(strings.ToLower(w.lastRun.said()), subject) {
		return fmt.Errorf("expected the failure to name %q, it said: %s", subject, w.lastRun.said())
	}
	return nil
}

func (w *accountCLIWorld) passwordNowhereInOutput() error {
	if w.lastPassword == "" {
		return fmt.Errorf("no password was supplied, so this assertion would pass vacuously")
	}
	if strings.Contains(w.lastRun.said(), w.lastPassword) {
		return fmt.Errorf("the password appeared in what the command said: %s", w.lastRun.said())
	}
	return nil
}

func (w *accountCLIWorld) passwordNotAnArgument() error {
	if w.lastPassword == "" {
		return fmt.Errorf("no password was supplied, so this assertion would pass vacuously")
	}
	for _, arg := range w.lastRun.args {
		if strings.Contains(arg, w.lastPassword) {
			return fmt.Errorf("the password was passed as an argument: %v", w.lastRun.args)
		}
	}
	return nil
}

// --- A running instance, for the scenarios that use the account ---

// startInstance boots the app against the same store the command wrote to and
// mounts the password routes, so an account minted on the command line is
// checked through the same door a registered one uses.
func (w *accountCLIWorld) startInstance(registration bool) error {
	w.stopInstance()
	setEnvFor := map[string]string{
		"PASSWORD_AUTH_ENABLED":         "true",
		"PASSWORD_REGISTRATION_ENABLED": fmt.Sprintf("%t", registration),
	}
	for k, v := range setEnvFor {
		os.Setenv(k, v)
		defer os.Unsetenv(k)
	}

	cfg := config.Default()
	cfg.LoadFromEnvironment()
	cfg.DatabaseDSN = w.dsn
	cfg.LogLevel = "error"

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Populate(&w.authService, &w.credRepo, &w.agentRepo, &w.accountRepo),
		fx.Populate(&w.sessionMgr, &w.logger),
		fx.Populate(&w.resourceService, &w.resourceTypeService),
	)
	startCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start the instance: %w", err)
	}
	w.app = app

	e := echo.New()
	e.HideBanner = true
	api := e.Group("/api")
	api.Use(apimw.Messages())
	handlers.MountPasswordAuth(api, handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
		AuthService:    w.authService,
		SessionManager: w.sessionMgr,
		SecureCookies:  false,
		Logger:         w.logger,
	}), handlers.PasswordAuthRoutes{SignIn: cfg.PasswordAuthEnabled, Registration: cfg.PasswordRegistrationEnabled})
	w.server = httptest.NewServer(e)
	return nil
}

func (w *accountCLIWorld) stopInstance() {
	if w.server != nil {
		w.server.Close()
		w.server = nil
	}
	if w.app != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer cancel()
		_ = w.app.Stop(stopCtx)
		w.app = nil
	}
}

func (w *accountCLIWorld) teardown() {
	w.stopInstance()
	if w.tmpDir != "" {
		_ = os.RemoveAll(w.tmpDir)
		w.tmpDir = ""
	}
}

// ensureInstance starts one on demand, so scenarios that only assert on the
// store don't have to say "and the instance is started" first.
func (w *accountCLIWorld) ensureInstance() error {
	if w.app != nil {
		return nil
	}
	return w.startInstance(false)
}

// --- Store assertions ---

func (w *accountCLIWorld) credentialsFor(email string) ([]*authentities.Credential, error) {
	if err := w.ensureInstance(); err != nil {
		return nil, err
	}
	creds, err := w.credRepo.FindByEmail(context.Background(), strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return nil, fmt.Errorf("could not look up %q: %w", email, err)
	}
	return creds, nil
}

func (w *accountCLIWorld) storeHoldsExactlyOne(email string) error {
	creds, err := w.credentialsFor(email)
	if err != nil {
		return err
	}
	if len(creds) != 1 {
		return fmt.Errorf("expected exactly one account for %q, found %d", email, len(creds))
	}
	return nil
}

func (w *accountCLIWorld) noAccountExists(email string) error {
	creds, err := w.credentialsFor(email)
	if err != nil {
		return err
	}
	if len(creds) != 0 {
		return fmt.Errorf("expected no account for %q, found %d", email, len(creds))
	}
	return nil
}

// accountPresentedAs reads the name the account is shown under, which is what
// an operator sees rather than the email it was keyed on.
func (w *accountCLIWorld) accountPresentedAs(email, name string) error {
	creds, err := w.credentialsFor(email)
	if err != nil {
		return err
	}
	if len(creds) != 1 {
		return fmt.Errorf("expected exactly one account for %q, found %d", email, len(creds))
	}
	agent, err := w.agentRepo.FindByID(context.Background(), creds[0].AgentID())
	if err != nil {
		return fmt.Errorf("could not read the agent for %q: %w", email, err)
	}
	if agent.Name() != name {
		return fmt.Errorf("expected the account for %q to be presented as %q, got %q", email, name, agent.Name())
	}
	return nil
}

func (w *accountCLIWorld) eachOwnsPersonalAccount() error {
	for email := range w.signIns {
		creds, err := w.credentialsFor(email)
		if err != nil {
			return err
		}
		if len(creds) != 1 {
			return fmt.Errorf("expected exactly one credential for %q, found %d", email, len(creds))
		}
		accounts, err := w.accountRepo.FindByMember(context.Background(), creds[0].AgentID())
		if err != nil {
			return fmt.Errorf("could not read accounts for %q: %w", email, err)
		}
		if len(accounts) == 0 {
			return fmt.Errorf("expected %q to own a personal account, found none", email)
		}
	}
	return nil
}

// --- Sign-in over HTTP ---

func (w *accountCLIWorld) post(path, body string) (int, string, error) {
	if err := w.ensureInstance(); err != nil {
		return 0, "", err
	}
	resp, err := http.Post(w.server.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", err
	}
	return resp.StatusCode, string(raw), nil
}

func (w *accountCLIWorld) attemptSignIn(email, password string) (int, error) {
	status, _, err := w.post("/api/auth/password-login", credentialsJSON(email, password))
	if err != nil {
		return 0, err
	}
	if w.signIns == nil {
		w.signIns = map[string]int{}
	}
	w.signIns[email] = status
	return status, nil
}

func (w *accountCLIWorld) cliSignIn(email, password string) error {
	_, err := w.attemptSignIn(email, password)
	return err
}

func (w *accountCLIWorld) bothSignIn(first, second, password string) error {
	if err := w.cliSignIn(first, password); err != nil {
		return err
	}
	return w.cliSignIn(second, password)
}

func (w *accountCLIWorld) registersOverHTTP(email, password string) error {
	status, body, err := w.post("/api/auth/register", credentialsJSON(email, password))
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return fmt.Errorf("expected %q to register, got %d: %s", email, status, body)
	}
	return nil
}

func (w *accountCLIWorld) canSignIn(email, password string) error {
	status, err := w.attemptSignIn(email, password)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("expected %q to sign in, got %d", email, status)
	}
	return nil
}

func (w *accountCLIWorld) cannotSignInCLI(email, password string) error {
	status, err := w.attemptSignIn(email, password)
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		return fmt.Errorf("expected %q not to sign in with that password, but it worked", email)
	}
	return nil
}

func (w *accountCLIWorld) signInSucceeded() error {
	for email, status := range w.signIns {
		if status != http.StatusOK {
			return fmt.Errorf("expected %q to sign in, got %d", email, status)
		}
	}
	if len(w.signIns) == 0 {
		return fmt.Errorf("no sign-in was attempted")
	}
	return nil
}

func (w *accountCLIWorld) bothSignInsSucceeded() error {
	if len(w.signIns) < 2 {
		return fmt.Errorf("expected two sign-ins, saw %d", len(w.signIns))
	}
	return w.signInSucceeded()
}

func (w *accountCLIWorld) cliHoldsSession(email string) error {
	if status, ok := w.signIns[email]; !ok || status != http.StatusOK {
		return fmt.Errorf("expected %q to hold a session, sign-in status was %d", email, status)
	}
	return nil
}

// --- The store is already in use ---

// contextForOwner builds the identity the given account acts under, so seeding
// and reading agree about who owns what.
func (w *accountCLIWorld) contextForOwner(email string) (context.Context, error) {
	ctx := context.Background()
	creds, err := w.credRepo.FindByEmail(ctx, email)
	if err != nil || len(creds) == 0 {
		return nil, fmt.Errorf("could not find the agent for %q: %w", email, err)
	}
	accounts, err := w.accountRepo.FindByMember(ctx, creds[0].AgentID())
	if err != nil || len(accounts) == 0 {
		return nil, fmt.Errorf("could not find an account for %q: %w", email, err)
	}
	return auth.ContextWithAgent(ctx, &auth.Identity{
		AgentID:         creds[0].AgentID(),
		AccountIDs:      []string{accounts[0].GetID()},
		ActiveAccountID: accounts[0].GetID(),
	}), nil
}

// seedTask puts unrelated data in the store, so a scenario can show the command
// adds an account without disturbing what was already there.
func (w *accountCLIWorld) seedTask(name string) error {
	ctx := context.Background()
	if _, err := w.resourceTypeService.InstallPreset(ctx, "tasks", true); err != nil {
		return fmt.Errorf("could not install the tasks preset: %w", err)
	}
	ownerCtx, err := w.contextForOwner("founder@harborlegal.example")
	if err != nil {
		return err
	}
	data, _ := json.Marshal(map[string]any{"name": name, "status": "open", "priority": "medium"})
	if _, err := w.resourceService.Create(ownerCtx, application.CreateResourceCommand{
		TypeSlug: "task", Data: data,
	}); err != nil {
		return fmt.Errorf("could not create the task %q: %w", name, err)
	}
	return nil
}

func (w *accountCLIWorld) storeStillHolds(email, taskName string) error {
	if err := w.storeHoldsExactlyOne(email); err != nil {
		return err
	}
	// Listed as the agent that owns it: resources are account-scoped, so an
	// anonymous read returns nothing and would report the task as lost when it
	// is merely not ours to see.
	ownerCtx, err := w.contextForOwner("founder@harborlegal.example")
	if err != nil {
		return err
	}
	page, err := w.resourceService.List(ownerCtx, "task", "", 100, repositories.SortOptions{})
	if err != nil {
		return fmt.Errorf("could not list tasks: %w", err)
	}
	// Resources are stored as JSON-LD, so the name lives in the @graph node
	// rather than at the top level.
	for _, resource := range page.Data {
		var payload struct {
			Graph []struct {
				Name string `json:"name"`
			} `json:"@graph"`
		}
		if err := json.Unmarshal(resource.Data(), &payload); err != nil {
			continue
		}
		for _, node := range payload.Graph {
			if node.Name == taskName {
				return nil
			}
		}
	}
	return fmt.Errorf("expected the task %q to survive; the store held %d task(s)", taskName, len(page.Data))
}

// --- Assertions for the two failures that look like success ---

func (w *accountCLIWorld) failureNamesNoDatabase() error {
	said := strings.ToLower(w.lastRun.said())
	if !strings.Contains(said, "no database specified") {
		return fmt.Errorf("expected the failure to say no database was specified, it said: %s", w.lastRun.said())
	}
	// Naming the remedy is half the point: this message is read by whoever is
	// staring at a container that would not start.
	if !strings.Contains(said, "database_dsn") {
		return fmt.Errorf("expected the failure to name how to supply a database, it said: %s", w.lastRun.said())
	}
	return nil
}

func (w *accountCLIWorld) reportedExistsViaGoogle() error {
	if err := w.reportedAlreadyExists(); err != nil {
		return err
	}
	if !strings.Contains(strings.ToLower(w.lastRun.said()), "google") {
		return fmt.Errorf("expected the report to name Google, it said: %s", w.lastRun.said())
	}
	return nil
}

// noStoreWasWritten checks the whole working directory, not one expected path.
// The defect this guards against was the command writing to a store nobody
// named, so asserting against a known filename would miss the case entirely.
func (w *accountCLIWorld) noStoreWasWritten() error {
	return w.noStrayDatabase()
}

func (w *accountCLIWorld) noStrayDatabase() error {
	entries, err := os.ReadDir(w.workDir)
	if err != nil {
		return fmt.Errorf("could not inspect the directory the command ran from: %w", err)
	}
	stray := []string{}
	for _, entry := range entries {
		stray = append(stray, entry.Name())
	}
	if len(stray) > 0 {
		return fmt.Errorf("expected the command to leave nothing behind, found: %v", stray)
	}
	return nil
}
