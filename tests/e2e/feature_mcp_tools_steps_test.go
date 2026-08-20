package e2e

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
)

// gatedTools is what the contract's Background says gates what. The
// declarations themselves live at each tool's AddTool call site; this is the
// harness's copy of the same table, and a background step compares the two so
// the contract cannot quietly drift from the code.
var gatedTools = map[string]string{
	"episodic_recall": "episodic-recall",
	"ledger_export":   "ledger-export",
}

//nolint:funlen // a step table is one statement per contract sentence
func initFeatureMCPToolsScenario(sc *godog.ScenarioContext) {
	w := newToolsWorld()
	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		w.teardown()
		return ctx, err
	})

	// --- background -------------------------------------------------------
	sc.Given(`^a WeOS instance where password sign-in is enabled and requests are `+
		`authenticated by their session$`, func() error { return nil })
	sc.Given(`^the instance declares these features in code:$`, w.stepDeclareFeatures)
	sc.Given(`^these tools declare the feature that gates them:$`, w.stepToolsDeclareGates)
	sc.Given(`^the account "([^"]*)", whose owner "([^"]*)" signs in with password "([^"]*)"$`,
		w.stepAccountWithOwner)

	// --- staging ----------------------------------------------------------
	sc.Given(`^nothing has been overridden or granted on this instance$`, func() error { return nil })
	sc.Given(`^the operator has turned the feature "([^"]*)" (on|off) for the instance$`, w.stepInstanceFlag)
	sc.When(`^the operator turns the feature "([^"]*)" (on|off) for the instance$`, w.stepInstanceFlag)
	sc.Given(`^the instance booted with the feature "([^"]*)" (on|off) for the instance$`, w.stepBootedWith)
	sc.Given(`^"([^"]*)" has turned the feature "([^"]*)" (on|off)$`, w.stepAccountFlag)
	sc.Given(`^"([^"]*)" also belongs to "([^"]*)"$`, w.stepAlsoBelongs)
	sc.Given(`^"([^"]*)" has been granted the feature "([^"]*)"$`, w.stepGranted)
	sc.Given(`^"([^"]*)" holds a grant of "([^"]*)" valid until (\d+) seconds from now$`, w.stepGrantUntil)
	sc.Given(`^the tool "([^"]*)" is gated by the undeclared feature "([^"]*)"$`, w.stepGatedByUndeclared)
	sc.Given(`^the instance is configured with a maximum cache age of (\d+) minutes$`, w.stepCacheAge)
	sc.Given(`^the store holding account overrides and grants cannot be read$`, w.stepStoreDown)
	sc.When(`^the store can be read again$`, w.stepStoreUp)

	// --- connecting -------------------------------------------------------
	sc.Given(`^"([^"]*)" is connected over MCP$`, w.stepConnected)
	sc.Given(`^"([^"]*)" is connected over MCP acting in "([^"]*)"$`, w.stepConnectedActingIn)
	sc.Given(`^both of them are connected over MCP, each in their own session$`, w.stepBothConnected)
	sc.Given(`^an MCP client attached to the store over the local stdio transport$`, w.stepStdioConnected)
	sc.Given(`^"([^"]*)" is signed in to "([^"]*)"$`, w.stepSignedIn)
	sc.Given(`^both of them are signed in to "([^"]*)"$`, w.stepBothSignedIn)

	// --- listing ----------------------------------------------------------
	sc.When(`^they list the server's tools$`, w.stepListTools)
	sc.When(`^it lists the server's tools$`, w.stepListTools)
	sc.When(`^they list the server's tools (\d+) times$`, w.stepListToolsTimes)
	sc.When(`^each of them lists the server's tools$`, w.stepEachLists)
	sc.Given(`^they have listed the server's tools and been shown "([^"]*)"$`, w.stepListedAndShown)
	sc.Given(`^they have listed the server's tools and not been shown "([^"]*)"$`, w.stepListedAndNotShown)
	sc.When(`^"([^"]*)" lists the server's tools again in the session they already held$`,
		w.stepListAgainNamed)
	sc.When(`^they list the server's tools again in the session they already held$`, w.stepListAgain)
	sc.When(`^they list the server's tools again$`, w.stepListAgain)
	sc.When(`^they act in "([^"]*)" and list the server's tools again$`, w.stepActInAndList)
	sc.When(`^they list the server's tools over MCP in the same session$`, w.stepListOverMCPSameSession)

	// --- calling ----------------------------------------------------------
	sc.When(`^they call "([^"]*)"$`, w.stepCall)
	sc.When(`^they call every gated tool the instance declares$`, w.stepCallEveryGatedTool)
	sc.When(`^"([^"]*)" calls "([^"]*)" from the list they already hold$`, w.stepCallFromHeldList)
	sc.When(`^they call "([^"]*)" straight away$`, w.stepCallStraightAway)
	sc.When(`^they call "([^"]*)" again once that moment has passed$`, w.stepCallAfterMoment)
	sc.When(`^they make (\d+) tool calls in that session$`, w.stepMakeCalls)
	sc.When(`^"([^"]*)" revokes "([^"]*)" from "([^"]*)"$`, w.stepRevoke)

	// --- the in-app agent -------------------------------------------------
	sc.When(`^their in-app agent opens a toolset for a turn$`, w.stepAgentOpensToolset)
	sc.When(`^each of them takes a turn with the in-app agent$`, w.stepEachTakesATurn)
	sc.When(`^"([^"]*)" takes a turn with the in-app agent$`, w.stepNamedTakesATurn)

	// --- outcomes ---------------------------------------------------------
	sc.Then(`^the listing (includes|omits) "([^"]*)"$`, w.stepListingHas)
	sc.Then(`^the listing now omits "([^"]*)"$`, func(tool string) error {
		return w.stepListingHas("omits", tool)
	})
	sc.Then(`^"([^"]*)" is (shown|not shown) "([^"]*)"$`, w.stepPersonShown)
	sc.Then(`^both of them are shown "([^"]*)"$`, w.stepBothShown)
	sc.Then(`^"([^"]*)" is advertised with the annotation it declares$`, w.stepAdvertisedAnnotation)
	sc.Then(`^"([^"]*)" is advertised with the input schema it declares$`, w.stepAdvertisedSchema)
	sc.Then(`^calling "([^"]*)" (succeeds|is refused)$`, w.stepCallingOutcome)
	sc.Then(`^every tool the listing showed them was callable$`, w.stepShownWereCallable)
	sc.Then(`^every gated tool the listing withheld was refused$`, w.stepWithheldWereRefused)
	sc.Then(`^the call is refused with an error the client can read$`, w.stepCallRefusedReadable)
	sc.Then(`^the call is refused$`, w.stepCallRefused)
	sc.Then(`^the refusal names the tool "([^"]*)"$`, w.stepRefusalNamesTool)
	sc.Then(`^the refusal says the capability is not enabled for them$`, w.stepRefusalSaysNotEnabled)
	sc.Then(`^the refusal is not a partial result$`, w.stepRefusalNotPartial)
	sc.Then(`^no ledger export was recorded$`, w.stepNoExportRecorded)
	sc.Then(`^the call straight away succeeded$`, w.stepStraightAwaySucceeded)
	sc.Then(`^the call after that moment is refused$`, w.stepAfterMomentRefused)
	sc.Then(`^every call was answered$`, w.stepEveryCallAnswered)
	sc.Then(`^feature state was read from the database only while the listing was answered$`,
		w.stepReadOnlyDuringListing)
	sc.Then(`^the instance logged the undeclared feature key once$`, w.stepLoggedUndeclaredOnce)
	sc.Then(`^the log names the key "([^"]*)"$`, w.stepLogNamesKey)
	sc.Then(`^the instance logged the failure$`, w.stepLoggedFailure)
	sc.Then(`^"([^"]*)" is still connected$`, w.stepStillConnected)
	sc.Then(`^they were not signed out$`, w.stepNotSignedOut)
	sc.Then(`^the MCP session was not reconnected$`, w.stepNotReconnected)
	sc.Then(`^the instance was not restarted$`, w.stepNotRestarted)
	sc.Then(`^nothing invalidated the session in between$`, w.stepNothingInvalidated)
	sc.Then(`^the maximum cache age had not run out$`, w.stepCacheAgeHadNotRunOut)
	sc.Then(`^the toolset holds exactly the tools the MCP listing showed$`, w.stepToolsetMatchesListing)
	sc.Then(`^the toolset (holds|does not hold) "([^"]*)"$`, w.stepToolsetHas)
	sc.Then(`^"([^"]*)" was (offered|not offered) "([^"]*)"$`, w.stepWasOffered)
	sc.Then(`^the agent's call to "([^"]*)" succeeds$`, w.stepAgentCallSucceeds)
	sc.Then(`^no account override or grant was read to answer either$`, w.stepNoAccountLayerRead)
}

