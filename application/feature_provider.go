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
	"strings"
	"sync"

	"github.com/open-feature/go-sdk/openfeature"

	"github.com/wepala/weos/v3/domain/entities"
)

// FeatureFlagPrefix is the key namespace this provider answers. Call sites ask
// for a feature as:
//
//	client.Boolean(ctx, application.FeatureFlagPrefix+"episodic-recall", false, evalCtx)
//
// A key outside the namespace falls through with the caller's own default and
// reason DEFAULT, so another provider can be composed alongside this one
// without either surprising the other. Apollo's entitlement provider uses the
// same convention on the "entitlement." prefix.
const FeatureFlagPrefix = "feature."

// featureProviderName appears in OpenFeature metadata and in any hook
// telemetry. Stable as an operations contract.
const featureProviderName = "weos-features"

// FeatureProviderDomain is the OpenFeature domain this provider binds to.
//
// Bound to a named domain rather than the global provider slot on purpose. The
// global slot is process-wide mutable state, and the godog suites each boot
// their own fx application inside one test binary — a stale global
// registration would silently evaluate against a previous application's
// resolver, producing a suite that passes alone and fails in a full run. It
// also leaves room for a second provider (Apollo's "entitlement." namespace,
// or a remote flag source) to be registered on its own domain later.
const FeatureProviderDomain = "weos"

// Compile-time proof the adapter is complete: a missing evaluation method
// would otherwise only surface when a call site asked for that kind.
var _ openfeature.FeatureProvider = (*FeatureProvider)(nil)

// FeatureProvider adapts the resolver to OpenFeature. It is in-process and
// stateless beyond a log-once set: every evaluation reads the caller's
// resolved set, which the resolver caches.
//
// Only boolean evaluation does real work. String, integer, float and object
// evaluations return the caller's supplied default with reason DEFAULT, so a
// later flag source that does carry richer values can be layered in without
// surprising any existing call site.
type FeatureProvider struct {
	resolver *FeatureResolver
	logger   entities.Logger
	// loggedUnknown remembers which undeclared keys have already been logged.
	// An undeclared key on a hot path — an agent turn asking thirty times —
	// must not produce thirty identical log lines, or the signal that a deploy
	// drifted drowns in its own repetition.
	loggedUnknown sync.Map
}

// NewFeatureProvider builds the provider.
func NewFeatureProvider(resolver *FeatureResolver, logger entities.Logger) *FeatureProvider {
	return &FeatureProvider{resolver: resolver, logger: logger}
}

func (p *FeatureProvider) Metadata() openfeature.Metadata {
	return openfeature.Metadata{Name: featureProviderName}
}

// Hooks returns nil. The provider installs no global hooks; a caller that
// wants eventing adds its own at the client level.
func (p *FeatureProvider) Hooks() []openfeature.Hook {
	return nil
}

// BooleanEvaluation answers "is this feature on for the caller?".
//
// The evaluation context is deliberately ignored. OpenFeature's targeting key
// is the SDK's portable mechanism for per-request data, but the caller's
// identity already travels on ctx, placed there by the auth middleware for the
// whole request pipeline. Accepting it from both would create two sources of
// truth that can disagree — and the one on ctx is the one the rest of the
// system authorises against.
func (p *FeatureProvider) BooleanEvaluation(
	ctx context.Context, flag string, defaultValue bool, _ openfeature.FlattenedContext,
) openfeature.BoolResolutionDetail {
	if !strings.HasPrefix(flag, FeatureFlagPrefix) {
		return boolDetail(defaultValue, openfeature.DefaultReason)
	}
	key := strings.TrimPrefix(flag, FeatureFlagPrefix)
	if key == "" {
		return boolDetail(defaultValue, openfeature.DefaultReason)
	}

	value, declared, err := p.resolver.Enabled(ctx, key)
	switch {
	case err != nil:
		// Fail closed. Not the caller's default — off. A resolver that
		// answered "on" while the store was unreadable would hand out the
		// capability at exactly the moment nobody could see why.
		//
		// Reason is ERROR so hooks and telemetry see the failure, but NO
		// ResolutionError is attached, and that is deliberate. The OpenFeature
		// client treats a resolution error as "fall back to the caller's
		// default", which would hand `true` to any call site that passed
		// `true` — the exact opposite of failing closed, and invisible to a
		// unit test that calls this provider directly instead of going through
		// the client. The failure is surfaced through the log line below
		// rather than through a mechanism that would overrule the value.
		p.log(ctx, "feature evaluation failed, answering off", "feature", key, "error", err)
		return boolDetail(false, openfeature.ErrorReason)
	case !declared:
		// Registry drift: a call site and the declarations disagree. The
		// caller gets what it asked for, and the instance says so once.
		p.logUnknownOnce(ctx, key)
		return boolDetail(defaultValue, openfeature.DefaultReason)
	default:
		return boolDetail(value, openfeature.TargetingMatchReason)
	}
}

