package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/api/handlers"
	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"
	mcpserver "github.com/wepala/weos/v3/internal/mcp"
	weosoauth "github.com/wepala/weos/v3/internal/oauth"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authhttp "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/http"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"github.com/cucumber/godog"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
	gormlib "gorm.io/gorm"
)

// TestOAuthAuthorizeSession runs the acceptance scenarios for story #140 (epic
// #135): a connector authorizes against whatever sign-in the instance has,
// rather than always against Google.
//
// Everything here is driven over real HTTP against a real boot, because the
// story is about redirects, cookies and a token that has to work afterwards —
// none of which survive being asserted at the function level. The Google
// scenarios stop at the redirect to accounts.google.com: the provider's
// endpoints are compiled in rather than configured, so no test can follow the
// person through Google and back. What matters for the regression is that the
// hand-off still happens and that the new path does not route around the
// allowlist, and both are reachable here.
func TestOAuthAuthorizeSession(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "oauth-authorize-session",
		ScenarioInitializer: initOAuthAuthorizeScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/oauth_authorize_session.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("oauth authorize session acceptance scenarios failed")
	}
}

const claudeRedirectURI = "https://claude.ai/api/mcp/auth_callback"

// The proof key the scenarios use. Fixed rather than generated so a failure is
// reproducible, and so "a proof key of their own" is visibly a different one.
const (
	claudeVerifier      = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	someoneElseVerifier = "M25iVq8Q9zGRTF4pQhx3Xk7pC2mWvJdT8sYbN5aLcRe"
)

type authorizeAnswer struct {
	status   int
	location string
	body     string
}

// redirectedTo reports where the person was sent, parsed, or nil if nowhere.
func (a authorizeAnswer) redirectedTo() *url.URL {
	if a.location == "" {
		return nil
	}
	u, err := url.Parse(a.location)
	if err != nil {
		return nil
	}
	return u
}

type oauthWorld struct {
	app    *fx.App
	tmpDir string
	server *httptest.Server
	client *http.Client

	authService     authapp.AuthenticationService
	sessionManager  session.SessionManager
	credRepo        authrepos.CredentialRepository
	agentRepo       authrepos.AgentRepository
	accountRepo     authrepos.AccountRepository
	resourceService application.ResourceService
	logger          entities.Logger

	clientID    string
	redirectURI string

	last        authorizeAnswer
	registered  authorizeAnswer
	discovery   map[string]any
	code        string
	tokens      []map[string]any
	tokenStatus []int

	// secondClient is the other person's Claude in the shared-identity scenario.
	secondToken string

	mcpSessionID      string
	mcpInitializedFor string
}

