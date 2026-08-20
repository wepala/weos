package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/internal/config"
	mcpserver "github.com/wepala/weos/v3/internal/mcp"

	pericarpdomain "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/cucumber/godog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/fx"
)

// TestMCPResourceCreate runs the resource_create MCP-tool acceptance scenarios
// against a freshly booted application with a clean SQLite database. All
// scenarios are part of the regression suite; the @wip tag is honored so a
// future scenario can be quarantined without changing this runner. Filter on
// demand with: GODOG_TAGS=@wip go test ./tests/e2e/ -run TestMCPResourceCreate -v
func TestMCPResourceCreate(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "mcp-resource-create",
		ScenarioInitializer: initResourceCreateScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/resource_create.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("resource_create acceptance scenarios failed")
	}
}

// mcpWorld holds the per-scenario application, the in-memory MCP client session,
// and the result of the most recent tool call.
type mcpWorld struct {
	app     *fx.App
	tmpDir  string
	client  *mcp.ClientSession
	srvSess *mcp.ServerSession
	cancel  context.CancelFunc
	rts     application.ResourceTypeService
	// eventStore backs direct event seeding (episodic recall scenarios need
	// controlled occurred-at timestamps the entity API never exposes).
	eventStore pericarpdomain.EventStore
	// runWorkers opts a suite into in-process background subscribers
	// (projections like event-references); manager drives checkpoint rebuilds.
	runWorkers bool
	manager    *application.Manager

	pendingContext string
	pendingSchema  string

	lastResult *mcp.CallToolResult
	lastErr    error
	lastText   string
}

func initResourceCreateScenario(sc *godog.ScenarioContext) {
	w := &mcpWorld{}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a clean WeOS knowledge graph$`, w.aCleanKnowledgeGraph)
	sc.Step(`^a resource type "([^"]*)" exists with a JSON Schema requiring an object with a "([^"]*)" property$`,
		w.aResourceTypeWithSchema)
	sc.Step(`^I call resource_create for type "([^"]*)" with the data:$`, w.iCallResourceCreate)
	sc.Step(`^the call succeeds$`, w.theCallSucceeds)
	sc.Step(`^the call fails$`, w.theCallFails)
	sc.Step(`^the call fails with a validation error$`, w.theCallFailsWithValidationError)
	sc.Step(`^the returned resource has a "([^"]*)" URN identifier$`, w.returnedResourceHasURN)
	sc.Step(`^the returned resource has status "([^"]*)"$`, w.returnedResourceHasStatus)
	sc.Step(`^the returned resource data has name "([^"]*)"$`, w.returnedResourceDataHasName)
	sc.Step(`^fetching the returned resource by its identifier returns the same capability$`, w.fetchReturnsSame)
	sc.Step(`^the error states that the "([^"]*)" argument must be a JSON object$`, w.errorStatesArgumentMustBeObject)
	sc.Step(`^the error does not contain the raw text "([^"]*)"$`, w.errorDoesNotContainRawText)
	sc.Step(`^the error does not leak a local file path$`, w.errorDoesNotLeakPath)
	sc.Step(`^the error names the missing property "([^"]*)"$`, w.errorNamesMissingProperty)
	sc.Step(`^the error states that the resource type "([^"]*)" is not found$`, w.errorStatesTypeNotFound)
}

// --- Background ---

func (w *mcpWorld) aCleanKnowledgeGraph(_ context.Context) error {
	cfg := config.Default()
	dir, err := os.MkdirTemp("", "weos-mcp-e2e-")
	if err != nil {
		return err
	}
	w.tmpDir = dir
	cfg.DatabaseDSN = filepath.Join(dir, "test.db") // ProvideGormDB adds the worker pragmas
	cfg.LogLevel = "error"
	if lvl := os.Getenv("E2E_LOG_LEVEL"); lvl != "" {
		cfg.LogLevel = lvl
	}
	cfg.Worker.RunInProcess = w.runWorkers

	// Both MCP sessions must outlive any single step, so anchor them to a
	// scenario-scoped context (canceled in teardown) rather than the per-step
	// context godog passes in.
	sessCtx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel

	var rts application.ResourceTypeService
	var rs application.ResourceService
	var kg application.KnowledgeGraphService
	var episodic application.EpisodicRecall
	var eventStore pericarpdomain.EventStore
	var manager *application.Manager

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Populate(&rts, &rs, &kg, &episodic, &eventStore, &manager),
	)
	startCtx, startCancel := context.WithTimeout(sessCtx, fx.DefaultTimeout)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}
	w.app = app
	w.eventStore = eventStore
	w.manager = manager

	server, err := mcpserver.NewMCPServer(rts, rs, kg, nil, episodic, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to build MCP server: %w", err)
	}
	// NewInMemoryTransports returns symmetric net.Pipe ends; the SDK convention
	// (see internal/mcp/handler_test.go) names the first end the server's.
	// Server.Connect returns immediately — the session reader runs in the
	// background and is reaped by ServerSession.Close in teardown.
	serverT, clientT := mcp.NewInMemoryTransports()
	srvSess, err := server.Connect(sessCtx, serverT, nil)
	if err != nil {
		return fmt.Errorf("failed to connect server session: %w", err)
	}
	w.srvSess = srvSess

	client := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "0.0.1"}, nil)
	clientSess, err := client.Connect(sessCtx, clientT, nil)
	if err != nil {
		return fmt.Errorf("failed to connect client session: %w", err)
	}
	w.client = clientSess

	// Stash the type service so the next Background step can seed the schema.
	w.rts = rts
	return nil
}

