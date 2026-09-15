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

// doorSub is the subject the door gives the person who signed up to it with
// ops@harborlegal.example and a password.
const doorSub = "2VhQ7kX9mT4rY8nL1pW6zC3dF5b"

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

// requireLinkedTo checks that a sign-in reached owner by linking the arriving
// identity to it: nobody created, and the identity stored against owner.
func requireLinkedTo(t *testing.T, s *memoryAuthStore, got AssertedSignInResult, err error, owner string, id AssertedIdentity) {
	t.Helper()
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if got.NewAccount || got.Agent.GetID() != owner || s.createCount() != 0 {
		t.Fatalf("want a link to %s: agent=%s new=%v creates=%d", owner, got.Agent.GetID(), got.NewAccount, s.createCount())
	}
	if linked := s.credentialFor(id.Provider, id.Subject); linked == nil || linked.AgentID() != owner {
		t.Fatalf("the %s identity is not stored against %s: %v", id.Provider, owner, linked)
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

// Only google, apple, a password under the opt-in, and a door credential for
// an arriving google or apple identity prove an owner. Every other provider
// string proves nothing, alone or beside a real owner, and is never counted
// toward ambiguous-owner. The door's key is tested on its own below, because
// whether it proves depends on the identity arriving.
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

// A door password identity and a Google or Apple identity that hold the same
// email are one person, in both directions, with or without an allowlist
// (decision wm-vvi6t). A door credential means the issuer vouches for the
// email, so for an arriving google or apple identity that credential proves
// who owns it.
func TestAssertedSignInJoinsADoorIdentityAndAGoogleOrAppleIdentityForOneEmail(t *testing.T) {
	cases := map[string]struct {
		heldProvider, heldSub string
		arriving              AssertedIdentity
	}{
		"(a) a door identity after google":     {"google", ownerSub, harborOps("door", doorSub)},
		"(a) a door identity after apple":      {"apple", appleSub, harborOps("door", doorSub)},
		"(b) a google identity after the door": {"door", doorSub, harborOps("google", ownerSub)},
		"(b) an apple identity after the door": {"door", doorSub, harborOps("apple", appleSub)},
	}
	for name, c := range cases {
		for _, allowlist := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s, allowlist set %v", name, allowlist), func(t *testing.T) {
				s := newMemoryAuthStore()
				s.seedPerson(t, "agent-ops", "Harbor Ops", c.heldProvider, c.heldSub, "ops@harborlegal.example")

				got, err := newTestAssertedSignIn(s, allowlist).SignIn(context.Background(), c.arriving)
				requireLinkedTo(t, s, got, err, "agent-ops", c.arriving)
			})
		}
	}
}

// recreatedSub is the subject the door gives ops@harborlegal.example after an
// operator re-created that person at the door.
const recreatedSub = "2Vj4nR8wQ1tZ6yK3mP9xB5cL7hD"

// Two door identities for one email are not one person. The door sends one
// subject for each person it holds, so a second door subject for an email
// comes only from an operator re-creating the person at the door. While an
// active door credential of an active person holds the email, the new door
// identity is refused, with or without an allowlist, and nothing is linked or
// created. That holds whatever else the person holds: a google or apple
// credential beside the door one proves the email, but it does not make a
// second door subject the same person as the first.
func TestAssertedSignInNeverJoinsTwoDoorIdentitiesForOneEmail(t *testing.T) {
	holdings := map[string]func(t *testing.T, s *memoryAuthStore){
		"the person holds only the door": func(t *testing.T, s *memoryAuthStore) {
			s.seedPerson(t, "agent-ops", "Harbor Ops", "door", doorSub, "ops@harborlegal.example")
		},
		"the person holds the door, then google": func(t *testing.T, s *memoryAuthStore) {
			s.seedPerson(t, "agent-ops", "Harbor Ops", "door", doorSub, "ops@harborlegal.example")
			s.seedCredential(t, "agent-ops", "google", ownerSub, "ops@harborlegal.example")
		},
		"the person holds google, then the door": func(t *testing.T, s *memoryAuthStore) {
			s.seedPerson(t, "agent-ops", "Harbor Ops", "google", ownerSub, "ops@harborlegal.example")
			s.seedCredential(t, "agent-ops", "door", doorSub, "ops@harborlegal.example")
		},
	}
	for name, seed := range holdings {
		for _, allowlist := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s, allowlist set %v", name, allowlist), func(t *testing.T) {
				s := newMemoryAuthStore()
				seed(t, s)

				_, err := newTestAssertedSignIn(s, allowlist).SignIn(context.Background(), harborOps("door", recreatedSub))
				requireUnprovenOwner(t, s, err, "door", recreatedSub)
			})
		}
	}
}

