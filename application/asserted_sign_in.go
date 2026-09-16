// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

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

	weosentities "github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	"github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	esapp "github.com/akeemphilbert/pericarp/pkg/eventsourcing/application"
	esdomain "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/segmentio/ksuid"
	"go.uber.org/fx"
)

// ErrAmbiguousOwner is returned when owner binding would link an identity to
// an email that more than one person holds. Picking one of them would sign a
// person in to someone else's data, so nothing is linked and nothing is made.
var ErrAmbiguousOwner = errors.New("more than one person holds a credential for the asserted email")

// ReasonAmbiguousOwner is the machine-readable reason an ErrAmbiguousOwner
// refusal is logged and answered under.
const ReasonAmbiguousOwner = "ambiguous-owner"

// ErrUnprovenOwner is returned when owner binding finds credentials holding
// the asserted email but none of them proves who owns it: every one is of a
// kind whose email nobody verified, is turned off, or belongs to a person who
// is turned off or gone. Linking to one could hand the identity to whoever
// wrote that email, and creating a person could leave the owner in a second,
// empty account, so nothing is linked and nothing is made. An operator decides.
var ErrUnprovenOwner = errors.New("credentials hold the asserted email, but none of them proves who owns it")

// ReasonUnprovenOwner is the machine-readable reason an ErrUnprovenOwner
// refusal is logged and answered under.
const ReasonUnprovenOwner = "unproven-owner"

// ownerProvingProviders are the credential providers whose email says who owns
// it, whatever identity arrives, because the provider verified the address
// before the credential was written. The list is explicit on purpose: a
// provider string that is not on it — invite, netsuite, a development
// provider, or one a downstream binary adds — never proves an owner. An invite
// credential's email is whatever the inviter and the accepter typed, and
// NetSuite reports an email its account administrator sets, with no
// verification flag. A password credential proves an owner only under the
// operator's opt-in, and a door credential only for the arriving identities
// doorCredentialProvesOwnerFor names (see provesOwnership).
var ownerProvingProviders = map[string]bool{
	OAuthProviderGoogle: true,
	OAuthProviderApple:  true,
}

// doorCredentialProvesOwnerFor are the providers of an arriving identity for
// which a door credential proves who owns its email. A door credential means
// the issuer vouches for its email: the mini-me door proves the address with a
// code sent to the mailbox when a person signs up, and the door's operator also
// writes demo people and owner people directly, with no code. An issuer must
// only write door credentials for addresses it controls or has proved. On that
// word, a door password identity and a Google or Apple identity that hold the
// same email are one person, in either order (decision wm-vvi6t). A door credential proves nothing
// for another door identity: the door sends one subject for each person it
// holds, so a second door subject for an email comes only from an operator
// re-creating the person, and that person is not joined to the first. While an
// active door credential of an active person holds the email, ownerOf refuses
// the second door subject as ErrUnprovenOwner, even when that person also
// holds a google or apple credential that proves the email.
var doorCredentialProvesOwnerFor = map[string]bool{
	OAuthProviderGoogle: true,
	OAuthProviderApple:  true,
}

// AssertedIdentity is the person a trusted issuer's accepted assertion names.
type AssertedIdentity struct {
	// Provider is the key of the provider that verified the person: a registry
	// key such as "google", or OAuthProviderDoor for an identity the door owns.
	Provider string
	// Subject is that provider's stable id for the person.
	Subject string
	// Email is the person's door-account email.
	Email string
	// Name is the name a person created by this sign-in is given. The caller
	// fills it in; it is never empty.
	Name string
}

// AssertedSignInResult is who an asserted sign-in reached.
type AssertedSignInResult struct {
	Agent      *entities.Agent
	Credential *entities.Credential
	// Account is the account the session acts in; nil when the person has no
	// active account left.
	Account *entities.Account
	// NewAccount is true only when this sign-in created the person. A sign-in
	// that reached a person by their identity, or linked a new identity to an
	// owner, created nobody.
	NewAccount bool
}

