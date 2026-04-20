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
	"fmt"
	"regexp"
	"sort"
	"sync"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/pkg/utils"
)

// PresetLinkDefinition declares a relationship between two resource types that
// lives outside either type's schema. Unlike x-resource-type properties baked
// into a schema, link definitions can be registered by a third package so
// neither the source nor target type needs to know about the other.
//
// A link activates the first time both SourceType and TargetType exist as
// installed resource types; until then it remains dormant. Activation is
// idempotent.
//
// Fields:
//   - Name is a human-readable identifier ("invoice-guardian"). Used in logs
//     and for disambiguation; not persisted.
//   - SourceType and TargetType are resource type slugs.
//   - PropertyName is the attribute name on the source resource that carries
//     the reference (e.g. "guardian"). This drives the FK column name and the
//     predicate IRI derivation.
//   - PredicateIRI, if empty, is derived later from the source type's JSON-LD
//     context and PropertyName using the same rule as schema-derived
//     x-resource-type properties; activation itself does not resolve or
//     validate it.
//   - DisplayProperty defaults to "name" when empty; it names the property on
//     the target whose value is denormalized into <prop>_display.
type PresetLinkDefinition struct {
	Name            string
	SourceType      string
	TargetType      string
	PropertyName    string
	PredicateIRI    string
	DisplayProperty string
}

// LinkRegistry holds cross-type link definitions contributed by presets and by
// packages that call application.RegisterLink at startup. Definitions are
// declarative code: "pending vs active" is a runtime view computed by
// ActiveFor given the set of installed type slugs, not a stored state.
type LinkRegistry struct {
	mu    sync.RWMutex
	links []PresetLinkDefinition
	// index prevents duplicate (SourceType, CamelToSnake(PropertyName))
	// entries. The key is what's unique in the database — multiple links
	// could legitimately target the same type, but only one can occupy a
	// given derived snake_case property/column on a source.
	index map[string]int // "source|snake_property" -> index into links
}

// NewLinkRegistry creates an empty registry.
func NewLinkRegistry() *LinkRegistry {
	return &LinkRegistry{
		index: make(map[string]int),
	}
}

// Add registers a link definition. If another definition already maps to
// the same source type and derived FK column — i.e. the same
// (SourceType, CamelToSnake(PropertyName)) pair — the new definition
// replaces the old one (last-writer-wins), so a package-init RegisterLink
// that overrides a preset-declared link takes effect without the caller
// needing to unregister first. Keying on the snake_case form (not the raw
// PropertyName) catches ambiguous camelCase spellings like "guardianId"
// vs "guardianID" that both project to the same "guardian_id" column.
func (r *LinkRegistry) Add(def PresetLinkDefinition) error {
	if err := validateLinkDefinition(def); err != nil {
		return err
	}
	key := def.SourceType + "|" + utils.CamelToSnake(def.PropertyName)
	r.mu.Lock()
	defer r.mu.Unlock()
	if idx, ok := r.index[key]; ok {
		r.links[idx] = def
		return nil
	}
	r.index[key] = len(r.links)
	r.links = append(r.links, def)
	return nil
}

// MustAdd is the panicking form of Add for init-time registration of
// known-good link definitions.
func (r *LinkRegistry) MustAdd(def PresetLinkDefinition) {
	if err := r.Add(def); err != nil {
		panic(fmt.Sprintf("link registration failed: %v", err))
	}
}

// All returns a copy of every registered link definition, sorted by
// (SourceType, PropertyName) for deterministic iteration.
func (r *LinkRegistry) All() []PresetLinkDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PresetLinkDefinition, len(r.links))
	copy(out, r.links)
	sort.Slice(out, func(i, j int) bool {
		if out[i].SourceType != out[j].SourceType {
			return out[i].SourceType < out[j].SourceType
		}
		return out[i].PropertyName < out[j].PropertyName
	})
	return out
}

// ActiveFor returns the subset of links whose SourceType and TargetType both
// appear as keys in installed. The caller supplies the installed set rather
// than having the registry query a repository, so the registry stays
// dependency-free and trivially testable.
func (r *LinkRegistry) ActiveFor(installed map[string]bool) []PresetLinkDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []PresetLinkDefinition
	for _, l := range r.links {
		if installed[l.SourceType] && installed[l.TargetType] {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SourceType != out[j].SourceType {
			return out[i].SourceType < out[j].SourceType
		}
		return out[i].PropertyName < out[j].PropertyName
	})
	return out
}

// BySource returns every link whose SourceType matches slug. Used by the
// resource write path to merge link-derived reference properties with the
// schema-derived x-resource-type properties for triple extraction.
func (r *LinkRegistry) BySource(slug string) []PresetLinkDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []PresetLinkDefinition
	for _, l := range r.links {
		if l.SourceType == slug {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PropertyName < out[j].PropertyName })
	return out
}

// ByTarget returns every link whose TargetType matches slug.
func (r *LinkRegistry) ByTarget(slug string) []PresetLinkDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []PresetLinkDefinition
	for _, l := range r.links {
		if l.TargetType == slug {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PropertyName < out[j].PropertyName })
	return out
}

