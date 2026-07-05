//go:build oxigraph_embedded

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"
	mcpserver "github.com/wepala/weos/v3/internal/mcp"

	pericarpdomain "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/cucumber/godog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/fx"
)

// TestEmbeddedKnowledgeGraph runs the embedded-graph acceptance scenarios
// (story #422). Requires the oxigraph_embedded build tag and CGO wired to the
// vendored lib — run via `make test-graph-embedded` (which fetches the lib and
// sets CGO_LDFLAGS) or:
//
//	CGO_LDFLAGS=-L<lib> go test -tags oxigraph_embedded ./tests/e2e/ -run TestEmbeddedKnowledgeGraph
func TestEmbeddedKnowledgeGraph(t *testing.T) {
	tags := os.Getenv("GODOG_TAGS") // empty runs every scenario, incl. @wip
	suite := godog.TestSuite{
		Name:                "embedded-knowledge-graph",
		ScenarioInitializer: initEmbeddedKGScenario,
		Options: &godog.Options{
			Format: "pretty",
			Paths: []string{
				"features/embedded_knowledge_graph.feature",
				"features/knowledge_graph_store_selection.feature",
			},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("embedded knowledge-graph acceptance scenarios failed")
	}
}

// kgWorld reuses mcpWorld's app/MCP-session lifecycle and helpers, adding a
// stable embedded store directory (so a restart reopens the same store), a
// name→URN map, and an optional capturing logger for the fail-open scenario.
type kgWorld struct {
	mcpWorld
	graphDir  string
	urns      map[string]string
	logs      *captureLogger
	lastQuery string
}

func initEmbeddedKGScenario(sc *godog.ScenarioContext) {
	w := &kgWorld{urns: map[string]string{}}
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})
	sc.Step(`^a WeOS twin with an embedded knowledge-graph store$`, w.twinEmbedded)
	sc.Step(`^a WeOS twin with no knowledge-graph store configured$`, w.twinNop)
	sc.Step(`^a WeOS twin whose embedded knowledge-graph store path cannot be opened$`, w.twinBadPath)
	sc.Step(`^the "([^"]*)" preset is installed$`, w.presetIsInstalled)
	sc.Step(`^a "([^"]*)" named "([^"]*)" exists$`, w.resourceExists)
	sc.Step(`^a "([^"]*)" named "([^"]*)" exists linked to the project "([^"]*)"$`, w.linkedResourceExists)
	sc.Step(`^I create a "([^"]*)" named "([^"]*)"$`, w.iCreate)
	sc.Step(`^the "([^"]*)" resource "([^"]*)" is created$`, w.resourceIsCreated)
	sc.Step(`^I search the knowledge graph for "([^"]*)"$`, w.iSearch)
	sc.Step(`^the search results include the "([^"]*)" resource "([^"]*)"$`, w.searchIncludes)
	sc.Step(`^I expand the "([^"]*)" resource "([^"]*)" in the knowledge graph$`, w.iExpand)
	sc.Step(`^the neighborhood includes the project "([^"]*)"$`, w.neighborhoodIncludes)
	sc.Step(`^I run the SPARQL query:$`, w.iRunSPARQL)
	sc.Step(`^the query results include the "([^"]*)" resource "([^"]*)"$`, w.queryIncludes)
	sc.Step(`^I delete the "([^"]*)" resource "([^"]*)"$`, w.iDelete)
	sc.Step(`^the knowledge graph no longer returns the "([^"]*)" resource "([^"]*)"$`, w.graphNoLongerReturns)
	sc.Step(`^the twin restarts against the same embedded store$`, w.restart)
	sc.Step(`^the knowledge graph still returns the "([^"]*)" resource "([^"]*)"$`, w.graphStillReturns)
	sc.Step(`^the knowledge graph reports it is not configured$`, w.reportsNotConfigured)
	sc.Step(`^the twin logs a single error that the embedded knowledge-graph store could not be opened$`, w.loggedOneOpenError)
}

// --- Background / boot ---

func (w *kgWorld) twinEmbedded(_ context.Context) error {
	dir := w.newTmp()
	w.graphDir = filepath.Join(dir, "graph")
	cfg := w.baseConfig(dir)
	cfg.Oxigraph.Path = w.graphDir
	return w.startApp(cfg, false)
}

