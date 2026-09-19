package application

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// fakeRevoker stands in for the refresh token store: it records whose tokens
// were revoked, and how often.
type fakeRevoker struct {
	mu      sync.Mutex
	calls   map[string]int
	tokens  map[string]int64
	failFor string
	order   *[]string
}

func newFakeRevoker() *fakeRevoker {
	return &fakeRevoker{calls: map[string]int{}, tokens: map[string]int64{}}
}

func (f *fakeRevoker) RevokeAllForAgent(_ context.Context, agentID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.order != nil {
		*f.order = append(*f.order, "tokens:"+agentID)
	}
	f.calls[agentID]++
	if f.failFor == agentID {
		return 0, errors.New("the database is locked")
	}
	return f.tokens[agentID], nil
}

func (f *fakeRevoker) reached(agentID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[agentID]
}

func (f *fakeRevoker) people() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// fakeSessions stands in for the browser session store: it records whose
// sessions were ended, and in what order against the token store.
type fakeSessions struct {
	mu      sync.Mutex
	ended   map[string]int
	failFor string
	order   *[]string
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{ended: map[string]int{}}
}

func (f *fakeSessions) RevokeAllSessions(_ context.Context, agentID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.order != nil {
		*f.order = append(*f.order, "sessions:"+agentID)
	}
	if f.failFor == agentID {
		return errors.New("the session store is locked")
	}
	f.ended[agentID]++
	return nil
}

func (f *fakeSessions) endedFor(agentID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ended[agentID]
}

// fakeCodes stands in for the authorization code store.
type fakeCodes struct {
	mu      sync.Mutex
	voided  map[string]int64
	calls   map[string]int
	failFor string
	order   *[]string
}

func newFakeCodes() *fakeCodes {
	return &fakeCodes{voided: map[string]int64{}, calls: map[string]int{}}
}

func (f *fakeCodes) VoidUnredeemedForAgent(_ context.Context, agentID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.order != nil {
		*f.order = append(*f.order, "codes:"+agentID)
	}
	f.calls[agentID]++
	if f.failFor == agentID {
		return 0, errors.New("the code store is locked")
	}
	voided := f.voided[agentID]
	f.voided[agentID] = 0
	return voided, nil
}

// revocation is every part of a revocation, so a test can reach the fake it
// wants to fail or count.
type revocation struct {
	service  *AssertedTokenRevocation
	tokens   *fakeRevoker
	sessions *fakeSessions
	codes    *fakeCodes
	order    []string
}

// newRevocation wires a whole revocation — sessions, codes and tokens — the
// way serve wires one, and records the order the three stores are touched in.
func newRevocation(store *memoryAuthStore, logs *signInLogs) *revocation {
	r := &revocation{tokens: newFakeRevoker(), sessions: newFakeSessions(), codes: newFakeCodes()}
	r.sessions.order = &r.order
	r.codes.order = &r.order
	r.tokens.order = &r.order
	r.service = NewAssertedTokenRevocation(AssertedTokenRevocationConfig{
		Credentials: storeCredentials{s: store},
		Sessions:    r.sessions,
		Codes:       r.codes,
		Tokens:      r.tokens,
		Logger:      logs,
	})
	return r
}

func newRevocationFor(store *memoryAuthStore, tokens RefreshTokenRevoker, logs *signInLogs) *AssertedTokenRevocation {
	return NewAssertedTokenRevocation(AssertedTokenRevocationConfig{
		Credentials: storeCredentials{s: store},
		Sessions:    newFakeSessions(),
		Codes:       newFakeCodes(),
		Tokens:      tokens,
		Logger:      logs,
	})
}

// The door resets the password of a person this instance already knows by the
// identity the door signs them in with. Their tokens — every one — stop
// renewing, and nobody else's are touched.
func TestRevokeEndsTheTokensOfThePersonHoldingTheAssertedIdentity(t *testing.T) {
	store := newMemoryAuthStore()
	store.seedPerson(t, "agent-dana", "Dana Whitfield", OAuthProviderDoor, "door-1", "dana@harborlegal.example")
	store.seedPerson(t, "agent-morgan", "Morgan Reyes", OAuthProviderDoor, "door-2", "morgan@harborlegal.example")
	tokens := newFakeRevoker()
	tokens.tokens["agent-dana"] = 4
	logs := &signInLogs{}

	result, err := newRevocationFor(store, tokens, logs).Revoke(context.Background(), AssertedIdentity{
		Provider: OAuthProviderDoor, Subject: "door-1", Email: "dana@harborlegal.example",
	})

	if err != nil {
		t.Fatalf("the revocation failed: %v", err)
	}
	if result.Tokens != 4 || len(result.People) != 1 || result.People[0] != "agent-dana" {
		t.Fatalf("revoked %+v, want the 4 tokens of agent-dana alone", result)
	}
	if tokens.reached("agent-morgan") != 0 {
		t.Fatalf("another person's tokens were revoked")
	}
	requireNoRawIdentity(t, logs, "door-1", "dana@harborlegal.example")
}

