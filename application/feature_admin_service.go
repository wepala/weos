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
	"fmt"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	esapp "github.com/akeemphilbert/pericarp/pkg/eventsourcing/application"
	esdomain "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/pkg/identity"
)

// FeatureAdminService is what the command line, the REST routes and the MCP
// tools all call. It owns exactly three things the surfaces would otherwise
// each have to get right on their own — who is allowed to change instance
// state, what gets recorded when they do, and what a listing looks like — and
// delegates every actual write to FeatureService untouched.
//
// The split is forced, not stylistic. FeatureService is called directly by
// #481's acceptance steps with a bare context and no identity; putting a
// permission check inside it would fail scenarios that are green and in the
// regression suite. It is also the seam FeatureService's own doc comment
// anticipates.
//
// Three surfaces, one guard, one audit path: a permission rule copied three
// times is a permission rule that will disagree with itself.
type FeatureAdminService struct {
	features *FeatureService
	resolver *FeatureResolver
	accounts authrepos.AccountRepository
	creds    authrepos.CredentialRepository

	// primaryAccountID names the account whose owners and admins may change
	// instance-wide state. Empty means "work it out from how many accounts
	// exist" — see requireOperator.
	primaryAccountID string

	eventStore esdomain.EventStore
	dispatcher *esdomain.EventDispatcher

	logger entities.Logger
}

// NewFeatureAdminService builds the surface layer.
func NewFeatureAdminService(
	cfg config.Config,
	features *FeatureService,
	resolver *FeatureResolver,
	accounts authrepos.AccountRepository,
	creds authrepos.CredentialRepository,
	eventStore esdomain.EventStore,
	dispatcher *esdomain.EventDispatcher,
	logger entities.Logger,
) *FeatureAdminService {
	return &FeatureAdminService{
		features:         features,
		resolver:         resolver,
		accounts:         accounts,
		creds:            creds,
		primaryAccountID: cfg.Features.PrimaryAccountID,
		eventStore:       eventStore,
		dispatcher:       dispatcher,
		logger:           logger,
	}
}

// Listing returns every declared feature with its resolved value and the layer
// that decided it, for whoever is on ctx.
//
// Readable by any authenticated caller. Seeing which capabilities exist is not
// the same as being able to change them, and an operator who cannot read the
// list cannot use the commands that change it.
func (s *FeatureAdminService) Listing(ctx context.Context) ([]entities.FeatureStatus, error) {
	return s.resolver.Explain(ctx)
}

// SetInstance turns a feature on or off for the whole instance.
func (s *FeatureAdminService) SetInstance(ctx context.Context, key string, enabled bool, src string) error {
	if err := s.requireOperator(ctx); err != nil {
		return err
	}
	if err := s.features.SetInstanceFeature(ctx, key, enabled); err != nil {
		return err
	}
	state := entities.FeatureChangeStateOff
	if enabled {
		state = entities.FeatureChangeStateOn
	}
	s.record(ctx, key, "instance", "", state, src)
	return nil
}

// ResetInstance drops the instance override so the feature returns to its
// declared default — which is not the same as turning it off, because an
// account or a grant can still turn a declared-off feature on.
func (s *FeatureAdminService) ResetInstance(ctx context.Context, key, src string) error {
	if err := s.requireOperator(ctx); err != nil {
		return err
	}
	if err := s.features.ClearInstanceFeature(ctx, key); err != nil {
		return err
	}
	s.record(ctx, key, "instance", "", entities.FeatureChangeStateReset, src)
	return nil
}

// SetAccount turns a feature on or off for the account the caller is signed in
// to. The account is never taken as a parameter: it comes off the session, so
// an admin cannot reach into an account they are not acting in.
func (s *FeatureAdminService) SetAccount(ctx context.Context, key string, enabled bool, src string) error {
	accountID, err := s.requireAccountAdmin(ctx)
	if err != nil {
		return err
	}
	if err := s.features.SetAccountFeature(ctx, accountID, key, enabled); err != nil {
		return err
	}
	state := entities.FeatureChangeStateOff
	if enabled {
		state = entities.FeatureChangeStateOn
	}
	s.record(ctx, key, "account", accountID, state, src)
	return nil
}

// ResetAccount drops the caller's account override.
func (s *FeatureAdminService) ResetAccount(ctx context.Context, key, src string) error {
	accountID, err := s.requireAccountAdmin(ctx)
	if err != nil {
		return err
	}
	if err := s.features.ClearAccountFeature(ctx, accountID, key); err != nil {
		return err
	}
	s.record(ctx, key, "account", accountID, entities.FeatureChangeStateReset, src)
	return nil
}