// AssertedSignInConfig wires an AssertedSignIn.
type AssertedSignInConfig struct {
	Auth        authapp.AuthenticationService
	Credentials authrepos.CredentialRepository
	Agents      authrepos.AgentRepository
	Emails      repositories.CredentialEmailQuery
	// EventStore and Dispatcher record a linked credential's creation, as
	// pericarp records every credential it makes. Optional, as they are to
	// pericarp: with no store, the credential is saved to its projection only.
	EventStore esdomain.EventStore
	Dispatcher *esdomain.EventDispatcher
	// CredentialRows takes back the row of a linked credential whose creation
	// event could not be committed (see link). Optional; without it such a row
	// is left behind, and a repair line names it.
	CredentialRows repositories.CredentialRowDeleter
	// Lock serializes the sign-ins for one identity, and for one email, across
	// every process that shares the database, from the owner look-up through the
	// link or the create. The application's AuthenticationService holds the same
	// lock around the OAuth callbacks' FindOrCreateAgent (see
	// newAccountSignalService). Optional; without it assertions are serialized
	// in process only, and two replicas that both find no holder for an email
	// each create a person.
	Lock repositories.SignInLock
	// Allowlisted says the instance has an identity allowlist
	// (OAUTH_ALLOWED_EMAILS). The allowlist is enforced before SignIn runs.
	// Owner binding runs whether or not it is set: only a credential whose
	// email was proved can say who owns it (see provesOwnership), which is as
	// true on an instance with no allowlist, as the instances behind the door
	// run (decision wm-vvi6t). Allowlisted decides only how a person a sign-in
	// creates is logged (see logCreated).
	Allowlisted bool
	// PasswordOwnersProven lets a password credential prove who owns its
	// email (TRUSTED_ISSUER_LINK_PASSWORD_OWNERS). Off by default: nothing
	// verifies a password credential's email, so while it is off a password
	// credential is neither linked to nor counted, and otherwise whoever once
	// registered the owner's email would be handed the owner's identity and
	// keep the password. An operator turns it on for an instance whose
	// password accounts they made themselves. Credentials from google and
	// apple, and a door credential for a google or apple identity, prove an
	// owner either way.
	PasswordOwnersProven bool
	// Logger receives one line for each link, each person a sign-in creates,
	// each ambiguous-owner or unproven-owner refusal, and each link that could
	// not be recorded. Optional; without it nothing is logged.
	Logger weosentities.Logger
}

// AssertedSignIn decides whom a trusted issuer's accepted assertion signs in.
// It resolves the person by (provider, subject) the way the OAuth callback
// does, through FindOrCreateAgent. An identity it has never seen is linked to
// the one person already holding a credential that proves the same email,
// instead of becoming a second person, whether or not the instance has an
// allowlist. See docs/decisions/trusted-issuer-login-assertion.md, "Owner
// binding".
//
// It is safe for concurrent use. Sign-ins for one identity, and sign-ins for
// one email, are serialized in process, and across the processes sharing the
// database when a Lock is configured, so two first sign-ins arriving together
// leave one person, on one replica or on two.
type AssertedSignIn struct {
	cfg        AssertedSignInConfig
	identities keyedMutex
	emails     keyedMutex
}

// NewAssertedSignIn builds an AssertedSignIn.
func NewAssertedSignIn(cfg AssertedSignInConfig) *AssertedSignIn {
	if cfg.Logger == nil {
		cfg.Logger = discardSignInLogs{}
	}
	return &AssertedSignIn{cfg: cfg}
}

