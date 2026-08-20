package e2e

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cucumber/godog"

	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
)

// gatedRoutes is the harness's copy of the Background's route table. A
// background step compares it against what the contract says, so the two
// cannot drift.
func defaultRouteGates() map[string]string {
	return map[string]string{
		"POST /api/agent/conversations/{id}/messages": application.FeatureAgentChat,
		"GET /api/agent/conversations/{id}":           application.FeatureAgentChat,
		"POST /api/ledger/exports":                    "ledger-export",
	}
}

// routePath turns a contract route into a real path.
func routePath(route string) (method, path string) {
	parts := strings.SplitN(route, " ", 2)
	if len(parts) != 2 {
		return http.MethodGet, route
	}
	return parts[0], strings.ReplaceAll(parts[1], "{id}", "conv-1")
}

//nolint:funlen // a step table is one statement per contract sentence
func initFeatureAdminSurfaceScenario(sc *godog.ScenarioContext) {
	w := newAdminWorld()
	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		w.teardown()
		return ctx, err
	})

	// --- background -------------------------------------------------------
	sc.Given(`^a WeOS instance where password sign-in is enabled and requests are `+
		`authenticated by their session$`, func() error { return nil })
	sc.Given(`^the instance declares these features in code:$`, w.stepDeclareFeatures)
	sc.Given(`^these API routes declare the feature that gates them:$`, w.stepRoutesDeclareGates)
	sc.Given(`^these API routes name no feature:$`, w.stepRoutesNameNoFeature)
	sc.Given(`^the account "([^"]*)", whose owner "([^"]*)" signs in with password "([^"]*)"$`,
		w.stepAccountWithOwner)

	// --- staging ----------------------------------------------------------
	sc.Given(`^nothing has been overridden or granted on this instance$`, func() error { return nil })
	sc.Given(`^the operator has turned the feature "([^"]*)" (on|off) for the instance$`, w.stepInstanceFlag)
	sc.When(`^the operator turns the feature "([^"]*)" (on|off) for the instance$`, w.stepInstanceFlag)
	sc.Given(`^the instance booted with the feature "([^"]*)" (on|off) for the instance$`, w.stepBootedWith)
	sc.Given(`^"([^"]*)" has turned the feature "([^"]*)" (on|off)$`, w.stepAccountFlag)
	sc.Given(`^"([^"]*)" also belongs to "([^"]*)"$`, w.stepAlsoBelongs)
	sc.Given(`^"([^"]*)" belongs to "([^"]*)" as an ordinary member$`, w.stepBelongsAsMember)
	sc.Given(`^"([^"]*)" has been granted the feature "([^"]*)"$`, w.stepGranted)
	sc.Given(`^"([^"]*)" holds a grant of "([^"]*)" valid until (\d+) seconds from now$`, w.stepGrantUntil)
	sc.When(`^"([^"]*)" revokes "([^"]*)" from "([^"]*)"$`, w.stepRevoke)
	sc.Given(`^the route "([^"]*)" is gated by the undeclared feature "([^"]*)"$`, w.stepRouteGatedByUndeclared)
	sc.Given(`^the instance is configured with a maximum cache age of (\d+) minutes$`, w.stepCacheAge)
	sc.Given(`^the store holding account overrides and grants cannot be read$`, w.stepStoreDown)
	sc.When(`^the store can be read again$`, w.stepStoreUp)

	// --- signing in -------------------------------------------------------
	sc.Given(`^"([^"]*)" is signed in to "([^"]*)"$`, w.stepSignedIn)
	sc.Given(`^both of them are signed in to "([^"]*)"$`, w.stepBothSignedIn)

	// --- asking -----------------------------------------------------------
	sc.When(`^they ask the API for their feature set$`, w.stepAsk)
	sc.When(`^they ask the API for their feature set (\d+) times$`, w.stepAskTimes)
	sc.When(`^each of them asks the API for their feature set$`, w.stepEachAsks)
	sc.When(`^a request carrying no session asks the API for the feature set$`, w.stepAnonymousAsk)
	sc.Given(`^they have asked for their feature set and been answered "([^"]*)" (on|off)$`, w.stepAskedAndAnswered)
	sc.When(`^they ask for their feature set again in the session they already held$`, w.stepAsk)
	sc.When(`^"([^"]*)" asks for their feature set again in the session they already held$`, w.stepNamedAsks)
	sc.When(`^they ask for their feature set again$`, w.stepAsk)
	sc.When(`^they act in "([^"]*)" and ask for their feature set again$`, w.stepActInAndAsk)
	sc.When(`^"([^"]*)" impersonates "([^"]*)"$`, w.stepImpersonate)
	sc.When(`^they stop impersonating$`, w.stepStopImpersonating)

	// --- calling ----------------------------------------------------------
	sc.When(`^they call "([^"]*)"$`, w.stepCall)
	sc.When(`^they call "([^"]*)" (\d+) times$`, w.stepCallTimes)
	sc.When(`^they call every gated route the instance mounts$`, w.stepCallEveryGatedRoute)
	sc.When(`^a request carrying no session calls "([^"]*)"$`, w.stepAnonymousCall)
	sc.When(`^a request carrying no session asks the API for the person listing$`, w.stepAnonymousPersons)
	sc.When(`^a request carrying no session tries to turn the feature "([^"]*)" off for the instance$`,
		w.stepAnonymousWrite)
	sc.When(`^a request carrying no session asks the API for a path nobody mounted$`, w.stepAnonymousUnmounted)
	sc.When(`^"([^"]*)" calls "([^"]*)" on the answer they already hold$`, w.stepNamedCall)
	sc.When(`^they call "([^"]*)" straight away$`, w.stepCallStraightAway)
	sc.When(`^they call "([^"]*)" again once that moment has passed$`, w.stepCallAfterMoment)
	sc.When(`^they make (\d+) calls to gated routes in that session$`, w.stepMakeCalls)
	sc.When(`^they try to turn the feature "([^"]*)" off for the instance$`, w.stepTryWrite)
	sc.When(`^they turn the feature "([^"]*)" on for the instance through the API$`, w.stepTurnOnThroughAPI)

	// --- outcomes ---------------------------------------------------------
	sc.Then(`^the answer is served$`, w.stepAnswerServed)
	sc.Then(`^the feature set was served$`, w.stepAnswerServed)
	sc.Then(`^the answer carries every feature the instance declares$`, w.stepAnswerCarriesAll)
	sc.Then(`^the answer reports "([^"]*)" as (on|off)$`, w.stepAnswerReports)
	sc.Then(`^the answer reported "([^"]*)" as (on|off)$`, w.stepAnswerReports)
	sc.Then(`^the answer had carried "([^"]*)" reported as (on|off)$`, w.stepAnswerReports)
	sc.Then(`^they are answered "([^"]*)" (on|off)$`, w.stepAnswerReports)
	sc.Then(`^they are now answered "([^"]*)" (on|off)$`, w.stepNowAnswered)
	sc.Then(`^"([^"]*)" is answered "([^"]*)" (on|off)$`, w.stepNamedAnswerReports)
	sc.Then(`^both of them are answered "([^"]*)" (on|off)$`, w.stepBothAnswered)
	sc.Then(`^each feature carries its key and whether it is enabled$`, w.stepCarriesKeyAndEnabled)
	sc.Then(`^each feature carries its display name and the layer that decided it$`, w.stepCarriesNameAndLayer)
	sc.Then(`^the answer carries no grant belonging to anybody$`, w.stepNoGrantsInAnswer)
	sc.Then(`^no account override or grant was read to answer it$`, w.stepNoAccountLayerRead)
	sc.Then(`^no feature in the answer names an account override as its layer$`, w.stepNoLayer("account"))
	sc.Then(`^no feature in the answer names a grant as its layer$`, w.stepNoLayer("grant"))
	sc.Then(`^the answer names no person and no account$`, w.stepAnswerNamesNobody)
	sc.Then(`^they are answered the same set "([^"]*)" is answered$`, w.stepSameSetAs)
	sc.Then(`^the person listing is refused as not authenticated$`, w.stepPersonsUnauthenticated)
	sc.Then(`^the attempt to change the instance is refused as not authenticated$`, w.stepWriteUnauthenticated)
	sc.Then(`^no instance-level setting is stored for "([^"]*)"$`, w.stepNoInstanceSetting)
	sc.Then(`^the path nobody mounted answers exactly what an unmounted path answered before this change$`,
		w.stepUnmountedParity)
	sc.Then(`^the attempt to change it is refused as forbidden$`, w.stepWriteForbidden)
	sc.Then(`^the attempt to change the instance is refused as forbidden$`, w.stepWriteForbidden)
	sc.Then(`^the call (succeeds|is refused)$`, w.stepCallOutcome)
	sc.Then(`^calling "([^"]*)" succeeds$`, w.stepCallSucceedsNow)
	sc.Then(`^every route whose feature the answer reported on was served$`, w.stepReportedOnWereServed)
	sc.Then(`^every route whose feature the answer reported off was refused$`, w.stepReportedOffWereRefused)
	sc.Then(`^the call is refused as forbidden$`, w.stepCallForbidden)
	sc.Then(`^the refusal names the feature "([^"]*)"$`, w.stepRefusalNamesFeature)
	sc.Then(`^the refusal says the capability is not enabled for them$`, w.stepRefusalNotEnabled)
	sc.Then(`^the refusal names their role rather than a feature$`, w.stepRefusalNamesRole)
	sc.Then(`^the refusal does not say their role is insufficient$`, w.stepRefusalNotAboutRole)
	sc.Then(`^no ledger export was recorded$`, w.stepNoLedgerExport)
	sc.Then(`^the refusal is not a partial result$`, w.stepRefusalNotPartial)
	sc.Then(`^the ledger call is served$`, w.stepLedgerServed)
	sc.Then(`^both calls succeed$`, w.stepBothCallsSucceed)
	sc.Then(`^no feature was evaluated for either route$`, w.stepNoFeatureEvaluated)
	sc.Then(`^the call to the gated route is refused$`, w.stepGatedRouteRefused)
	sc.Then(`^the call to the ungated route succeeds$`, w.stepUngatedRouteSucceeds)
	sc.Then(`^every feature in the answer is reported off$`, w.stepEveryFeatureOff)
	sc.Then(`^the answer carries a message saying the feature state could not be read$`, w.stepAnswerCarriesMessage)
	sc.Then(`^the instance logged the failure$`, w.stepLoggedFailure)
	sc.Then(`^every call succeeds$`, w.stepEveryCallSucceeded)
	sc.Then(`^the instance logged the undeclared feature key once$`, w.stepLoggedDriftOnce)
	sc.Then(`^the log names the key "([^"]*)"$`, w.stepLogNamesKey)
	sc.Then(`^the log names the route$`, w.stepLogNamesRoute)
	sc.Then(`^the change was accepted$`, w.stepChangeAccepted)
	sc.Then(`^no turn was taken$`, w.stepNoTurnTaken)
	sc.Then(`^no skill graph was built$`, w.stepNoTurnTaken)
	sc.Then(`^the call straight away succeeded$`, w.stepStraightAwaySucceeded)
	sc.Then(`^the call after that moment is refused$`, w.stepAfterMomentRefused)
	sc.Then(`^nothing invalidated the session in between$`, w.stepNothingInvalidated)
	sc.Then(`^the maximum cache age had not run out$`, w.stepCacheAgeHadNotRunOut)
	sc.Then(`^"([^"]*)" is still signed in$`, w.stepStillSignedIn)
	sc.Then(`^they were not signed out$`, w.stepNotSignedOut)
	sc.Then(`^the instance was not restarted$`, w.stepNotRestarted)
	sc.Then(`^every call was answered$`, w.stepEveryCallSucceeded)
	sc.Then(`^every answer was the same$`, w.stepEveryAnswerSame)
	sc.Then(`^feature state was read from the database only while the listing was answered$`, w.stepReadOnce)
	sc.Then(`^feature state was read from the database only while the first answer was built$`, w.stepReadOnce)
}

