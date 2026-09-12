package e2e

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wepala/weos/v3/api/handlers"
	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"
	weosoauth "github.com/wepala/weos/v3/internal/oauth"
	"github.com/wepala/weos/v3/internal/trustedissuer"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"github.com/cucumber/godog"
	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
)

// TestTrustedIssuerAccount runs the acceptance scenarios for story wm-63gg0.2
// (epic wm-63gg0): an assertion the instance accepts signs in the one person
// it names — found by provider and subject, linked to its owner by email on an
// allowlisted instance, or created — and answers as a password sign-in does,
// plus new_account.
//
// The scenarios drive real HTTP against the real application, with the
// assertion route mounted through handlers.MountTrustedIssuerAssertion and the
// sign-in service the application module provides, as serve.go does. The door
// is the story-1 test door, publishing one key. Unlike the story-1 world, this
// one keeps its database across restarts: "the instance has since been
// restarted with no allowlist" is a real restart onto the same store.
func TestTrustedIssuerAccount(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "trusted-issuer-account",
		ScenarioInitializer: initTrustedIssuerAccountScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/trusted_issuer_account.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("trusted issuer account acceptance scenarios failed")
	}
}

const (
	taSigningKey = "door-2026-09"
	// taTokenCookie is the JWT cookie completeAuth sets under its default name.
	taTokenCookie = "pericarp_token"
)

func initTrustedIssuerAccountScenario(sc *godog.ScenarioContext) {
	w := &taWorld{
		logs:     &tiLogCapture{},
		earlier:  map[string]string{},
		accounts: map[string]string{},
	}
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	// The instance
	sc.Step(`^a WeOS instance that trusts login assertions from "([^"]*)" for the audience "([^"]*)"$`, w.instanceTrusting)
	sc.Step(`^the instance's allowlist names "([^"]*)"$`, func(email string) error { return w.allowlist(email) })
	sc.Step(`^the instance's allowlist names "([^"]*)" and "([^"]*)"$`, func(a, b string) error { return w.allowlist(a, b) })
	sc.Step(`^the instance has no allowlist$`, func() error { return w.allowlist() })
	sc.Step(`^the instance has since been restarted with no allowlist$`, func() error { return w.allowlist() })
	sc.Step(`^password sign-in is also enabled on that instance$`, w.passwordSignInEnabled)
	sc.Step(`^the account "([^"]*)", whose owner "([^"]*)" signs in with password "([^"]*)"$`, w.accountWithOwner)

	// Sign-ins
	sc.Step(`^the door presents an assertion for "([^"]*)" from "([^"]*)" with the subject "([^"]*)"$`,
		func(email, provider, sub string) error { return w.present(email, "", provider, sub) })
	sc.Step(`^the door presents an assertion for "([^"]*)" named "([^"]*)" from "([^"]*)" with the subject "([^"]*)"$`, w.present)
	sc.Step(`^the door presents an assertion for "([^"]*)" from "([^"]*)" with the subject "([^"]*)" and no name$`,
		func(email, provider, sub string) error { return w.present(email, "", provider, sub) })
	sc.Step(`^the door presents two separately signed assertions for "([^"]*)" from "([^"]*)" with the subject "([^"]*)" at the same moment$`, w.presentTwoTogether)
	sc.Step(`^"([^"]*)" signed in through the door from "([^"]*)" with the subject "([^"]*)" earlier$`, w.signedInEarlier)
	sc.Step(`^that person's own personal account has been deactivated$`, w.personalAccountDeactivated)
	sc.Step(`^"([^"]*)" signs in with password "([^"]*)"$`, w.passwordSignIn)

	// Outcomes
	sc.Step(`^the sign-in succeeds$`, func() error { return w.last().succeeded() })
	sc.Step(`^both sign-ins succeed$`, w.bothSucceed)
	sc.Step(`^both sign-ins name the same person$`, w.bothNameSamePerson)
	sc.Step(`^the sign-in reports that it created a new account$`, func() error { return w.reportsNewAccount(true) })
	sc.Step(`^the sign-in reports that it created no new account$`, func() error { return w.reportsNewAccount(false) })
	sc.Step(`^the store holds exactly one account for "([^"]*)"$`, w.storeHoldsOneAccount)
	sc.Step(`^the account their requests act in is the one the sign-in reported$`, w.actingAccountIsReported)
	sc.Step(`^the account their requests act in is "([^"]*)"$`, w.actingAccountIsNamed)
	sc.Step(`^the person the sign-in names is the one "([^"]*)" signed in as earlier$`, func(email string) error { return w.personIsEarlier(email, true) })
	sc.Step(`^the person the sign-in names is not the one "([^"]*)" signed in as earlier$`, func(email string) error { return w.personIsEarlier(email, false) })
	sc.Step(`^the answer carries these fields and no others:$`, w.answerCarriesFields)
	sc.Step(`^the token cookie it sets holds the token the answer carries$`, w.tokenCookieHoldsToken)
	sc.Step(`^the two sign-ins set the same cookies, each with the same path, lifetime and protections$`, w.sameCookies)
	sc.Step(`^the answer names the person "([^"]*)"$`, w.answerNamesPerson)
	sc.Step(`^the sign-in reports no account$`, w.reportsNoAccount)
	sc.Step(`^the sign-in hands back no token$`, w.handsBackNoToken)
	sc.Step(`^no token cookie is set$`, w.noTokenCookie)
}

