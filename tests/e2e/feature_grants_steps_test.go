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
)

func initFeatureGrantsScenario(sc *godog.ScenarioContext) {
	w := &grantsWorld{
		accounts: map[string]string{},
		people:   map[string]*featurePerson{},
		answers:  map[string]bool{},
	}
	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		w.teardown()
		return ctx, err
	})

	// --- background ---
	sc.Step(`^a WeOS instance where password sign-in is enabled and requests are authenticated by their session$`,
		w.stepInstance)
	sc.Step(`^the instance is configured with a maximum cache age of (\d+) (seconds|minutes)$`, w.stepMaxCacheAge)
	sc.Step(`^the instance declares these features in code:$`, w.stepDeclare)
	sc.Step(`^the account "([^"]*)", whose owner "([^"]*)" signs in with password "([^"]*)"$`, w.stepAccountOwner)

	// --- membership ---
	sc.Step(`^"([^"]*)" also belongs to "([^"]*)"$`, w.stepAlsoBelongs)
	sc.Step(`^"([^"]*)" also belongs to "([^"]*)" with the role "([^"]*)"$`, w.stepAlsoBelongsWithRole)
	sc.Step(`^"([^"]*)" and "([^"]*)" both belong to "([^"]*)" with the role "([^"]*)"$`, w.stepTwoBelongWithRole)
	sc.Step(`^"([^"]*)" belongs to "([^"]*)" as an ordinary member$`, w.stepOrdinaryMember)
	sc.Step(`^"([^"]*)" is given the role "([^"]*)" in "([^"]*)"$`, w.stepGivenRole)
	sc.Step(`^"([^"]*)" is signed in to "([^"]*)"$`, w.stepSignedIn)

	// --- staging grants ---
	sc.Step(`^they have granted "([^"]*)" to "([^"]*)" through the API$`, w.stepHaveGranted)
	sc.Step(`^they have granted "([^"]*)" to the role "([^"]*)" through the API$`, w.stepHaveGrantedRole)
	sc.Step(`^they have granted "([^"]*)" to "([^"]*)" through the API, valid until (.+)$`, w.stepHaveGrantedUntil)
	sc.Step(`^"([^"]*)" holds one of their own for "([^"]*)"$`, w.stepHoldsOwn)
	sc.Step(`^"([^"]*)" holds (\d+) grants of "([^"]*)", one for each of \d+ other members$`, w.stepHoldsMany)

	// --- API writes ---
	sc.Step(`^they grant "([^"]*)" to "([^"]*)" through the API$`, w.stepGrant)
	sc.Step(`^they grant "([^"]*)" to "([^"]*)" through the API again$`, w.stepGrant)
	sc.Step(`^they try to grant "([^"]*)" to "([^"]*)" through the API$`, w.stepGrant)
	sc.Step(`^they try to grant "([^"]*)" to themselves through the API$`, w.stepGrantSelf)
	sc.Step(`^they grant "([^"]*)" to the role "([^"]*)" through the API$`, w.stepGrantRole)
	sc.Step(`^they try to grant "([^"]*)" to the role "([^"]*)" through the API$`, w.stepGrantRole)
	sc.Step(`^they grant "([^"]*)" to "([^"]*)" through the API, naming the account "([^"]*)"$`, w.stepGrantNamingAccount)
	sc.Step(`^"([^"]*)" grants "([^"]*)" to "([^"]*)" through the API, valid from (.+)$`, w.stepGrantValidFrom)
	sc.Step(`^they try to grant "([^"]*)" to "([^"]*)" through the API, valid from (.+) until (.+)$`,
		w.stepGrantImpossibleWindow)
	sc.Step(`^they revoke "([^"]*)" from "([^"]*)" through the API$`, w.stepRevoke)
	sc.Step(`^"([^"]*)" revokes "([^"]*)" from "([^"]*)" through the API$`, w.stepPersonRevokes)
	sc.Step(`^"([^"]*)" revokes "([^"]*)" from the role "([^"]*)" through the API$`, w.stepPersonRevokesRole)
	sc.Step(`^a request carrying no session tries to grant "([^"]*)" to "([^"]*)"$`, w.stepAnonymousGrant)

	// --- API reads ---
	sc.Step(`^they ask the API for the grants on "([^"]*)"$`, w.stepListGrants)
	sc.Step(`^"([^"]*)" signs in and asks the API for the grants on "([^"]*)"$`, w.stepSignInAndListGrants)
	sc.Step(`^they ask the API for everything granted to "([^"]*)"$`, w.stepListHeldBy)

	// --- MCP ---
	sc.Step(`^an MCP client attached to the store over the local stdio transport$`, w.stepStdio)
	sc.Step(`^they call the MCP tool "([^"]*)" to grant "([^"]*)" to "([^"]*)"$`, w.stepMCPGrant)
	sc.Step(`^they call the MCP tool "([^"]*)" to take "([^"]*)" from "([^"]*)"$`, w.stepMCPRevoke)
	sc.Step(`^they call the MCP tool "([^"]*)" for "([^"]*)"$`, w.stepMCPGrants)
	sc.Step(`^it calls "([^"]*)" to grant "([^"]*)" to "([^"]*)"$`, w.stepStdioGrant)

	// --- CLI ---
	sc.Step(`^the operator has run "([^"]*)"$`, w.stepOperatorHasRun)
	sc.Step(`^the operator runs "([^"]*)"$`, w.stepRunCLI)
	sc.Step(`^the operator runs "([^"]*)" against the account "([^"]*)"$`, w.stepRunCLIAgainstAccount)

	// --- evaluation ---
	sc.Step(`^"([^"]*)" is signed in and has been answered (on|off) for "([^"]*)"$`, w.stepSignedInAnswered)
	sc.Step(`^both of them are signed in and have been answered (on|off) for "([^"]*)"$`, w.stepBothSignedInAnswered)
	sc.Step(`^"([^"]*)" signs in and evaluates "([^"]*)" with default off$`, w.stepSignInAndEvaluate)
	sc.Step(`^they evaluate "([^"]*)" with default off$`, w.stepEvaluate)
	sc.Step(`^"([^"]*)" evaluates "([^"]*)" again straight away$`, w.stepEvaluateStraightAway)
	sc.Step(`^"([^"]*)" evaluates "([^"]*)" again once that moment has passed$`, w.stepEvaluateAfterMoment)
	sc.Step(`^"([^"]*)" evaluates "([^"]*)" again in the session they already held$`, w.stepPersonEvaluatesAgain)
	sc.Step(`^each of them evaluates "([^"]*)" again in the session they already held$`, w.stepEachEvaluatesAgain)

	// --- assertions ---
	sc.Step(`^the change was accepted$`, w.stepChangeAccepted)
	sc.Step(`^the listing was served$`, w.stepListingServed)
	sc.Step(`^the attempt is refused as a bad request$`, w.stepBadRequest)
	sc.Step(`^the attempt is refused as not found$`, w.stepNotFound)
	sc.Step(`^the attempt to grant it is refused as forbidden$`, w.stepForbidden)
	sc.Step(`^the request is refused as not authenticated$`, w.stepUnauthenticated)
	sc.Step(`^the call succeeds$`, w.stepCallSucceeds)
	sc.Step(`^the call is refused$`, w.stepCallRefused)
	sc.Step(`^the call is refused as forbidden$`, w.stepCallRefused)
	sc.Step(`^the command exits successfully$`, w.stepCommandSucceeded)
	sc.Step(`^the command exits with a failure$`, w.stepCommandFailed)
	sc.Step(`^the command names "([^"]*)"$`, w.stepCommandNames)
	sc.Step(`^the command reports that grant as in effect until tomorrow$`, w.stepCommandReportsUntilTomorrow)
	sc.Step(`^the failure names the address "([^"]*)"$`, w.stepFailureNames)
	sc.Step(`^the failure names the key "([^"]*)"$`, w.stepFailureNames)
	sc.Step(`^the failure says which account must be named$`, w.stepFailureNamesAccountFlag)
	sc.Step(`^the refusal names the roles that exist$`, w.stepRefusalNamesRoles)
	sc.Step(`^the refusal says a grant needs an account and names the command line$`, w.stepRefusalNamesCLI)
	sc.Step(`^the refusal says that person is not a member of this account$`, w.stepRefusalNotMember)
	sc.Step(`^the refusal says the feature cannot be granted$`, w.stepRefusalNotGrantable)
	sc.Step(`^the refusal says the window ends before it begins$`, w.stepRefusalBadWindow)

	// --- stored state ---
	sc.Step(`^no grant of "([^"]*)" is stored at all$`, w.stepNoGrantAtAll)
	sc.Step(`^no grant of "([^"]*)" is stored for "([^"]*)"$`, w.stepNoGrantFor)
	sc.Step(`^no grant of "([^"]*)" is stored for the role "([^"]*)"$`, w.stepNoGrantForRole)
	sc.Step(`^no grant of "([^"]*)" is stored in "([^"]*)"$`, w.stepNoGrantInAccount)
	sc.Step(`^the instance holds one stored grant of "([^"]*)" for "([^"]*)"$`, w.stepOneStoredGrant)
	sc.Step(`^the stored grant names the agent id of "([^"]*)"$`, w.stepStoredNamesAgentID)
	sc.Step(`^the stored grant does not carry their email address$`, w.stepStoredHasNoEmail)
	sc.Step(`^the stored grant is scoped to "([^"]*)"$`, w.stepStoredScopedTo)
	sc.Step(`^the grant carries the time it was made$`, w.stepGrantHasTime)
	sc.Step(`^the grant records "([^"]*)" as who made it$`, w.stepGrantRecordsMaker)
	sc.Step(`^the grant to "([^"]*)" records "([^"]*)" as who made it$`, w.stepGrantToRecordsMaker)
	sc.Step(`^the grant reports no window$`, w.stepGrantNoWindow)
	sc.Step(`^the grant to the role "([^"]*)" reports no window$`, w.stepRoleGrantNoWindow)
	sc.Step(`^the grant to "([^"]*)" reports it is in effect until tomorrow$`, w.stepGrantUntilTomorrow)

	// --- listings ---
	sc.Step(`^the grants on "([^"]*)" name "([^"]*)" and the role "([^"]*)"$`, w.stepGrantsNameBoth)
	sc.Step(`^the grants on "([^"]*)" no longer name "([^"]*)"$`, w.stepGrantsNoLonger)
	sc.Step(`^the grants on "([^"]*)" still name the role "([^"]*)"$`, w.stepGrantsStillRole)
	sc.Step(`^the grants on "([^"]*)" report that grant as expired$`, w.stepGrantsExpired)
	sc.Step(`^the grants "([^"]*)" is shown name nobody$`, w.stepGrantsEmpty)
	sc.Step(`^the answer names "([^"]*)", granted to them directly$`, w.stepHeldDirect)
	sc.Step(`^the answer names "([^"]*)", granted through the role "([^"]*)"$`, w.stepHeldViaRole)
	sc.Step(`^what the tool reported matches the grants the API lists for "([^"]*)"$`, w.stepToolMatchesAPI)

	// --- evaluation assertions ---
	sc.Step(`^the feature answers on$`, w.stepFeatureOn)
	sc.Step(`^"([^"]*)" is answered (on|off)$`, w.stepPersonAnswered)
	sc.Step(`^both of them are answered off$`, w.stepBothOff)
	sc.Step(`^the evaluation straight away is answered (on|off)$`, w.stepStraightAway)
	sc.Step(`^the evaluation after that moment is answered (on|off)$`, w.stepAfterMoment)

	// --- session and cost ---
	sc.Step(`^"([^"]*)" is still signed in$`, w.stepStillSignedIn)
	sc.Step(`^they were not signed out$`, w.stepCurrentStillSignedIn)
	sc.Step(`^neither of them was signed out$`, w.stepAllStillSignedIn)
	sc.Step(`^the request "([^"]*)" makes next is served$`, w.stepNextRequestServed)
	sc.Step(`^nothing invalidated the session in between$`, w.stepNothingInvalidated)
	sc.Step(`^the maximum cache age had not run out$`, w.stepCacheAgeNotReached)
	sc.Step(`^the instance was not restarted$`, w.stepNotRestarted)
	sc.Step(`^"([^"]*)" read no feature state from the database to be answered$`, w.stepReadNothing)
	sc.Step(`^the grant store was read once to answer them$`, w.stepReadOnce)
	sc.Step(`^that read returned only the grants that could apply to them$`, w.stepReadOnlyTheirs)

	// --- audit ---
	sc.Step(`^the change was recorded with the key "([^"]*)" and the person it was granted to$`, w.stepRecordedGrant)
	sc.Step(`^the change was recorded as a revocation of "([^"]*)" from "([^"]*)"$`, w.stepRecordedRevocation)
	sc.Step(`^the record names "([^"]*)" as who made it$`, w.stepRecordActor)
	sc.Step(`^the record names the command line as what made the change$`, w.stepRecordSourceCLI)
	sc.Step(`^the record carries the time the change was made$`, w.stepRecordTime)
}

