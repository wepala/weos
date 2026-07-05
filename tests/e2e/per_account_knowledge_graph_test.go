//go:build oxigraph_embedded

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"
	mcpserver "github.com/wepala/weos/v3/internal/mcp"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	pericarpdomain "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/cucumber/godog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/fx"
)

// TestPerAccountKnowledgeGraph runs the per-account isolation acceptance
// scenarios (story #431). Like TestEmbeddedKnowledgeGraph it needs the
// oxigraph_embedded build tag and the vendored lib — run via
// `make test-graph-embedded` or explicitly with
// `-tags oxigraph_embedded -run TestPerAccountKnowledgeGraph`.
func TestPerAccountKnowledgeGraph(t *testing.T) {
	tags := os.Getenv("GODOG_TAGS") // empty runs every scenario, including @wip
	suite := godog.TestSuite{
		Name:                "per-account-knowledge-graph",
		ScenarioInitializer: initPerAccountKGScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/per_account_knowledge_graph.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("per-account knowledge-graph acceptance scenarios failed")
	}
}

// localGraphKey is the urns-map key for resources owned by the reserved local
// graph (the stdio exception), kept distinct from any real account name.
const localGraphKey = "__local__"

// acctSession is one MCP client/server session pair bound to a caller identity
// via the server session's connect context (the in-memory transport roots the
// handler context there, and context values propagate to the tool handler).
type acctSession struct {
	client *mcp.ClientSession
	srv    *mcp.ServerSession
}

// perAccountKGWorld drives the per-account scenarios. It boots ONE per-account
// twin + MCP server and opens a distinct identity-bearing session per account,
// so a kg_* call routes to the caller account's own store. It embeds kgWorld to
// reuse the single-tenant steps (scenario 9) and the shared helpers/assertions.
type perAccountKGWorld struct {
	kgWorld
	graphBase string
	server    *mcp.Server
	sessCtx   context.Context

	sessions map[string]*acctSession // accountName -> identity-bearing session
	local    *acctSession            // no identity + local-transport marker
	remote   *acctSession            // no identity, no marker (a remote caller)

	// urnsByAccount maps accountName (or localGraphKey) -> resourceName -> URN,
	// so identically-named resources in different accounts stay distinguishable.
	urnsByAccount map[string]map[string]string
	bulkAccounts  []string

	rerun func() // re-executes the last kg action, refreshing lastText
}

