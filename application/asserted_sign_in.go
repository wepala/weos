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
	"errors"
	"fmt"
	"strings"
	"sync"

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

// AssertedIdentity is the person a trusted issuer's accepted assertion names.
type AssertedIdentity struct {
	// Provider is the registry key of the provider that verified the person.
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
	// LinkByEmail turns owner binding on. It is set exactly when the instance
	// has an identity allowlist (OAUTH_ALLOWED_EMAILS): an allowlisted
	// instance has named its owners, so an email there says who a person is;
	// an open instance lets anyone in, so an email there proves nothing.
	LinkByEmail bool
	// PasswordRegistrationOpen is set exactly when anyone may register a
	// password account on the instance (PASSWORD_REGISTRATION_ENABLED).
	// Registration verifies neither the email nor the allowlist, so a password
	// credential's email is then whatever its registrant typed. Owner binding
	// neither links to such a credential nor counts it: otherwise whoever
	// registered the owner's email before the owner's first door sign-in would
	// be handed the owner's identity and keep the password. Credentials from a
	// provider that verified the email are unaffected.
	PasswordRegistrationOpen bool
}

// AssertedSignIn decides whom a trusted issuer's accepted assertion signs in.
// It resolves the person by (provider, subject) the way the OAuth callback
// does, through FindOrCreateAgent. On an allowlisted instance, an identity it
// has never seen is linked to the one person already holding a credential for
// the same email instead of becoming a second person. See
// docs/decisions/trusted-issuer-login-assertion.md, "Owner binding".
//
// It is safe for concurrent use. Sign-ins for one identity, and on an
// allowlisted instance sign-ins for one email, are serialized in process, so
// two first sign-ins arriving together leave one person. The locks are held
// per process: replicas sharing a database still race, and the store's unique
// (provider, provider_user_id) index is what stops a second credential there.
type AssertedSignIn struct {
	cfg        AssertedSignInConfig
	identities keyedMutex
	emails     keyedMutex
}

// NewAssertedSignIn builds an AssertedSignIn.
func NewAssertedSignIn(cfg AssertedSignInConfig) *AssertedSignIn {
	return &AssertedSignIn{cfg: cfg}
}

// ProvideAssertedSignIn builds the AssertedSignIn the application wires, with
// owner binding on exactly when the instance has an identity allowlist, and
// password credentials left out of it exactly when password registration is
// open.
func ProvideAssertedSignIn(params struct {
	fx.In
	Config      config.Config
	Auth        authapp.AuthenticationService
	Credentials authrepos.CredentialRepository
	Agents      authrepos.AgentRepository
	Emails      repositories.CredentialEmailQuery
	EventStore  esdomain.EventStore       `optional:"true"`
	Dispatcher  *esdomain.EventDispatcher `optional:"true"`
}) *AssertedSignIn {
	return NewAssertedSignIn(AssertedSignInConfig{
		Auth:        params.Auth,
		Credentials: params.Credentials,
		Agents:      params.Agents,
		Emails:      params.Emails,
		EventStore:  params.EventStore,
		Dispatcher:  params.Dispatcher,
		LinkByEmail:              len(params.Config.OAuth.AllowedEmails) > 0,
		PasswordRegistrationOpen: params.Config.PasswordRegistrationEnabled,
	})
}