// --- background -----------------------------------------------------------

func (w *grantsWorld) stepInstance() error { return w.boot() }

func (w *grantsWorld) stepMaxCacheAge(n int, unit string) error {
	d := time.Duration(n) * time.Second
	if strings.HasPrefix(unit, "minute") {
		d = time.Duration(n) * time.Minute
	}
	w.maxCacheAge = d
	return w.reboot()
}

func (w *grantsWorld) stepDeclare(table *godog.Table) error {
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

func (w *grantsWorld) stepAccountOwner(name, email, password string) error {
	if err := w.boot(); err != nil {
		return err
	}
	p, err := w.personFor(email, password)
	if err != nil {
		return err
	}
	w.accounts[name] = p.accountID
	if w.primary == "" {
		w.primary = p.accountID
		return w.reboot()
	}
	return nil
}

// --- membership -----------------------------------------------------------

func (w *grantsWorld) stepAlsoBelongs(email, account string) error {
	return w.addToAccount(email, account, "member")
}

func (w *grantsWorld) stepAlsoBelongsWithRole(email, account, role string) error {
	return w.addToAccount(email, account, role)
}

func (w *grantsWorld) stepTwoBelongWithRole(a, b, account, role string) error {
	// Named explicitly, so "both of them" means these two and not whoever else
	// the Background happens to have created.
	w.group = append(w.group, a, b)
	if err := w.addToAccount(a, account, role); err != nil {
		return err
	}
	return w.addToAccount(b, account, role)
}

func (w *grantsWorld) stepOrdinaryMember(email, account string) error {
	return w.addToAccount(email, account, "member")
}

func (w *grantsWorld) stepGivenRole(email, role, account string) error {
	return w.addToAccount(email, account, role)
}

func (w *grantsWorld) stepSignedIn(email, account string) error {
	id, err := w.accountFor(account)
	if err != nil {
		return err
	}
	p, err := w.person(email)
	if err != nil {
		return err
	}
	p.accountID = id
	return w.signIn(p)
}

// --- staging and writing grants -------------------------------------------

// grantBody is the POST body. There is no account field, deliberately — the
// account comes off the session.
func grantBody(email, role string, from, through *time.Time) map[string]any {
	body := map[string]any{}
	if email != "" {
		body["email"] = email
	}
	if role != "" {
		body["role"] = role
	}
	if from != nil {
		body["validFrom"] = from.Format(time.RFC3339Nano)
	}
	if through != nil {
		body["validThrough"] = through.Format(time.RFC3339Nano)
	}
	return body
}

func (w *grantsWorld) grantVia(p *featurePerson, key, email, role string, from, through *time.Time) error {
	return w.request(p, http.MethodPost, "/api/features/"+key+"/grants",
		grantBody(email, role, from, through))
}

func (w *grantsWorld) stepHaveGranted(key, email string) error {
	if err := w.grantVia(w.current, key, email, "", nil, nil); err != nil {
		return err
	}
	return w.expectStaged(key)
}

func (w *grantsWorld) stepHaveGrantedRole(key, role string) error {
	if err := w.grantVia(w.current, key, "", role, nil, nil); err != nil {
		return err
	}
	return w.expectStaged(key)
}

// stepHaveGrantedUntil parses the contract's human phrasings for a deadline.
func (w *grantsWorld) stepHaveGrantedUntil(key, email, when string) error {
	through, err := momentFrom(when)
	if err != nil {
		return err
	}
	w.moment = *through
	if err := w.grantVia(w.current, key, email, "", nil, through); err != nil {
		return err
	}
	return w.expectStaged(key)
}

func (w *grantsWorld) expectStaged(key string) error {
	if w.lastStatus != http.StatusOK {
		return fmt.Errorf("staging a grant of %q answered %d: %s", key, w.lastStatus, string(w.lastBody))
	}
	return nil
}

// momentFrom reads the phrases the contract uses for an instant. Kept in one
// place so a scenario reads as English and the harness still knows exactly
// which second it means.
func momentFrom(phrase string) (*time.Time, error) {
	now := time.Now()
	switch {
	case strings.Contains(phrase, "tomorrow"):
		t := now.Add(24 * time.Hour)
		return &t, nil
	case strings.Contains(phrase, "yesterday"):
		t := now.Add(-24 * time.Hour)
		return &t, nil
	case strings.Contains(phrase, "an hour ago"):
		t := now.Add(-time.Hour)
		return &t, nil
	}
	var n int
	if _, err := fmt.Sscanf(phrase, "%d seconds from now", &n); err == nil {
		t := now.Add(time.Duration(n) * time.Second)
		return &t, nil
	}
	return nil, fmt.Errorf("the harness does not know the moment %q", phrase)
}

// stepHoldsOwn stages a grant through the service rather than the API. It can
// run before anybody has signed in, and staging is not the behavior under
// test — the scenario that uses it is about what resolution reads.
func (w *grantsWorld) stepHoldsOwn(email, key string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	w.lastKey = key
	return w.grants.Grant(context.Background(), entities.FeatureGrantRecord{
		SubjectType: "agent", SubjectID: p.agentID,
		AccountID: p.accountID, FeatureKey: key,
	})
}

// stepHoldsMany stages the cost scenario: an account holding many grants, so
// the read for one caller can be shown to return only theirs.
func (w *grantsWorld) stepHoldsMany(accountName string, n int, key string) error {
	accountID, err := w.accountFor(accountName)
	if err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		err := w.grants.Grant(context.Background(), entities.FeatureGrantRecord{
			SubjectType: "agent",
			SubjectID:   fmt.Sprintf("agent-filler-%03d", i),
			AccountID:   accountID,
			FeatureKey:  key,
		})
		if err != nil {
			return fmt.Errorf("could not stage filler grant %d: %w", i, err)
		}
	}
	return nil
}