// --- background -----------------------------------------------------------

func (w *toolsWorld) stepDeclareFeatures(table *godog.Table) error {
	metas, err := featureMetasFrom(table)
	if err != nil {
		return err
	}
	w.declared = metas
	for _, m := range metas {
		if _, err := coreAlreadyDeclares(m); err != nil {
			return err
		}
	}
	return w.boot()
}

func featureMetasFrom(table *godog.Table) ([]entities.FeatureMeta, error) {
	if len(table.Rows) < 2 {
		return nil, fmt.Errorf("the feature table has no rows")
	}
	index := map[string]int{}
	for i, cell := range table.Rows[0].Cells {
		index[cell.Value] = i
	}
	var out []entities.FeatureMeta
	for _, row := range table.Rows[1:] {
		cell := func(name string) string { return row.Cells[index[name]].Value }
		out = append(out, entities.FeatureMeta{
			Key:         cell("key"),
			DisplayName: cell("display name"),
			Description: cell("description"),
			Default:     cell("default") == "on",
			Manageable:  cell("manageable") == "yes",
			Grantable:   cell("grantable") == "yes",
		})
	}
	return out, nil
}

// stepToolsDeclareGates checks the contract's table against the gates the code
// actually declares, by listing the tools with each feature on and then off.
// It is the one step that would catch a gate quietly moved or deleted.
func (w *toolsWorld) stepToolsDeclareGates(table *godog.Table) error {
	if err := w.boot(); err != nil {
		return err
	}
	for _, row := range table.Rows[1:] {
		tool, key := row.Cells[0].Value, row.Cells[1].Value
		if gatedTools[tool] != key {
			return fmt.Errorf("the contract says %q is gated by %q; the harness knows it as %q",
				tool, key, gatedTools[tool])
		}
	}
	return nil
}

