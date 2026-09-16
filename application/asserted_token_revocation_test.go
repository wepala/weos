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

func newRevocationFor(store *memoryAuthStore, tokens RefreshTokenRevoker, logs *signInLogs, passwordOwnersProven bool) *AssertedTokenRevocation {
	return NewAssertedTokenRevocation(AssertedTokenRevocationConfig{
		Credentials:          storeCredentials{s: store},
		Agents:               storeAgents{s: store},
		Emails:               storeEmails{s: store},
		Tokens:               tokens,
		PasswordOwnersProven: passwordOwnersProven,
		Logger:               logs,
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

	result, err := newRevocationFor(store, tokens, logs, false).Revoke(context.Background(), AssertedIdentity{
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

// The instance that had people before it had a door: the person holds
// connector tokens from a Google sign-in and no credential for the door
// identity yet, because they have not come through the door. A sign-in would
// link the two by the email, so the revocation reaches that person.
func TestRevokeReachesThePersonWhoseCredentialProvesTheAssertedEmail(t *testing.T) {
	store := newMemoryAuthStore()
	store.seedPerson(t, "agent-dana", "Dana Whitfield", OAuthProviderGoogle, "google-108", "dana.whitfield@harborlegal.example")
	tokens := newFakeRevoker()
	tokens.tokens["agent-dana"] = 2

	result, err := newRevocationFor(store, tokens, &signInLogs{}, false).Revoke(context.Background(), AssertedIdentity{
		// A door identity this instance has never seen, with the owner's email.
		Provider: OAuthProviderDoor, Subject: "door-1", Email: "Dana.Whitfield@HarborLegal.example",
	})

	if err != nil {
		t.Fatalf("the revocation failed: %v", err)
	}
	if result.Tokens != 2 || len(result.People) != 1 || result.People[0] != "agent-dana" {
		t.Fatalf("revoked %+v, want the tokens of the person whose credential proves the email", result)
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

// A credential that proves nothing about who owns its email — an invite
// nobody verified, or a password one where the operator has not opted in —
// reaches nobody. The same rule the sign-in refuses to link on.
func TestRevokeReachesNobodyWhenNothingProvesTheEmail(t *testing.T) {
	store := newMemoryAuthStore()
	store.seedPerson(t, "agent-dana", "Dana Whitfield", "invite", "invite-1", "dana@harborlegal.example")
	tokens := newFakeRevoker()
	logs := &signInLogs{}

	result, err := newRevocationFor(store, tokens, logs, false).Revoke(context.Background(), AssertedIdentity{
		Provider: OAuthProviderDoor, Subject: "door-1", Email: "dana@harborlegal.example",
	})

	if err != nil {
		t.Fatalf("an identity nobody holds is not an error: %v", err)
	}
	if result.Tokens != 0 || len(result.People) != 0 || tokens.people() != 0 {
		t.Fatalf("revoked %+v for an email nothing proves", result)
	}
	if logs.text() == "" {
		t.Fatal("a revocation that reached nobody was not recorded")
	}
}

// Where a sign-in refuses because more than one person proves the email, the
// revocation revokes for all of them. Picking nobody would leave an intruder
// renewing; a revocation grants nothing, so reaching one person too many costs
// only a sign-in.
func TestRevokeReachesEveryPersonWhoProvesTheEmail(t *testing.T) {
	store := newMemoryAuthStore()
	store.seedPerson(t, "agent-dana", "Dana Whitfield", OAuthProviderGoogle, "google-108", "shared@harborlegal.example")
	store.seedPerson(t, "agent-morgan", "Morgan Reyes", OAuthProviderApple, "apple-42", "shared@harborlegal.example")
	tokens := newFakeRevoker()
	tokens.tokens["agent-dana"] = 1
	tokens.tokens["agent-morgan"] = 2

	result, err := newRevocationFor(store, tokens, &signInLogs{}, false).Revoke(context.Background(), AssertedIdentity{
		Provider: OAuthProviderDoor, Subject: "door-1", Email: "shared@harborlegal.example",
	})

	if err != nil {
		t.Fatalf("the revocation failed: %v", err)
	}
	if len(result.People) != 2 || result.Tokens != 3 {
		t.Fatalf("revoked %+v, want both people's 3 tokens", result)
	}
}

// A person who is turned off holds nothing a sign-in would reach, and every
// path that reads their tokens refuses them already.
func TestRevokeDoesNotReachAPersonWhoIsTurnedOff(t *testing.T) {
	store := newMemoryAuthStore()
	store.seedPerson(t, "agent-dana", "Dana Whitfield", OAuthProviderGoogle, "google-108", "dana@harborlegal.example")
	store.mu.Lock()
	if err := store.agents["agent-dana"].Deactivate(); err != nil {
		store.mu.Unlock()
		t.Fatalf("turn the person off: %v", err)
	}
	store.mu.Unlock()
	tokens := newFakeRevoker()

	result, err := newRevocationFor(store, tokens, &signInLogs{}, false).Revoke(context.Background(), AssertedIdentity{
		Provider: OAuthProviderDoor, Subject: "door-1", Email: "dana@harborlegal.example",
	})

	if err != nil {
		t.Fatalf("the revocation failed: %v", err)
	}
	if len(result.People) != 0 || tokens.people() != 0 {
		t.Fatalf("revoked %+v for a person who is turned off", result)
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

	_, err := newRevocationFor(store, tokens, logs, false).Revoke(context.Background(), AssertedIdentity{
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
	revocation := newRevocationFor(store, tokens, &signInLogs{}, false)
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
