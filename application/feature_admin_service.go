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

	"time"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authentities "github.com/akeemphilbert/pericarp/pkg/auth/domain/entities"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	esapp "github.com/akeemphilbert/pericarp/pkg/eventsourcing/application"
	esdomain "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
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
		// The role is read in the INSTANCE's account, not this one, so a
		// session naming no account could in principle still be checked. It is
		// refused anyway: a session that has not settled on an account has not
		// finished authenticating, and instance-wide state is the last thing
		// that should be reachable from a half-formed one.
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
		// Checked, not trusted. A typo makes FindMemberRole return an empty
		// role for everyone, so every operator would be told they lack the
		// role — a message that blames the caller for a mistake in an
		// environment variable, and one nobody would think to doubt.
		account, err := s.accounts.FindByID(ctx, s.primaryAccountID)
		if err != nil || account == nil {
			return "", fmt.Errorf(
				"FEATURE_PRIMARY_ACCOUNT_ID names account %q, which does not exist: %w",
				s.primaryAccountID, ErrForbidden)
		}
		return s.primaryAccountID, nil
	}
	// Deactivated accounts still count here — FindAll does not filter on
	// Active — so an instance that retired an old account reads as ambiguous
	// and is refused rather than guessed at. Naming the account explicitly is
	// the way out, and the message says so.
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
	s.recordEvent(ctx, entities.FeatureChanged{
		Key:     key,
		Scope:   scope,
		ScopeID: scopeID,
		State:   state,
		Source:  src,
	})
}

// recordEvent stamps the actor and appends one change to the event log.
func (s *FeatureAdminService) recordEvent(ctx context.Context, event entities.FeatureChanged) {
	if s.eventStore == nil || s.dispatcher == nil {
		if s.logger != nil {
			s.logger.Error(ctx, "the feature change was applied but there is nowhere to record it",
				"feature", event.Key, "scope", event.Scope, "state", event.State)
		}
		return
	}
	if identityCtx := auth.AgentFromCtx(ctx); identityCtx != nil {
		event.ActorID = identityCtx.AgentID
		event.ActorEmail = s.emailFor(ctx, identityCtx.AgentID)
	}
	key := event.Key

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
				"feature", key, "scope", event.Scope, "state", event.State, "error", err)
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

// GrantRequest names a grant to make. Exactly one of Email or Role identifies
// the subject.
//
// AccountID is honored ONLY on the local transport. Over HTTP and MCP the
// account comes off the session, and the request shapes there do not expose
// the field at all — so an admin naming somebody else's account in a body has
// nothing to bind to, rather than being refused after the fact.
type GrantRequest struct {
	Key   string
	Email string
	Role  string

	ValidFrom    *time.Time
	ValidThrough *time.Time

	AccountID string
	Source    string
}

// RevokeRequest names a grant to take back.
type RevokeRequest struct {
	Key       string
	Email     string
	Role      string
	AccountID string
	Source    string
}

// GrantView is one grant as an operator reads it.
type GrantView struct {
	FeatureKey   string     `json:"featureKey"`
	SubjectType  string     `json:"subjectType"`
	Email        string     `json:"email,omitempty"`
	Role         string     `json:"role,omitempty"`
	ValidFrom    *time.Time `json:"validFrom,omitempty"`
	ValidThrough *time.Time `json:"validThrough,omitempty"`
	// Status is active, pending or expired. An expired grant is still a row —
	// revocation deletes, a closed window does not — so a listing has to be
	// able to say which it is looking at.
	Status    string    `json:"status"`
	GrantedBy string    `json:"grantedBy,omitempty"`
	GrantedAt time.Time `json:"grantedAt"`
	// Via is "direct" or "role:<r>", set only when listing what one person
	// holds, where the difference is the whole question.
	Via string `json:"via,omitempty"`
}

// Grant gives a feature to one person or to a role.
func (s *FeatureAdminService) Grant(ctx context.Context, req GrantRequest) error {
	accountID, err := s.grantAccount(ctx, req.AccountID, true)
	if err != nil {
		return err
	}
	subjectType, subjectID, subjectEmail, err := s.resolveSubject(ctx, accountID, req.Key, req.Email, req.Role)
	if err != nil {
		return err
	}

	terms := GrantTerms{
		ValidFrom:    req.ValidFrom,
		ValidThrough: req.ValidThrough,
		Source:       req.Source,
	}
	if identityCtx := auth.AgentFromCtx(ctx); identityCtx != nil {
		terms.GrantedByID = identityCtx.AgentID
		terms.GrantedByEmail = s.emailFor(ctx, identityCtx.AgentID)
	}

	if subjectType == repositories.FeatureSubjectRole {
		err = s.features.GrantToRole(ctx, accountID, subjectID, req.Key, terms)
	} else {
		err = s.features.GrantToAgent(ctx, accountID, subjectID, req.Key, terms)
	}
	if err != nil {
		return err
	}
	s.recordGrant(ctx, req.Key, accountID, entities.FeatureChangeStateGranted,
		req.Source, subjectType, subjectID, subjectEmail)
	return nil
}

