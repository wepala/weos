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

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
)

// FeatureService is the only writer of feature state, and every method here
// follows the same two-step shape: write the store, then invalidate.
//
// The invalidation is an explicit call rather than an event subscription. It
// has to be: a member's role change is a direct projection write that emits no
// event at all, and resolution depends on the caller's role. An
// event-driven-only invalidator would silently miss exactly the changes most
// likely to matter.
//
// Stories #482 and #483 put a CLI, REST routes and MCP tools in front of these
// methods. Nothing here knows about any of those surfaces.
type FeatureService struct {
	registry    *FeatureRegistry
	settings    repositories.FeatureSettingsRepository
	grants      repositories.FeatureGrantRepository
	members     repositories.AccountMemberQuery
	invalidator repositories.FeatureCacheInvalidator
	logger      entities.Logger
}

// NewFeatureService builds the service.
func NewFeatureService(
	registry *FeatureRegistry,
	settings repositories.FeatureSettingsRepository,
	grants repositories.FeatureGrantRepository,
	members repositories.AccountMemberQuery,
	invalidator repositories.FeatureCacheInvalidator,
	logger entities.Logger,
) *FeatureService {
	return &FeatureService{
		registry:    registry,
		settings:    settings,
		grants:      grants,
		members:     members,
		invalidator: invalidator,
		logger:      logger,
	}
}

// List returns every declared feature, sorted by key.
func (s *FeatureService) List() []entities.FeatureMeta {
	return s.registry.All()
}

// SetInstanceFeature turns a feature explicitly on or off for the whole
// instance. An explicit off is final for every layer below it, so this is the
// switch that stops a broken feature for everyone.
func (s *FeatureService) SetInstanceFeature(ctx context.Context, key string, enabled bool) error {
	if _, err := s.declared(key); err != nil {
		return err
	}
	if err := s.settings.SetOverride(ctx, repositories.FeatureScopeInstance, "", key, enabled); err != nil {
		return err
	}
	s.invalidator.InvalidateAll(ctx)
	s.log(ctx, "instance feature set", "feature", key, "enabled", enabled)
	return nil
}

// ClearInstanceFeature removes the instance-level override so the feature
// falls back to its declared default — which is NOT the same as turning it
// off, because an account or a grant may still turn a declared-off feature on.
func (s *FeatureService) ClearInstanceFeature(ctx context.Context, key string) error {
	if _, err := s.declared(key); err != nil {
		return err
	}
	if err := s.settings.ClearOverride(ctx, repositories.FeatureScopeInstance, "", key); err != nil {
		return err
	}
	s.invalidator.InvalidateAll(ctx)
	s.log(ctx, "instance feature cleared", "feature", key)
	return nil
}

// SetAccountFeature turns a feature explicitly on or off for one account.
func (s *FeatureService) SetAccountFeature(ctx context.Context, accountID, key string, enabled bool) error {
	meta, err := s.declared(key)
	if err != nil {
		return err
	}
	if accountID == "" {
		return fmt.Errorf("an account is required to override feature %q: %w", key, ErrValidation)
	}
	// Refused at the write rather than only ignored at the read. The resolver
	// ignores an ineligible row regardless — stored rows outlive declaration
	// changes — but an operator who asks for something impossible deserves to
	// be told, not silently obeyed and then overruled.
	if !meta.Manageable {
		return fmt.Errorf("feature %q cannot be changed per account: %w", key, ErrValidation)
	}
	if err := s.settings.SetOverride(ctx, repositories.FeatureScopeAccount, accountID, key, enabled); err != nil {
		return err
	}
	s.invalidator.InvalidateAccount(ctx, accountID)
	s.log(ctx, "account feature set", "feature", key, "account", accountID, "enabled", enabled)
	return nil
}

