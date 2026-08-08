package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/api/handlers"
	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authcasbin "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/casbin"
	authhttp "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/http"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"github.com/cucumber/godog"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
)

// TestPasswordRegistrationFlag runs the acceptance scenarios for story #136
// (epic #135): an instance can offer password sign-in without also opening
// account registration.
//
// The scenarios turn on a distinction that only holds if the route is truly
// absent — 404 with no authentication challenge and no Allow header, answering
// exactly like a path the server has never had. So these tests drive real HTTP
// against an Echo instance mounted through handlers.MountPasswordAuth, the same
// call serve.go makes. Re-declaring the routes here instead would let the test
// and production drift apart while both stayed green.
func TestPasswordRegistrationFlag(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "password-registration-flag",
		ScenarioInitializer: initPasswordRegistrationScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/password_registration_flag.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("password registration flag acceptance scenarios failed")
	}
}

const registrationPath = "/api/auth/register"
const signInPath = "/api/auth/password-login"

// capturedResponse is everything a caller can observe about one answer. The
// scenarios compare whole answers against each other ("the same status and the
// same body"), so the body is kept verbatim rather than parsed.
type capturedResponse struct {
	status     int
	body       string
	wwwAuth    string
	allow      string
	setCookies []string
}

// passwordAuthWorld boots the real application against a temp SQLite database
// and mounts the password routes exactly as serve.go does.
type passwordAuthWorld struct {
	app    *fx.App
	tmpDir string
	dsn    string
	server *httptest.Server

	authService authapp.AuthenticationService
	credRepo    authrepos.CredentialRepository
	agentRepo   authrepos.AgentRepository
	accountRepo authrepos.AccountRepository
	sessionMgr  session.SessionManager
	sessionsSt  sessions.Store
	authzCheck  *authcasbin.CasbinAuthorizationChecker
	resources   application.ResourceService
	resourceTps application.ResourceTypeService
	logger      entities.Logger

	// answers accumulates every response the scenario provoked, in order, so
	// steps phrased as "both requests..." can compare them.
	answers []*capturedResponse
}

func initPasswordRegistrationScenario(sc *godog.ScenarioContext) {
	w := &passwordAuthWorld{}

	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*w = passwordAuthWorld{}
		return ctx, nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a WeOS instance where password sign-in is (enabled|disabled) and account registration is (enabled|disabled|not configured)$`,
		w.anInstanceConfigured)
	sc.Step(`^the instance already has the account "([^"]*)" with password "([^"]*)"$`, w.instanceHasAccount)
	sc.Step(`^"([^"]*)" registered with password "([^"]*)"$`, w.someoneRegisters)
	sc.Step(`^the operator restarts the instance with account registration disabled$`, w.restartWithRegistrationDisabled)

	sc.Step(`^"([^"]*)" signs in with password "([^"]*)"$`, w.signIn)
	sc.Step(`^"([^"]*)" attempts a password sign-in with password "([^"]*)"$`, w.signIn)
	sc.Step(`^someone submits a registration for "([^"]*)" with password "([^"]*)"$`, w.submitRegistration)
	sc.Step(`^someone submits a well-formed registration for "([^"]*)" with password "([^"]*)"$`, w.submitRegistration)
	sc.Step(`^someone registers "([^"]*)" with password "([^"]*)"$`, w.submitRegistration)
	sc.Step(`^someone sends a ([A-Z]+) request to the registration endpoint$`, w.sendMethodToRegistration)
	sc.Step(`^someone posts the same details to "([^"]*)", an endpoint this instance has never had$`, w.postToNeverMountedPath)
	sc.Step(`^someone sends the same ([A-Z]+) request to "([^"]*)", a path this instance has never had$`, w.sendMethodToNeverMountedPath)
	sc.Step(`^someone submits a registration request whose body is not valid JSON$`, w.submitMalformedRegistration)

	sc.Step(`^the sign-in succeeds$`, w.lastSucceeded)
	sc.Step(`^the registration succeeds$`, w.lastSucceeded)
	sc.Step(`^"([^"]*)" holds an authenticated session$`, w.holdsAuthenticatedSession)
	sc.Step(`^the registration request is answered "([^"]*)"$`, w.lastAnsweredWith)
	sc.Step(`^the request is answered "([^"]*)"$`, w.lastAnsweredWith)
	sc.Step(`^a registration for "([^"]*)" is answered "([^"]*)"$`, w.registrationForIsAnswered)
	sc.Step(`^the answer offers no authentication challenge$`, w.noAuthChallenge)
	sc.Step(`^the answer advertises no permitted methods for that path$`, w.noAllowHeader)
	sc.Step(`^both requests are answered with the same status and the same body$`, w.lastTwoAnswersIdentical)
	sc.Step(`^both requests are answered "([^"]*)"$`, w.lastTwoAnsweredWith)
	sc.Step(`^no account exists for "([^"]*)"$`, w.noAccountExistsFor)
	sc.Step(`^"([^"]*)" cannot sign in with password "([^"]*)"$`, w.cannotSignIn)
	sc.Step(`^"([^"]*)" can sign in again with password "([^"]*)"$`, w.canSignInAgain)
}

