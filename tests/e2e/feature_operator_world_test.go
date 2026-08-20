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

// TestFeatureOperatorSwitch runs the acceptance contract for story #482.
//
//	GODOG_TAGS=@story-482 go test ./tests/e2e/ -run TestFeatureOperatorSwitch
func TestFeatureOperatorSwitch(t *testing.T) {
	tags := "~@wip"
	if v := os.Getenv("GODOG_TAGS"); v != "" {
		tags = v
	}
	suite := godog.TestSuite{
		ScenarioInitializer: initFeatureOperatorScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/feature_flag_operator_switch.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("operator feature switch acceptance scenarios failed")
	}
}

// operatorWorld is one scenario's instance, plus everything needed to reach it
// from the three surfaces the story adds.
type operatorWorld struct {
	app    *fx.App
	tmpDir string
	dsn    string
	server *httptest.Server

	registry  *application.FeatureRegistry
	admin     *application.FeatureAdminService
	resolver  *application.FeatureResolver
	db        *gormlib.DB
	settings  repositories.FeatureSettingsRepository
	eventLog  repositories.EventLogRepository
	resources application.ResourceService
	rts       application.ResourceTypeService

	authService    authapp.AuthenticationService
	accountRepo    authrepos.AccountRepository
	credRepo       authrepos.CredentialRepository
	sessionManager session.SessionManager
	logger         entities.Logger

	maxCacheAge time.Duration
	declared    []entities.FeatureMeta

	accounts map[string]string
	people   map[string]*featurePerson
	current  *featurePerson

	// workDir is where a CLI subprocess runs, so "leaves no database behind in
	// the directory it ran from" can be checked against a directory nothing
	// else writes to.
	workDir string
	// noDSN stages the instance-with-no-database case: the subprocess gets an
	// empty DATABASE_DSN and no flag.
	noDSN bool
	// stdio marks the MCP client as attached over the local transport.
	stdio   bool
	lastRun commandRun

	// The last answer from each surface, so the assertions can be phrased the
	// way the contract phrases them. Reads and writes are tracked separately
	// because a scenario can do both before asserting on either — a member
	// reads the listing, is refused a change, and then both are checked.
	listingStatus    int
	changeStatus     int
	lastStatus       int
	lastBody         []byte
	listingBody      []byte
	lastListing      []entities.FeatureStatus
	lastMCPOut       map[string]any
	lastMCPErr       error
	lastEvalBool     *bool
	lastEvaluatedKey string

	envBefore map[string]*string
}

