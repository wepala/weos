package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"
	mcpserver "github.com/wepala/weos/v3/internal/mcp"
	"github.com/wepala/weos/v3/internal/testpg"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	pericarpdomain "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/cucumber/godog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

// TestLexicalSearchTypeFilter runs the memory_search type-filter scenarios
// (story wm-irqzji) on SQLite: naming a type narrows the search, the limit is
// spent on that type alone, and the narrowing holds on the degraded graph
// label path too. Filter on demand with:
// GODOG_TAGS=@wip go test ./tests/e2e/ -run TestLexicalSearchTypeFilter -v
func TestLexicalSearchTypeFilter(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "lexical-search-type-filter",
		ScenarioInitializer: initLexicalSearchScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/lexical_search_type_filter.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("lexical search type-filter acceptance scenarios failed")
	}
}

// TestLexicalSearchAccountScope runs the memory_search account-scope
// scenarios (finding wm-yna4s3, story wm-irqzji) on SQLite: one account's
// search never returns or quotes another account's resource, the scope is
// applied before the limit, it composes with the type filter, and it survives
// an update and a rebuild. Filter on demand with:
// GODOG_TAGS=@wip go test ./tests/e2e/ -run TestLexicalSearchAccountScope -v
func TestLexicalSearchAccountScope(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "lexical-search-account-scope",
		ScenarioInitializer: initLexicalSearchScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/lexical_search_account_scope.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("lexical search account-scope acceptance scenarios failed")
	}
}

// TestPostgresLexicalIndex runs the PostgreSQL full-text index scenarios
// (story wm-irqzji). It needs a real PostgreSQL server supplied through
// TEST_POSTGRES_DSN and skips cleanly without one; each scenario gets its own
// freshly created database on that server, dropped when the scenario ends.
// GODOG_TAGS=@wip TEST_POSTGRES_DSN=... go test ./tests/e2e/ -run TestPostgresLexicalIndex -v
func TestPostgresLexicalIndex(t *testing.T) {
	if os.Getenv("TEST_POSTGRES_DSN") == "" {
		t.Skip("TEST_POSTGRES_DSN not set — postgres lexical index scenarios skipped")
	}
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "postgres-lexical-index",
		ScenarioInitializer: initLexicalSearchScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/postgres_lexical_index.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("postgres lexical index acceptance scenarios failed")
	}
}

const (
	lexicalSubscriber     = "lexical-index"
	lexicalCatchUpTimeout = 30 * time.Second
)

// lexicalWorld boots the whole application — event store, in-process
// subscribers, MCP server with memory_search wired to the real lexical search
// — on SQLite or on a fresh PostgreSQL database. It reuses the memory world's
// fact steps, which drive resource_create / resource_update over MCP.
type lexicalWorld struct {
	*memoryWorld

	postgres   bool
	noFullText bool
	presets    []string

	search   application.LexicalSearch
	kg       application.KnowledgeGraphService
	cleanups []func()

	ids   map[string]string // name → resource id
	names map[string]string // resource id → name
	texts map[string]string // name → the text the resource was last written with

	// server is the MCP server; each account gets its own session on it,
	// whose context carries that account's identity.
	server   *mcp.Server
	accounts map[string]*acctSearchSession

	// comparison holds what the cross-engine steps read from each engine.
	pgEntries     map[string]lexicalEntry
	sqliteEntries map[string]lexicalEntry
}

type lexicalEntry struct {
	TypeSlug  string
	Content   string
	CreatedBy string
	AccountID string
}

// acctSearchSession is an MCP client session whose server side runs every
// tool call under one account's identity.
type acctSearchSession struct {
	client *mcp.ClientSession
	srv    *mcp.ServerSession
}

type memorySearchResult struct {
	Results []struct {
		ID       string `json:"id"`
		TypeSlug string `json:"typeSlug"`
	} `json:"results"`
	Mode string `json:"mode"`
}