// ClearAccountFeature removes one account's override.
func (s *FeatureService) ClearAccountFeature(ctx context.Context, accountID, key string) error {
	if _, err := s.declared(key); err != nil {
		return err
	}
	if accountID == "" {
		return fmt.Errorf("an account is required to clear feature %q: %w", key, ErrValidation)
	}
	if err := s.settings.ClearOverride(ctx, repositories.FeatureScopeAccount, accountID, key); err != nil {
		return err
	}
	s.invalidator.InvalidateAccount(ctx, accountID)
	s.log(ctx, "account feature cleared", "feature", key, "account", accountID)
	return nil
}

// GrantTerms is everything about a grant other than who holds it: when it
// applies, and who made it.
type GrantTerms struct {
	ValidFrom      *time.Time
	ValidThrough   *time.Time
	GrantedByID    string
	GrantedByEmail string
	Source         string
}

// GrantToAgent gives one person a feature within an account.
func (s *FeatureService) GrantToAgent(
	ctx context.Context, accountID, agentID, key string, terms GrantTerms,
) error {
	if err := s.validateGrant(accountID, agentID, key); err != nil {
		return err
	}
	if err := s.validateWindow(terms); err != nil {
		return err
	}
	if err := s.grants.Grant(ctx, s.recordFor(
		repositories.FeatureSubjectAgent, agentID, accountID, key, terms)); err != nil {
		return err
	}
	s.invalidator.InvalidateAgents(ctx, accountID, agentID)
	s.log(ctx, "agent grant made", "feature", key, "account", accountID, "agent", agentID)
	return nil
}

// RevokeFromAgent takes it back, and reports whether there was anything to
// take. The person stays signed in; their next evaluation costs one database
// read and answers off.
func (s *FeatureService) RevokeFromAgent(
	ctx context.Context, accountID, agentID, key string,
) (bool, error) {
	if err := s.validateGrant(accountID, agentID, key); err != nil {
		return false, err
	}
	removed, err := s.grants.Revoke(ctx, repositories.FeatureSubjectAgent, agentID, accountID, key)
	if err != nil {
		return false, err
	}
	if !removed {
		// Nothing changed, so nothing is invalidated. Dropping a cache entry
		// for a revocation that removed nothing would cost every session in
		// the account a re-resolve for no reason.
		return false, nil
	}
	s.invalidator.InvalidateAgents(ctx, accountID, agentID)
	s.log(ctx, "agent grant revoked", "feature", key, "account", accountID, "agent", agentID)
	return true, nil
}

func (s *FeatureService) recordFor(
	subjectType, subjectID, accountID, key string, terms GrantTerms,
) entities.FeatureGrantRecord {
	return entities.FeatureGrantRecord{
		SubjectType:    subjectType,
		SubjectID:      subjectID,
		AccountID:      accountID,
		FeatureKey:     key,
		ValidFrom:      terms.ValidFrom,
		ValidThrough:   terms.ValidThrough,
		GrantedByID:    terms.GrantedByID,
		GrantedByEmail: terms.GrantedByEmail,
		Source:         terms.Source,
	}
}

// validateWindow refuses a window that can never be open. Checked once here so
// all three surfaces refuse it the same way.
func (s *FeatureService) validateWindow(terms GrantTerms) error {
	if terms.ValidFrom == nil || terms.ValidThrough == nil {
		return nil
	}
	if !terms.ValidThrough.After(*terms.ValidFrom) {
		return fmt.Errorf(
			"the window ends before it begins (%s is not after %s): %w",
			terms.ValidThrough.Format(time.RFC3339), terms.ValidFrom.Format(time.RFC3339), ErrValidation)
	}
	return nil
}

// GrantsOnFeature lists every grant on one feature in an account, including
// pending and expired ones.
func (s *FeatureService) GrantsOnFeature(
	ctx context.Context, accountID, key string,
) ([]entities.FeatureGrantRecord, error) {
	if _, err := s.declared(key); err != nil {
		return nil, err
	}
	return s.grants.ListByFeature(ctx, accountID, key)
}

// GrantsFor lists the grants that could apply to one caller.
func (s *FeatureService) GrantsFor(
	ctx context.Context, accountID, agentID, roleID string,
) ([]entities.FeatureGrantRecord, error) {
	return s.grants.GrantsFor(ctx, accountID, agentID, roleID)
}