func initOAuthAuthorizeScenario(sc *godog.ScenarioContext) {
	w := &oauthWorld{}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	// --- instance shapes ---
	sc.Step(`^a demo instance where password sign-in is enabled, no Google provider is configured, and dynamic client registration is (on|off)$`,
		func(reg string) error { return w.boot(bootOpts{password: true, dynamicRegistration: reg == "on"}) })
	sc.Step(`^a demo instance where password sign-in is enabled and no Google provider is configured$`,
		func() error { return w.boot(bootOpts{password: true, dynamicRegistration: true}) })
	sc.Step(`^an instance where Google sign-in is configured and the allowlist names "([^"]*)"$`,
		func(email string) error {
			return w.boot(bootOpts{google: true, allowlist: email, dynamicRegistration: true})
		})
	sc.Step(`^password sign-in is also enabled on that instance$`, w.alsoEnablePasswordSignIn)

	// --- accounts ---
	sc.Step(`^the bootstrap account "([^"]*)" with password "([^"]*)"$`, w.anAccount)
	sc.Step(`^a second account "([^"]*)" with password "([^"]*)"$`, w.anAccount)
	sc.Step(`^the account "([^"]*)" with password "([^"]*)", whom the allowlist does not name$`, w.anAccount)

	// --- connector registration ---
	sc.Step(`^Claude is registered as a connector on that instance$`, w.claudeIsRegistered)
	sc.Step(`^Claude registers itself as a connector with the redirect URI "([^"]*)"$`, w.claudeRegisters)
	sc.Step(`^a connector asks the instance how to authorize against it$`, w.fetchDiscovery)

	// --- who the person is ---
	sc.Step(`^the person adding the connector is signed in as "([^"]*)"$`, w.signedInAs)
	sc.Step(`^the person adding the connector is not signed in$`, func() error { return nil })
	sc.Step(`^the person adding the connector signed in as "([^"]*)" and their session has since expired$`, w.signedInWithExpiredSession)
	sc.Step(`^the person adding the connector signed in as "([^"]*)" and their session names no account$`, w.signedInWithUnscopedSession)
	sc.Step(`^the person adding the connector signed in as "([^"]*)" and then signed out, keeping the cookie they were left with$`, w.signedInThenSignedOut)
	sc.Step(`^the person adding the connector carries a session cookie this instance cannot read$`, w.carriesUnreadableCookie)

	// --- authorizing ---
	sc.Step(`^Claude asks the instance to authorize the connector$`, func() error { return w.authorize(nil) })
	sc.Step(`^Claude has asked the instance to authorize the connector$`, func() error { return w.authorize(nil) })
	sc.Step(`^Claude asks the instance to authorize the connector (.+)$`, w.authorizeWithFlaw)
	sc.Step(`^Claude asks the instance to authorize the connector, naming a redirect URI it never registered$`,
		func() error { return w.authorize(map[string]string{"redirect_uri": "https://example.invalid/stolen"}) })
	sc.Step(`^a connector the instance has never registered asks it to authorize$`,
		func() error { return w.authorize(map[string]string{"client_id": "never-registered"}) })
	sc.Step(`^the person signs in as "([^"]*)" and follows the way back they were given$`, w.signInThenFollowReturnTo)

	// --- exchanging ---
	sc.Step(`^Claude exchanges the authorization code it received, presenting the proof key it started with$`,
		func() error { return w.exchange(claudeVerifier) })
	sc.Step(`^Claude exchanges the same authorization code a second time$`, func() error { return w.exchange(claudeVerifier) })
	sc.Step(`^someone else exchanges the authorization code presenting a proof key of their own$`,
		func() error { return w.exchange(someoneElseVerifier) })

	// --- using the connector ---
	sc.Step(`^Claude has been authorized as a connector by "([^"]*)" signing in$`, w.authorizedBySigningIn)
	sc.Step(`^two people have each authorized their own Claude by signing in as "([^"]*)"$`, w.twoPeopleAuthorized)
	sc.Step(`^Claude asks the instance which tools it offers$`, w.listTools)
	sc.Step(`^Claude creates the task "([^"]*)" through the instance's tools$`, w.createTask)
	sc.Step(`^the first person's Claude creates the task "([^"]*)" through the instance's tools$`, w.createTask)

	registerOAuthAssertions(sc, w)
}

type bootOpts struct {
	password            bool
	google              bool
	allowlist           string
	dynamicRegistration bool
}