// --- answers ---

type taAnswer struct {
	status  int
	body    string
	cookies []*http.Cookie
	fields  map[string]json.RawMessage

	agentID, agentName, agentEmail string
	accountID, accountName         string
	token                          string
	expiresAt                      string
	newAccount                     *bool
}

func (a *taAnswer) succeeded() error {
	if a == nil {
		return fmt.Errorf("no sign-in has been attempted")
	}
	if a.status != http.StatusOK {
		return fmt.Errorf("expected the sign-in to succeed, got %d: %s", a.status, a.body)
	}
	return nil
}

func (a *taAnswer) cookie(name string) *http.Cookie {
	for _, c := range a.cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func readTaAnswer(resp *http.Response) (*taAnswer, error) {
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	a := &taAnswer{status: resp.StatusCode, body: strings.TrimSpace(string(raw)), cookies: resp.Cookies()}
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Data == nil {
		return a, nil // a refusal or failure; the steps that need data say so
	}
	a.fields = envelope.Data
	var agent struct{ ID, Name, Email string }
	_ = json.Unmarshal(envelope.Data["agent"], &agent) // absent reads as empty, and the steps check
	a.agentID, a.agentName, a.agentEmail = agent.ID, agent.Name, agent.Email
	var account *struct{ ID, Name string }
	_ = json.Unmarshal(envelope.Data["account"], &account) // absent or null reads as no account
	if account != nil {
		a.accountID, a.accountName = account.ID, account.Name
	}
	_ = json.Unmarshal(envelope.Data["token"], &a.token)            // absent reads as no token
	_ = json.Unmarshal(envelope.Data["expires_at"], &a.expiresAt)   // checked by the step that names it
	_ = json.Unmarshal(envelope.Data["new_account"], &a.newAccount) // absent stays nil, which no step accepts
	return a, nil
}

// --- the world ---

type taWorld struct {
	logs *tiLogCapture
	door *tiDoor

	issuer, audience string
	tmpDir, dsn      string
	app              *fx.App
	server           *httptest.Server

	authService    authapp.AuthenticationService
	credRepo       authrepos.CredentialRepository
	accountRepo    authrepos.AccountRepository
	sessionManager session.SessionManager
	erasureLocks   repositories.AccountErasureLocks
	signIn         *application.AssertedSignIn

	mu              sync.Mutex
	signIns         []*taAnswer // answers to assertions, in order
	passwordSignIns []*taAnswer
	earlier         map[string]string // email -> the person its earlier sign-in named
	lastEarlier     string            // the email of the most recent earlier sign-in
	accounts        map[string]string // account name -> id

	envBefore map[string]*string
}

func (w *taWorld) instanceTrusting(issuer, audience string) error {
	w.door = newTiDoor()
	if err := w.door.publish(taSigningKey); err != nil {
		return err
	}
	w.issuer, w.audience = issuer, audience
	w.setEnv(config.EnvTrustedIssuer, ptr(issuer))
	w.setEnv(config.EnvTrustedIssuerJWKSURL, ptr(w.door.server.URL+"/door/jwks.json"))
	w.setEnv(config.EnvTrustedIssuerAudience, ptr(audience))
	// An ambient setting must not decide a scenario that does not name it.
	w.setEnv("OAUTH_ALLOWED_EMAILS", nil)
	w.setEnv("PASSWORD_AUTH_ENABLED", nil)
	w.setEnv("PASSWORD_REGISTRATION_ENABLED", nil)
	w.setEnv("GOOGLE_CLIENT_ID", nil)
	w.setEnv("GOOGLE_CLIENT_SECRET", nil)
	return w.boot()
}

// allowlist restarts the instance with the allowlist naming emails, or with no
// allowlist when none are given.
func (w *taWorld) allowlist(emails ...string) error {
	if len(emails) == 0 {
		w.setEnv("OAUTH_ALLOWED_EMAILS", nil)
	} else {
		w.setEnv("OAUTH_ALLOWED_EMAILS", ptr(strings.Join(emails, ",")))
	}
	return w.boot()
}

func (w *taWorld) passwordSignInEnabled() error {
	w.setEnv("PASSWORD_AUTH_ENABLED", ptr("true"))
	return w.boot()
}

// boot starts, or restarts, the application on the scenario's one database,
// reading every setting from the environment through config, and mounts the
// password and assertion routes the way serve.go does.
func (w *taWorld) boot() error {
	if w.tmpDir == "" {
		dir, err := os.MkdirTemp("", "weos-trusted-issuer-account-e2e-")
		if err != nil {
			return err
		}
		w.tmpDir, w.dsn = dir, filepath.Join(dir, "test.db")
	}
	w.shutdown()

	cfg := config.Default()
	cfg.LoadFromEnvironment()
	cfg.DatabaseDSN = w.dsn
	cfg.LogLevel = "error"

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Provide(weosoauth.ProvideJWTService),
		fx.Populate(&w.authService, &w.credRepo, &w.accountRepo),
		fx.Populate(&w.sessionManager, &w.erasureLocks, &w.signIn),
	)
	startCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}
	w.app = app

	e := echo.New()
	e.HideBanner = true
	api := e.Group("/api")
	api.Use(apimw.Messages())
	sessions := handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
		AuthService:    w.authService,
		SessionManager: w.sessionManager,
		SecureCookies:  cfg.SessionSecret != "change-me-in-production",
		Logger:         w.logs,
		AccountRepo:    w.accountRepo,
		ErasureLocks:   w.erasureLocks,
	})
	handlers.MountPasswordAuth(api, sessions, handlers.PasswordAuthRoutes{
		SignIn:       cfg.PasswordAuthEnabled,
		Registration: cfg.PasswordRegistrationEnabled,
	})
	mounted := handlers.MountTrustedIssuerAssertion(context.Background(), api, cfg.TrustedIssuer, w.logs,
		func() *handlers.TrustedIssuerHandler {
			return handlers.NewTrustedIssuerHandler(handlers.TrustedIssuerHandlerConfig{
				Verifier: trustedissuer.NewVerifier(trustedissuer.Config{
					Issuer:    cfg.TrustedIssuer.Issuer,
					JWKSURL:   cfg.TrustedIssuer.JWKSURL,
					Audience:  cfg.TrustedIssuer.Audience,
					Providers: application.OAuthProviderKeys(),
					Logger:    w.logs,
				}),
				SignIn:   w.signIn,
				Sessions: sessions,
				Logger:   w.logs,
			})
		})
	if !mounted {
		return fmt.Errorf("the instance did not mount the assertion route; its log:\n%s", w.logs.text())
	}
	w.server = httptest.NewServer(e)
	return nil
}

