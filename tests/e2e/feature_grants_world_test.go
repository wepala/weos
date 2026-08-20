package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/labstack/echo/v4"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/fx"
	gormlib "gorm.io/gorm"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authhttp "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/http"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"

	"github.com/wepala/weos/v3/api/handlers"
	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"
	mcpserver "github.com/wepala/weos/v3/internal/mcp"
)

// TestFeatureGrants runs the acceptance contract for story #483.
//
//	GODOG_TAGS=@story-483 go test ./tests/e2e/ -run TestFeatureGrants
func TestFeatureGrants(t *testing.T) {
	tags := "~@wip"
	if v := os.Getenv("GODOG_TAGS"); v != "" {
		tags = v
	}
	suite := godog.TestSuite{
		ScenarioInitializer: initFeatureGrantsScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/feature_flag_grants.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("feature grant acceptance scenarios failed")
	}
}

// grantsSpyRepo counts reads per caller, so "read no feature state" and "the
// grant store was read once" are measurements rather than intentions.
type grantsSpyRepo struct {
	inner repositories.FeatureGrantRepository

	mu    sync.Mutex
	reads map[string]int
	// lastRows records how many rows the last read returned, which is what the
	// cost scenario is actually about: a caller with three grants must not
	// read the account's two hundred.
	lastRows int
}

func (g *grantsSpyRepo) GrantsFor(
	ctx context.Context, accountID, agentID, roleID string,
) ([]entities.FeatureGrantRecord, error) {
	g.mu.Lock()
	if g.reads == nil {
		g.reads = map[string]int{}
	}
	g.reads[agentOf(ctx)]++
	g.mu.Unlock()
	rows, err := g.inner.GrantsFor(ctx, accountID, agentID, roleID)
	g.mu.Lock()
	g.lastRows = len(rows)
	g.mu.Unlock()
	return rows, err
}

func (g *grantsSpyRepo) ListByFeature(
	ctx context.Context, accountID, key string,
) ([]entities.FeatureGrantRecord, error) {
	return g.inner.ListByFeature(ctx, accountID, key)
}

func (g *grantsSpyRepo) Grant(ctx context.Context, record entities.FeatureGrantRecord) error {
	return g.inner.Grant(ctx, record)
}

func (g *grantsSpyRepo) Revoke(
	ctx context.Context, subjectType, subjectID, accountID, key string,
) (bool, error) {
	return g.inner.Revoke(ctx, subjectType, subjectID, accountID, key)
}

func (g *grantsSpyRepo) countFor(agentID string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.reads[agentID]
}

func (g *grantsSpyRepo) reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.reads = map[string]int{}
}

func (g *grantsSpyRepo) rowsLastRead() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.lastRows
}

// invalidatorSpy counts invalidations, so a scenario claiming a window took
// effect on its own can prove nothing dropped the cache for it.
type invalidatorSpy struct {
	inner repositories.FeatureCacheInvalidator
	mu    sync.Mutex
	calls int
}

func (i *invalidatorSpy) InvalidateAll(ctx context.Context) {
	i.bump()
	i.inner.InvalidateAll(ctx)
}

func (i *invalidatorSpy) InvalidateAccount(ctx context.Context, accountID string) {
	i.bump()
	i.inner.InvalidateAccount(ctx, accountID)
}

func (i *invalidatorSpy) InvalidateAgents(ctx context.Context, accountID string, agentIDs ...string) {
	i.bump()
	i.inner.InvalidateAgents(ctx, accountID, agentIDs...)
}

func (i *invalidatorSpy) bump() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.calls++
}

func (i *invalidatorSpy) count() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.calls
}