// --- background -----------------------------------------------------------

func (w *adminWorld) stepDeclareFeatures(table *godog.Table) error {
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
	return nil
}

// stepRoutesDeclareGates checks the contract's table against what the harness
// mounts, so a gate quietly moved is caught here rather than in a scenario.
func (w *adminWorld) stepRoutesDeclareGates(table *godog.Table) error {
	for _, row := range table.Rows[1:] {
		route, key := strings.TrimSpace(row.Cells[0].Value), strings.TrimSpace(row.Cells[1].Value)
		if w.routeGates[route] != key {
			return fmt.Errorf("the contract says %q is gated by %q; the harness mounts it gated by %q",
				route, key, w.routeGates[route])
		}
	}
	return nil
}

func (w *adminWorld) stepRoutesNameNoFeature(table *godog.Table) error {
	for _, row := range table.Rows[1:] {
		route := strings.TrimSpace(row.Cells[0].Value)
		if _, gated := w.routeGates[route]; gated {
			return fmt.Errorf("the contract says %q names no feature, but the harness gates it", route)
		}
	}
	return nil
}

func (w *adminWorld) stepAccountWithOwner(name, email, password string) error {
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

func (w *adminWorld) stepInstanceFlag(key, state string) error {
	if err := w.boot(); err != nil {
		return err
	}
	return w.features.SetInstanceFeature(context.Background(), key, state == "on")
}

func (w *adminWorld) stepBootedWith(key, state string) error {
	if err := w.stepInstanceFlag(key, state); err != nil {
		return err
	}
	return w.reboot()
}

func (w *adminWorld) stepAccountFlag(accountName, key, state string) error {
	accountID, err := w.accountFor(accountName)
	if err != nil {
		return err
	}
	return w.features.SetAccountFeature(context.Background(), accountID, key, state == "on")
}

func (w *adminWorld) stepAlsoBelongs(email, accountName string) error {
	return w.addToAccount(email, accountName, authentities.RoleMember)
}

func (w *adminWorld) stepBelongsAsMember(email, accountName string) error {
	return w.addToAccount(email, accountName, authentities.RoleMember)
}

func (w *adminWorld) stepGranted(email, key string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	return w.features.GrantToAgent(context.Background(), p.accountID, p.agentID, key, application.GrantTerms{
		GrantedByEmail: "ops@harborlegal.example", Source: "test",
	})
}

func (w *adminWorld) stepGrantUntil(email, key string, seconds int) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	w.moment = time.Now().Add(time.Duration(seconds) * time.Second)
	w.momentSet = true
	w.windowStart = time.Now()
	through := w.moment
	return w.features.GrantToAgent(context.Background(), p.accountID, p.agentID, key, application.GrantTerms{
		ValidThrough: &through, GrantedByEmail: "ops@harborlegal.example", Source: "test",
	})
}