// shutdown stops the server and the application and keeps the database.
func (w *taWorld) shutdown() {
	if w.server != nil {
		w.server.Close()
		w.server = nil
	}
	if w.app != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer cancel()
		_ = w.app.Stop(stopCtx) // teardown: a failed stop cannot change the scenario's verdict
		w.app = nil
	}
}

func (w *taWorld) teardown() {
	w.shutdown()
	w.door.close()
	w.restoreEnv()
	if w.tmpDir != "" {
		_ = os.RemoveAll(w.tmpDir) // a temp dir left behind costs disk, not correctness
		w.tmpDir = ""
	}
}

func (w *taWorld) setEnv(key string, value *string) {
	if w.envBefore == nil {
		w.envBefore = map[string]*string{}
	}
	if _, remembered := w.envBefore[key]; !remembered {
		if prior, ok := os.LookupEnv(key); ok {
			w.envBefore[key] = &prior
		} else {
			w.envBefore[key] = nil
		}
	}
	if value == nil {
		os.Unsetenv(key)
		return
	}
	os.Setenv(key, *value)
}

func (w *taWorld) restoreEnv() {
	for key, prior := range w.envBefore {
		if prior == nil {
			os.Unsetenv(key)
			continue
		}
		os.Setenv(key, *prior)
	}
	w.envBefore = nil
}

