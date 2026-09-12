package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	"github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
)

// memoryAuthStore stands in for pericarp's store and its FindOrCreateAgent.
// window widens the gap between FindOrCreateAgent's look-up and its create, so
// a missing lock shows as a second person rather than as luck.
type memoryAuthStore struct {
	mu       sync.Mutex
	agents   map[string]*entities.Agent
	accounts map[string]*entities.Account // by agent id
	creds    []*entities.Credential
	creates  int
	window   time.Duration

	emailsErr error
}

func newMemoryAuthStore() *memoryAuthStore {
	return &memoryAuthStore{agents: map[string]*entities.Agent{}, accounts: map[string]*entities.Account{}}
}

func (s *memoryAuthStore) findByProvider(provider, sub string) *entities.Credential {
	for _, c := range s.creds {
		if c.Provider() == provider && c.ProviderUserID() == sub {
			return c
		}
	}
	return nil
}

func (s *memoryAuthStore) seedPerson(t *testing.T, agentID, name, provider, sub, email string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	agent, err := new(entities.Agent).With(agentID, name, entities.AgentTypePerson)
	if err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	account, err := new(entities.Account).With("account-"+agentID, name+"'s Account", entities.AccountTypePersonal)
	if err != nil {
		t.Fatalf("seed account: %v", err)
	}
	s.agents[agentID] = agent
	s.accounts[agentID] = account
	s.seedCredentialLocked(t, agentID, provider, sub, email)
}

// seedCredential adds a credential without a person, or a second credential
// to a person already seeded.
func (s *memoryAuthStore) seedCredential(t *testing.T, agentID, provider, sub, email string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seedCredentialLocked(t, agentID, provider, sub, email)
}

func (s *memoryAuthStore) seedCredentialLocked(t *testing.T, agentID, provider, sub, email string) {
	t.Helper()
	cred, err := new(entities.Credential).With(fmt.Sprintf("cred-%d", len(s.creds)+1), agentID, provider, sub, email, "")
	if err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	s.creds = append(s.creds, cred)
}

func (s *memoryAuthStore) createCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.creates
}

func (s *memoryAuthStore) credentialFor(provider, sub string) *entities.Credential {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.findByProvider(provider, sub)
}

type storeAuth struct {
	authapp.AuthenticationService
	s *memoryAuthStore
}

func (a storeAuth) FindOrCreateAgent(_ context.Context, info authapp.UserInfo) (
	*entities.Agent, *entities.Credential, *entities.Account, error,
) {
	s := a.s
	s.mu.Lock()
	if cred := s.findByProvider(info.Provider, info.ProviderUserID); cred != nil {
		agent := s.agents[cred.AgentID()]
		account := s.accounts[cred.AgentID()]
		s.mu.Unlock()
		if agent == nil {
			return nil, nil, nil, fmt.Errorf("agent %s not found", cred.AgentID())
		}
		return agent, cred, account, nil
	}
	s.mu.Unlock()

	time.Sleep(s.window)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.creates++
	agentID := fmt.Sprintf("agent-created-%d", s.creates)
	agent, err := new(entities.Agent).With(agentID, info.DisplayName, entities.AgentTypePerson)
	if err != nil {
		return nil, nil, nil, err
	}
	account, err := new(entities.Account).With("account-"+agentID, info.DisplayName+"'s Account", entities.AccountTypePersonal)
	if err != nil {
		return nil, nil, nil, err
	}
	cred, err := new(entities.Credential).With(fmt.Sprintf("cred-%d", len(s.creds)+1), agentID,
		info.Provider, info.ProviderUserID, info.Email, info.DisplayName)
	if err != nil {
		return nil, nil, nil, err
	}
	s.agents[agentID] = agent
	s.accounts[agentID] = account
	s.creds = append(s.creds, cred)
	return agent, cred, account, nil
}

type storeCredentials struct {
	authrepos.CredentialRepository
	s *memoryAuthStore
}

func (r storeCredentials) FindByProvider(_ context.Context, provider, sub string) (*entities.Credential, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	return r.s.findByProvider(provider, sub), nil
}

func (r storeCredentials) Save(_ context.Context, cred *entities.Credential) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if existing := r.s.findByProvider(cred.Provider(), cred.ProviderUserID()); existing != nil && existing.GetID() != cred.GetID() {
		return authrepos.ErrDuplicateCredential
	}
	r.s.creds = append(r.s.creds, cred)
	return nil
}

type storeAgents struct {
	authrepos.AgentRepository
	s *memoryAuthStore
}

func (r storeAgents) FindByID(_ context.Context, id string) (*entities.Agent, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	return r.s.agents[id], nil
}

type storeEmails struct{ s *memoryAuthStore }