func (w *kgWorld) twinNop(_ context.Context) error {
	return w.startApp(w.baseConfig(w.newTmp()), false)
}

func (w *kgWorld) twinBadPath(_ context.Context) error {
	dir := w.newTmp()
	bad := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(bad, []byte("x"), 0o600); err != nil {
		return err
	}
	cfg := w.baseConfig(dir)
	cfg.Oxigraph.Path = bad // a regular file where the store dir is expected
	return w.startApp(cfg, true)
}

func (w *kgWorld) newTmp() string {
	if w.tmpDir == "" {
		w.tmpDir, _ = os.MkdirTemp("", "weos-kg-e2e-")
	}
	return w.tmpDir
}

func (w *kgWorld) baseConfig(dir string) config.Config {
	cfg := config.Default()
	cfg.DatabaseDSN = filepath.Join(dir, "test.db")
	cfg.LogLevel = "error"
	// Run the background subscribers in-process so the oxigraph projector
	// catches up and writes resource triples into the graph.
	cfg.Worker.RunInProcess = true
	return cfg
}

func (w *kgWorld) startApp(cfg config.Config, capture bool) error {
	sessCtx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel

	var rts application.ResourceTypeService
	var rs application.ResourceService
	var kg application.KnowledgeGraphService
	var episodic application.EpisodicRecall
	var eventStore pericarpdomain.EventStore
	var manager *application.Manager

	opts := []fx.Option{
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Populate(&rts, &rs, &kg, &episodic, &eventStore, &manager),
	}
	if capture {
		w.logs = &captureLogger{}
		opts = append(opts, fx.Decorate(func(entities.Logger) entities.Logger { return w.logs }))
	}
	app := fx.New(opts...)
	startCtx, startCancel := context.WithTimeout(sessCtx, fx.DefaultTimeout)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("start app: %w", err)
	}
	w.app = app
	w.eventStore = eventStore
	w.manager = manager
	w.rts = rts

	server, err := mcpserver.NewMCPServer(rts, rs, kg, nil, episodic, nil)
	if err != nil {
		return fmt.Errorf("build MCP server: %w", err)
	}
	serverT, clientT := mcp.NewInMemoryTransports()
	srvSess, err := server.Connect(sessCtx, serverT, nil)
	if err != nil {
		return fmt.Errorf("connect server session: %w", err)
	}
	w.srvSess = srvSess
	client := mcp.NewClient(&mcp.Implementation{Name: "kg-e2e", Version: "0.0.1"}, nil)
	clientSess, err := client.Connect(sessCtx, clientT, nil)
	if err != nil {
		return fmt.Errorf("connect client session: %w", err)
	}
	w.client = clientSess
	return nil
}

// restart stops the app + sessions (the embedded store flushes and unlocks via
// the fx OnStop hook) WITHOUT removing tmpDir, then reboots against the same
// database and embedded store directory.
func (w *kgWorld) restart(_ context.Context) error {
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
		_ = w.app.Stop(stopCtx)
		cancel()
	}
	cfg := w.baseConfig(w.tmpDir)
	cfg.Oxigraph.Path = w.graphDir
	return w.startApp(cfg, false)
}

// --- Resource seeding (through the resource_create MCP tool) ---

func (w *kgWorld) resourceExists(ctx context.Context, typeSlug, name string) error {
	return w.create(ctx, typeSlug, name, "")
}

func (w *kgWorld) linkedResourceExists(ctx context.Context, typeSlug, name, projectName string) error {
	proj, ok := w.urns[projectName]
	if !ok {
		return fmt.Errorf("no known project %q to link to", projectName)
	}
	return w.create(ctx, typeSlug, name, proj)
}

func (w *kgWorld) iCreate(ctx context.Context, typeSlug, name string) error {
	return w.create(ctx, typeSlug, name, "")
}