// --- staging ---

// accountWithOwner has the operator create a person with a password, and names
// their personal account.
func (w *taWorld) accountWithOwner(name, email, password string) error {
	ctx := context.Background()
	_, _, account, err := w.authService.RegisterPassword(ctx, email, handlers.DefaultDisplayName(email, ""), password)
	if err != nil {
		return fmt.Errorf("could not create %q: %w", email, err)
	}
	if account == nil {
		return fmt.Errorf("%q was created without a personal account to name %q", email, name)
	}
	renamed := &authentities.Account{}
	if err := renamed.Restore(account.GetID(), name, account.AccountType(), account.Active(), account.CreatedAt()); err != nil {
		return fmt.Errorf("could not name the account %q: %w", name, err)
	}
	if err := w.accountRepo.Save(ctx, renamed); err != nil {
		return fmt.Errorf("could not save the account %q: %w", name, err)
	}
	w.accounts[name] = account.GetID()
	return nil
}

func (w *taWorld) personalAccountDeactivated() error {
	agentID, ok := w.earlier[w.lastEarlier]
	if !ok {
		return fmt.Errorf("nobody has signed in earlier")
	}
	ctx := context.Background()
	personal, err := w.accountRepo.FindPersonalByMember(ctx, agentID)
	if err != nil || personal == nil {
		return fmt.Errorf("could not read the personal account of %q: %v", w.lastEarlier, err)
	}
	if err := personal.Deactivate(); err != nil {
		return fmt.Errorf("could not deactivate the personal account of %q: %w", w.lastEarlier, err)
	}
	return w.accountRepo.Save(ctx, personal)
}

// --- requests ---

// assertion is a good assertion for the person, minted now with a fresh jti.
// An empty name leaves the claim out.
func (w *taWorld) assertion(email, name, provider, sub string) (string, error) {
	now := time.Now()
	claims := gojwt.MapClaims{
		"iss":            w.issuer,
		"aud":            w.audience,
		"sub":            sub,
		"email":          email,
		"email_verified": true,
		"provider":       provider,
		"iat":            now.Unix(),
		"exp":            now.Add(45 * time.Second).Unix(),
		"jti":            tiJTI(),
	}
	if name != "" {
		claims["name"] = name
	}
	key, err := w.door.key(taSigningKey)
	if err != nil {
		return "", err
	}
	return tiSignES256(key, taSigningKey, claims)
}

