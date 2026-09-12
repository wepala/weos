package application

import (
	"context"
	"errors"
	"fmt"
	"testing"

	weosentities "github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	esdomain "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"go.uber.org/fx"
)

// ownerSub is the subject Google gives the owner of ops@harborlegal.example.
const ownerSub = "117590246813570924368"

func harborOps(provider, sub string) AssertedIdentity {
	return AssertedIdentity{Provider: provider, Subject: sub, Email: "ops@harborlegal.example", Name: "Harbor Ops"}
}

// requireUnprovenOwner checks that a sign-in was refused as unproven-owner and
// left nothing behind: nobody created, and no credential for the identity.
func requireUnprovenOwner(t *testing.T, s *memoryAuthStore, err error, provider, sub string) {
	t.Helper()
	if !errors.Is(err, ErrUnprovenOwner) {
		t.Fatalf("err = %v, want ErrUnprovenOwner", err)
	}
	if s.createCount() != 0 {
		t.Fatalf("an unproven owner still created %d people", s.createCount())
	}
	if linked := s.credentialFor(provider, sub); linked != nil {
		t.Fatalf("an unproven owner was linked: the identity is stored against %s", linked.AgentID())
	}
}

// The capture the invite path allows. On an allowlisted instance with open
// registration, anyone can register, invite the owner's email from their own
// personal account, and accept that invite with no session. That leaves an
// active person holding an active invite credential for the owner's email
// before the owner's first sign-in through the door. The invite credential
// proves nothing, with or without the password opt-in, so the owner's sign-in
// is refused rather than handed to the inviter's person.
func TestAssertedSignInNeverHandsAnOwnersIdentityToAnInvitedPerson(t *testing.T) {
	for _, passwordOwners := range []bool{false, true} {
		t.Run(fmt.Sprintf("password owners proven %v", passwordOwners), func(t *testing.T) {
			s := newMemoryAuthStore()
			s.seedPerson(t, "agent-invited", "ops", "invite", "2VbXk9hQ4mT7rY1pL8nW3cZ6dF0", "ops@harborlegal.example")

			_, err := newTestAssertedSignInWith(s, allowlisted(passwordOwners)).SignIn(context.Background(), harborOps("google", ownerSub))
			requireUnprovenOwner(t, s, err, "google", ownerSub)
		})
	}
}

// Only google, apple and (under the opt-in) password prove an owner. Every
// other provider string proves nothing, alone or beside a real owner, and is
// never counted toward ambiguous-owner.
func TestAssertedSignInCountsNoProviderOutsideTheProvingList(t *testing.T) {
	for _, provider := range []string{"invite", "netsuite", "dev", "acme-sso"} {
		t.Run(provider+" alone", func(t *testing.T) {
			s := newMemoryAuthStore()
			s.seedPerson(t, "agent-holder", "ops", provider, "holder-"+provider, "ops@harborlegal.example")

			_, err := newTestAssertedSignInWith(s, allowlisted(true)).SignIn(context.Background(), harborOps("apple", appleSub))
			requireUnprovenOwner(t, s, err, "apple", appleSub)
		})
		t.Run(provider+" beside a google owner", func(t *testing.T) {
			s := newMemoryAuthStore()
			s.seedPerson(t, "agent-ops", "Harbor Ops", "google", ownerSub, "ops@harborlegal.example")
			s.seedPerson(t, "agent-holder", "ops", provider, "holder-"+provider, "ops@harborlegal.example")

			got, err := newTestAssertedSignInWith(s, allowlisted(true)).SignIn(context.Background(), harborOps("apple", appleSub))
			if err != nil {
				t.Fatalf("SignIn: %v (a credential that proves nothing was counted)", err)
			}
			if got.NewAccount || got.Agent.GetID() != "agent-ops" {
				t.Fatalf("want a link to agent-ops: agent=%s new=%v", got.Agent.GetID(), got.NewAccount)
			}
		})
	}
}

func TestAssertedSignInLinksToAnOwnerAGoogleOrAppleCredentialProves(t *testing.T) {
	cases := map[string]AssertedIdentity{
		"google": harborOps("apple", appleSub),
		"apple":  harborOps("google", googleSub),
	}
	for held, identity := range cases {
		t.Run(held, func(t *testing.T) {
			s := newMemoryAuthStore()
			s.seedPerson(t, "agent-ops", "Harbor Ops", held, "held-"+held, "ops@harborlegal.example")

			got, err := newTestAssertedSignIn(s, true).SignIn(context.Background(), identity)
			if err != nil {
				t.Fatalf("SignIn: %v", err)
			}
			if got.NewAccount || got.Agent.GetID() != "agent-ops" || s.createCount() != 0 {
				t.Fatalf("want a link to agent-ops: agent=%s new=%v creates=%d", got.Agent.GetID(), got.NewAccount, s.createCount())
			}
		})
	}
}