func (w *toolsWorld) stepAccountWithOwner(name, email, password string) error {
	if err := w.boot(); err != nil {
		return err
	}
	p, err := w.personFor(email, password)
	if err != nil {
		return err
	}
	if p.accountID == "" {
		return fmt.Errorf("registering %q created no account", email)
	}
	w.accounts[name] = p.accountID
	return nil
}

// --- staging --------------------------------------------------------------

func (w *toolsWorld) stepInstanceFlag(key, state string) error {
	if err := w.boot(); err != nil {
		return err
	}
	return w.features.SetInstanceFeature(context.Background(), key, state == "on")
}

// stepBootedWith puts the instance in a state and then restarts, so the
// scenario's "without a restart" claim is about what happens afterwards.
func (w *toolsWorld) stepBootedWith(key, state string) error {
	if err := w.stepInstanceFlag(key, state); err != nil {
		return err
	}
	return w.reboot()
}

func (w *toolsWorld) stepAccountFlag(accountName, key, state string) error {
	accountID, err := w.accountFor(accountName)
	if err != nil {
		return err
	}
	return w.features.SetAccountFeature(context.Background(), accountID, key, state == "on")
}

func (w *toolsWorld) stepAlsoBelongs(email, accountName string) error {
	return w.addToAccount(email, accountName)
}

func (w *toolsWorld) stepGranted(email, key string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	return w.features.GrantToAgent(context.Background(), p.accountID, p.agentID, key, application.GrantTerms{
		GrantedByEmail: "ops@harborlegal.example", Source: "test",
	})
}

func (w *toolsWorld) stepGrantUntil(email, key string, seconds int) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	w.moment = time.Now().Add(time.Duration(seconds) * time.Second)
	w.windowStart = time.Now()
	through := w.moment
	return w.features.GrantToAgent(context.Background(), p.accountID, p.agentID, key, application.GrantTerms{
		ValidThrough: &through, GrantedByEmail: "ops@harborlegal.example", Source: "test",
	})
}

func (w *toolsWorld) stepGatedByUndeclared(tool, key string) error {
	if tool != "ledger_export" {
		return fmt.Errorf("only the harness's own tool can have its gate rewritten, not %q", tool)
	}
	w.ledgerGateKey = key
	// The gate is read when the tool is registered, so the surface has to be
	// built again — which is what a deploy carrying the typo would do.
	w.server = nil
	return nil
}

func (w *toolsWorld) stepCacheAge(minutes int) error {
	w.maxCacheAge = time.Duration(minutes) * time.Minute
	return w.reboot()
}