// A door identity this instance has never seen reaches nobody, even when a
// person here holds a credential that proves the asserted email. The old
// password never signed anybody in here — every door sign-in leaves a door
// credential — so there is nothing of the reset's to end, and resolving by the
// email would let the issuer end the tokens of a person it never signed in.
func TestRevokeDoesNotReachAPersonByTheAssertedEmail(t *testing.T) {
	store := newMemoryAuthStore()
	store.seedPerson(t, "agent-dana", "Dana Whitfield", OAuthProviderGoogle, "google-108", "dana.whitfield@harborlegal.example")
	store.seedPerson(t, "agent-morgan", "Morgan Reyes", OAuthProviderApple, "apple-42", "dana.whitfield@harborlegal.example")
	tokens := newFakeRevoker()
	tokens.tokens["agent-dana"] = 2
	logs := &signInLogs{}

	result, err := newRevocationFor(store, tokens, logs).Revoke(context.Background(), AssertedIdentity{
		Provider: OAuthProviderDoor, Subject: "door-1", Email: "Dana.Whitfield@HarborLegal.example",
	})

	if err != nil {
		t.Fatalf("an identity nobody holds is not an error: %v", err)
	}
	if result.Tokens != 0 || len(result.People) != 0 || tokens.people() != 0 {
		t.Fatalf("revoked %+v for an identity nobody here holds", result)
	}
	if logs.text() == "" {
		t.Fatal("a revocation that reached nobody was not recorded")
	}
	// Nothing is created and nothing is linked: the revocation is not a
	// sign-in, and an identity it has never seen must not become a credential.
	if cred := store.credentialFor(OAuthProviderDoor, "door-1"); cred != nil {
		t.Fatalf("the revocation linked the asserted identity to %s", cred.AgentID())
	}
	if store.createCount() != 0 {
		t.Fatalf("the revocation created %d people", store.createCount())
	}
}

// The person holding the door identity may also hold a Google credential with
// the same email (the door and Google identities are one person). Revoking by
// the door identity ends every token of that one person, whichever sign-in
// issued it.
func TestRevokeEndsEveryTokenOfThePersonWhateverSignInIssuedIt(t *testing.T) {
	store := newMemoryAuthStore()
	store.seedPerson(t, "agent-dana", "Dana Whitfield", OAuthProviderDoor, "door-1", "dana@harborlegal.example")
	store.seedPerson(t, "agent-morgan", "Morgan Reyes", OAuthProviderGoogle, "google-7", "morgan@harborlegal.example")
	tokens := newFakeRevoker()
	tokens.tokens["agent-dana"] = 5

	result, err := newRevocationFor(store, tokens, &signInLogs{}).Revoke(context.Background(), AssertedIdentity{
		// The email the issuer names does not widen the reach.
		Provider: OAuthProviderDoor, Subject: "door-1", Email: "morgan@harborlegal.example",
	})

	if err != nil {
		t.Fatalf("the revocation failed: %v", err)
	}
	if result.Tokens != 5 || len(result.People) != 1 || result.People[0] != "agent-dana" {
		t.Fatalf("revoked %+v, want the 5 tokens of agent-dana alone", result)
	}
	if tokens.reached("agent-morgan") != 0 {
		t.Fatal("the asserted email reached a person the identity does not name")
	}
}

// A store that could not be written is reported, so the route answers the door
// a failure it will retry rather than a success. A revocation that quietly did
// not finish leaves an intruder renewing.
func TestRevokeReportsAStoreThatCouldNotBeWritten(t *testing.T) {
	store := newMemoryAuthStore()
	store.seedPerson(t, "agent-dana", "Dana Whitfield", OAuthProviderDoor, "door-1", "dana@harborlegal.example")
	tokens := newFakeRevoker()
	tokens.failFor = "agent-dana"
	logs := &signInLogs{}

	_, err := newRevocationFor(store, tokens, logs).Revoke(context.Background(), AssertedIdentity{
		Provider: OAuthProviderDoor, Subject: "door-1", Email: "dana@harborlegal.example",
	})

	if err == nil {
		t.Fatal("a store that could not be written was reported as a revocation")
	}
	var errored bool
	for _, line := range logs.all() {
		if line.level == "error" {
			errored = true
		}
	}
	if !errored {
		t.Fatalf("nothing was logged at error level:\n%s", logs.text())
	}
}

