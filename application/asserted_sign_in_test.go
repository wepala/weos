package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/repositories"

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

// FindAll answers every person in one page, ordered by id.
func (r storeAgents) FindAll(_ context.Context, _ string, _ int) (*authrepos.PaginatedResponse[*entities.Agent], error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	ids := make([]string, 0, len(r.s.agents))
	for id := range r.s.agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	page := &authrepos.PaginatedResponse[*entities.Agent]{}
	for _, id := range ids {
		page.Data = append(page.Data, r.s.agents[id])
	}
	return page, nil
}

// --- a logger that remembers every line ---

type signInLogLine struct {
	level, msg string
	fields     []any
}

func (l signInLogLine) field(key string) any {
	for i := 0; i+1 < len(l.fields); i += 2 {
		if k, ok := l.fields[i].(string); ok && k == key {
			return l.fields[i+1]
		}
	}
	return nil
}

type signInLogs struct {
	mu    sync.Mutex
	lines []signInLogLine
}

func (l *signInLogs) add(level, msg string, fields []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, signInLogLine{level: level, msg: msg, fields: fields})
}

func (l *signInLogs) Debug(_ context.Context, m string, f ...any) { l.add("debug", m, f) }
func (l *signInLogs) Info(_ context.Context, m string, f ...any)  { l.add("info", m, f) }
func (l *signInLogs) Warn(_ context.Context, m string, f ...any)  { l.add("warn", m, f) }
func (l *signInLogs) Error(_ context.Context, m string, f ...any) { l.add("error", m, f) }

func (l *signInLogs) all() []signInLogLine {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]signInLogLine(nil), l.lines...)
}

func (l *signInLogs) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = nil
}

func (l *signInLogs) text() string {
	var b strings.Builder
	for _, line := range l.all() {
		fmt.Fprintf(&b, "%s %s %v\n", line.level, line.msg, line.fields)
	}
	return b.String()
}

// sha256Prefix is the operator's recipe for finding a person's lines:
// printf '%s' "$value" | shasum -a 256 | cut -c1-16
func sha256Prefix(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}

func requireIdentityFields(t *testing.T, line signInLogLine, provider, sub, normalizedEmail, agentIDs string) {
	t.Helper()
	want := map[string]string{
		"provider":   provider,
		"sub_hash":   sha256Prefix(sub),
		"email_hash": sha256Prefix(normalizedEmail),
		"agent_ids":  agentIDs,
	}
	for key, value := range want {
		if got := line.field(key); got != value {
			t.Fatalf("log line %q has %s = %v, want %q (fields %v)", line.msg, key, got, value, line.fields)
		}
	}
}

// requireNoRawIdentity fails when any line carries one of the values, in any
// capitals.
func requireNoRawIdentity(t *testing.T, logs *signInLogs, values ...string) {
	t.Helper()
	logged := strings.ToLower(logs.text())
	for _, v := range values {
		if strings.Contains(logged, strings.ToLower(v)) {
			t.Fatalf("the log carries %q:\n%s", v, logs.text())
		}
	}
}

type storeEmails struct{ s *memoryAuthStore }

func (q storeEmails) CredentialsByEmail(_ context.Context, email string) ([]repositories.CredentialEmailMatch, error) {
	q.s.mu.Lock()
	defer q.s.mu.Unlock()
	if q.s.emailsErr != nil {
		return nil, q.s.emailsErr
	}
	normalized := strings.ToLower(strings.TrimSpace(email))
	var matches []repositories.CredentialEmailMatch
	for _, c := range q.s.creds {
		if strings.ToLower(strings.TrimSpace(c.Email())) == normalized {
			matches = append(matches, repositories.CredentialEmailMatch{
				AgentID: c.AgentID(), Provider: c.Provider(), Active: c.Active(),
			})
		}
	}
	return matches, nil
}

// deactivateCredential turns off the credential for (provider, sub), as an
// operator turning off one sign-in method would.
func (s *memoryAuthStore) deactivateCredential(t *testing.T, provider, sub string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	cred := s.findByProvider(provider, sub)
	if cred == nil {
		t.Fatalf("no credential for %s %s to deactivate", provider, sub)
	}
	if err := cred.Deactivate(); err != nil {
		t.Fatalf("deactivate credential: %v", err)
	}
}

// deactivateAgent turns the person off.
func (s *memoryAuthStore) deactivateAgent(t *testing.T, agentID string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	agent := s.agents[agentID]
	if agent == nil {
		t.Fatalf("no agent %s to deactivate", agentID)
	}
	if err := agent.Deactivate(); err != nil {
		t.Fatalf("deactivate agent: %v", err)
	}
}

func newTestAssertedSignIn(s *memoryAuthStore, linkByEmail bool) *AssertedSignIn {
	return newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) { cfg.LinkByEmail = linkByEmail })
}