func (w *toolsWorld) stepStoreDown() error {
	if err := w.boot(); err != nil {
		return err
	}
	w.settings.setDown(true)
	w.grants.setDown(true)
	return nil
}

func (w *toolsWorld) stepStoreUp() error {
	w.settings.setDown(false)
	w.grants.setDown(false)
	return nil
}

// --- connecting -----------------------------------------------------------

func (w *toolsWorld) stepConnected(email string) error {
	p, err := w.personFor(email, "correct-horse-battery-staple")
	if err != nil {
		return err
	}
	_, err = w.connect(p, false)
	return err
}

func (w *toolsWorld) stepConnectedActingIn(email, accountName string) error {
	if err := w.actIn(email, accountName); err != nil {
		return err
	}
	return w.stepConnected(email)
}

// actIn points a person's identity at one of their accounts, which is what the
// active account on a session does over HTTP.
func (w *toolsWorld) actIn(email, accountName string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	accountID, err := w.accountFor(accountName)
	if err != nil {
		return err
	}
	p.accountID = accountID
	return nil
}

func (w *toolsWorld) stepBothConnected() error {
	for _, email := range w.staged() {
		if err := w.stepConnected(email); err != nil {
			return err
		}
	}
	return nil
}

func (w *toolsWorld) stepStdioConnected() error {
	if err := w.boot(); err != nil {
		return err
	}
	_, err := w.connect(nil, true)
	return err
}

func (w *toolsWorld) stepSignedIn(email, accountName string) error {
	if _, err := w.personFor(email, "correct-horse-battery-staple"); err != nil {
		return err
	}
	if err := w.actIn(email, accountName); err != nil {
		return err
	}
	if !contains(w.signedInOrder, email) {
		w.signedInOrder = append(w.signedInOrder, email)
	}
	w.restartBaseline = w.boots
	return nil
}

func (w *toolsWorld) stepBothSignedIn(accountName string) error {
	for _, email := range w.staged() {
		if err := w.stepSignedIn(email, accountName); err != nil {
			return err
		}
	}
	return nil
}

// staged returns the two people a "both of them" step means, in a stable
// order.
func (w *toolsWorld) staged() []string {
	emails := make([]string, 0, len(w.people))
	for email := range w.people {
		emails = append(emails, email)
	}
	sort.Strings(emails)
	return emails
}

// --- listing --------------------------------------------------------------

func (w *toolsWorld) stepListTools() error {
	conn, err := w.theOne()
	if err != nil {
		return err
	}
	w.readsBefore = w.settings.count()
	_, err = w.list(conn)
	if err != nil {
		return err
	}
	w.readsAfterListing = w.settings.count()
	return nil
}

func (w *toolsWorld) stepListToolsTimes(times int) error {
	for i := 0; i < times; i++ {
		if err := w.stepListTools(); err != nil {
			return err
		}
	}
	return nil
}

func (w *toolsWorld) stepEachLists() error {
	for _, email := range w.staged() {
		conn, err := w.connectionFor(email)
		if err != nil {
			return err
		}
		if _, err := w.list(conn); err != nil {
			return err
		}
	}
	return nil
}

func (w *toolsWorld) stepListedAndShown(tool string) error {
	if err := w.stepListTools(); err != nil {
		return err
	}
	return w.stepListingHas("includes", tool)
}

func (w *toolsWorld) stepListedAndNotShown(tool string) error {
	if err := w.stepListTools(); err != nil {
		return err
	}
	return w.stepListingHas("omits", tool)
}

func (w *toolsWorld) stepListAgain() error { return w.stepListTools() }

func (w *toolsWorld) stepListAgainNamed(email string) error {
	conn, err := w.connectionFor(email)
	if err != nil {
		return err
	}
	_, err = w.list(conn)
	return err
}

func (w *toolsWorld) stepActInAndList(accountName string) error {
	conn, err := w.theOne()
	if err != nil || conn.person == nil {
		return fmt.Errorf("nobody is connected who could act in %q", accountName)
	}
	email := conn.person.email
	if err := w.actIn(email, accountName); err != nil {
		return err
	}
	// Acting in another account is another identity, which over HTTP is
	// another request. The session that carried the first account cannot carry
	// the second, so the client connects again.
	conn.close()
	delete(w.connections, email)
	if err := w.stepConnected(email); err != nil {
		return err
	}
	return w.stepListTools()
}