// LinkReferencesForSource implements repositories.LinkSource so the projection
// manager can replay link-declared refs after a schema re-parse clears its
// forward/reverse maps. Returns an empty slice (not nil) for unknown slugs
// so callers can range over the result without a nil check.
func (r *LinkRegistry) LinkReferencesForSource(sourceSlug string) []repositories.LinkReference {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []repositories.LinkReference{}
	for _, l := range r.links {
		if l.SourceType != sourceSlug {
			continue
		}
		out = append(out, repositories.LinkReference{
			SourceSlug:      l.SourceType,
			PropertyName:    l.PropertyName,
			TargetSlug:      l.TargetType,
			DisplayProperty: l.DisplayProperty,
		})
	}
	return out
}

// defaultLinkRegistry is the process-wide registry that package-init
// RegisterLink calls populate. application.Module seeds its Fx-provided
// LinkRegistry from this value so package-init registrations are visible
// to the running service.
var defaultLinkRegistry = NewLinkRegistry()

// RegisterLink appends a link definition to the process-wide default
// registry. Intended for package init() of integration packages that don't
// carry a full PresetDefinition but still want to declare a cross-preset
// link — e.g. a lightweight "finance-education" integration package that
// connects Invoice to Guardian without either preset depending on the other.
//
// Links declared inside a PresetDefinition.Links slice are collected through
// preset registration separately; the runtime *LinkRegistry seen by the
// application is assembled later by the DI wiring from both PresetRegistry
// and DefaultLinkRegistry().
func RegisterLink(def PresetLinkDefinition) error {
	return defaultLinkRegistry.Add(def)
}

// DefaultLinkRegistry returns the process-wide registry seeded by
// package-init RegisterLink calls. Exported for the DI layer to use as the
// seed for the Fx-provided *LinkRegistry.
func DefaultLinkRegistry() *LinkRegistry {
	return defaultLinkRegistry
}

// linkSlugPattern mirrors validateSlug's regex (resource_type_service.go):
// lowercase kebab-case, digits allowed. Catches typos like "Invoice" or
// "invoice_id" at registration time rather than at reconcile, where the
// mismatch would silently leave the link dormant forever.
var linkSlugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// linkPropertyNamePattern enforces lowerCamelCase for PropertyName: first
// rune must be a lowercase letter, remaining runes are letters/digits only.
// Without this a link could register "Guardian" while another registered
// "guardian" — both CamelToSnake to "guardian" and silently collide in the
// projection column. Underscores, hyphens, leading uppercase, and leading
// digits are all rejected.
var linkPropertyNamePattern = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)

// linkReservedPropertyNames rejects property names that would collide with
// the projection table's standard columns — an ALTER TABLE ADD COLUMN for
// any of these would fail at reconcile time. Rejecting at Add() surfaces the
// mistake at registration instead of burying it in a log entry later.
//
// Both the raw PropertyName and its snake_case projection (the actual column
// name) are checked, so "typeSlug", "type_slug", "createdAt", and "created_at"
// all get rejected. JSON-LD reserved keys (@id, @type, @context) are also
// rejected because they have special meaning in the serialized resource.
var linkReservedPropertyNames = map[string]bool{
	"@id":         true,
	"@type":       true,
	"@context":    true,
	"id":          true,
	"type":        true,
	"typeSlug":    true,
	"type_slug":   true,
	"data":        true,
	"status":      true,
	"createdBy":   true,
	"created_by":  true,
	"accountId":   true,
	"account_id":  true,
	"sequenceNo":  true,
	"sequence_no": true,
	"createdAt":   true,
	"created_at":  true,
	"updatedAt":   true,
	"updated_at":  true,
	"deletedAt":   true,
	"deleted_at":  true,
}

func validateLinkDefinition(def PresetLinkDefinition) error {
	if def.SourceType == "" {
		return fmt.Errorf("link definition: SourceType is required")
	}
	if def.TargetType == "" {
		return fmt.Errorf("link definition: TargetType is required")
	}
	if def.PropertyName == "" {
		return fmt.Errorf("link definition: PropertyName is required")
	}
	if !linkPropertyNamePattern.MatchString(def.PropertyName) {
		// Enforce lowerCamelCase end-to-end. Two links like "guardianId" and
		// "guardian_id" — or "Guardian" and "guardian" — would dedup to
		// different registry keys but share the same snake_case FK column,
		// causing a silent ALTER TABLE collision later. Requiring a lowercase
		// first rune and alphanumeric tail keeps the (SourceType, PropertyName)
		// registry key one-to-one with the derived DB column name.
		return fmt.Errorf(
			"link definition: PropertyName %q must be lowerCamelCase (first rune lowercase letter, alphanumeric only)",
			def.PropertyName,
		)
	}
	if !linkSlugPattern.MatchString(def.SourceType) {
		return fmt.Errorf("link definition: SourceType %q must be lowercase kebab-case", def.SourceType)
	}
	if !linkSlugPattern.MatchString(def.TargetType) {
		return fmt.Errorf("link definition: TargetType %q must be lowercase kebab-case", def.TargetType)
	}
	if linkReservedPropertyNames[def.PropertyName] ||
		linkReservedPropertyNames[utils.CamelToSnake(def.PropertyName)] {
		return fmt.Errorf("link definition: PropertyName %q collides with a standard projection column", def.PropertyName)
	}
	return nil
}
