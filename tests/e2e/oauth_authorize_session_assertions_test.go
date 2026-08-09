package e2e

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/cucumber/godog"
)

// The Then half of story #140's contract. Split from the driving steps so the
// file that answers "what did the instance do" stays readable next to the one
// that answers "what did we ask it".
func registerOAuthAssertions(sc *godog.ScenarioContext, w *oauthWorld) {
	// --- discovery and registration ---
	sc.Step(`^the instance names itself as the authorization server$`, w.namesItselfAsAuthorizationServer)
	sc.Step(`^it offers to register connectors on the spot$`, func() error { return w.offersRegistration(true) })
	sc.Step(`^the instance offers no way to register connectors on the spot$`, func() error { return w.offersRegistration(false) })
	sc.Step(`^it requires the S256 proof-key method$`, w.requiresS256)
	sc.Step(`^the registration succeeds$`, w.registrationSucceeded)
	sc.Step(`^the registration is refused$`, w.registrationRefused)
	sc.Step(`^the connector is issued its own client identifier$`, w.issuedAClientID)
	sc.Step(`^the redirect URI it registered is the one the instance will send people back to$`, w.registeredRedirectURIEchoed)

	// --- where the person ended up ---
	sc.Step(`^the person is sent back to Claude with an authorization code$`, w.sentBackWithCode)
	sc.Step(`^the state Claude sent is returned with it$`, func() error { return w.stateReturned() })
	sc.Step(`^the state Claude sent is returned with the error$`, func() error { return w.stateReturned() })
	sc.Step(`^the person is never sent to Google$`, w.neverSentToGoogle)
	sc.Step(`^the person is not sent to Google$`, w.neverSentToGoogle)
	sc.Step(`^the person ends up at Google's sign-in$`, w.endsUpAtGoogle)
	sc.Step(`^the person is sent to a sign-in page on this instance$`, w.sentToOwnSignIn)
	sc.Step(`^the person is not sent to a sign-in page$`, w.notSentToSignIn)
	sc.Step(`^the person is not asked to sign in a second time$`, w.notAskedToSignInAgain)
	sc.Step(`^the person is not sent to the unregistered redirect URI$`, w.notSentToUnregisteredURI)
	sc.Step(`^the instance answers the request itself rather than redirecting anywhere$`, w.answeredInPlace)
	sc.Step(`^it reports that the client is not one it knows$`, w.reportsUnknownClient)
	sc.Step(`^Claude is sent the error "([^"]*)" instead of an authorization code$`, w.sentTheError)

	// --- codes and tokens ---
	sc.Step(`^Claude is not sent an authorization code$`, w.noCodeIssued)
	sc.Step(`^Claude is not sent an authorization code yet$`, w.noCodeIssued)
	sc.Step(`^no authorization code is issued$`, w.noCodeIssued)
	sc.Step(`^Claude receives an access token$`, w.receivedAccessToken)
	sc.Step(`^the token is issued for the scope Claude asked for$`, w.tokenCarriesScope)
	sc.Step(`^the access token acts as "([^"]*)"$`, w.tokenActsAs)
	sc.Step(`^the second exchange is refused$`, w.secondExchangeRefused)
	sc.Step(`^no second access token is issued$`, w.noSecondToken)
	sc.Step(`^the exchange is refused$`, w.lastExchangeRefused)
	sc.Step(`^no access token is issued$`, w.noTokenAtAll)
	sc.Step(`^nothing Claude can present afterwards acts as "([^"]*)"$`, w.nothingActsAs)
	sc.Step(`^the instance does not report a failure of its own$`, w.noServerError)

	// --- the connector in use ---
	sc.Step(`^the instance lists the tools it offers$`, w.listedTools)
	sc.Step(`^the task "([^"]*)" is one of the demo account's tasks$`, w.taskExists)
	sc.Step(`^the second person's Claude sees the task "([^"]*)"$`, w.secondPersonSeesTask)
	sc.Step(`^both connectors act as "([^"]*)"$`, w.bothConnectorsActAs)
}

// --- discovery and registration ---

func (w *oauthWorld) namesItselfAsAuthorizationServer() error {
	endpoint, _ := w.discovery["authorization_endpoint"].(string)
	if endpoint == "" || !strings.Contains(endpoint, "/oauth/authorize") {
		return fmt.Errorf("the instance did not name its own authorization endpoint: %s", w.last.body)
	}
	return nil
}