// ProvideAssertedSignIn builds the AssertedSignIn the application wires, with
// Allowlisted set exactly when the instance has an identity allowlist, and
// password credentials proving an owner exactly when the operator opted in
// with TRUSTED_ISSUER_LINK_PASSWORD_OWNERS. Owner binding runs either way.
func ProvideAssertedSignIn(params struct {
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
}) *AssertedSignIn {
	return NewAssertedSignIn(AssertedSignInConfig{
		Auth:                 params.Auth,
		Credentials:          params.Credentials,
		Agents:               params.Agents,
		Emails:               params.Emails,
		EventStore:           params.EventStore,
		Dispatcher:           params.Dispatcher,
		CredentialRows:       params.CredentialRows,
		Lock:                 params.Lock,
		Logger:               params.Logger,
		Allowlisted:          len(params.Config.OAuth.AllowedEmails) > 0,
		PasswordOwnersProven: params.Config.TrustedIssuer.LinkPasswordOwners,
	})
}

// SignIn resolves, links or creates the person the identity names.
func (s *AssertedSignIn) SignIn(ctx context.Context, id AssertedIdentity) (AssertedSignInResult, error) {
	// Always the identity lock first and the email lock second, and never more
	// than one of each, so two sign-ins can never wait on each other in a
	// circle. The in-process locks come before the cross-process ones, which
	// take the same keys in the same order. A request that ends while it waits
	// holds nothing and never reaches the owner look-up.
	unlockIdentity, err := s.identities.lock(ctx, id.Provider+"\x00"+id.Subject)
	if err != nil {
		return AssertedSignInResult{}, fmt.Errorf("wait for the sign-in lock: %w", err)
	}
	defer unlockIdentity()
	// The same fold the credential query compares under, so the email lock
	// serializes exactly the sign-ins that could reach one owner.
	email := repositories.FoldCredentialEmail(id.Email)
	if email != "" {
		unlockEmail, err := s.emails.lock(ctx, email)
		if err != nil {
			return AssertedSignInResult{}, fmt.Errorf("wait for the sign-in lock: %w", err)
		}
		defer unlockEmail()
	}
	if s.cfg.Lock != nil {
		keys := signInLockKeys(id.Provider, id.Subject, email)
		// Held until SignIn returns, so another replica reads the owner look-up
		// only after this sign-in's link or create is written.
		release, err := s.cfg.Lock.Hold(ctx, keys...)
		if err != nil {
			return AssertedSignInResult{}, fmt.Errorf("hold the sign-in lock: %w", err)
		}
		defer release()
		// The application's AuthenticationService takes the same keys around
		// FindOrCreateAgent for the OAuth callbacks; below, it must not wait on
		// the keys this sign-in already holds.
		ctx = withSignInKeysHeld(ctx, keys)
	}

	existing, err := s.cfg.Credentials.FindByProvider(ctx, id.Provider, id.Subject)
	if err != nil {
		return AssertedSignInResult{}, fmt.Errorf("look up the credential for provider %s: %w", id.Provider, err)
	}
	known := existing != nil
	if !known && email != "" {
		owner, err := s.ownerOf(ctx, id, email)
		if err != nil {
			return AssertedSignInResult{}, err
		}
		if owner != "" {
			linked, err := s.link(ctx, owner, id)
			if err != nil {
				return AssertedSignInResult{}, err
			}
			if linked {
				// A link attaches a new way in to an existing person, so it is
				// the one sign-in outcome an operator must be able to find later.
				s.cfg.Logger.Info(ctx, "trusted issuer sign-in linked an identity the instance had not seen to the person holding its email",
					identityFields(id, email, owner)...)
			}
			// Linked here or by the process that won the race: either way the
			// identity now has a credential, and this sign-in created nobody.
			known = true
		}
	}

	// With a credential for the identity in the store — found, or just linked —
	// FindOrCreateAgent takes its returning path: the credential's person, the
	// account that person acts in, the credential marked used. Without one it
	// creates the person, their personal account and the credential.
	agent, credential, account, err := s.cfg.Auth.FindOrCreateAgent(ctx, authapp.UserInfo{
		ProviderUserID: id.Subject,
		Email:          id.Email,
		DisplayName:    id.Name,
		Provider:       id.Provider,
	})
	if err != nil {
		return AssertedSignInResult{}, fmt.Errorf("find or create the agent: %w", err)
	}
	if !known {
		s.logCreated(ctx, id, email, agent.GetID())
	}
	return AssertedSignInResult{
		Agent:      agent,
		Credential: credential,
		Account:    account,
		// Under the identity lock, a look-up that found nothing and linked
		// nothing is followed by FindOrCreateAgent's create.
		NewAccount: !known,
	}, nil
}

