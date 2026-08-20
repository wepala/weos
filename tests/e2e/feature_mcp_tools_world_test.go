package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cucumber/godog"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/open-feature/go-sdk/openfeature"
	"go.uber.org/fx"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"
	mcpserver "github.com/wepala/weos/v3/internal/mcp"

	adkagent "google.golang.org/adk/v2/agent"
)

// TestFeatureMCPTools runs the acceptance contract for story #484.
//
//	GODOG_TAGS=@story-484 go test ./tests/e2e/ -run TestFeatureMCPTools
func TestFeatureMCPTools(t *testing.T) {
	tags := "~@wip"
	if v := os.Getenv("GODOG_TAGS"); v != "" {
		tags = v
	}
	suite := godog.TestSuite{
		ScenarioInitializer: initFeatureMCPToolsScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/feature_flag_mcp_tools.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("MCP tool gating acceptance scenarios failed")
	}
}

// --- the harness-registered tool ------------------------------------------

// activeToolsWorld is the world the ledger_export configurer writes into.
//
// The configurer is a process-global (that is what RegisterMCPConfigurer is),
// so it is registered once and does nothing unless a #484 scenario is running.
// Without this guard every other suite's NewConfiguredServer would grow a tool
// it never asked for.
var (
	activeToolsMu sync.Mutex
	activeTools   *toolsWorld
)

func setActiveToolsWorld(w *toolsWorld) {
	activeToolsMu.Lock()
	defer activeToolsMu.Unlock()
	activeTools = w
}

func currentToolsWorld() *toolsWorld {
	activeToolsMu.Lock()
	defer activeToolsMu.Unlock()
	return activeTools
}

// ledgerExportInput is deliberately empty: the tool exists to be gated and to
// prove a refused call writes nothing, not to do work.
type ledgerExportInput struct{}

type ledgerExportOutput struct {
	Rows int `json:"rows"`
}

func init() {
	mcpserver.RegisterMCPConfigurer(func(server *gomcp.Server, deps mcpserver.ConfigurerDeps) {
		w := currentToolsWorld()
		if w == nil {
			return
		}
		// Registered exactly the way a downstream binary registers a custom
		// tool, and gated exactly the way a built-in one is: at its own
		// AddTool call site. It mutates, so a refused call is provable by the
		// absence of what it would have written.
		mcpserver.AddGatedTool(server, deps.Gates, w.ledgerGateKey, &gomcp.Tool{
			Name:        "ledger_export",
			Description: "Export the ledger to a spreadsheet.",
		}, func(
			_ context.Context, _ *gomcp.CallToolRequest, _ ledgerExportInput,
		) (*gomcp.CallToolResult, ledgerExportOutput, error) {
			return nil, ledgerExportOutput{Rows: w.recordExport()}, nil
		})
	})
}

// --- spies ----------------------------------------------------------------

// settingsSpy counts reads of the stored feature state and can be made
// unreadable, so "resolved once" and "the store cannot be read" are both
// measurements rather than intentions.
type toolsSettingsSpy struct {
	inner repositories.FeatureSettingsRepository
	mu    sync.Mutex
	// Counted apart, because the stdio scenarios ask specifically whether an
	// account override was read — an answer that a combined counter, which the
	// instance read always moves, could not give.
	instanceReads int
	accountReads  int
	down          bool
}

var errStoreDown = fmt.Errorf("the feature store is unreadable")

func (s *toolsSettingsSpy) InstanceOverrides(ctx context.Context) (map[string]bool, error) {
	s.mu.Lock()
	s.instanceReads++
	down := s.down
	s.mu.Unlock()
	if down {
		return nil, errStoreDown
	}
	return s.inner.InstanceOverrides(ctx)
}

func (s *toolsSettingsSpy) AccountOverrides(ctx context.Context, accountID string) (map[string]bool, error) {
	s.mu.Lock()
	s.accountReads++
	down := s.down
	s.mu.Unlock()
	if down {
		return nil, errStoreDown
	}
	return s.inner.AccountOverrides(ctx, accountID)
}