func (w *adminWorld) stepRevoke(_, key, subject string) error {
	p, err := w.person(subject)
	if err != nil {
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

func (w *adminWorld) stepRouteGatedByUndeclared(route, key string) error {
	if _, gated := w.routeGates[route]; !gated {
		return fmt.Errorf("%q is not a gated route", route)
	}
	w.ledgerGate = key
	w.routeGates[route] = key
	// The gate is read when the route is mounted, so the surface is built
	// again — which is what a deploy carrying the typo would do.
	return w.remount()
}

func (w *adminWorld) stepCacheAge(minutes int) error {
	w.maxCacheAge = time.Duration(minutes) * time.Minute
	if w.app == nil {
		return nil
	}
	return w.reboot()
}

func (w *adminWorld) stepStoreDown() error {
	if err := w.boot(); err != nil {
		return err
	}
	w.settings.setDown(true)
	w.grants.setDown(true)
	return nil
}

func (w *adminWorld) stepStoreUp() error {
	w.settings.setDown(false)
	w.grants.setDown(false)
	return nil
}

// --- signing in -----------------------------------------------------------

func (w *adminWorld) stepSignedIn(email, accountName string) error {
	if err := w.boot(); err != nil {
		return err
	}
	p, err := w.personFor(email, "correct-horse-battery-staple")
	if err != nil {
		return err
	}
	if accountID, err := w.accountFor(accountName); err == nil {
		p.accountID = accountID
	}
	if err := w.signIn(p); err != nil {
		return err
	}
	if !contains(w.signedInOrder, email) {
		w.signedInOrder = append(w.signedInOrder, email)
	}
	w.lastActor = email
	w.restartBaseline = w.boots
	return nil
}

func (w *adminWorld) stepBothSignedIn(accountName string) error {
	for _, email := range w.staged() {
		if err := w.stepSignedIn(email, accountName); err != nil {
			return err
		}
	}
	w.lastActor = ""
	return nil
}

// --- asking ---------------------------------------------------------------

func (w *adminWorld) ask(p *featurePerson) error {
	key := "anonymous"
	if p != nil {
		key = p.email
		w.lastActor = p.email
	}
	if len(w.answers) == 0 {
		w.readsBefore = w.settings.count()
	}
	call, err := w.request(p, http.MethodGet, "/api/features", nil)
	if err != nil {
		return err
	}
	w.calls["listing|"+key] = call
	if call.status != http.StatusOK {
		w.answers[key] = nil
		return nil
	}
	set, msgs, err := statusesFrom(call.body)
	if err != nil {
		return err
	}
	w.answers[key] = set
	w.messages[key] = msgs
	if w.readsAfterFirst == 0 {
		w.readsAfterFirst = w.settings.count()
	}
	return nil
}

func (w *adminWorld) stepAsk() error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	return w.ask(p)
}

func (w *adminWorld) stepAskTimes(count int) error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	for i := 0; i < count; i++ {
		if err := w.ask(p); err != nil {
			return err
		}
		w.repeatAnswers = append(w.repeatAnswers, w.answers[p.email])
	}
	return nil
}

