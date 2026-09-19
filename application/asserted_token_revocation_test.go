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
}

func newFakeRevoker() *fakeRevoker {
	return &fakeRevoker{calls: map[string]int{}, tokens: map[string]int64{}}
}

func (f *fakeRevoker) RevokeAllForAgent(_ context.Context, agentID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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

func newRevocationFor(store *memoryAuthStore, tokens RefreshTokenRevoker, logs *signInLogs) *AssertedTokenRevocation {
	return NewAssertedTokenRevocation(AssertedTokenRevocationConfig{
		Credentials: storeCredentials{s: store},
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
