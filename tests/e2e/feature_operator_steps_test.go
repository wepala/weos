package e2e

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
)

func initFeatureOperatorScenario(sc *godog.ScenarioContext) {
	w := &operatorWorld{
		accounts: map[string]string{},
		people:   map[string]*featurePerson{},
	}
	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		w.teardown()
		return ctx, err
	})

	// --- instance and declarations ---
	sc.Step(`^a WeOS instance where password sign-in is enabled and requests are authenticated by their session$`,
		w.stepInstance)
	sc.Step(`^a WeOS instance with no database configured$`, w.stepInstanceNoDatabase)
	sc.Step(`^the instance is configured with a maximum cache age of (\d+) seconds$`, w.stepMaxCacheAge)
	sc.Step(`^the instance declares these features in code:$`, w.stepDeclare)
	sc.Step(`^the account "([^"]*)", whose owner "([^"]*)" signs in with password "([^"]*)"$`, w.stepAccountOwner)
	sc.Step(`^"([^"]*)" also belongs to "([^"]*)" with the role "([^"]*)"$`, w.stepAlsoBelongs)
	sc.Step(`^"([^"]*)" belongs to "([^"]*)" as an ordinary member$`, w.stepOrdinaryMember)
	sc.Step(`^"([^"]*)" is signed in to "([^"]*)"$`, w.stepSignedIn)

	// --- staged state ---
	sc.Step(`^the operator has turned the feature "([^"]*)" (on|off) for the instance$`, w.stepOperatorHasSet)
	sc.Step(`^"([^"]*)" has turned the feature "([^"]*)" on$`, w.stepAccountHasTurnedOn)
	sc.Step(`^the operator has run "([^"]*)"$`, w.stepOperatorHasRun)
	sc.Step(`^they have already evaluated "([^"]*)" and been answered on$`, w.stepAlreadyAnsweredOn)

	// --- the command line ---
	sc.Step(`^the operator runs "([^"]*)"$`, w.stepRunCLI)
	sc.Step(`^the operator runs "([^"]*)" again$`, w.stepRunCLI)
	sc.Step(`^the operator runs "([^"]*)" against the same store$`, w.stepRunCLI)
	sc.Step(`^the command exits successfully$`, w.stepCommandSucceeded)
	sc.Step(`^the command exits with a failure$`, w.stepCommandFailed)
	sc.Step(`^the failure says no database was specified and names how to supply one$`, w.stepFailureNamesDSN)
	sc.Step(`^the failure names the key "([^"]*)"$`, w.stepFailureNamesKey)
	sc.Step(`^the command leaves no database behind in the directory it ran from$`, w.stepNoStrayDatabase)
	sc.Step(`^the listing names each feature by its display name$`, w.stepListingNamesDisplayNames)
	sc.Step(`^the listing does not report "([^"]*)"$`, w.stepListingOmits)

	// --- the API ---
	sc.Step(`^they ask the API for the feature listing$`, w.stepAPIList)
	sc.Step(`^they turn the feature "([^"]*)" (on|off) for the instance through the API$`, w.stepAPISetInstance)
	sc.Step(`^they try to turn the feature "([^"]*)" off for the instance through the API$`,
		w.stepAPITrySetInstanceOff)
	sc.Step(`^they reset the feature "([^"]*)" for the instance through the API$`, w.stepAPIResetInstance)
	sc.Step(`^they turn the feature "([^"]*)" (on|off) for their own account through the API$`, w.stepAPISetAccount)
	sc.Step(`^they try to turn the feature "([^"]*)" off for their own account through the API$`,
		w.stepAPITrySetAccountOff)
	sc.Step(`^a request carrying no session tries to turn the feature "([^"]*)" off for the instance$`,
		w.stepAnonymousSet)
	sc.Step(`^the listing was served$`, w.stepListingServed)
	sc.Step(`^the change was accepted$`, w.stepChangeAccepted)
	sc.Step(`^the attempt to change it is refused as forbidden$`, w.stepRefusedForbidden)
	sc.Step(`^the attempt is refused as not found$`, w.stepRefusedNotFound)
	sc.Step(`^the attempt is refused as a bad request$`, w.stepRefusedBadRequest)
	sc.Step(`^the request is refused as not authenticated$`, w.stepRefusedUnauthenticated)
	sc.Step(`^the refusal says the feature cannot be changed per account$`, w.stepRefusalMentionsAccount)

	// --- MCP ---
	sc.Step(`^an MCP client attached to the store over the local stdio transport$`, w.stepStdioClient)
	sc.Step(`^they call the MCP tool "([^"]*)"$`, w.stepCallTool)
	sc.Step(`^they call the MCP tool "([^"]*)" to turn "([^"]*)" off for the instance$`, w.stepCallToolSetOff)
	sc.Step(`^it calls "([^"]*)" to turn "([^"]*)" off for the instance$`, w.stepStdioSetOff)
	sc.Step(`^they reset the feature "([^"]*)" for the instance through MCP$`, w.stepMCPReset)
	sc.Step(`^the call succeeds$`, w.stepCallSucceeded)
	sc.Step(`^the call is refused as forbidden$`, w.stepCallForbidden)
	sc.Step(`^what the tool reported matches what the API lists$`, w.stepToolMatchesAPI)

	// --- listings, from whichever surface last answered ---
	sc.Step(`^the listing reports "([^"]*)" as (on|off), from an instance override$`, w.stepListingReportsInstance)
	sc.Step(`^the listing reports "([^"]*)" as (on|off), from the declared default$`, w.stepListingReportsDefault)

	// --- stored state ---
	sc.Step(`^no instance-level setting is stored for "([^"]*)"$`, w.stepNoInstanceSetting)
	sc.Step(`^no account-level setting is stored for "([^"]*)"$`, w.stepNoAccountSetting)
	sc.Step(`^the instance holds one stored instance-level setting for "([^"]*)"$`, w.stepOneInstanceSetting)

	// --- evaluation and sessions ---
	sc.Step(`^they evaluate "([^"]*)" with default off$`, w.stepEvaluate)
	sc.Step(`^they evaluate "([^"]*)" again in the same session$`, w.stepEvaluate)
	sc.Step(`^they evaluate "([^"]*)" again once the maximum cache age has passed$`, w.stepEvaluateAfterAge)
	sc.Step(`^"([^"]*)" signs in and evaluates "([^"]*)" with default off$`, w.stepSignInAndEvaluate)
	sc.Step(`^the feature answers (on|off)$`, w.stepFeatureAnswers)
	sc.Step(`^"([^"]*)" is answered (on|off)$`, w.stepPersonAnswered)
	sc.Step(`^they are still signed in$`, w.stepStillSignedIn)
	sc.Step(`^the instance was not restarted$`, w.stepNotRestarted)

	// --- the audit record ---
	sc.Step(`^the change was recorded with the key "([^"]*)" and the state it was set to$`, w.stepRecorded)
	sc.Step(`^the record names the command line as what made the change$`, w.stepRecordSourceCLI)
	sc.Step(`^the record names "([^"]*)" as who made it$`, w.stepRecordActor)
	sc.Step(`^the record carries the time the change was made$`, w.stepRecordTime)
}