// The refusal of a second door subject is logged like every unproven-owner
// refusal: one error line that names the people holding the email and the
// kinds of credential they hold, and never the subject or the email.
func TestAssertedSignInLogsASecondDoorSubjectAsAnUnprovenOwner(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-ops", "Harbor Ops", "door", doorSub, "ops@harborlegal.example")
	s.seedCredential(t, "agent-ops", "google", ownerSub, "ops@harborlegal.example")
	logs := &signInLogs{}
	svc := newTestAssertedSignInWith(s, func(cfg *AssertedSignInConfig) { cfg.Logger = logs })

	_, err := svc.SignIn(context.Background(), harborOps("door", recreatedSub))
	requireUnprovenOwner(t, s, err, "door", recreatedSub)
	lines := logs.all()
	if len(lines) != 1 || lines[0].level != "error" {
		t.Fatalf("expected one error line for the refusal, got:\n%s", logs.text())
	}
	if reason := lines[0].field("reason"); reason != ReasonUnprovenOwner {
		t.Fatalf("reason = %v, want %q", reason, ReasonUnprovenOwner)
	}
	requireIdentityFields(t, lines[0], "door", recreatedSub, "ops@harborlegal.example", "agent-ops")
	if kinds := lines[0].field("matched_providers"); kinds != "door,google" {
		t.Fatalf("matched_providers = %v, want door,google", kinds)
	}
	requireNoRawIdentity(t, logs, recreatedSub, "ops@harborlegal.example")
}

// Only an active door credential of an active person refuses a second door
// subject. One that was turned off, or whose person was turned off, holds the
// email for nobody, so the new door identity is linked to the person whose
// google credential proves the email.
func TestAssertedSignInLinksANewDoorSubjectPastATurnedOffDoorCredential(t *testing.T) {
	t.Run("the door credential is turned off", func(t *testing.T) {
		s := newMemoryAuthStore()
		s.seedPerson(t, "agent-ops", "Harbor Ops", "google", ownerSub, "ops@harborlegal.example")
		s.seedCredential(t, "agent-ops", "door", doorSub, "ops@harborlegal.example")
		s.deactivateCredential(t, "door", doorSub)
		arriving := harborOps("door", recreatedSub)

		got, err := newTestAssertedSignIn(s, false).SignIn(context.Background(), arriving)
		requireLinkedTo(t, s, got, err, "agent-ops", arriving)
	})
	t.Run("the person holding the door credential is turned off", func(t *testing.T) {
		s := newMemoryAuthStore()
		s.seedPerson(t, "agent-ops", "Harbor Ops", "google", ownerSub, "ops@harborlegal.example")
		s.seedPerson(t, "agent-door", "ops", "door", doorSub, "ops@harborlegal.example")
		s.deactivateAgent(t, "agent-door")
		arriving := harborOps("door", recreatedSub)

		got, err := newTestAssertedSignIn(s, false).SignIn(context.Background(), arriving)
		requireLinkedTo(t, s, got, err, "agent-ops", arriving)
	})
}