// signInLockKeys are the keys a sign-in for an identity holds on the SignInLock:
// the identity first and the folded email second, the one order every holder
// takes them in, so two sign-ins never wait on each other in a circle.
func signInLockKeys(provider, subject, foldedEmail string) []string {
	keys := []string{"identity\x00" + provider + "\x00" + subject}
	if foldedEmail != "" {
		keys = append(keys, "email\x00"+foldedEmail)
	}
	return keys
}

// heldSignInKeysCtxKey is the context key under which a holder of the SignInLock
// records the keys it holds.
type heldSignInKeysCtxKey struct{}

// withSignInKeysHeld returns a context recording that its caller holds keys on
// the SignInLock.
func withSignInKeysHeld(ctx context.Context, keys []string) context.Context {
	held := map[string]bool{}
	if outer, ok := ctx.Value(heldSignInKeysCtxKey{}).(map[string]bool); ok {
		for key := range outer {
			held[key] = true
		}
	}
	for _, key := range keys {
		held[key] = true
	}
	return context.WithValue(ctx, heldSignInKeysCtxKey{}, held)
}

// signInKeysHeld reports whether ctx records that its caller holds every one of
// keys on the SignInLock.
func signInKeysHeld(ctx context.Context, keys []string) bool {
	held, _ := ctx.Value(heldSignInKeysCtxKey{}).(map[string]bool)
	for _, key := range keys {
		if !held[key] {
			return false
		}
	}
	return true
}

// ownerOf returns the one person holding a credential for email, "" when no
// credential holds it, ErrAmbiguousOwner when more than one person does, or
// ErrUnprovenOwner when credentials hold it but none says who owns it. Only a
// credential that provesOwnership for the arriving identity counts, and only
// for a person who still exists and is active: a person who is gone or turned
// off owns nothing. A door identity also gets ErrUnprovenOwner when an active
// door credential of an active person already holds the email, whatever other
// credentials hold it.
func (s *AssertedSignIn) ownerOf(ctx context.Context, id AssertedIdentity, email string) (string, error) {
	matches, err := s.cfg.Emails.CredentialsByEmail(ctx, email)
	if err != nil {
		return "", fmt.Errorf("look up the owner of the asserted email: %w", err)
	}
	var owners, holders []string
	counted := map[string]bool{}
	held := map[string]bool{}
	providers := map[string]bool{}
	activePeople := map[string]bool{}
	isActive := func(agentID string) (bool, error) {
		if active, read := activePeople[agentID]; read {
			return active, nil
		}
		agent, err := s.cfg.Agents.FindByID(ctx, agentID)
		if err != nil {
			return false, fmt.Errorf("read a person holding the asserted email: %w", err)
		}
		active := agent != nil && agent.Active()
		activePeople[agentID] = active
		return active, nil
	}
	// doorHeld is true when the arriving identity is a door identity and an
	// active door credential of an active person already holds the email.
	doorHeld := false
	for _, m := range matches {
		providers[m.Provider] = true
		if !held[m.AgentID] {
			held[m.AgentID] = true
			holders = append(holders, m.AgentID)
		}
		if !doorHeld && id.Provider == OAuthProviderDoor && m.Provider == OAuthProviderDoor && m.Active {
			active, err := isActive(m.AgentID)
			if err != nil {
				return "", err
			}
			doorHeld = active
		}
		if counted[m.AgentID] || !s.provesOwnership(m, id.Provider) {
			continue
		}
		counted[m.AgentID] = true
		active, err := isActive(m.AgentID)
		if err != nil {
			return "", err
		}
		if active {
			owners = append(owners, m.AgentID)
		}
	}
	if doorHeld {
		// A second door subject for an email comes only from an operator
		// re-creating the person at the door, and it is never joined to the
		// first, whatever else the person holding the first one holds: a
		// google credential beside it proves the email, not that the two door
		// subjects are one person.
		s.refuseUnproven(ctx, id, email, holders, providers,
			"trusted issuer sign-in refused: a door credential already holds the asserted email, and a second door identity is never joined to it, so nothing was linked or created")
		return "", fmt.Errorf("%w (%d people)", ErrUnprovenOwner, len(holders))
	}
	switch len(owners) {
	case 0:
		if len(holders) == 0 {
			return "", nil
		}
		// Somebody holds the email, so creating a person could leave the
		// owner in a second, empty account; nobody proves they own it, so
		// linking could hand the identity to whoever wrote the email.
		s.refuseUnproven(ctx, id, email, holders, providers,
			"trusted issuer sign-in refused: credentials hold the asserted email but none of them proves who owns it, so nothing was linked or created")
		return "", fmt.Errorf("%w (%d people)", ErrUnprovenOwner, len(holders))
	case 1:
		return owners[0], nil
	default:
		// Only an operator can say which of these people owns the identity,
		// so the line names every one of them.
		sort.Strings(owners)
		s.cfg.Logger.Error(ctx, "trusted issuer sign-in refused: more than one person holds the asserted email, so nothing was linked or created",
			append([]any{"reason", ReasonAmbiguousOwner}, identityFields(id, email, owners...)...)...)
		return "", fmt.Errorf("%w (%d people)", ErrAmbiguousOwner, len(owners))
	}
}