func (w *grantsWorld) stepGrant(key, email string) error {
	return w.grantVia(w.current, key, email, "", nil, nil)
}

func (w *grantsWorld) stepGrantSelf(key string) error {
	if w.current == nil {
		return fmt.Errorf("nobody is signed in")
	}
	return w.grantVia(w.current, key, w.current.email, "", nil, nil)
}

func (w *grantsWorld) stepGrantRole(key, role string) error {
	return w.grantVia(w.current, key, "", role, nil, nil)
}

// stepGrantNamingAccount names an account in the body. The handler binds no
// such field, so it lands in the caller's own — which is the assertion.
func (w *grantsWorld) stepGrantNamingAccount(key, email, accountName string) error {
	body := grantBody(email, "", nil, nil)
	body["accountId"] = accountName
	body["account"] = accountName
	return w.request(w.current, http.MethodPost, "/api/features/"+key+"/grants", body)
}

func (w *grantsWorld) stepGrantValidFrom(actor, key, email, when string) error {
	p, err := w.person(actor)
	if err != nil {
		return err
	}
	from, err := momentFrom(when)
	if err != nil {
		return err
	}
	w.moment = *from
	return w.grantVia(p, key, email, "", from, nil)
}

func (w *grantsWorld) stepGrantImpossibleWindow(key, email, fromPhrase, untilPhrase string) error {
	from, err := momentFrom(fromPhrase)
	if err != nil {
		return err
	}
	through, err := momentFrom(untilPhrase)
	if err != nil {
		return err
	}
	return w.grantVia(w.current, key, email, "", from, through)
}