// Declared exposes a feature's declaration so a surface can order its refusals
// — an unknown key is a 404 and must be answered before anything looks up a
// person or a membership.
func (s *FeatureService) Declared(key string) (entities.FeatureMeta, error) {
	return s.declared(key)
}

// GrantToRole gives a feature to everyone holding a role in an account.
func (s *FeatureService) GrantToRole(
	ctx context.Context, accountID, roleID, key string, terms GrantTerms,
) error {
	if err := s.validateGrant(accountID, roleID, key); err != nil {
		return err
	}
	if err := s.validateWindow(terms); err != nil {
		return err
	}
	if err := s.grants.Grant(ctx, s.recordFor(
		repositories.FeatureSubjectRole, roleID, accountID, key, terms)); err != nil {
		return err
	}
	s.fanOutRoleChange(ctx, accountID, roleID, key, true)
	return nil
}

// RevokeFromRole takes it back from everyone holding the role, and reports
// whether there was anything to take.
func (s *FeatureService) RevokeFromRole(
	ctx context.Context, accountID, roleID, key string,
) (bool, error) {
	if err := s.validateGrant(accountID, roleID, key); err != nil {
		return false, err
	}
	removed, err := s.grants.Revoke(ctx, repositories.FeatureSubjectRole, roleID, accountID, key)
	if err != nil {
		return false, err
	}
	if !removed {
		return false, nil
	}
	s.fanOutRoleChange(ctx, accountID, roleID, key, false)
	return true, nil
}

// changeRoleGrant is the expensive case. One row changes, but the resolved set
// of every member holding that role changes with it, so each of their cache
// entries has to be invalidated individually.
//
// The member list is read AFTER the write, so anyone who joined the role
// concurrently is included rather than missed. A member this query does not
// return keeps a stale answer until the maximum cache age expires, and nothing
// will point at it — which is why the listing query is tested directly.
func (s *FeatureService) fanOutRoleChange(
	ctx context.Context, accountID, roleID, key string, grant bool,
) {
	agentIDs, err := s.members.ListMemberIDsByRole(ctx, accountID, roleID)
	if err != nil {
		// The write landed but the fan-out failed. Fall back to invalidating
		// the whole account: coarser than asked for, but every affected member
		// is certainly reached, and over-invalidation only costs a re-resolve.
		// Leaving the caches alone would strand a stale answer instead.
		s.log(ctx, "could not list role members; invalidating the whole account instead",
			"feature", key, "account", accountID, "role", roleID, "error", err)
		s.invalidator.InvalidateAccount(ctx, accountID)
		return
	}

	s.invalidator.InvalidateAgents(ctx, accountID, agentIDs...)
	s.log(ctx, "role grant changed", "feature", key, "account", accountID,
		"role", roleID, "granted", grant, "membersInvalidated", len(agentIDs))
}

func (s *FeatureService) validateGrant(accountID, subjectID, key string) error {
	meta, err := s.declared(key)
	if err != nil {
		return err
	}
	if accountID == "" || subjectID == "" {
		return fmt.Errorf("an account and a subject are required to grant feature %q: %w", key, ErrValidation)
	}
	if !meta.Grantable {
		return fmt.Errorf("feature %q cannot be granted: %w", key, ErrValidation)
	}
	return nil
}

func (s *FeatureService) declared(key string) (entities.FeatureMeta, error) {
	meta, ok := s.registry.Lookup(key)
	if !ok {
		// Wrapped so a handler can answer 404 without matching on the message.
		// An undeclared key is genuinely "not found" rather than a bad
		// request: the caller named something that does not exist.
		return entities.FeatureMeta{}, fmt.Errorf(
			"no feature named %q is declared: %w", key, repositories.ErrNotFound)
	}
	return meta, nil
}

func (s *FeatureService) log(ctx context.Context, msg string, kv ...any) {
	if s.logger == nil {
		return
	}
	s.logger.Info(ctx, msg, kv...)
}
