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
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"sync"

	"weos/domain/entities"
)

// PresetResourceType defines a single resource type within a preset.
type PresetResourceType struct {
	Name        string
	Slug        string
	Description string
	Context     json.RawMessage
	Schema      json.RawMessage
}

// PresetSidebarConfig holds default sidebar settings applied when a preset is installed.
type PresetSidebarConfig struct {
	HiddenSlugs []string          // resource type slugs hidden by default
	MenuGroups  map[string]string // slug -> parent slug for sidebar nesting
}

// PresetDefinition defines a named preset package that bundles resource types,
// behaviors, screen components, and sidebar configuration.
type PresetDefinition struct {
	Name        string
	Description string
	Types       []PresetResourceType
	Behaviors   map[string]entities.ResourceBehavior // slug -> behavior
	Screens     fs.FS                                // optional embedded screen components
	Sidebar     *PresetSidebarConfig                 // optional sidebar defaults
	AutoInstall bool                                 // if true, types are auto-created at startup
}

// InstallPresetResult reports which types were created, updated, or skipped.
type InstallPresetResult struct {
	Created []string `json:"created"`
	Updated []string `json:"updated,omitempty"`
	Skipped []string `json:"skipped"`
}

// PresetRegistry holds all registered presets. Preset packages call Add() to register.
type PresetRegistry struct {
	mu      sync.RWMutex
	presets map[string]PresetDefinition
}

// NewPresetRegistry creates an empty registry.
func NewPresetRegistry() *PresetRegistry {
	return &PresetRegistry{
		presets: make(map[string]PresetDefinition),
	}
}

// Add registers a preset. Returns an error if the definition is invalid.
// If a preset with the same name already exists, it is replaced.
func (r *PresetRegistry) Add(def PresetDefinition) error {
	if def.Name == "" {
		return fmt.Errorf("preset name must not be empty")
	}
	for i, pt := range def.Types {
		if pt.Name == "" || pt.Slug == "" {
			return fmt.Errorf("type at index %d in preset %q: name and slug are required", i, def.Name)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.presets[def.Name] = def
	return nil
}

// MustAdd registers a preset and panics if the definition is invalid.
// Use for compile-time registration of known-good preset data.
func (r *PresetRegistry) MustAdd(def PresetDefinition) {
	if err := r.Add(def); err != nil {
		panic(fmt.Sprintf("preset registration failed: %v", err))
	}
}

// Get returns a preset by name.
func (r *PresetRegistry) Get(name string) (PresetDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.presets[name]
	return def, ok
}

// List returns all registered presets sorted by name.
func (r *PresetRegistry) List() []PresetDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]PresetDefinition, 0, len(r.presets))
	for _, def := range r.presets {
		result = append(result, def)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Behaviors returns a merged ResourceBehaviorRegistry from all registered presets.
// Presets are processed in alphabetical order for deterministic conflict resolution.
func (r *PresetRegistry) Behaviors() ResourceBehaviorRegistry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	// Sort preset names for deterministic merge order.
	names := make([]string, 0, len(r.presets))
	for name := range r.presets {
		names = append(names, name)
	}
	sort.Strings(names)

	behaviors := make(ResourceBehaviorRegistry)
	for _, name := range names {
		for slug, behavior := range r.presets[name].Behaviors {
			behaviors[slug] = behavior
		}
	}
	return behaviors
}

// NewPresetType is a helper to create a PresetResourceType from raw strings.
func NewPresetType(name, slug, desc, ctx, schema string) PresetResourceType {
	pt := PresetResourceType{
		Name:        name,
		Slug:        slug,
		Description: desc,
	}
	if ctx != "" {
		pt.Context = json.RawMessage(ctx)
	}
	if schema != "" {
		pt.Schema = json.RawMessage(schema)
	}
	return pt
}