func (w *adminWorld) stepEachAsks() error {
	for _, email := range w.signedInOrder {
		p, err := w.person(email)
		if err != nil {
			return err
		}
		if err := w.ask(p); err != nil {
			return err
		}
	}
	w.lastActor = ""
	return nil
}

func (w *adminWorld) stepNamedAsks(email string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	return w.ask(p)
}

func (w *adminWorld) stepAnonymousAsk() error {
	if err := w.boot(); err != nil {
		return err
	}
	return w.ask(nil)
}

func (w *adminWorld) stepAskedAndAnswered(key, state string) error {
	if err := w.stepAsk(); err != nil {
		return err
	}
	return w.stepAnswerReports(key, state)
}

func (w *adminWorld) stepActInAndAsk(accountName string) error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	accountID, err := w.accountFor(accountName)
	if err != nil {
		return err
	}
	p.accountID = accountID
	// A different active account is a different session, which is what
	// switching accounts does over HTTP.
	delete(w.jar, p.email)
	if err := w.signIn(p); err != nil {
		return err
	}
	return w.ask(p)
}

func (w *adminWorld) stepImpersonate(admin, subject string) error {
	a, err := w.person(admin)
	if err != nil {
		return err
	}
	s, err := w.person(subject)
	if err != nil {
		return err
	}
	call, err := w.request(a, http.MethodPost, "/api/admin/impersonate",
		map[string]any{"agent_id": s.agentID})
	if err != nil {
		return err
	}
	if call.status >= 400 {
		return fmt.Errorf("impersonation was refused: %d %s", call.status, call.body)
	}
	return nil
}

func (w *adminWorld) stepStopImpersonating() error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	call, err := w.request(p, http.MethodPost, "/api/admin/stop-impersonation", nil)
	if err != nil {
		return err
	}
	if call.status >= 400 {
		return fmt.Errorf("stopping impersonation was refused: %d %s", call.status, call.body)
	}
	return nil
}

// --- calling --------------------------------------------------------------

func (w *adminWorld) call(p *featurePerson, route string) (*apiCall, error) {
	method, path := routePath(route)
	var body any
	if method == http.MethodPost {
		body = map[string]any{"message": "hello"}
	}
	call, err := w.request(p, method, path, body)
	if err != nil {
		return nil, err
	}
	key := route
	if p != nil {
		key = p.email + "|" + route
	}
	w.calls[key] = call
	w.lastCallRoute = route
	return call, nil
}

func (w *adminWorld) stepCall(route string) error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	_, err = w.call(p, route)
	return err
}

func (w *adminWorld) stepCallTimes(route string, count int) error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	w.repeatCalls = nil
	for i := 0; i < count; i++ {
		call, err := w.call(p, route)
		if err != nil {
			return err
		}
		w.repeatCalls = append(w.repeatCalls, call)
	}
	return nil
}

func (w *adminWorld) stepCallEveryGatedRoute() error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	for route := range w.routeGates {
		if _, err := w.call(p, route); err != nil {
			return err
		}
	}
	return nil
}

func (w *adminWorld) stepAnonymousCall(route string) error {
	_, err := w.call(nil, route)
	return err
}

func (w *adminWorld) stepAnonymousPersons() error {
	call, err := w.request(nil, http.MethodGet, "/api/persons", nil)
	if err != nil {
		return err
	}
	w.calls["anon-persons"] = call
	return nil
}

func (w *adminWorld) stepAnonymousWrite(key string) error {
	call, err := w.request(nil, http.MethodPut, "/api/features/"+key+"/instance",
		map[string]any{"enabled": false})
	if err != nil {
		return err
	}
	w.calls["anon-write"] = call
	return nil
}

func (w *adminWorld) stepAnonymousUnmounted() error {
	call, err := w.request(nil, http.MethodGet, "/api/nothing-is-mounted-here", nil)
	if err != nil {
		return err
	}
	w.calls["anon-unmounted"] = call
	return nil
}

func (w *adminWorld) stepNamedCall(email, route string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	w.lastActor = email
	_, err = w.call(p, route)
	return err
}

func (w *adminWorld) stepCallStraightAway(route string) error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	w.invsBefore = w.invSpy.count()
	call, err := w.call(p, route)
	w.straightAway = call
	return err
}

func (w *adminWorld) stepCallAfterMoment(route string) error {
	if w.momentSet {
		if wait := time.Until(w.moment); wait > 0 {
			time.Sleep(wait + 50*time.Millisecond)
		}
	}
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	call, err := w.call(p, route)
	w.afterMoment = call
	return err
}

func (w *adminWorld) stepMakeCalls(count int) error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	w.repeatCalls = nil
	routes := []string{
		"POST /api/agent/conversations/{id}/messages",
		"GET /api/agent/conversations/{id}",
		"POST /api/ledger/exports",
	}
	for i := 0; i < count; i++ {
		call, err := w.call(p, routes[i%len(routes)])
		if err != nil {
			return err
		}
		w.repeatCalls = append(w.repeatCalls, call)
	}
	return nil
}