func (w *taWorld) post(path, body string) (*taAnswer, error) {
	if w.server == nil {
		return nil, fmt.Errorf("the instance has not started")
	}
	resp, err := http.Post(w.server.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	return readTaAnswer(resp)
}

func (w *taWorld) sendAssertion(token string) (*taAnswer, error) {
	answer, err := w.post(tiAssertPath, tiAssertionBody(token))
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	w.signIns = append(w.signIns, answer)
	w.mu.Unlock()
	return answer, nil
}

func (w *taWorld) present(email, name, provider, sub string) error {
	token, err := w.assertion(email, name, provider, sub)
	if err != nil {
		return err
	}
	_, err = w.sendAssertion(token)
	return err
}

func (w *taWorld) presentTwoTogether(email, provider, sub string) error {
	tokens := make([]string, 2)
	for i := range tokens {
		token, err := w.assertion(email, "", provider, sub)
		if err != nil {
			return err
		}
		tokens[i] = token
	}
	start := make(chan struct{})
	errs := make(chan error, len(tokens))
	var wg sync.WaitGroup
	for _, token := range tokens {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := w.sendAssertion(token)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func (w *taWorld) signedInEarlier(email, provider, sub string) error {
	if err := w.present(email, "", provider, sub); err != nil {
		return err
	}
	last := w.last()
	if err := last.succeeded(); err != nil {
		return fmt.Errorf("the earlier sign-in for %q did not succeed: %w", email, err)
	}
	if last.agentID == "" {
		return fmt.Errorf("the earlier sign-in for %q named no person: %s", email, last.body)
	}
	if prior, ok := w.earlier[email]; ok && prior != last.agentID {
		return fmt.Errorf("the earlier sign-ins for %q named two different people, %s and %s", email, prior, last.agentID)
	}
	w.earlier[email] = last.agentID
	w.lastEarlier = email
	return nil
}

func (w *taWorld) passwordSignIn(email, password string) error {
	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		return err
	}
	answer, err := w.post("/api/auth/password-login", string(body))
	if err != nil {
		return err
	}
	w.passwordSignIns = append(w.passwordSignIns, answer)
	return nil
}

// --- outcomes ---

func (w *taWorld) last() *taAnswer {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.signIns) == 0 {
		return nil
	}
	return w.signIns[len(w.signIns)-1]
}

func (w *taWorld) lastTwo() ([]*taAnswer, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.signIns) < 2 {
		return nil, fmt.Errorf("expected two sign-ins, saw %d", len(w.signIns))
	}
	return w.signIns[len(w.signIns)-2:], nil
}

func (w *taWorld) bothSucceed() error {
	pair, err := w.lastTwo()
	if err != nil {
		return err
	}
	for _, a := range pair {
		if err := a.succeeded(); err != nil {
			return err
		}
	}
	return nil
}

func (w *taWorld) bothNameSamePerson() error {
	pair, err := w.lastTwo()
	if err != nil {
		return err
	}
	if pair[0].agentID == "" || pair[0].agentID != pair[1].agentID {
		return fmt.Errorf("the two sign-ins name %q and %q", pair[0].agentID, pair[1].agentID)
	}
	return nil
}

func (w *taWorld) reportsNewAccount(want bool) error {
	last := w.last()
	if err := last.succeeded(); err != nil {
		return err
	}
	if last.newAccount == nil {
		return fmt.Errorf("the answer does not say whether the sign-in created the person: %s", last.body)
	}
	if *last.newAccount != want {
		return fmt.Errorf("new_account = %v, want %v: %s", *last.newAccount, want, last.body)
	}
	return nil
}

// storeHoldsOneAccount requires exactly one person to hold a credential for the
// email, and that person to belong to exactly one account.
func (w *taWorld) storeHoldsOneAccount(email string) error {
	ctx := context.Background()
	creds, err := w.credRepo.FindByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("could not look up %q: %w", email, err)
	}
	people := map[string]bool{}
	for _, c := range creds {
		people[c.AgentID()] = true
	}
	if len(people) != 1 {
		return fmt.Errorf("the store holds %d people for %q, want 1", len(people), email)
	}
	for agentID := range people {
		accounts, err := w.accountRepo.FindByMember(ctx, agentID)
		if err != nil {
			return fmt.Errorf("could not read the accounts of %q: %w", email, err)
		}
		if len(accounts) != 1 {
			return fmt.Errorf("the person for %q belongs to %d accounts, want 1", email, len(accounts))
		}
	}
	return nil
}