func initLexicalSearchScenario(sc *godog.ScenarioContext) {
	w := &lexicalWorld{
		memoryWorld: &memoryWorld{mcpWorld: &mcpWorld{runWorkers: true}, facts: map[string]recordedFact{}},
		ids:         map[string]string{},
		names:       map[string]string{},
		texts:       map[string]string{},
		accounts:    map[string]*acctSearchSession{},
	}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardownAll()
		return ctx, nil
	})

	sc.Step(`^a clean WeOS knowledge graph$`, w.aCleanSQLiteInstance)
	sc.Step(`^a clean WeOS instance backed by a PostgreSQL database$`, w.aCleanPostgresInstance)
	sc.Step(`^the "([^"]*)" preset is installed$`, w.presetInstalled)
	sc.Step(`^the instance has no full-text index available$`, w.noFullTextIndex)
	sc.Step(`^the instance runs with no knowledge graph configured$`, w.noKnowledgeGraph)

	sc.Step(`^the following "([^"]*)" resources exist:$`, w.theFollowingResourcesExist)
	sc.Step(`^(\d+) "([^"]*)" resources whose description mentions "([^"]*)" exist$`, w.resourcesMentioning)
	sc.Step(`^the "([^"]*)" resource "([^"]*)" is updated with the description "([^"]*)"$`, w.resourceUpdated)
	sc.Step(`^the "([^"]*)" resource "([^"]*)" is deleted$`, w.resourceDeleted)
	sc.Step(`^a recorded fact "([^"]*)" stating "([^"]*)" about "([^"]*)"$`, w.aRecordedFact)
	sc.Step(`^I record a fact superseding "([^"]*)" stating "([^"]*)"$`, w.iRecordSupersedingFact)
	sc.Step(`^the lexical index is rebuilt from the event history$`, w.indexRebuilt)
	sc.Step(`^account "([^"]*)" has the following "([^"]*)" resources:$`, w.accountHasResources)
	sc.Step(`^account "([^"]*)" has (\d+) "([^"]*)" resources whose description mentions "([^"]*)"$`,
		w.accountHasResourcesMentioning)
	sc.Step(`^account "([^"]*)" updates its "([^"]*)" resource "([^"]*)" with the description "([^"]*)"$`,
		w.accountUpdatesResource)

	sc.Step(`^I call memory_search for "([^"]*)"$`, w.iSearch)
	sc.Step(`^I call memory_search for "([^"]*)" narrowed to type "([^"]*)"$`, w.iSearchNarrowed)
	sc.Step(`^I call memory_search for the query:$`, w.iSearchDocString)
	sc.Step(`^account "([^"]*)" calls memory_search for "([^"]*)"$`, w.accountSearches)
	sc.Step(`^account "([^"]*)" calls memory_search for "([^"]*)" narrowed to type "([^"]*)"$`,
		w.accountSearchesNarrowed)

	sc.Step(`^the call succeeds$`, w.theCallSucceeds)
	sc.Step(`^the search returns exactly these resources:$`, w.searchReturnsExactly)
	sc.Step(`^the search returns these resources in order:$`, w.searchReturnsInOrder)
	sc.Step(`^the search returns no resources$`, w.searchReturnsNone)
	sc.Step(`^the search returns no text from the "([^"]*)" resource "([^"]*)"$`, w.searchReturnsNoTextFrom)
	sc.Step(`^the search reports graph labels as its source$`, w.reportsGraphLabels)
	sc.Step(`^the search reports the full-text index as its source$`, w.reportsFullText)
	sc.Step(`^the index holds an entry for each resource a SQLite instance indexes from the same events$`,
		w.indexHoldsSQLiteEntries)
	sc.Step(`^each entry holds the same indexed text as the SQLite instance holds for that resource$`,
		w.entriesHoldSQLiteText)
}

// --- Boot ---

// Helper and Cleanup let the world stand in for testing.TB with testpg.
func (w *lexicalWorld) Helper()          {}
func (w *lexicalWorld) Cleanup(f func()) { w.cleanups = append(w.cleanups, f) }

func (w *lexicalWorld) aCleanSQLiteInstance(_ context.Context) error {
	w.postgres = false
	return w.boot()
}

func (w *lexicalWorld) aCleanPostgresInstance(_ context.Context) error {
	w.postgres = true
	return w.boot()
}