// The door repeats a call whose answer it could not read.
func TestRevokeIsIdempotent(t *testing.T) {
	store := newMemoryAuthStore()
	store.seedPerson(t, "agent-dana", "Dana Whitfield", OAuthProviderDoor, "door-1", "dana@harborlegal.example")
	tokens := newFakeRevoker()
	tokens.tokens["agent-dana"] = 3
	revocation := newRevocationFor(store, tokens, &signInLogs{})
	id := AssertedIdentity{Provider: OAuthProviderDoor, Subject: "door-1", Email: "dana@harborlegal.example"}

	first, err := revocation.Revoke(context.Background(), id)
	if err != nil {
		t.Fatalf("the first revocation failed: %v", err)
	}
	tokens.tokens["agent-dana"] = 0
	second, err := revocation.Revoke(context.Background(), id)
	if err != nil {
		t.Fatalf("the second revocation failed: %v", err)
	}
	if len(first.People) != 1 || len(second.People) != 1 {
		t.Fatalf("the two calls reached %d and %d people, want the same one", len(first.People), len(second.People))
	}
	if second.Tokens != 0 {
		t.Fatalf("the second call revoked %d tokens, want none left", second.Tokens)
	}
}

// wm-4ke17. A revocation that ended the tokens and left the browser session
// alive would be undone by one request: the cookie mints a fresh authorization
// code with no re-authentication, and that code buys a new 30-day refresh
// token. So the session goes first, then the codes already handed out, then
// the tokens — and all three, for the person the identity names.
func TestRevokeEndsTheSessionAndTheCodesBeforeItEndsTheTokens(t *testing.T) {
	store := newMemoryAuthStore()
	store.seedPerson(t, "agent-dana", "Dana Whitfield", OAuthProviderDoor, "door-1", "dana@harborlegal.example")
	logs := &signInLogs{}
	r := newRevocation(store, logs)
	r.tokens.tokens["agent-dana"] = 4
	r.codes.voided["agent-dana"] = 2

	result, err := r.service.Revoke(context.Background(), AssertedIdentity{
		Provider: OAuthProviderDoor, Subject: "door-1", Email: "dana@harborlegal.example",
	})

	if err != nil {
		t.Fatalf("the revocation failed: %v", err)
	}
	if r.sessions.endedFor("agent-dana") != 1 {
		t.Fatal("the person's browser sessions were left alive, so a cookie can mint new token access")
	}
	if result.Tokens != 4 || result.Codes != 2 {
		t.Fatalf("revoked %+v, want 4 tokens and 2 codes", result)
	}
	want := []string{"sessions:agent-dana", "codes:agent-dana", "tokens:agent-dana"}
	if len(r.order) != len(want) {
		t.Fatalf("the stores were touched %v, want %v", r.order, want)
	}
	for i, step := range want {
		if r.order[i] != step {
			t.Fatalf("the stores were touched %v, want %v", r.order, want)
		}
	}
	requireNoRawIdentity(t, logs, "door-1", "dana@harborlegal.example")
}

// A session store that could not be written is a failure, not a partial
// success: the door is answered 503 and asks again. Nothing after it runs,
// because ending the tokens while the cookie lives is the state this change
// exists to prevent.
func TestRevokeReportsASessionStoreThatCouldNotBeWritten(t *testing.T) {
	store := newMemoryAuthStore()
	store.seedPerson(t, "agent-dana", "Dana Whitfield", OAuthProviderDoor, "door-1", "dana@harborlegal.example")
	logs := &signInLogs{}
	r := newRevocation(store, logs)
	r.sessions.failFor = "agent-dana"
	r.tokens.tokens["agent-dana"] = 4

	_, err := r.service.Revoke(context.Background(), AssertedIdentity{
		Provider: OAuthProviderDoor, Subject: "door-1", Email: "dana@harborlegal.example",
	})

	if err == nil {
		t.Fatal("a session store that could not be written was reported as a revocation")
	}
	if r.tokens.reached("agent-dana") != 0 {
		t.Fatal("the tokens were revoked behind a session that is still alive")
	}
	requireErrorLogged(t, logs)
}

// A code store that could not be written is a failure too: a code already
// handed out still buys a fresh 30-day refresh token.
func TestRevokeReportsACodeStoreThatCouldNotBeWritten(t *testing.T) {
	store := newMemoryAuthStore()
	store.seedPerson(t, "agent-dana", "Dana Whitfield", OAuthProviderDoor, "door-1", "dana@harborlegal.example")
	logs := &signInLogs{}
	r := newRevocation(store, logs)
	r.codes.failFor = "agent-dana"

	_, err := r.service.Revoke(context.Background(), AssertedIdentity{
		Provider: OAuthProviderDoor, Subject: "door-1", Email: "dana@harborlegal.example",
	})

	if err == nil {
		t.Fatal("a code store that could not be written was reported as a revocation")
	}
	requireErrorLogged(t, logs)
}

