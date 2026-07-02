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
	"google.golang.org/genai"
)

// widgetOutputInstruction is appended to every agent's instructions so
// replies land in the widget contract (pkg/widgets). Emission is
// instruction-driven because Gemini cannot combine a forced response schema
// with tool calling; widgets.Parse guarantees a bad payload still renders.
const widgetOutputInstruction = `
Format your final reply as WeOS widget JSON — a single JSON object, nothing before or after it:
{"schemaVersion":1,"widgets":[...]}
Widget types:
{"type":"markdown","markdown":"..."} for prose;
{"type":"table","title":"...","columns":["..."],"rows":[["..."]]} for tabular data (each row has exactly one cell per column);
{"type":"list","title":"...","items":["..."]} for short enumerations;
{"type":"card","title":"...","body":"...","url":"...","fields":[{"label":"...","value":"..."}]} for a single entity.
Prefer the specific widget over markdown when the data fits one.`

// widgetResponseSchema forces contract-shaped JSON for agents that use no
// tools (the only case Gemini allows a response schema).
var widgetResponseSchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"schemaVersion": {Type: genai.TypeInteger},
		"widgets": {
			Type: genai.TypeArray,
			Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"type":     {Type: genai.TypeString, Enum: []string{"markdown", "table", "list", "card"}},
					"markdown": {Type: genai.TypeString},
					"title":    {Type: genai.TypeString},
					"columns":  {Type: genai.TypeArray, Items: &genai.Schema{Type: genai.TypeString}},
					"rows": {Type: genai.TypeArray, Items: &genai.Schema{
						Type: genai.TypeArray, Items: &genai.Schema{Type: genai.TypeString},
					}},
					"items": {Type: genai.TypeArray, Items: &genai.Schema{Type: genai.TypeString}},
					"body":  {Type: genai.TypeString},
					"url":   {Type: genai.TypeString},
					"fields": {Type: genai.TypeArray, Items: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"label": {Type: genai.TypeString},
							"value": {Type: genai.TypeString},
						},
						Required: []string{"label", "value"},
					}},
				},
				Required: []string{"type"},
			},
		},
	},
	Required: []string{"widgets"},
}

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
		Instruction: def.Instructions + "\n" + widgetOutputInstruction,
		Mode:        mode,
	}
	if toolset != nil && len(def.Tools) > 0 {
		cfg.Toolsets = []tool.Toolset{
			tool.FilterToolset(toolset, tool.AllowedToolsPredicate(def.Tools)),
		}
	} else {
		// Tool-less skills can have the contract enforced at the model level.
		cfg.GenerateContentConfig = &genai.GenerateContentConfig{
			ResponseSchema:   widgetResponseSchema,
			ResponseMIMEType: "application/json",
		}
	}

	a, err := llmagent.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("build skill agent %q: %w", def.Name, err)
	}
	return a, nil
}