func (s *toolsSettingsSpy) SetOverride(ctx context.Context, scopeType, scopeID, key string, on bool) error {
	return s.inner.SetOverride(ctx, scopeType, scopeID, key, on)
}

func (s *toolsSettingsSpy) ClearOverride(ctx context.Context, scopeType, scopeID, key string) error {
	return s.inner.ClearOverride(ctx, scopeType, scopeID, key)
}

// count is every read of stored feature state, which is what the cost
// assertion is about.
func (s *toolsSettingsSpy) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.instanceReads + s.accountReads
}

func (s *toolsSettingsSpy) accountCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accountReads
}

func (s *toolsSettingsSpy) setDown(down bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.down = down
}

// toolsGrantSpy counts grant reads and can be made unreadable alongside the
// settings store — the scenarios speak of one store holding both.
type toolsGrantSpy struct {
	inner repositories.FeatureGrantRepository
	mu    sync.Mutex
	reads int
	down  bool
}

func (g *toolsGrantSpy) GrantsFor(
	ctx context.Context, accountID, agentID, roleID string,
) ([]entities.FeatureGrantRecord, error) {
	g.mu.Lock()
	g.reads++
	down := g.down
	g.mu.Unlock()
	if down {
		return nil, errStoreDown
	}
	return g.inner.GrantsFor(ctx, accountID, agentID, roleID)
}

func (g *toolsGrantSpy) ListByFeature(
	ctx context.Context, accountID, key string,
) ([]entities.FeatureGrantRecord, error) {
	return g.inner.ListByFeature(ctx, accountID, key)
}

func (g *toolsGrantSpy) Grant(ctx context.Context, record entities.FeatureGrantRecord) error {
	return g.inner.Grant(ctx, record)
}

func (g *toolsGrantSpy) Revoke(
	ctx context.Context, accountID, subjectType, subjectID, key string,
) (bool, error) {
	return g.inner.Revoke(ctx, accountID, subjectType, subjectID, key)
}

func (g *toolsGrantSpy) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.reads
}

func (g *toolsGrantSpy) setDown(down bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.down = down
}

// loggerSpy keeps every warning, so "logged once" and "logged the failure" are
// assertions about what an operator can actually see.
type loggerSpy struct {
	inner entities.Logger
	mu    sync.Mutex
	warns []string
}

func (l *loggerSpy) Debug(ctx context.Context, msg string, f ...any) { l.inner.Debug(ctx, msg, f...) }
func (l *loggerSpy) Info(ctx context.Context, msg string, f ...any)  { l.inner.Info(ctx, msg, f...) }

// Error is recorded alongside Warn. An operator looking for why something is
// off does not filter by level, and #486 logs the unreadable store at error.
func (l *loggerSpy) Error(ctx context.Context, msg string, fields ...any) {
	l.record(msg, fields)
	l.inner.Error(ctx, msg, fields...)
}

func (l *loggerSpy) Warn(ctx context.Context, msg string, fields ...any) {
	l.record(msg, fields)
	l.inner.Warn(ctx, msg, fields...)
}

func (l *loggerSpy) record(msg string, fields []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	line := msg
	for _, f := range fields {
		line += fmt.Sprintf(" %v", f)
	}
	l.warns = append(l.warns, line)
}

func (l *loggerSpy) matching(substrings ...string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
outer:
	for _, line := range l.warns {
		for _, s := range substrings {
			if !strings.Contains(line, s) {
				continue outer
			}
		}
		out = append(out, line)
	}
	return out
}

// --- the world ------------------------------------------------------------

// mcpConnection is one client's live MCP session. Held open across steps
// because the contract turns on sessions outliving a change: a client that
// listed before a revoke must be able to call, and to list again, in the
// session it already had.
type mcpConnection struct {
	client *gomcp.ClientSession
	server *gomcp.ServerSession
	cancel context.CancelFunc
	person *featurePerson
	listed []string
}