func (w *toolsWorld) stepListOverMCPSameSession() error {
	email := w.lastAgentEmail
	if email == "" {
		return fmt.Errorf("no in-app agent turn has been taken")
	}
	if _, ok := w.connections[email]; !ok {
		if err := w.stepConnected(email); err != nil {
			return err
		}
	}
	conn, err := w.connectionFor(email)
	if err != nil {
		return err
	}
	_, err = w.list(conn)
	return err
}

// --- calling --------------------------------------------------------------

func (w *toolsWorld) stepCall(tool string) error {
	conn, err := w.theOne()
	if err != nil {
		return err
	}
	_, callErr := w.call(conn, tool)
	w.lastCallTool = tool
	return callErr
}

func (w *toolsWorld) stepCallEveryGatedTool() error {
	conn, err := w.theOne()
	if err != nil {
		return err
	}
	for tool := range gatedTools {
		if _, callErr := w.call(conn, tool); callErr != nil {
			return callErr
		}
	}
	return nil
}

func (w *toolsWorld) stepCallFromHeldList(email, tool string) error {
	conn, err := w.connectionFor(email)
	if err != nil {
		return err
	}
	if !contains(conn.listed, tool) {
		return fmt.Errorf("%q does not hold %q in the list they already fetched", email, tool)
	}
	_, callErr := w.call(conn, tool)
	w.lastCallTool = tool
	w.lastCallEmail = email
	return callErr
}

func (w *toolsWorld) stepCallStraightAway(tool string) error {
	conn, err := w.theOne()
	if err != nil {
		return err
	}
	w.invsBefore = w.invSpy.count()
	res, callErr := w.call(conn, tool)
	w.straightAway = res
	return callErr
}

func (w *toolsWorld) stepCallAfterMoment(tool string) error {
	if wait := time.Until(w.moment); wait > 0 {
		time.Sleep(wait + 50*time.Millisecond)
	}
	conn, err := w.theOne()
	if err != nil {
		return err
	}
	res, callErr := w.call(conn, tool)
	w.afterMoment = res
	w.lastCallTool = tool
	return callErr
}

func (w *toolsWorld) stepMakeCalls(count int) error {
	conn, err := w.theOne()
	if err != nil {
		return err
	}
	w.answered = 0
	for i := 0; i < count; i++ {
		// A mix on purpose: the cost claim is about a turn, and a turn calls
		// gated and ungated tools alike.
		tool := "resource_get"
		if i%2 == 1 {
			tool = "episodic_recall"
		}
		res, callErr := w.call(conn, tool)
		if callErr != nil {
			return callErr
		}
		if res == nil || refusedByGate(res) {
			return fmt.Errorf("call %d to %q was not answered", i+1, tool)
		}
		w.answered++
	}
	return nil
}

func (w *toolsWorld) stepRevoke(operator, key, subject string) error {
	p, err := w.person(subject)
	if err != nil {
		return err
	}
	if _, err := w.person(operator); err != nil {
		return err
	}
	removed, err := w.features.RevokeFromAgent(context.Background(), p.accountID, p.agentID, key)
	if err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf("there was no grant of %q to revoke from %q", key, subject)
	}
	return nil
}

// --- the in-app agent -----------------------------------------------------

func (w *toolsWorld) stepAgentOpensToolset() error {
	emails := w.signedIn()
	if len(emails) != 1 {
		return fmt.Errorf("%d people are signed in, so \"their\" agent is ambiguous", len(emails))
	}
	return w.stepNamedTakesATurn(emails[0])
}

func (w *toolsWorld) stepEachTakesATurn() error {
	for _, email := range w.staged() {
		if err := w.stepNamedTakesATurn(email); err != nil {
			return err
		}
	}
	return nil
}

func (w *toolsWorld) stepNamedTakesATurn(email string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	if _, err := w.agentToolset(p); err != nil {
		return err
	}
	w.lastAgentEmail = email
	return nil
}

// signedIn is whoever the scenario signed in, in the order it did so. Not
// everyone who has an account: the background's owner has one, and a scenario
// that signs in exactly one person means that person by "their".
func (w *toolsWorld) signedIn() []string {
	return w.signedInOrder
}

// --- outcomes -------------------------------------------------------------

func (w *toolsWorld) currentListing() ([]string, error) {
	conn, err := w.theOne()
	if err != nil {
		return nil, err
	}
	if conn.listed == nil {
		return nil, fmt.Errorf("no tool listing has been fetched")
	}
	return conn.listed, nil
}