// instanceConfig builds the configuration for a fresh instance of the world's
// engine: a temp SQLite file, or a new database on the TEST_POSTGRES_DSN
// server. No knowledge graph is configured either way.
func (w *lexicalWorld) instanceConfig(postgres bool) (config.Config, error) {
	cfg := config.Default()
	cfg.LogLevel = "error"
	if lvl := os.Getenv("E2E_LOG_LEVEL"); lvl != "" {
		cfg.LogLevel = lvl
	}
	cfg.Oxigraph = config.OxigraphConfig{}
	cfg.Worker.RunInProcess = true
	if postgres {
		dsn, err := testpg.FreshDatabase(w, os.Getenv("TEST_POSTGRES_DSN"), "weos_lexical_e2e")
		if err != nil {
			return cfg, err
		}
		cfg.DatabaseDSN = dsn
		return cfg, nil
	}
	dir, err := os.MkdirTemp("", "weos-lexical-e2e-")
	if err != nil {
		return cfg, err
	}
	w.Cleanup(func() {
		// Best-effort: a leftover temp directory holds only this scenario's
		// throwaway database, and no later scenario reads it.
		_ = os.RemoveAll(dir)
	})
	cfg.DatabaseDSN = filepath.Join(dir, "test.db")
	return cfg, nil
}

func (w *lexicalWorld) boot() error {
	cfg, err := w.instanceConfig(w.postgres)
	if err != nil {
		return err
	}
	sessCtx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel

	var episodic application.EpisodicRecall
	opts := []fx.Option{
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Populate(&w.rts, &w.rs, &w.kg, &episodic, &w.eventStore, &w.manager, &w.search),
		fx.Populate(&w.db, &w.projMgr),
	}
	if w.noFullText {
		// An instance whose full-text index is unavailable answers from graph
		// labels. No graph store is built into this binary, so a label
		// search over the seeded resources' names stands in for one.
		opts = append(opts,
			fx.Decorate(func(repositories.LexicalIndex) repositories.LexicalIndex { return inactiveLexicalIndex{} }),
			fx.Decorate(func(inner application.KnowledgeGraphService) application.KnowledgeGraphService {
				return &nameLabelGraph{KnowledgeGraphService: inner, names: w.names}
			}),
		)
	}
	app := fx.New(opts...)
	startCtx, startCancel := context.WithTimeout(sessCtx, fx.DefaultTimeout)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}
	w.app = app

	server, err := mcpserver.NewMCPServer(w.rts, w.rs, w.kg, w.search, episodic, nil, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to build MCP server: %w", err)
	}
	w.server = server
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
	return nil
}

func (w *lexicalWorld) presetInstalled(ctx context.Context, name string) error {
	if err := w.presetIsInstalled(ctx, name); err != nil {
		return err
	}
	w.presets = append(w.presets, name)
	return nil
}