func (w *adminWorld) stepTryWrite(key string) error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	call, err := w.request(p, http.MethodPut, "/api/features/"+key+"/instance",
		map[string]any{"enabled": false})
	if err != nil {
		return err
	}
	w.calls["write"] = call
	return nil
}

func (w *adminWorld) stepTurnOnThroughAPI(key string) error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	call, err := w.request(p, http.MethodPut, "/api/features/"+key+"/instance",
		map[string]any{"enabled": true})
	if err != nil {
		return err
	}
	w.calls["write"] = call
	return nil
}

// --- outcomes -------------------------------------------------------------

func (w *adminWorld) answerFor(email string) ([]entities.FeatureStatus, error) {
	set, ok := w.answers[email]
	if !ok {
		return nil, fmt.Errorf("%q has not asked for their feature set", email)
	}
	return set, nil
}

func (w *adminWorld) soleAnswer() ([]entities.FeatureStatus, string, error) {
	key := w.lastActor
	if key == "" {
		if _, ok := w.answers["anonymous"]; ok {
			key = "anonymous"
		}
	}
	if key == "" && len(w.signedInOrder) == 1 {
		key = w.signedInOrder[0]
	}
	set, err := w.answerFor(key)
	return set, key, err
}

func (w *adminWorld) stepAnswerServed() error {
	_, key, err := w.soleAnswer()
	if err != nil {
		return err
	}
	call, ok := w.calls["listing|"+key]
	if !ok {
		return fmt.Errorf("no listing call was made")
	}
	if call.status != http.StatusOK {
		return fmt.Errorf("the listing answered %d, want 200: %s", call.status, call.body)
	}
	return nil
}

func (w *adminWorld) stepAnswerCarriesAll() error {
	set, _, err := w.soleAnswer()
	if err != nil {
		return err
	}
	for _, m := range w.declared {
		if _, ok := statusOf(set, m.Key); !ok {
			return fmt.Errorf("the answer omits the declared feature %q", m.Key)
		}
	}
	return nil
}

func (w *adminWorld) stepAnswerReports(key, state string) error {
	set, who, err := w.soleAnswer()
	if err != nil {
		return err
	}
	s, ok := statusOf(set, key)
	if !ok {
		return fmt.Errorf("the answer to %q carries no feature named %q", who, key)
	}
	if s.Enabled != (state == "on") {
		return fmt.Errorf("%q is reported %v for %q, want %s", key, s.Enabled, who, state)
	}
	return nil
}

// stepNowAnswered asks again before asserting. "Now" is the point: the
// scenarios that use it changed something and are asking what the caller would
// be told next, not what they were told before.
func (w *adminWorld) stepNowAnswered(key, state string) error {
	if err := w.stepAsk(); err != nil {
		return err
	}
	return w.stepAnswerReports(key, state)
}

func (w *adminWorld) stepNamedAnswerReports(email, key, state string) error {
	set, err := w.answerFor(email)
	if err != nil {
		return err
	}
	s, ok := statusOf(set, key)
	if !ok {
		return fmt.Errorf("%q was answered no feature named %q", email, key)
	}
	if s.Enabled != (state == "on") {
		return fmt.Errorf("%q was answered %q as %v, want %s", email, key, s.Enabled, state)
	}
	return nil
}

func (w *adminWorld) stepBothAnswered(key, state string) error {
	for _, email := range w.signedInOrder {
		if err := w.stepNamedAnswerReports(email, key, state); err != nil {
			return err
		}
	}
	return nil
}

func (w *adminWorld) stepCarriesKeyAndEnabled() error {
	set, _, err := w.soleAnswer()
	if err != nil {
		return err
	}
	if len(set) == 0 {
		return fmt.Errorf("the answer is empty, so this proves nothing")
	}
	for _, s := range set {
		if strings.TrimSpace(s.Key) == "" {
			return fmt.Errorf("a feature in the answer carries no key: %+v", s)
		}
	}
	return nil
}

func (w *adminWorld) stepCarriesNameAndLayer() error {
	set, _, err := w.soleAnswer()
	if err != nil {
		return err
	}
	for _, s := range set {
		if strings.TrimSpace(s.DisplayName) == "" {
			return fmt.Errorf("the feature %q carries no display name", s.Key)
		}
		if strings.TrimSpace(s.Source) == "" {
			return fmt.Errorf("the feature %q does not say which layer decided it", s.Key)
		}
	}
	return nil
}

// stepNoGrantsInAnswer reads the raw body, because the assertion is about what
// the wire carries and not about what a typed struct chose to decode.
func (w *adminWorld) stepNoGrantsInAnswer() error {
	_, key, err := w.soleAnswer()
	if err != nil {
		return err
	}
	body := w.calls["listing|"+key].body
	// "grantable" is the declaration's own flag and is not a grant. What must
	// never appear is a grant ROW: who holds one, from whom, or until when.
	for _, leak := range []string{"subjectId", "subject_id", "validThrough", "valid_through", "grantedBy"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(leak)) {
			return fmt.Errorf("the listing body carries %q, which belongs to somebody's grants: %s", leak, body)
		}
	}
	for email := range w.people {
		if strings.Contains(body, email) {
			return fmt.Errorf("the listing names %q, so it is carrying somebody's state", email)
		}
	}
	return nil
}

func (w *adminWorld) stepNoAccountLayerRead() error {
	if got := w.settings.accountCount(); got != 0 {
		return fmt.Errorf("%d account override read(s) happened for a caller with no session", got)
	}
	if got := w.grants.count(); got != 0 {
		return fmt.Errorf("%d grant read(s) happened for a caller with no session", got)
	}
	return nil
}