func (w *oauthWorld) offersRegistration(expected bool) error {
	_, present := w.discovery["registration_endpoint"]
	if present != expected {
		return fmt.Errorf("expected registration_endpoint present=%v, got %v: %s",
			expected, present, w.last.body)
	}
	return nil
}

func (w *oauthWorld) requiresS256() error {
	methods, _ := w.discovery["code_challenge_methods_supported"].([]any)
	for _, m := range methods {
		if s, ok := m.(string); ok && s == "S256" {
			return nil
		}
	}
	return fmt.Errorf("S256 is not among the advertised proof-key methods: %s", w.last.body)
}

func (w *oauthWorld) registrationSucceeded() error {
	if w.registered.status != http.StatusOK && w.registered.status != http.StatusCreated {
		return fmt.Errorf("registration failed: %d %s", w.registered.status, w.registered.body)
	}
	return nil
}

func (w *oauthWorld) registrationRefused() error {
	if w.registered.status == http.StatusOK || w.registered.status == http.StatusCreated {
		return fmt.Errorf("expected the registration to be refused, it succeeded: %s", w.registered.body)
	}
	return nil
}

func (w *oauthWorld) issuedAClientID() error {
	if w.clientID == "" {
		return fmt.Errorf("no client identifier was issued: %s", w.registered.body)
	}
	return nil
}

func (w *oauthWorld) registeredRedirectURIEchoed() error {
	if !strings.Contains(w.registered.body, claudeRedirectURI) {
		return fmt.Errorf("the registration did not echo the redirect URI: %s", w.registered.body)
	}
	return nil
}

// --- where the person ended up ---

func (w *oauthWorld) sentBackWithCode() error {
	to := w.last.redirectedTo()
	if to == nil {
		return fmt.Errorf("the person was not redirected: %d %s", w.last.status, w.last.body)
	}
	if !strings.HasPrefix(w.last.location, claudeRedirectURI) {
		return fmt.Errorf("the person was sent to %q rather than back to the connector", w.last.location)
	}
	if to.Query().Get("code") == "" {
		return fmt.Errorf("no authorization code came back: %s", w.last.location)
	}
	return nil
}

func (w *oauthWorld) stateReturned() error {
	to := w.last.redirectedTo()
	if to == nil {
		return fmt.Errorf("the person was not redirected: %d %s", w.last.status, w.last.body)
	}
	if got := to.Query().Get("state"); got != "st-140" {
		return fmt.Errorf("expected the state to come back as %q, got %q", "st-140", got)
	}
	return nil
}

func (w *oauthWorld) neverSentToGoogle() error {
	if strings.Contains(w.last.location, "accounts.google.com") {
		return fmt.Errorf("the person was sent to Google: %s", w.last.location)
	}
	return nil
}

func (w *oauthWorld) endsUpAtGoogle() error {
	if !strings.Contains(w.last.location, "accounts.google.com") {
		return fmt.Errorf("expected a hand-off to Google, got: %d %s", w.last.status, w.last.location)
	}
	return nil
}

// sentToOwnSignIn requires a same-origin destination, which is what "on this
// instance" means: a redirect to somebody else's sign-in would satisfy a looser
// check and be precisely the bug.
func (w *oauthWorld) sentToOwnSignIn() error {
	to := w.last.redirectedTo()
	if to == nil {
		return fmt.Errorf("the person was not redirected anywhere: %d %s", w.last.status, w.last.body)
	}
	if to.IsAbs() && !strings.HasPrefix(w.last.location, w.server.URL) {
		return fmt.Errorf("the person was sent off this instance: %s", w.last.location)
	}
	if to.Path != "/" {
		return fmt.Errorf("expected the instance's sign-in, got %q", w.last.location)
	}
	if to.Query().Get("return_to") == "" {
		return fmt.Errorf("the sign-in was given no way back: %s", w.last.location)
	}
	return nil
}

func (w *oauthWorld) notSentToSignIn() error {
	to := w.last.redirectedTo()
	if to != nil && to.Path == "/" {
		return fmt.Errorf("the person was sent to sign in: %s", w.last.location)
	}
	return nil
}