// --- instance -------------------------------------------------------------

func (w *operatorWorld) stepInstance() error { return w.boot() }

// stepInstanceNoDatabase stages the case where the command must refuse. The
// instance still boots against a temp store so the scenario has a working
// directory to check afterwards; the subprocess is the thing denied a DSN.
func (w *operatorWorld) stepInstanceNoDatabase() error {
	if err := w.boot(); err != nil {
		return err
	}
	w.noDSN = true
	return nil
}

func (w *operatorWorld) stepMaxCacheAge(seconds int) error {
	w.maxCacheAge = time.Duration(seconds) * time.Second
	// The resolver reads its maximum age at construction, so the instance is
	// restarted against the same store.
	return w.reboot()
}

// stepDeclare records the declarations and restarts the instance so they are
// read the way a real one reads them — from configuration at boot, which is
// also what the CLI subprocess will read.
func (w *operatorWorld) stepDeclare(table *godog.Table) error {
	for i, row := range table.Rows {
		if i == 0 {
			continue
		}
		cell := func(n int) string { return strings.TrimSpace(row.Cells[n].Value) }
		w.declared = append(w.declared, entities.FeatureMeta{
			Key: cell(0), DisplayName: cell(1), Description: cell(2),
			Default: cell(3) == "on", Manageable: cell(4) == "yes", Grantable: cell(5) == "yes",
		})
	}
	return w.reboot()
}