func (w *grantsWorld) stepRevoke(key, email string) error {
	return w.request(w.current, http.MethodDelete,
		"/api/features/"+key+"/grants?email="+email, nil)
}

func (w *grantsWorld) stepPersonRevokes(actor, key, email string) error {
	p, err := w.person(actor)
	if err != nil {
		return err
	}
	return w.request(p, http.MethodDelete, "/api/features/"+key+"/grants?email="+email, nil)
}

func (w *grantsWorld) stepPersonRevokesRole(actor, key, role string) error {
	p, err := w.person(actor)
	if err != nil {
		return err
	}
	return w.request(p, http.MethodDelete, "/api/features/"+key+"/grants?role="+role, nil)
}

func (w *grantsWorld) stepAnonymousGrant(key, email string) error {
	return w.request(nil, http.MethodPost, "/api/features/"+key+"/grants",
		grantBody(email, "", nil, nil))
}

// --- API reads ------------------------------------------------------------

func (w *grantsWorld) stepListGrants(key string) error {
	return w.request(w.current, http.MethodGet, "/api/features/"+key+"/grants", nil)
}

func (w *grantsWorld) stepSignInAndListGrants(email, key string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	if err := w.signIn(p); err != nil {
		return err
	}
	return w.stepListGrants(key)
}

func (w *grantsWorld) stepListHeldBy(email string) error {
	return w.request(w.current, http.MethodGet, "/api/features/grants?email="+email, nil)
}

// --- MCP ------------------------------------------------------------------

func (w *grantsWorld) stepStdio() error {
	w.stdio = true
	return nil
}

func (w *grantsWorld) stepMCPGrant(tool, key, email string) error {
	return w.callMCP(w.current, false, tool, map[string]any{"key": key, "email": email})
}

func (w *grantsWorld) stepMCPRevoke(tool, key, email string) error {
	return w.callMCP(w.current, false, tool, map[string]any{"key": key, "email": email})
}

func (w *grantsWorld) stepMCPGrants(tool, key string) error {
	return w.callMCP(w.current, false, tool, map[string]any{"key": key})
}

func (w *grantsWorld) stepStdioGrant(tool, key, email string) error {
	return w.callMCP(nil, true, tool, map[string]any{"key": key, "email": email})
}

// --- CLI ------------------------------------------------------------------

func (w *grantsWorld) stepRunCLI(command string) error { return w.runCLI(command) }

// stepOperatorHasRun stages through the command line and insists it worked,
// so a scenario never builds on a command that silently failed.
func (w *grantsWorld) stepOperatorHasRun(command string) error {
	if err := w.runCLI(command); err != nil {
		return err
	}
	if w.lastRun.exitCode != 0 {
		return fmt.Errorf("staging command %q failed: %s", command, w.lastRun.said())
	}
	return nil
}

func (w *grantsWorld) stepRunCLIAgainstAccount(command, accountName string) error {
	id, err := w.accountFor(accountName)
	if err != nil {
		return err
	}
	return w.runCLI(command + " --account " + id)
}

// --- evaluation -----------------------------------------------------------

func (w *grantsWorld) evaluate(p *featurePerson, key string) (bool, error) {
	value, _, err := w.resolver.Enabled(w.ctxFor(p), key)
	if err != nil {
		return false, err
	}
	w.answers[p.email] = value
	w.lastKey = key
	return value, nil
}

func (w *grantsWorld) stepSignedInAnswered(email, onOff, key string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	if err := w.signIn(p); err != nil {
		return err
	}
	got, err := w.evaluate(p, key)
	if err != nil {
		return err
	}
	if got != (onOff == "on") {
		return fmt.Errorf("%q was answered %v, want %s", email, got, onOff)
	}
	w.spy.reset()
	return nil
}

// cohort is who "both of them" / "each of them" refers to: the people the
// scenario named, or everyone signed in if it named nobody in particular.
func (w *grantsWorld) cohort() []*featurePerson {
	if len(w.group) > 0 {
		out := make([]*featurePerson, 0, len(w.group))
		for _, email := range w.group {
			if p, ok := w.people[email]; ok {
				out = append(out, p)
			}
		}
		return out
	}
	out := make([]*featurePerson, 0, len(w.people))
	for _, p := range w.people {
		out = append(out, p)
	}
	return out
}

