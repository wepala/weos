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

package entities

import (
	"fmt"
	"regexp"
)

// Skill modes map to ADK delegation modes. A task skill converses with the
// user until the task is done; a single-turn skill answers once without
// follow-up questions.
const (
	SkillModeTask       = "task"
	SkillModeSingleTurn = "single_turn"
)

// SkillSchemaVersion is the agent-skill definition version this build
// understands. v1 skills are single agents; composed multi-step skills will
// bump this, so older builds skip definitions they cannot run instead of
// misinterpreting them.
const SkillSchemaVersion = 1

// skillNameRe keeps skill names usable as ADK agent names (they travel
// through transfer_to_agent function calls).
var skillNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)

// SkillDefinition is a parsed agent-skill resource: the declarative
// description of one skill a WeOS app's orchestrator can route requests to.
// Skills are ordinary event-sourced resources; this struct is the validated
// in-memory form the agent factory consumes.
type SkillDefinition struct {
	// ID is the resource URN (urn:agent-skill:<ksuid>).
	ID            string
	SchemaVersion int
	// Name doubles as the ADK agent name.
	Name string
	// Description is the orchestrator's routing signal.
	Description  string
	Instructions string
	// Tools is the allowlist of tool names the skill may call. Empty means
	// the skill answers from instructions alone.
	Tools []string
	Mode  string
	// Widgets hints which output widgets the skill prefers (markdown, table,
	// list, card).
	Widgets []string
	// Model optionally overrides the instance's default model ID.
	Model string
}

// Validate reports why the definition cannot be loaded. knownTools guards
// against typo'd allowlists — a skill referencing a tool that is not
// registered fails to load. Pass nil to skip that check when no live tool
// surface is available.
func (d SkillDefinition) Validate(knownTools map[string]bool) error {
	if d.SchemaVersion != SkillSchemaVersion {
		return fmt.Errorf("unsupported schemaVersion %d (this build supports v%d)",
			d.SchemaVersion, SkillSchemaVersion)
	}
	if !skillNameRe.MatchString(d.Name) {
		return fmt.Errorf("name %q must match %s", d.Name, skillNameRe.String())
	}
	if d.Description == "" {
		return fmt.Errorf("description is required — the orchestrator routes on it")
	}
	if d.Instructions == "" {
		return fmt.Errorf("instructions are required")
	}
	if d.Mode != SkillModeTask && d.Mode != SkillModeSingleTurn {
		return fmt.Errorf("mode %q must be %q or %q", d.Mode, SkillModeTask, SkillModeSingleTurn)
	}
	if knownTools != nil {
		for _, t := range d.Tools {
			if !knownTools[t] {
				return fmt.Errorf("unknown tool %q in allowlist", t)
			}
		}
	}
	return nil
}
