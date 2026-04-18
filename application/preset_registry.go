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
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/wepala/weos/v3/domain/entities"
)

// PresetResourceType defines a single resource type within a preset.
type PresetResourceType struct {
	Name        string
	Slug        string
	Description string
	Context     json.RawMessage
	Schema      json.RawMessage
	Fixtures    []json.RawMessage // optional seed data created on install
}

// PresetSidebarConfig holds default sidebar settings applied when a preset is installed.
type PresetSidebarConfig struct {
	HiddenSlugs []string          // resource type slugs hidden by default
	MenuGroups  map[string]string // slug -> parent slug for sidebar nesting
}

// PresetHTTPHandler describes one HTTP route contributed by a preset. The
// Factory is invoked once at server start with the same BehaviorServices that
// behavior factories receive, so handlers can close over real repositories
// without depending on Fx directly.
//
// Path is relative to /api: a preset declaring Path "/leads/upload" mounts at
// /api/leads/upload. Protected selects the auth-gated group; public handlers
// land on the bare /api group and run without auth middleware.
type PresetHTTPHandler struct {
	Method    string // GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD
	Path      string // relative to /api, e.g. "/leads/upload"
	Factory   func(BehaviorServices) http.HandlerFunc
	Protected bool // true => mount behind auth middleware
}

// PresetDefinition defines a named preset package that bundles resource types,
// behaviors, screen components, sidebar configuration, and HTTP routes.
type PresetDefinition struct {
	Name         string
	Description  string
	Types        []PresetResourceType
	Behaviors    map[string]BehaviorFactory       // slug -> factory invoked at startup with services
	BehaviorMeta map[string]entities.BehaviorMeta // slug -> metadata for UI/config
	Screens      fs.FS                            // optional embedded screen components
	Sidebar      *PresetSidebarConfig             // optional sidebar defaults
	Handlers     []PresetHTTPHandler              // optional HTTP routes mounted under /api
	AutoInstall  bool                             // if true, types are auto-created at startup
}

// InstallPresetResult reports which types were created, updated, or skipped.
type InstallPresetResult struct {
	Created []string       `json:"created"`
	Updated []string       `json:"updated,omitempty"`
	Skipped []string       `json:"skipped"`
	Seeded  map[string]int `json:"seeded,omitempty"` // slug -> count of fixtures created
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
		if err := pt.validateFixtures(); err != nil {
			return fmt.Errorf("preset %q: %w", def.Name, err)
		}
	}
	for slug, factory := range def.Behaviors {
		if factory == nil {
			return fmt.Errorf("preset %q: behavior factory for slug %q is nil", def.Name, slug)
		}
	}
	if err := validateHandlers(def); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.presets[def.Name] = def
	return nil
}

// MustAdd registers a preset and panics if the definition is invalid.
// Use for init-time registration of known-good preset data.
func (r *PresetRegistry) MustAdd(def PresetDefinition) {
	if err := r.Add(def); err != nil {
		panic(fmt.Sprintf("preset registration failed: %v", err))
	}
}

// Get returns a preset by name. The returned value is a deep copy safe to mutate.
func (r *PresetRegistry) Get(name string) (PresetDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.presets[name]
	return def.clone(), ok
}