// refuseUnproven writes the one error line for an unproven-owner refusal. The
// line names every person holding the email and the kinds of credential they
// hold, which is what an operator decides from.
func (s *AssertedSignIn) refuseUnproven(ctx context.Context, id AssertedIdentity, email string, holders []string, providers map[string]bool, message string) {
	sort.Strings(holders)
	kinds := make([]string, 0, len(providers))
	for p := range providers {
		kinds = append(kinds, p)
	}
	sort.Strings(kinds)
	s.cfg.Logger.Error(ctx, message,
		append(append([]any{"reason", ReasonUnprovenOwner}, identityFields(id, email, holders...)...),
			"matched_providers", strings.Join(kinds, ","))...)
}

// logCreated writes the one line for a person a sign-in created. On an
// allowlisted instance that already has another active person it is a
// warning: an instance with named owners rarely gains a second person on
// purpose, and the likelier story is an owner whose email the door sent
// matches no credential here.
func (s *AssertedSignIn) logCreated(ctx context.Context, id AssertedIdentity, email, agentID string) {
	fields := identityFields(id, email, agentID)
	if !s.cfg.Allowlisted {
		s.cfg.Logger.Info(ctx, "trusted issuer sign-in created a person", fields...)
		return
	}
	others, err := s.otherPeople(ctx, agentID)
	switch {
	case err != nil:
		s.cfg.Logger.Warn(ctx, "trusted issuer sign-in created a person on an allowlisted instance and could not tell whether another person already exists",
			append(fields, "error", err.Error())...)
	case len(others) > 0:
		s.cfg.Logger.Warn(ctx, "trusted issuer sign-in created a person on an allowlisted instance that already has one; if this is the owner, the email the door sent matches no credential here",
			append(fields, "other_agent_ids", strings.Join(others, ","))...)
	default:
		s.cfg.Logger.Info(ctx, "trusted issuer sign-in created a person", fields...)
	}
}

// otherPeopleShown is the most other people a created-person warning names.
const otherPeopleShown = 5

