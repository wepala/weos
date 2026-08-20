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

	"github.com/open-feature/go-sdk/openfeature"

	"github.com/wepala/weos/v3/domain/entities"
)

// ToolFeatureGate answers whether a gated capability is available to the
// caller on ctx. The MCP tool surface is its first consumer (#484), and
// nothing about it is MCP-specific — any call site gating on a feature can
// use it.
//
// It evaluates through the OpenFeature client rather than reaching for the
// resolver, so a call site gets the epic's whole contract for free: the
// per-caller cache, the layer precedence, the fail-closed store error, and
// the log-once on registry drift all live behind one boolean.
//
// The default passed to the client is `true`, and that is the load-bearing
// detail. It is NOT a fail-open: a store that cannot be read resolves off
// through the provider's ERROR reason, which the client does not overrule,
// so a gated capability still disappears during an outage. The default is
// reached only when nobody declared the key — registry drift, where a deploy's
// call sites and declarations disagree — and there the answer must be to leave
// the capability exactly as it was. The alternative, closing every gate whose
// key does not resolve, turns one mistyped constant into a silent outage
// across an instance. The provider logs the drift once so it is fixable.
func ToolFeatureGate(client *openfeature.Client) func(context.Context, string) bool {
	if client == nil {
		return nil
	}
	return func(ctx context.Context, featureKey string) bool {
		if featureKey == "" {
			return true
		}
		details, err := client.BooleanValueDetails(
			ctx, FeatureFlagPrefix+featureKey, true, openfeature.EvaluationContext{})
		if err != nil {
			// Closed. This is the one path the provider cannot answer for
			// itself: the client short-circuits BEFORE calling the provider
			// when the domain's provider is NOT_READY or FATAL, and returns
			// the caller's default — which is `true` here. Client.Boolean
			// discards that error, so using it would open every gate exactly
			// when the flag source is unavailable.
			//
			// SetNamedProviderAndWait makes that unreachable today. It becomes
			// reachable the moment anyone switches to the async setter, gives
			// the provider a state handler, or re-registers on the domain at
			// runtime — and nothing would fail to say so. Failing closed here
			// stops the guarantee depending on a client internal.
			return false
		}
		// ERROR already carries false from the provider; DEFAULT carries the
		// caller's `true`, which is the undeclared-key case.
		return details.Value
	}
}

// HasCallerIdentity reports whether ctx carries an authenticated caller.
//
// Kept as the application-layer name for entities.HasCallerIdentity, which is
// where every gating surface reads it from. One definition, because it decides
// how a refusal is worded and a second copy would drift.
func HasCallerIdentity(ctx context.Context) bool {
	return entities.HasCallerIdentity(ctx)
}