// --- Boot ---

// anInstanceConfigured boots an instance with the two settings in the stated
// positions. The settings are delivered as environment variables and read back
// through config.LoadFromEnvironment so the scenarios cover the real parsing
// path — including "not configured", which is the default that decides whether
// an existing deployment's register route stays open after an upgrade.
func (w *passwordAuthWorld) anInstanceConfigured(signIn, registration string) error {
	dir, err := os.MkdirTemp("", "weos-password-registration-e2e-")
	if err != nil {
		return err
	}
	w.tmpDir = dir
	w.dsn = filepath.Join(dir, "test.db")
	return w.boot(signIn == "enabled", registrationSetting(registration))
}

// registrationSetting maps the Gherkin wording to the environment variable's
// value: "not configured" means the variable is absent entirely, which is a
// different input from an explicit "false" even though both must end up off.
func registrationSetting(registration string) *bool {
	switch registration {
	case "enabled":
		v := true
		return &v
	case "disabled":
		v := false
		return &v
	default: // "not configured"
		return nil
	}
}

func (w *passwordAuthWorld) boot(signIn bool, registration *bool) error {
	os.Setenv("PASSWORD_AUTH_ENABLED", strconv.FormatBool(signIn))
	if registration == nil {
		os.Unsetenv("PASSWORD_REGISTRATION_ENABLED")
	} else {
		os.Setenv("PASSWORD_REGISTRATION_ENABLED", strconv.FormatBool(*registration))
	}

	cfg := config.Default()
	cfg.LoadFromEnvironment()
	// Pin the storage and noise level after reading the environment so an
	// ambient DATABASE_DSN can't pull the scenario onto a real database.
	cfg.DatabaseDSN = w.dsn
	cfg.LogLevel = "error"

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Populate(&w.authService, &w.credRepo, &w.agentRepo, &w.accountRepo),
		fx.Populate(&w.sessionMgr, &w.sessionsSt, &w.authzCheck, &w.logger),
		fx.Populate(&w.resources, &w.resourceTps),
	)
	startCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}
	w.app = app

	// The same public route layout serve.go builds, mounted through the same
	// call — so whichever endpoints production would expose for this config
	// are exactly the ones under test.
	e := echo.New()
	e.HideBanner = true
	api := e.Group("/api")
	api.Use(apimw.Messages())
	passwordAuthHandlers := handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
		AuthService:    w.authService,
		SessionManager: w.sessionMgr,
		SecureCookies:  false,
		Logger:         w.logger,
	})
	handlers.MountPasswordAuth(api, passwordAuthHandlers, handlers.PasswordAuthRoutes{
		SignIn:       cfg.PasswordAuthEnabled,
		Registration: cfg.PasswordRegistrationEnabled,
	})

	// The generic resource catch-all, mounted the way serve.go mounts it.
	//
	// This is not incidental scenery. /api/auth/register is two path segments,
	// so GET, PUT and DELETE on it match `/:typeSlug/:id` and are answered by
	// the catch-all rather than by anything to do with registration. Leaving
	// these routes out would let a scenario "prove" that a closed register
	// endpoint is indistinguishable from a path that never existed while
	// comparing two paths that were both simply absent from a much smaller
	// routing table than production's. The comparison only means something
	// against the real one.
	// The auth middleware is attached per route rather than through
	// api.Group("").Use(...), which is how serve.go spells it. That is not a
	// cosmetic difference and it is worth knowing why.
	//
	// echo's Group.Use registers a catch-all not-found route for the group's
	// prefix, wrapped in that group's middleware. serve.go builds *three*
	// groups on the same empty prefix (the protected group, the invite-accept
	// group, and the MCP group), so the /api/* catch-all ends up owned by
	// whichever of them called Use last — the invite/MCP groups, whose
	// middleware does not reject anonymous callers. That accident is why an
	// unmatched POST under /api answers a plain 404 while an unmatched GET
	// answers 401: the GET matches the real /:typeSlug/:id route and meets
	// RequireAuth, the POST matches nothing and falls to the catch-all.
	//
	// Reproducing that by declaring three throwaway groups here would be
	// copying an accident. Attaching the middleware to the routes themselves
	// leaves the catch-all as api.Use registered it and reproduces both
	// observed behaviours exactly, which is what these scenarios rest on.
	var guards []echo.MiddlewareFunc
	if cfg.AuthEnabled() {
		guards = []echo.MiddlewareFunc{
			echo.WrapMiddleware(authhttp.RequireAuth(w.sessionMgr, w.authService)),
			apimw.Impersonation(w.sessionsSt, w.accountRepo, w.logger),
			apimw.AuthorizeResource(w.authzCheck, w.accountRepo, w.logger),
		}
	} else {
		guards = []echo.MiddlewareFunc{apimw.SoftAuth(w.credRepo, w.agentRepo, w.accountRepo, w.logger)}
	}
	resourceHandler := handlers.NewResourceHandler(w.resources, w.resourceTps, w.logger)
	api.POST("/:typeSlug", resourceHandler.Create, guards...)
	api.GET("/:typeSlug", resourceHandler.List, guards...)
	api.GET("/:typeSlug/:id", resourceHandler.Get, guards...)
	api.PUT("/:typeSlug/:id", resourceHandler.Update, guards...)
	api.DELETE("/:typeSlug/:id", resourceHandler.Delete, guards...)

	w.server = httptest.NewServer(e)
	return nil
}