// SignIn resolves, links or creates the person the identity names.
func (s *AssertedSignIn) SignIn(ctx context.Context, id AssertedIdentity) (AssertedSignInResult, error) {
	// Always the identity lock first and the email lock second, and never more
	// than one of each, so two sign-ins can never wait on each other in a
	// circle.
	defer s.identities.lock(id.Provider + "\x00" + id.Subject)()
	email := strings.ToLower(strings.TrimSpace(id.Email))
	if s.cfg.LinkByEmail && email != "" {
		defer s.emails.lock(email)()
	}

	existing, err := s.cfg.Credentials.FindByProvider(ctx, id.Provider, id.Subject)
	if err != nil {
		return AssertedSignInResult{}, fmt.Errorf("look up the credential for provider %s: %w", id.Provider, err)
	}
	known := existing != nil
	if !known && s.cfg.LinkByEmail {
		owner, err := s.ownerOf(ctx, email)
		if err != nil {
			return AssertedSignInResult{}, err
		}
		if owner != "" {
			if err := s.link(ctx, owner, id); err != nil {
				return AssertedSignInResult{}, err
			}
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
	return AssertedSignInResult{
		Agent:      agent,
		Credential: credential,
		Account:    account,
		// Under the identity lock, a look-up that found nothing and linked
		// nothing is followed by FindOrCreateAgent's create.
		NewAccount: !known,
	}, nil
}

// ownerOf returns the one person holding a credential for email, "" when
// nobody does, or ErrAmbiguousOwner when more than one person does. Only a
// credential that provesOwnership counts, and only for a person who still
// exists and is active: a person who is gone or turned off owns nothing.
func (s *AssertedSignIn) ownerOf(ctx context.Context, email string) (string, error) {
	matches, err := s.cfg.Emails.CredentialsByEmail(ctx, email)
	if err != nil {
		return "", fmt.Errorf("look up the owner of the asserted email: %w", err)
	}
	var owners []string
	seen := map[string]bool{}
	for _, m := range matches {
		if seen[m.AgentID] || !s.provesOwnership(m) {
			continue
		}
		seen[m.AgentID] = true
		agent, err := s.cfg.Agents.FindByID(ctx, m.AgentID)
		if err != nil {
			return "", fmt.Errorf("read a person holding the asserted email: %w", err)
		}
		if agent != nil && agent.Active() {
			owners = append(owners, m.AgentID)
		}
	}
	switch len(owners) {
	case 0:
		return "", nil
	case 1:
		return owners[0], nil
	default:
		return "", fmt.Errorf("%w (%d people)", ErrAmbiguousOwner, len(owners))
	}
}

// provesOwnership reports whether a credential holding the asserted email may
// say who owns it. An inactive credential may not: a sign-in method someone
// turned off must not come back through the door. Nor may a password
// credential while anyone may register one (see PasswordRegistrationOpen).
func (s *AssertedSignIn) provesOwnership(m repositories.CredentialEmailMatch) bool {
	if !m.Active {
		return false
	}
	return m.Provider != entities.ProviderPassword || !s.cfg.PasswordRegistrationOpen
}

// link stores a credential for the identity against the owner, recording its
// creation the way pericarp records a credential it makes.
func (s *AssertedSignIn) link(ctx context.Context, ownerID string, id AssertedIdentity) error {
	credential, err := new(entities.Credential).With(
		ksuid.New().String(), ownerID, id.Provider, id.Subject, id.Email, id.Name,
	)
	if err != nil {
		return fmt.Errorf("build the linked credential: %w", err)
	}
	if s.cfg.EventStore != nil {
		uow := esapp.NewSimpleUnitOfWork(s.cfg.EventStore, s.cfg.Dispatcher)
		if err := uow.Track(credential); err != nil {
			return fmt.Errorf("track the linked credential: %w", err)
		}
		if err := uow.Commit(ctx); err != nil {
			return fmt.Errorf("record the linked credential: %w", err)
		}
	}
	if err := s.cfg.Credentials.Save(ctx, credential); err != nil {
		// Another process stored a credential for this identity between the
		// look-up and here. The identity now has a credential either way, and
		// FindOrCreateAgent resolves whichever one the store holds.
		if errors.Is(err, authrepos.ErrDuplicateCredential) {
			return nil
		}
		return fmt.Errorf("save the linked credential: %w", err)
	}
	return nil
}

// keyedMutex serializes work per key. An entry lives only while someone holds
// or waits for its key, so the map does not grow with every identity seen.
type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*keyedLock
}

type keyedLock struct {
	mu      sync.Mutex
	waiters int
}

// lock blocks until key is free and returns the function that frees it.
func (k *keyedMutex) lock(key string) func() {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = map[string]*keyedLock{}
	}
	l, ok := k.locks[key]
	if !ok {
		l = &keyedLock{}
		k.locks[key] = l
	}
	l.waiters++
	k.mu.Unlock()

	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		k.mu.Lock()
		l.waiters--
		if l.waiters == 0 {
			delete(k.locks, key)
		}
		k.mu.Unlock()
	}
}