// RevokeGrant takes a grant back.
//
// A revocation that matched nothing succeeds and records nothing. The caller's
// intent is already satisfied, and an audit log stays readable by containing
// only things that happened.
func (s *FeatureAdminService) RevokeGrant(ctx context.Context, req RevokeRequest) error {
	accountID, err := s.grantAccount(ctx, req.AccountID, true)
	if err != nil {
		return err
	}
	subjectType, subjectID, subjectEmail, err := s.resolveSubject(ctx, accountID, req.Key, req.Email, req.Role)
	if err != nil {
		return err
	}

	var removed bool
	if subjectType == repositories.FeatureSubjectRole {
		removed, err = s.features.RevokeFromRole(ctx, accountID, subjectID, req.Key)
	} else {
		removed, err = s.features.RevokeFromAgent(ctx, accountID, subjectID, req.Key)
	}
	if err != nil {
		return err
	}
	if removed {
		s.recordGrant(ctx, req.Key, accountID, entities.FeatureChangeStateRevoked,
			req.Source, subjectType, subjectID, subjectEmail)
	}
	return nil
}

// GrantsOn lists every grant on one feature in an account.
//
// Readable by any member of the account, like the feature listing itself:
// seeing who holds something is not the same as being able to change it.
func (s *FeatureAdminService) GrantsOn(ctx context.Context, key, accountID string) ([]GrantView, error) {
	resolved, err := s.grantAccount(ctx, accountID, false)
	if err != nil {
		return nil, err
	}
	if _, err := s.features.Declared(key); err != nil {
		return nil, err
	}
	records, err := s.features.GrantsOnFeature(ctx, resolved, key)
	if err != nil {
		return nil, err
	}
	return s.viewsOf(ctx, records, ""), nil
}

// GrantsHeldBy lists everything one person holds in the caller's account,
// whether they hold it themselves or through their role.
func (s *FeatureAdminService) GrantsHeldBy(ctx context.Context, email string) ([]GrantView, error) {
	accountID, err := s.grantAccount(ctx, "", false)
	if err != nil {
		return nil, err
	}
	agentID, _, err := s.agentForEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	roleID, err := s.accounts.FindMemberRole(ctx, accountID, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the member's role: %w", err)
	}
	records, err := s.features.GrantsFor(ctx, accountID, agentID, roleID)
	if err != nil {
		return nil, err
	}
	return s.viewsOf(ctx, records, agentID), nil
}

// viewsOf renders records for an operator. markDirectFor, when set, tags each
// row as held directly or through a role.
func (s *FeatureAdminService) viewsOf(
	ctx context.Context, records []entities.FeatureGrantRecord, markDirectFor string,
) []GrantView {
	now := time.Now()
	out := make([]GrantView, 0, len(records))
	for _, r := range records {
		v := GrantView{
			FeatureKey:   r.FeatureKey,
			SubjectType:  r.SubjectType,
			ValidFrom:    r.ValidFrom,
			ValidThrough: r.ValidThrough,
			Status:       r.WindowState(now),
			GrantedBy:    r.GrantedByEmail,
			GrantedAt:    r.CreatedAt,
		}
		if r.SubjectType == repositories.FeatureSubjectRole {
			v.Role = r.SubjectID
		} else {
			// Resolved at read time rather than stored: an address can change
			// and a grant should not follow it, so the row holds the agent id.
			v.Email = s.emailFor(ctx, r.SubjectID)
		}
		if markDirectFor != "" {
			if r.SubjectType == repositories.FeatureSubjectRole {
				v.Via = "role:" + r.SubjectID
			} else {
				v.Via = "direct"
			}
		}
		out = append(out, v)
	}
	return out
}

// DefaultGrantAccount names the account a command-line grant lands in when the
// operator did not say.
//
// Exactly one account means that account, and nothing has to be configured.
// That is narrower than it sounds: registration mints every person a personal
// account, so a single-account instance is a single-person instance — the
// mini-me shape, where nobody should have to look up a KSUID to grant
// themselves something.
//
// Deliberately does NOT fall back to FEATURE_PRIMARY_ACCOUNT_ID. That names
// who owns the instance switch, not where a grant lands, and quietly dropping
// a grant into it on a multi-tenant instance would put a right in an account
// nobody named.
func (s *FeatureAdminService) DefaultGrantAccount(ctx context.Context) (string, error) {
	page, err := s.accounts.FindAll(ctx, "", 2)
	if err != nil {
		return "", fmt.Errorf("failed to read the instance's accounts: %w", err)
	}
	switch {
	case page == nil || len(page.Data) == 0:
		return "", fmt.Errorf("this instance has no account to grant in: %w", ErrValidation)
	case len(page.Data) == 1:
		return page.Data[0].GetID(), nil
	default:
		return "", fmt.Errorf(
			"this instance has more than one account, so a grant has to say which: "+
				"name it with --account <id>: %w", ErrValidation)
	}
}