// requireOperator decides who may change instance-wide state.
//
// A caller on the local stdio transport passes without further checks. That is
// not a hole: whoever can run `weos mcp` on the machine can already run
// `weos feature disable` against the same database, so refusing would buy
// nothing and would make the operator tooling unusable on the surface mini-me
// actually uses. HTTP requests never carry the marker.
//
// The audit source is deliberately NOT consulted here. If passing
// FeatureChangeSourceCLI were enough to skip the check, a handler with the
// wrong constant would be a full instance-write bypass.
func (s *FeatureAdminService) requireOperator(ctx context.Context) error {
	if isLocalTransport(ctx) {
		return nil
	}
	identityCtx := auth.AgentFromCtx(ctx)
	if identityCtx == nil || identityCtx.ActiveAccountID == "" {
		// A session that names no account has no role to check. Refused
		// explicitly rather than left to the lookup, which returns an empty
		// role for an empty account and would reach the same answer less
		// obviously.
		return fmt.Errorf("changing instance feature state requires a signed-in operator: %w", ErrForbidden)
	}
	primary, err := s.instanceAccount(ctx)
	if err != nil {
		return err
	}
	// The role must be held in the INSTANCE's account, not merely in some
	// account the caller happens to own. Registration mints every new user as
	// owner of their own personal account, so checking the caller's active
	// account would make "owner or admin" mean "anyone who signed up".
	role, err := s.accounts.FindMemberRole(ctx, primary, identityCtx.AgentID)
	if err != nil {
		return fmt.Errorf("failed to read the caller's role: %w", err)
	}
	if role != authentities.RoleOwner && role != authentities.RoleAdmin {
		return fmt.Errorf(
			"only an owner or admin of this instance's account may change instance feature state: %w",
			ErrForbidden)
	}
	return nil
}

// instanceAccount answers which account owns the instance-wide switch.
//
// Configured wins. Otherwise, exactly one account means that account is the
// instance — the single-user shape, where no configuration should be needed.
// More than one and nothing configured is refused rather than guessed: picking
// one would hand the switch to whoever happened to register first, and picking
// the caller's would hand it to everyone.
func (s *FeatureAdminService) instanceAccount(ctx context.Context) (string, error) {
	if s.primaryAccountID != "" {
		return s.primaryAccountID, nil
	}
	page, err := s.accounts.FindAll(ctx, "", 2)
	if err != nil {
		return "", fmt.Errorf("failed to read the instance's accounts: %w", err)
	}
	switch {
	case page == nil || len(page.Data) == 0:
		return "", fmt.Errorf(
			"this instance has no account to own its feature switch: %w", ErrForbidden)
	case len(page.Data) == 1:
		return page.Data[0].GetID(), nil
	default:
		return "", fmt.Errorf(
			"this instance has more than one account, so it cannot tell which one owns the "+
				"feature switch: set FEATURE_PRIMARY_ACCOUNT_ID (the command line still works): %w",
			ErrForbidden)
	}
}

// requireAccountAdmin checks the same roles and returns the account the caller
// is acting in. Local stdio has no account to act in, so it is refused here
// even though it passes requireOperator — an instance-wide switch needs no
// account, an account override does.
func (s *FeatureAdminService) requireAccountAdmin(ctx context.Context) (string, error) {
	identityCtx := auth.AgentFromCtx(ctx)
	if identityCtx == nil || identityCtx.ActiveAccountID == "" {
		return "", fmt.Errorf("changing account feature state requires a session in an account: %w", ErrForbidden)
	}
	role, err := s.accounts.FindMemberRole(ctx, identityCtx.ActiveAccountID, identityCtx.AgentID)
	if err != nil {
		return "", fmt.Errorf("failed to read the caller's role: %w", err)
	}
	if role != authentities.RoleOwner && role != authentities.RoleAdmin {
		return "", fmt.Errorf("only an owner or admin may change account feature state: %w", ErrForbidden)
	}
	return identityCtx.ActiveAccountID, nil
}

// record appends the change to the event log.
//
// Deliberately after the write and never fatal. The setting has already
// changed by the time this runs, so returning an error here would report
// failure for work that succeeded. A failed audit write is logged and
// surfaced as a message instead, which is the honest account: the change
// happened, the record of it did not.
func (s *FeatureAdminService) record(ctx context.Context, key, scope, scopeID, state, src string) {
	if s.eventStore == nil || s.dispatcher == nil {
		// Nothing to record into. Guarded rather than left to fail inside the
		// unit of work, which panics on a nil store — and a panic here would
		// take down a request whose actual work already succeeded, which is
		// the opposite of this method's whole failure policy.
		if s.logger != nil {
			s.logger.Error(ctx, "the feature change was applied but there is nowhere to record it",
				"feature", key, "scope", scope, "state", state)
		}
		return
	}
	event := entities.FeatureChanged{
		Key:     key,
		Scope:   scope,
		ScopeID: scopeID,
		State:   state,
		Source:  src,
	}
	if identityCtx := auth.AgentFromCtx(ctx); identityCtx != nil {
		event.ActorID = identityCtx.AgentID
		event.ActorEmail = s.emailFor(ctx, identityCtx.AgentID)
	}

	change, err := new(entities.FeatureChange).With(identity.NewFeatureChange(), event)
	if err == nil {
		uow := esapp.NewSimpleUnitOfWork(s.eventStore, s.dispatcher)
		if err = uow.Track(change); err == nil {
			err = uow.Commit(ctx)
		}
	}
	if err != nil {
		if s.logger != nil {
			s.logger.Error(ctx, "the feature change was applied but not recorded",
				"feature", key, "scope", scope, "state", state, "error", err)
		}
		entities.AddMessage(ctx, entities.Message{
			Type: "warning",
			Text: fmt.Sprintf("feature %q was changed, but the change could not be recorded", key),
		})
	}
}

// emailFor resolves an agent's email so the audit record is readable later.
// The identity on a request carries only a KSUID, and a log of KSUIDs answers
// "who" in a way nobody can use without a lookup they will not do. One extra
// read on a rare operator action is a fair price.
func (s *FeatureAdminService) emailFor(ctx context.Context, agentID string) string {
	if s.creds == nil {
		return ""
	}
	creds, err := s.creds.FindByAgent(ctx, agentID)
	if err != nil || len(creds) == 0 {
		return ""
	}
	return creds[0].Email()
}