func (w *operatorWorld) stepAccountOwner(name, email, password string) error {
	if err := w.boot(); err != nil {
		return err
	}
	p, err := w.personFor(email, password)
	if err != nil {
		return err
	}
	if p.accountID == "" {
		return fmt.Errorf("%q has no account to name %q", email, name)
	}
	w.accounts[name] = p.accountID
	if w.primaryAccountID == "" {
		// The first account staged is this instance's own. Recorded and the
		// instance restarted so the guard reads it — otherwise every later
		// registration makes the instance ambiguous, and a member is refused
		// for that rather than for their role.
		w.primaryAccountID = p.accountID
		return w.reboot()
	}
	return nil
}

func (w *operatorWorld) addToAccount(email, accountName, role string) error {
	accountID, ok := w.accounts[accountName]
	if !ok {
		return fmt.Errorf("no account named %q has been staged", accountName)
	}
	p, err := w.personFor(email, "correct-horse-battery-staple")
	if err != nil {
		return err
	}
	if err := w.accountRepo.SaveMember(context.Background(), accountID, p.agentID, role); err != nil {
		return fmt.Errorf("could not add %q to %q: %w", email, accountName, err)
	}
	p.accountID = accountID
	return nil
}

func (w *operatorWorld) stepAlsoBelongs(email, accountName, role string) error {
	return w.addToAccount(email, accountName, role)
}

func (w *operatorWorld) stepOrdinaryMember(email, accountName string) error {
	return w.addToAccount(email, accountName, "member")
}

func (w *operatorWorld) stepSignedIn(email, accountName string) error {
	accountID, ok := w.accounts[accountName]
	if !ok {
		return fmt.Errorf("no account named %q has been staged", accountName)
	}
	p, err := w.person(email)
	if err != nil {
		return err
	}
	p.accountID = accountID
	return w.signIn(p)
}

// --- staged state ---------------------------------------------------------

func (w *operatorWorld) stepOperatorHasSet(key, onOff string) error {
	return w.admin.SetInstance(
		w.operatorCtx(), key, onOff == "on", entities.FeatureChangeSourceCLI)
}

func (w *operatorWorld) stepAccountHasTurnedOn(accountName, key string) error {
	accountID, ok := w.accounts[accountName]
	if !ok {
		return fmt.Errorf("no account named %q has been staged", accountName)
	}
	// Staged through the repository: the service takes the account off the
	// caller's session, and this step names an account with nobody signed in.
	return w.settings.SetOverride(
		context.Background(), repositories.FeatureScopeAccount, accountID, key, true)
}

// operatorCtx stages instance changes the way the command line does — trusted
// local caller, no session.
func (w *operatorWorld) operatorCtx() context.Context {
	return application.WithLocalTransport(context.Background())
}

func (w *operatorWorld) stepOperatorHasRun(command string) error {
	if err := w.runCLI(command); err != nil {
		return err
	}
	if w.lastRun.exitCode != 0 {
		return fmt.Errorf("staging command %q failed: %s", command, w.lastRun.said())
	}
	return nil
}

func (w *operatorWorld) stepAlreadyAnsweredOn(key string) error {
	p := w.current
	if p == nil {
		return fmt.Errorf("nobody is signed in")
	}
	value, _, err := w.resolverEnabled(p, key)
	if err != nil {
		return err
	}
	if !value {
		return fmt.Errorf("%q was answered off, want on", p.email)
	}
	return nil
}

// --- the command line -----------------------------------------------------

func (w *operatorWorld) stepRunCLI(command string) error { return w.runCLI(command) }

func (w *operatorWorld) stepCommandSucceeded() error {
	if w.lastRun.exitCode != 0 {
		return fmt.Errorf("the command exited %d: %s", w.lastRun.exitCode, w.lastRun.said())
	}
	return nil
}