type grantsWorld struct {
	app    *fx.App
	tmpDir string
	dsn    string
	server *httptest.Server

	registry *application.FeatureRegistry
	admin    *application.FeatureAdminService
	resolver *application.FeatureResolver
	grants   repositories.FeatureGrantRepository
	spy      *grantsSpyRepo
	invSpy   *invalidatorSpy
	settings repositories.FeatureSettingsRepository
	eventLog repositories.EventLogRepository
	db       *gormlib.DB

	resources application.ResourceService
	rts       application.ResourceTypeService

	authService    authapp.AuthenticationService
	accountRepo    authrepos.AccountRepository
	credRepo       authrepos.CredentialRepository
	sessionManager session.SessionManager
	logger         entities.Logger

	maxCacheAge time.Duration
	declared    []entities.FeatureMeta
	primary     string

	accounts map[string]string
	people   map[string]*featurePerson
	current  *featurePerson
	// group is who "both of them" means: the people a scenario named.
	group   []string
	workDir string
	stdio   bool

	lastRun      commandRun
	lastStatus   int
	lastBody     []byte
	listingStat  int
	listingBody  []byte
	lastMCPOut   map[string]any
	lastMCPErr   error
	answers      map[string]bool
	lastKey      string
	straightAway bool
	afterMoment  bool
	// readsStraightAway and readsAfterMoment record what each of the two
	// window evaluations cost, so the scenarios can tell the boundary
	// mechanism from an implementation that simply never caches.
	readsStraightAway int
	readsAfterMoment  int
	// invalidationsAtStraightAway snapshots the invalidation count, so
	// "nothing invalidated the session in between" is a measurement.
	invalidationsAtStraightAway int
	// moment is the instant a window scenario waits for.
	moment time.Time

	envBefore map[string]*string
}

func (w *grantsWorld) teardown() {
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
	for k, v := range w.envBefore {
		if v == nil {
			os.Unsetenv(k)
		} else {
			os.Setenv(k, *v)
		}
	}
	if w.tmpDir != "" {
		os.RemoveAll(w.tmpDir)
		w.tmpDir = ""
	}
}