func (w *kgWorld) create(ctx context.Context, typeSlug, name, projectURN string) error {
	var data string
	switch typeSlug {
	case "task":
		data = fmt.Sprintf(`{"name":%q,"status":"open","priority":"medium"`, name)
		if projectURN != "" {
			data += fmt.Sprintf(`,"project":%q`, projectURN)
		}
		data += `}`
	default:
		data = fmt.Sprintf(`{"name":%q,"description":"e2e","status":"active"}`, name)
	}
	res, err := w.client.CallTool(ctx, &mcp.CallToolParams{
		Name:      "resource_create",
		Arguments: json.RawMessage(fmt.Sprintf(`{"type_slug":%q,"data":%s}`, typeSlug, data)),
	})
	if err != nil {
		return fmt.Errorf("resource_create protocol error: %w", err)
	}
	w.lastResult, w.lastErr, w.lastText = res, nil, textOf(res)
	if res.IsError {
		return fmt.Errorf("resource_create failed: %s", w.lastText)
	}
	m, err := w.resultMap()
	if err != nil {
		return err
	}
	id, _ := m["id"].(string)
	if id == "" {
		return fmt.Errorf("resource_create returned no id: %s", w.lastText)
	}
	w.urns[name] = id
	return nil
}

func (w *kgWorld) resourceIsCreated(_ context.Context, _, name string) error {
	if w.urns[name] == "" {
		return fmt.Errorf("resource %q was not created", name)
	}
	return nil
}

// --- kg_* tool calls + assertions ---

func (w *kgWorld) callKG(ctx context.Context, name, args string) {
	res, err := w.client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: json.RawMessage(args)})
	w.lastResult, w.lastErr, w.lastText = res, err, textOf(res)
}

func (w *kgWorld) iSearch(ctx context.Context, q string) error {
	w.lastQuery = q
	w.callKG(ctx, "kg_search_entities", fmt.Sprintf(`{"q":%q}`, q))
	return nil
}

func (w *kgWorld) searchIncludes(ctx context.Context, _, name string) error {
	urn := w.mustURN(name)
	return w.poll("search to include "+name, func() bool {
		w.callKG(ctx, "kg_search_entities", fmt.Sprintf(`{"q":%q}`, w.lastQuery))
		return jsonHasValue(w.lastText, "matches", urn)
	})
}

func (w *kgWorld) iExpand(ctx context.Context, _, name string) error {
	urn := w.mustURN(name)
	w.callKG(ctx, "kg_expand_entity", fmt.Sprintf(`{"iri":%q}`, urn))
	return nil
}

func (w *kgWorld) neighborhoodIncludes(ctx context.Context, projectName string) error {
	proj := w.mustURN(projectName)
	// The projection is async, so re-expand the same entity until its linked
	// project shows up as a triple object.
	iri := lastExpandIRI(w.lastText)
	return w.poll("neighborhood to include "+projectName, func() bool {
		w.callKG(ctx, "kg_expand_entity", fmt.Sprintf(`{"iri":%q}`, iri))
		return tripleObjectPresent(w.lastText, proj)
	})
}

func (w *kgWorld) iRunSPARQL(ctx context.Context, query *godog.DocString) error {
	w.lastQuery = strings.TrimSpace(query.Content)
	w.callKG(ctx, "kg_sparql_query", fmt.Sprintf(`{"query":%q}`, w.lastQuery))
	return nil
}

func (w *kgWorld) queryIncludes(ctx context.Context, _, name string) error {
	urn := w.mustURN(name)
	return w.poll("query to include "+name, func() bool {
		w.callKG(ctx, "kg_sparql_query", fmt.Sprintf(`{"query":%q}`, w.lastQuery))
		return bindingsHaveValue(w.lastText, urn)
	})
}

func (w *kgWorld) iDelete(ctx context.Context, _, name string) error {
	urn := w.mustURN(name)
	res, err := w.client.CallTool(ctx, &mcp.CallToolParams{
		Name: "resource_delete", Arguments: json.RawMessage(fmt.Sprintf(`{"id":%q}`, urn)),
	})
	if err != nil {
		return fmt.Errorf("resource_delete protocol error: %w", err)
	}
	if res.IsError {
		return fmt.Errorf("resource_delete failed: %s", textOf(res))
	}
	return nil
}