// boot brings up an instance in the shape the scenario describes and mounts the
// routes serve.go mounts for it.
//
// The mounting is hand-copied from serve.go, which is a known hazard — weos#467
// tracks extracting the real route table so tests can call it. Until then the
// pieces that decide this story's behavior are taken from the same constructors
// serve.go uses, and the one thing that must not drift (whether the OAuth block
// is mounted at all) is derived from the same cfg.AuthEnabled() call.
func (w *oauthWorld) boot(opts bootOpts) error {
	w.teardown()
	dir, err := os.MkdirTemp("", "weos-oauth-authorize-e2e-")
	if err != nil {
		return err
	}
	w.tmpDir = dir

	cfg := config.Default()
	cfg.DatabaseDSN = filepath.Join(dir, "test.db")
	cfg.LogLevel = "error"
	// Left at the shipped default on purpose. That default is what makes the
	// session cookie non-Secure (see serve.go's secureCookies), and this server
	// speaks plain HTTP — with a "real" secret the cookie is marked Secure, the
	// client drops it, and every scenario about a signed-in person quietly
	// becomes a scenario about an anonymous one.
	cfg.PasswordAuthEnabled = opts.password
	if opts.google {
		cfg.OAuth.GoogleClientID = "acceptance-client-id.apps.googleusercontent.com"
		cfg.OAuth.GoogleClientSecret = "acceptance-client-secret"
	}
	if opts.allowlist != "" {
		cfg.OAuth.AllowedEmails = []string{opts.allowlist}
	}
	cfg.OAuth.DynamicRegistration = opts.dynamicRegistration
	cfg.OAuth.BaseURL = ""

	var resourceTypeService application.ResourceTypeService
	var kgService application.KnowledgeGraphService
	var lexicalSearch application.LexicalSearch
	var episodicRecall application.EpisodicRecall
	var jwtService authapp.JWTService
	var sessionStore sessions.Store
	var db *gormlib.DB

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Provide(weosoauth.ProvideJWTService),
		fx.Populate(&w.authService, &w.credRepo, &w.agentRepo, &w.accountRepo),
		fx.Populate(&w.sessionManager, &sessionStore, &w.logger, &jwtService),
		fx.Populate(&resourceTypeService, &w.resourceService),
		fx.Populate(&kgService, &lexicalSearch, &episodicRecall),
		fx.Populate(&db),
	)
	startCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start the instance: %w", err)
	}
	w.app = app

	if _, err := resourceTypeService.InstallPreset(context.Background(), "tasks", true); err != nil {
		return fmt.Errorf("failed to install the tasks preset: %w", err)
	}

	e := echo.New()
	e.HideBanner = true
	api := e.Group("/api")
	api.Use(apimw.Messages())

	handlers.MountPasswordAuth(api, handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
		AuthService:    w.authService,
		SessionManager: w.sessionManager,
		SecureCookies:  false,
		Logger:         w.logger,
	}), handlers.PasswordAuthRoutes{SignIn: cfg.PasswordAuthEnabled, Registration: false})

	// The OAuth 2.1 surface, gated exactly as serve.go gates it.
	if cfg.AuthEnabled() {
		clientRepo := weosoauth.NewClientRepository(db)
		codeRepo := weosoauth.NewAuthCodeRepository(db)
		refreshRepo := weosoauth.NewRefreshTokenRepository(db)
		baseURL := "http://" + "127.0.0.1"

		asHandler := weosoauth.AuthorizationServerMetadata(baseURL, cfg.OAuth.DynamicRegistration)
		regHandler := weosoauth.RegisterClient(clientRepo, cfg.OAuth.DynamicRegistration)
		authzHandler := weosoauth.Authorize(w.authService, w.sessionManager, sessionStore,
			clientRepo, codeRepo, w.credRepo, w.logger, baseURL,
			cfg.OAuthEnabled(), cfg.OAuth.AllowedEmails)
		tokHandler := weosoauth.Token(jwtService, codeRepo, refreshRepo, w.agentRepo, w.accountRepo, w.logger)

		e.Pre(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				p := c.Request().URL.Path
				m := c.Request().Method
				switch {
				case m == http.MethodGet && p == "/.well-known/oauth-authorization-server":
					return asHandler(c)
				case m == http.MethodPost && p == "/oauth/register":
					return regHandler(c)
				case m == http.MethodGet && p == "/oauth/authorize":
					return authzHandler(c)
				case m == http.MethodPost && p == "/oauth/token":
					return tokHandler(c)
				default:
					return next(c)
				}
			}
		})

		// The MCP surface a connector uses afterwards, behind the same bearer
		// auth serve.go puts in front of it.
		mcpSrv, mcpErr := mcpserver.NewConfiguredServer(
			resourceTypeService, w.resourceService, kgService, lexicalSearch, episodicRecall,
			nil, ungatedForOAuthSuite, slog.Default())
		if mcpErr != nil {
			return fmt.Errorf("failed to create the MCP server: %w", mcpErr)
		}
		mcpHandler := mcpserver.HandlerForServer(mcpSrv, slog.Default())
		mcpGroup := api.Group("")
		sessionAuth := authhttp.RequireAuth(w.sessionManager, w.authService)
		mcpGroup.Use(apimw.BearerOrSession(jwtService, sessionAuth, baseURL))
		mcpGroup.Any("/mcp", echo.WrapHandler(mcpHandler))
		mcpGroup.Any("/mcp/*", echo.WrapHandler(mcpHandler))
	}

	w.server = httptest.NewServer(e)
	jar, _ := cookiejar.New(nil)
	// No redirects followed: where the person is sent IS the assertion.
	w.client = &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	w.redirectURI = claudeRedirectURI
	w.tokens = nil
	w.tokenStatus = nil
	w.code = ""
	return nil
}