func (w *operatorWorld) stepCommandFailed() error {
	if w.lastRun.exitCode == 0 {
		return fmt.Errorf("the command exited 0, want a failure: %s", w.lastRun.said())
	}
	return nil
}

func (w *operatorWorld) stepFailureNamesDSN() error {
	said := w.lastRun.said()
	if !strings.Contains(said, "no database specified") {
		return fmt.Errorf("the failure does not say a database was missing: %s", said)
	}
	if !strings.Contains(said, "DATABASE_DSN") || !strings.Contains(said, "--database-dsn") {
		return fmt.Errorf("the failure does not name how to supply one: %s", said)
	}
	return nil
}

func (w *operatorWorld) stepFailureNamesKey(key string) error {
	if !strings.Contains(w.lastRun.said(), key) {
		return fmt.Errorf("the failure does not name %q: %s", key, w.lastRun.said())
	}
	return nil
}

// stepNoStrayDatabase is the assertion that makes the refusal worth having: a
// command that guessed a store would leave one here.
func (w *operatorWorld) stepNoStrayDatabase() error {
	entries, err := osReadDir(w.workDir)
	if err != nil {
		return err
	}
	for _, name := range entries {
		if strings.HasSuffix(name, ".db") || strings.HasSuffix(name, ".db-wal") ||
			strings.HasSuffix(name, ".db-shm") {
			return fmt.Errorf("the command left %q behind in the directory it ran from", name)
		}
	}
	return nil
}

func (w *operatorWorld) stepListingNamesDisplayNames() error {
	statuses, err := w.currentListing()
	if err != nil {
		return err
	}
	if statuses == nil {
		for _, m := range w.declared {
			if !strings.Contains(w.lastRun.said(), m.DisplayName) {
				return fmt.Errorf("the listing does not name %q by its display name %q",
					m.Key, m.DisplayName)
			}
		}
		return nil
	}
	for _, m := range w.declared {
		s, ok := findStatus(statuses, m.Key)
		if !ok {
			return fmt.Errorf("the listing does not include %q", m.Key)
		}
		if s.DisplayName != m.DisplayName {
			return fmt.Errorf("%q is named %q, want its display name %q",
				m.Key, s.DisplayName, m.DisplayName)
		}
	}
	return nil
}

// stepListingOmits asks for a listing rather than searching the last command's
// output — a refusal names the key it refused, so searching that text would
// fail for the wrong reason.
func (w *operatorWorld) stepListingOmits(key string) error {
	statuses, err := w.admin.Listing(w.operatorCtx())
	if err != nil {
		return err
	}
	if _, present := findStatus(statuses, key); present {
		return fmt.Errorf("the listing reports %q, which nothing declared", key)
	}
	return nil
}

// --- the API --------------------------------------------------------------

func (w *operatorWorld) stepAPIList() error {
	return w.request(w.current, http.MethodGet, "/api/features", nil)
}

func (w *operatorWorld) stepAPISetInstance(key, onOff string) error {
	return w.request(w.current, http.MethodPut, "/api/features/"+key+"/instance",
		map[string]any{"enabled": onOff == "on"})
}

func (w *operatorWorld) stepAPITrySetInstanceOff(key string) error {
	return w.stepAPISetInstance(key, "off")
}

func (w *operatorWorld) stepAPIResetInstance(key string) error {
	return w.request(w.current, http.MethodDelete, "/api/features/"+key+"/instance", nil)
}

func (w *operatorWorld) stepAPISetAccount(key, onOff string) error {
	return w.request(w.current, http.MethodPut, "/api/features/"+key+"/account",
		map[string]any{"enabled": onOff == "on"})
}

func (w *operatorWorld) stepAPITrySetAccountOff(key string) error {
	return w.stepAPISetAccount(key, "off")
}

func (w *operatorWorld) stepAnonymousSet(key string) error {
	return w.request(nil, http.MethodPut, "/api/features/"+key+"/instance",
		map[string]any{"enabled": false})
}