func (w *oauthWorld) notAskedToSignInAgain() error {
	to := w.last.redirectedTo()
	if to != nil && to.Path == "/" && to.Query().Get("return_to") != "" {
		return fmt.Errorf("the person was sent back to sign in a second time: %s", w.last.location)
	}
	return nil
}

func (w *oauthWorld) notSentToUnregisteredURI() error {
	if strings.Contains(w.last.location, "example.invalid") {
		return fmt.Errorf("the person was sent to the unregistered redirect URI: %s", w.last.location)
	}
	return nil
}

// answeredInPlace is the RFC 6749 §4.1.2.1 rule: until the redirect URI is
// known to belong to the client, nothing may be redirected to it.
func (w *oauthWorld) answeredInPlace() error {
	if w.last.location != "" {
		return fmt.Errorf("expected an answer in place, the person was redirected to %q", w.last.location)
	}
	if w.last.status < 400 || w.last.status >= 500 {
		return fmt.Errorf("expected a client error answered in place, got %d: %s", w.last.status, w.last.body)
	}
	return nil
}

func (w *oauthWorld) reportsUnknownClient() error {
	if !strings.Contains(w.last.body, "invalid_client") {
		return fmt.Errorf("expected the instance to report an unknown client, it said: %s", w.last.body)
	}
	return nil
}

func (w *oauthWorld) sentTheError(errCode string) error {
	to := w.last.redirectedTo()
	if to == nil {
		return fmt.Errorf("expected the error to go back to the connector, got %d %s", w.last.status, w.last.body)
	}
	if got := to.Query().Get("error"); got != errCode {
		return fmt.Errorf("expected error %q, got %q (%s)", errCode, got, w.last.location)
	}
	if to.Query().Get("code") != "" {
		return fmt.Errorf("an authorization code came back alongside the error: %s", w.last.location)
	}
	return nil
}

// --- codes and tokens ---

func (w *oauthWorld) noCodeIssued() error {
	if w.code != "" {
		return fmt.Errorf("an authorization code was issued: %s", w.code)
	}
	if to := w.last.redirectedTo(); to != nil && to.Query().Get("code") != "" {
		return fmt.Errorf("an authorization code came back: %s", w.last.location)
	}
	return nil
}

func (w *oauthWorld) receivedAccessToken() error {
	if w.accessToken() == "" {
		return fmt.Errorf("no access token was issued: %v", w.tokens)
	}
	return nil
}

func (w *oauthWorld) tokenCarriesScope() error {
	for i := len(w.tokens) - 1; i >= 0; i-- {
		if w.tokenStatus[i] != http.StatusOK {
			continue
		}
		scope, _ := w.tokens[i]["scope"].(string)
		if scope == "" {
			return fmt.Errorf("the token carried no scope: %v", w.tokens[i])
		}
		for _, want := range []string{"mcp:read", "mcp:write"} {
			if !strings.Contains(scope, want) {
				return fmt.Errorf("expected the token's scope to include %q, got %q", want, scope)
			}
		}
		return nil
	}
	return fmt.Errorf("no successful token exchange to read a scope from")
}

// tokenActsAs proves the identity by using the token, not by reading it: a
// token that decodes to the right agent but is refused by the API would satisfy
// an inspection and fail the person.
func (w *oauthWorld) tokenActsAs(email string) error {
	token := w.accessToken()
	if token == "" {
		return fmt.Errorf("no access token to check")
	}
	agentID, err := w.agentIDFor(email)
	if err != nil {
		return err
	}
	if err := w.initializeMCP(token); err != nil {
		return fmt.Errorf("the token was refused by the instance: %w", err)
	}
	if err := w.createTaskWith(token, "identity probe for "+email); err != nil {
		return err
	}
	owner, err := w.lastTaskOwner("identity probe for " + email)
	if err != nil {
		return err
	}
	if owner != agentID {
		return fmt.Errorf("the token acted as %q rather than %q (%s)", owner, agentID, email)
	}
	return nil
}

func (w *oauthWorld) secondExchangeRefused() error {
	if len(w.tokenStatus) < 2 {
		return fmt.Errorf("expected two exchanges, saw %d", len(w.tokenStatus))
	}
	if w.tokenStatus[len(w.tokenStatus)-1] == http.StatusOK {
		return fmt.Errorf("the second exchange succeeded: %v", w.tokens[len(w.tokens)-1])
	}
	return nil
}