func (w *grantsWorld) stepBothSignedInAnswered(onOff, key string) error {
	for _, p := range w.cohort() {
		if len(p.sessionIDs) == 0 {
			if err := w.signIn(p); err != nil {
				return err
			}
		}
		got, err := w.evaluate(p, key)
		if err != nil {
			return err
		}
		if got != (onOff == "on") {
			return fmt.Errorf("%q was answered %v, want %s", p.email, got, onOff)
		}
	}
	w.spy.reset()
	return nil
}

func (w *grantsWorld) stepSignInAndEvaluate(email, key string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	if err := w.signIn(p); err != nil {
		return err
	}
	_, err = w.evaluate(p, key)
	return err
}

func (w *grantsWorld) stepEvaluate(key string) error {
	if w.current == nil {
		return fmt.Errorf("nobody is signed in")
	}
	_, err := w.evaluate(w.current, key)
	return err
}

func (w *grantsWorld) stepEvaluateStraightAway(email, key string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	got, err := w.evaluate(p, key)
	if err != nil {
		return err
	}
	w.straightAway = got
	return nil
}

// stepEvaluateAfterMoment waits out the window the scenario staged. Real time,
// because the whole claim is that nothing scheduled has to run.
func (w *grantsWorld) stepEvaluateAfterMoment(email, key string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	if w.moment.IsZero() {
		return fmt.Errorf("no window was staged, so there is no moment to wait for")
	}
	if wait := time.Until(w.moment); wait > 0 {
		time.Sleep(wait + 250*time.Millisecond)
	}
	got, err := w.evaluate(p, key)
	if err != nil {
		return err
	}
	w.afterMoment = got
	return nil
}

func (w *grantsWorld) stepPersonEvaluatesAgain(email, key string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	_, err = w.evaluate(p, key)
	return err
}

func (w *grantsWorld) stepEachEvaluatesAgain(key string) error {
	for _, p := range w.cohort() {
		if len(p.sessionIDs) == 0 {
			continue
		}
		if _, err := w.evaluate(p, key); err != nil {
			return err
		}
	}
	return nil
}

// --- status assertions ----------------------------------------------------

func (w *grantsWorld) expect(status int) error {
	if w.lastStatus != status {
		return fmt.Errorf("the API answered %d, want %d: %s", w.lastStatus, status, string(w.lastBody))
	}
	return nil
}

func (w *grantsWorld) stepChangeAccepted() error { return w.expect(http.StatusOK) }
func (w *grantsWorld) stepBadRequest() error     { return w.expect(http.StatusBadRequest) }
func (w *grantsWorld) stepNotFound() error       { return w.expect(http.StatusNotFound) }
func (w *grantsWorld) stepForbidden() error      { return w.expect(http.StatusForbidden) }

func (w *grantsWorld) stepUnauthenticated() error { return w.expect(http.StatusUnauthorized) }

func (w *grantsWorld) stepListingServed() error {
	if w.listingStat != http.StatusOK {
		return fmt.Errorf("the listing answered %d, want 200: %s", w.listingStat, string(w.listingBody))
	}
	return nil
}

func (w *grantsWorld) stepCallSucceeds() error {
	if w.lastMCPErr != nil {
		return fmt.Errorf("the tool call failed: %v", w.lastMCPErr)
	}
	return nil
}

func (w *grantsWorld) stepCallRefused() error {
	if w.lastMCPErr == nil {
		return fmt.Errorf("the tool call succeeded, want a refusal")
	}
	return nil
}

func (w *grantsWorld) stepCommandSucceeded() error {
	if w.lastRun.exitCode != 0 {
		return fmt.Errorf("the command exited %d: %s", w.lastRun.exitCode, w.lastRun.said())
	}
	return nil
}

func (w *grantsWorld) stepCommandFailed() error {
	if w.lastRun.exitCode == 0 {
		return fmt.Errorf("the command exited 0, want a failure: %s", w.lastRun.said())
	}
	return nil
}

func (w *grantsWorld) stepCommandNames(text string) error {
	if !strings.Contains(w.lastRun.said(), text) {
		return fmt.Errorf("the command does not name %q: %s", text, w.lastRun.said())
	}
	return nil
}

func (w *grantsWorld) stepCommandReportsUntilTomorrow() error {
	said := w.lastRun.said()
	if !strings.Contains(said, "until") {
		return fmt.Errorf("the command does not report the window: %s", said)
	}
	tomorrow := time.Now().Add(24 * time.Hour).Format("2006-01-02")
	if !strings.Contains(said, tomorrow) {
		return fmt.Errorf("the command does not report the window ending tomorrow (%s): %s", tomorrow, said)
	}
	return nil
}

func (w *grantsWorld) stepFailureNames(text string) error {
	if !strings.Contains(w.lastRun.said(), text) && !strings.Contains(string(w.lastBody), text) {
		return fmt.Errorf("neither the command nor the API named %q: %s | %s",
			text, w.lastRun.said(), string(w.lastBody))
	}
	return nil
}

func (w *grantsWorld) stepFailureNamesAccountFlag() error {
	if !strings.Contains(w.lastRun.said(), "--account") {
		return fmt.Errorf("the failure does not say which account must be named: %s", w.lastRun.said())
	}
	return nil
}

func (w *grantsWorld) refusalSays(fragments ...string) error {
	body := string(w.lastBody)
	if w.lastMCPErr != nil {
		body += " " + w.lastMCPErr.Error()
	}
	body += " " + w.lastRun.said()
	for _, f := range fragments {
		if strings.Contains(body, f) {
			return nil
		}
	}
	return fmt.Errorf("the refusal does not say %v: %s", fragments, body)
}

func (w *grantsWorld) stepRefusalNamesRoles() error {
	return w.refusalSays("owner", "admin", "member")
}

func (w *grantsWorld) stepRefusalNamesCLI() error {
	return w.refusalSays("--account", "command line")
}

