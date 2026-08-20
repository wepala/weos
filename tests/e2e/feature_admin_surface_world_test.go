package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
	"github.com/open-feature/go-sdk/openfeature"
	"go.uber.org/fx"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authcasbin "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/casbin"
	authhttp "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/http"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"

	"github.com/wepala/weos/v3/api/handlers"
	apimw "github.com/wepala/weos/v3/api/middleware"
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"
)

// TestFeatureAdminSurface runs the API half of story #486 (task #487).
//
//	GODOG_TAGS=@story-486 go test ./tests/e2e/ -run TestFeatureAdminSurface
func TestFeatureAdminSurface(t *testing.T) {
	tags := "~@wip"
	if v := os.Getenv("GODOG_TAGS"); v != "" {
		tags = v
	}
	suite := godog.TestSuite{
		ScenarioInitializer: initFeatureAdminSurfaceScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/feature_flag_admin_surface.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("admin feature surface acceptance scenarios failed")
	}
}

// apiCall is one request the harness made, kept so a later step can assert on
// it without repeating it.
type apiCall struct {
	route  string
	status int
	body   string
}

type adminWorld struct {
	app    *fx.App
	server *httptest.Server
	tmpDir string
	dsn    string
	boots  int

	registry *application.FeatureRegistry
	features *application.FeatureService
	admin    *application.FeatureAdminService
	resolver *application.FeatureResolver
	client   *openfeature.Client

	settings *toolsSettingsSpy
	grants   *toolsGrantSpy
	invSpy   *invalidatorSpy
	logs     *loggerSpy

	resources application.ResourceService
	rts       application.ResourceTypeService

	authService    authapp.AuthenticationService
	accountRepo    authrepos.AccountRepository
	credRepo       authrepos.CredentialRepository
	agentRepo      authrepos.AgentRepository
	sessionManager session.SessionManager
	sessionStore   sessions.Store
	authzChecker   *authcasbin.CasbinAuthorizationChecker
	logger         entities.Logger

	declared    []entities.FeatureMeta
	maxCacheAge time.Duration
	ledgerGate  string

	people        map[string]*featurePerson
	accounts      map[string]string
	signedInOrder []string

	// ledgerExports counts what the harness-mounted mutating route wrote, so a
	// refusal can be proved to have written nothing.
	ledgerExports int
	// agentTurns counts turns the gated agent route would have taken.
	agentTurns int

	answers  map[string][]entities.FeatureStatus
	messages map[string][]string
	calls    map[string]*apiCall

	restartBaseline int
	readsBefore     int
	readsAfterFirst int
	invsBefore      int

	straightAway *apiCall
	afterMoment  *apiCall
	momentSet    bool
	moment       time.Time
	windowStart  time.Time

	lastActor     string
	lastCallRoute string
	repeatCalls   []*apiCall
	repeatAnswers [][]entities.FeatureStatus
	// routeGates is this scenario's copy of the Background's route table.
	// Per-world on purpose: a scenario that rewrites a gate must not change
	// what the next scenario mounts.
	routeGates map[string]string
	jar        map[string][]*http.Cookie
}

func newAdminWorld() *adminWorld {
	return &adminWorld{
		ledgerGate: "ledger-export",
		people:     map[string]*featurePerson{},
		accounts:   map[string]string{},
		answers:    map[string][]entities.FeatureStatus{},
		messages:   map[string][]string{},
		calls:      map[string]*apiCall{},
		jar:        map[string][]*http.Cookie{},
		routeGates: defaultRouteGates(),
	}
}

func (w *adminWorld) teardown() {
	if w.server != nil {
		w.server.Close()
		w.server = nil
	}
	if w.app != nil {
		ctx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer cancel()
		_ = w.app.Stop(ctx)
		w.app = nil
	}
	if w.tmpDir != "" {
		_ = os.RemoveAll(w.tmpDir)
	}
}