func (w *mcpWorld) presetIsInstalled(ctx context.Context, name string) error {
	if w.rts == nil {
		return fmt.Errorf("application not booted")
	}
	if _, err := w.rts.InstallPreset(ctx, name, true); err != nil {
		return fmt.Errorf("failed to install %q preset: %w", name, err)
	}
	return nil
}

func (w *mcpWorld) aResourceTypeWithSchema(ctx context.Context, slug, requiredProp string) error {
	if w.rts == nil {
		return fmt.Errorf("application not booted")
	}
	schema := fmt.Sprintf(
		`{"type":"object","properties":{%q:{"type":"string"},"description":{"type":"string"},`+
			`"kind":{"type":"string"}},"required":[%q]}`, requiredProp, requiredProp)
	_, err := w.rts.Create(ctx, application.CreateResourceTypeCommand{
		Name:        titleCase(slug),
		Slug:        slug,
		Description: "A digital-twin capability",
		Context:     json.RawMessage(`{"@vocab":"https://schema.org/"}`),
		Schema:      json.RawMessage(schema),
	})
	if err != nil {
		return fmt.Errorf("failed to seed resource type %q: %w", slug, err)
	}
	return nil
}

// --- Actions ---

func (w *mcpWorld) iCallResourceCreate(ctx context.Context, typeSlug string, data *godog.DocString) error {
	args := fmt.Sprintf(`{"type_slug":%q,"data":%s}`, typeSlug, data.Content)
	res, err := w.client.CallTool(ctx, &mcp.CallToolParams{
		Name: "resource_create", Arguments: json.RawMessage(args),
	})
	w.lastResult = res
	w.lastErr = err
	w.lastText = textOf(res)
	return nil
}

// --- Outcomes ---

func (w *mcpWorld) theCallSucceeds() error {
	if w.lastErr != nil {
		return fmt.Errorf("expected success, got protocol error: %v", w.lastErr)
	}
	if w.lastResult == nil || w.lastResult.IsError {
		return fmt.Errorf("expected success, got tool error: %s", w.lastText)
	}
	return nil
}

func (w *mcpWorld) theCallFails() error {
	if w.lastErr == nil && (w.lastResult == nil || !w.lastResult.IsError) {
		return fmt.Errorf("expected the call to fail, but it succeeded: %s", w.lastText)
	}
	return nil
}

func (w *mcpWorld) theCallFailsWithValidationError() error {
	if err := w.theCallFails(); err != nil {
		return err
	}
	// "validat" covers both the tool's own "validation" errors and the MCP
	// SDK's protocol-level "validating \"arguments\"" rejection, which now
	// fires before the handler for non-object payloads.
	if !strings.Contains(strings.ToLower(w.errMessage()), "validat") {
		return fmt.Errorf("expected a validation error, got: %s", w.errMessage())
	}
	return nil
}

func (w *mcpWorld) returnedResourceHasURN(prefix string) error {
	m, err := w.resultMap()
	if err != nil {
		return err
	}
	id, _ := m["id"].(string)
	want := "urn:" + prefix + ":"
	if !strings.HasPrefix(id, want) {
		return fmt.Errorf("expected id to start with %q, got %q", want, id)
	}
	return nil
}

func (w *mcpWorld) returnedResourceHasStatus(status string) error {
	m, err := w.resultMap()
	if err != nil {
		return err
	}
	if got, _ := m["status"].(string); got != status {
		return fmt.Errorf("expected status %q, got %q", status, got)
	}
	return nil
}