// A door credential proves its email for a google or apple identity only, and
// for those it counts like any proving credential.
func TestAssertedSignInCountsADoorCredentialOnlyForAGoogleOrAppleIdentity(t *testing.T) {
	t.Run("beside a google owner, it makes an arriving apple identity ambiguous", func(t *testing.T) {
		s := newMemoryAuthStore()
		s.seedPerson(t, "agent-ops", "Harbor Ops", "google", ownerSub, "ops@harborlegal.example")
		s.seedPerson(t, "agent-door", "ops", "door", doorSub, "ops@harborlegal.example")

		_, err := newTestAssertedSignIn(s, false).SignIn(context.Background(), harborOps("apple", appleSub))
		if !errors.Is(err, ErrAmbiguousOwner) {
			t.Fatalf("err = %v, want ErrAmbiguousOwner", err)
		}
		if s.createCount() != 0 || s.credentialFor("apple", appleSub) != nil {
			t.Fatalf("an ambiguous owner still created something: creates=%d", s.createCount())
		}
	})
	t.Run("beside a google owner, it refuses an arriving door identity", func(t *testing.T) {
		for _, allowlist := range []bool{true, false} {
			t.Run(fmt.Sprintf("allowlist set %v", allowlist), func(t *testing.T) {
				s := newMemoryAuthStore()
				s.seedPerson(t, "agent-ops", "Harbor Ops", "google", ownerSub, "ops@harborlegal.example")
				s.seedPerson(t, "agent-door", "ops", "door", doorSub, "ops@harborlegal.example")

				_, err := newTestAssertedSignIn(s, allowlist).SignIn(context.Background(), harborOps("door", recreatedSub))
				requireUnprovenOwner(t, s, err, "door", recreatedSub)
			})
		}
	})
	t.Run("alone, it proves nothing for an arriving netsuite identity", func(t *testing.T) {
		s := newMemoryAuthStore()
		s.seedPerson(t, "agent-door", "ops", "door", doorSub, "ops@harborlegal.example")

		_, err := newTestAssertedSignIn(s, false).SignIn(context.Background(), harborOps("netsuite", "4812337"))
		requireUnprovenOwner(t, s, err, "netsuite", "4812337")
	})
	t.Run("turned off, it proves nothing for an arriving google identity", func(t *testing.T) {
		s := newMemoryAuthStore()
		s.seedPerson(t, "agent-door", "ops", "door", doorSub, "ops@harborlegal.example")
		s.deactivateCredential(t, "door", doorSub)

		_, err := newTestAssertedSignIn(s, false).SignIn(context.Background(), harborOps("google", ownerSub))
		requireUnprovenOwner(t, s, err, "google", ownerSub)
	})
}

