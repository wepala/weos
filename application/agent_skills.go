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
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/akeemphilbert/pericarp/pkg/auth"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// AgentSkillTypeSlug is the resource type declarative agent skills live in.
const AgentSkillTypeSlug = "agent-skill"

const agentSkillURNPrefix = "urn:" + AgentSkillTypeSlug + ":"

// KnownToolsFunc returns the names of the tools currently registered on the
// instance's tool surface. The registry uses it to fail skills whose
// allowlist references a tool that does not exist. It is injected late (the
// tool surface is assembled downstream of this package); until set, the
// unknown-tool check is skipped.
type KnownToolsFunc func(ctx context.Context) (map[string]bool, error)

// skillSource is the slice of ResourceService the registry needs.
type skillSource interface {
	List(ctx context.Context, typeSlug, cursor string, limit int, sort repositories.SortOptions) (
		repositories.PaginatedResponse[*entities.Resource], error)
}

// SkillRegistry holds the validated agent-skill definitions the orchestrator
// builds sub-agents from. It loads lazily from published agent-skill
// resources and reloads after any agent-skill resource event, so adding a
// skill is a data change — no recompile, no redeploy.
type SkillRegistry struct {
	resources skillSource
	logger    entities.Logger

	mu         sync.Mutex
	knownTools KnownToolsFunc
	cache      []entities.SkillDefinition
	loaded     bool
}

// NewSkillRegistry creates the registry. It is provided via fx; the
// orchestrator wiring calls SetKnownTools once the tool surface exists.
func NewSkillRegistry(resources ResourceService, logger entities.Logger) *SkillRegistry {
	return &SkillRegistry{resources: resources, logger: logger}
}

// SetKnownTools installs the tool-name source and invalidates the cache so
// the next load re-validates every allowlist.
func (r *SkillRegistry) SetKnownTools(f KnownToolsFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.knownTools = f
	r.loaded = false
}

// MarkDirty invalidates the cache; the next Skills call reloads. Wired to
// agent-skill resource events.
func (r *SkillRegistry) MarkDirty() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loaded = false
}

// Skills returns the currently-loaded definitions, reloading if a skill
// resource changed since the last call. Invalid definitions are skipped with
// a log line; they never take the rest of the registry down.
func (r *SkillRegistry) Skills(ctx context.Context) ([]entities.SkillDefinition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.loaded {
		defs, err := r.load(ctx)
		if err != nil {
			return nil, err
		}
		r.cache = defs
		r.loaded = true
	}
	out := make([]entities.SkillDefinition, len(r.cache))
	copy(out, r.cache)
	return out, nil
}

func (r *SkillRegistry) load(ctx context.Context) ([]entities.SkillDefinition, error) {
	var knownTools map[string]bool
	if r.knownTools != nil {
		kt, err := r.knownTools(ctx)
		if err != nil {
			return nil, fmt.Errorf("list known tools: %w", err)
		}
		knownTools = kt
	}

	var defs []entities.SkillDefinition
	seen := map[string]string{} // skill name -> resource URN
	cursor := ""
	// Skills are installed app capabilities, not per-user data: a skill
	// seeded at boot (a preset fixture, a downstream binary's seed hook)
	// carries no account attribution, and the caller's account-scoped
	// visibility would hide it from every authenticated user. Strip only
	// the identity — the service then reads unscoped — keeping the
	// caller's context so cancellation and deadlines still bound the
	// listing (load holds the registry mutex).
	listCtx := auth.ContextWithAgent(ctx, nil)
	for {
		page, err := r.resources.List(listCtx, AgentSkillTypeSlug, cursor, 100, repositories.SortOptions{})
		if err != nil {
			return nil, fmt.Errorf("list %s resources: %w", AgentSkillTypeSlug, err)
		}
		for _, res := range page.Data {
			if res.Status() != "active" {
				continue
			}
			def, err := ParseSkillDefinition(res.GetID(), res.Data())
			if err == nil {
				err = def.Validate(knownTools)
			}
			if err != nil {
				r.logger.Error(ctx, "skipping agent-skill: definition failed to load",
					"id", res.GetID(), "error", err.Error())
				continue
			}
			if prior, dup := seen[def.Name]; dup {
				r.logger.Error(ctx, "skipping agent-skill: duplicate skill name",
					"id", res.GetID(), "name", def.Name, "conflictsWith", prior)
				continue
			}
			seen[def.Name] = def.ID
			defs = append(defs, def)
		}
		if !page.HasMore || page.Cursor == "" {
			break
		}
		cursor = page.Cursor
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs, nil
}

// ParseSkillDefinition parses an agent-skill resource's JSON-LD data into a
// definition, applying v1 defaults (schemaVersion 1, task mode). Validation
// is separate so callers control the known-tools check.
func ParseSkillDefinition(id string, data json.RawMessage) (entities.SkillDefinition, error) {
	var raw struct {
		SchemaVersion int      `json:"schemaVersion"`
		Name          string   `json:"name"`
		Description   string   `json:"description"`
		Instructions  string   `json:"instructions"`
		Tools         []string `json:"tools"`
		Mode          string   `json:"mode"`
		Widgets       []string `json:"widgets"`
		Model         string   `json:"model"`
		GatedBy       string   `json:"gatedBy"`
	}
	if err := json.Unmarshal(ExtractEntityNode(data), &raw); err != nil {
		return entities.SkillDefinition{}, fmt.Errorf("parse agent-skill data: %w", err)
	}
	if raw.SchemaVersion == 0 {
		raw.SchemaVersion = entities.SkillSchemaVersion
	}
	if raw.Mode == "" {
		raw.Mode = entities.SkillModeTask
	}
	return entities.SkillDefinition{
		ID:            id,
		SchemaVersion: raw.SchemaVersion,
		Name:          raw.Name,
		Description:   raw.Description,
		Instructions:  raw.Instructions,
		Tools:         raw.Tools,
		Mode:          raw.Mode,
		Widgets:       raw.Widgets,
		Model:         raw.Model,
		GatedBy:       raw.GatedBy,
	}, nil
}

// subscribeSkillRegistry invalidates the registry whenever an agent-skill
// resource is published or deleted, so skill changes take effect without a
// restart. Handlers are idempotent (MarkDirty is a flag flip) per the event
// replay constraint.
func subscribeSkillRegistry(d *domain.EventDispatcher, registry *SkillRegistry) error {
	if err := domain.Subscribe(d, "Resource.Published",
		func(_ context.Context, env domain.EventEnvelope[entities.ResourcePublished]) error {
			if strings.HasPrefix(env.AggregateID, agentSkillURNPrefix) {
				registry.MarkDirty()
			}
			return nil
		},
	); err != nil {
		return err
	}
	return domain.Subscribe(d, "Resource.Deleted",
		func(_ context.Context, env domain.EventEnvelope[entities.ResourceDeleted]) error {
			if strings.HasPrefix(env.AggregateID, agentSkillURNPrefix) {
				registry.MarkDirty()
			}
			return nil
		},
	)
}