func (q storeEmails) AgentIDsByEmail(_ context.Context, email string) ([]string, error) {
	q.s.mu.Lock()
	defer q.s.mu.Unlock()
	if q.s.emailsErr != nil {
		return nil, q.s.emailsErr
	}
	normalized := strings.ToLower(strings.TrimSpace(email))
	seen := map[string]bool{}
	var ids []string
	for _, c := range q.s.creds {
		if strings.ToLower(strings.TrimSpace(c.Email())) == normalized && !seen[c.AgentID()] {
			seen[c.AgentID()] = true
			ids = append(ids, c.AgentID())
		}
	}
	return ids, nil
}

func newTestAssertedSignIn(s *memoryAuthStore, linkByEmail bool) *AssertedSignIn {
	return NewAssertedSignIn(AssertedSignInConfig{
		Auth:        storeAuth{s: s},
		Credentials: storeCredentials{s: s},
		Agents:      storeAgents{s: s},
		Emails:      storeEmails{s: s},
		LinkByEmail: linkByEmail,
	})
}

func dana(provider, sub string) AssertedIdentity {
	return AssertedIdentity{
		Provider: provider, Subject: sub,
		Email: "dana.whitfield@harborlegal.example", Name: "Dana Whitfield",
	}
}

const (
	googleSub = "108234917650023841257"
	appleSub  = "001482.7f3c9a2e5b8d4e61a0c2f9b7d3e6a815.1734"
)

func TestAssertedSignInCreatesAPersonForAnIdentityNeverSeen(t *testing.T) {
	for _, link := range []bool{false, true} {
		t.Run(fmt.Sprintf("allowlist set %v", link), func(t *testing.T) {
			s := newMemoryAuthStore()
			got, err := newTestAssertedSignIn(s, link).SignIn(context.Background(), dana("google", googleSub))
			if err != nil {
				t.Fatalf("SignIn: %v", err)
			}
			if !got.NewAccount {
				t.Fatalf("a sign-in that created the person reports NewAccount false")
			}
			if s.createCount() != 1 {
				t.Fatalf("created %d people, want 1", s.createCount())
			}
			if got.Agent == nil || got.Agent.Name() != "Dana Whitfield" || got.Account == nil || got.Credential == nil {
				t.Fatalf("result does not name the new person, account and credential: %+v", got)
			}
		})
	}
}

func TestAssertedSignInReachesThePersonAnIdentityAlreadyHolds(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-dana", "Dana Whitfield", "google", googleSub, "dana.whitfield@harborlegal.example")

	identity := dana("google", googleSub)
	identity.Email = "dana.whitfield@cedarrealty.example" // the email beside the subject has changed
	got, err := newTestAssertedSignIn(s, true).SignIn(context.Background(), identity)
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if got.NewAccount || got.Agent.GetID() != "agent-dana" || s.createCount() != 0 {
		t.Fatalf("reuse: NewAccount=%v agent=%s creates=%d", got.NewAccount, got.Agent.GetID(), s.createCount())
	}
}

func TestAssertedSignInLinksAnOwnersNewIdentityWhenTheAllowlistIsSet(t *testing.T) {
	s := newMemoryAuthStore()
	// A password credential, as the operator makes one: a lower-case email.
	s.seedPerson(t, "agent-ops", "ops", "password", "ops@harborlegal.example", "ops@harborlegal.example")

	identity := AssertedIdentity{Provider: "apple", Subject: appleSub, Email: " Ops@HarborLegal.example", Name: "Harbor Ops"}
	got, err := newTestAssertedSignIn(s, true).SignIn(context.Background(), identity)
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if got.NewAccount {
		t.Fatalf("a link reports NewAccount true")
	}
	if got.Agent.GetID() != "agent-ops" || s.createCount() != 0 {
		t.Fatalf("link reached agent %s after %d creates, want agent-ops and none", got.Agent.GetID(), s.createCount())
	}
	if got.Account == nil || got.Account.GetID() != "account-agent-ops" {
		t.Fatalf("link acts in %v, want the owner's account", got.Account)
	}
	linked := s.credentialFor("apple", appleSub)
	if linked == nil || linked.AgentID() != "agent-ops" {
		t.Fatalf("the new identity is not stored against its owner: %v", linked)
	}
	if got.Credential == nil || got.Credential.GetID() != linked.GetID() {
		t.Fatalf("the session would be made from credential %v, want the linked one %s", got.Credential, linked.GetID())
	}

	// The link is stored: once the allowlist is gone the identity still
	// reaches its owner, by subject.
	again, err := newTestAssertedSignIn(s, false).SignIn(context.Background(), identity)
	if err != nil || again.NewAccount || again.Agent.GetID() != "agent-ops" {
		t.Fatalf("after the allowlist is cleared: agent=%v new=%v err=%v", again.Agent, again.NewAccount, err)
	}
}