func (w *operatorWorld) teardown() {
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

func (w *operatorWorld) setEnv(key string, value *string) {
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

// boot starts the instance and mounts the API the admin UI calls.
func (w *operatorWorld) boot() error {
	if w.app != nil {
		return nil
	}
	if w.tmpDir == "" {
		dir, err := os.MkdirTemp("", "weos-feature-operator-e2e-")
		if err != nil {
			return fmt.Errorf("could not create a temp dir: %w", err)
		}
		w.tmpDir = dir
		w.dsn = filepath.Join(dir, "operator.db")
		w.workDir = filepath.Join(dir, "cwd")
		if err := os.MkdirAll(w.workDir, 0o755); err != nil {
			return fmt.Errorf("could not create the command working dir: %w", err)
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
	// Declared through the environment rather than by calling Register, so the
	// CLI subprocess sees exactly the features this instance does. Two
	// processes can only agree about which features exist by reading the same
	// declarations, and nothing is persisted for them to share.
	cfg.Features.Declared = append([]entities.FeatureMeta(nil), w.declared...)

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Populate(&w.registry, &w.admin, &w.resolver, &w.settings, &w.eventLog),
		fx.Populate(&w.db),
		fx.Populate(&w.resources, &w.rts),
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

// mountAPI builds the HTTP surface.
//
// RequireAuth is mounted explicitly rather than taken from serve.go's wiring.
// serve.go mounts it only when OAuth is configured and otherwise mounts
// SoftAuth, which never answers 401 — so the "a request carrying no session is
// refused as not authenticated" scenario would be unprovable against the
// default password-only wiring. account_scoped_sessions_test.go builds its own
// echo instance for exactly this reason.
func (w *operatorWorld) mountAPI() error {
	e := echo.New()
	e.HideBanner = true
	api := e.Group("/api")

	protected := api.Group("", echo.WrapMiddleware(authhttp.RequireAuth(w.sessionManager, w.authService)))
	fh := handlers.NewFeatureHandler(handlers.FeatureHandlerConfig{Admin: w.admin, Logger: w.logger})
	protected.GET("/features", fh.List)
	protected.PUT("/features/:key/instance", fh.SetInstance)
	protected.DELETE("/features/:key/instance", fh.ResetInstance)
	protected.PUT("/features/:key/account", fh.SetAccount)
	protected.DELETE("/features/:key/account", fh.ResetAccount)

	w.server = httptest.NewServer(e)
	return nil
}

func (w *operatorWorld) reboot() error {
	if w.server != nil {
		w.server.Close()
		w.server = nil
	}
	if w.app != nil {
		ctx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer cancel()
		if err := w.app.Stop(ctx); err != nil {
			return fmt.Errorf("could not stop the app to reconfigure it: %w", err)
		}
		w.app = nil
	}
	return w.boot()
}

// --- people ---------------------------------------------------------------

func (w *operatorWorld) personFor(email, password string) (*featurePerson, error) {
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
			return nil, fmt.Errorf("could not record %q as owner: %w", email, err)
		}
	}
	w.people[email] = p
	return p, nil
}

func (w *operatorWorld) person(email string) (*featurePerson, error) {
	p, ok := w.people[email]
	if !ok {
		return nil, fmt.Errorf("no person named %q has been staged", email)
	}
	return p, nil
}

func (w *operatorWorld) signIn(p *featurePerson) error {
	ctx := context.Background()
	creds, err := w.credRepo.FindByAgent(ctx, p.agentID)
	if err != nil || len(creds) == 0 {
		return fmt.Errorf("could not find credentials for %q: %w", p.email, err)
	}
	s, err := w.authService.CreateSession(ctx, p.agentID, p.accountID, creds[0].GetID(),
		"127.0.0.1", "godog", time.Hour)
	if err != nil {
		return fmt.Errorf("could not sign %q in: %w", p.email, err)
	}
	p.sessionIDs = append(p.sessionIDs, s.GetID())
	w.current = p
	return nil
}

func (w *operatorWorld) ctxFor(p *featurePerson) context.Context {
	return auth.ContextWithAgent(context.Background(),
		&auth.Identity{AgentID: p.agentID, ActiveAccountID: p.accountID})
}

// --- HTTP -----------------------------------------------------------------

// request makes an authenticated call as p, or an anonymous one when p is nil.
func (w *operatorWorld) request(p *featurePerson, method, path string, body any) error {
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
	w.lastListing = nil
	if method == http.MethodGet {
		w.listingStatus = resp.StatusCode
		w.listingBody = buf.Bytes()
	} else {
		w.changeStatus = resp.StatusCode
	}
	return nil
}

// cookieFor builds the session cookie header pericarp's RequireAuth reads.
func (w *operatorWorld) cookieFor(p *featurePerson) (string, error) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	err := w.sessionManager.CreateHTTPSession(rec, req, session.SessionData{
		SessionID: p.sessionIDs[len(p.sessionIDs)-1],
		AgentID:   p.agentID,
		AccountID: p.accountID,
	})
	if err != nil {
		return "", fmt.Errorf("could not build a session cookie for %q: %w", p.email, err)
	}
	return sessionCookieHeader(rec.Result().Cookies()), nil
}

// listingFromBody decodes the envelope the API answers with.
func (w *operatorWorld) listingFromBody() ([]entities.FeatureStatus, error) {
	if w.lastListing != nil {
		return w.lastListing, nil
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.lastBody, &envelope); err != nil {
		return nil, fmt.Errorf("could not read the response: %w (%s)", err, string(w.lastBody))
	}
	var many []entities.FeatureStatus
	if err := json.Unmarshal(envelope.Data, &many); err == nil {
		w.lastListing = many
		return many, nil
	}
	var one entities.FeatureStatus
	if err := json.Unmarshal(envelope.Data, &one); err != nil {
		return nil, fmt.Errorf("could not read the feature data: %w (%s)", err, string(envelope.Data))
	}
	w.lastListing = []entities.FeatureStatus{one}
	return w.lastListing, nil
}

// --- CLI ------------------------------------------------------------------

// runCLI runs the real binary against the same store, as a separate process —
// which is the point of the command-line scenarios.
func (w *operatorWorld) runCLI(command string) error {
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
	dsn := "DATABASE_DSN=" + w.dsn
	if w.noDSN {
		dsn = "DATABASE_DSN="
	}
	env := append(os.Environ(), dsn, "LOG_LEVEL=error")
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

// callMCP drives the real tool over an in-memory transport.
//
// local decides whether the session is marked as the stdio transport. The
// marker normally comes from mcp.Run(), which an in-process test does not go
// through, so the harness applies it deliberately — a harness that forgot
// would fail the stdio scenario for the wrong reason.
func (w *operatorWorld) callMCP(
	p *featurePerson, local bool, tool string, args map[string]any,
) error {
	server, err := mcpserver.NewMCPServer(w.rts, w.resources, nil, nil, nil, w.admin, []string{"feature"})
	if err != nil {
		return fmt.Errorf("could not build the MCP server: %w", err)
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
		return fmt.Errorf("could not connect the MCP server: %w", err)
	}
	defer serverSession.Close()

	client := gomcp.NewClient(&gomcp.Implementation{Name: "godog", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return fmt.Errorf("could not connect the MCP client: %w", err)
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

func mcpText(res *gomcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*gomcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// mcpFeatures pulls the listing out of whatever the last tool returned.
func (w *operatorWorld) mcpFeatures() ([]entities.FeatureStatus, error) {
	if w.lastMCPOut == nil {
		return nil, fmt.Errorf("the last MCP call returned no structured output")
	}
	raw, err := json.Marshal(w.lastMCPOut["features"])
	if err != nil {
		return nil, err
	}
	var out []entities.FeatureStatus
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("could not read the tool's features: %w", err)
	}
	return out, nil
}

// --- shared assertions ----------------------------------------------------

func findStatus(statuses []entities.FeatureStatus, key string) (entities.FeatureStatus, bool) {
	for _, s := range statuses {
		if s.Key == key {
			return s, true
		}
	}
	return entities.FeatureStatus{}, false
}

// sourcePhrase maps the contract's wording onto the layer names the resolver
// reports.
func sourcePhrase(phrase string) string {
	switch {
	case strings.Contains(phrase, "instance"):
		return "instance"
	case strings.Contains(phrase, "account"):
		return "account"
	case strings.Contains(phrase, "grant"):
		return "grant"
	default:
		return "default"
	}
}

// --- helpers the steps use ------------------------------------------------

// resolverEnabled asks the resolver the way a call site would.
func (w *operatorWorld) resolverEnabled(p *featurePerson, key string) (bool, bool, error) {
	return w.resolver.Enabled(w.ctxFor(p), key)
}

// lastFeatureChange reads the most recent audit record out of the event log.
func (w *operatorWorld) lastFeatureChange() (entities.FeatureChanged, error) {
	page, err := w.eventLog.Query(context.Background(), repositories.EventLogFilter{
		EventType: entities.FeatureChanged{}.EventType(),
		Limit:     100,
	})
	if err != nil {
		return entities.FeatureChanged{}, fmt.Errorf("could not read the change log: %w", err)
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
		return entities.FeatureChanged{}, fmt.Errorf("could not read the recorded change: %w", err)
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = last.CreatedAt
	}
	return event, nil
}

// countInstanceRows counts stored rows directly.
//
// The repository returns a map keyed by feature, which collapses duplicates —
// so it can say whether a setting exists but not how many rows back it. "One"
// is the assertion worth making: a second flip that inserted instead of
// replacing would leave two rows and resolve identically, so nothing else
// would ever notice.
func (w *operatorWorld) countInstanceRows(key string) (int64, error) {
	var count int64
	err := w.db.Table("feature_settings").
		Where("scope_type = ? AND scope_id = ? AND feature_key = ?",
			repositories.FeatureScopeInstance, "", key).
		Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("could not count stored settings: %w", err)
	}
	return count, nil
}

// osReadDir lists a directory's entry names.
func osReadDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("could not read %q: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}