// actingAccount reads the account the last sign-in's session acts in back from
// the store, through the session cookie it set, rather than from the answer.
func (w *taWorld) actingAccount() (string, error) {
	last := w.last()
	if err := last.succeeded(); err != nil {
		return "", err
	}
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	for _, c := range last.cookies {
		req.AddCookie(c)
	}
	data, err := w.sessionManager.GetHTTPSession(req)
	if err != nil || data == nil {
		return "", fmt.Errorf("the cookies the sign-in set carry no session: %v", err)
	}
	info, err := w.authService.ValidateSession(context.Background(), data.SessionID)
	if err != nil || info == nil {
		return "", fmt.Errorf("the sign-in's session does not validate: %v", err)
	}
	return info.AccountID, nil
}

func (w *taWorld) actingAccountIsReported() error {
	last := w.last()
	if err := last.succeeded(); err != nil {
		return err
	}
	if last.accountID == "" {
		return fmt.Errorf("the sign-in reported no account: %s", last.body)
	}
	got, err := w.actingAccount()
	if err != nil {
		return err
	}
	if got != last.accountID {
		return fmt.Errorf("requests act in account %q, the sign-in reported %q", got, last.accountID)
	}
	return nil
}

func (w *taWorld) actingAccountIsNamed(name string) error {
	want, ok := w.accounts[name]
	if !ok {
		return fmt.Errorf("no account named %q has been staged", name)
	}
	got, err := w.actingAccount()
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("requests act in account %q, want %q (%s)", got, want, name)
	}
	return nil
}

func (w *taWorld) personIsEarlier(email string, same bool) error {
	last := w.last()
	if err := last.succeeded(); err != nil {
		return err
	}
	earlier, ok := w.earlier[email]
	if !ok {
		return fmt.Errorf("%q has not signed in earlier", email)
	}
	if last.agentID == "" {
		return fmt.Errorf("the sign-in names no person: %s", last.body)
	}
	if same && last.agentID != earlier {
		return fmt.Errorf("the sign-in names %q, %q signed in earlier as %q", last.agentID, email, earlier)
	}
	if !same && last.agentID == earlier {
		return fmt.Errorf("the sign-in names %q, the person %q signed in as earlier", last.agentID, email)
	}
	return nil
}

var taQuoted = regexp.MustCompile(`"([^"]*)"`)

func (w *taWorld) answerCarriesFields(table *godog.Table) error {
	last := w.last()
	if err := last.succeeded(); err != nil {
		return err
	}
	if len(table.Rows) < 2 {
		return fmt.Errorf("the table names no fields")
	}
	want := map[string]string{}
	for _, row := range table.Rows[1:] {
		want[row.Cells[0].Value] = row.Cells[1].Value
	}
	got := make([]string, 0, len(last.fields))
	for key := range last.fields {
		got = append(got, key)
	}
	sort.Strings(got)
	if len(got) != len(want) {
		return fmt.Errorf("the answer carries %v, want exactly the %d fields the table names", got, len(want))
	}
	actingIn, err := w.actingAccount()
	if err != nil {
		return err
	}
	for field, holds := range want {
		if _, ok := last.fields[field]; !ok {
			return fmt.Errorf("the answer carries %v, and not %q", got, field)
		}
		if err := w.fieldHolds(last, field, holds, actingIn); err != nil {
			return err
		}
	}
	return nil
}

func (w *taWorld) fieldHolds(a *taAnswer, field, holds, actingIn string) error {
	switch field {
	case "agent":
		quoted := taQuoted.FindAllStringSubmatch(holds, -1)
		if len(quoted) != 2 {
			return fmt.Errorf("cannot read a name and an email out of %q", holds)
		}
		if a.agentID == "" || a.agentName != quoted[0][1] || a.agentEmail != quoted[1][1] {
			return fmt.Errorf("agent = {id %q, name %q, email %q}, want an id, %q and %q",
				a.agentID, a.agentName, a.agentEmail, quoted[0][1], quoted[1][1])
		}
	case "account":
		if a.accountID == "" || a.accountName == "" {
			return fmt.Errorf("account = {id %q, name %q}, want both", a.accountID, a.accountName)
		}
		if a.accountID != actingIn {
			return fmt.Errorf("the answer names account %q, the session acts in %q", a.accountID, actingIn)
		}
	case "token":
		if a.token == "" {
			return fmt.Errorf("the answer carries no token")
		}
		if !tokenNames(a.token, a.accountID) {
			return fmt.Errorf("the token does not name the account %q", a.accountID)
		}
	case "expires_at":
		at, err := time.Parse(time.RFC3339Nano, a.expiresAt)
		if err != nil || !at.After(time.Now()) {
			return fmt.Errorf("expires_at = %q, want a moment still to come", a.expiresAt)
		}
	case "new_account":
		if a.newAccount == nil || fmt.Sprint(*a.newAccount) != strings.TrimSpace(holds) {
			return fmt.Errorf("new_account = %s, want %s", a.fields["new_account"], holds)
		}
	default:
		return fmt.Errorf("the table names a field %q these steps do not know how to judge", field)
	}
	return nil
}