// otherPeople returns up to otherPeopleShown active people other than except.
func (s *AssertedSignIn) otherPeople(ctx context.Context, except string) ([]string, error) {
	var others []string
	cursor := ""
	for {
		page, err := s.cfg.Agents.FindAll(ctx, cursor, 100)
		if err != nil {
			return nil, fmt.Errorf("list the people on the instance: %w", err)
		}
		if page == nil {
			return others, nil
		}
		for _, agent := range page.Data {
			if agent == nil || agent.GetID() == except || !agent.Active() || agent.AgentType() != entities.AgentTypePerson {
				continue
			}
			others = append(others, agent.GetID())
			if len(others) == otherPeopleShown {
				return others, nil
			}
		}
		if !page.HasMore || page.Cursor == "" || page.Cursor == cursor {
			return others, nil
		}
		cursor = page.Cursor
	}
}

// identityFields are the fields every owner-binding log line carries: the
// provider, the people the line is about, and hashes of the subject and the
// normalized email. Never the subject or the email themselves: a log line
// outlives the sign-in, and the pair names a person.
func identityFields(id AssertedIdentity, email string, agentIDs ...string) []any {
	return []any{
		"provider", id.Provider,
		"sub_hash", logHash(id.Subject),
		"email_hash", logHash(email),
		"agent_ids", strings.Join(agentIDs, ","),
	}
}

// logHash is the first 16 hexadecimal characters of value's SHA-256. An
// operator who suspects an address computes the same and searches for it:
//
//	printf '%s' "$value" | shasum -a 256 | cut -c1-16
func logHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

// discardSignInLogs is the logger an AssertedSignIn built without one uses.
type discardSignInLogs struct{}

func (discardSignInLogs) Debug(context.Context, string, ...any) {}
func (discardSignInLogs) Info(context.Context, string, ...any)  {}
func (discardSignInLogs) Warn(context.Context, string, ...any)  {}
func (discardSignInLogs) Error(context.Context, string, ...any) {}

// provesOwnership reports whether a credential holding the asserted email may
// say who owns it, for an identity arriving from the provider arriving. It
// must be active: a sign-in method someone turned off must not come back
// through the door. And it must come from a provider that verified the email
// (ownerProvingProviders), be a password credential on an instance whose
// operator opted in (PasswordOwnersProven), or be a door credential while the
// arriving identity is one doorCredentialProvesOwnerFor names.
func (s *AssertedSignIn) provesOwnership(m repositories.CredentialEmailMatch, arriving string) bool {
	return credentialProvesOwnership(m, arriving, s.cfg.PasswordOwnersProven)
}

// credentialProvesOwnership is that rule as a function of the one setting it
// reads. The trusted issuer's token revocation reaches the same people a
// sign-in would reach (see AssertedTokenRevocation), so it asks this and not a
// second copy of the rule: a copy that drifts would revoke the tokens of
// somebody the sign-in never links.
func credentialProvesOwnership(
	m repositories.CredentialEmailMatch, arriving string, passwordOwnersProven bool,
) bool {
	if !m.Active {
		return false
	}
	switch m.Provider {
	case entities.ProviderPassword:
		return passwordOwnersProven
	case OAuthProviderDoor:
		return doorCredentialProvesOwnerFor[arriving]
	}
	return ownerProvingProviders[m.Provider]
}