// noFullTextIndex reboots the instance on a fresh store with its full-text
// index unavailable, and installs the presets the Background installed.
func (w *lexicalWorld) noFullTextIndex(ctx context.Context) error {
	w.closeAccountSessions()
	w.teardown()
	w.app, w.client, w.srvSess, w.cancel = nil, nil, nil, nil
	w.noFullText = true
	if err := w.boot(); err != nil {
		return err
	}
	for _, p := range w.presets {
		if err := w.presetIsInstalled(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

func (w *lexicalWorld) noKnowledgeGraph() error {
	if w.kg != nil && w.kg.Active() {
		return fmt.Errorf("a knowledge graph is configured, but the scenario needs an instance without one")
	}
	return nil
}

func (w *lexicalWorld) teardownAll() {
	w.closeAccountSessions()
	w.teardown()
	for i := len(w.cleanups) - 1; i >= 0; i-- {
		w.cleanups[i]()
	}
	w.cleanups = nil
}

// --- Given ---

func (w *lexicalWorld) theFollowingResourcesExist(ctx context.Context, typeSlug string, table *godog.Table) error {
	if len(table.Rows) < 2 {
		return fmt.Errorf("the table needs a header row and at least one resource")
	}
	header := table.Rows[0].Cells
	for _, row := range table.Rows[1:] {
		data := map[string]any{}
		for i, cell := range row.Cells {
			if cell.Value != "" {
				data[header[i].Value] = cell.Value
			}
		}
		if _, err := w.create(ctx, typeSlug, data); err != nil {
			return err
		}
	}
	return nil
}

// resourcesMentioning seeds n resources whose description repeats the word,
// so each outranks a resource that mentions it once and the unnarrowed limit
// is spent on them.
func (w *lexicalWorld) resourcesMentioning(ctx context.Context, n int, typeSlug, word string) error {
	for i := 1; i <= n; i++ {
		data := map[string]any{
			"name": fmt.Sprintf("Billing workstream %02d", i),
			"description": fmt.Sprintf("%s schedule, %s reminders and %s follow-up for workstream %02d",
				word, word, word, i),
		}
		if _, err := w.create(ctx, typeSlug, data); err != nil {
			return err
		}
	}
	return nil
}

func (w *lexicalWorld) resourceUpdated(ctx context.Context, _, name, description string) error {
	id, ok := w.ids[name]
	if !ok {
		return fmt.Errorf("no seeded resource named %q", name)
	}
	raw, err := json.Marshal(map[string]any{"name": name, "description": description})
	if err != nil {
		return err
	}
	if _, err := w.rs.Update(ctx, application.UpdateResourceCommand{ID: id, Data: raw}); err != nil {
		return fmt.Errorf("update %q: %w", name, err)
	}
	w.texts[name] = name + " " + description
	return nil
}

func (w *lexicalWorld) resourceDeleted(ctx context.Context, _, name string) error {
	id, ok := w.ids[name]
	if !ok {
		return fmt.Errorf("no seeded resource named %q", name)
	}
	if err := w.rs.Delete(ctx, application.DeleteResourceCommand{ID: id}); err != nil {
		return fmt.Errorf("delete %q: %w", name, err)
	}
	return nil
}

// accountHasResources creates the table's resources as the named account, so
// each is owned by that account's agent exactly as one created over its own
// authenticated MCP session would be.
func (w *lexicalWorld) accountHasResources(
	ctx context.Context, account, typeSlug string, table *godog.Table,
) error {
	return w.theFollowingResourcesExist(lexicalAccountContext(ctx, account), typeSlug, table)
}

func (w *lexicalWorld) accountHasResourcesMentioning(
	ctx context.Context, account string, n int, typeSlug, word string,
) error {
	return w.resourcesMentioning(lexicalAccountContext(ctx, account), n, typeSlug, word)
}

func (w *lexicalWorld) accountUpdatesResource(
	ctx context.Context, account, typeSlug, name, description string,
) error {
	return w.resourceUpdated(lexicalAccountContext(ctx, account), typeSlug, name, description)
}

// indexRebuilt is the in-process equivalent of
// `weos worker checkpoint reset lexical-index --truncate`: clear the index,
// replay every event through the subscriber, and wait for it to finish.
func (w *lexicalWorld) indexRebuilt(ctx context.Context) error {
	if err := w.waitForIndex(ctx); err != nil {
		return err
	}
	if err := w.manager.ResetCheckpoint(ctx, lexicalSubscriber, true); err != nil {
		return fmt.Errorf("reset %s: %w", lexicalSubscriber, err)
	}
	return w.waitForIndex(ctx)
}

// --- When ---

func (w *lexicalWorld) iSearch(ctx context.Context, query string) error {
	return w.callSearch(ctx, query, nil)
}

func (w *lexicalWorld) iSearchNarrowed(ctx context.Context, query, typeSlug string) error {
	return w.callSearch(ctx, query, []string{typeSlug})
}

func (w *lexicalWorld) iSearchDocString(ctx context.Context, doc *godog.DocString) error {
	return w.callSearch(ctx, strings.TrimSpace(doc.Content), nil)
}

func (w *lexicalWorld) accountSearches(ctx context.Context, account, query string) error {
	return w.accountSearch(ctx, account, query, nil)
}

func (w *lexicalWorld) accountSearchesNarrowed(ctx context.Context, account, query, typeSlug string) error {
	return w.accountSearch(ctx, account, query, []string{typeSlug})
}

func (w *lexicalWorld) accountSearch(ctx context.Context, account, query string, typeSlugs []string) error {
	sess, err := w.sessionFor(account)
	if err != nil {
		return err
	}
	return w.callSearchOn(ctx, sess.client, query, typeSlugs)
}

// callSearch waits for the index subscriber to catch up with every write the
// scenario made, then calls memory_search the way an LLM client would.
func (w *lexicalWorld) callSearch(ctx context.Context, query string, typeSlugs []string) error {
	return w.callSearchOn(ctx, w.client, query, typeSlugs)
}

func (w *lexicalWorld) callSearchOn(
	ctx context.Context, client *mcp.ClientSession, query string, typeSlugs []string,
) error {
	if err := w.waitForIndex(ctx); err != nil {
		return err
	}
	args := map[string]any{"query": query}
	if len(typeSlugs) > 0 {
		args["typeSlugs"] = typeSlugs
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return err
	}
	res, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "memory_search", Arguments: json.RawMessage(raw)})
	w.lastResult = res
	w.lastErr = err
	w.lastText = textOf(res)
	return nil
}

// --- Then ---

func (w *lexicalWorld) result() (memorySearchResult, error) {
	var out memorySearchResult
	if err := w.theCallSucceeds(); err != nil {
		return out, err
	}
	if err := json.Unmarshal([]byte(w.lastText), &out); err != nil {
		return out, fmt.Errorf("unparseable memory_search output: %s", w.lastText)
	}
	return out, nil
}

// resultNames names each hit by its seeded name; a hit the scenario did not
// seed is shown by its id.
func (w *lexicalWorld) resultNames() ([]string, error) {
	out, err := w.result()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(out.Results))
	for _, r := range out.Results {
		if n, ok := w.names[r.ID]; ok {
			names = append(names, n)
		} else {
			names = append(names, r.ID)
		}
	}
	return names, nil
}

