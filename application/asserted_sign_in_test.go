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
	esdomain "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	esinfra "github.com/akeemphilbert/pericarp/pkg/eventsourcing/infrastructure"
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
	// saveErr fails every credential save. beforeSave, when set, runs once
	// inside the next save, with the store locked, before the save checks
	// for a duplicate: another process writing first.
	saveErr    error
	beforeSave func()
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
	if r.s.saveErr != nil {
		return r.s.saveErr
	}
	if hook := r.s.beforeSave; hook != nil {
		r.s.beforeSave = nil
		hook()
	}
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
	folded := repositories.FoldCredentialEmail(email)
	var matches []repositories.CredentialEmailMatch
	for _, c := range q.s.creds {
		if repositories.FoldCredentialEmail(c.Email()) == folded {
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

func newTestAssertedSignIn(s *memoryAuthStore, allowlist bool) *AssertedSignIn {
	return newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) { cfg.Allowlisted = allowlist })
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

// allowlisted configures an allowlisted instance, with the operator's opt-in
// that lets password credentials prove an owner set or not.
func allowlisted(passwordOwners bool) func(*AssertedSignInConfig) {
	return func(cfg *AssertedSignInConfig) {
		cfg.Allowlisted = true
		cfg.PasswordOwnersProven = passwordOwners
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
	got, err := newTestAssertedSignInWith(s, allowlisted(true)).SignIn(context.Background(), identity)
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

// Owner binding runs with or without an allowlist (decision wm-vvi6t). The
// instances behind the door set none, and an owner who signs in there with a
// second provider must still reach the one person they already are.
func TestAssertedSignInLinksAnOwnersNewIdentityWithoutTheAllowlist(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-dana", "Dana Whitfield", "google", googleSub, "dana.whitfield@harborlegal.example")

	got, err := newTestAssertedSignIn(s, false).SignIn(context.Background(), dana("apple", appleSub))
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if got.NewAccount || got.Agent.GetID() != "agent-dana" || s.createCount() != 0 {
		t.Fatalf("no allowlist: NewAccount=%v agent=%s creates=%d, want a link to agent-dana", got.NewAccount, got.Agent.GetID(), s.createCount())
	}
	if linked := s.credentialFor("apple", appleSub); linked == nil || linked.AgentID() != "agent-dana" {
		t.Fatalf("the new identity is not stored against its owner: %v", linked)
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

	_, err := newTestAssertedSignInWith(s, allowlisted(true)).SignIn(context.Background(), dana("apple", appleSub))
	if !errors.Is(err, ErrAmbiguousOwner) {
		t.Fatalf("err = %v, want ErrAmbiguousOwner", err)
	}
	if s.createCount() != 0 || s.credentialFor("apple", appleSub) != nil {
		t.Fatalf("an ambiguous owner still created something: creates=%d", s.createCount())
	}
}

// A credential whose person no longer exists proves no owner, and it still
// holds the email, so the sign-in is refused for an operator to decide rather
// than creating a person beside it. Account erasure deletes a person's
// credentials with them, so only damaged data leaves one behind.
func TestAssertedSignInRefusesWhenTheOnlyCredentialHoldingTheEmailHasNoPerson(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedCredential(t, "agent-erased", "google", googleSub, "dana.whitfield@harborlegal.example")

	_, err := newTestAssertedSignIn(s, true).SignIn(context.Background(), dana("apple", appleSub))
	requireUnprovenOwner(t, s, err, "apple", appleSub)
}

// The capture: someone registers the owner's email with a password before the
// owner's first sign-in through the door. Unless the operator has said the
// instance's password accounts are theirs, that credential proves nothing: the
// sign-in is refused, neither linked to it nor given a second person.
func TestAssertedSignInNeverHandsAnOwnersIdentityToAPasswordAccountWithoutTheOptIn(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-registrant", "ops", "password", "ops@harborlegal.example", "ops@harborlegal.example")

	_, err := newTestAssertedSignInWith(s, allowlisted(false)).SignIn(context.Background(), harborOps("google", ownerSub))
	requireUnprovenOwner(t, s, err, "google", ownerSub)
}

func TestAssertedSignInLinksAPasswordOwnerWhenTheOperatorOptsIn(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-ops", "ops", "password", "ops@harborlegal.example", "ops@harborlegal.example")

	got, err := newTestAssertedSignInWith(s, allowlisted(true)).SignIn(context.Background(), harborOps("google", ownerSub))
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if got.NewAccount || got.Agent.GetID() != "agent-ops" || s.createCount() != 0 {
		t.Fatalf("want a link to agent-ops: agent=%s new=%v creates=%d", got.Agent.GetID(), got.NewAccount, s.createCount())
	}
}

func TestAssertedSignInLinksAProvingCredentialBesideAPasswordOneThatProvesNothing(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-dana", "Dana Whitfield", "google", googleSub, "dana.whitfield@harborlegal.example")
	// A registrant holding the same email is not counted, so it neither
	// captures the owner nor makes the owner ambiguous.
	s.seedPerson(t, "agent-registrant", "dana", "password", "dana.whitfield@harborlegal.example", "dana.whitfield@harborlegal.example")

	got, err := newTestAssertedSignInWith(s, allowlisted(false)).SignIn(context.Background(), dana("apple", appleSub))
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
		t.Run(name+" alone proves no owner, so the sign-in is refused", func(t *testing.T) {
			s := newMemoryAuthStore()
			s.seedPerson(t, "agent-dana-old", "Dana Whitfield", "google", googleSub, "dana.whitfield@harborlegal.example")
			off(t, s)

			_, err := newTestAssertedSignIn(s, true).SignIn(context.Background(), dana("apple", appleSub))
			requireUnprovenOwner(t, s, err, "apple", appleSub)
		})
		t.Run(name+" does not make the owner ambiguous", func(t *testing.T) {
			s := newMemoryAuthStore()
			s.seedPerson(t, "agent-dana-old", "Dana Whitfield", "google", googleSub, "dana.whitfield@harborlegal.example")
			s.seedPerson(t, "agent-dana", "Dana Whitfield", "google", ownerSub, "dana.whitfield@harborlegal.example")
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
		cfg.Allowlisted = true
		cfg.PasswordOwnersProven = true
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
				cfg.Allowlisted = c.allowlist
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
		cfg.Allowlisted = true
		cfg.PasswordOwnersProven = true
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

func TestAssertedSignInMatchesAnOwnersEmailFoldingOnlyASCIICapitals(t *testing.T) {
	cases := map[string]struct {
		stored, claimed string
		linked          bool
	}{
		"ASCII capitals":                         {"dana.whitfield@harborlegal.example", "DANA.Whitfield@HarborLegal.example", true},
		"the same accented capital":              {"ÉLISE.MARTIN@harborlegal.example", "Élise.martin@harborlegal.example", true},
		"an accented capital and its lower case": {"élise.martin@harborlegal.example", "Élise.martin@harborlegal.example", false},
		"the Kelvin sign for a k":                {"karl.berg@harborlegal.example", "Karl.berg@harborlegal.example", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newMemoryAuthStore()
			s.seedPerson(t, "agent-owner", "Owner", "google", googleSub, c.stored)

			identity := AssertedIdentity{Provider: "apple", Subject: appleSub, Email: c.claimed, Name: "Owner"}
			got, err := newTestAssertedSignIn(s, true).SignIn(context.Background(), identity)
			if err != nil {
				t.Fatalf("SignIn: %v", err)
			}
			if linked := !got.NewAccount && got.Agent.GetID() == "agent-owner"; linked != c.linked {
				t.Fatalf("%q against a stored %q: linked = %v, want %v", c.claimed, c.stored, linked, c.linked)
			}
		})
	}
}

// failingEventStore refuses every append.
type failingEventStore struct{ *esinfra.MemoryStore }

func (failingEventStore) Append(context.Context, string, int, ...esdomain.EventEnvelope[any]) error {
	return errors.New("event store unavailable")
}

func TestAssertedSignInRecordsALinkedCredentialsCreationOnce(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-dana", "Dana Whitfield", "google", googleSub, "dana.whitfield@harborlegal.example")
	events := esinfra.NewMemoryStore()
	svc := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) {
		cfg.Allowlisted = true
		cfg.EventStore = events
	})

	got, err := svc.SignIn(context.Background(), dana("apple", appleSub))
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if got.NewAccount || got.Agent.GetID() != "agent-dana" {
		t.Fatalf("want a link to agent-dana: agent=%s new=%v", got.Agent.GetID(), got.NewAccount)
	}
	linked := s.credentialFor("apple", appleSub)
	if linked == nil {
		t.Fatalf("the linked credential was not saved")
	}
	if ids := events.GetAllAggregateIDs(); len(ids) != 1 || ids[0] != linked.GetID() {
		t.Fatalf("recorded events for %v, want only the linked credential %s", ids, linked.GetID())
	}
	recorded, err := events.GetEvents(context.Background(), linked.GetID())
	if err != nil || len(recorded) != 1 {
		t.Fatalf("the linked credential has %d events (err %v), want its one creation", len(recorded), err)
	}
}

func TestAssertedSignInThatLosesALinkRaceRecordsAndLogsNothing(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-dana", "Dana Whitfield", "google", googleSub, "dana.whitfield@harborlegal.example")
	// Another process links the same identity between this sign-in's look-up
	// and its save, so the save meets the unique (provider, subject) index.
	s.beforeSave = func() {
		s.seedCredentialLocked(t, "agent-dana", "apple", appleSub, "dana.whitfield@harborlegal.example")
	}
	events := esinfra.NewMemoryStore()
	logs := &signInLogs{}
	svc := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) {
		cfg.Allowlisted = true
		cfg.EventStore = events
		cfg.Logger = logs
	})

	got, err := svc.SignIn(context.Background(), dana("apple", appleSub))
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if got.NewAccount || got.Agent.GetID() != "agent-dana" || s.createCount() != 0 {
		t.Fatalf("want the credential the winner stored: agent=%s new=%v creates=%d", got.Agent.GetID(), got.NewAccount, s.createCount())
	}
	if ids := events.GetAllAggregateIDs(); len(ids) != 0 {
		t.Fatalf("a link that lost the race recorded events for %v", ids)
	}
	if logged := logs.text(); logged != "" {
		t.Fatalf("a link this sign-in did not make was logged:\n%s", logged)
	}
}

func TestAssertedSignInRecordsNoEventForALinkThatCannotBeSaved(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-dana", "Dana Whitfield", "google", googleSub, "dana.whitfield@harborlegal.example")
	s.saveErr = errors.New("database unavailable")
	events := esinfra.NewMemoryStore()
	svc := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) {
		cfg.Allowlisted = true
		cfg.EventStore = events
	})

	_, err := svc.SignIn(context.Background(), dana("apple", appleSub))
	if err == nil || errors.Is(err, ErrAmbiguousOwner) {
		t.Fatalf("err = %v, want the save failure", err)
	}
	if ids := events.GetAllAggregateIDs(); len(ids) != 0 {
		t.Fatalf("a link that was never saved recorded events for %v", ids)
	}
	if s.createCount() != 0 {
		t.Fatalf("a failed link fell through to creating a person")
	}
}