func (w *adminWorld) boot() error {
	if w.app != nil {
		return nil
	}
	if w.tmpDir == "" {
		dir, err := os.MkdirTemp("", "weos-feature-admin-e2e-")
		if err != nil {
			return fmt.Errorf("could not create a temp dir: %w", err)
		}
		w.tmpDir = dir
		w.dsn = filepath.Join(dir, "admin.db")
	}
	pw := "true"
	os.Setenv("PASSWORD_AUTH_ENABLED", pw)
	os.Unsetenv("GOOGLE_CLIENT_ID")
	os.Unsetenv("GOOGLE_CLIENT_SECRET")

	cfg := config.Default()
	cfg.LoadFromEnvironment()
	cfg.DatabaseDSN = w.dsn
	cfg.LogLevel = "error"
	if w.maxCacheAge > 0 {
		cfg.Features.CacheMaxAge = w.maxCacheAge
	}
	cfg.Features.Declared = declarationsBeyondCore(w.declared)

	w.settings = &toolsSettingsSpy{}
	w.grants = &toolsGrantSpy{}
	w.invSpy = &invalidatorSpy{}
	w.logs = &loggerSpy{}

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Decorate(func(inner repositories.FeatureSettingsRepository) repositories.FeatureSettingsRepository {
			w.settings.inner = inner
			return w.settings
		}),
		fx.Decorate(func(inner repositories.FeatureGrantRepository) repositories.FeatureGrantRepository {
			w.grants.inner = inner
			return w.grants
		}),
		fx.Decorate(func(inner repositories.FeatureCacheInvalidator) repositories.FeatureCacheInvalidator {
			w.invSpy.inner = inner
			return w.invSpy
		}),
		fx.Decorate(func(inner entities.Logger) entities.Logger {
			w.logs.inner = inner
			return w.logs
		}),
		fx.Populate(&w.registry, &w.features, &w.admin, &w.resolver, &w.client),
		fx.Populate(&w.resources, &w.rts, &w.logger),
		fx.Populate(&w.authService, &w.accountRepo, &w.credRepo, &w.agentRepo),
		fx.Populate(&w.sessionManager, &w.sessionStore, &w.authzChecker),
	)
	startCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}
	w.app = app
	w.boots++
	w.restartBaseline = w.boots
	return w.mountAPI()
}

// mountAPI mirrors serve.go's group order deliberately, because one of this
// contract's scenarios is about that order.
//
// serve.go registers protected, then acceptGroup, then featuresGroup, then
// mcpGroup, and echo's Group.Use installs a not-found catch-all at the group
// prefix — so the LAST empty-prefix group under /api owns what an unmatched
// /api path answers. A harness that mounted these in a different order would
// prove the wrong thing about the one property this file was asked to pin.
//
// It is a replica, not the real table (#467 would give us the real one), so it
// is kept deliberately thin: only the routes the contract names, in the groups
// serve.go puts them in.
func (w *adminWorld) mountAPI() error {
	e := echo.New()
	e.HideBanner = true
	api := e.Group("/api")

	fh := handlers.NewFeatureHandler(handlers.FeatureHandlerConfig{Admin: w.admin, Logger: w.logger})
	personHandler := handlers.NewPersonHandler(w.resources)
	rtHandler := handlers.NewResourceTypeHandler(w.rts, w.authzChecker, w.accountRepo, w.logger)
	impersonation := handlers.NewImpersonationHandler(handlers.ImpersonationHandlerConfig{
		Store: w.sessionStore, AccountRepo: w.accountRepo,
		AgentRepo: w.agentRepo, CredRepo: w.credRepo, Logger: w.logger,
	})

	// 1. protected — everything that needs a session.
	protected := api.Group("")
	protected.Use(apimw.Messages())
	protected.Use(echo.WrapMiddleware(authhttp.RequireAuth(w.sessionManager, w.authService)))
	protected.Use(apimw.Impersonation(w.sessionStore, w.accountRepo, w.logger))
	protected.GET("/persons", personHandler.List)
	protected.GET("/resource-types", rtHandler.List)
	protected.PUT("/features/:key/instance", fh.SetInstance)
	protected.POST("/admin/impersonate", impersonation.Start)
	protected.POST("/admin/stop-impersonation", impersonation.Stop)

	// 2. featuresGroup — answers a caller with no session, and still honors
	//    impersonation.
	featuresGroup := api.Group("")
	featuresGroup.Use(apimw.Messages())
	featuresGroup.Use(echo.WrapMiddleware(apimw.OptionalAuth(w.sessionManager, w.authService)))
	featuresGroup.Use(apimw.Impersonation(w.sessionStore, w.accountRepo, w.logger))
	featuresGroup.GET("/features", fh.List)

	// 3. mcpGroup — LAST, so it keeps ownership of unmatched /api paths.
	gate := apimw.RequireFeature(application.ToolFeatureGate(w.client), application.FeatureAgentChat)
	mcpGroup := api.Group("")
	mcpGroup.Use(apimw.Messages())
	mcpGroup.Use(echo.WrapMiddleware(authhttp.RequireAuth(w.sessionManager, w.authService)))
	mcpGroup.POST("/agent/conversations/:id/messages", w.agentTurnHandler, gate)
	mcpGroup.GET("/agent/conversations/:id", w.agentHistoryHandler, gate)
	// The harness's own mutating route, mounted through a group the way a
	// preset's handlers are, gated on a feature that is declared off — so a
	// refused call can be proved to have written nothing.
	mcpGroup.POST("/ledger/exports", w.ledgerExportHandler,
		apimw.RequireFeature(application.ToolFeatureGate(w.client),
			w.routeGates["POST /api/ledger/exports"]))

	w.server = httptest.NewServer(e)
	return nil
}