// requireErrorLogged fails the test unless something was logged at error level.
func requireErrorLogged(t *testing.T, logs *signInLogs) {
	t.Helper()
	for _, line := range logs.all() {
		if line.level == "error" {
			return
		}
	}
	t.Fatalf("nothing was logged at error level:\n%s", logs.text())
}

// wm-yhy83. A person who predates the door holds no door credential, so a door
// reset reaches nobody — and the door is still answered 204, because the
// answer must not say whether an address has an account here. The instance's
// own log is where that is said, and it must be a WARNING: an operator alerts
// on a reset that ended nothing, and cannot alert on an information line.
func TestRevokeWarnsWhenNobodyHoldsTheAssertedIdentity(t *testing.T) {
	store := newMemoryAuthStore()
	store.seedPerson(t, "agent-dana", "Dana Whitfield", OAuthProviderGoogle, "google-108", "dana@harborlegal.example")
	logs := &signInLogs{}

	if _, err := newRevocation(store, logs).service.Revoke(context.Background(), AssertedIdentity{
		Provider: OAuthProviderDoor, Subject: "door-1", Email: "dana@harborlegal.example",
	}); err != nil {
		t.Fatalf("an identity nobody holds is not an error: %v", err)
	}

	requireWarnLogged(t, logs)
}

// The same, one step along: the person was found and held no refresh token.
// "Every refresh token of the person is revoked" with none revoked reads as
// success to whoever reads it next, so a count of zero is a warning too.
func TestRevokeWarnsWhenItEndedNoToken(t *testing.T) {
	store := newMemoryAuthStore()
	store.seedPerson(t, "agent-dana", "Dana Whitfield", OAuthProviderDoor, "door-1", "dana@harborlegal.example")
	logs := &signInLogs{}
	r := newRevocation(store, logs)

	result, err := r.service.Revoke(context.Background(), AssertedIdentity{
		Provider: OAuthProviderDoor, Subject: "door-1", Email: "dana@harborlegal.example",
	})

	if err != nil {
		t.Fatalf("the revocation failed: %v", err)
	}
	if result.Tokens != 0 || len(result.People) != 1 {
		t.Fatalf("revoked %+v, want the one person and no token", result)
	}
	requireWarnLogged(t, logs)
	// The person was still reached: their sessions are ended even though they
	// held no token.
	if r.sessions.endedFor("agent-dana") != 1 {
		t.Fatal("the person's browser sessions were left alive")
	}
}

// requireWarnLogged fails the test unless something was logged at warn level.
func requireWarnLogged(t *testing.T, logs *signInLogs) {
	t.Helper()
	for _, line := range logs.all() {
		if line.level == "warn" {
			return
		}
	}
	t.Fatalf("nothing was logged at warning level:\n%s", logs.text())
}

// wm-gf5xd. "204, always" holds on every success path and not on the error
// path: a store that is down is only ever reached for a person the instance
// holds, so the same broken store answers 503 for an identity somebody here
// holds and 204 for one nobody holds. It is recorded in the ADR as accepted —
// only the issuer gets past 401, and it already knows whom it signed in — and
// pinned here so that it is a known difference rather than a surprise.
func TestRevokeReachesNoStoreForAnIdentityNobodyHolds(t *testing.T) {
	store := newMemoryAuthStore()
	store.seedPerson(t, "agent-dana", "Dana Whitfield", OAuthProviderDoor, "door-1", "dana@harborlegal.example")
	r := newRevocation(store, &signInLogs{})
	r.tokens.failFor = "agent-dana"

	held := AssertedIdentity{Provider: OAuthProviderDoor, Subject: "door-1", Email: "dana@harborlegal.example"}
	unheld := AssertedIdentity{Provider: OAuthProviderDoor, Subject: "door-999", Email: "dana@harborlegal.example"}

	if _, err := r.service.Revoke(context.Background(), held); err == nil {
		t.Fatal("a broken store was reported as a revocation for a person the instance holds")
	}
	if _, err := r.service.Revoke(context.Background(), unheld); err != nil {
		t.Fatalf("the same broken store failed for an identity nobody holds: %v", err)
	}
	if len(r.order) != 3 {
		// sessions, codes, tokens for the person; nothing at all for nobody.
		t.Fatalf("the stores were touched %v, want only the three steps for the person the instance holds", r.order)
	}
}
