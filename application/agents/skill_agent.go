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

package agents

import (
	"fmt"

	"github.com/wepala/weos/v3/domain/entities"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
)

// BuildSkillAgent turns a validated skill definition into a runnable ADK
// agent. The shared toolset is filtered down to the skill's allowlist, and
// the declared mode maps to the ADK delegation mode that governs how an
// orchestrator hands work to the skill. The definition is data — this
// factory is the only place a skill becomes code, which is what lets
// projects add skills without recompiling.
func BuildSkillAgent(def entities.SkillDefinition, m model.LLM, toolset tool.Toolset) (agent.Agent, error) {
	// Validate defensively (nil known-tools: allowlist membership was the
	// registry's job); a definition that slipped past loading must not build
	// a half-configured agent.
	if err := def.Validate(nil); err != nil {
		return nil, fmt.Errorf("invalid skill definition %q: %w", def.ID, err)
	}

	mode := llmagent.ModeTask
	if def.Mode == entities.SkillModeSingleTurn {
		mode = llmagent.ModeSingleTurn
	}

	cfg := llmagent.Config{
		Name:        def.Name,
		Description: def.Description,
		Model:       m,
		Instruction: def.Instructions,
		Mode:        mode,
	}
	if toolset != nil && len(def.Tools) > 0 {
		cfg.Toolsets = []tool.Toolset{
			tool.FilterToolset(toolset, tool.AllowedToolsPredicate(def.Tools)),
		}
	}

	a, err := llmagent.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("build skill agent %q: %w", def.Name, err)
	}
	return a, nil
}