// List returns all registered presets sorted by name. Returned values are deep copies.
func (r *PresetRegistry) List() []PresetDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]PresetDefinition, 0, len(r.presets))
	for _, def := range r.presets {
		result = append(result, def.clone())
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// clone returns a deep copy of the definition, so callers cannot mutate registry internals.
func (d PresetDefinition) clone() PresetDefinition {
	if d.Types != nil {
		types := make([]PresetResourceType, len(d.Types))
		for i, t := range d.Types {
			types[i] = t
			if t.Context != nil {
				types[i].Context = append(json.RawMessage(nil), t.Context...)
			}
			if t.Schema != nil {
				types[i].Schema = append(json.RawMessage(nil), t.Schema...)
			}
			if t.Fixtures != nil {
				fixtures := make([]json.RawMessage, len(t.Fixtures))
				for j, f := range t.Fixtures {
					fixtures[j] = append(json.RawMessage(nil), f...)
				}
				types[i].Fixtures = fixtures
			}
		}
		d.Types = types
	}
	if d.Behaviors != nil {
		behaviors := make(map[string]BehaviorFactory, len(d.Behaviors))
		for k, v := range d.Behaviors {
			behaviors[k] = v
		}
		d.Behaviors = behaviors
	}
	if d.BehaviorMeta != nil {
		meta := make(map[string]entities.BehaviorMeta, len(d.BehaviorMeta))
		for k, v := range d.BehaviorMeta {
			meta[k] = v
		}
		d.BehaviorMeta = meta
	}
	if d.Handlers != nil {
		handlers := make([]PresetHTTPHandler, len(d.Handlers))
		copy(handlers, d.Handlers)
		d.Handlers = handlers
	}
	if d.Sidebar != nil {
		s := *d.Sidebar
		if s.HiddenSlugs != nil {
			hs := make([]string, len(s.HiddenSlugs))
			copy(hs, s.HiddenSlugs)
			s.HiddenSlugs = hs
		}
		if s.MenuGroups != nil {
			mg := make(map[string]string, len(s.MenuGroups))
			for k, v := range s.MenuGroups {
				mg[k] = v
			}
			s.MenuGroups = mg
		}
		d.Sidebar = &s
	}
	return d
}

// ScreenManifest walks the Screens FS and returns a map of typeSlug to screen
// filenames. Returns nil if Screens is nil or contains no .mjs files.
// Only files at exactly one level of nesting (<typeSlug>/<ScreenName>.mjs) are
// included; deeper paths are ignored.
func (d PresetDefinition) ScreenManifest() map[string][]string {
	if d.Screens == nil {
		return nil
	}
	manifest := make(map[string][]string)
	// WalkDir error is intentionally not propagated: the Screens FS is an
	// embedded filesystem whose contents are guaranteed at compile time.
	// If WalkDir fails (e.g., root unreadable), manifest stays empty and
	// the method returns nil — the same result as "no screens".
	_ = fs.WalkDir(d.Screens, ".", func(p string, entry fs.DirEntry, err error) error { //nolint:errcheck
		if err != nil || entry.IsDir() {
			return nil //nolint:nilerr // skip unreadable entries
		}
		if !strings.HasSuffix(p, ".mjs") {
			return nil
		}
		dir := path.Dir(p)
		if dir == "." {
			return nil // files must be under a type-slug directory
		}
		if strings.Contains(dir, "/") {
			return nil // ignore nested paths; only <typeSlug>/<ScreenName>.mjs is supported
		}
		manifest[dir] = append(manifest[dir], path.Base(p))
		return nil
	})
	if len(manifest) == 0 {
		return nil
	}
	for slug := range manifest {
		sort.Strings(manifest[slug])
	}
	return manifest
}

// Behaviors returns a merged ResourceBehaviorRegistry from all registered presets,
// invoking each preset's BehaviorFactory exactly once with the supplied services.
// Presets are processed in alphabetical order; if multiple presets declare a behavior
// for the same slug, the last preset alphabetically wins and a warning is logged
// (when services.Logger is non-nil). Returns an error if any factory is nil or
// returns a nil ResourceBehavior — Add() rejects nil factories at registration time,
// so the nil-factory branch here is defense in depth.
func (r *PresetRegistry) Behaviors(services BehaviorServices) (ResourceBehaviorRegistry, error) {
	// Snapshot presets under the lock so we don't hold it while invoking
	// factories or the logger (either of which could be slow or call back
	// into the registry).
	r.mu.RLock()
	names := make([]string, 0, len(r.presets))
	snapshot := make(map[string]map[string]BehaviorFactory, len(r.presets))
	for name, def := range r.presets {
		names = append(names, name)
		if len(def.Behaviors) == 0 {
			continue
		}
		factories := make(map[string]BehaviorFactory, len(def.Behaviors))
		for slug, f := range def.Behaviors {
			factories[slug] = f
		}
		snapshot[name] = factories
	}
	r.mu.RUnlock()

	sort.Strings(names)

	behaviors := make(ResourceBehaviorRegistry)
	source := make(map[string]string) // slug -> preset name that registered it
	for _, name := range names {
		for slug, factory := range snapshot[name] {
			if factory == nil {
				return nil, fmt.Errorf("preset %q: behavior factory for slug %q is nil", name, slug)
			}
			if prev, ok := source[slug]; ok && !isNilInterface(services.Logger) {
				services.Logger.Warn(context.Background(),
					"resource behavior overridden by later preset",
					"slug", slug, "previousPreset", prev, "newPreset", name)
			}
			behavior := factory(services)
			if behavior == nil {
				return nil, fmt.Errorf("preset %q: behavior factory for slug %q returned nil", name, slug)
			}
			behaviors[slug] = behavior
			source[slug] = name
		}
	}
	return behaviors, nil
}

// MountedHandler is a fully-resolved preset HTTP route, ready to mount on the
// HTTP router. Source names the preset that contributed it, surfaced in
// collision errors and startup logs.
type MountedHandler struct {
	Method    string
	Path      string
	Protected bool
	Handler   http.HandlerFunc
	Source    string
}

// validHTTPMethods is the allowlist for PresetHTTPHandler.Method. Uppercase
// only — matches the constants in net/http. CONNECT and TRACE are omitted by
// design: preset endpoints have no legitimate use for them.
var validHTTPMethods = map[string]struct{}{
	http.MethodGet:     {},
	http.MethodPost:    {},
	http.MethodPut:     {},
	http.MethodPatch:   {},
	http.MethodDelete:  {},
	http.MethodOptions: {},
	http.MethodHead:    {},
}

// validateHandlers enforces the per-preset invariants on PresetDefinition.Handlers:
// non-empty path, known HTTP verb, non-nil factory, and no intra-preset duplicates
// of "METHOD /path". Cross-preset collisions are detected later in Handlers().
func validateHandlers(def PresetDefinition) error {
	if len(def.Handlers) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(def.Handlers))
	for i, h := range def.Handlers {
		if h.Path == "" {
			return fmt.Errorf("preset %q: handler[%d] path is empty", def.Name, i)
		}
		// A path without a leading slash silently produces a wrong route once
		// concatenated with the /api group prefix (e.g. "leads" -> "/apileads").
		if !strings.HasPrefix(h.Path, "/") {
			return fmt.Errorf("preset %q: handler[%d] path %q must start with %q",
				def.Name, i, h.Path, "/")
		}
		// Handler paths are documented as relative to the /api router group,
		// so an already-prefixed "/api/leads" would mount at "/api/api/leads".
		// Reject such paths so the runtime matches the documented contract.
		if h.Path == "/api" || strings.HasPrefix(h.Path, "/api/") {
			return fmt.Errorf("preset %q: handler[%d] path %q must be relative to %q, not include it",
				def.Name, i, h.Path, "/api")
		}
		if _, ok := validHTTPMethods[h.Method]; !ok {
			return fmt.Errorf("preset %q: handler[%d] method %q is not a known HTTP verb", def.Name, i, h.Method)
		}
		if h.Factory == nil {
			return fmt.Errorf("preset %q: handler[%d] factory is nil", def.Name, i)
		}
		key := h.Method + " " + h.Path
		if _, dup := seen[key]; dup {
			return fmt.Errorf("preset %q: handler[%d] duplicates %q within the same preset", def.Name, i, key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// Handlers returns all preset-contributed HTTP routes, with each Factory
// invoked once with the supplied services. Ordering is alphabetical by preset
// name, preserving each preset's declared handler order. Returns an error if
// any factory is nil or returns nil, or if two presets declare the same
// "METHOD /path" — startup fails so the conflict can't go unnoticed.
func (r *PresetRegistry) Handlers(services BehaviorServices) ([]MountedHandler, error) {
	r.mu.RLock()
	names := make([]string, 0, len(r.presets))
	snapshot := make(map[string][]PresetHTTPHandler, len(r.presets))
	for name, def := range r.presets {
		names = append(names, name)
		if len(def.Handlers) == 0 {
			continue
		}
		copied := make([]PresetHTTPHandler, len(def.Handlers))
		copy(copied, def.Handlers)
		snapshot[name] = copied
	}
	r.mu.RUnlock()

	sort.Strings(names)

	var mounted []MountedHandler
	source := make(map[string]string) // "METHOD /path" -> preset name
	for _, name := range names {
		for i, h := range snapshot[name] {
			if h.Factory == nil {
				return nil, fmt.Errorf("preset %q: handler[%d] factory is nil", name, i)
			}
			key := h.Method + " " + h.Path
			if prev, ok := source[key]; ok {
				return nil, fmt.Errorf("preset %q and preset %q both declare handler %q",
					prev, name, key)
			}
			fn := h.Factory(services)
			if fn == nil {
				return nil, fmt.Errorf("preset %q: handler[%d] factory returned nil", name, i)
			}
			source[key] = name
			mounted = append(mounted, MountedHandler{
				Method:    h.Method,
				Path:      h.Path,
				Protected: h.Protected,
				Handler:   fn,
				Source:    name,
			})
		}
	}
	return mounted, nil
}

// validateFixtures checks that fixture data is well-formed at registration time.
func (pt PresetResourceType) validateFixtures() error {
	if len(pt.Fixtures) == 0 {
		return nil
	}
	if len(pt.Schema) == 0 {
		return fmt.Errorf("type %q has fixtures but no schema", pt.Slug)
	}
	for i, f := range pt.Fixtures {
		if !json.Valid(f) {
			return fmt.Errorf("type %q fixture[%d] is not valid JSON", pt.Slug, i)
		}
		var obj map[string]any
		if err := json.Unmarshal(f, &obj); err != nil || obj == nil {
			return fmt.Errorf("type %q fixture[%d] must be a JSON object", pt.Slug, i)
		}
	}
	return nil
}

// BehaviorsMeta returns a merged BehaviorMetaRegistry from all registered presets.
// Same merge semantics as Behaviors(): alphabetical order, last wins.
func (r *PresetRegistry) BehaviorsMeta() BehaviorMetaRegistry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.presets))
	for name := range r.presets {
		names = append(names, name)
	}
	sort.Strings(names)

	meta := make(BehaviorMetaRegistry)
	for _, name := range names {
		for slug, m := range r.presets[name].BehaviorMeta {
			meta[slug] = m
		}
	}
	return meta
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