// A core older than wm-6lx6z linked nothing on an instance with no allowlist,
// so an owner who signed in there with two methods became two people. The
// ADR's upgrade note tells an operator what a new identity for that email
// meets after the upgrade; these are those outcomes.
func TestAssertedSignInMeetsAnOwnerAnOlderCoreSplitIntoTwoPeople(t *testing.T) {
	type outcome int
	const (
		ambiguous outcome = iota
		unproven
		linkedToGoogle
	)
	doorAndGoogle := func(t *testing.T, s *memoryAuthStore) {
		s.seedPerson(t, "agent-google", "Harbor Ops", "google", ownerSub, "ops@harborlegal.example")
		s.seedPerson(t, "agent-door", "ops", "door", doorSub, "ops@harborlegal.example")
	}
	googleAndApple := func(t *testing.T, s *memoryAuthStore) {
		s.seedPerson(t, "agent-google", "Harbor Ops", "google", ownerSub, "ops@harborlegal.example")
		s.seedPerson(t, "agent-apple", "ops", "apple", appleSub, "ops@harborlegal.example")
	}
	cases := map[string]struct {
		seed     func(t *testing.T, s *memoryAuthStore)
		arriving AssertedIdentity
		want     outcome
	}{
		"door and google people, a new google identity":  {doorAndGoogle, harborOps("google", googleSub), ambiguous},
		"door and google people, a new apple identity":   {doorAndGoogle, harborOps("apple", appleSub), ambiguous},
		"door and google people, a netsuite identity":    {doorAndGoogle, harborOps("netsuite", "4812337"), linkedToGoogle},
		"door and google people, a new door identity":    {doorAndGoogle, harborOps("door", recreatedSub), unproven},
		"google and apple people, a new google identity": {googleAndApple, harborOps("google", googleSub), ambiguous},
		"google and apple people, a netsuite identity":   {googleAndApple, harborOps("netsuite", "4812337"), ambiguous},
		"google and apple people, a door identity":       {googleAndApple, harborOps("door", doorSub), ambiguous},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newMemoryAuthStore()
			c.seed(t, s)

			got, err := newTestAssertedSignIn(s, false).SignIn(context.Background(), c.arriving)
			switch c.want {
			case ambiguous:
				if !errors.Is(err, ErrAmbiguousOwner) {
					t.Fatalf("err = %v, want ErrAmbiguousOwner", err)
				}
				if s.createCount() != 0 || s.credentialFor(c.arriving.Provider, c.arriving.Subject) != nil {
					t.Fatalf("an ambiguous owner still created or linked something: creates=%d", s.createCount())
				}
			case unproven:
				requireUnprovenOwner(t, s, err, c.arriving.Provider, c.arriving.Subject)
			case linkedToGoogle:
				requireLinkedTo(t, s, got, err, "agent-google", c.arriving)
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
		cfg.Allowlisted = true
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
		cfg.Allowlisted = true
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

// Owner binding runs with or without an allowlist (decision wm-vvi6t), so a
// credential that proves nothing refuses the sign-in on an instance with no
// allowlist too. Creating could leave the owner in a second, empty account,
// and linking could hand the identity to whoever wrote the email.
func TestAssertedSignInRefusesAnUnprovenEmailWithoutTheAllowlistToo(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-invited", "ops", "invite", "2VbXk9hQ4mT7rY1pL8nW3cZ6dF0", "ops@harborlegal.example")

	_, err := newTestAssertedSignIn(s, false).SignIn(context.Background(), harborOps("google", ownerSub))
	requireUnprovenOwner(t, s, err, "google", ownerSub)
}

// provideOver builds the AssertedSignIn the application wires, from cfg, over
// the memory store.
func provideOver(s *memoryAuthStore, cfg config.Config) *AssertedSignIn {
	return provideOverWithLock(s, cfg, nil)
}

// provideOverWithLock is provideOver with the sign-in lock the application
// wires beside it.
func provideOverWithLock(s *memoryAuthStore, cfg config.Config, lock repositories.SignInLock) *AssertedSignIn {
	return ProvideAssertedSignIn(struct {
		fx.In
		Config         config.Config
		Auth           authapp.AuthenticationService
		Credentials    authrepos.CredentialRepository
		Agents         authrepos.AgentRepository
		Emails         repositories.CredentialEmailQuery
		EventStore     esdomain.EventStore               `optional:"true"`
		Dispatcher     *esdomain.EventDispatcher         `optional:"true"`
		CredentialRows repositories.CredentialRowDeleter `optional:"true"`
		Lock           repositories.SignInLock           `optional:"true"`
		Logger         weosentities.Logger               `optional:"true"`
	}{
		Config: cfg, Auth: storeAuth{s: s}, Credentials: storeCredentials{s: s},
		Agents: storeAgents{s: s}, Emails: storeEmails{s: s}, Lock: lock,
	})
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

			got, err := provideOver(s, cfg).SignIn(context.Background(), harborOps("google", ownerSub))
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

// The instances behind the door set no OAUTH_ALLOWED_EMAILS. The service the
// application wires binds owners there too, so a Google identity arriving
// after the door reaches the person the door made, not a second, empty one.
func TestProvideAssertedSignInBindsOwnersWithoutAnAllowlist(t *testing.T) {
	s := newMemoryAuthStore()
	s.seedPerson(t, "agent-ops", "Harbor Ops", "door", doorSub, "ops@harborlegal.example")
	cfg := config.Default()
	cfg.OAuth.AllowedEmails = nil

	arriving := harborOps("google", ownerSub)
	got, err := provideOver(s, cfg).SignIn(context.Background(), arriving)
	requireLinkedTo(t, s, got, err, "agent-ops", arriving)
}