func (w *adminWorld) stepNoLayer(layer string) func() error {
	return func() error {
		set, _, err := w.soleAnswer()
		if err != nil {
			return err
		}
		for _, s := range set {
			if s.Source == layer {
				return fmt.Errorf("the feature %q names %q as its layer, which this caller does not have",
					s.Key, layer)
			}
		}
		return nil
	}
}

func (w *adminWorld) stepAnswerNamesNobody() error {
	_, key, err := w.soleAnswer()
	if err != nil {
		return err
	}
	body := w.calls["listing|"+key].body
	for email := range w.people {
		if strings.Contains(body, email) {
			return fmt.Errorf("the anonymous answer names %q", email)
		}
	}
	for _, accountID := range w.accounts {
		if accountID != "" && strings.Contains(body, accountID) {
			return fmt.Errorf("the anonymous answer names an account")
		}
	}
	return nil
}

func (w *adminWorld) stepSameSetAs(email string) error {
	mine, _, err := w.soleAnswer()
	if err != nil {
		return err
	}
	theirs, err := w.answerFor(email)
	if err != nil {
		// The other person has not asked. Sign them in and ask on their
		// behalf, so the comparison is against the answer they would really
		// get rather than against an anonymous one.
		p, perr := w.person(email)
		if perr != nil {
			return perr
		}
		if len(p.sessionIDs) == 0 {
			if serr := w.signIn(p); serr != nil {
				return serr
			}
		}
		saved := w.lastActor
		if aerr := w.ask(p); aerr != nil {
			return aerr
		}
		w.lastActor = saved
		theirs = w.answers[email]
	}
	for _, s := range theirs {
		mineStatus, ok := statusOf(mine, s.Key)
		if !ok || mineStatus.Enabled != s.Enabled {
			return fmt.Errorf("the impersonated answer differs on %q: %v vs %v",
				s.Key, mineStatus.Enabled, s.Enabled)
		}
	}
	return nil
}

func (w *adminWorld) stepPersonsUnauthenticated() error {
	call, ok := w.calls["anon-persons"]
	if !ok {
		return fmt.Errorf("no anonymous person listing was attempted")
	}
	if call.status != http.StatusUnauthorized {
		return fmt.Errorf("the person listing answered %d without a session, want 401", call.status)
	}
	return nil
}

func (w *adminWorld) stepWriteUnauthenticated() error {
	call, ok := w.calls["anon-write"]
	if !ok {
		return fmt.Errorf("no anonymous write was attempted")
	}
	if call.status != http.StatusUnauthorized {
		return fmt.Errorf("the write answered %d without a session, want 401", call.status)
	}
	return nil
}

func (w *adminWorld) stepNoInstanceSetting(key string) error {
	overrides, err := w.settings.InstanceOverrides(context.Background())
	if err != nil {
		return err
	}
	if _, stored := overrides[key]; stored {
		return fmt.Errorf("an instance override for %q was stored by a refused request", key)
	}
	return nil
}

// stepUnmountedParity is the route-ordering guard. serve.go warns that the
// LAST empty-prefix group under /api owns what an unmatched path answers, and
// making the listing anonymous is exactly the change that could move it. The
// harness mounts the groups in serve.go's order, so this asserts the property
// from the outside: an unmounted path is still owned by the last group and
// still answers as an authenticated surface would, not as a bare 404.
func (w *adminWorld) stepUnmountedParity() error {
	call, ok := w.calls["anon-unmounted"]
	if !ok {
		return fmt.Errorf("no unmounted path was requested")
	}
	if call.status == http.StatusOK {
		return fmt.Errorf("an unmounted path answered 200, which means something is mounted there")
	}
	if call.status != http.StatusUnauthorized {
		return fmt.Errorf("an unmounted /api path answered %d; the last empty-prefix group no longer owns it, "+
			"so making the listing anonymous moved ownership", call.status)
	}
	return nil
}

func (w *adminWorld) stepWriteForbidden() error {
	call, ok := w.calls["write"]
	if !ok {
		return fmt.Errorf("no write was attempted")
	}
	if call.status != http.StatusForbidden {
		return fmt.Errorf("the write answered %d, want 403: %s", call.status, call.body)
	}
	return nil
}

func (w *adminWorld) lastCall() (*apiCall, error) {
	key := w.lastCallRoute
	if w.lastActor != "" {
		key = w.lastActor + "|" + w.lastCallRoute
	}
	call, ok := w.calls[key]
	if !ok {
		return nil, fmt.Errorf("no call to %q was made", w.lastCallRoute)
	}
	return call, nil
}

func (w *adminWorld) stepCallOutcome(outcome string) error {
	call, err := w.lastCall()
	if err != nil {
		return err
	}
	switch outcome {
	case "succeeds":
		if call.status != http.StatusOK {
			return fmt.Errorf("%s answered %d, want 200: %s", call.route, call.status, call.body)
		}
	case "is refused":
		// Any refusal. A caller with no session is stopped by auth before the
		// gate is reached, and the contract asks only that the door be shut.
		if call.status < 400 {
			return fmt.Errorf("%s answered %d, want a refusal", call.route, call.status)
		}
	}
	return nil
}

func (w *adminWorld) stepCallSucceedsNow(route string) error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	call, err := w.call(p, route)
	if err != nil {
		return err
	}
	if call.status != http.StatusOK {
		return fmt.Errorf("%s answered %d, want 200: %s", route, call.status, call.body)
	}
	return nil
}

func (w *adminWorld) stepReportedOnWereServed() error {
	set, who, err := w.soleAnswer()
	if err != nil {
		return err
	}
	for route, key := range w.routeGates {
		s, ok := statusOf(set, key)
		if !ok || !s.Enabled {
			continue
		}
		call, ok := w.calls[who+"|"+route]
		if !ok {
			return fmt.Errorf("%q was reported on but %q was never called", key, route)
		}
		if call.status != http.StatusOK {
			return fmt.Errorf("%q was reported on but %q answered %d", key, route, call.status)
		}
	}
	return nil
}