func (w *oauthWorld) noSecondToken() error {
	issued := 0
	for _, s := range w.tokenStatus {
		if s == http.StatusOK {
			issued++
		}
	}
	if issued > 1 {
		return fmt.Errorf("expected at most one access token, %d were issued", issued)
	}
	return nil
}

func (w *oauthWorld) lastExchangeRefused() error {
	if len(w.tokenStatus) == 0 {
		return fmt.Errorf("no exchange was attempted")
	}
	if w.tokenStatus[len(w.tokenStatus)-1] == http.StatusOK {
		return fmt.Errorf("the exchange succeeded: %v", w.tokens[len(w.tokens)-1])
	}
	return nil
}

func (w *oauthWorld) noTokenAtAll() error {
	if w.accessToken() != "" {
		return fmt.Errorf("an access token was issued: %v", w.tokens)
	}
	return nil
}

// nothingActsAs is the allowlist assertion reachable from here: whatever the
// instance did with the request, no token exists that acts as this person.
func (w *oauthWorld) nothingActsAs(email string) error {
	token := w.accessToken()
	if token == "" {
		return nil
	}
	agentID, err := w.agentIDFor(email)
	if err != nil {
		return err
	}
	if err := w.initializeMCP(token); err != nil {
		// The token is refused outright, which is a stronger form of the same
		// guarantee.
		return nil
	}
	probe := "allowlist probe for " + email
	if err := w.createTaskWith(token, probe); err != nil {
		return nil
	}
	owner, err := w.lastTaskOwner(probe)
	if err != nil {
		return err
	}
	if owner == agentID {
		return fmt.Errorf("a token acting as %q was issued despite the allowlist", email)
	}
	return nil
}

func (w *oauthWorld) noServerError() error {
	if w.last.status >= 500 {
		return fmt.Errorf("the instance reported a failure of its own: %d %s", w.last.status, w.last.body)
	}
	if to := w.last.redirectedTo(); to != nil && to.Query().Get("error") == "server_error" {
		return fmt.Errorf("the instance reported server_error: %s", w.last.location)
	}
	return nil
}

// --- the connector in use ---

func (w *oauthWorld) listedTools() error {
	if !strings.Contains(w.last.body, "resource_create") {
		return fmt.Errorf("the instance listed no recognizable tools: %s", truncate(w.last.body))
	}
	return nil
}

func (w *oauthWorld) taskExists(name string) error {
	owner, err := w.lastTaskOwner(name)
	if err != nil {
		return err
	}
	if owner == "" {
		return fmt.Errorf("the task %q is not in the store", name)
	}
	return nil
}

func (w *oauthWorld) secondPersonSeesTask(name string) error {
	if w.secondToken == "" {
		return fmt.Errorf("the second person has no token")
	}
	if err := w.initializeMCP(w.secondToken); err != nil {
		return fmt.Errorf("the second connector's token was refused: %w", err)
	}
	reply, err := w.mcpCall(w.secondToken, "tools/call", map[string]any{
		"name":      "resource_list",
		"arguments": map[string]any{"type_slug": "task"},
	})
	if err != nil {
		return err
	}
	if !strings.Contains(fmt.Sprintf("%v", reply), name) {
		return fmt.Errorf("the second connector does not see %q", name)
	}
	return nil
}

func (w *oauthWorld) bothConnectorsActAs(email string) error {
	agentID, err := w.agentIDFor(email)
	if err != nil {
		return err
	}
	for label, token := range map[string]string{"the first": w.accessToken(), "the second": w.secondToken} {
		if token == "" {
			return fmt.Errorf("%s connector has no token", label)
		}
		probe := fmt.Sprintf("shared identity probe %s", label)
		if err := w.initializeMCP(token); err != nil {
			return fmt.Errorf("%s connector's token was refused: %w", label, err)
		}
		if err := w.createTaskWith(token, probe); err != nil {
			return err
		}
		owner, err := w.lastTaskOwner(probe)
		if err != nil {
			return err
		}
		if owner != agentID {
			return fmt.Errorf("%s connector acted as %q rather than %q", label, owner, agentID)
		}
	}
	return nil
}

func truncate(s string) string {
	if len(s) <= 300 {
		return s
	}
	return s[:300] + "…"
}