func (w *operatorWorld) stepListingServed() error {
	if w.listingStatus != http.StatusOK {
		return fmt.Errorf("the listing answered %d, want 200: %s", w.listingStatus, string(w.listingBody))
	}
	return nil
}

// stepChangeAccepted works for whichever surface made the change. The contract
// phrases it the same way after an API call and after an MCP tool call, and it
// means the same thing both times.
func (w *operatorWorld) stepChangeAccepted() error {
	if w.changeStatus == 0 && w.lastMCPOut != nil || w.changeStatus == 0 && w.lastMCPErr != nil {
		if w.lastMCPErr != nil {
			return fmt.Errorf("the change was refused: %v", w.lastMCPErr)
		}
		return nil
	}
	return w.expectChange(http.StatusOK)
}

func (w *operatorWorld) stepRefusedForbidden() error {
	return w.expectChange(http.StatusForbidden)
}

func (w *operatorWorld) stepRefusedNotFound() error { return w.expectChange(http.StatusNotFound) }

func (w *operatorWorld) stepRefusedBadRequest() error {
	return w.expectChange(http.StatusBadRequest)
}

func (w *operatorWorld) stepRefusedUnauthenticated() error {
	return w.expectChange(http.StatusUnauthorized)
}

// expectChange asserts on the last WRITE, not on whatever call happened last.
// A scenario that reads the listing and is then refused a change asserts on
// both, and a single "last status" would report the refusal for both.
func (w *operatorWorld) expectChange(want int) error {
	if w.changeStatus != want {
		return fmt.Errorf("the change answered %d, want %d: %s",
			w.changeStatus, want, string(w.lastBody))
	}
	return nil
}

func (w *operatorWorld) stepRefusalMentionsAccount() error {
	if !strings.Contains(string(w.lastBody), "cannot be changed per account") {
		return fmt.Errorf("the refusal does not say why: %s", string(w.lastBody))
	}
	return nil
}

// --- MCP ------------------------------------------------------------------

func (w *operatorWorld) stepStdioClient() error {
	w.stdio = true
	return nil
}

func (w *operatorWorld) stepCallTool(tool string) error {
	return w.callMCP(w.current, false, tool, map[string]any{})
}

func (w *operatorWorld) stepCallToolSetOff(tool, key string) error {
	return w.callMCP(w.current, false, tool, map[string]any{"key": key, "enabled": false})
}

func (w *operatorWorld) stepStdioSetOff(tool, key string) error {
	return w.callMCP(nil, true, tool, map[string]any{"key": key, "enabled": false})
}

func (w *operatorWorld) stepMCPReset(key string) error {
	return w.callMCP(w.current, w.stdio, "feature_reset", map[string]any{"key": key})
}

func (w *operatorWorld) stepCallSucceeded() error {
	if w.lastMCPErr != nil {
		return fmt.Errorf("the tool call failed: %v", w.lastMCPErr)
	}
	return nil
}

func (w *operatorWorld) stepCallForbidden() error {
	if w.lastMCPErr == nil {
		return fmt.Errorf("the tool call succeeded, want a refusal")
	}
	if !strings.Contains(w.lastMCPErr.Error(), "forbidden") &&
		!strings.Contains(w.lastMCPErr.Error(), "owner or admin") {
		return fmt.Errorf("the refusal does not read as forbidden: %v", w.lastMCPErr)
	}
	return nil
}

// stepToolMatchesAPI is what stops the three surfaces drifting: the same
// question asked two ways must come back the same.
func (w *operatorWorld) stepToolMatchesAPI() error {
	fromTool, err := w.mcpFeatures()
	if err != nil {
		return err
	}
	if err := w.request(w.current, http.MethodGet, "/api/features", nil); err != nil {
		return err
	}
	fromAPI, err := w.listingFromBody()
	if err != nil {
		return err
	}
	if len(fromTool) != len(fromAPI) {
		return fmt.Errorf("the tool reported %d features, the API lists %d", len(fromTool), len(fromAPI))
	}
	for _, t := range fromTool {
		a, ok := findStatus(fromAPI, t.Key)
		if !ok {
			return fmt.Errorf("the tool reported %q, the API does not list it", t.Key)
		}
		if a.Enabled != t.Enabled || a.Source != t.Source {
			return fmt.Errorf("%q: the tool says %v/%s, the API says %v/%s",
				t.Key, t.Enabled, t.Source, a.Enabled, a.Source)
		}
	}
	return nil
}