type toolsWorld struct {
	app    *fx.App
	tmpDir string
	dsn    string
	boots  int

	registry *application.FeatureRegistry
	admin    *application.FeatureAdminService
	features *application.FeatureService
	resolver *application.FeatureResolver
	client   *openfeature.Client
	settings *toolsSettingsSpy
	grants   *toolsGrantSpy
	invSpy   *invalidatorSpy
	logs     *loggerSpy
	logger   entities.Logger

	resources      application.ResourceService
	rts            application.ResourceTypeService
	episodicRecall application.EpisodicRecall

	authService authapp.AuthenticationService
	accountRepo authrepos.AccountRepository

	server *gomcp.Server

	declared      []entities.FeatureMeta
	maxCacheAge   time.Duration
	ledgerGateKey string
	seededID      string

	people   map[string]*featurePerson
	accounts map[string]string

	connections map[string]*mcpConnection
	stdio       *mcpConnection
	// connects counts how many times a caller opened a session, so "the MCP
	// session was not reconnected" is a measurement.
	connects map[string]int
	// restartBaseline is the boot count when the scenario's action began, so
	// "the instance was not restarted" survives a Given that legitimately
	// reboots.
	restartBaseline int

	// what the steps observed
	listings          map[string][]string
	calls             map[string]*gomcp.CallToolResult
	callErrs          map[string]error
	toolsets          map[string][]string
	readsBefore       int
	readsAfterListing int
	invsBefore        int
	exportsSeen       int
	answered          int
	straightAway      *gomcp.CallToolResult
	afterMoment       *gomcp.CallToolResult
	windowStart       time.Time
	moment            time.Time
	lastCallTool      string
	lastCallEmail     string
	lastAgentEmail    string
	signedInOrder     []string
}

// advertisedTool is the part of a listed tool the contract asserts about.
type advertisedTool struct {
	Annotations *gomcp.ToolAnnotations
	InputSchema any
}

// gomcpResult pairs a call result with the tool it came from, so a failure
// message can name it.
type gomcpResult struct {
	res  *gomcp.CallToolResult
	tool string
}

func newToolsWorld() *toolsWorld {
	return &toolsWorld{
		ledgerGateKey: "ledger-export",
		people:        map[string]*featurePerson{},
		accounts:      map[string]string{},
		connections:   map[string]*mcpConnection{},
		connects:      map[string]int{},
		listings:      map[string][]string{},
		calls:         map[string]*gomcp.CallToolResult{},
		callErrs:      map[string]error{},
		toolsets:      map[string][]string{},
	}
}

func (w *toolsWorld) recordExport() int {
	w.exportsSeen++
	return w.exportsSeen
}