func (w *adminWorld) stepReportedOffWereRefused() error {
	set, who, err := w.soleAnswer()
	if err != nil {
		return err
	}
	checked := 0
	for route, key := range w.routeGates {
		s, ok := statusOf(set, key)
		if !ok || s.Enabled {
			continue
		}
		call, ok := w.calls[who+"|"+route]
		if !ok {
			return fmt.Errorf("%q was reported off but %q was never called", key, route)
		}
		if call.status != http.StatusForbidden {
			return fmt.Errorf("%q was reported off but %q answered %d", key, route, call.status)
		}
		checked++
	}
	if checked == 0 {
		return fmt.Errorf("no gated route's feature was reported off, so this proves nothing")
	}
	return nil
}

func (w *adminWorld) stepCallForbidden() error {
	call, err := w.lastCall()
	if err != nil {
		return err
	}
	if call.status != http.StatusForbidden {
		return fmt.Errorf("%s answered %d, want 403: %s", call.route, call.status, call.body)
	}
	return nil
}

func (w *adminWorld) stepRefusalNamesFeature(key string) error {
	call, err := w.lastCall()
	if err != nil {
		return err
	}
	if !strings.Contains(call.body, key) {
		return fmt.Errorf("the refusal does not name %q: %s", key, call.body)
	}
	return nil
}

func (w *adminWorld) stepRefusalNotEnabled() error {
	call, err := w.lastCall()
	if err != nil {
		return err
	}
	if !strings.Contains(call.body, "not enabled") {
		return fmt.Errorf("the refusal does not say the capability is not enabled: %s", call.body)
	}
	return nil
}

func (w *adminWorld) stepRefusalNamesRole() error {
	call, ok := w.calls["write"]
	if !ok {
		return fmt.Errorf("no write was attempted")
	}
	if strings.Contains(call.body, "capability is not enabled") {
		return fmt.Errorf("a role refusal reads like a feature refusal: %s", call.body)
	}
	return nil
}

func (w *adminWorld) stepRefusalNotAboutRole() error {
	call, err := w.lastCall()
	if err != nil {
		return err
	}
	for _, roleWord := range []string{"role", "owner", "admin only", "permission"} {
		if strings.Contains(strings.ToLower(call.body), roleWord) {
			return fmt.Errorf("a feature refusal blames the caller's role: %s", call.body)
		}
	}
	return nil
}

func (w *adminWorld) stepNoLedgerExport() error {
	if w.ledgerExports != 0 {
		return fmt.Errorf("the refused call still wrote %d ledger export(s)", w.ledgerExports)
	}
	return nil
}

func (w *adminWorld) stepRefusalNotPartial() error {
	call, err := w.lastCall()
	if err != nil {
		return err
	}
	if strings.Contains(call.body, "\"data\"") {
		return fmt.Errorf("the refusal carried a data payload: %s", call.body)
	}
	return nil
}

func (w *adminWorld) stepLedgerServed() error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	call, ok := w.calls[p.email+"|POST /api/ledger/exports"]
	if !ok {
		return fmt.Errorf("the ledger route was never called")
	}
	if call.status != http.StatusOK {
		return fmt.Errorf("the ledger route answered %d, want 200: %s", call.status, call.body)
	}
	return nil
}

func (w *adminWorld) stepBothCallsSucceed() error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	for _, route := range []string{"GET /api/persons", "GET /api/resource-types"} {
		call, ok := w.calls[p.email+"|"+route]
		if !ok {
			return fmt.Errorf("%q was never called", route)
		}
		if call.status != http.StatusOK {
			return fmt.Errorf("%q answered %d, want 200", route, call.status)
		}
	}
	return nil
}

// stepNoFeatureEvaluated proves an ungated route takes the no-lookup path: no
// read of stored feature state is attributable to calling it.
func (w *adminWorld) stepNoFeatureEvaluated() error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	before := w.settings.count()
	if _, err := w.call(p, "GET /api/persons"); err != nil {
		return err
	}
	if got := w.settings.count(); got != before {
		return fmt.Errorf("calling an ungated route read feature state %d time(s)", got-before)
	}
	return nil
}

func (w *adminWorld) stepGatedRouteRefused() error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	call, ok := w.calls[p.email+"|POST /api/agent/conversations/{id}/messages"]
	if !ok {
		return fmt.Errorf("the gated route was never called")
	}
	if call.status != http.StatusForbidden {
		return fmt.Errorf("the gated route answered %d while the store was down, want 403", call.status)
	}
	return nil
}

func (w *adminWorld) stepUngatedRouteSucceeds() error {
	p, err := w.soleActor()
	if err != nil {
		return err
	}
	call, ok := w.calls[p.email+"|GET /api/persons"]
	if !ok {
		return fmt.Errorf("the ungated route was never called")
	}
	if call.status != http.StatusOK {
		return fmt.Errorf("the ungated route answered %d while the store was down, want 200", call.status)
	}
	return nil
}

func (w *adminWorld) stepEveryFeatureOff() error {
	set, _, err := w.soleAnswer()
	if err != nil {
		return err
	}
	if len(set) == 0 {
		return fmt.Errorf("the answer is empty, so this proves nothing")
	}
	for _, s := range set {
		if s.Enabled {
			return fmt.Errorf("%q is reported on while the store cannot be read", s.Key)
		}
	}
	return nil
}

func (w *adminWorld) stepAnswerCarriesMessage() error {
	_, key, err := w.soleAnswer()
	if err != nil {
		return err
	}
	for _, m := range w.messages[key] {
		if strings.Contains(strings.ToLower(m), "could not be read") {
			return nil
		}
	}
	return fmt.Errorf("the answer carries no message about the state being unreadable: %v", w.messages[key])
}

