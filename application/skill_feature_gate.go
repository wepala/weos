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
	"sync"

	"github.com/open-feature/go-sdk/openfeature"

	"github.com/wepala/weos/v3/application/agents"
	"github.com/wepala/weos/v3/domain/entities"
)

// SkillFeatureGate answers whether an agent skill's feature is on for the
// caller (#485). It is ToolFeatureGate pointed at a different surface — the
// same OpenFeature client, the same per-caller resolved set, the same
// precedence — with one addition: it can name the skill in the drift log.
//
// A skill's gate key is authored in data, through the API, through MCP, or in
// a seed file, where no compiler and no reviewer ever sees it. So a key nobody
// declared is likelier here than it is for a tool, and the log line has to
// carry the skill's name or an operator cannot find which resource to fix.
//
// The rule itself is #484's, deliberately: a skill naming an undeclared key
// stays offered. A skill closed by drift produces no event at all — the
// coordinator simply never routes there, the caller is told the agent cannot
// help, and nothing anywhere says why. What a drifted skill exposes is bounded
// by #484 in turn: every gated tool gates at its own call site against this
// same caller, so the skill can be reached and can still run nothing the
// caller's features do not already allow. A description escapes, not a
// capability. See tests/e2e/features/feature_flag_agent_skills.feature for the
// full reasoning and the counter-argument this was chosen over.
func SkillFeatureGate(
	client *openfeature.Client, registry *FeatureRegistry, logger entities.Logger,
) agents.SkillGate {
	if client == nil {
		return nil
	}
	gate := ToolFeatureGate(client)
	var loggedDrift sync.Map
	return func(ctx context.Context, skillName, featureKey string) bool {
		if featureKey == "" {
			return true
		}
		if registry != nil && logger != nil {
			if _, declared := registry.Lookup(featureKey); !declared {
				// Once per skill and key. A coordinator graph is rebuilt every
				// turn, so logging per evaluation would bury the one line that
				// says a deploy has drifted under a copy of itself per message.
				if _, seen := loggedDrift.LoadOrStore(skillName+"\x00"+featureKey, struct{}{}); !seen {
					logger.Warn(ctx,
						"an agent skill names a feature nobody declared; the skill stays available "+
							"and is not gated by anything",
						"skill", skillName, "feature", featureKey)
				}
			}
		}
		return gate(ctx, featureKey)
	}
}
