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

package repositories

import (
	"context"

	"github.com/wepala/weos/v3/domain/entities"
)

// Override scopes. The instance scope has no identifier — there is one
// instance — so its rows carry an empty ScopeID.
const (
	FeatureScopeInstance = "instance"
	FeatureScopeAccount  = "account"
)

// Grant subjects. A grant names either one agent or one role; roles are
// per-account memberships, so both are stored with the owning account.
const (
	FeatureSubjectAgent = "agent"
	FeatureSubjectRole  = "role"
)

// FeatureSettingsRepository persists the two override layers — instance and
// account. Both layers have identical shape, so they share a table
// discriminated by scope.
//
// Presence of a row IS the explicit state and absence IS "unset". That is why
// setting and clearing are separate methods rather than one method taking a
// *bool: a nil-versus-false slip at a call site would turn "stop overriding
// this" into "turn this off for everyone", which is the single most damaging
// mistake available in this subsystem.
type FeatureSettingsRepository interface {
	// InstanceOverrides returns every explicit instance-level value, keyed by
	// feature key. An absent key means the instance layer says nothing.
	//
	// Deliberately separate from AccountOverrides rather than one call taking
	// an optional account: resolution for a caller with no identity must read
	// the instance layer and nothing else, and keeping the reads separate is
	// what makes that observable to a test rather than merely intended.
	InstanceOverrides(ctx context.Context) (map[string]bool, error)

	// AccountOverrides returns every explicit value for one account.
	AccountOverrides(ctx context.Context, accountID string) (map[string]bool, error)

	// SetOverride writes an explicit value for a scope, replacing any existing
	// one. scopeID is empty for FeatureScopeInstance.
	SetOverride(ctx context.Context, scopeType, scopeID, featureKey string, enabled bool) error

	// ClearOverride removes the row, returning the layer to "says nothing" so
	// the layer above it decides. Clearing a row that does not exist is not an
	// error — the caller's intent is already satisfied.
	ClearOverride(ctx context.Context, scopeType, scopeID, featureKey string) error
}

// FeatureGrantRepository persists the bottom layer. A grant is a fact with a
// subject, not a value: grants only ever turn a feature on, so there is no
// enabled column and revoking means deleting the row. An override is what says
// "off".
//
// Kept separate from FeatureSettingsRepository because the two diverge
// immediately: #483 adds validity windows and provenance to grants alone,
// which would be permanently null on every instance-scoped override row.
type FeatureGrantRepository interface {
	// GrantsFor returns the grant ROWS that could apply to this caller within
	// an account — the grant made to them directly, and the one carried by
	// their role. roleID may be empty when the caller holds no role.
	//
	// Rows outside their validity window are returned too, deliberately. A
	// grant that has not started yet is a boundary the resolver has to know
	// about so it can expire its cached answer at the right instant, and a
	// grant that has closed is still a row a listing has to be able to show.
	// Filtering in SQL would hide both.
	GrantsFor(ctx context.Context, accountID, agentID, roleID string) ([]entities.FeatureGrantRecord, error)

	// ListByFeature returns every grant on one feature within an account, for
	// the admin listing. Includes pending and expired rows.
	ListByFeature(ctx context.Context, accountID, featureKey string) ([]entities.FeatureGrantRecord, error)

	// Grant records a grant, replacing the window and provenance of one that
	// already exists. Granting twice leaves one grant, not two.
	Grant(ctx context.Context, record entities.FeatureGrantRecord) error

	// Revoke removes a grant and reports whether one was there. Revoking a
	// grant nobody holds is not an error — the caller's intent is already
	// satisfied — but nothing is recorded and no cache is disturbed for a
	// change that did not happen.
	Revoke(ctx context.Context, subjectType, subjectID, accountID, featureKey string) (bool, error)
}

// AccountMemberQuery reads pericarp's account_members projection for the one
// question feature invalidation needs and pericarp's own AccountRepository
// cannot answer: who holds this role?
//
// It exists because a role grant changes the resolved set of every member
// carrying that role, and each member's cache entry must be invalidated
// individually. pericarp offers FindMemberRole, FindByMember, SaveMember and
// RemoveMember — all agent-first — so the account-first direction is ours to
// add. It is a read-only port over a table pericarp owns; nothing here writes.
type AccountMemberQuery interface {
	// ListMemberIDsByRole returns the agent IDs holding roleID in accountID.
	// An empty result is not an error: a role nobody holds invalidates nothing.
	ListMemberIDsByRole(ctx context.Context, accountID, roleID string) ([]string, error)
}