func initPerAccountKGScenario(sc *godog.ScenarioContext) {
	w := &perAccountKGWorld{
		sessions:      map[string]*acctSession{},
		urnsByAccount: map[string]map[string]string{},
	}
	w.urns = map[string]string{}
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	// Boot (per-account modes all boot the same twin; the transport distinction
	// lives in which session a request uses).
	sc.Step(`^a per-account WeOS twin with embedded knowledge-graph stores$`, w.perAccountTwin)
	sc.Step(`^a per-account WeOS twin serving the remote HTTP MCP transport$`, w.perAccountTwin)
	sc.Step(`^a per-account WeOS twin serving the local stdio MCP transport$`, w.perAccountTwin)

	// Reused single-tenant steps (scenario 9) + shared preset step.
	sc.Step(`^a WeOS twin with an embedded knowledge-graph store$`, w.twinEmbedded)
	sc.Step(`^the "([^"]*)" preset is installed$`, w.presetIsInstalled)
	sc.Step(`^a "([^"]*)" named "([^"]*)" exists$`, w.resourceExists)
	sc.Step(`^I run the SPARQL query:$`, w.iRunSPARQL)
	sc.Step(`^the query results include the "([^"]*)" resource "([^"]*)"$`, w.queryIncludes)

	// Per-account seeding.
	sc.Step(`^account "([^"]*)" has a "([^"]*)" named "([^"]*)"$`, w.accountHasResource)
	sc.Step(`^account "([^"]*)" has a "([^"]*)" named "([^"]*)" linked to the project "([^"]*)"$`, w.accountHasLinked)
	sc.Step(`^account "([^"]*)" has no resources in the knowledge graph$`, w.accountHasNoResources)
	sc.Step(`^a local request with no resolvable account has a "([^"]*)" named "([^"]*)" in the local graph$`, w.localHasResource)
	sc.Step(`^(\d+) accounts each have their own "([^"]*)" in the knowledge graph$`, w.bulkAccountsSeed)

	// Per-account actions.
	sc.Step(`^account "([^"]*)" searches the knowledge graph for "([^"]*)"$`, w.accountSearches)
	sc.Step(`^account "([^"]*)" runs the SPARQL query:$`, w.accountSPARQL)
	sc.Step(`^account "([^"]*)" expands its task "([^"]*)" in the knowledge graph$`, w.accountExpandsOwnTask)
	sc.Step(`^account "([^"]*)" expands the project "([^"]*)" owned by account "([^"]*)"$`, w.accountExpandsForeign)
	sc.Step(`^account "([^"]*)" lists the knowledge graph classes$`, w.accountListsClasses)
	sc.Step(`^account "([^"]*)" describes the "([^"]*)" class with example instances$`, w.accountDescribesClass)
	sc.Step(`^account "([^"]*)" looks for a path from its task "([^"]*)" to its project "([^"]*)"$`, w.accountFindsPath)
	sc.Step(`^account "([^"]*)" deletes its project "([^"]*)"$`, w.accountDeletes)
	sc.Step(`^a remote request with no resolvable account searches the knowledge graph for "([^"]*)"$`, w.remoteSearches)
	sc.Step(`^the local request searches the knowledge graph for "([^"]*)"$`, w.localSearches)
	sc.Step(`^the knowledge-graph projection is rebuilt from event history$`, w.rebuildProjection)
	sc.Step(`^the twin restarts against the same embedded stores$`, w.restartPerAccount)

	// Per-account assertions.
	sc.Step(`^the search results include the "([^"]*)" resource "([^"]*)" owned by account "([^"]*)"$`, w.searchIncludesOwned)
	sc.Step(`^the search results exclude the "([^"]*)" resource "([^"]*)" owned by account "([^"]*)"$`, w.searchExcludesOwned)
	sc.Step(`^the search results include the "([^"]*)" resource "([^"]*)" owned by the local graph$`, w.searchIncludesLocal)
	sc.Step(`^the neighborhood includes the project "([^"]*)" owned by account "([^"]*)"$`, w.neighborhoodIncludesOwned)
	sc.Step(`^the neighborhood excludes the project "([^"]*)" owned by account "([^"]*)"$`, w.neighborhoodExcludesOwned)
	sc.Step(`^the neighborhood is empty$`, w.neighborhoodIsEmpty)
	sc.Step(`^the query results include the "([^"]*)" resource "([^"]*)" owned by account "([^"]*)"$`, w.queryIncludesOwned)
	sc.Step(`^the query results exclude the "([^"]*)" resource "([^"]*)" owned by account "([^"]*)"$`, w.queryExcludesOwned)
	sc.Step(`^no classes are listed$`, w.noClassesListed)
	sc.Step(`^the example instances include the "([^"]*)" resource "([^"]*)" owned by account "([^"]*)"$`, w.instancesIncludeOwned)
	sc.Step(`^the example instances exclude the "([^"]*)" resource "([^"]*)" owned by account "([^"]*)"$`, w.instancesExcludeOwned)
	sc.Step(`^no path is found$`, w.noPathFound)
	sc.Step(`^the knowledge graph no longer returns the "([^"]*)" resource "([^"]*)" owned by account "([^"]*)"$`, w.graphNoLongerReturnsOwned)
	sc.Step(`^the knowledge graph still returns the "([^"]*)" resource "([^"]*)" owned by account "([^"]*)"$`, w.graphStillReturnsOwned)
	sc.Step(`^the knowledge graph does not return the "([^"]*)" resource "([^"]*)" owned by account "([^"]*)"$`, w.graphDoesNotReturnOwned)
	sc.Step(`^the knowledge graph reports it is not configured$`, w.reportsNotConfiguredLast)
	sc.Step(`^no entities are returned$`, w.noEntitiesReturned)
	sc.Step(`^account "([^"]*)" does not see the "([^"]*)" resource "([^"]*)" owned by the local graph$`, w.accountDoesNotSeeLocal)
	sc.Step(`^the knowledge graph returns each account's own project$`, w.eachAccountSeesOwn)
	sc.Step(`^no account sees another account's project$`, w.noAccountSeesOthers)
	sc.Step(`^the twin reports no knowledge-graph store lock or file-handle errors$`, w.noLockOrHandleErrors)
}

// --- Boot / lifecycle ---

func (w *perAccountKGWorld) perAccountTwin(_ context.Context) error {
	dir, err := w.newTmp()
	if err != nil {
		return err
	}
	w.graphBase = filepath.Join(dir, "graph")
	return w.startPerAccountApp()
}