func (w *adminWorld) agentTurnHandler(c echo.Context) error {
	w.agentTurns++
	return c.JSON(http.StatusOK, map[string]any{"data": map[string]any{"reply": "ok"}})
}

func (w *adminWorld) agentHistoryHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]any{"data": []any{}})
}

func (w *adminWorld) ledgerExportHandler(c echo.Context) error {
	w.ledgerExports++
	return c.JSON(http.StatusOK, map[string]any{"data": map[string]any{"rows": w.ledgerExports}})
}

// remount rebuilds the HTTP surface without restarting the application, which
// is what a deploy carrying a different gate constant would produce.
func (w *adminWorld) remount() error {
	if w.server != nil {
		w.server.Close()
		w.server = nil
	}
	return w.mountAPI()
}

func (w *adminWorld) reboot() error {
	if w.server != nil {
		w.server.Close()
		w.server = nil
	}
	if w.app != nil {
		ctx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer cancel()
		if err := w.app.Stop(ctx); err != nil {
			return err
		}
		w.app = nil
	}
	return w.boot()
}

// --- people ---------------------------------------------------------------

func (w *adminWorld) personFor(email, password string) (*featurePerson, error) {
	if p, ok := w.people[email]; ok {
		return p, nil
	}
	ctx := context.Background()
	agent, _, account, err := w.authService.RegisterPassword(ctx, email, displayNameForFeature(email), password)
	if err != nil {
		return nil, fmt.Errorf("could not create %q: %w", email, err)
	}
	p := &featurePerson{email: email, password: password, agentID: agent.GetID()}
	if account != nil {
		p.accountID = account.GetID()
		if err := w.accountRepo.SaveMember(ctx, account.GetID(), agent.GetID(), authentities.RoleOwner); err != nil {
			return nil, err
		}
	}
	w.people[email] = p
	return p, nil
}

func (w *adminWorld) person(email string) (*featurePerson, error) {
	p, ok := w.people[email]
	if !ok {
		return nil, fmt.Errorf("no person named %q has been staged", email)
	}
	return p, nil
}

func (w *adminWorld) accountFor(name string) (string, error) {
	id, ok := w.accounts[name]
	if !ok {
		return "", fmt.Errorf("no account named %q has been staged", name)
	}
	return id, nil
}

func (w *adminWorld) addToAccount(email, accountName, role string) error {
	accountID, err := w.accountFor(accountName)
	if err != nil {
		return err
	}
	p, err := w.personFor(email, "correct-horse-battery-staple")
	if err != nil {
		return err
	}
	if err := w.accountRepo.SaveMember(context.Background(), accountID, p.agentID, role); err != nil {
		return err
	}
	w.resolver.InvalidateAgents(context.Background(), accountID, p.agentID)
	p.accountID = accountID
	return nil
}

