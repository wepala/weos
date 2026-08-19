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
	"sync"
	"time"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"
)

// The resolver IS the in-process invalidator. Asserted here so the two never
// drift apart silently.
var _ repositories.FeatureCacheInvalidator = (*FeatureResolver)(nil)

// featureCacheKey identifies one resolved feature set.
//
// Deliberately NOT a session identifier. There is none on the MCP bearer
// surface — BearerOrSession validates the JWT and keeps only the identity, so
// the token's jti never reaches a handler — and keying on something one whole
// surface cannot supply would mean either no caching there or a silent
// fallback that behaves differently per surface.
//
// Keying on (agent, account) is also strictly more correct than per-session
// would be: resolution reads the instance layer, the account layer, and the
// caller's grants and roles, and nothing else. Two sessions of one person in
// one account therefore always resolve identically, so sharing one entry
// cannot produce a wrong answer — and it means one invalidation reaches every
// device that person is signed in on, including MCP connectors that hold no
// enumerable server-side session at all.
//
// The zero key — both fields empty — is the anonymous caller, whose set is
// resolved from the instance layer alone.
type featureCacheKey struct {
	AgentID   string
	AccountID string
}

type resolvedFeatureSet struct {
	values     map[string]bool
	resolvedAt time.Time
}

// FeatureResolver answers what features are on for a caller, and owns the
// cache that keeps that answer cheap. Resolution sits in front of MCP tool
// listings and agent turns, where thirty tool calls in one turn is ordinary,
// so a caller's whole set is resolved once and read from memory afterwards.
//
// The resolver is itself the in-process FeatureCacheInvalidator. On SQLite —
// one process, serialized writers — that is not a fallback or a degraded mode,
// it is the complete and exact implementation. The Postgres broadcast wraps
// it rather than replacing it.
type FeatureResolver struct {
	registry *FeatureRegistry
	settings repositories.FeatureSettingsRepository
	grants   repositories.FeatureGrantRepository
	accounts authrepos.AccountRepository
	logger   entities.Logger

	maxAge time.Duration

	mu   sync.RWMutex
	sets map[featureCacheKey]resolvedFeatureSet
	// now is injectable so the max-age behaviour can be tested without
	// sleeping. Production leaves it nil and uses time.Now.
	now func() time.Time
}

// NewFeatureResolver builds the resolver. maxAge comes from configuration and
// bounds how long a cached set may be served without re-resolution.
func NewFeatureResolver(
	cfg config.Config,
	registry *FeatureRegistry,
	settings repositories.FeatureSettingsRepository,
	grants repositories.FeatureGrantRepository,
	accounts authrepos.AccountRepository,
	logger entities.Logger,
) *FeatureResolver {
	maxAge := cfg.Features.CacheMaxAge
	if maxAge <= 0 {
		maxAge = 15 * time.Minute
	}
	return &FeatureResolver{
		registry: registry,
		settings: settings,
		grants:   grants,
		accounts: accounts,
		logger:   logger,
		maxAge:   maxAge,
		sets:     make(map[featureCacheKey]resolvedFeatureSet),
	}
}

func (r *FeatureResolver) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// Enabled answers whether one feature is on for the caller on ctx.
//
// The second result reports whether the key is declared at all. An undeclared
// key is registry drift — a deploy where a call site and the declarations
// disagree — and the caller decides what to do about it; the resolver does not
// invent a value for it.
//
// An error means the stored state could not be read. Callers must treat that
// as OFF: a resolver that answers "on" on the way to a database error hands
// out the capability at exactly the moment nobody can see why.
func (r *FeatureResolver) Enabled(ctx context.Context, key string) (bool, bool, error) {
	meta, declared := r.registry.Lookup(key)
	if !declared {
		return false, false, nil
	}
	set, err := r.ResolvedSet(ctx)
	if err != nil {
		return false, true, err
	}
	value, ok := set[key]
	if !ok {
		// Declared after this set was cached. Fall back to the declaration
		// rather than reporting it undeclared — the set is stale, not the
		// registry.
		return meta.Default, true, nil
	}
	return value, true, nil
}

