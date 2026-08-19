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
		return fmt.Errorf("an account is required to override feature %q", key)
	}
	// Refused at the write rather than only ignored at the read. The resolver
	// ignores an ineligible row regardless — stored rows outlive declaration
	// changes — but an operator who asks for something impossible deserves to
	// be told, not silently obeyed and then overruled.
	if !meta.Manageable {
		return fmt.Errorf("feature %q cannot be changed per account", key)
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
		return fmt.Errorf("an account is required to clear feature %q", key)
	}
	if err := s.settings.ClearOverride(ctx, repositories.FeatureScopeAccount, accountID, key); err != nil {
		return err
	}
	s.invalidator.InvalidateAccount(ctx, accountID)
	s.log(ctx, "account feature cleared", "feature", key, "account", accountID)
	return nil
}

// GrantToAgent gives one person a feature within an account.
func (s *FeatureService) GrantToAgent(ctx context.Context, accountID, agentID, key string) error {
	return s.changeAgentGrant(ctx, accountID, agentID, key, true)
}

// RevokeFromAgent takes it back. The person stays signed in; their next
// evaluation costs one database read and answers off.
func (s *FeatureService) RevokeFromAgent(ctx context.Context, accountID, agentID, key string) error {
	return s.changeAgentGrant(ctx, accountID, agentID, key, false)
}

func (s *FeatureService) changeAgentGrant(
	ctx context.Context, accountID, agentID, key string, grant bool,
) error {
	if err := s.validateGrant(accountID, agentID, key); err != nil {
		return err
	}
	var err error
	if grant {
		err = s.grants.Grant(ctx, repositories.FeatureSubjectAgent, agentID, accountID, key)
	} else {
		err = s.grants.Revoke(ctx, repositories.FeatureSubjectAgent, agentID, accountID, key)
	}
	if err != nil {
		return err
	}
	s.invalidator.InvalidateAgents(ctx, accountID, agentID)
	s.log(ctx, "agent grant changed", "feature", key, "account", accountID, "agent", agentID, "granted", grant)
	return nil
}

// GrantToRole gives a feature to everyone holding a role in an account.
func (s *FeatureService) GrantToRole(ctx context.Context, accountID, roleID, key string) error {
	return s.changeRoleGrant(ctx, accountID, roleID, key, true)
}

// RevokeFromRole takes it back from everyone holding the role.
func (s *FeatureService) RevokeFromRole(ctx context.Context, accountID, roleID, key string) error {
	return s.changeRoleGrant(ctx, accountID, roleID, key, false)
}

// changeRoleGrant is the expensive case. One row changes, but the resolved set
// of every member holding that role changes with it, so each of their cache
// entries has to be invalidated individually.
//
// The member list is read AFTER the write, so anyone who joined the role
// concurrently is included rather than missed. A member this query does not
// return keeps a stale answer until the maximum cache age expires, and nothing
// will point at it — which is why the listing query is tested directly.
func (s *FeatureService) changeRoleGrant(
	ctx context.Context, accountID, roleID, key string, grant bool,
) error {
	if err := s.validateGrant(accountID, roleID, key); err != nil {
		return err
	}
	var err error
	if grant {
		err = s.grants.Grant(ctx, repositories.FeatureSubjectRole, roleID, accountID, key)
	} else {
		err = s.grants.Revoke(ctx, repositories.FeatureSubjectRole, roleID, accountID, key)
	}
	if err != nil {
		return err
	}

	agentIDs, err := s.members.ListMemberIDsByRole(ctx, accountID, roleID)
	if err != nil {
		// The write landed but the fan-out failed. Fall back to invalidating
		// the whole account: coarser than asked for, but every affected member
		// is certainly reached, and over-invalidation only costs a re-resolve.
		// Leaving the caches alone would strand a stale answer instead.
		s.log(ctx, "could not list role members; invalidating the whole account instead",
			"feature", key, "account", accountID, "role", roleID, "error", err)
		s.invalidator.InvalidateAccount(ctx, accountID)
		return nil
	}

	s.invalidator.InvalidateAgents(ctx, accountID, agentIDs...)
	s.log(ctx, "role grant changed", "feature", key, "account", accountID,
		"role", roleID, "granted", grant, "membersInvalidated", len(agentIDs))
	return nil
}

func (s *FeatureService) validateGrant(accountID, subjectID, key string) error {
	meta, err := s.declared(key)
	if err != nil {
		return err
	}
	if accountID == "" || subjectID == "" {
		return fmt.Errorf("an account and a subject are required to grant feature %q", key)
	}
	if !meta.Grantable {
		return fmt.Errorf("feature %q cannot be granted", key)
	}
	return nil
}

func (s *FeatureService) declared(key string) (entities.FeatureMeta, error) {
	meta, ok := s.registry.Lookup(key)
	if !ok {
		return entities.FeatureMeta{}, fmt.Errorf("no feature named %q is declared", key)
	}
	return meta, nil
}

func (s *FeatureService) log(ctx context.Context, msg string, kv ...any) {
	if s.logger == nil {
		return
	}
	s.logger.Info(ctx, msg, kv...)
}