func (w *toolsWorld) teardown() {
	setActiveToolsWorld(nil)
	for _, c := range w.connections {
		c.close()
	}
	if w.stdio != nil {
		w.stdio.close()
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

func (c *mcpConnection) close() {
	if c == nil {
		return
	}
	if c.client != nil {
		_ = c.client.Close()
	}
	if c.server != nil {
		_ = c.server.Close()
	}
	if c.cancel != nil {
		c.cancel()
	}
}

func (w *toolsWorld) boot() error {
	if w.app != nil {
		return nil
	}
	setActiveToolsWorld(w)
	if w.tmpDir == "" {
		dir, err := os.MkdirTemp("", "weos-feature-tools-e2e-")
		if err != nil {
			return fmt.Errorf("could not create a temp dir: %w", err)
		}
		w.tmpDir = dir
		w.dsn = filepath.Join(dir, "tools.db")
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
	// The background's declarations arrive through FEATURES, the same channel
	// an operator uses, so the suite does not need a code change to declare a
	// feature. episodic-recall is dropped from the list: core declares it
	// itself now that it gates a shipped tool, and two declarations of one key
	// is an error by design.
	cfg.Features.Declared = w.configDeclarations()

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
		fx.Populate(&w.registry, &w.admin, &w.features, &w.resolver, &w.client),
		fx.Populate(&w.resources, &w.rts, &w.episodicRecall, &w.logger),
		fx.Populate(&w.authService, &w.accountRepo),
	)
	startCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}
	w.app = app
	w.boots++
	w.server = nil
	// The database outlives a reboot, so the seed is written once. Seeding
	// again would collide with the resource type that is already there.
	if w.seededID == "" {
		if err := w.seedResource(); err != nil {
			return err
		}
	}
	return nil
}

// configDeclarations is the background's table minus anything core declares
// itself, so the suite declares each key exactly once.
func (w *toolsWorld) configDeclarations() []entities.FeatureMeta {
	return declarationsBeyondCore(w.declared)
}

func coreDeclared() map[string]entities.FeatureMeta {
	out := map[string]entities.FeatureMeta{}
	for _, m := range application.CoreFeatureDeclarations() {
		out[m.Key] = m
	}
	return out
}

// coreAlreadyDeclares reports whether core declares this feature itself, so a
// suite's Background does not declare it a second time — which is an error by
// design, because two declarations of one key means two things believe they own
// it.
//
// It also checks that core says what the contract's table says, so a scenario
// can never prove something about a feature nobody has. The description is left
// out of the comparison on purpose: it is prose an operator reads, and
// improving it must not fail an acceptance run.
//
// Shared by every feature suite. Since #484 core declares episodic-recall,
// because a shipped tool is gated on it.
func coreAlreadyDeclares(m entities.FeatureMeta) (bool, error) {
	got, ok := coreDeclared()[m.Key]
	if !ok {
		return false, nil
	}
	got.Description, m.Description = "", ""
	if got != m {
		return true, fmt.Errorf("core declares %q as %+v, but the contract says %+v", m.Key, got, m)
	}
	return true, nil
}

// seedResource gives resource_get something real to fetch, so "calling
// resource_get succeeds" means the call succeeded rather than merely that the
// gate let it past.
func (w *toolsWorld) seedResource() error {
	ctx := context.Background()
	if _, err := w.rts.Create(ctx, application.CreateResourceTypeCommand{
		Name:        "Note",
		Slug:        "note",
		Description: "A note",
		Context:     json.RawMessage(`{"@vocab":"https://schema.org/"}`),
		Schema:      json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}}}`),
	}); err != nil {
		return fmt.Errorf("could not seed the note resource type: %w", err)
	}
	res, err := w.resources.Create(ctx, application.CreateResourceCommand{
		TypeSlug: "note",
		Data:     json.RawMessage(`{"title":"a seeded note"}`),
	})
	if err != nil {
		return fmt.Errorf("could not seed a note: %w", err)
	}
	w.seededID = res.GetID()
	return nil
}

func (w *toolsWorld) reboot() error {
	for email, c := range w.connections {
		c.close()
		delete(w.connections, email)
	}
	if w.stdio != nil {
		w.stdio.close()
		w.stdio = nil
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

// mcpServer builds the tool surface the way serve.go does — through
// NewConfiguredServer, with the real OpenFeature-backed gate and every
// downstream configurer applied. Built lazily so a scenario can change a gate
// key or an override before the first client connects.
func (w *toolsWorld) mcpServer() (*gomcp.Server, error) {
	if w.server != nil {
		return w.server, nil
	}
	server, err := mcpserver.NewConfiguredServer(
		w.rts, w.resources, nil, nil, w.episodicRecall, w.admin,
		application.ToolFeatureGate(w.client), slog.New(slog.NewTextHandler(os.Stderr, nil)),
	)
	if err != nil {
		return nil, err
	}
	w.server = server
	return server, nil
}

// --- people and accounts --------------------------------------------------

func (w *toolsWorld) personFor(email, password string) (*featurePerson, error) {
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

func (w *toolsWorld) person(email string) (*featurePerson, error) {
	p, ok := w.people[email]
	if !ok {
		return nil, fmt.Errorf("no person named %q has been staged", email)
	}
	return p, nil
}

func (w *toolsWorld) accountFor(name string) (string, error) {
	id, ok := w.accounts[name]
	if !ok {
		return "", fmt.Errorf("no account named %q has been staged", name)
	}
	return id, nil
}

func (w *toolsWorld) addToAccount(email, accountName string) error {
	accountID, err := w.accountFor(accountName)
	if err != nil {
		return err
	}
	p, err := w.personFor(email, "correct-horse-battery-staple")
	if err != nil {
		return err
	}
	if err := w.accountRepo.SaveMember(
		context.Background(), accountID, p.agentID, authentities.RoleMember); err != nil {
		return err
	}
	w.resolver.InvalidateAgents(context.Background(), accountID, p.agentID)
	p.accountID = accountID
	return nil
}

func (w *toolsWorld) ctxFor(p *featurePerson) context.Context {
	return auth.ContextWithAgent(context.Background(),
		&auth.Identity{AgentID: p.agentID, ActiveAccountID: p.accountID})
}

// --- connecting -----------------------------------------------------------

// connect opens a live MCP session under a caller's identity and keeps it. A
// nil person is the local stdio transport: no session, no token, no active
// account, so resolution reaches the instance layer and stops.
func (w *toolsWorld) connect(p *featurePerson, local bool) (*mcpConnection, error) {
	server, err := w.mcpServer()
	if err != nil {
		return nil, err
	}
	base := context.Background()
	if p != nil {
		base = w.ctxFor(p)
	}
	if local {
		base = application.WithLocalTransport(base)
	}
	ctx, cancel := context.WithCancel(base)

	serverTransport, clientTransport := gomcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	client := gomcp.NewClient(&gomcp.Implementation{Name: "godog", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	conn := &mcpConnection{client: clientSession, server: serverSession, cancel: cancel, person: p}
	if p != nil {
		w.connections[p.email] = conn
		w.connects[p.email]++
	} else {
		w.stdio = conn
		w.connects["stdio"]++
	}
	w.restartBaseline = w.boots
	return conn, nil
}

func (w *toolsWorld) connectionFor(email string) (*mcpConnection, error) {
	c, ok := w.connections[email]
	if !ok {
		return nil, fmt.Errorf("%q is not connected over MCP", email)
	}
	return c, nil
}

// theOne returns the single connection a scenario has, for the steps that say
// "they" without naming anybody.
func (w *toolsWorld) theOne() (*mcpConnection, error) {
	if w.stdio != nil && len(w.connections) == 0 {
		return w.stdio, nil
	}
	if len(w.connections) != 1 {
		return nil, fmt.Errorf("%d MCP connections are open, so \"they\" is ambiguous", len(w.connections))
	}
	for _, c := range w.connections {
		return c, nil
	}
	return nil, fmt.Errorf("no MCP connection is open")
}

// --- listing and calling --------------------------------------------------

func (w *toolsWorld) list(conn *mcpConnection) ([]string, error) {
	ctx := context.Background()
	res, err := conn.client.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(res.Tools))
	for _, t := range res.Tools {
		names = append(names, t.Name)
	}
	conn.listed = names
	key := "stdio"
	if conn.person != nil {
		key = conn.person.email
	}
	w.listings[key] = names
	return names, nil
}

func (w *toolsWorld) listedTools(conn *mcpConnection) (map[string]*gomcp.Tool, error) {
	res, err := conn.client.ListTools(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*gomcp.Tool, len(res.Tools))
	for _, t := range res.Tools {
		out[t.Name] = t
	}
	return out, nil
}

// argsFor supplies whatever a tool needs to be able to succeed, so a call that
// fails failed for the reason under test.
func (w *toolsWorld) argsFor(tool string) map[string]any {
	switch tool {
	case "resource_get":
		return map[string]any{"id": w.seededID}
	default:
		return map[string]any{}
	}
}

func (w *toolsWorld) call(conn *mcpConnection, tool string) (*gomcp.CallToolResult, error) {
	res, err := conn.client.CallTool(context.Background(), &gomcp.CallToolParams{
		Name: tool, Arguments: w.argsFor(tool),
	})
	key := tool
	if conn.person != nil {
		key = conn.person.email + "|" + tool
	}
	w.calls[key] = res
	w.callErrs[key] = err
	return res, err
}

func (w *toolsWorld) resultFor(email, tool string) (*gomcp.CallToolResult, error) {
	key := tool
	if email != "" {
		key = email + "|" + tool
	}
	res, ok := w.calls[key]
	if !ok {
		return nil, fmt.Errorf("no call to %q was made", tool)
	}
	return res, nil
}

// refusalMarker is the phrase the gate's refusal carries. Matching on it is
// what separates "the gate refused this" from "the tool ran and failed",
// which the contract needs to tell apart in both directions.
const refusalMarker = "is not available: the"

func refusedByGate(res *gomcp.CallToolResult) bool {
	return res != nil && res.IsError && strings.Contains(mcpText(res), refusalMarker)
}

// --- the in-app agent -----------------------------------------------------

// agentContext adapts a plain context to the ADK agent context a toolset needs,
// so a turn can be driven outside a running agent. The embedded mock covers the
// agent-side methods; the context methods delegate to a real ctx, which is what
// carries the caller's identity to the gate.
type agentContext struct {
	adkagent.ContextMock
	ctx context.Context
}

func (a *agentContext) Deadline() (time.Time, bool) { return a.ctx.Deadline() }
func (a *agentContext) Done() <-chan struct{}       { return a.ctx.Done() }
func (a *agentContext) Err() error                  { return a.ctx.Err() }
func (a *agentContext) Value(key any) any           { return a.ctx.Value(key) }

func (w *toolsWorld) agentToolset(p *featurePerson) ([]string, error) {
	server, err := w.mcpServer()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(w.ctxFor(p))
	defer cancel()
	ts, err := mcpserver.NewAgentToolset(ctx, server, mcpserver.AgentToolsetConfig{})
	if err != nil {
		return nil, err
	}
	tools, err := ts.Tools(&agentContext{ctx: ctx})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name())
	}
	w.toolsets[p.email] = names
	return names, nil
}

func (w *toolsWorld) callFromAgentToolset(p *featurePerson, name string) error {
	server, err := w.mcpServer()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(w.ctxFor(p))
	defer cancel()
	ts, err := mcpserver.NewAgentToolset(ctx, server, mcpserver.AgentToolsetConfig{})
	if err != nil {
		return err
	}
	tools, err := ts.Tools(&agentContext{ctx: ctx})
	if err != nil {
		return err
	}
	for _, t := range tools {
		if t.Name() != name {
			continue
		}
		runner, ok := t.(interface {
			Run(adkagent.Context, any) (map[string]any, error)
		})
		if !ok {
			return fmt.Errorf("%q is not callable from the agent's toolset", name)
		}
		// A refusal arrives as an error, not as output: ADK's mcpTool.Run
		// checks IsError before it looks at structured content, so the
		// refusal text comes back in the error and never reaches output
		// validation. Returning it is enough to fail a "succeeds" step.
		_, runErr := runner.Run(&agentContext{ctx: ctx}, map[string]any{})
		return runErr
	}
	return fmt.Errorf("the agent's toolset does not hold %q", name)
}

func contains(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

var _ = testing.Verbose

// declarationsBeyondCore drops the features core declares itself, so a suite's
// Background reaches the registry through FEATURES without colliding with the
// code declaration of the same key.
func declarationsBeyondCore(declared []entities.FeatureMeta) []entities.FeatureMeta {
	out := make([]entities.FeatureMeta, 0, len(declared))
	for _, m := range declared {
		if core, _ := coreAlreadyDeclares(m); core {
			continue
		}
		out = append(out, m)
	}
	return out
}