func (w *mcpWorld) returnedResourceDataHasName(name string) error {
	if !strings.Contains(w.lastText, fmt.Sprintf(`"name":%q`, name)) {
		return fmt.Errorf("expected returned data to contain name %q, got: %s", name, w.lastText)
	}
	return nil
}

func (w *mcpWorld) fetchReturnsSame(ctx context.Context) error {
	m, err := w.resultMap()
	if err != nil {
		return err
	}
	id, _ := m["id"].(string)
	if id == "" {
		return fmt.Errorf("no id in created resource: %s", w.lastText)
	}
	res, err := w.client.CallTool(ctx, &mcp.CallToolParams{
		Name: "resource_get", Arguments: json.RawMessage(fmt.Sprintf(`{"id":%q}`, id)),
	})
	if err != nil {
		return fmt.Errorf("resource_get protocol error: %v", err)
	}
	if res.IsError {
		return fmt.Errorf("resource_get returned an error: %s", textOf(res))
	}
	got := textOf(res)
	if !strings.Contains(got, id) {
		return fmt.Errorf("fetched resource did not contain id %q: %s", id, got)
	}
	return nil
}

func (w *mcpWorld) errorStatesArgumentMustBeObject(arg string) error {
	msg := strings.ToLower(w.errMessage())
	if !strings.Contains(msg, strings.ToLower(arg)) || !strings.Contains(msg, "object") {
		return fmt.Errorf("expected an error telling the caller the %q argument must be a JSON object, got: %s",
			arg, w.errMessage())
	}
	// The tool's own message says the argument "must be" an object; the MCP
	// SDK's earlier protocol-level rejection says it "want"s an object. Both
	// clearly point the caller at the fix.
	for _, phrasing := range []string{"must", "expected", "should", "want"} {
		if strings.Contains(msg, phrasing) {
			return nil
		}
	}
	return fmt.Errorf("expected an error telling the caller the %q argument must be a JSON object, got: %s",
		arg, w.errMessage())
}

func (w *mcpWorld) errorDoesNotContainRawText(raw string) error {
	if strings.Contains(w.errMessage(), raw) {
		return fmt.Errorf("error leaked raw library text %q: %s", raw, w.errMessage())
	}
	return nil
}

func (w *mcpWorld) errorDoesNotLeakPath() error {
	msg := w.errMessage()
	if strings.Contains(msg, "file://") || strings.Contains(msg, "/Users/") || strings.Contains(msg, "schema.json") {
		return fmt.Errorf("error leaked a local file path: %s", msg)
	}
	return nil
}

func (w *mcpWorld) errorNamesMissingProperty(prop string) error {
	msg := strings.ToLower(w.errMessage())
	if !strings.Contains(msg, strings.ToLower(prop)) {
		return fmt.Errorf("expected the error to name missing property %q, got: %s", prop, w.errMessage())
	}
	if !strings.Contains(msg, "missing") && !strings.Contains(msg, "required") {
		return fmt.Errorf("expected the error to say the property is missing/required, got: %s", w.errMessage())
	}
	return nil
}

func (w *mcpWorld) errorStatesTypeNotFound(slug string) error {
	msg := strings.ToLower(w.errMessage())
	if !strings.Contains(msg, strings.ToLower(slug)) || !strings.Contains(msg, "not found") {
		return fmt.Errorf("expected a 'resource type %q not found' error, got: %s", slug, w.errMessage())
	}
	return nil
}

// --- Helpers ---

func (w *mcpWorld) errMessage() string {
	parts := make([]string, 0, 2)
	if w.lastText != "" {
		parts = append(parts, w.lastText)
	}
	if w.lastErr != nil {
		parts = append(parts, w.lastErr.Error())
	}
	return strings.Join(parts, "\n")
}

func (w *mcpWorld) resultMap() (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(w.lastText), &m); err != nil {
		return nil, fmt.Errorf("tool result is not a JSON object: %w (text=%s)", err, w.lastText)
	}
	return m, nil
}

func (w *mcpWorld) teardown() {
	// Close the client first (the server end then sees EOF), reap the server
	// session goroutine via Close, then cancel the scenario context as a
	// belt-and-suspenders guard against any lingering session goroutine.
	if w.client != nil {
		_ = w.client.Close()
	}
	if w.srvSess != nil {
		_ = w.srvSess.Close()
	}
	if w.cancel != nil {
		w.cancel()
	}
	if w.app != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer cancel()
		_ = w.app.Stop(stopCtx)
	}
	if w.tmpDir != "" {
		_ = os.RemoveAll(w.tmpDir)
	}
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func textOf(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
