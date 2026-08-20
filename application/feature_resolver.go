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
	// retryAfter throttles re-reads while the store is failing. Without it an
	// agent turn making thirty evaluations against an unreadable database
	// makes thirty failing round trips, turning a blip into a query storm at
	// the worst moment.
	retryAfter time.Time
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
	// generation increments on every invalidation. A resolve that started
	// before an invalidation and finished after it must not store its result:
	// it read the database before the write landed, so caching it would give
	// stale values a fresh timestamp and break the one rule this design has —
	// that a change reaches sessions already open. The window is widest
	// exactly when the instance is busy.
	generation uint64
	// now is injectable so the max-age behavior can be tested without
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
		// Declared after this set was cached — the set is stale, not the
		// registry. Re-resolve rather than answering meta.Default, which would
		// bypass every stored layer and could serve ON past an explicit
		// instance OFF for the lifetime of the cache entry.
		fresh, err := r.resolveNow(ctx)
		if err != nil {
			return false, true, err
		}
		if value, ok := fresh[key]; ok {
			return value, true, nil
		}
		return meta.Default, true, nil
	}
	return value, true, nil
}

// ResolvedSet returns every declared feature's value for the caller on ctx,
// reading from cache when one is present and young enough.
//
// The whole set is resolved at once rather than one key at a time. An agent
// turn evaluates many keys, and resolving per key would multiply the database
// reads by the number of features rather than amortizing them to one.
func (r *FeatureResolver) ResolvedSet(ctx context.Context) (map[string]bool, error) {
	identity := auth.AgentFromCtx(ctx)
	key := featureCacheKey{}
	if identity != nil {
		key = featureCacheKey{AgentID: identity.AgentID, AccountID: identity.ActiveAccountID}
	}

	if set, ok := r.cached(key); ok {
		return set, nil
	}
	return r.refresh(ctx, key)
}

// resolveNow bypasses the cache entirely. Used when a cached set turns out not
// to contain a key it should — the set is stale and must be rebuilt rather
// than guessed around.
func (r *FeatureResolver) resolveNow(ctx context.Context) (map[string]bool, error) {
	identity := auth.AgentFromCtx(ctx)
	key := featureCacheKey{}
	if identity != nil {
		key = featureCacheKey{AgentID: identity.AgentID, AccountID: identity.ActiveAccountID}
	}
	return r.refresh(ctx, key)
}