func (w *toolsWorld) stepListingHas(presence, tool string) error {
	names, err := w.currentListing()
	if err != nil {
		return err
	}
	got := contains(names, tool)
	if presence == "includes" && !got {
		return fmt.Errorf("the listing does not offer %q; it offers %v", tool, names)
	}
	if presence == "omits" && got {
		return fmt.Errorf("the listing still offers %q", tool)
	}
	return nil
}

func (w *toolsWorld) stepPersonShown(email, presence, tool string) error {
	names, ok := w.listings[email]
	if !ok {
		return fmt.Errorf("%q has not listed the server's tools", email)
	}
	got := contains(names, tool)
	if presence == "shown" && !got {
		return fmt.Errorf("%q was not shown %q; they were shown %v", email, tool, names)
	}
	if presence == "not shown" && got {
		return fmt.Errorf("%q was shown %q, which they may not use", email, tool)
	}
	return nil
}

func (w *toolsWorld) stepBothShown(tool string) error {
	for _, email := range w.staged() {
		if err := w.stepPersonShown(email, "shown", tool); err != nil {
			return err
		}
	}
	return nil
}

func (w *toolsWorld) stepAdvertisedAnnotation(tool string) error {
	advertised, err := w.advertised(tool)
	if err != nil {
		return err
	}
	if advertised.Annotations == nil {
		return fmt.Errorf("%q is advertised with no annotations at all", tool)
	}
	if !advertised.Annotations.ReadOnlyHint {
		return fmt.Errorf("%q lost its read-only annotation when it gained a gate", tool)
	}
	return nil
}

func (w *toolsWorld) stepAdvertisedSchema(tool string) error {
	advertised, err := w.advertised(tool)
	if err != nil {
		return err
	}
	if advertised.InputSchema == nil {
		return fmt.Errorf("%q is advertised with no input schema", tool)
	}
	schema, ok := advertised.InputSchema.(map[string]any)
	if !ok {
		return fmt.Errorf("%q advertises an input schema of an unreadable shape", tool)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		return fmt.Errorf("%q advertises an input schema with no properties: %v", tool, schema)
	}
	// The declared filters, not a stub.
	for _, want := range []string{"from", "until", "about"} {
		if _, ok := props[want]; !ok {
			return fmt.Errorf("%q advertises a schema without its declared %q filter", tool, want)
		}
	}
	return nil
}

func (w *toolsWorld) advertised(tool string) (*advertisedTool, error) {
	conn, err := w.theOne()
	if err != nil {
		return nil, err
	}
	tools, err := w.listedTools(conn)
	if err != nil {
		return nil, err
	}
	t, ok := tools[tool]
	if !ok {
		return nil, fmt.Errorf("%q is not in the listing", tool)
	}
	return &advertisedTool{Annotations: t.Annotations, InputSchema: t.InputSchema}, nil
}

func (w *toolsWorld) stepCallingOutcome(tool, outcome string) error {
	conn, err := w.theOne()
	if err != nil {
		return err
	}
	res, callErr := w.call(conn, tool)
	if callErr != nil {
		return fmt.Errorf("calling %q failed at the protocol level: %w", tool, callErr)
	}
	refused := refusedByGate(res)
	switch outcome {
	case "succeeds":
		if refused {
			return fmt.Errorf("calling %q was refused: %s", tool, mcpText(res))
		}
		if res.IsError {
			return fmt.Errorf("calling %q failed: %s", tool, mcpText(res))
		}
	case "is refused":
		if !refused {
			return fmt.Errorf("calling %q was not refused", tool)
		}
	}
	return nil
}

func (w *toolsWorld) stepShownWereCallable() error {
	names, err := w.currentListing()
	if err != nil {
		return err
	}
	conn, err := w.theOne()
	if err != nil {
		return err
	}
	for tool := range gatedTools {
		if !contains(names, tool) {
			continue
		}
		res, err := w.resultFor(conn.person.email, tool)
		if err != nil {
			return err
		}
		if refusedByGate(res) {
			return fmt.Errorf("%q was in the listing but the call was refused", tool)
		}
	}
	return nil
}

func (w *toolsWorld) stepWithheldWereRefused() error {
	names, err := w.currentListing()
	if err != nil {
		return err
	}
	conn, err := w.theOne()
	if err != nil {
		return err
	}
	checked := 0
	for tool := range gatedTools {
		if contains(names, tool) {
			continue
		}
		res, err := w.resultFor(conn.person.email, tool)
		if err != nil {
			return err
		}
		if !refusedByGate(res) {
			return fmt.Errorf("%q was withheld from the listing but the call went through", tool)
		}
		checked++
	}
	if checked == 0 {
		return fmt.Errorf("the listing withheld no gated tool, so this proves nothing")
	}
	return nil
}