// ResolvedSet returns every declared feature's value for the caller on ctx,
// reading from cache when one is present and young enough.
//
// The whole set is resolved at once rather than one key at a time. An agent
// turn evaluates many keys, and resolving per key would multiply the database
// reads by the number of features rather than amortising them to one.
func (r *FeatureResolver) ResolvedSet(ctx context.Context) (map[string]bool, error) {
	identity := auth.AgentFromCtx(ctx)
	key := featureCacheKey{}
	if identity != nil {
		key = featureCacheKey{AgentID: identity.AgentID, AccountID: identity.ActiveAccountID}
	}

	if set, ok := r.cached(key); ok {
		return set, nil
	}

	values, err := r.resolve(ctx, key)
	if err != nil {
		// A failure is never cached. Remembering it would turn one unreadable
		// moment into a persistently-off feature long after the store
		// recovered.
		return nil, err
	}

	r.mu.Lock()
	r.sets[key] = resolvedFeatureSet{values: values, resolvedAt: r.clock()}
	r.mu.Unlock()

	return copyFeatureValues(values), nil
}

func (r *FeatureResolver) cached(key featureCacheKey) (map[string]bool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	set, ok := r.sets[key]
	if !ok {
		return nil, false
	}
	if r.clock().Sub(set.resolvedAt) >= r.maxAge {
		// Too old to trust. Treated as a miss rather than evicted here so the
		// read path keeps only a read lock; the entry is replaced on write.
		return nil, false
	}
	return copyFeatureValues(set.values), true
}

// resolve reads every layer and folds them through entities.ResolveFeature.
func (r *FeatureResolver) resolve(ctx context.Context, key featureCacheKey) (map[string]bool, error) {
	instance, err := r.settings.InstanceOverrides(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve features: %w", err)
	}

	var (
		account map[string]bool
		granted map[string]bool
	)
	// An anonymous caller resolves from the instance layer alone. No account
	// override and no grant is read — there is no subject for either to
	// attach to. This deliberately differs from the rest of the codebase,
	// where a nil identity means system context and bypasses gates: a
	// background worker or a stdio MCP session must not silently receive every
	// gated capability.
	if key.AgentID != "" && key.AccountID != "" {
		account, err = r.settings.AccountOverrides(ctx, key.AccountID)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve features: %w", err)
		}
		roleID, err := r.accounts.FindMemberRole(ctx, key.AccountID, key.AgentID)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve features: %w", err)
		}
		granted, err = r.grants.GrantedKeys(ctx, key.AccountID, key.AgentID, roleID)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve features: %w", err)
		}
	}

	declared := r.registry.All()
	values := make(map[string]bool, len(declared))
	for _, meta := range declared {
		values[meta.Key] = entities.ResolveFeature(
			meta,
			featureStateFrom(instance, meta.Key),
			featureStateFrom(account, meta.Key),
			granted[meta.Key],
		)
	}
	return values, nil
}

// featureStateFrom turns a stored override map into a tri-state. An absent key
// is Unset — the layer says nothing — which is what lets row absence carry the
// third state with no nullable column.
func featureStateFrom(overrides map[string]bool, key string) entities.FeatureState {
	enabled, ok := overrides[key]
	if !ok {
		return entities.FeatureUnset
	}
	if enabled {
		return entities.FeatureOn
	}
	return entities.FeatureOff
}

func copyFeatureValues(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// InvalidateAll drops every cached set. Used when an instance-level value
// changes, and when a replica is told that something changed elsewhere.
func (r *FeatureResolver) InvalidateAll(_ context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sets = make(map[featureCacheKey]resolvedFeatureSet)
}

// InvalidateAccount drops every cached set belonging to one account, reaching
// every member signed in right now without disturbing any other account.
func (r *FeatureResolver) InvalidateAccount(_ context.Context, accountID string) {
	if accountID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for key := range r.sets {
		if key.AccountID == accountID {
			delete(r.sets, key)
		}
	}
}

// InvalidateAgents drops the cached sets of specific people within an account.
// Every session those people hold is reached, because sessions share one entry
// per (agent, account) — that is the property the cache key was chosen for.
func (r *FeatureResolver) InvalidateAgents(_ context.Context, accountID string, agentIDs ...string) {
	if accountID == "" || len(agentIDs) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, agentID := range agentIDs {
		delete(r.sets, featureCacheKey{AgentID: agentID, AccountID: accountID})
	}
}