func (w *perAccountKGWorld) startPerAccountApp() error {
	cfg := w.baseConfig(w.tmpDir)
	cfg.Oxigraph.AccountStorePath = w.graphBase
	return w.bootPerAccount(cfg)
}

func (w *perAccountKGWorld) bootPerAccount(cfg config.Config) error {
	sessCtx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.sessCtx = sessCtx

	var rts application.ResourceTypeService
	var rs application.ResourceService
	var kg application.KnowledgeGraphService
	var episodic application.EpisodicRecall
	var eventStore pericarpdomain.EventStore
	var manager *application.Manager

	// Capture logs across restarts so the lock/handle-error assertion sees the
	// whole run; reuse one logger instance so a restart accumulates rather than
	// resets.
	if w.logs == nil {
		w.logs = &captureLogger{}
	}
	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, presets.NewDefaultRegistry()),
		fx.Populate(&rts, &rs, &kg, &episodic, &eventStore, &manager),
		fx.Decorate(func(entities.Logger) entities.Logger { return w.logs }),
	)
	startCtx, startCancel := context.WithTimeout(sessCtx, fx.DefaultTimeout)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("start per-account app: %w", err)
	}
	w.app = app
	w.eventStore = eventStore
	w.manager = manager
	w.rts = rts

	server, err := mcpserver.NewMCPServer(rts, rs, kg, nil, episodic, nil)
	if err != nil {
		return fmt.Errorf("build MCP server: %w", err)
	}
	w.server = server
	return nil
}

// connectSession opens a client/server session pair rooted at ctx. The server
// session's context carries the caller identity that every tool call on the
// paired client will run under.
func (w *perAccountKGWorld) connectSession(ctx context.Context) (*acctSession, error) {
	serverT, clientT := mcp.NewInMemoryTransports()
	srv, err := w.server.Connect(ctx, serverT, nil)
	if err != nil {
		return nil, fmt.Errorf("connect server session: %w", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "kg-pa-e2e", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		_ = srv.Close()
		return nil, fmt.Errorf("connect client session: %w", err)
	}
	return &acctSession{client: cs, srv: srv}, nil
}

// sessionFor returns the identity-bearing session for an account, opening it on
// first use. The identity is synthetic but stable per name: a distinct agent and
// account id (a slug of the name) so the projector routes writes to
// <base>/graph/<slug> and the per-agent permission filter passes the account's
// own resources.
func (w *perAccountKGWorld) sessionFor(accountName string) (*acctSession, error) {
	if s, ok := w.sessions[accountName]; ok {
		return s, nil
	}
	id := accountSlug(accountName)
	ctx := auth.ContextWithAgent(w.sessCtx, &auth.Identity{
		AgentID:         "agent-" + id,
		AccountIDs:      []string{id},
		ActiveAccountID: id,
	})
	s, err := w.connectSession(ctx)
	if err != nil {
		return nil, err
	}
	w.sessions[accountName] = s
	return s, nil
}

func (w *perAccountKGWorld) localSession() (*acctSession, error) {
	if w.local != nil {
		return w.local, nil
	}
	s, err := w.connectSession(application.WithLocalTransport(w.sessCtx))
	if err != nil {
		return nil, err
	}
	w.local = s
	return s, nil
}

func (w *perAccountKGWorld) remoteSession() (*acctSession, error) {
	if w.remote != nil {
		return w.remote, nil
	}
	s, err := w.connectSession(w.sessCtx) // no identity, no local marker
	if err != nil {
		return nil, err
	}
	w.remote = s
	return s, nil
}

func (w *perAccountKGWorld) teardown() {
	w.closeSessions()
	w.kgWorld.teardown()
}

func (w *perAccountKGWorld) closeSessions() {
	for _, s := range w.sessions {
		closeSession(s)
	}
	w.sessions = map[string]*acctSession{}
	closeSession(w.local)
	closeSession(w.remote)
	w.local, w.remote = nil, nil
}

func closeSession(s *acctSession) {
	if s == nil {
		return
	}
	if s.client != nil {
		_ = s.client.Close()
	}
	if s.srv != nil {
		_ = s.srv.Close()
	}
}

// stopApp tears down the running app + sessions without removing tmpDir, so a
// restart can reopen the same database and stores.
func (w *perAccountKGWorld) stopApp() {
	w.closeSessions()
	if w.cancel != nil {
		w.cancel()
	}
	if w.app != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		_ = w.app.Stop(stopCtx)
		cancel()
		w.app = nil
	}
}

