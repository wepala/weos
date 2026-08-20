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
	"context"
	"fmt"

	"github.com/wepala/weos/v3/domain/entities"
)

// Gating agent skills on a feature (epic #480, story #485).
//
// #484 gated the tools; this gates the layer above them. Two gates again, and
// again unequal. Filtering the coordinator's sub-agents is what stops
// transfer_to_agent: the coordinator cannot transfer to a sub-agent it was
// never given, so a hidden skill cannot be routed to, described, or talked
// about as though it existed. Refusing direct invocation — the ?skill=<name>
// door #419 shipped — is the control, because a client that names a skill it
// saw last week must not reach further than a caller whose features say no.
//
// Both doors are served by allowedSkills below, deliberately: an
// implementation with two filters is an implementation with one filter and a
// hole, and the hole is invisible until somebody types the name.

// SkillGate reports whether a skill's feature is on for the caller on ctx.
// skillName travels with the key so the one place that owns the rule can also
// own the log line, and name both when a skill points at a feature nobody
// declared.
//
// A nil gate means nothing is gated, which is how every existing caller of
// NewOrchestrator behaves until the application layer wires one.
type SkillGate func(ctx context.Context, skillName, featureKey string) bool

// SetSkillGate wires the feature gate (called from the application layer's fx
// wiring, which owns the registry, the OpenFeature client and the logger).
func (o *Orchestrator) SetSkillGate(g SkillGate) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.skillGate = g
}

// allowedSkills loads the skills and drops the ones the caller's features do
// not reach. It is the single filter both doors use.
//
// Filtering here rather than in the registry is load-bearing. SkillRegistry
// caches one set of definitions for the whole instance, loaded with the
// caller's identity stripped on purpose, because a skill is an installed
// capability and not per-user data. A filter inside that cache would resolve
// one caller's features and then serve the answer to everybody until the next
// skill event — the second person to take a turn would get the first person's
// permissions. This runs once per turn, against the caller's own context.
//
// It costs no extra database work. Each gated skill asks the resolver for one
// key, and the resolver answers every key after the first from the set it
// already holds for this caller — the same set the turn's toolset resolved.
// Twenty gated skills therefore read stored feature state no more often than
// none do.
func (o *Orchestrator) allowedSkills(ctx context.Context) ([]entities.SkillDefinition, error) {
	o.mu.RLock()
	skills := o.skills
	gate := o.skillGate
	o.mu.RUnlock()

	if skills == nil {
		return nil, nil
	}
	defs, err := skills(ctx)
	if err != nil {
		return nil, fmt.Errorf("load skills: %w", err)
	}
	if gate == nil {
		return defs, nil
	}
	allowed := make([]entities.SkillDefinition, 0, len(defs))
	for _, def := range defs {
		if def.GatedBy != "" && !gate(ctx, def.Name, def.GatedBy) {
			continue
		}
		allowed = append(allowed, def)
	}
	return allowed, nil
}

// RoutableSkills returns the skills the caller on ctx can be routed to — the
// exact list buildRoot turns into the coordinator's sub-agents.
//
// Exported because more than the coordinator needs it: the admin surface
// (#486) has to show a person what their agent can actually reach, and a
// second implementation of "what may this caller use" would be a second
// answer. Callers get definitions, not agents, so nothing here builds a model.
func (o *Orchestrator) RoutableSkills(ctx context.Context) ([]entities.SkillDefinition, error) {
	return o.allowedSkills(ctx)
}

// findSkill locates a named skill for the direct-invocation door and reports
// why the caller cannot have it.
//
// The two refusals must read differently. "Unknown skill" and "you do not have
// this skill" send the reader to different places: one is a typo or a skill
// nobody installed, the other is a capability an admin can grant. A single
// message for both would make a permission question look like a spelling
// mistake, and an operator would go looking in the wrong place.
func (o *Orchestrator) findSkill(
	ctx context.Context, name string,
) (entities.SkillDefinition, error) {
	o.mu.RLock()
	skills := o.skills
	gate := o.skillGate
	o.mu.RUnlock()

	if skills == nil {
		return entities.SkillDefinition{}, fmt.Errorf("unknown skill %q: no skills are loaded", name)
	}
	defs, err := skills(ctx)
	if err != nil {
		return entities.SkillDefinition{}, fmt.Errorf("load skills: %w", err)
	}
	for _, def := range defs {
		if def.Name != name {
			continue
		}
		if def.GatedBy != "" && gate != nil && !gate(ctx, def.Name, def.GatedBy) {
			return entities.SkillDefinition{}, fmt.Errorf("%s",
				entities.GateRefusal(ctx, name, def.GatedBy))
		}
		return def, nil
	}
	return entities.SkillDefinition{}, fmt.Errorf("unknown skill %q", name)
}