// link stores a credential for the identity against the owner, records its
// creation the way pericarp records a credential it makes, and reports whether
// it linked.
//
// The row is saved before the event is committed, so the store's unique
// (provider, provider_user_id) index settles a race with another process
// before any event exists. A link that loses the race records nothing, and a
// Credential.Created event is only ever committed for a row that was saved:
// replaying the event store cannot bind the identity to a person the store
// never did.
//
// The row and its event cannot be written in one transaction from here:
// pericarp's UnitOfWork carries events only, and its event store joins an
// outer transaction only from inside a subscription batch. So when the event
// cannot be committed after the save, link takes the row back (takeBackLink)
// and the sign-in fails, rather than leaving a link that works until a
// projection rebuild drops it. The person's next sign-in links again.
func (s *AssertedSignIn) link(ctx context.Context, ownerID string, id AssertedIdentity) (bool, error) {
	credential, err := new(entities.Credential).With(
		ksuid.New().String(), ownerID, id.Provider, id.Subject, id.Email, id.Name,
	)
	if err != nil {
		return false, fmt.Errorf("build the linked credential: %w", err)
	}
	if err := s.cfg.Credentials.Save(ctx, credential); err != nil {
		// Another process stored a credential for this identity between the
		// look-up and here. The identity has a credential either way, and
		// FindOrCreateAgent resolves whichever one the store holds.
		if errors.Is(err, authrepos.ErrDuplicateCredential) {
			return false, nil
		}
		return false, fmt.Errorf("save the linked credential: %w", err)
	}
	if s.cfg.EventStore != nil {
		uow := esapp.NewSimpleUnitOfWork(s.cfg.EventStore, s.cfg.Dispatcher)
		err := uow.Track(credential)
		if err == nil {
			err = uow.Commit(ctx)
		}
		if err != nil {
			s.takeBackLink(ctx, ownerID, id, credential.GetID(), err)
			return false, fmt.Errorf("record the linked credential: %w", err)
		}
	}
	return true, nil
}

// takeBackLink deletes the row of a linked credential whose creation event
// could not be committed, and writes the ERROR line that names the owner and
// the credential. When the row cannot be deleted, or nothing is configured to
// delete it, a second ERROR line tells an operator which row to repair.
func (s *AssertedSignIn) takeBackLink(ctx context.Context, ownerID string, id AssertedIdentity, credentialID string, cause error) {
	fields := append(identityFields(id, repositories.FoldCredentialEmail(id.Email), ownerID),
		"owner_agent_id", ownerID,
		"credential_id", credentialID,
		"error", cause.Error())
	deleteErr := errors.New("no credential row deleter is configured")
	if s.cfg.CredentialRows != nil {
		// Not the request's own context: a request that ended is a common
		// reason the commit failed, and the row must go all the same.
		deleteErr = s.cfg.CredentialRows.DeleteCredentialRow(context.WithoutCancel(ctx), credentialID)
	}
	if deleteErr == nil {
		s.cfg.Logger.Error(ctx, "trusted issuer sign-in could not record a linked credential's creation, so it deleted the credential's row; nothing was linked",
			append(fields, "row_deleted", true)...)
		return
	}
	s.cfg.Logger.Error(ctx, "trusted issuer sign-in could not record a linked credential's creation, and could not delete the credential's row",
		append(fields, "row_deleted", false)...)
	s.cfg.Logger.Error(ctx, "repair: a linked credential's row has no Credential.Created event; delete that row from credentials, then let the person sign in through the door again",
		"owner_agent_id", ownerID,
		"credential_id", credentialID,
		"delete_error", deleteErr.Error())
}

// keyedMutex serializes work per key. A wait for a key ends with ctx. An entry
// lives only while someone holds or waits for its key, so the map does not grow
// with every identity seen.
type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*keyedLock
}

type keyedLock struct {
	held  chan struct{}
	users int
}

// lock blocks until key is free and returns the function that frees it. When
// ctx ends first, or has ended by the time the key is free, it holds nothing
// and returns ctx's error.
func (k *keyedMutex) lock(ctx context.Context, key string) (func(), error) {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = map[string]*keyedLock{}
	}
	l, ok := k.locks[key]
	if !ok {
		l = &keyedLock{held: make(chan struct{}, 1)}
		k.locks[key] = l
	}
	l.users++
	k.mu.Unlock()

	leave := func() {
		k.mu.Lock()
		defer k.mu.Unlock()
		l.users--
		if l.users == 0 {
			delete(k.locks, key)
		}
	}
	select {
	case l.held <- struct{}{}:
	case <-ctx.Done():
		leave()
		return nil, ctx.Err()
	}
	unlock := func() {
		<-l.held
		leave()
	}
	// A free key and an ended ctx are both ready, and select picks either. A
	// caller whose request has ended must not go on to write.
	if err := ctx.Err(); err != nil {
		unlock()
		return nil, err
	}
	return unlock, nil
}