func (w *perAccountKGWorld) restartPerAccount(_ context.Context) error {
	w.stopApp()
	return w.startPerAccountApp()
}

// rebuildProjection restarts with OXIGRAPH_REBUILD, so the manager resets the
// oxigraph checkpoint and replays the whole feed BEFORE the subscriber launches
// (the race-free rebuild path), routing each event back into its account store.
func (w *perAccountKGWorld) rebuildProjection(_ context.Context) error {
	w.stopApp()
	cfg := w.baseConfig(w.tmpDir)
	cfg.Oxigraph.AccountStorePath = w.graphBase
	cfg.Oxigraph.Rebuild = true
	return w.bootPerAccount(cfg)
}

// --- Seeding ---

func (w *perAccountKGWorld) accountHasResource(ctx context.Context, accountName, typeSlug, name string) error {
	return w.seed(ctx, accountName, typeSlug, name, "")
}

func (w *perAccountKGWorld) accountHasLinked(ctx context.Context, accountName, typeSlug, name, projName string) error {
	// The task links to a project in the SAME account; create the project first
	// if this account doesn't have it yet, so the link resolves within the store.
	projURN := w.urnFor(accountName, projName)
	if projURN == "" {
		if err := w.seed(ctx, accountName, "project", projName, ""); err != nil {
			return err
		}
		projURN = w.urnFor(accountName, projName)
	}
	return w.seed(ctx, accountName, typeSlug, name, projURN)
}

func (w *perAccountKGWorld) accountHasNoResources(_ context.Context, accountName string) error {
	// Ensure the account's session (and thus its empty store on first query) is
	// known; nothing to seed.
	_, err := w.sessionFor(accountName)
	return err
}

func (w *perAccountKGWorld) localHasResource(ctx context.Context, typeSlug, name string) error {
	sess, err := w.localSession()
	if err != nil {
		return err
	}
	return w.seedOn(ctx, sess, localGraphKey, typeSlug, name, "")
}

func (w *perAccountKGWorld) bulkAccountsSeed(ctx context.Context, count int, typeSlug string) error {
	w.bulkAccounts = w.bulkAccounts[:0]
	for i := 1; i <= count; i++ {
		acct := fmt.Sprintf("acct-%02d", i)
		w.bulkAccounts = append(w.bulkAccounts, acct)
		if err := w.seed(ctx, acct, typeSlug, fmt.Sprintf("Project %02d", i), ""); err != nil {
			return err
		}
	}
	return nil
}

func (w *perAccountKGWorld) seed(ctx context.Context, accountName, typeSlug, name, projURN string) error {
	sess, err := w.sessionFor(accountName)
	if err != nil {
		return err
	}
	return w.seedOn(ctx, sess, accountName, typeSlug, name, projURN)
}

func (w *perAccountKGWorld) seedOn(
	ctx context.Context, sess *acctSession, ownerKey, typeSlug, name, projURN string,
) error {
	data := resourceData(typeSlug, name, projURN)
	res, err := sess.client.CallTool(ctx, &mcp.CallToolParams{
		Name:      "resource_create",
		Arguments: json.RawMessage(fmt.Sprintf(`{"type_slug":%q,"data":%s}`, typeSlug, data)),
	})
	if err != nil {
		return fmt.Errorf("resource_create protocol error: %w", err)
	}
	if res.IsError {
		return fmt.Errorf("resource_create failed for %s/%s: %s", ownerKey, name, textOf(res))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(textOf(res)), &m); err != nil {
		return fmt.Errorf("resource_create result not JSON: %w", err)
	}
	id, _ := m["id"].(string)
	if id == "" {
		return fmt.Errorf("resource_create returned no id for %s/%s: %s", ownerKey, name, textOf(res))
	}
	w.rememberURN(ownerKey, name, id)
	return nil
}

func resourceData(typeSlug, name, projURN string) string {
	if typeSlug == "task" {
		data := fmt.Sprintf(`{"name":%q,"status":"open","priority":"medium"`, name)
		if projURN != "" {
			data += fmt.Sprintf(`,"project":%q`, projURN)
		}
		return data + `}`
	}
	return fmt.Sprintf(`{"name":%q,"description":"e2e","status":"active"}`, name)
}

// --- Actions ---

func (w *perAccountKGWorld) callOn(ctx context.Context, sess *acctSession, tool, args string) {
	res, err := sess.client.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: json.RawMessage(args)})
	w.lastResult, w.lastErr, w.lastText = res, err, textOf(res)
}