// restartWithRegistrationDisabled stops the instance and brings it back up
// against the same database with registration off — the upgrade an operator
// performs, and the moment an already-registered account must not lose access.
func (w *passwordAuthWorld) restartWithRegistrationDisabled() error {
	w.stopServer()
	off := false
	return w.boot(true, &off)
}

func (w *passwordAuthWorld) stopServer() {
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

func (w *passwordAuthWorld) teardown() {
	w.stopServer()
	os.Unsetenv("PASSWORD_AUTH_ENABLED")
	os.Unsetenv("PASSWORD_REGISTRATION_ENABLED")
	if w.tmpDir != "" {
		_ = os.RemoveAll(w.tmpDir)
		w.tmpDir = ""
	}
}

// --- Seeding ---

// instanceHasAccount creates an account the way an operator would out of band
// (the CLI path of story #137) rather than through the HTTP route under test,
// so the scenario holds even on an instance where registration is closed.
func (w *passwordAuthWorld) instanceHasAccount(email, password string) error {
	local, _, _ := strings.Cut(email, "@")
	_, _, _, err := w.authService.RegisterPassword(context.Background(), email, local, password)
	if err != nil {
		return fmt.Errorf("seed account %q: %w", email, err)
	}
	return nil
}

// someoneRegisters registers through the HTTP route and requires it to work —
// used as a Given on instances where registration is open.
func (w *passwordAuthWorld) someoneRegisters(email, password string) error {
	if err := w.submitRegistration(email, password); err != nil {
		return err
	}
	return w.lastSucceeded()
}

// --- Requests ---

func (w *passwordAuthWorld) do(method, path, body string) error {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, w.server.URL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	w.answers = append(w.answers, &capturedResponse{
		status:     resp.StatusCode,
		body:       strings.TrimSpace(string(raw)),
		wwwAuth:    resp.Header.Get("WWW-Authenticate"),
		allow:      resp.Header.Get("Allow"),
		setCookies: resp.Header.Values("Set-Cookie"),
	})
	return nil
}

func credentialsJSON(email, password string) string {
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	return string(body)
}

func (w *passwordAuthWorld) submitRegistration(email, password string) error {
	return w.do(http.MethodPost, registrationPath, credentialsJSON(email, password))
}

func (w *passwordAuthWorld) submitMalformedRegistration() error {
	return w.do(http.MethodPost, registrationPath, `{"email": "newcomer@harborlegal.example", "password":`)
}

func (w *passwordAuthWorld) sendMethodToRegistration(method string) error {
	return w.do(method, registrationPath, "")
}

func (w *passwordAuthWorld) postToNeverMountedPath(path string) error {
	return w.do(http.MethodPost, path,
		credentialsJSON("newcomer@harborlegal.example", "correct-horse-battery-staple"))
}

// sendMethodToNeverMountedPath issues the same method against a path this
// instance has never had, so the previous answer can be compared with it. The
// control is what makes "absent" measurable: the registration path has to be
// as ordinary as any invented one, whatever that happens to look like.
func (w *passwordAuthWorld) sendMethodToNeverMountedPath(method, path string) error {
	return w.do(method, path, "")
}

func (w *passwordAuthWorld) signIn(email, password string) error {
	return w.do(http.MethodPost, signInPath, credentialsJSON(email, password))
}

// --- Assertions ---

func (w *passwordAuthWorld) last() (*capturedResponse, error) {
	if len(w.answers) == 0 {
		return nil, fmt.Errorf("no request has been made yet")
	}
	return w.answers[len(w.answers)-1], nil
}

// parseStatus turns the Gherkin wording ("404 Not Found") into a code, so the
// scenarios stay readable while the assertion stays exact.
func parseStatus(want string) (int, error) {
	code, _, _ := strings.Cut(want, " ")
	n, err := strconv.Atoi(code)
	if err != nil {
		return 0, fmt.Errorf("could not read a status code from %q", want)
	}
	return n, nil
}

func (w *passwordAuthWorld) lastSucceeded() error {
	got, err := w.last()
	if err != nil {
		return err
	}
	if got.status != http.StatusOK && got.status != http.StatusCreated {
		return fmt.Errorf("expected success, got %d: %s", got.status, got.body)
	}
	return nil
}

func (w *passwordAuthWorld) lastAnsweredWith(want string) error {
	code, err := parseStatus(want)
	if err != nil {
		return err
	}
	got, err := w.last()
	if err != nil {
		return err
	}
	if got.status != code {
		return fmt.Errorf("expected %d, got %d: %s", code, got.status, got.body)
	}
	return nil
}

func (w *passwordAuthWorld) registrationForIsAnswered(email, want string) error {
	if err := w.submitRegistration(email, "correct-horse-battery-staple"); err != nil {
		return err
	}
	return w.lastAnsweredWith(want)
}

func (w *passwordAuthWorld) lastTwoAnsweredWith(want string) error {
	code, err := parseStatus(want)
	if err != nil {
		return err
	}
	if len(w.answers) < 2 {
		return fmt.Errorf("expected two requests, saw %d", len(w.answers))
	}
	for _, got := range w.answers[len(w.answers)-2:] {
		if got.status != code {
			return fmt.Errorf("expected both answers to be %d, got %d: %s", code, got.status, got.body)
		}
	}
	return nil
}

// lastTwoAnswersIdentical is the anti-probe assertion: a closed registration
// endpoint must be indistinguishable from a path that never existed, and must
// not betray whether it understood what it was sent.
func (w *passwordAuthWorld) lastTwoAnswersIdentical() error {
	if len(w.answers) < 2 {
		return fmt.Errorf("expected two requests, saw %d", len(w.answers))
	}
	a, b := w.answers[len(w.answers)-2], w.answers[len(w.answers)-1]
	if a.status != b.status {
		return fmt.Errorf("answers differ in status: %d vs %d", a.status, b.status)
	}
	if a.body != b.body {
		return fmt.Errorf("answers differ in body: %q vs %q", a.body, b.body)
	}
	return nil
}

func (w *passwordAuthWorld) noAuthChallenge() error {
	got, err := w.last()
	if err != nil {
		return err
	}
	if got.wwwAuth != "" {
		return fmt.Errorf("expected no authentication challenge, got WWW-Authenticate: %q", got.wwwAuth)
	}
	return nil
}

func (w *passwordAuthWorld) noAllowHeader() error {
	got, err := w.last()
	if err != nil {
		return err
	}
	if got.allow != "" {
		return fmt.Errorf("expected no permitted methods to be advertised, got Allow: %q", got.allow)
	}
	return nil
}

func (w *passwordAuthWorld) holdsAuthenticatedSession(email string) error {
	got, err := w.last()
	if err != nil {
		return err
	}
	if got.status != http.StatusOK && got.status != http.StatusCreated {
		return fmt.Errorf("expected an authenticated answer, got %d: %s", got.status, got.body)
	}
	if len(got.setCookies) == 0 {
		return fmt.Errorf("expected a session cookie to be issued for %q, none was", email)
	}
	// Successful answers ride in the standard {"data": …} envelope.
	var payload struct {
		Data struct {
			Agent struct {
				Email string `json:"email"`
			} `json:"agent"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got.body), &payload); err != nil {
		return fmt.Errorf("could not read the authentication answer: %w (body: %s)", err, got.body)
	}
	if !strings.EqualFold(payload.Data.Agent.Email, email) {
		return fmt.Errorf("expected the session to belong to %q, got %q", email, payload.Data.Agent.Email)
	}
	return nil
}

func (w *passwordAuthWorld) noAccountExistsFor(email string) error {
	// An unknown email is an empty result, not an error, so a real error means
	// the lookup never happened — report it rather than reading it as absence,
	// which would let a broken database pass this scenario.
	creds, err := w.credRepo.FindByEmail(context.Background(), email)
	if err != nil {
		return fmt.Errorf("could not check whether an account exists for %q: %w", email, err)
	}
	if len(creds) > 0 {
		return fmt.Errorf("expected no account for %q, found %d credential(s)", email, len(creds))
	}
	return nil
}

func (w *passwordAuthWorld) cannotSignIn(email, password string) error {
	if err := w.signIn(email, password); err != nil {
		return err
	}
	got, err := w.last()
	if err != nil {
		return err
	}
	if got.status == http.StatusOK || got.status == http.StatusCreated {
		return fmt.Errorf("expected %q not to be able to sign in, but the sign-in succeeded", email)
	}
	return nil
}

func (w *passwordAuthWorld) canSignInAgain(email, password string) error {
	if err := w.signIn(email, password); err != nil {
		return err
	}
	return w.holdsAuthenticatedSession(email)
}