func tableNames(table *godog.Table) []string {
	want := make([]string, 0, len(table.Rows))
	for _, row := range table.Rows[1:] {
		want = append(want, row.Cells[0].Value)
	}
	return want
}

func (w *lexicalWorld) searchReturnsExactly(table *godog.Table) error {
	got, err := w.resultNames()
	if err != nil {
		return err
	}
	want := tableNames(table)
	gs := append([]string(nil), got...)
	ws := append([]string(nil), want...)
	sort.Strings(gs)
	sort.Strings(ws)
	if strings.Join(gs, "\n") != strings.Join(ws, "\n") {
		return fmt.Errorf("search returned %q, want exactly %q", got, want)
	}
	return nil
}

func (w *lexicalWorld) searchReturnsInOrder(table *godog.Table) error {
	got, err := w.resultNames()
	if err != nil {
		return err
	}
	want := tableNames(table)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		return fmt.Errorf("search returned %q, want in order %q", got, want)
	}
	return nil
}

func (w *lexicalWorld) searchReturnsNone() error {
	got, err := w.resultNames()
	if err != nil {
		return err
	}
	if len(got) != 0 {
		return fmt.Errorf("search returned %q, want no resources", got)
	}
	return nil
}

// searchReturnsNoTextFrom checks the whole memory_search payload, not only
// the result ids: a snippet quoting the resource is as much a leak as its id.
// It fails on the resource's id or on any word of four letters or more from
// the text the resource was written with.
func (w *lexicalWorld) searchReturnsNoTextFrom(_, name string) error {
	if err := w.theCallSucceeds(); err != nil {
		return err
	}
	id, ok := w.ids[name]
	if !ok {
		return fmt.Errorf("no seeded resource named %q", name)
	}
	payload := strings.ToLower(w.lastText)
	if strings.Contains(payload, strings.ToLower(id)) {
		return fmt.Errorf("the search payload names %s (%s): %s", id, name, w.lastText)
	}
	for _, word := range strings.Fields(w.texts[name]) {
		word = strings.ToLower(strings.Trim(word, ".,;:!?\"'()"))
		if len(word) >= 4 && strings.Contains(payload, word) {
			return fmt.Errorf("the search payload quotes %q from %q: %s", word, name, w.lastText)
		}
	}
	return nil
}

func (w *lexicalWorld) reportsGraphLabels() error {
	out, err := w.result()
	if err != nil {
		return err
	}
	if out.Mode != application.LexicalModeGraphLabels {
		return fmt.Errorf("mode = %q, want %q", out.Mode, application.LexicalModeGraphLabels)
	}
	return nil
}

func (w *lexicalWorld) reportsFullText() error {
	out, err := w.result()
	if err != nil {
		return err
	}
	want := application.LexicalModeFTS5
	if w.postgres {
		want = application.LexicalModePostgresFTS
	}
	if out.Mode != want {
		return fmt.Errorf("mode = %q, want the %s full-text index %q", out.Mode, engineName(w.postgres), want)
	}
	return nil
}

// indexHoldsSQLiteEntries builds a SQLite instance from this instance's
// events — the same events, copied aggregate by aggregate, with their ids and
// transaction ids — lets its subscriber index them, and compares the entries.
func (w *lexicalWorld) indexHoldsSQLiteEntries(ctx context.Context) error {
	pg, err := readLexicalEntries(w.db)
	if err != nil {
		return fmt.Errorf("read the %s index: %w", engineName(w.postgres), err)
	}
	sqlite, err := w.sqliteIndexFromSameEvents(ctx)
	if err != nil {
		return err
	}
	w.pgEntries, w.sqliteEntries = pg, sqlite
	if len(sqlite) == 0 {
		return fmt.Errorf("the SQLite instance indexed nothing; the comparison would be vacuous")
	}
	for id := range sqlite {
		if _, ok := pg[id]; !ok {
			return fmt.Errorf("the SQLite index holds %s (%s), the rebuilt index does not", id, w.names[id])
		}
	}
	for id := range pg {
		if _, ok := sqlite[id]; !ok {
			return fmt.Errorf("the rebuilt index holds %s (%s), the SQLite index does not", id, w.names[id])
		}
	}
	return nil
}