// tokenNames reports whether a JWT's claims carry the account id as one of
// their values. The signature is the issuer's business; this only reads what
// the token says.
func tokenNames(token, accountID string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || accountID == "" {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil {
		return false
	}
	for _, v := range claims {
		if s, ok := v.(string); ok && s == accountID {
			return true
		}
	}
	return false
}

func (w *taWorld) tokenCookieHoldsToken() error {
	last := w.last()
	if err := last.succeeded(); err != nil {
		return err
	}
	c := last.cookie(taTokenCookie)
	if c == nil || c.Value == "" || c.Value != last.token {
		return fmt.Errorf("the token cookie %v does not hold the token the answer carries", c)
	}
	return nil
}

func (w *taWorld) sameCookies() error {
	if len(w.passwordSignIns) == 0 {
		return fmt.Errorf("nobody has signed in with a password")
	}
	password, door := w.passwordSignIns[len(w.passwordSignIns)-1], w.last()
	if err := password.succeeded(); err != nil {
		return fmt.Errorf("the password sign-in: %w", err)
	}
	if err := door.succeeded(); err != nil {
		return fmt.Errorf("the sign-in through the door: %w", err)
	}
	if len(password.cookies) != len(door.cookies) {
		return fmt.Errorf("the password sign-in set %d cookies, the door's %d", len(password.cookies), len(door.cookies))
	}
	for _, p := range password.cookies {
		d := door.cookie(p.Name)
		if d == nil {
			return fmt.Errorf("the door's sign-in sets no %q cookie", p.Name)
		}
		if p.Path != d.Path || p.Domain != d.Domain || p.MaxAge != d.MaxAge ||
			p.HttpOnly != d.HttpOnly || p.Secure != d.Secure || p.SameSite != d.SameSite {
			return fmt.Errorf("cookie %q differs: password %+v, door %+v", p.Name, p, d)
		}
		if p.MaxAge == 0 && (p.Expires.IsZero() != d.Expires.IsZero() || absDuration(p.Expires.Sub(d.Expires)) > 5*time.Second) {
			return fmt.Errorf("cookie %q lives until %v on a password sign-in, %v through the door", p.Name, p.Expires, d.Expires)
		}
	}
	return nil
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func (w *taWorld) answerNamesPerson(name string) error {
	last := w.last()
	if err := last.succeeded(); err != nil {
		return err
	}
	if last.agentName != name {
		return fmt.Errorf("the answer names the person %q, want %q", last.agentName, name)
	}
	return nil
}

func (w *taWorld) reportsNoAccount() error {
	last := w.last()
	if err := last.succeeded(); err != nil {
		return err
	}
	if last.accountID != "" {
		return fmt.Errorf("the sign-in reports account %q, but the person has none left to act in", last.accountID)
	}
	return nil
}

func (w *taWorld) handsBackNoToken() error {
	if last := w.last(); last == nil || last.token != "" {
		return fmt.Errorf("the sign-in handed back a token for a person with no account to act in")
	}
	return nil
}

func (w *taWorld) noTokenCookie() error {
	last := w.last()
	if last == nil {
		return fmt.Errorf("no sign-in has been attempted")
	}
	if c := last.cookie(taTokenCookie); c != nil && c.Value != "" {
		return fmt.Errorf("a token cookie was set for a person with no account to act in")
	}
	return nil
}