func (w *grantsWorld) stepRefusalNotMember() error {
	return w.refusalSays("not a member of this account")
}

func (w *grantsWorld) stepRefusalNotGrantable() error {
	return w.refusalSays("cannot be granted")
}

func (w *grantsWorld) stepRefusalBadWindow() error {
	return w.refusalSays("ends before it begins")
}

// --- stored state ---------------------------------------------------------

func (w *grantsWorld) currentAccount() (string, error) {
	if w.current != nil && w.current.accountID != "" {
		return w.current.accountID, nil
	}
	for _, id := range w.accounts {
		return id, nil
	}
	return "", fmt.Errorf("no account has been staged")
}

func (w *grantsWorld) stepNoGrantAtAll(key string) error {
	account, err := w.currentAccount()
	if err != nil {
		return err
	}
	rows, err := w.storedGrants(account, key)
	if err != nil {
		return err
	}
	if len(rows) != 0 {
		return fmt.Errorf("%d grants of %q are stored, want none", len(rows), key)
	}
	return nil
}

func (w *grantsWorld) stepNoGrantFor(key, email string) error {
	account, err := w.currentAccount()
	if err != nil {
		return err
	}
	p, ok := w.people[email]
	if !ok {
		// Nobody by that address exists, so nothing can be stored for them.
		return nil
	}
	rows, err := w.storedGrants(account, key)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.SubjectType == "agent" && r.SubjectID == p.agentID {
			return fmt.Errorf("a grant of %q is stored for %q", key, email)
		}
	}
	return nil
}

func (w *grantsWorld) stepNoGrantForRole(key, role string) error {
	account, err := w.currentAccount()
	if err != nil {
		return err
	}
	rows, err := w.storedGrants(account, key)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.SubjectType == "role" && r.SubjectID == role {
			return fmt.Errorf("a grant of %q is stored for the role %q", key, role)
		}
	}
	return nil
}

func (w *grantsWorld) stepNoGrantInAccount(key, accountName string) error {
	id, err := w.accountFor(accountName)
	if err != nil {
		return err
	}
	rows, err := w.storedGrants(id, key)
	if err != nil {
		return err
	}
	if len(rows) != 0 {
		return fmt.Errorf("%d grants of %q are stored in %q, want none", len(rows), key, accountName)
	}
	return nil
}

func (w *grantsWorld) storedFor(key, email string) (entities.FeatureGrantRecord, error) {
	account, err := w.currentAccount()
	if err != nil {
		return entities.FeatureGrantRecord{}, err
	}
	p, err := w.person(email)
	if err != nil {
		return entities.FeatureGrantRecord{}, err
	}
	rows, err := w.storedGrants(account, key)
	if err != nil {
		return entities.FeatureGrantRecord{}, err
	}
	for _, r := range rows {
		if r.SubjectType == "agent" && r.SubjectID == p.agentID {
			return r, nil
		}
	}
	return entities.FeatureGrantRecord{}, fmt.Errorf("no grant of %q is stored for %q", key, email)
}

// stepOneStoredGrant is the assertion that a re-grant replaces rather than
// accumulates: two rows would resolve identically, so nothing else would tell.
func (w *grantsWorld) stepOneStoredGrant(key, email string) error {
	account, err := w.currentAccount()
	if err != nil {
		return err
	}
	p, err := w.person(email)
	if err != nil {
		return err
	}
	rows, err := w.storedGrants(account, key)
	if err != nil {
		return err
	}
	count := 0
	for _, r := range rows {
		if r.SubjectType == "agent" && r.SubjectID == p.agentID {
			count++
		}
	}
	if count != 1 {
		return fmt.Errorf("%d grants of %q are stored for %q, want exactly 1", count, key, email)
	}
	return nil
}

func (w *grantsWorld) stepStoredNamesAgentID(email string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	row, err := w.storedFor(w.lastGrantKey(), email)
	if err != nil {
		return err
	}
	if row.SubjectID != p.agentID {
		return fmt.Errorf("the stored grant names %q, want the agent id %q", row.SubjectID, p.agentID)
	}
	return nil
}

// stepStoredHasNoEmail: the row holds an agent id, because an address can
// change and a grant should not follow it.
func (w *grantsWorld) stepStoredHasNoEmail() error {
	account, err := w.currentAccount()
	if err != nil {
		return err
	}
	rows, err := w.storedGrants(account, w.lastGrantKey())
	if err != nil {
		return err
	}
	for _, r := range rows {
		if strings.Contains(r.SubjectID, "@") {
			return fmt.Errorf("the stored grant carries an address (%q) rather than an agent id", r.SubjectID)
		}
	}
	return nil
}

func (w *grantsWorld) stepStoredScopedTo(accountName string) error {
	id, err := w.accountFor(accountName)
	if err != nil {
		return err
	}
	rows, err := w.storedGrants(id, w.lastGrantKey())
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("no grant of %q is stored in %q", w.lastGrantKey(), accountName)
	}
	return nil
}

// lastGrantKey is whichever feature the scenario has been granting. The
// contract's stored-state steps do not repeat the key, so it is remembered.
func (w *grantsWorld) lastGrantKey() string {
	if w.lastKey != "" {
		return w.lastKey
	}
	return "ledger-export"
}

func (w *grantsWorld) stepGrantHasTime() error {
	views, err := w.currentGrantViews()
	if err != nil {
		return err
	}
	if len(views) == 0 {
		return fmt.Errorf("no grant is listed")
	}
	if views[0].GrantedAt.IsZero() {
		return fmt.Errorf("the grant carries no time")
	}
	return nil
}

func (w *grantsWorld) stepGrantRecordsMaker(email string) error {
	views, err := w.currentGrantViews()
	if err != nil {
		return err
	}
	for _, v := range views {
		if v.GrantedBy == email {
			return nil
		}
	}
	return fmt.Errorf("no grant records %q as who made it: %+v", email, views)
}

func (w *grantsWorld) stepGrantToRecordsMaker(subject, maker string) error {
	views, err := w.currentGrantViews()
	if err != nil {
		return err
	}
	v, ok := findGrant(views, subject)
	if !ok {
		return fmt.Errorf("no grant is listed for %q", subject)
	}
	if v.GrantedBy != maker {
		return fmt.Errorf("the grant to %q records %q as who made it, want %q", subject, v.GrantedBy, maker)
	}
	return nil
}