// grantAccount decides which account a grant acts in.
//
// The local transport is the operator's, so it may name an account — the
// command line resolved the id already and whoever runs it holds the database.
// With no account named there, the caller is refused and told where to go: an
// account-scoped grant has no account to land in when nobody is signed in, and
// guessing one would be the multi-tenant hole this arrangement exists to avoid.
//
// Everywhere else the account comes off the session and any named one is
// ignored, so an admin cannot reach into an account by naming it.
func (s *FeatureAdminService) grantAccount(ctx context.Context, named string, write bool) (string, error) {
	if isLocalTransport(ctx) {
		if named == "" {
			return "", fmt.Errorf(
				"a grant needs an account and this transport has no signed-in caller; "+
					"make it from the command line with --account <id>: %w", ErrValidation)
		}
		account, err := s.accounts.FindByID(ctx, named)
		if err != nil || account == nil {
			return "", fmt.Errorf("no account %q exists: %w", named, ErrValidation)
		}
		return named, nil
	}
	if write {
		return s.requireAccountAdmin(ctx)
	}
	identityCtx := auth.AgentFromCtx(ctx)
	if identityCtx == nil || identityCtx.ActiveAccountID == "" {
		return "", fmt.Errorf("reading grants requires a session in an account: %w", ErrForbidden)
	}
	role, err := s.accounts.FindMemberRole(ctx, identityCtx.ActiveAccountID, identityCtx.AgentID)
	if err != nil {
		return "", fmt.Errorf("failed to read the caller's role: %w", err)
	}
	if role == "" {
		return "", fmt.Errorf("only a member of the account may read its grants: %w", ErrForbidden)
	}
	return identityCtx.ActiveAccountID, nil
}

// resolveSubject turns an email or a role into a stored subject, refusing in a
// deliberate order: the feature first, then eligibility, then the person.
//
// The order is the point. Granting a mistyped feature to a stranger should
// report the feature, not the stranger — the first thing wrong is the first
// thing said.
func (s *FeatureAdminService) resolveSubject(
	ctx context.Context, accountID, key, email, role string,
) (subjectType, subjectID, subjectEmail string, err error) {
	meta, err := s.features.Declared(key)
	if err != nil {
		return "", "", "", err
	}
	if !meta.Grantable {
		return "", "", "", fmt.Errorf("feature %q cannot be granted: %w", key, ErrValidation)
	}
	if (email == "") == (role == "") {
		return "", "", "", fmt.Errorf(
			"name exactly one of a person or a role to grant to: %w", ErrValidation)
	}

	if role != "" {
		switch role {
		case authentities.RoleOwner, authentities.RoleAdmin, authentities.RoleMember:
			return repositories.FeatureSubjectRole, role, "", nil
		default:
			return "", "", "", fmt.Errorf(
				"no role %q exists; the roles are %q, %q and %q: %w",
				role, authentities.RoleOwner, authentities.RoleAdmin,
				authentities.RoleMember, ErrValidation)
		}
	}

	agentID, resolvedEmail, err := s.agentForEmail(ctx, email)
	if err != nil {
		return "", "", "", err
	}
	memberRole, err := s.accounts.FindMemberRole(ctx, accountID, agentID)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to read the member's role: %w", err)
	}
	if memberRole == "" {
		return "", "", "", fmt.Errorf(
			"%q is not a member of this account: %w", email, ErrValidation)
	}
	return repositories.FeatureSubjectAgent, agentID, resolvedEmail, nil
}

// agentForEmail finds who an address belongs to. An address nobody signed up
// with is a 404 rather than a grant stored against nothing.
func (s *FeatureAdminService) agentForEmail(ctx context.Context, email string) (string, string, error) {
	if s.creds == nil {
		return "", "", fmt.Errorf("cannot look up %q: %w", email, repositories.ErrNotFound)
	}
	creds, err := s.creds.FindByEmail(ctx, email)
	if err != nil {
		return "", "", fmt.Errorf("failed to look up %q: %w", email, err)
	}
	if len(creds) == 0 {
		return "", "", fmt.Errorf("nobody signed up with %q: %w", email, repositories.ErrNotFound)
	}
	return creds[0].AgentID(), email, nil
}

// recordGrant appends a grant or revocation to the same log that carries
// override changes, so "who took that away, and when" has one answer.
func (s *FeatureAdminService) recordGrant(
	ctx context.Context, key, accountID, state, src, subjectType, subjectID, subjectEmail string,
) {
	s.recordEvent(ctx, entities.FeatureChanged{
		Key:          key,
		Scope:        "account",
		ScopeID:      accountID,
		State:        state,
		Source:       src,
		SubjectType:  subjectType,
		SubjectID:    subjectID,
		SubjectEmail: subjectEmail,
	})
}