func TestAssertedSignInLinksNothingWithoutTheAllowlist(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-dana", "Dana Whitfield", "google", googleSub, "dana.whitfield@harborlegal.example")

	got, err := newTestAssertedSignIn(s, false).SignIn(context.Background(), dana("apple", appleSub))
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if !got.NewAccount || got.Agent.GetID() == "agent-dana" || s.createCount() != 1 {
		t.Fatalf("no allowlist: NewAccount=%v agent=%s creates=%d", got.NewAccount, got.Agent.GetID(), s.createCount())
	}
}

func TestAssertedSignInNeverLinksTwoDifferentEmails(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-dana", "Dana Whitfield", "google", googleSub, "dana.whitfield@harborlegal.example")

	marcus := AssertedIdentity{Provider: "apple", Subject: "000917.3b6e", Email: "marcus.okafor@harborlegal.example", Name: "marcus.okafor"}
	got, err := newTestAssertedSignIn(s, true).SignIn(context.Background(), marcus)
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if !got.NewAccount || got.Agent.GetID() == "agent-dana" {
		t.Fatalf("a different email reached agent %s (new=%v)", got.Agent.GetID(), got.NewAccount)
	}
}

func TestAssertedSignInRefusesAnEmailTwoPeopleHold(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-dana", "Dana Whitfield", "password", "dana.whitfield@harborlegal.example", "dana.whitfield@harborlegal.example")
	s.seedPerson(t, "agent-dana-2", "Dana W", "google", googleSub, "Dana.Whitfield@harborlegal.example")

	_, err := newTestAssertedSignIn(s, true).SignIn(context.Background(), dana("apple", appleSub))
	if !errors.Is(err, ErrAmbiguousOwner) {
		t.Fatalf("err = %v, want ErrAmbiguousOwner", err)
	}
	if s.createCount() != 0 || s.credentialFor("apple", appleSub) != nil {
		t.Fatalf("an ambiguous owner still created something: creates=%d", s.createCount())
	}
}

func TestAssertedSignInPassesOverACredentialWhosePersonIsGone(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedCredential(t, "agent-erased", "google", googleSub, "dana.whitfield@harborlegal.example")

	got, err := newTestAssertedSignIn(s, true).SignIn(context.Background(), dana("apple", appleSub))
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if !got.NewAccount || s.createCount() != 1 {
		t.Fatalf("a credential with no person behind it was linked: new=%v creates=%d", got.NewAccount, s.createCount())
	}
}

func TestAssertedSignInSurfacesAnEmailLookupFailure(t *testing.T) {
	s := newMemoryAuthStore()
	s.emailsErr = errors.New("database unavailable")

	_, err := newTestAssertedSignIn(s, true).SignIn(context.Background(), dana("apple", appleSub))
	if err == nil || errors.Is(err, ErrAmbiguousOwner) {
		t.Fatalf("err = %v, want the lookup failure", err)
	}
	if s.createCount() != 0 {
		t.Fatalf("a failed owner lookup fell through to creating a person")
	}
}

func TestAssertedSignInConcurrentFirstSignInsForOneIdentityLeaveOnePerson(t *testing.T) {
	s := newMemoryAuthStore()
	s.window = 20 * time.Millisecond
	svc := newTestAssertedSignIn(s, false)

	results := runTogether(t, 8, func(int) AssertedIdentity { return dana("google", googleSub) }, svc)

	if s.createCount() != 1 {
		t.Fatalf("created %d people for one identity, want 1", s.createCount())
	}
	requireOnePersonOneCreate(t, results)
}

func TestAssertedSignInConcurrentFirstSignInsFromTwoProvidersLeaveOneOwner(t *testing.T) {
	s := newMemoryAuthStore()
	s.window = 20 * time.Millisecond
	svc := newTestAssertedSignIn(s, true)

	results := runTogether(t, 2, func(i int) AssertedIdentity {
		if i == 0 {
			return dana("google", googleSub)
		}
		return dana("apple", appleSub)
	}, svc)

	if s.createCount() != 1 {
		t.Fatalf("created %d people for one owner's two identities, want 1", s.createCount())
	}
	requireOnePersonOneCreate(t, results)
}

func runTogether(t *testing.T, n int, identity func(int) AssertedIdentity, svc *AssertedSignIn) []AssertedSignInResult {
	t.Helper()
	results := make([]AssertedSignInResult, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = svc.SignIn(context.Background(), identity(i))
		}()
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("sign-in %d: %v", i, err)
		}
	}
	return results
}

func requireOnePersonOneCreate(t *testing.T, results []AssertedSignInResult) {
	t.Helper()
	created := 0
	for _, r := range results {
		if r.Agent.GetID() != results[0].Agent.GetID() {
			t.Fatalf("the sign-ins reached %s and %s", results[0].Agent.GetID(), r.Agent.GetID())
		}
		if r.NewAccount {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("%d sign-ins report that they created the person, want exactly 1", created)
	}
}