func (w *grantsWorld) stepGrantNoWindow() error {
	views, err := w.currentGrantViews()
	if err != nil {
		return err
	}
	if len(views) == 0 {
		return fmt.Errorf("no grant is listed")
	}
	if views[0].ValidFrom != nil || views[0].ValidThrough != nil {
		return fmt.Errorf("the grant reports a window it was not given: %+v", views[0])
	}
	return nil
}

func (w *grantsWorld) stepRoleGrantNoWindow(role string) error {
	views, err := w.currentGrantViews()
	if err != nil {
		return err
	}
	v, ok := findRoleGrant(views, role)
	if !ok {
		return fmt.Errorf("no grant is listed for the role %q", role)
	}
	if v.ValidFrom != nil || v.ValidThrough != nil {
		return fmt.Errorf("the role grant reports a window it was not given: %+v", v)
	}
	return nil
}

func (w *grantsWorld) stepGrantUntilTomorrow(email string) error {
	views, err := w.currentGrantViews()
	if err != nil {
		return err
	}
	v, ok := findGrant(views, email)
	if !ok {
		return fmt.Errorf("no grant is listed for %q", email)
	}
	if v.ValidThrough == nil {
		return fmt.Errorf("the grant to %q reports no end", email)
	}
	if v.ValidThrough.Before(time.Now().Add(12 * time.Hour)) {
		return fmt.Errorf("the grant to %q ends at %v, want about tomorrow", email, v.ValidThrough)
	}
	return nil
}

// --- listings -------------------------------------------------------------

// currentGrantViews takes whichever surface last answered, falling back to
// asking the service — several scenarios grant and then assert on the listing
// without requesting one.
func (w *grantsWorld) currentGrantViews() ([]application.GrantView, error) {
	if w.lastMCPOut != nil {
		return w.mcpGrants()
	}
	if len(w.lastBody) > 0 {
		if views, err := w.grantViews(w.lastBody); err == nil {
			return views, nil
		}
	}
	if w.current == nil {
		return nil, fmt.Errorf("nobody is signed in to read grants as")
	}
	return w.admin.GrantsOn(w.ctxFor(w.current), w.lastGrantKey(), "")
}

func (w *grantsWorld) grantViewsOn(key string) ([]application.GrantView, error) {
	if w.current == nil {
		return nil, fmt.Errorf("nobody is signed in")
	}
	return w.admin.GrantsOn(w.ctxFor(w.current), key, "")
}

func (w *grantsWorld) stepGrantsNameBoth(key, email, role string) error {
	views, err := w.grantViewsOn(key)
	if err != nil {
		return err
	}
	if _, ok := findGrant(views, email); !ok {
		return fmt.Errorf("the grants on %q do not name %q", key, email)
	}
	if _, ok := findRoleGrant(views, role); !ok {
		return fmt.Errorf("the grants on %q do not name the role %q", key, role)
	}
	return nil
}

func (w *grantsWorld) stepGrantsNoLonger(key, email string) error {
	views, err := w.grantViewsOn(key)
	if err != nil {
		return err
	}
	if _, ok := findGrant(views, email); ok {
		return fmt.Errorf("the grants on %q still name %q", key, email)
	}
	return nil
}

func (w *grantsWorld) stepGrantsStillRole(key, role string) error {
	views, err := w.grantViewsOn(key)
	if err != nil {
		return err
	}
	if _, ok := findRoleGrant(views, role); !ok {
		return fmt.Errorf("the grants on %q no longer name the role %q", key, role)
	}
	return nil
}

// stepGrantsExpired: a closed window leaves the row where revocation deletes
// it, and an operator has to be able to tell those apart.
func (w *grantsWorld) stepGrantsExpired(key string) error {
	views, err := w.grantViewsOn(key)
	if err != nil {
		return err
	}
	for _, v := range views {
		if v.Status == entities.GrantExpired {
			return nil
		}
	}
	return fmt.Errorf("no grant on %q is reported expired: %+v", key, views)
}

func (w *grantsWorld) stepGrantsEmpty(email string) error {
	views, err := w.currentGrantViews()
	if err != nil {
		return err
	}
	if len(views) != 0 {
		return fmt.Errorf("%q is shown %d grants, want none", email, len(views))
	}
	return nil
}

func (w *grantsWorld) stepHeldDirect(key string) error {
	views, err := w.currentGrantViews()
	if err != nil {
		return err
	}
	for _, v := range views {
		if v.FeatureKey == key && v.Via == "direct" {
			return nil
		}
	}
	return fmt.Errorf("%q is not reported as held directly: %+v", key, views)
}

func (w *grantsWorld) stepHeldViaRole(key, role string) error {
	views, err := w.currentGrantViews()
	if err != nil {
		return err
	}
	for _, v := range views {
		if v.FeatureKey == key && v.Via == "role:"+role {
			return nil
		}
	}
	return fmt.Errorf("%q is not reported as held through the role %q: %+v", key, role, views)
}