// alsoEnablePasswordSignIn rebuilds the Google instance with password sign-in
// added, which is the shape the "not opened up by another way in" scenario needs.
func (w *oauthWorld) alsoEnablePasswordSignIn() error {
	return w.boot(bootOpts{google: true, password: true,
		allowlist: "ops@harborlegal.example", dynamicRegistration: true})
}

func (w *oauthWorld) teardown() {
	if w.server != nil {
		w.server.Close()
		w.server = nil
	}
	if w.app != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_ = w.app.Stop(stopCtx)
		w.app = nil
	}
	if w.tmpDir != "" {
		_ = os.RemoveAll(w.tmpDir)
		w.tmpDir = ""
	}
}

func (w *oauthWorld) anAccount(email, password string) error {
	local, _, _ := strings.Cut(email, "@")
	_, _, _, err := w.authService.RegisterPassword(context.Background(), email, local, password)
	if err != nil {
		return fmt.Errorf("could not create %q: %w", email, err)
	}
	return nil
}

// --- registration ---

func (w *oauthWorld) claudeRegisters(redirectURI string) error {
	body, _ := json.Marshal(map[string]any{
		"redirect_uris": []string{redirectURI}, "client_name": "Claude",
	})
	res, err := w.client.Post(w.server.URL+"/oauth/register", "application/json", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	w.registered = authorizeAnswer{status: res.StatusCode, body: string(raw)}
	var payload struct {
		ClientID     string   `json:"client_id"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if json.Unmarshal(raw, &payload) == nil && payload.ClientID != "" {
		w.clientID = payload.ClientID
		w.redirectURI = redirectURI
	}
	return nil
}

func (w *oauthWorld) claudeIsRegistered() error {
	if err := w.claudeRegisters(claudeRedirectURI); err != nil {
		return err
	}
	if w.clientID == "" {
		return fmt.Errorf("the connector could not register: %s", w.registered.body)
	}
	return nil
}

func (w *oauthWorld) fetchDiscovery() error {
	res, err := w.client.Get(w.server.URL + "/.well-known/oauth-authorization-server")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	w.last = authorizeAnswer{status: res.StatusCode, body: string(raw)}
	w.discovery = map[string]any{}
	_ = json.Unmarshal(raw, &w.discovery)
	return nil
}

// --- sessions ---

// signedInAs gives the person a live session for that account.
//
// Deliberately not routed through the password endpoint. How someone signed in
// is not what this story is about, and half these scenarios run on a
// Google-configured instance where no password route exists — staging it
// through the store means one step works on every instance shape, and the
// account is created here if the scenario never named it.
func (w *oauthWorld) signedInAs(email string) error {
	ctx := context.Background()
	creds, err := w.credRepo.FindByEmail(ctx, strings.ToLower(email))
	if err != nil {
		return fmt.Errorf("could not look up %q: %w", email, err)
	}
	if len(creds) == 0 {
		if err := w.anAccount(email, passwordFor(email)); err != nil {
			return err
		}
		creds, err = w.credRepo.FindByEmail(ctx, strings.ToLower(email))
		if err != nil || len(creds) == 0 {
			return fmt.Errorf("could not create a session identity for %q", email)
		}
	}
	// The account has to be known before the session is made, not after: a
	// session now carries the account it acts in, and one made without it is
	// refused on sight. This helper stages a session rather than signing in,
	// so it looks the membership up — and it deliberately does not vouch for
	// it, leaving pericarp to verify the agent really is a member.
	accountID := ""
	if accounts, lookupErr := w.accountRepo.FindByMember(ctx, creds[0].AgentID()); lookupErr == nil && len(accounts) > 0 {
		accountID = accounts[0].GetID()
	}
	authSession, err := w.authService.CreateSession(ctx, creds[0].AgentID(), accountID, creds[0].GetID(),
		"127.0.0.1", "acceptance", time.Hour)
	if err != nil {
		return fmt.Errorf("could not create a session for %q: %w", email, err)
	}
	return w.setSessionCookie(session.SessionData{
		SessionID: authSession.GetID(),
		AgentID:   creds[0].AgentID(),
		AccountID: accountID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	})
}

// signedInWithUnscopedSession builds a live, valid session that names no
// account — the shape every row written before sessions carried one still has.
//
// It is worth being explicit about why this is not simply refused upstream:
// ValidateSession only runs its membership and deactivation checks when the
// session names an account, so an unscoped session validates cleanly and
// reaches the authorize handler looking healthy. RequireAuth refuses it on the
// ordinary API; this rail has to take the same stance itself.
func (w *oauthWorld) signedInWithUnscopedSession(email string) error {
	ctx := context.Background()
	creds, err := w.credRepo.FindByEmail(ctx, strings.ToLower(email))
	if err != nil || len(creds) == 0 {
		return fmt.Errorf("no account for %q to build an unscoped session from", email)
	}
	authSession, err := w.authService.CreateSession(ctx, creds[0].AgentID(), "", creds[0].GetID(),
		"127.0.0.1", "acceptance", time.Hour)
	if err != nil {
		return fmt.Errorf("could not create an unscoped session: %w", err)
	}
	return w.setSessionCookie(session.SessionData{
		SessionID: authSession.GetID(),
		AgentID:   creds[0].AgentID(),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	})
}

// signedInWithExpiredSession builds the cookie a person would still be holding
// after their session lapsed: a real session for a real agent, created already
// expired, so the cookie decodes and the store refuses it.
func (w *oauthWorld) signedInWithExpiredSession(email string) error {
	ctx := context.Background()
	creds, err := w.credRepo.FindByEmail(ctx, strings.ToLower(email))
	if err != nil || len(creds) == 0 {
		return fmt.Errorf("no account for %q to expire", email)
	}
	// Scoped like any other session, so that expiry is the only thing wrong
	// with it. An unscoped session is refused for its own separate reason,
	// which would make this scenario pass for the wrong cause.
	accountID := ""
	if accounts, lookupErr := w.accountRepo.FindByMember(ctx, creds[0].AgentID()); lookupErr == nil && len(accounts) > 0 {
		accountID = accounts[0].GetID()
	}
	authSession, err := w.authService.CreateSession(ctx, creds[0].AgentID(), accountID, creds[0].GetID(),
		"127.0.0.1", "acceptance", -time.Hour)
	if err != nil {
		return fmt.Errorf("could not create an expired session: %w", err)
	}
	return w.setSessionCookie(session.SessionData{
		SessionID: authSession.GetID(),
		AgentID:   creds[0].AgentID(),
		CreatedAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-time.Hour),
	})
}

// signedInThenSignedOut revokes the session server-side while the browser keeps
// the cookie, which is what signing out on another device looks like here.
func (w *oauthWorld) signedInThenSignedOut(email string) error {
	if err := w.signedInAs(email); err != nil {
		return err
	}
	data, err := w.sessionDataFromJar()
	if err != nil {
		return err
	}
	if err := w.authService.RevokeSession(context.Background(), data.SessionID); err != nil {
		return fmt.Errorf("could not revoke the session: %w", err)
	}
	return nil
}

func (w *oauthWorld) carriesUnreadableCookie() error {
	u, _ := url.Parse(w.server.URL)
	w.client.Jar.SetCookies(u, []*http.Cookie{{
		Name: "pericarp_session", Value: "not-a-session-this-instance-signed", Path: "/",
	}})
	return nil
}

// setSessionCookie writes a session cookie by round-tripping through the real
// session manager, so the cookie is signed the way the instance signs them.
func (w *oauthWorld) setSessionCookie(data session.SessionData) error {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, w.server.URL, nil)
	if err := w.sessionManager.CreateHTTPSession(rec, req, data); err != nil {
		return fmt.Errorf("could not build a session cookie: %w", err)
	}
	u, _ := url.Parse(w.server.URL)
	w.client.Jar.SetCookies(u, rec.Result().Cookies())
	return nil
}

func (w *oauthWorld) sessionDataFromJar() (*session.SessionData, error) {
	u, _ := url.Parse(w.server.URL)
	req := httptest.NewRequest(http.MethodGet, w.server.URL, nil)
	for _, ck := range w.client.Jar.Cookies(u) {
		req.AddCookie(ck)
	}
	data, err := w.sessionManager.GetHTTPSession(req)
	if err != nil {
		return nil, fmt.Errorf("no readable session in the jar: %w", err)
	}
	return data, nil
}

// --- authorizing ---

func challengeFor(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (w *oauthWorld) authorizeURL(overrides map[string]string) string {
	q := url.Values{}
	q.Set("client_id", w.clientID)
	q.Set("redirect_uri", w.redirectURI)
	q.Set("response_type", "code")
	q.Set("code_challenge", challengeFor(claudeVerifier))
	q.Set("code_challenge_method", "S256")
	q.Set("state", "st-140")
	q.Set("scope", "mcp:read mcp:write")
	for k, v := range overrides {
		if v == "" {
			q.Del(k)
			continue
		}
		q.Set(k, v)
	}
	return w.server.URL + "/oauth/authorize?" + q.Encode()
}

func (w *oauthWorld) authorize(overrides map[string]string) error {
	res, err := w.client.Get(w.authorizeURL(overrides))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	w.last = authorizeAnswer{
		status:   res.StatusCode,
		location: res.Header.Get("Location"),
		body:     string(raw),
	}
	if to := w.last.redirectedTo(); to != nil {
		if code := to.Query().Get("code"); code != "" {
			w.code = code
		}
	}
	return nil
}

// authorizeWithFlaw maps the outline's plain-English flaw onto the parameter it
// spoils, so the feature never has to name a query parameter.
func (w *oauthWorld) authorizeWithFlaw(flaw string) error {
	switch strings.TrimSpace(flaw) {
	case "asking for a token rather than a code":
		return w.authorize(map[string]string{"response_type": "token"})
	case "with no proof key at all":
		return w.authorize(map[string]string{"code_challenge": "", "code_challenge_method": ""})
	case "with an unhashed proof key":
		return w.authorize(map[string]string{"code_challenge_method": "plain"})
	case "asking for a scope this instance never issues":
		return w.authorize(map[string]string{"scope": "mcp:everything"})
	default:
		return fmt.Errorf("unknown flaw %q — add it to authorizeWithFlaw", flaw)
	}
}

// signInThenFollowReturnTo does what the person does: signs in on the page they
// were sent to, then follows the way back the instance handed them.
func (w *oauthWorld) signInThenFollowReturnTo(email string) error {
	to := w.last.redirectedTo()
	if to == nil {
		return fmt.Errorf("the person was not sent anywhere to sign in")
	}
	returnTo := to.Query().Get("return_to")
	if returnTo == "" {
		return fmt.Errorf("no way back was given: %s", w.last.location)
	}
	if err := w.signedInAs(email); err != nil {
		return err
	}
	res, err := w.client.Get(w.server.URL + returnTo)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	w.last = authorizeAnswer{status: res.StatusCode, location: res.Header.Get("Location"), body: string(raw)}
	if t := w.last.redirectedTo(); t != nil {
		if code := t.Query().Get("code"); code != "" {
			w.code = code
		}
	}
	return nil
}

// --- exchanging ---

func (w *oauthWorld) exchange(verifier string) error {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", w.code)
	form.Set("redirect_uri", w.redirectURI)
	form.Set("client_id", w.clientID)
	form.Set("code_verifier", verifier)
	res, err := w.client.PostForm(w.server.URL+"/oauth/token", form)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	payload := map[string]any{}
	_ = json.Unmarshal(raw, &payload)
	w.tokens = append(w.tokens, payload)
	w.tokenStatus = append(w.tokenStatus, res.StatusCode)
	return nil
}

func (w *oauthWorld) accessToken() string {
	for i := len(w.tokens) - 1; i >= 0; i-- {
		if w.tokenStatus[i] == http.StatusOK {
			if tok, ok := w.tokens[i]["access_token"].(string); ok {
				return tok
			}
		}
	}
	return ""
}

// authorizedBySigningIn runs the whole flow a connector runs, ending with a
// usable token.
func (w *oauthWorld) authorizedBySigningIn(email string) error {
	if err := w.claudeIsRegistered(); err != nil {
		return err
	}
	if err := w.signedInAs(email); err != nil {
		return err
	}
	if err := w.authorize(nil); err != nil {
		return err
	}
	if w.code == "" {
		return fmt.Errorf("no authorization code was issued: %s %s", w.last.location, w.last.body)
	}
	return w.exchange(claudeVerifier)
}

func (w *oauthWorld) twoPeopleAuthorized(email string) error {
	if err := w.authorizedBySigningIn(email); err != nil {
		return err
	}
	first := w.accessToken()
	if first == "" {
		return fmt.Errorf("the first connector got no token")
	}
	// The second person is a different browser: a fresh jar, its own sign-in,
	// its own authorization, against the same account.
	jar, _ := cookiejar.New(nil)
	w.client.Jar = jar
	w.code = ""
	if err := w.signedInAs(email); err != nil {
		return err
	}
	if err := w.authorize(nil); err != nil {
		return err
	}
	if err := w.exchange(claudeVerifier); err != nil {
		return err
	}
	w.secondToken = w.accessToken()
	if w.secondToken == "" {
		return fmt.Errorf("the second connector got no token")
	}
	// Keep the first person's token as the one the "creates the task" step uses.
	w.tokens = append(w.tokens, map[string]any{"access_token": first})
	w.tokenStatus = append(w.tokenStatus, http.StatusOK)
	return nil
}

// --- the MCP surface ---

func (w *oauthWorld) mcpCall(token, method string, params any) (map[string]any, error) {
	payload := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		payload["params"] = params
	}
	return w.mcpSend(token, payload, true)
}

// mcpNotify sends a notification — no id, and no reply expected. The transport
// requires "initialized" before it will accept anything but the handshake.
func (w *oauthWorld) mcpNotify(token, method string) error {
	_, err := w.mcpSend(token, map[string]any{"jsonrpc": "2.0", "method": method}, false)
	return err
}

func (w *oauthWorld) mcpSend(token string, payload map[string]any, wantReply bool) (map[string]any, error) {
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, w.server.URL+"/api/mcp", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	// The streamable transport hands out a session id on initialize and expects
	// it back on every later call; without it each request looks like a brand
	// new, uninitialized session.
	if w.mcpSessionID != "" {
		req.Header.Set("Mcp-Session-Id", w.mcpSessionID)
	}
	res, err := w.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if id := res.Header.Get("Mcp-Session-Id"); id != "" {
		w.mcpSessionID = id
	}
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("MCP call %v: %d %s", payload["method"], res.StatusCode, raw)
	}
	if !wantReply {
		return nil, nil
	}
	reply, err := decodeMCPBody(raw)
	if err != nil {
		return nil, err
	}
	if errObj, ok := reply["error"]; ok {
		return nil, fmt.Errorf("MCP call %v failed: %v", payload["method"], errObj)
	}
	return reply, nil
}

// decodeMCPBody reads either a plain JSON-RPC reply or the SSE framing the
// streamable transport uses, so the step never has to care which it got.
func decodeMCPBody(raw []byte) (map[string]any, error) {
	text := string(raw)
	out := map[string]any{}
	if json.Unmarshal(raw, &out) == nil {
		return out, nil
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &out) == nil {
			return out, nil
		}
	}
	return nil, fmt.Errorf("could not read the MCP reply: %s", text)
}

// initializeMCP performs the handshake the transport requires before any other
// call is accepted.
func (w *oauthWorld) initializeMCP(token string) error {
	if w.mcpInitializedFor == token {
		return nil
	}
	w.mcpSessionID = ""
	if _, err := w.mcpCall(token, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "acceptance", "version": "1"},
	}); err != nil {
		return err
	}
	if err := w.mcpNotify(token, "notifications/initialized"); err != nil {
		return err
	}
	w.mcpInitializedFor = token
	return nil
}

func (w *oauthWorld) listTools() error {
	token := w.accessToken()
	if token == "" {
		return fmt.Errorf("the connector has no access token")
	}
	if err := w.initializeMCP(token); err != nil {
		return err
	}
	reply, err := w.mcpCall(token, "tools/list", map[string]any{})
	if err != nil {
		return err
	}
	w.last = authorizeAnswer{status: http.StatusOK, body: fmt.Sprintf("%v", reply)}
	return nil
}

func (w *oauthWorld) createTask(name string) error {
	token := w.accessToken()
	if token == "" {
		return fmt.Errorf("the connector has no access token")
	}
	if err := w.initializeMCP(token); err != nil {
		return err
	}
	args, _ := json.Marshal(map[string]any{"name": name, "status": "open", "priority": "medium"})
	reply, err := w.mcpCall(token, "tools/call", map[string]any{
		"name":      "resource_create",
		"arguments": map[string]any{"type_slug": "task", "data": json.RawMessage(args)},
	})
	if err != nil {
		return err
	}
	w.last = authorizeAnswer{status: http.StatusOK, body: fmt.Sprintf("%v", reply)}
	return nil
}

// passwordFor maps the accounts the feature names to the passwords it gave
// them, so a sign-in step doesn't have to repeat the password every time.
func passwordFor(email string) string {
	switch email {
	case "counsel@harborlegal.example":
		return "trellis-anchor-mango-9"
	default:
		return "correct-horse-battery-staple"
	}
}

// agentIDFor resolves the agent behind an email, so an identity assertion can
// compare what the token did against who it claims to be.
func (w *oauthWorld) agentIDFor(email string) (string, error) {
	creds, err := w.credRepo.FindByEmail(context.Background(), strings.ToLower(email))
	if err != nil {
		return "", fmt.Errorf("could not look up %q: %w", email, err)
	}
	if len(creds) == 0 {
		return "", fmt.Errorf("no account for %q", email)
	}
	return creds[0].AgentID(), nil
}

// createTaskWith creates a task as whoever the given token is.
func (w *oauthWorld) createTaskWith(token, name string) error {
	args, _ := json.Marshal(map[string]any{"name": name, "status": "open", "priority": "medium"})
	_, err := w.mcpCall(token, "tools/call", map[string]any{
		"name":      "resource_create",
		"arguments": map[string]any{"type_slug": "task", "data": json.RawMessage(args)},
	})
	return err
}

// lastTaskOwner reports which agent owns the named task, read from the store
// rather than from the reply — the question is what was persisted, and by whom.
func (w *oauthWorld) lastTaskOwner(name string) (string, error) {
	page, err := w.resourceService.List(context.Background(), "task", "", 200, repositories.SortOptions{})
	if err != nil {
		return "", fmt.Errorf("could not list tasks: %w", err)
	}
	for _, resource := range page.Data {
		var payload struct {
			Graph []struct {
				Name string `json:"name"`
			} `json:"@graph"`
		}
		if json.Unmarshal(resource.Data(), &payload) != nil {
			continue
		}
		for _, node := range payload.Graph {
			if node.Name == name {
				return resource.CreatedBy(), nil
			}
		}
	}
	return "", nil
}

// ungatedForOAuthSuite says out loud that this suite is about bearer auth, not
// about feature gating: every gated tool is available so the scenarios exercise
// the auth path alone. Stated rather than left as a nil gate, which
// NewConfiguredServer refuses.
func ungatedForOAuthSuite(context.Context, string) bool { return true }