func (w *perAccountKGWorld) accountSearches(ctx context.Context, accountName, q string) error {
	sess, err := w.sessionFor(accountName)
	if err != nil {
		return err
	}
	w.rerun = func() { w.callOn(ctx, sess, "kg_search_entities", fmt.Sprintf(`{"q":%q}`, q)) }
	w.rerun()
	return nil
}

func (w *perAccountKGWorld) accountSPARQL(ctx context.Context, accountName string, doc *godog.DocString) error {
	sess, err := w.sessionFor(accountName)
	if err != nil {
		return err
	}
	query := strings.TrimSpace(doc.Content)
	w.rerun = func() { w.callOn(ctx, sess, "kg_sparql_query", fmt.Sprintf(`{"query":%q}`, query)) }
	w.rerun()
	return nil
}

func (w *perAccountKGWorld) accountExpandsOwnTask(ctx context.Context, accountName, taskName string) error {
	sess, err := w.sessionFor(accountName)
	if err != nil {
		return err
	}
	iri := w.urnFor(accountName, taskName)
	if iri == "" {
		return fmt.Errorf("account %q has no task %q", accountName, taskName)
	}
	w.rerun = func() { w.callOn(ctx, sess, "kg_expand_entity", fmt.Sprintf(`{"iri":%q}`, iri)) }
	w.rerun()
	return nil
}

func (w *perAccountKGWorld) accountExpandsForeign(ctx context.Context, caller, projName, owner string) error {
	iri := w.urnFor(owner, projName)
	if iri == "" {
		return fmt.Errorf("account %q has no project %q to target", owner, projName)
	}
	// Prove isolation, not timing: wait until the owner's own store has the
	// project projected, THEN read it through the other account's session.
	ownerSess, err := w.sessionFor(owner)
	if err != nil {
		return err
	}
	if err := w.poll("owner "+owner+" to project "+projName, func() bool {
		w.callOn(ctx, ownerSess, "kg_expand_entity", fmt.Sprintf(`{"iri":%q}`, iri))
		return tripleSubjectPresent(w.lastText, iri)
	}); err != nil {
		return err
	}
	callerSess, err := w.sessionFor(caller)
	if err != nil {
		return err
	}
	w.rerun = func() { w.callOn(ctx, callerSess, "kg_expand_entity", fmt.Sprintf(`{"iri":%q}`, iri)) }
	w.rerun()
	return nil
}

func (w *perAccountKGWorld) accountListsClasses(ctx context.Context, accountName string) error {
	sess, err := w.sessionFor(accountName)
	if err != nil {
		return err
	}
	w.rerun = func() { w.callOn(ctx, sess, "kg_list_classes", `{}`) }
	w.rerun()
	return nil
}

func (w *perAccountKGWorld) accountDescribesClass(ctx context.Context, accountName, classSlug string) error {
	sess, err := w.sessionFor(accountName)
	if err != nil {
		return err
	}
	// Discover the class IRI from the account's own graph (async ontology), then
	// describe it with example instances.
	var classIRI string
	if err := w.poll(accountName+" to have the "+classSlug+" class", func() bool {
		w.callOn(ctx, sess, "kg_list_classes", `{}`)
		classIRI = classIRIMatching(w.lastText, classSlug)
		return classIRI != ""
	}); err != nil {
		return err
	}
	w.rerun = func() {
		w.callOn(ctx, sess, "kg_describe_class",
			fmt.Sprintf(`{"class_iri":%q,"sample_instances":25}`, classIRI))
	}
	w.rerun()
	return nil
}

func (w *perAccountKGWorld) accountFindsPath(ctx context.Context, accountName, taskName, projName string) error {
	sess, err := w.sessionFor(accountName)
	if err != nil {
		return err
	}
	from := w.urnFor(accountName, taskName)
	to := w.urnFor(accountName, projName)
	if from == "" || to == "" {
		return fmt.Errorf("account %q missing task %q or project %q", accountName, taskName, projName)
	}
	// Let projection settle so a "no path" result reflects the real (unlinked)
	// graph rather than an empty one that hasn't caught up.
	if err := w.poll(accountName+" to project "+taskName, func() bool {
		w.callOn(ctx, sess, "kg_expand_entity", fmt.Sprintf(`{"iri":%q}`, from))
		return tripleSubjectPresent(w.lastText, from)
	}); err != nil {
		return err
	}
	w.callOn(ctx, sess, "kg_find_path", fmt.Sprintf(`{"from":%q,"to":%q}`, from, to))
	return nil
}