func (w *grantsWorld) setEnv(key string, value *string) {
	if w.envBefore == nil {
		w.envBefore = map[string]*string{}
	}
	if _, seen := w.envBefore[key]; !seen {
		if old, ok := os.LookupEnv(key); ok {
			w.envBefore[key] = &old
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

func (w *grantsWorld) boot() error {
	if w.app != nil {
		return nil
	}
	if w.tmpDir == "" {
		dir, err := os.MkdirTemp("", "weos-feature-grants-e2e-")
		if err != nil {
			return fmt.Errorf("could not create a temp dir: %w", err)
		}
		w.tmpDir = dir
		w.dsn = filepath.Join(dir, "grants.db")
		w.workDir = filepath.Join(dir, "cwd")
		if err := os.MkdirAll(w.workDir, 0o755); err != nil {
			return err
		}
	}
	pw := "true"
	w.setEnv("PASSWORD_AUTH_ENABLED", &pw)
	w.setEnv("GOOGLE_CLIENT_ID", nil)
	w.setEnv("GOOGLE_CLIENT_SECRET", nil)

	cfg := config.Default()
	cfg.LoadFromEnvironment()
	cfg.DatabaseDSN = w.dsn
	cfg.LogLevel = "error"
	if w.maxCacheAge > 0 {
		cfg.Features.CacheMaxAge = w.maxCacheAge
	}
	cfg.Features.Declared = append([]entities.FeatureMeta(nil), w.declared...)
	cfg.Features.PrimaryAccountID = w.primary

	w.spy = &grantsSpyRepo{}
	w.invSpy = &invalidatorSpy{}
	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Decorate(func(inner repositories.FeatureGrantRepository) repositories.FeatureGrantRepository {
			w.spy.inner = inner
			return w.spy
		}),
		fx.Decorate(func(inner repositories.FeatureCacheInvalidator) repositories.FeatureCacheInvalidator {
			w.invSpy.inner = inner
			return w.invSpy
		}),
		fx.Populate(&w.registry, &w.admin, &w.resolver, &w.grants, &w.settings, &w.eventLog),
		fx.Populate(&w.db, &w.resources, &w.rts),
		fx.Populate(&w.authService, &w.accountRepo, &w.credRepo, &w.sessionManager, &w.logger),
	)
	startCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}
	w.app = app
	return w.mountAPI()
}

// mountAPI mounts RequireAuth explicitly, for the same reason #482's world
// does: serve.go mounts SoftAuth under password-only auth, which never answers
// 401, so the unauthenticated scenario would be unprovable against it.
func (w *grantsWorld) mountAPI() error {
	e := echo.New()
	e.HideBanner = true
	api := e.Group("/api")
	protected := api.Group("", echo.WrapMiddleware(authhttp.RequireAuth(w.sessionManager, w.authService)))
	fh := handlers.NewFeatureHandler(handlers.FeatureHandlerConfig{Admin: w.admin, Logger: w.logger})
	protected.GET("/features", fh.List)
	protected.PUT("/features/:key/instance", fh.SetInstance)
	protected.PUT("/features/:key/account", fh.SetAccount)
	protected.GET("/features/grants", fh.GrantsHeldBy)
	protected.GET("/features/:key/grants", fh.ListGrants)
	protected.POST("/features/:key/grants", fh.Grant)
	protected.DELETE("/features/:key/grants", fh.RevokeGrant)
	w.server = httptest.NewServer(e)
	return nil
}

func (w *grantsWorld) reboot() error {
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

func (w *grantsWorld) personFor(email, password string) (*featurePerson, error) {
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

func (w *grantsWorld) person(email string) (*featurePerson, error) {
	p, ok := w.people[email]
	if !ok {
		return nil, fmt.Errorf("no person named %q has been staged", email)
	}
	return p, nil
}

func (w *grantsWorld) addToAccount(email, accountName, role string) error {
	accountID, ok := w.accounts[accountName]
	if !ok {
		return fmt.Errorf("no account named %q has been staged", accountName)
	}
	p, err := w.personFor(email, "correct-horse-battery-staple")
	if err != nil {
		return err
	}
	if err := w.accountRepo.SaveMember(context.Background(), accountID, p.agentID, role); err != nil {
		return err
	}
	// Mirror what production does at the one non-service write site: a role
	// change is a direct projection write that emits no event, so
	// UserHandler.Update invalidates explicitly. Staging that here keeps the
	// harness faithful — without it a role granted after the fact would never
	// reach a session that was already open, and the scenario would fail for a
	// reason production does not have.
	w.resolver.InvalidateAgents(context.Background(), accountID, p.agentID)
	p.accountID = accountID
	return nil
}

func (w *grantsWorld) signIn(p *featurePerson) error {
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
	w.current = p
	return nil
}

func (w *grantsWorld) ctxFor(p *featurePerson) context.Context {
	return auth.ContextWithAgent(context.Background(),
		&auth.Identity{AgentID: p.agentID, ActiveAccountID: p.accountID})
}

// --- HTTP -----------------------------------------------------------------

func (w *grantsWorld) request(p *featurePerson, method, path string, body any) error {
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, w.server.URL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if p != nil && len(p.sessionIDs) > 0 {
		header, err := w.cookieFor(p)
		if err != nil {
			return err
		}
		req.Header.Set("Cookie", header)
	}
	resp, err := w.server.Client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return err
	}
	w.lastStatus = resp.StatusCode
	w.lastBody = buf.Bytes()
	// An HTTP answer supersedes any earlier tool payload, so a later
	// assertion cannot read a stale MCP result as if it were this response.
	w.lastMCPOut = nil
	if method == http.MethodGet {
		w.listingStat = resp.StatusCode
		w.listingBody = buf.Bytes()
	}
	return nil
}

func (w *grantsWorld) cookieFor(p *featurePerson) (string, error) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	err := w.sessionManager.CreateHTTPSession(rec, req, session.SessionData{
		SessionID: p.sessionIDs[len(p.sessionIDs)-1],
		AgentID:   p.agentID,
		AccountID: p.accountID,
	})
	if err != nil {
		return "", err
	}
	return sessionCookieHeader(rec.Result().Cookies()), nil
}

// grantViews decodes the envelope either listing endpoint answers with.
func (w *grantsWorld) grantViews(raw []byte) ([]application.GrantView, error) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("could not read the response: %w (%s)", err, string(raw))
	}
	var views []application.GrantView
	if err := json.Unmarshal(envelope.Data, &views); err != nil {
		return nil, fmt.Errorf("could not read the grants: %w (%s)", err, string(envelope.Data))
	}
	return views, nil
}

// --- CLI ------------------------------------------------------------------