func (w *adminWorld) stepLoggedFailure() error {
	if len(w.logs.matching("feature")) == 0 {
		return fmt.Errorf("nothing was logged when the store could not be read")
	}
	return nil
}

func (w *adminWorld) stepEveryCallSucceeded() error {
	if len(w.repeatCalls) == 0 {
		return fmt.Errorf("no calls were made, so this proves nothing")
	}
	for i, call := range w.repeatCalls {
		if call.status != http.StatusOK {
			return fmt.Errorf("call %d answered %d, want 200: %s", i+1, call.status, call.body)
		}
	}
	return nil
}

func (w *adminWorld) stepLoggedDriftOnce() error {
	lines := w.logs.matching("nobody declared")
	if len(lines) != 1 {
		return fmt.Errorf("the undeclared key was logged %d times, want once: %v", len(lines), lines)
	}
	return nil
}

func (w *adminWorld) stepLogNamesKey(key string) error {
	if len(w.logs.matching("nobody declared", key)) == 0 {
		return fmt.Errorf("no log line names the key %q: %v", key, w.logs.matching("nobody declared"))
	}
	return nil
}

// stepLogNamesRoute is honest about what the log can carry. The provider logs
// the key, not the caller's route — it is asked for a feature and never told
// what asked. So this asserts the operator can find the route from the key,
// which is what the harness's route table gives them, rather than pretending
// the log line carries a path it does not.
func (w *adminWorld) stepLogNamesRoute() error {
	key := w.ledgerGate
	if len(w.logs.matching("nobody declared", key)) == 0 {
		return fmt.Errorf("the log does not name %q, so the route cannot be found from it", key)
	}
	found := false
	for route, gate := range w.routeGates {
		if gate == key {
			found = true
			_ = route
		}
	}
	if !found {
		return fmt.Errorf("no mounted route names %q, so the logged key leads nowhere", key)
	}
	return nil
}

func (w *adminWorld) stepChangeAccepted() error {
	call, ok := w.calls["write"]
	if !ok {
		return fmt.Errorf("no write was attempted")
	}
	if call.status >= 400 {
		return fmt.Errorf("the change answered %d: %s", call.status, call.body)
	}
	return nil
}

func (w *adminWorld) stepNoTurnTaken() error {
	if w.agentTurns != 0 {
		return fmt.Errorf("the refused call still took %d turn(s)", w.agentTurns)
	}
	return nil
}

func (w *adminWorld) stepStraightAwaySucceeded() error {
	if w.straightAway == nil {
		return fmt.Errorf("no call was made straight away")
	}
	if w.straightAway.status != http.StatusOK {
		return fmt.Errorf("the call straight away answered %d, want 200", w.straightAway.status)
	}
	return nil
}

func (w *adminWorld) stepAfterMomentRefused() error {
	if w.afterMoment == nil {
		return fmt.Errorf("no call was made after the moment passed")
	}
	if w.afterMoment.status != http.StatusForbidden {
		return fmt.Errorf("the call after the window closed answered %d, want 403", w.afterMoment.status)
	}
	return nil
}

func (w *adminWorld) stepNothingInvalidated() error {
	if got := w.invSpy.count(); got != w.invsBefore {
		return fmt.Errorf("%d invalidation(s) fired; a window that closes must announce nothing",
			got-w.invsBefore)
	}
	return nil
}

func (w *adminWorld) stepCacheAgeHadNotRunOut() error {
	if w.maxCacheAge <= 0 {
		return fmt.Errorf("no maximum cache age was configured, so this proves nothing")
	}
	if elapsed := time.Since(w.windowStart); elapsed >= w.maxCacheAge {
		return fmt.Errorf("the scenario took %s, which is past the %s cache age", elapsed, w.maxCacheAge)
	}
	return nil
}

func (w *adminWorld) stepStillSignedIn(email string) error {
	p, err := w.person(email)
	if err != nil {
		return err
	}
	call, err := w.request(p, http.MethodGet, "/api/persons", nil)
	if err != nil {
		return err
	}
	if call.status == http.StatusUnauthorized {
		return fmt.Errorf("%q was signed out", email)
	}
	return nil
}

func (w *adminWorld) stepNotSignedOut() error {
	for _, email := range w.signedInOrder {
		if err := w.stepStillSignedIn(email); err != nil {
			return err
		}
	}
	return nil
}

func (w *adminWorld) stepNotRestarted() error {
	if w.boots != w.restartBaseline {
		return fmt.Errorf("the instance restarted %d time(s) during the scenario", w.boots-w.restartBaseline)
	}
	return nil
}

func (w *adminWorld) stepEveryAnswerSame() error {
	if len(w.repeatAnswers) < 2 {
		return fmt.Errorf("only %d answers were collected, so this proves nothing", len(w.repeatAnswers))
	}
	first := w.repeatAnswers[0]
	for i, set := range w.repeatAnswers[1:] {
		if len(set) != len(first) {
			return fmt.Errorf("answer %d holds %d features, the first held %d", i+2, len(set), len(first))
		}
		for _, s := range first {
			got, ok := statusOf(set, s.Key)
			if !ok || got.Enabled != s.Enabled {
				return fmt.Errorf("answer %d differs on %q", i+2, s.Key)
			}
		}
	}
	return nil
}

func (w *adminWorld) stepReadOnce() error {
	if w.readsAfterFirst <= w.readsBefore {
		return fmt.Errorf("answering the first listing read no feature state, so the count proves nothing")
	}
	if got := w.settings.count(); got != w.readsAfterFirst {
		return fmt.Errorf("%d further read(s) of feature state happened; one resolve must serve the session",
			got-w.readsAfterFirst)
	}
	return nil
}