// signIn creates a real session, because the listing's whole point is to be
// answered over HTTP with whatever the browser is carrying.
func (w *adminWorld) signIn(p *featurePerson) error {
	ctx := context.Background()
	creds, err := w.credRepo.FindByAgent(ctx, p.agentID)
	if err != nil || len(creds) == 0 {
		return fmt.Errorf("could not find credentials for %q: %w", p.email, err)
	}
	s, err := w.authService.CreateSession(ctx, p.agentID, p.accountID, creds[0].GetID(),
		"127.0.0.1", "godog", time.Hour)
	if err != nil {
		return err
	}
	p.sessionIDs = append(p.sessionIDs, s.GetID())
	return nil
}

func (w *adminWorld) staged() []string {
	emails := make([]string, 0, len(w.people))
	for email := range w.people {
		emails = append(emails, email)
	}
	sort.Strings(emails)
	return emails
}

func (w *adminWorld) soleActor() (*featurePerson, error) {
	if len(w.signedInOrder) == 1 {
		return w.person(w.signedInOrder[0])
	}
	if w.lastActor != "" {
		return w.person(w.lastActor)
	}
	return nil, fmt.Errorf("%d people are signed in, so \"they\" is ambiguous", len(w.signedInOrder))
}

// --- HTTP -----------------------------------------------------------------

// request drives the mounted API. A nil person is a caller carrying no session
// at all, which is the case the listing exists to answer.
func (w *adminWorld) request(p *featurePerson, method, path string, body any) (*apiCall, error) {
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, w.server.URL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p != nil {
		for _, c := range w.cookiesFor(p) {
			req.AddCookie(c)
		}
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, readErr := res.Body.Read(buf)
		sb.Write(buf[:n])
		if readErr != nil {
			break
		}
	}
	// Carry any cookie the server set, so impersonation survives the next call.
	if p != nil {
		w.jar[p.email] = mergeCookies(w.jar[p.email], res.Cookies())
	}
	return &apiCall{route: method + " " + path, status: res.StatusCode, body: sb.String()}, nil
}

// cookiesFor is the browser's jar for one person: the session cookie built
// from their real session, plus whatever the server has set since — which is
// how the impersonation cookie survives from one call to the next.
func (w *adminWorld) cookiesFor(p *featurePerson) []*http.Cookie {
	if len(w.jar[p.email]) == 0 && len(p.sessionIDs) > 0 {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if err := w.sessionManager.CreateHTTPSession(rec, req, session.SessionData{
			SessionID: p.sessionIDs[len(p.sessionIDs)-1],
			AgentID:   p.agentID,
			AccountID: p.accountID,
		}); err == nil {
			w.jar[p.email] = rec.Result().Cookies()
		}
	}
	return w.jar[p.email]
}

func mergeCookies(have []*http.Cookie, got []*http.Cookie) []*http.Cookie {
	out := append([]*http.Cookie(nil), have...)
	for _, c := range got {
		replaced := false
		for i := range out {
			if out[i].Name == c.Name {
				out[i] = c
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, c)
		}
	}
	return out
}

// statusesFrom pulls the listing out of the response envelope.
func statusesFrom(body string) ([]entities.FeatureStatus, []string, error) {
	var env struct {
		Data     []entities.FeatureStatus `json:"data"`
		Messages []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		return nil, nil, fmt.Errorf("could not read the feature listing: %w (body %q)", err, body)
	}
	msgs := make([]string, 0, len(env.Messages))
	for _, m := range env.Messages {
		msgs = append(msgs, m.Text)
	}
	return env.Data, msgs, nil
}

func statusOf(set []entities.FeatureStatus, key string) (entities.FeatureStatus, bool) {
	for _, s := range set {
		if s.Key == key {
			return s, true
		}
	}
	return entities.FeatureStatus{}, false
}

var _ = testing.Verbose