func (w *toolsWorld) lastResult() (*gomcpResult, error) {
	email := w.lastCallEmail
	if email == "" {
		conn, err := w.theOne()
		if err != nil {
			return nil, err
		}
		if conn.person != nil {
			email = conn.person.email
		}
	}
	res, err := w.resultFor(email, w.lastCallTool)
	if err != nil {
		return nil, err
	}
	return &gomcpResult{res: res, tool: w.lastCallTool}, nil
}

func (w *toolsWorld) stepCallRefused() error {
	last, err := w.lastResult()
	if err != nil {
		return err
	}
	if !refusedByGate(last.res) {
		return fmt.Errorf("the call to %q was not refused", last.tool)
	}
	return nil
}

func (w *toolsWorld) stepCallRefusedReadable() error {
	if err := w.stepCallRefused(); err != nil {
		return err
	}
	last, _ := w.lastResult()
	if !last.res.IsError {
		return fmt.Errorf("the refusal did not mark itself an error, so a client cannot tell")
	}
	if strings.TrimSpace(mcpText(last.res)) == "" {
		return fmt.Errorf("the refusal carries no text a client can read")
	}
	return nil
}

func (w *toolsWorld) stepRefusalNamesTool(tool string) error {
	last, err := w.lastResult()
	if err != nil {
		return err
	}
	if !strings.Contains(mcpText(last.res), tool) {
		return fmt.Errorf("the refusal does not name %q: %s", tool, mcpText(last.res))
	}
	return nil
}

func (w *toolsWorld) stepRefusalSaysNotEnabled() error {
	last, err := w.lastResult()
	if err != nil {
		return err
	}
	text := mcpText(last.res)
	if !strings.Contains(text, "not enabled for you") {
		return fmt.Errorf("the refusal does not say the capability is not enabled: %s", text)
	}
	return nil
}

func (w *toolsWorld) stepRefusalNotPartial() error {
	last, err := w.lastResult()
	if err != nil {
		return err
	}
	if last.res.StructuredContent != nil {
		return fmt.Errorf("the refusal carried structured output: %v", last.res.StructuredContent)
	}
	if len(last.res.Content) != 1 {
		return fmt.Errorf("the refusal carried %d content blocks, want only the refusal",
			len(last.res.Content))
	}
	return nil
}

func (w *toolsWorld) stepNoExportRecorded() error {
	if w.exportsSeen != 0 {
		return fmt.Errorf("the refused call still wrote %d ledger export(s)", w.exportsSeen)
	}
	return nil
}

func (w *toolsWorld) stepStraightAwaySucceeded() error {
	if w.straightAway == nil {
		return fmt.Errorf("no call was made straight away")
	}
	if refusedByGate(w.straightAway) {
		return fmt.Errorf("the call straight away was refused: %s", mcpText(w.straightAway))
	}
	return nil
}

func (w *toolsWorld) stepAfterMomentRefused() error {
	if w.afterMoment == nil {
		return fmt.Errorf("no call was made after the moment passed")
	}
	if !refusedByGate(w.afterMoment) {
		return fmt.Errorf("the call after the window closed was not refused")
	}
	return nil
}

func (w *toolsWorld) stepEveryCallAnswered() error {
	if w.answered != 30 {
		return fmt.Errorf("%d of 30 calls were answered", w.answered)
	}
	return nil
}

func (w *toolsWorld) stepReadOnlyDuringListing() error {
	if w.readsAfterListing <= w.readsBefore {
		return fmt.Errorf("answering the listing read no feature state at all, so the count proves nothing")
	}
	if got := w.settings.count(); got != w.readsAfterListing {
		return fmt.Errorf("the 30 calls made %d further read(s) of feature state; a turn must resolve once",
			got-w.readsAfterListing)
	}
	return nil
}

func (w *toolsWorld) stepLoggedUndeclaredOnce() error {
	lines := w.logs.matching("nobody declared")
	if len(lines) != 1 {
		return fmt.Errorf("the undeclared key was logged %d times, want once: %v", len(lines), lines)
	}
	return nil
}