// --- listings -------------------------------------------------------------

// currentListing takes whichever surface last answered. The contract phrases
// the assertion the same way after a command, an API call and a tool call, and
// it means the same thing each time.
// currentListing takes whichever surface last answered.
//
// When the last command printed an actual listing, that text is what the
// assertion checks. Otherwise "the listing reports X" means what a listing
// WOULD report — several scenarios flip a feature and then assert on the
// listing without ever asking for one, and reading that as "search the flip's
// output" would make the assertion pass on a message that merely contains the
// key.
func (w *operatorWorld) currentListing() ([]entities.FeatureStatus, error) {
	if w.lastMCPOut != nil {
		return w.mcpFeatures()
	}
	if len(w.lastBody) > 0 {
		return w.listingFromBody()
	}
	if strings.Contains(w.lastRun.said(), "KEY") {
		return nil, nil // a command-line listing, asserted against its text
	}
	return w.admin.Listing(w.operatorCtx())
}

func (w *operatorWorld) stepListingReportsInstance(key, onOff string) error {
	return w.assertListing(key, onOff, "instance")
}

func (w *operatorWorld) stepListingReportsDefault(key, onOff string) error {
	return w.assertListing(key, onOff, "default")
}

func (w *operatorWorld) assertListing(key, onOff, wantSource string) error {
	statuses, err := w.currentListing()
	if err != nil {
		return err
	}
	if statuses == nil {
		// A command-line listing: assert against what it printed.
		said := w.lastRun.said()
		wantState := onOff
		wantPhrase := map[string]string{
			"instance": "instance override",
			"account":  "account override",
			"default":  "declared default",
		}[wantSource]
		for _, line := range strings.Split(said, "\n") {
			if !strings.HasPrefix(line, key) {
				continue
			}
			if !strings.Contains(line, wantState) || !strings.Contains(line, wantPhrase) {
				return fmt.Errorf("the listing row for %q reads %q, want %s from the %s",
					key, strings.TrimSpace(line), wantState, wantPhrase)
			}
			return nil
		}
		return fmt.Errorf("the listing has no row for %q: %s", key, said)
	}

	s, ok := findStatus(statuses, key)
	if !ok {
		return fmt.Errorf("the listing does not include %q", key)
	}
	if s.Enabled != (onOff == "on") {
		return fmt.Errorf("%q is reported %v, want %s", key, s.Enabled, onOff)
	}
	if s.Source != wantSource {
		return fmt.Errorf("%q is reported from the %s, want the %s", key, s.Source, wantSource)
	}
	return nil
}

// --- stored state ---------------------------------------------------------

func (w *operatorWorld) stepNoInstanceSetting(key string) error {
	overrides, err := w.settings.InstanceOverrides(context.Background())
	if err != nil {
		return err
	}
	if _, present := overrides[key]; present {
		return fmt.Errorf("an instance-level setting is stored for %q, want none", key)
	}
	return nil
}

func (w *operatorWorld) stepNoAccountSetting(key string) error {
	if w.current == nil {
		return fmt.Errorf("nobody is signed in")
	}
	overrides, err := w.settings.AccountOverrides(context.Background(), w.current.accountID)
	if err != nil {
		return err
	}
	if _, present := overrides[key]; present {
		return fmt.Errorf("an account-level setting is stored for %q, want none", key)
	}
	return nil
}

// stepOneInstanceSetting counts rows directly. The repository returns a map,
// which collapses duplicates and so cannot answer "one" — and "one" is the
// assertion, because a second flip that inserted rather than replaced would
// leave two rows and resolve identically.
func (w *operatorWorld) stepOneInstanceSetting(key string) error {
	count, err := w.countInstanceRows(key)
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("%d instance-level rows are stored for %q, want exactly 1", count, key)
	}
	return nil
}

// --- evaluation and sessions ----------------------------------------------