func (w *grantsWorld) runCLI(command string) error {
	binary, err := weosBinary()
	if err != nil {
		return err
	}
	args := strings.Fields(command)
	if len(args) > 0 && args[0] == "weos" {
		args = args[1:]
	}
	cmd := exec.Command(binary, args...)
	cmd.Dir = w.workDir
	env := append(os.Environ(), "DATABASE_DSN="+w.dsn, "LOG_LEVEL=error")
	if declared, err := json.Marshal(w.declared); err == nil && len(w.declared) > 0 {
		env = append(env, "FEATURES="+string(declared))
	}
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	w.lastRun = commandRun{stdout: stdout.String(), stderr: stderr.String(), args: args}
	if runErr != nil {
		if exit, ok := runErr.(*exec.ExitError); ok {
			w.lastRun.exitCode = exit.ExitCode()
			return nil
		}
		return fmt.Errorf("could not run the command: %w", runErr)
	}
	return nil
}

// --- MCP ------------------------------------------------------------------

func (w *grantsWorld) callMCP(
	p *featurePerson, local bool, tool string, args map[string]any,
) error {
	server, err := mcpserver.NewMCPServer(w.rts, w.resources, nil, nil, nil, w.admin, []string{"feature"})
	if err != nil {
		return err
	}
	ctx := context.Background()
	if p != nil {
		ctx = w.ctxFor(p)
	}
	if local {
		ctx = application.WithLocalTransport(ctx)
	}
	serverTransport, clientTransport := gomcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return err
	}
	defer serverSession.Close()
	client := gomcp.NewClient(&gomcp.Implementation{Name: "godog", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return err
	}
	defer clientSession.Close()

	res, callErr := clientSession.CallTool(ctx, &gomcp.CallToolParams{Name: tool, Arguments: args})
	w.lastMCPErr = callErr
	w.lastMCPOut = nil
	if callErr == nil && res != nil {
		if res.IsError {
			w.lastMCPErr = fmt.Errorf("%s", mcpText(res))
		} else if res.StructuredContent != nil {
			if m, ok := res.StructuredContent.(map[string]any); ok {
				w.lastMCPOut = m
			}
		}
	}
	return nil
}

func (w *grantsWorld) mcpGrants() ([]application.GrantView, error) {
	if w.lastMCPOut == nil {
		return nil, fmt.Errorf("the last MCP call returned no structured output")
	}
	raw, err := json.Marshal(w.lastMCPOut["grants"])
	if err != nil {
		return nil, err
	}
	var out []application.GrantView
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// --- helpers --------------------------------------------------------------

func (w *grantsWorld) accountFor(name string) (string, error) {
	id, ok := w.accounts[name]
	if !ok {
		return "", fmt.Errorf("no account named %q has been staged", name)
	}
	return id, nil
}

// storedGrants reads the rows directly, for the assertions about what is
// stored rather than what resolves.
func (w *grantsWorld) storedGrants(accountID, key string) ([]entities.FeatureGrantRecord, error) {
	return w.grants.ListByFeature(context.Background(), accountID, key)
}

func (w *grantsWorld) lastFeatureChangeEvent() (entities.FeatureChanged, error) {
	page, err := w.eventLog.Query(context.Background(), repositories.EventLogFilter{
		EventType: entities.FeatureChanged{}.EventType(),
		Limit:     200,
	})
	if err != nil {
		return entities.FeatureChanged{}, err
	}
	if page == nil || len(page.Data) == 0 {
		return entities.FeatureChanged{}, fmt.Errorf("no feature change was recorded at all")
	}
	last := page.Data[len(page.Data)-1]
	raw, err := json.Marshal(last.Payload)
	if err != nil {
		return entities.FeatureChanged{}, err
	}
	var event entities.FeatureChanged
	if err := json.Unmarshal(raw, &event); err != nil {
		return entities.FeatureChanged{}, err
	}
	return event, nil
}

func findGrant(views []application.GrantView, email string) (application.GrantView, bool) {
	for _, v := range views {
		if v.Email == email {
			return v, true
		}
	}
	return application.GrantView{}, false
}

func findRoleGrant(views []application.GrantView, role string) (application.GrantView, bool) {
	for _, v := range views {
		if v.Role == role {
			return v, true
		}
	}
	return application.GrantView{}, false
}

var _ = testing.Verbose