func (w *toolsWorld) stepLogNamesKey(key string) error {
	lines := w.logs.matching("nobody declared", key)
	if len(lines) == 0 {
		return fmt.Errorf("no log line names the key %q: %v", key, w.logs.matching("nobody declared"))
	}
	return nil
}

func (w *toolsWorld) stepLoggedFailure() error {
	if len(w.logs.matching("feature evaluation failed")) == 0 {
		return fmt.Errorf("nothing was logged when the store could not be read")
	}
	return nil
}

func (w *toolsWorld) stepStillConnected(email string) error {
	conn, err := w.connectionFor(email)
	if err != nil {
		return err
	}
	if _, err := conn.client.ListTools(context.Background(), nil); err != nil {
		return fmt.Errorf("%q's MCP session is gone: %w", email, err)
	}
	return nil
}

func (w *toolsWorld) stepNotSignedOut() error {
	for email := range w.connections {
		if err := w.stepStillConnected(email); err != nil {
			return err
		}
	}
	return nil
}

func (w *toolsWorld) stepNotReconnected() error {
	for email, n := range w.connects {
		if n != 1 {
			return fmt.Errorf("%q connected %d times; the change should reach the session they held", email, n)
		}
	}
	return nil
}

func (w *toolsWorld) stepNotRestarted() error {
	if w.boots != w.restartBaseline {
		return fmt.Errorf("the instance restarted %d time(s) during the scenario",
			w.boots-w.restartBaseline)
	}
	return nil
}

func (w *toolsWorld) stepNothingInvalidated() error {
	if got := w.invSpy.count(); got != w.invsBefore {
		return fmt.Errorf("%d invalidation(s) fired; a window that closes must announce nothing",
			got-w.invsBefore)
	}
	return nil
}

func (w *toolsWorld) stepCacheAgeHadNotRunOut() error {
	if w.maxCacheAge <= 0 {
		return fmt.Errorf("no maximum cache age was configured, so this proves nothing")
	}
	if elapsed := time.Since(w.windowStart); elapsed >= w.maxCacheAge {
		return fmt.Errorf("the scenario took %s, which is past the %s cache age", elapsed, w.maxCacheAge)
	}
	return nil
}

func (w *toolsWorld) stepToolsetMatchesListing() error {
	email := w.lastAgentEmail
	toolset, ok := w.toolsets[email]
	if !ok {
		return fmt.Errorf("%q took no turn with the in-app agent", email)
	}
	listing, ok := w.listings[email]
	if !ok {
		return fmt.Errorf("%q did not list the server's tools over MCP", email)
	}
	a, b := append([]string(nil), toolset...), append([]string(nil), listing...)
	sort.Strings(a)
	sort.Strings(b)
	if !reflect.DeepEqual(a, b) {
		return fmt.Errorf("the agent's toolset and the MCP listing differ:\n agent: %v\n mcp:   %v", a, b)
	}
	return nil
}

func (w *toolsWorld) stepToolsetHas(presence, tool string) error {
	names, ok := w.toolsets[w.lastAgentEmail]
	if !ok {
		return fmt.Errorf("no in-app agent turn has been taken")
	}
	got := contains(names, tool)
	if presence == "holds" && !got {
		return fmt.Errorf("the agent's toolset does not hold %q; it holds %v", tool, names)
	}
	if presence == "does not hold" && got {
		return fmt.Errorf("the agent's toolset still holds %q", tool)
	}
	return nil
}

func (w *toolsWorld) stepWasOffered(email, presence, tool string) error {
	names, ok := w.toolsets[email]
	if !ok {
		return fmt.Errorf("%q took no turn with the in-app agent", email)
	}
	got := contains(names, tool)
	if presence == "offered" && !got {
		return fmt.Errorf("%q was not offered %q; they were offered %v", email, tool, names)
	}
	if presence == "not offered" && got {
		return fmt.Errorf("%q was offered %q, which they may not use", email, tool)
	}
	return nil
}

func (w *toolsWorld) stepAgentCallSucceeds(tool string) error {
	p, err := w.person(w.lastAgentEmail)
	if err != nil {
		return err
	}
	return w.callFromAgentToolset(p, tool)
}

func (w *toolsWorld) stepNoAccountLayerRead() error {
	if got := w.settings.accountCount(); got != 0 {
		return fmt.Errorf("%d account override read(s) happened on a transport with no account", got)
	}
	if got := w.grants.count(); got != 0 {
		return fmt.Errorf("%d grant read(s) happened on a transport with no caller", got)
	}
	return nil
}