func (w *perAccountKGWorld) accountDeletes(ctx context.Context, accountName, projName string) error {
	sess, err := w.sessionFor(accountName)
	if err != nil {
		return err
	}
	urn := w.urnFor(accountName, projName)
	if urn == "" {
		return fmt.Errorf("account %q has no project %q", accountName, projName)
	}
	// Ensure the resource is projected before deleting, so the delete has
	// something to remove and the assertion isn't racing the create.
	if err := w.poll(accountName+" to project "+projName, func() bool {
		w.callOn(ctx, sess, "kg_search_entities", fmt.Sprintf(`{"q":%q}`, projName))
		return searchHasValue(w.lastText, urn)
	}); err != nil {
		return err
	}
	res, err := sess.client.CallTool(ctx, &mcp.CallToolParams{
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

func (w *perAccountKGWorld) remoteSearches(ctx context.Context, q string) error {
	sess, err := w.remoteSession()
	if err != nil {
		return err
	}
	w.callOn(ctx, sess, "kg_search_entities", fmt.Sprintf(`{"q":%q}`, q))
	return nil
}

func (w *perAccountKGWorld) localSearches(ctx context.Context, q string) error {
	sess, err := w.localSession()
	if err != nil {
		return err
	}
	w.rerun = func() { w.callOn(ctx, sess, "kg_search_entities", fmt.Sprintf(`{"q":%q}`, q)) }
	w.rerun()
	return nil
}

// --- Assertions ---

func (w *perAccountKGWorld) searchIncludesOwned(_ context.Context, _, name, owner string) error {
	return w.pollInclude("search to include "+name+" of "+owner, w.urnFor(owner, name), searchHasValue)
}

func (w *perAccountKGWorld) searchExcludesOwned(_ context.Context, _, name, owner string) error {
	return w.assertExcluded(w.urnFor(owner, name), searchHasValue, "search")
}

func (w *perAccountKGWorld) searchIncludesLocal(_ context.Context, _, name string) error {
	return w.pollInclude("local search to include "+name, w.urnFor(localGraphKey, name), searchHasValue)
}

func (w *perAccountKGWorld) neighborhoodIncludesOwned(_ context.Context, name, owner string) error {
	return w.pollInclude("neighborhood to include "+name+" of "+owner, w.urnFor(owner, name), tripleObjectPresent)
}

func (w *perAccountKGWorld) neighborhoodExcludesOwned(_ context.Context, name, owner string) error {
	return w.assertExcluded(w.urnFor(owner, name), tripleObjectPresent, "neighborhood")
}

func (w *perAccountKGWorld) neighborhoodIsEmpty(_ context.Context) error {
	if n := len(parseTriples(w.lastText)); n != 0 {
		return fmt.Errorf("expected an empty neighborhood, got %d triples: %s", n, w.lastText)
	}
	return nil
}

func (w *perAccountKGWorld) queryIncludesOwned(_ context.Context, _, name, owner string) error {
	return w.pollInclude("query to include "+name+" of "+owner, w.urnFor(owner, name), bindingsHaveValue)
}

func (w *perAccountKGWorld) queryExcludesOwned(_ context.Context, _, name, owner string) error {
	return w.assertExcluded(w.urnFor(owner, name), bindingsHaveValue, "query")
}

func (w *perAccountKGWorld) noClassesListed(_ context.Context) error {
	if cs := parseClasses(w.lastText); len(cs) != 0 {
		return fmt.Errorf("expected no classes for the empty account, got %v", cs)
	}
	return nil
}

func (w *perAccountKGWorld) instancesIncludeOwned(_ context.Context, _, name, owner string) error {
	return w.pollInclude("instances to include "+name+" of "+owner, w.urnFor(owner, name), instancesHaveValue)
}

func (w *perAccountKGWorld) instancesExcludeOwned(_ context.Context, _, name, owner string) error {
	return w.assertExcluded(w.urnFor(owner, name), instancesHaveValue, "instances")
}

func (w *perAccountKGWorld) noPathFound(_ context.Context) error {
	if n := len(parseTriples(w.lastText)); n != 0 {
		return fmt.Errorf("expected no path, got %d triples: %s", n, w.lastText)
	}
	return nil
}

func (w *perAccountKGWorld) graphNoLongerReturnsOwned(ctx context.Context, _, name, owner string) error {
	sess, err := w.sessionFor(owner)
	if err != nil {
		return err
	}
	urn := w.urnFor(owner, name)
	return w.poll("graph to drop "+name+" of "+owner, func() bool {
		w.callOn(ctx, sess, "kg_expand_entity", fmt.Sprintf(`{"iri":%q}`, urn))
		return !tripleSubjectPresent(w.lastText, urn)
	})
}

func (w *perAccountKGWorld) graphStillReturnsOwned(ctx context.Context, _, name, owner string) error {
	sess, err := w.sessionFor(owner)
	if err != nil {
		return err
	}
	urn := w.urnFor(owner, name)
	return w.poll("graph to still return "+name+" of "+owner, func() bool {
		w.callOn(ctx, sess, "kg_search_entities", fmt.Sprintf(`{"q":%q}`, name))
		return searchHasValue(w.lastText, urn)
	})
}

// graphDoesNotReturnOwned asserts a resource of one account is absent from a
// DIFFERENT account's graph (isolation holds after a rebuild). The urn is the
// owner's; we query the account named in the step, which must not see it.
func (w *perAccountKGWorld) graphDoesNotReturnOwned(ctx context.Context, _, name, viewer string) error {
	sess, err := w.sessionFor(viewer)
	if err != nil {
		return err
	}
	// The resource belongs to whichever account actually created it; find its
	// URN wherever it was remembered.
	urn := w.anyURN(name)
	// Give projection time to catch up, then confirm the viewer never sees it.
	deadline := time.Now().Add(3 * time.Second)
	for {
		w.callOn(ctx, sess, "kg_search_entities", fmt.Sprintf(`{"q":%q}`, name))
		if searchHasValue(w.lastText, urn) {
			return fmt.Errorf("account %q unexpectedly sees %q (%s)", viewer, name, urn)
		}
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (w *perAccountKGWorld) reportsNotConfiguredLast(_ context.Context) error {
	if w.lastResult == nil || !w.lastResult.IsError {
		return fmt.Errorf("expected the kg tool to report the graph unavailable, got: %s", w.lastText)
	}
	if !strings.Contains(strings.ToLower(w.lastText), "not configured") {
		return fmt.Errorf("expected a 'not configured' message, got: %s", w.lastText)
	}
	return nil
}

func (w *perAccountKGWorld) noEntitiesReturned(_ context.Context) error {
	if vs := parseSearchValues(w.lastText); len(vs) != 0 {
		return fmt.Errorf("expected no entities, got %v", vs)
	}
	return nil
}

func (w *perAccountKGWorld) accountDoesNotSeeLocal(ctx context.Context, accountName, _, name string) error {
	sess, err := w.sessionFor(accountName)
	if err != nil {
		return err
	}
	urn := w.urnFor(localGraphKey, name)
	deadline := time.Now().Add(3 * time.Second)
	for {
		w.callOn(ctx, sess, "kg_search_entities", fmt.Sprintf(`{"q":%q}`, name))
		if searchHasValue(w.lastText, urn) {
			return fmt.Errorf("account %q unexpectedly sees local resource %q", accountName, name)
		}
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (w *perAccountKGWorld) eachAccountSeesOwn(ctx context.Context) error {
	for i, acct := range w.bulkAccounts {
		name := fmt.Sprintf("Project %02d", i+1)
		sess, err := w.sessionFor(acct)
		if err != nil {
			return err
		}
		urn := w.urnFor(acct, name)
		if err := w.poll(acct+" to see its own "+name, func() bool {
			w.callOn(ctx, sess, "kg_search_entities", fmt.Sprintf(`{"q":%q}`, name))
			return searchHasValue(w.lastText, urn)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (w *perAccountKGWorld) noAccountSeesOthers(ctx context.Context) error {
	if len(w.bulkAccounts) < 2 {
		return nil
	}
	// Spot-check a few accounts don't see a neighbor's project.
	for _, i := range []int{0, len(w.bulkAccounts) / 2, len(w.bulkAccounts) - 1} {
		acct := w.bulkAccounts[i]
		other := (i + 1) % len(w.bulkAccounts)
		otherName := fmt.Sprintf("Project %02d", other+1)
		sess, err := w.sessionFor(acct)
		if err != nil {
			return err
		}
		w.callOn(ctx, sess, "kg_search_entities", fmt.Sprintf(`{"q":%q}`, otherName))
		if searchHasValue(w.lastText, w.urnFor(w.bulkAccounts[other], otherName)) {
			return fmt.Errorf("account %q unexpectedly sees %q", acct, otherName)
		}
	}
	return nil
}

func (w *perAccountKGWorld) noLockOrHandleErrors(_ context.Context) error {
	if w.logs == nil {
		return nil
	}
	for _, m := range w.logs.snapshot() {
		lower := strings.ToLower(m)
		if strings.Contains(lower, "lock") || strings.Contains(lower, "file handle") ||
			strings.Contains(lower, "too many open files") {
			return fmt.Errorf("found a store lock/file-handle error: %q", m)
		}
	}
	return nil
}

// --- shared assertion plumbing ---

type valuePresent func(text, want string) bool

// pollInclude re-runs the last kg action until the wanted URN appears in the
// asserted field (projection is async).
func (w *perAccountKGWorld) pollInclude(desc, urn string, present valuePresent) error {
	if urn == "" {
		return fmt.Errorf("%s: no known URN", desc)
	}
	if w.rerun == nil {
		return fmt.Errorf("%s: no prior kg action to re-run", desc)
	}
	return w.poll(desc, func() bool {
		w.rerun()
		return present(w.lastText, urn)
	})
}

// assertExcluded confirms a URN is absent from the last result. Store isolation
// makes this deterministic, but re-run once to read a fresh, caught-up result.
func (w *perAccountKGWorld) assertExcluded(urn string, present valuePresent, what string) error {
	if urn == "" {
		return fmt.Errorf("%s exclude: no known URN", what)
	}
	if w.rerun != nil {
		w.rerun()
	}
	if present(w.lastText, urn) {
		return fmt.Errorf("expected %s to exclude %s, but it was present: %s", what, urn, w.lastText)
	}
	return nil
}

// --- URN bookkeeping ---

func (w *perAccountKGWorld) rememberURN(ownerKey, name, urn string) {
	if w.urnsByAccount[ownerKey] == nil {
		w.urnsByAccount[ownerKey] = map[string]string{}
	}
	w.urnsByAccount[ownerKey][name] = urn
}

func (w *perAccountKGWorld) urnFor(ownerKey, name string) string {
	if m := w.urnsByAccount[ownerKey]; m != nil {
		return m[name]
	}
	return ""
}

// anyURN returns the URN of a resource by name across all owners (used when the
// step names a resource whose owner is implicit).
func (w *perAccountKGWorld) anyURN(name string) string {
	for _, m := range w.urnsByAccount {
		if urn := m[name]; urn != "" {
			return urn
		}
	}
	return ""
}

// --- parsing helpers ---

func accountSlug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func classIRIMatching(text, slug string) string {
	for _, c := range parseClasses(text) {
		if strings.Contains(strings.ToLower(c), strings.ToLower(slug)) {
			return c
		}
	}
	return ""
}

func parseClasses(text string) []string {
	var out struct {
		Classes []struct {
			Value string `json:"value"`
		} `json:"classes"`
	}
	if json.Unmarshal([]byte(text), &out) != nil {
		return nil
	}
	vs := make([]string, 0, len(out.Classes))
	for _, c := range out.Classes {
		vs = append(vs, c.Value)
	}
	return vs
}

func parseSearchValues(text string) []string {
	var out struct {
		Matches []struct {
			Value string `json:"value"`
		} `json:"matches"`
	}
	if json.Unmarshal([]byte(text), &out) != nil {
		return nil
	}
	vs := make([]string, 0, len(out.Matches))
	for _, m := range out.Matches {
		vs = append(vs, m.Value)
	}
	return vs
}

func searchHasValue(text, want string) bool {
	for _, v := range parseSearchValues(text) {
		if v == want {
			return true
		}
	}
	return false
}

func instancesHaveValue(text, want string) bool {
	var out struct {
		Instances []struct {
			Value string `json:"value"`
		} `json:"instances"`
	}
	if json.Unmarshal([]byte(text), &out) != nil {
		return false
	}
	for _, i := range out.Instances {
		if i.Value == want {
			return true
		}
	}
	return false
}

func parseTriples(text string) []struct{ Subject, Predicate, Object string } {
	var t kgTriples
	if json.Unmarshal([]byte(text), &t) != nil {
		return nil
	}
	out := make([]struct{ Subject, Predicate, Object string }, 0, len(t.Triples))
	for _, tr := range t.Triples {
		out = append(out, struct{ Subject, Predicate, Object string }{tr.Subject, tr.Predicate, tr.Object})
	}
	return out
}