// StringEvaluation returns the caller's default. See FeatureProvider.
func (p *FeatureProvider) StringEvaluation(
	_ context.Context, _ string, defaultValue string, _ openfeature.FlattenedContext,
) openfeature.StringResolutionDetail {
	return openfeature.StringResolutionDetail{
		Value:                    defaultValue,
		ProviderResolutionDetail: openfeature.ProviderResolutionDetail{Reason: openfeature.DefaultReason},
	}
}

// FloatEvaluation returns the caller's default. See FeatureProvider.
func (p *FeatureProvider) FloatEvaluation(
	_ context.Context, _ string, defaultValue float64, _ openfeature.FlattenedContext,
) openfeature.FloatResolutionDetail {
	return openfeature.FloatResolutionDetail{
		Value:                    defaultValue,
		ProviderResolutionDetail: openfeature.ProviderResolutionDetail{Reason: openfeature.DefaultReason},
	}
}

// IntEvaluation returns the caller's default. See FeatureProvider.
func (p *FeatureProvider) IntEvaluation(
	_ context.Context, _ string, defaultValue int64, _ openfeature.FlattenedContext,
) openfeature.IntResolutionDetail {
	return openfeature.IntResolutionDetail{
		Value:                    defaultValue,
		ProviderResolutionDetail: openfeature.ProviderResolutionDetail{Reason: openfeature.DefaultReason},
	}
}

// ObjectEvaluation returns the caller's default. See FeatureProvider.
func (p *FeatureProvider) ObjectEvaluation(
	_ context.Context, _ string, defaultValue any, _ openfeature.FlattenedContext,
) openfeature.InterfaceResolutionDetail {
	return openfeature.InterfaceResolutionDetail{
		Value:                    defaultValue,
		ProviderResolutionDetail: openfeature.ProviderResolutionDetail{Reason: openfeature.DefaultReason},
	}
}

func boolDetail(value bool, reason openfeature.Reason) openfeature.BoolResolutionDetail {
	return openfeature.BoolResolutionDetail{
		Value:                    value,
		ProviderResolutionDetail: openfeature.ProviderResolutionDetail{Reason: reason},
	}
}

func (p *FeatureProvider) logUnknownOnce(ctx context.Context, key string) {
	if _, seen := p.loggedUnknown.LoadOrStore(key, struct{}{}); seen {
		return
	}
	p.log(ctx, "evaluated a feature nobody declared; the deploy has drifted from its registry",
		"feature", key)
}

func (p *FeatureProvider) log(ctx context.Context, msg string, kv ...any) {
	if p.logger == nil {
		return
	}
	p.logger.Warn(ctx, msg, kv...)
}

// RegisterFeatureProvider binds the provider to its OpenFeature domain and
// returns a client for it.
//
// This is wired as an fx.Invoke rather than left to a consumer, and that is
// load-bearing: fx is lazy, so a provider that nothing depends on is never
// constructed. Before this existed the provider was built only by tests that
// populated it directly, the "weos" domain fell through to the SDK's no-op
// provider in the real server, and every evaluation returned the caller's
// default — the whole subsystem inert in the shipped binary while every test
// passed.
func RegisterFeatureProvider(p *FeatureProvider, logger entities.Logger) (*openfeature.Client, error) {
	if err := openfeature.SetNamedProviderAndWait(FeatureProviderDomain, p); err != nil {
		return nil, fmt.Errorf("failed to register the feature provider: %w", err)
	}
	if logger != nil {
		logger.Debug(context.Background(), "feature provider registered",
			"domain", FeatureProviderDomain, "provider", featureProviderName)
	}
	return openfeature.NewClient(FeatureProviderDomain), nil
}
