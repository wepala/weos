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
		return client.Boolean(ctx, FeatureFlagPrefix+featureKey, true, openfeature.EvaluationContext{})
	}
}