func (w *operatorWorld) stepEvaluate(key string) error {
	if w.current == nil {
		return fmt.Errorf("nobody is signed in")
	}
	value, _, err := w.resolverEnabled(w.current, key)
	if err != nil {
		return err
	}
	w.lastEvalBool = &value
	w.lastEvaluatedKey = key
	return nil
}

func (w *operatorWorld) stepEvaluateAfterAge(key string) error {
	time.Sleep(w.maxCacheAge + 250*time.Millisecond)
	return w.stepEvaluate(key)
}

func (w *operatorWorld) stepSignInAndEvaluate(email, key string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	if err := w.signIn(p); err != nil {
		return err
	}
	value, _, err := w.resolverEnabled(p, key)
	if err != nil {
		return err
	}
	w.lastEvalBool = &value
	w.lastEvaluatedKey = key
	return nil
}

func (w *operatorWorld) stepFeatureAnswers(onOff string) error {
	if w.lastEvalBool == nil {
		return fmt.Errorf("nothing has been evaluated")
	}
	if *w.lastEvalBool != (onOff == "on") {
		return fmt.Errorf("the feature answered %v, want %s", *w.lastEvalBool, onOff)
	}
	return nil
}

// stepPersonAnswered resolves for the person named, rather than reading back
// whatever the last evaluation produced. A scenario can name two people and
// evaluate only one of them explicitly, and reading the shared last answer
// would report the same value for both.
func (w *operatorWorld) stepPersonAnswered(email, onOff string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	value, declared, err := w.resolverEnabled(p, w.lastEvaluatedKey)
	if err != nil {
		return err
	}
	if !declared {
		return fmt.Errorf("nothing declares %q", w.lastEvaluatedKey)
	}
	if value != (onOff == "on") {
		return fmt.Errorf("%q was answered %v for %q, want %s",
			email, value, w.lastEvaluatedKey, onOff)
	}
	return nil
}

func (w *operatorWorld) stepStillSignedIn() error {
	if w.current == nil {
		return fmt.Errorf("nobody is signed in")
	}
	for i, id := range w.current.sessionIDs {
		info, err := w.authService.ValidateSession(context.Background(), id)
		if err != nil || info == nil {
			return fmt.Errorf("session %d of %q is no longer valid: %w — changing a feature "+
				"must never sign anyone out", i+1, w.current.email, err)
		}
	}
	return nil
}

// stepNotRestarted holds the instance the scenario has been using to account.
// The point of the scenario is that a command-line flip reaches a server that
// stayed up, so a harness that quietly rebooted would prove nothing.
func (w *operatorWorld) stepNotRestarted() error {
	if w.app == nil {
		return fmt.Errorf("the instance is not running; the scenario needs the one it started with")
	}
	return nil
}

// --- the audit record -----------------------------------------------------

func (w *operatorWorld) stepRecorded(key string) error {
	event, err := w.lastFeatureChange()
	if err != nil {
		return err
	}
	if event.Key != key {
		return fmt.Errorf("the record names %q, want %q", event.Key, key)
	}
	if event.State == "" {
		return fmt.Errorf("the record carries no state for %q", key)
	}
	return nil
}

func (w *operatorWorld) stepRecordSourceCLI() error {
	event, err := w.lastFeatureChange()
	if err != nil {
		return err
	}
	if event.Source != entities.FeatureChangeSourceCLI {
		return fmt.Errorf("the record names %q as the source, want %q",
			event.Source, entities.FeatureChangeSourceCLI)
	}
	return nil
}

func (w *operatorWorld) stepRecordActor(email string) error {
	event, err := w.lastFeatureChange()
	if err != nil {
		return err
	}
	if event.ActorEmail != email {
		return fmt.Errorf("the record names %q as who made the change, want %q — a record "+
			"that answers \"who\" with an opaque id is one nobody can read later",
			event.ActorEmail, email)
	}
	return nil
}

func (w *operatorWorld) stepRecordTime() error {
	event, err := w.lastFeatureChange()
	if err != nil {
		return err
	}
	if event.Timestamp.IsZero() {
		return fmt.Errorf("the record carries no time")
	}
	return nil
}