// newTestAssertedSignInWith builds the service over the store, letting a test
// change the configuration first.
func newTestAssertedSignInWith(s *memoryAuthStore, configure func(*AssertedSignInConfig)) *AssertedSignIn {
	cfg := AssertedSignInConfig{
		Auth:        storeAuth{s: s},
		Credentials: storeCredentials{s: s},
		Agents:      storeAgents{s: s},
		Emails:      storeEmails{s: s},
	}
	configure(&cfg)
	return NewAssertedSignIn(cfg)
}

// allowlisted configures an allowlisted instance, with password registration
// open or not.
func allowlisted(registrationOpen bool) func(*AssertedSignInConfig) {
	return func(cfg *AssertedSignInConfig) {
		cfg.LinkByEmail = true
		cfg.PasswordRegistrationOpen = registrationOpen
	}
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

// The capture: registration is open on an allowlisted instance, and someone
// registers the owner's email with a password before the owner's first sign-in
// through the door.
func TestAssertedSignInNeverHandsAnOwnersIdentityToAPasswordAccountAnyoneCouldRegister(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-registrant", "ops", "password", "ops@harborlegal.example", "ops@harborlegal.example")

	owner := AssertedIdentity{Provider: "google", Subject: "117590246813570924368", Email: "ops@harborlegal.example", Name: "Harbor Ops"}
	got, err := newTestAssertedSignInWith(s, allowlisted(true)).SignIn(context.Background(), owner)
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if got.Agent.GetID() == "agent-registrant" {
		t.Fatalf("the owner's door sign-in reached the self-registered password account")
	}
	if !got.NewAccount || s.createCount() != 1 {
		t.Fatalf("want a new person: new=%v creates=%d", got.NewAccount, s.createCount())
	}
	if linked := s.credentialFor("google", "117590246813570924368"); linked == nil || linked.AgentID() == "agent-registrant" {
		t.Fatalf("the owner's identity is stored against %v, want the new person", linked)
	}
}

func TestAssertedSignInLinksAPasswordOwnerWhenOnlyTheOperatorCanRegister(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-ops", "ops", "password", "ops@harborlegal.example", "ops@harborlegal.example")

	owner := AssertedIdentity{Provider: "google", Subject: "117590246813570924368", Email: "ops@harborlegal.example", Name: "Harbor Ops"}
	got, err := newTestAssertedSignInWith(s, allowlisted(false)).SignIn(context.Background(), owner)
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if got.NewAccount || got.Agent.GetID() != "agent-ops" || s.createCount() != 0 {
		t.Fatalf("want a link to agent-ops: agent=%s new=%v creates=%d", got.Agent.GetID(), got.NewAccount, s.createCount())
	}
}

func TestAssertedSignInStillLinksAProviderCredentialWhilePasswordRegistrationIsOpen(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-dana", "Dana Whitfield", "google", googleSub, "dana.whitfield@harborlegal.example")
	// A registrant holding the same email is not counted, so it neither
	// captures the owner nor makes the owner ambiguous.
	s.seedPerson(t, "agent-registrant", "dana", "password", "dana.whitfield@harborlegal.example", "dana.whitfield@harborlegal.example")

	got, err := newTestAssertedSignInWith(s, allowlisted(true)).SignIn(context.Background(), dana("apple", appleSub))
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if got.NewAccount || got.Agent.GetID() != "agent-dana" {
		t.Fatalf("want a link to agent-dana: agent=%s new=%v", got.Agent.GetID(), got.NewAccount)
	}
}

func TestAssertedSignInPassesOverWhatWasTurnedOff(t *testing.T) {
	turnOff := map[string]func(t *testing.T, s *memoryAuthStore){
		"an inactive credential": func(t *testing.T, s *memoryAuthStore) { s.deactivateCredential(t, "google", googleSub) },
		"an inactive person":     func(t *testing.T, s *memoryAuthStore) { s.deactivateAgent(t, "agent-dana-old") },
	}
	for name, off := range turnOff {
		t.Run(name+" is not linked", func(t *testing.T) {
			s := newMemoryAuthStore()
			s.seedPerson(t, "agent-dana-old", "Dana Whitfield", "google", googleSub, "dana.whitfield@harborlegal.example")
			off(t, s)

			got, err := newTestAssertedSignIn(s, true).SignIn(context.Background(), dana("apple", appleSub))
			if err != nil {
				t.Fatalf("SignIn: %v", err)
			}
			if got.Agent.GetID() == "agent-dana-old" || !got.NewAccount {
				t.Fatalf("what was turned off was linked back: agent=%s new=%v", got.Agent.GetID(), got.NewAccount)
			}
		})
		t.Run(name+" does not make the owner ambiguous", func(t *testing.T) {
			s := newMemoryAuthStore()
			s.seedPerson(t, "agent-dana-old", "Dana Whitfield", "google", googleSub, "dana.whitfield@harborlegal.example")
			s.seedPerson(t, "agent-dana", "Dana Whitfield", "password", "dana.whitfield@harborlegal.example", "dana.whitfield@harborlegal.example")
			off(t, s)

			got, err := newTestAssertedSignIn(s, true).SignIn(context.Background(), dana("apple", appleSub))
			if err != nil {
				t.Fatalf("SignIn: %v", err)
			}
			if got.NewAccount || got.Agent.GetID() != "agent-dana" {
				t.Fatalf("want a link to the one active owner: agent=%s new=%v", got.Agent.GetID(), got.NewAccount)
			}
		})
	}
}

func TestAssertedSignInLogsEachLinkOnceWithHashesAndTheOwner(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-ops", "ops", "password", "ops@harborlegal.example", "ops@harborlegal.example")
	logs := &signInLogs{}
	svc := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) {
		cfg.LinkByEmail = true
		cfg.Logger = logs
	})

	identity := AssertedIdentity{Provider: "apple", Subject: appleSub, Email: " Ops@HarborLegal.example", Name: "Harbor Ops"}
	if _, err := svc.SignIn(context.Background(), identity); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	lines := logs.all()
	if len(lines) != 1 || lines[0].level != "info" {
		t.Fatalf("expected one info line for the link, got:\n%s", logs.text())
	}
	requireIdentityFields(t, lines[0], "apple", appleSub, "ops@harborlegal.example", "agent-ops")
	requireNoRawIdentity(t, logs, appleSub, "ops@harborlegal.example")

	// Signing in again reaches the person by subject: nothing new to log.
	logs.reset()
	if _, err := svc.SignIn(context.Background(), identity); err != nil {
		t.Fatalf("second SignIn: %v", err)
	}
	if got := logs.text(); got != "" {
		t.Fatalf("a returning sign-in logged:\n%s", got)
	}
}