func TestAssertedSignInFailsWhenALinkedCredentialsCreationCannotBeRecorded(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-dana", "Dana Whitfield", "google", googleSub, "dana.whitfield@harborlegal.example")
	svc := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) {
		cfg.Allowlisted = true
		cfg.EventStore = failingEventStore{esinfra.NewMemoryStore()}
	})

	_, err := svc.SignIn(context.Background(), dana("apple", appleSub))
	if err == nil || errors.Is(err, ErrAmbiguousOwner) {
		t.Fatalf("err = %v, want the event store failure", err)
	}
	if s.createCount() != 0 {
		t.Fatalf("a failed link fell through to creating a person")
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

// Owner binding runs with or without an allowlist, so sign-ins for one email
// are serialized either way.
func TestAssertedSignInConcurrentFirstSignInsFromTwoProvidersLeaveOneOwner(t *testing.T) {
	for _, allowlist := range []bool{true, false} {
		t.Run(fmt.Sprintf("allowlist set %v", allowlist), func(t *testing.T) {
			s := newMemoryAuthStore()
			s.window = 20 * time.Millisecond
			svc := newTestAssertedSignIn(s, allowlist)

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
		})
	}
}

// A request queued behind a slow sign-in for the same identity or the same email
// must not stay blocked after it ends, and must create nobody.
func TestAssertedSignInQueuedBehindAHeldKeyEndsWithItsRequest(t *testing.T) {
	for name, queued := range map[string]AssertedIdentity{
		"the identity key": dana("google", googleSub),
		"the email key":    dana("apple", appleSub),
	} {
		t.Run(name, func(t *testing.T) {
			s := newMemoryAuthStore()
			emails := newPausingEmails(storeEmails{s: s})
			svc := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) { cfg.Emails = emails })
			proceed := sync.OnceFunc(func() { close(emails.proceed) })
			t.Cleanup(proceed)

			firstDone := make(chan signInOutcome, 1)
			go func() {
				r, err := svc.SignIn(context.Background(), dana("google", googleSub))
				firstDone <- signInOutcome{r, err}
			}()
			select {
			case <-emails.paused:
			case o := <-firstDone:
				t.Fatalf("the first sign-in finished (%v) before it reached its pause", o.err)
			case <-time.After(5 * time.Second):
				t.Fatal("the first sign-in never reached its pause")
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			queuedDone := make(chan signInOutcome, 1)
			go func() {
				r, err := svc.SignIn(ctx, queued)
				queuedDone <- signInOutcome{r, err}
			}()
			// Time to join the queue. A cancel that lands before it does must
			// give the same answer, so this only makes the test exercise the wait.
			time.Sleep(20 * time.Millisecond)
			cancel()

			select {
			case o := <-queuedDone:
				if !errors.Is(o.err, context.Canceled) {
					t.Fatalf("the queued sign-in returned (reached %s, err %v), want its request's cancellation", agentIDOf(o.result), o.err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("the queued sign-in still waits behind the held key after its request ended")
			}

			proceed()
			select {
			case o := <-firstDone:
				if o.err != nil {
					t.Fatalf("the first sign-in: %v", o.err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the first sign-in never finished, so the queued one left a key held")
			}
			if s.createCount() != 1 {
				t.Fatalf("created %d people, want only the first sign-in's person", s.createCount())
			}
			if s.credentialFor("apple", appleSub) != nil {
				t.Fatal("the ended sign-in linked its identity")
			}
		})
	}
}

// A request that has already ended holds nothing and creates nobody, even when
// every key is free.
func TestAssertedSignInForAnEndedRequestCreatesNobody(t *testing.T) {
	s := newMemoryAuthStore()
	svc := newTestAssertedSignIn(s, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.SignIn(ctx, dana("google", googleSub))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the request's cancellation", err)
	}
	if s.createCount() != 0 {
		t.Fatalf("created %d people for an ended request, want 0", s.createCount())
	}
	// The keys are free again: the next sign-in for the same identity and email
	// goes through.
	if _, err := svc.SignIn(context.Background(), dana("google", googleSub)); err != nil {
		t.Fatalf("a sign-in after the ended one: %v", err)
	}
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