func (w *lexicalWorld) entriesHoldSQLiteText() error {
	if w.sqliteEntries == nil {
		return fmt.Errorf("no SQLite comparison was built by a preceding step")
	}
	for id, want := range w.sqliteEntries {
		got := w.pgEntries[id]
		if got != want {
			return fmt.Errorf("entry %s (%s): rebuilt index holds %+v, SQLite holds %+v", id, w.names[id], got, want)
		}
	}
	return nil
}

// --- Helpers ---

func engineName(postgres bool) string {
	if postgres {
		return "PostgreSQL"
	}
	return "SQLite"
}

func (w *lexicalWorld) create(ctx context.Context, typeSlug string, data map[string]any) (string, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	e, err := w.rs.Create(ctx, application.CreateResourceCommand{TypeSlug: typeSlug, Data: raw})
	if err != nil {
		return "", fmt.Errorf("failed to create %s %v: %w", typeSlug, data, err)
	}
	if name, ok := data["name"].(string); ok {
		w.ids[name] = e.GetID()
		w.names[e.GetID()] = name
		description, _ := data["description"].(string)
		w.texts[name] = strings.TrimSpace(name + " " + description)
	}
	return e.GetID(), nil
}

// lexicalAccountID is a stable synthetic account id for an account name.
func lexicalAccountID(name string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", "-"))
}

// lexicalAccountIdentity is the caller identity of an account: its own agent,
// active in its own account. Two accounts never share an agent, so what one
// creates is owned by an agent the other is not.
func lexicalAccountIdentity(name string) *auth.Identity {
	id := lexicalAccountID(name)
	return &auth.Identity{AgentID: "agent-" + id, AccountIDs: []string{id}, ActiveAccountID: id}
}

func lexicalAccountContext(ctx context.Context, name string) context.Context {
	return auth.ContextWithAgent(ctx, lexicalAccountIdentity(name))
}