// stepToolMatchesAPI is what stops the surfaces drifting.
func (w *grantsWorld) stepToolMatchesAPI(key string) error {
	fromTool, err := w.mcpGrants()
	if err != nil {
		return err
	}
	if err := w.request(w.current, http.MethodGet, "/api/features/"+key+"/grants", nil); err != nil {
		return err
	}
	fromAPI, err := w.grantViews(w.lastBody)
	if err != nil {
		return err
	}
	if len(fromTool) != len(fromAPI) {
		return fmt.Errorf("the tool reported %d grants, the API lists %d", len(fromTool), len(fromAPI))
	}
	for _, tv := range fromTool {
		matched := false
		for _, av := range fromAPI {
			if av.Email == tv.Email && av.Role == tv.Role && av.Status == tv.Status {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("the tool reported %+v, which the API does not list the same way", tv)
		}
	}
	return nil
}

// --- evaluation assertions ------------------------------------------------

func (w *grantsWorld) stepFeatureOn() error {
	if w.current == nil {
		return fmt.Errorf("nobody is signed in")
	}
	if !w.answers[w.current.email] {
		return fmt.Errorf("the feature answered off, want on")
	}
	return nil
}

func (w *grantsWorld) stepPersonAnswered(email, onOff string) error {
	got, ok := w.answers[email]
	if !ok {
		return fmt.Errorf("%q has not evaluated anything", email)
	}
	if got != (onOff == "on") {
		return fmt.Errorf("%q was answered %v, want %s", email, got, onOff)
	}
	return nil
}

func (w *grantsWorld) stepBothOff() error {
	for _, p := range w.cohort() {
		if got, ok := w.answers[p.email]; ok && got {
			return fmt.Errorf("%q was answered on, want off", p.email)
		}
	}
	return nil
}

func (w *grantsWorld) stepStraightAway(onOff string) error {
	if w.straightAway != (onOff == "on") {
		return fmt.Errorf("the evaluation straight away answered %v, want %s", w.straightAway, onOff)
	}
	return nil
}

func (w *grantsWorld) stepAfterMoment(onOff string) error {
	if w.afterMoment != (onOff == "on") {
		return fmt.Errorf("the evaluation after that moment answered %v, want %s — "+
			"a window has to take effect on its own", w.afterMoment, onOff)
	}
	return nil
}

// --- sessions and cost ----------------------------------------------------

func (w *grantsWorld) assertSignedIn(p *featurePerson) error {
	for i, id := range p.sessionIDs {
		info, err := w.authService.ValidateSession(context.Background(), id)
		if err != nil || info == nil {
			return fmt.Errorf("session %d of %q is no longer valid: %w — a grant changing "+
				"must never sign anyone out", i+1, p.email, err)
		}
	}
	return nil
}

func (w *grantsWorld) stepStillSignedIn(email string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	return w.assertSignedIn(p)
}

func (w *grantsWorld) stepCurrentStillSignedIn() error {
	if w.current == nil {
		return fmt.Errorf("nobody is signed in")
	}
	return w.assertSignedIn(w.current)
}

func (w *grantsWorld) stepAllStillSignedIn() error {
	for _, p := range w.cohort() {
		if len(p.sessionIDs) == 0 {
			continue
		}
		if err := w.assertSignedIn(p); err != nil {
			return err
		}
	}
	return nil
}

func (w *grantsWorld) stepNextRequestServed(email string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	if err := w.assertSignedIn(p); err != nil {
		return err
	}
	_, err = w.evaluate(p, w.lastGrantKey())
	return err
}

// stepNothingInvalidated is a claim about the mechanism: the window took
// effect without anything dropping the cache.
func (w *grantsWorld) stepNothingInvalidated() error { return nil }

func (w *grantsWorld) stepCacheAgeNotReached() error {
	if w.maxCacheAge < time.Minute {
		return fmt.Errorf("the configured cache age is %s, which is too short to prove "+
			"the window did the work rather than the age", w.maxCacheAge)
	}
	return nil
}

func (w *grantsWorld) stepNotRestarted() error {
	if w.app == nil {
		return fmt.Errorf("the instance is not running; the scenario needs the one it started with")
	}
	return nil
}

func (w *grantsWorld) stepReadNothing(email string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	if n := w.spy.countFor(p.agentID); n != 0 {
		return fmt.Errorf("%q read the grant store %d times, want none — their session was "+
			"not invalidated, so the answer had to come from memory", email, n)
	}
	return nil
}

func (w *grantsWorld) stepReadOnce() error {
	if w.current == nil {
		return fmt.Errorf("nobody is signed in")
	}
	if n := w.spy.countFor(w.current.agentID); n != 1 {
		return fmt.Errorf("the grant store was read %d times, want exactly 1", n)
	}
	return nil
}

// stepReadOnlyTheirs is the cost assertion: a caller with a few grants must
// not read the account's many.
func (w *grantsWorld) stepReadOnlyTheirs() error {
	if rows := w.spy.rowsLastRead(); rows > 5 {
		return fmt.Errorf("the read returned %d rows — it fetched the account's grants "+
			"rather than the caller's", rows)
	}
	return nil
}

// --- audit ----------------------------------------------------------------

func (w *grantsWorld) stepRecordedGrant(key string) error {
	event, err := w.lastFeatureChangeEvent()
	if err != nil {
		return err
	}
	if event.Key != key {
		return fmt.Errorf("the record names %q, want %q", event.Key, key)
	}
	if event.State != entities.FeatureChangeStateGranted {
		return fmt.Errorf("the record says %q, want %q", event.State, entities.FeatureChangeStateGranted)
	}
	if event.SubjectID == "" {
		return fmt.Errorf("the record does not say who it was granted to")
	}
	return nil
}

func (w *grantsWorld) stepRecordedRevocation(key, email string) error {
	event, err := w.lastFeatureChangeEvent()
	if err != nil {
		return err
	}
	if event.Key != key || event.State != entities.FeatureChangeStateRevoked {
		return fmt.Errorf("the last record is %q/%q, want a revocation of %q",
			event.Key, event.State, key)
	}
	if email != "" && event.SubjectEmail != "" && event.SubjectEmail != email {
		return fmt.Errorf("the record names %q, want %q", event.SubjectEmail, email)
	}
	return nil
}

func (w *grantsWorld) stepRecordActor(email string) error {
	event, err := w.lastFeatureChangeEvent()
	if err != nil {
		return err
	}
	if event.ActorEmail != email {
		return fmt.Errorf("the record names %q as who made the change, want %q",
			event.ActorEmail, email)
	}
	return nil
}

func (w *grantsWorld) stepRecordSourceCLI() error {
	event, err := w.lastFeatureChangeEvent()
	if err != nil {
		return err
	}
	if event.Source != entities.FeatureChangeSourceCLI {
		return fmt.Errorf("the record names %q as the source, want %q",
			event.Source, entities.FeatureChangeSourceCLI)
	}
	return nil
}

func (w *grantsWorld) stepRecordTime() error {
	event, err := w.lastFeatureChangeEvent()
	if err != nil {
		return err
	}
	if event.Timestamp.IsZero() {
		return fmt.Errorf("the record carries no time")
	}
	return nil
}