func TestAssertedSignInLogsEachPersonItCreates(t *testing.T) {
	marcus := func(t *testing.T, s *memoryAuthStore) {
		s.seedPerson(t, "agent-marcus", "Marcus Okafor", "apple", "000917.3b6e", "marcus.okafor@harborlegal.example")
	}
	cases := map[string]struct {
		allowlist bool
		stage     func(t *testing.T, s *memoryAuthStore)
		level     string
		others    any
	}{
		"no allowlist, another person already here": {false, marcus, "info", nil},
		"allowlisted, nobody else here":             {true, func(*testing.T, *memoryAuthStore) {}, "info", nil},
		"allowlisted, only a person turned off": {true, func(t *testing.T, s *memoryAuthStore) {
			marcus(t, s)
			s.deactivateAgent(t, "agent-marcus")
		}, "info", nil},
		"allowlisted, another person already here": {true, marcus, "warn", "agent-marcus"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newMemoryAuthStore()
			c.stage(t, s)
			logs := &signInLogs{}
			svc := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) {
				cfg.LinkByEmail = c.allowlist
				cfg.Logger = logs
			})

			got, err := svc.SignIn(context.Background(), dana("google", googleSub))
			if err != nil || !got.NewAccount {
				t.Fatalf("SignIn: new=%v err=%v", got.NewAccount, err)
			}
			lines := logs.all()
			if len(lines) != 1 || lines[0].level != c.level {
				t.Fatalf("expected one %s line for the created person, got:\n%s", c.level, logs.text())
			}
			requireIdentityFields(t, lines[0], "google", googleSub, "dana.whitfield@harborlegal.example", got.Agent.GetID())
			if others := lines[0].field("other_agent_ids"); others != c.others {
				t.Fatalf("other_agent_ids = %v, want %v", others, c.others)
			}
			requireNoRawIdentity(t, logs, googleSub, "dana.whitfield@harborlegal.example")
		})
	}
}

func TestAssertedSignInLogsAnAmbiguousOwnerWithEveryPersonHoldingTheEmail(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-dana-2", "Dana W", "google", googleSub, "Dana.Whitfield@harborlegal.example")
	s.seedPerson(t, "agent-dana", "Dana Whitfield", "password", "dana.whitfield@harborlegal.example", "dana.whitfield@harborlegal.example")
	logs := &signInLogs{}
	svc := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) {
		cfg.LinkByEmail = true
		cfg.Logger = logs
	})

	_, err := svc.SignIn(context.Background(), dana("apple", appleSub))
	if !errors.Is(err, ErrAmbiguousOwner) {
		t.Fatalf("err = %v, want ErrAmbiguousOwner", err)
	}
	lines := logs.all()
	if len(lines) != 1 || lines[0].level != "error" {
		t.Fatalf("expected one error line for the refusal, got:\n%s", logs.text())
	}
	if reason := lines[0].field("reason"); reason != ReasonAmbiguousOwner {
		t.Fatalf("reason = %v, want %q", reason, ReasonAmbiguousOwner)
	}
	requireIdentityFields(t, lines[0], "apple", appleSub, "dana.whitfield@harborlegal.example", "agent-dana,agent-dana-2")
	requireNoRawIdentity(t, logs, appleSub, googleSub, "dana.whitfield@harborlegal.example")
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