// refresh reads every layer and stores the result, unless an invalidation
// overtook it.
func (r *FeatureResolver) refresh(ctx context.Context, key featureCacheKey) (map[string]bool, error) {
	r.mu.RLock()
	startedAt := r.generation
	r.mu.RUnlock()

	values, err := r.resolve(ctx, key)
	if err != nil {
		return r.onResolveError(ctx, key, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.generation != startedAt {
		// An invalidation landed while this read was in flight, so these
		// values may predate the write that caused it. Serve them to this
		// caller — they are no more stale than the read itself — but do not
		// store them, or the next caller inherits a stale set with a fresh
		// timestamp.
		return copyFeatureValues(values), nil
	}
	r.sets[key] = resolvedFeatureSet{values: values, resolvedAt: r.clock()}
	return copyFeatureValues(values), nil
}

// onResolveError decides what a caller is told when the store cannot be read.
//
// A failure is never cached as an answer. But if this caller already has a
// resolved set, serving it beats answering off for everything: a database blip
// would otherwise strip every capability from every user mid-turn — including
// features nobody ever turned off — and once #484 gates tool listings, clients
// cache the emptied list and the outage outlives the blip.
//
// With no previous set there is nothing to fall back to, so the answer is off.
// That is the contract's case: a store unreadable from the start resolves off
// with reason ERROR.
func (r *FeatureResolver) onResolveError(
	ctx context.Context, key featureCacheKey, err error,
) (map[string]bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	previous, ok := r.sets[key]
	if !ok {
		return nil, err
	}
	// Throttle the retry so a sustained outage does not turn every evaluation
	// into another failing round trip.
	previous.retryAfter = r.clock().Add(resolveFailureBackoff)
	r.sets[key] = previous
	if r.logger != nil {
		r.logger.Warn(ctx, "serving the last known feature set; the store could not be read",
			"agent", key.AgentID, "account", key.AccountID, "error", err)
	}
	return copyFeatureValues(previous.values), nil
}

// resolveFailureBackoff is how long a failed refresh suppresses another
// attempt for the same caller.
const resolveFailureBackoff = time.Second

func (r *FeatureResolver) cached(key featureCacheKey) (map[string]bool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	set, ok := r.sets[key]
	if !ok {
		return nil, false
	}
	now := r.clock()
	if now.Sub(set.resolvedAt) >= r.maxAge {
		// Too old to trust. Treated as a miss rather than evicted here so the
		// read path keeps only a read lock; the entry is replaced on write.
		// Unless a refresh just failed — then serving this set is better than
		// hammering a store that is down.
		if set.retryAfter.After(now) {
			return copyFeatureValues(set.values), true
		}
		return nil, false
	}
	return copyFeatureValues(set.values), true
}

// resolve reads every layer and folds them into a plain value map for the
// cache. It is Explain's answer with the layer information dropped, so the two
// cannot disagree about what a feature resolves to.
func (r *FeatureResolver) resolve(ctx context.Context, key featureCacheKey) (map[string]bool, error) {
	statuses, err := r.resolveDetailed(ctx, key)
	if err != nil {
		return nil, err
	}
	values := make(map[string]bool, len(statuses))
	for _, s := range statuses {
		values[s.Key] = s.Enabled
	}
	return values, nil
}

// Explain resolves every declared feature for the caller on ctx and reports
// which layer decided each one.
//
// Deliberately uncached, and it does not populate the cache. The cached set
// holds values only — adding the deciding layer to it would grow every entry
// to serve a listing that an operator hits occasionally and a hot path never
// does. Listings are rare; evaluations are not.
func (r *FeatureResolver) Explain(ctx context.Context) ([]entities.FeatureStatus, error) {
	identity := auth.AgentFromCtx(ctx)
	key := featureCacheKey{}
	if identity != nil {
		key = featureCacheKey{AgentID: identity.AgentID, AccountID: identity.ActiveAccountID}
	}
	return r.resolveDetailed(ctx, key)
}

// resolveDetailed reads every layer and folds them through
// entities.ResolveFeature, keeping the layer each answer came from.
//
// This is the single place the layers are read. resolve() maps its output
// down for the cache and Explain() returns it whole, so the value a call site
// evaluates and the source an operator is shown can never drift apart — which
// is exactly what a listing is for.
func (r *FeatureResolver) resolveDetailed(
	ctx context.Context, key featureCacheKey,
) ([]entities.FeatureStatus, error) {
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
	out := make([]entities.FeatureStatus, 0, len(declared))
	for _, meta := range declared {
		value, decidedBy := entities.ResolveFeature(
			meta,
			featureStateFrom(instance, meta.Key),
			featureStateFrom(account, meta.Key),
			granted[meta.Key],
		)
		out = append(out, entities.FeatureStatus{
			Key:         meta.Key,
			DisplayName: meta.DisplayName,
			Description: meta.Description,
			Enabled:     value,
			Source:      decidedBy.String(),
			Default:     meta.Default,
			Manageable:  meta.Manageable,
			Grantable:   meta.Grantable,
		})
	}
	return out, nil
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

// expire ages a cached entry past the maximum, so a test can force the next
// read to attempt a refresh without sleeping.
func (r *FeatureResolver) expire(key featureCacheKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if set, ok := r.sets[key]; ok {
		set.resolvedAt = r.clock().Add(-2 * r.maxAge)
		set.retryAfter = time.Time{}
		r.sets[key] = set
	}
}

// InvalidateAll drops every cached set. Used when an instance-level value
// changes, and when a replica is told that something changed elsewhere.
func (r *FeatureResolver) InvalidateAll(_ context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.generation++
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
	r.generation++
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
	r.generation++
	for _, agentID := range agentIDs {
		delete(r.sets, featureCacheKey{AgentID: agentID, AccountID: accountID})
	}
}