// sessionFor returns the account's MCP session, opening it on first use. The
// identity is set on the server session's context, which is where the MCP
// server reads the caller of every tool call on that session.
func (w *lexicalWorld) sessionFor(account string) (*acctSearchSession, error) {
	if s, ok := w.accounts[account]; ok {
		return s, nil
	}
	if w.server == nil {
		return nil, fmt.Errorf("MCP server not booted")
	}
	ctx := lexicalAccountContext(context.Background(), account)
	serverT, clientT := mcp.NewInMemoryTransports()
	srv, err := w.server.Connect(ctx, serverT, nil)
	if err != nil {
		return nil, fmt.Errorf("connect %s server session: %w", account, err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "lexical-e2e", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		// Best-effort: the session never carried a call, and the step fails
		// with the connect error either way.
		_ = srv.Close()
		return nil, fmt.Errorf("connect %s client session: %w", account, err)
	}
	s := &acctSearchSession{client: cs, srv: srv}
	w.accounts[account] = s
	return s, nil
}

func (w *lexicalWorld) closeAccountSessions() {
	for _, s := range w.accounts {
		// Best-effort: a session closed at teardown has no call in flight,
		// and a close error leaves nothing a later scenario reads.
		_ = s.client.Close()
		_ = s.srv.Close()
	}
	w.accounts = map[string]*acctSearchSession{}
}

// waitForIndex returns once the lexical-index subscriber has processed every
// event in the store. An instance with no active index has no subscriber and
// nothing to wait for.
func (w *lexicalWorld) waitForIndex(ctx context.Context) error {
	return waitForSubscriber(ctx, w.manager, lexicalSubscriber)
}

func waitForSubscriber(ctx context.Context, manager *application.Manager, name string) error {
	if manager == nil {
		return fmt.Errorf("worker manager not booted")
	}
	registered := false
	for _, n := range manager.Names() {
		if n == name {
			registered = true
		}
	}
	if !registered {
		return nil
	}
	sub, err := manager.Subscriber(name)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(lexicalCatchUpTimeout)
	for {
		lag, err := sub.Lag(ctx)
		if err == nil && lag == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s subscriber did not catch up within %s (lag %d, err %v)",
				name, lexicalCatchUpTimeout, lag, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func readLexicalEntries(db *gorm.DB) (map[string]lexicalEntry, error) {
	var rows []struct {
		ID        string
		TypeSlug  string
		Content   string
		CreatedBy string
		AccountID string
	}
	if err := db.Raw("SELECT id, type_slug, content, created_by, account_id FROM resource_search").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]lexicalEntry, len(rows))
	for _, r := range rows {
		out[r.ID] = lexicalEntry{
			TypeSlug: r.TypeSlug, Content: r.Content, CreatedBy: r.CreatedBy, AccountID: r.AccountID,
		}
	}
	return out, nil
}

// sqliteIndexFromSameEvents boots a SQLite instance, appends this instance's
// resource events to its event store unchanged, waits for its lexical-index
// subscriber, and returns the entries it built.
func (w *lexicalWorld) sqliteIndexFromSameEvents(ctx context.Context) (map[string]lexicalEntry, error) {
	cfg, err := w.instanceConfig(false)
	if err != nil {
		return nil, err
	}
	var store pericarpdomain.EventStore
	var manager *application.Manager
	var db *gorm.DB
	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Populate(&store, &manager, &db),
	)
	startCtx, cancel := context.WithTimeout(ctx, fx.DefaultTimeout)
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		return nil, fmt.Errorf("failed to start the SQLite instance: %w", err)
	}
	w.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer stopCancel()
		// Best-effort: the scenario's assertions have already run, and a
		// failed stop leaves nothing a later scenario reads.
		_ = app.Stop(stopCtx)
	})

	ids := make([]string, 0, len(w.names))
	for id := range w.names {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		events, err := w.eventStore.GetEvents(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("read events of %s: %w", id, err)
		}
		if len(events) == 0 {
			continue
		}
		if err := store.Append(ctx, id, -1, events...); err != nil {
			return nil, fmt.Errorf("copy events of %s to SQLite: %w", id, err)
		}
	}
	if err := waitForSubscriber(ctx, manager, lexicalSubscriber); err != nil {
		return nil, fmt.Errorf("SQLite instance: %w", err)
	}
	return readLexicalEntries(db)
}

// inactiveLexicalIndex is an index that is not available on this instance.
type inactiveLexicalIndex struct{}

func (inactiveLexicalIndex) Active() bool   { return false }
func (inactiveLexicalIndex) Engine() string { return "" }
func (inactiveLexicalIndex) Index(context.Context, string, string, string, repositories.LexicalOwner) error {
	return nil
}
func (inactiveLexicalIndex) Remove(context.Context, string) error { return nil }
func (inactiveLexicalIndex) Owner(context.Context, string) (repositories.LexicalOwner, bool, error) {
	return repositories.LexicalOwner{}, false, nil
}
func (inactiveLexicalIndex) Clear(context.Context) error        { return nil }
func (inactiveLexicalIndex) Lag(context.Context) (int64, error) { return 0, nil }
func (inactiveLexicalIndex) Search(
	context.Context, string, int, *repositories.VisibilityScope, ...string,
) ([]repositories.LexicalHit, error) {
	return nil, nil
}

// nameLabelGraph answers label search the way the graph store does — a
// case-insensitive substring of a resource's name — over the resources the
// scenario seeded. It ignores the class filter, so any narrowing comes from
// the lexical search under test, not from the stand-in.
type nameLabelGraph struct {
	application.KnowledgeGraphService
	names map[string]string
}

func (g *nameLabelGraph) Active() bool { return true }

func (g *nameLabelGraph) SearchEntities(
	_ context.Context, q, _ string, limit int,
) ([]repositories.KGTerm, error) {
	ids := make([]string, 0, len(g.names))
	for id := range g.names {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []repositories.KGTerm
	for _, id := range ids {
		if strings.Contains(strings.ToLower(g.names[id]), strings.ToLower(q)) {
			out = append(out, repositories.KGTerm{Value: id})
		}
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

// Query answers the supersession check with no invalidated facts.
func (g *nameLabelGraph) Query(context.Context, string) (repositories.KGQueryResult, error) {
	return repositories.KGQueryResult{}, nil
}