func (w *kgWorld) graphNoLongerReturns(ctx context.Context, _, name string) error {
	urn := w.mustURN(name)
	return w.poll("graph to drop "+name, func() bool {
		w.callKG(ctx, "kg_expand_entity", fmt.Sprintf(`{"iri":%q}`, urn))
		return !tripleSubjectPresent(w.lastText, urn)
	})
}

func (w *kgWorld) graphStillReturns(ctx context.Context, _, name string) error {
	urn := w.mustURN(name)
	return w.poll("graph to still return "+name, func() bool {
		w.callKG(ctx, "kg_search_entities", fmt.Sprintf(`{"q":%q}`, name))
		return jsonHasValue(w.lastText, "matches", urn)
	})
}

func (w *kgWorld) reportsNotConfigured(ctx context.Context) error {
	w.callKG(ctx, "kg_search_entities", `{"q":"anything"}`)
	if w.lastResult == nil || !w.lastResult.IsError {
		return fmt.Errorf("expected the kg tool to report the graph unavailable, got: %s", w.lastText)
	}
	if !strings.Contains(strings.ToLower(w.lastText), "not configured") {
		return fmt.Errorf("expected a 'not configured' message, got: %s", w.lastText)
	}
	return nil
}

func (w *kgWorld) loggedOneOpenError() error {
	if w.logs == nil {
		return fmt.Errorf("no capturing logger was installed")
	}
	if n := w.logs.count("embedded store"); n != 1 {
		return fmt.Errorf("expected exactly one 'embedded store' error, got %d: %v", n, w.logs.snapshot())
	}
	return nil
}

// --- helpers ---

func (w *kgWorld) mustURN(name string) string {
	if urn := w.urns[name]; urn != "" {
		return urn
	}
	return "urn:unknown:" + name
}

func (w *kgWorld) poll(desc string, cond func() bool) error {
	deadline := time.Now().Add(8 * time.Second)
	for {
		if cond() {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s (last kg result: %s)", desc, w.lastText)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// jsonHasValue reports whether any object in the named array field carries the
// given "value" (kg_search_entities matches / any {value} list).
func jsonHasValue(text, field, want string) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &m) != nil {
		return false
	}
	var arr []struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(m[field], &arr) != nil {
		return false
	}
	for _, e := range arr {
		if e.Value == want {
			return true
		}
	}
	return false
}

type kgTriples struct {
	IRI     string `json:"iri"`
	Triples []struct {
		Subject   string `json:"subject"`
		Predicate string `json:"predicate"`
		Object    string `json:"object"`
	} `json:"triples"`
}

func lastExpandIRI(text string) string {
	var t kgTriples
	_ = json.Unmarshal([]byte(text), &t)
	return t.IRI
}

func tripleObjectPresent(text, want string) bool {
	var t kgTriples
	if json.Unmarshal([]byte(text), &t) != nil {
		return false
	}
	for _, tr := range t.Triples {
		if tr.Object == want {
			return true
		}
	}
	return false
}

func tripleSubjectPresent(text, want string) bool {
	var t kgTriples
	if json.Unmarshal([]byte(text), &t) != nil {
		return false
	}
	for _, tr := range t.Triples {
		if tr.Subject == want {
			return true
		}
	}
	return false
}

func bindingsHaveValue(text, want string) bool {
	var out struct {
		Bindings []map[string]struct {
			Value string `json:"value"`
		} `json:"bindings"`
	}
	if json.Unmarshal([]byte(text), &out) != nil {
		return false
	}
	for _, row := range out.Bindings {
		for _, term := range row {
			if term.Value == want {
				return true
			}
		}
	}
	return false
}

// captureLogger records Error messages so a scenario can assert loud
// degradation (exactly one open-failure error).
type captureLogger struct {
	mu   sync.Mutex
	msgs []string
}

func (c *captureLogger) Debug(context.Context, string, ...interface{}) {}
func (c *captureLogger) Info(context.Context, string, ...interface{})  {}
func (c *captureLogger) Warn(context.Context, string, ...interface{})  {}
func (c *captureLogger) Error(_ context.Context, msg string, _ ...interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, msg)
}

func (c *captureLogger) count(substr string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, m := range c.msgs {
		if strings.Contains(m, substr) {
			n++
		}
	}
	return n
}

func (c *captureLogger) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.msgs...)
}