// Two people each holding a credential that proves the email are ambiguous;
// a third holding one that proves nothing adds nobody to the count or the line.
func TestAssertedSignInCountsOnlyProvingCredentialsTowardAnAmbiguousOwner(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-ops-google", "Harbor Ops", "google", ownerSub, "ops@harborlegal.example")
	s.seedPerson(t, "agent-ops-apple", "Harbor Ops", "apple", appleSub, "ops@harborlegal.example")
	s.seedPerson(t, "agent-invited", "ops", "invite", "2VbXk9hQ4mT7rY1pL8nW3cZ6dF0", "ops@harborlegal.example")
	logs := &signInLogs{}
	svc := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) {
		cfg.LinkByEmail = true
		cfg.Logger = logs
	})

	_, err := svc.SignIn(context.Background(), harborOps("google", googleSub))
	if !errors.Is(err, ErrAmbiguousOwner) {
		t.Fatalf("err = %v, want ErrAmbiguousOwner", err)
	}
	lines := logs.all()
	if len(lines) != 1 {
		t.Fatalf("expected one line for the refusal, got:\n%s", logs.text())
	}
	requireIdentityFields(t, lines[0], "google", googleSub, "ops@harborlegal.example", "agent-ops-apple,agent-ops-google")
}

func TestAssertedSignInLogsAnUnprovenOwnerWithEveryPersonHoldingTheEmail(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-registrant", "ops", "password", "ops@harborlegal.example", "ops@harborlegal.example")
	s.seedPerson(t, "agent-invited", "ops", "invite", "2VbXk9hQ4mT7rY1pL8nW3cZ6dF0", "Ops@HarborLegal.example")
	logs := &signInLogs{}
	svc := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) {
		cfg.LinkByEmail = true
		cfg.Logger = logs
	})

	_, err := svc.SignIn(context.Background(), harborOps("google", ownerSub))
	requireUnprovenOwner(t, s, err, "google", ownerSub)
	lines := logs.all()
	if len(lines) != 1 || lines[0].level != "error" {
		t.Fatalf("expected one error line for the refusal, got:\n%s", logs.text())
	}
	if reason := lines[0].field("reason"); reason != ReasonUnprovenOwner {
		t.Fatalf("reason = %v, want %q", reason, ReasonUnprovenOwner)
	}
	if ReasonUnprovenOwner != "unproven-owner" {
		t.Fatalf("ReasonUnprovenOwner = %q, want unproven-owner", ReasonUnprovenOwner)
	}
	requireIdentityFields(t, lines[0], "google", ownerSub, "ops@harborlegal.example", "agent-invited,agent-registrant")
	if kinds := lines[0].field("matched_providers"); kinds != "invite,password" {
		t.Fatalf("matched_providers = %v, want invite,password", kinds)
	}
	requireNoRawIdentity(t, logs, ownerSub, "ops@harborlegal.example")
}

// An instance with no allowlist links nothing, so a credential that proves
// nothing is not a reason to refuse there either: the sign-in creates, as the
// OAuth callback does.
func TestAssertedSignInWithoutTheAllowlistRefusesNothingForAnUnprovenEmail(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-invited", "ops", "invite", "2VbXk9hQ4mT7rY1pL8nW3cZ6dF0", "ops@harborlegal.example")

	got, err := newTestAssertedSignIn(s, false).SignIn(context.Background(), harborOps("google", ownerSub))
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if !got.NewAccount || got.Agent.GetID() == "agent-invited" {
		t.Fatalf("want a new person: agent=%s new=%v", got.Agent.GetID(), got.NewAccount)
	}
}

// Whether a password credential proves an owner is the operator's opt-in
// alone. PASSWORD_REGISTRATION_ENABLED decides nothing: turning registration
// off does not remove the accounts it let anyone make.
func TestProvideAssertedSignInTakesPasswordOwnersFromTheOptInAlone(t *testing.T) {
	cases := map[string]struct{ registration, optIn, linked bool }{
		"registration off, no opt-in": {false, false, false},
		"registration on, no opt-in":  {true, false, false},
		"registration off, opt-in":    {false, true, true},
		"registration on, opt-in":     {true, true, true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newMemoryAuthStore()
			s.seedPerson(t, "agent-ops", "ops", "password", "ops@harborlegal.example", "ops@harborlegal.example")
			cfg := config.Default()
			cfg.OAuth.AllowedEmails = []string{"ops@harborlegal.example"}
			cfg.PasswordRegistrationEnabled = c.registration
			cfg.TrustedIssuer.LinkPasswordOwners = c.optIn

			svc := ProvideAssertedSignIn(struct {
				fx.In
				Config      config.Config
				Auth        authapp.AuthenticationService
				Credentials authrepos.CredentialRepository
				Agents      authrepos.AgentRepository
				Emails      repositories.CredentialEmailQuery
				EventStore  esdomain.EventStore       `optional:"true"`
				Dispatcher  *esdomain.EventDispatcher `optional:"true"`
				Logger      weosentities.Logger       `optional:"true"`
			}{
				Config: cfg, Auth: storeAuth{s: s}, Credentials: storeCredentials{s: s},
				Agents: storeAgents{s: s}, Emails: storeEmails{s: s},
			})

			got, err := svc.SignIn(context.Background(), harborOps("google", ownerSub))
			if !c.linked {
				requireUnprovenOwner(t, s, err, "google", ownerSub)
				return
			}
			if err != nil || got.NewAccount || got.Agent.GetID() != "agent-ops" {
				t.Fatalf("want a link to agent-ops: agent=%v new=%v err=%v", got.Agent, got.NewAccount, err)
			}
		})
	}
}
