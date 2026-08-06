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
	"sort"
)

// schemaReconciliation reports what an additive reconcile would do to one
// resource type's stored JSON Schema (issue #379).
//
// Only Schema is safe to persist, and only when Changed is true. When
// Conflicts is non-empty the reconcile is refused wholesale — Changed is
// false and Schema is nil — because a property whose definition changed is a
// non-additive migration, which is explicitly out of scope and needs an
// operator decision rather than a silent rewrite.
type schemaReconciliation struct {
	// Added names the properties present in the preset but absent from the
	// stored schema. These are what the projection table is missing columns for.
	Added []string
	// Conflicts names properties present in BOTH schemas whose definitions
	// differ — a retype or redefinition. Refusing on these is what keeps the
	// reconcile additive.
	Conflicts []string
	// Schema is the merged result. Nil unless Changed.
	Schema json.RawMessage
	// Changed reports whether the stored schema needs rewriting at all. False
	// means the boot is a no-op for this type, so no ResourceTypeUpdated event
	// is emitted and repeated restarts stay quiet.
	Changed bool
}

// reconcileAdditiveSchema merges a preset's code-defined JSON Schema into the
// schema currently stored for a resource type, additively.
//
// The rules, in the order they matter:
//
//   - A property the preset declares and the stored schema lacks is MERGED IN.
//     This is the whole point: it is what gives the projection table its missing
//     column on the next EnsureTable pass.
//   - A property the stored schema has and the preset does not is PRESERVED.
//     WeOS has no provenance field on ResourceType, so an operator's own
//     addition through the API is indistinguishable from drift; dropping it
//     would trade one silent data loss for another. Preserving it also stops a
//     single customization from permanently blocking later preset additions.
//   - A property in BOTH whose definition differs is a CONFLICT. The whole type
//     is refused and left untouched for the caller to warn about — that is the
//     deferred non-additive case (rename/retype/drop).
//   - Every other top-level keyword (`required`, `type`, `additionalProperties`,
//     …) follows the PRESET where the preset declares it; stored-only keywords
//     are preserved. Note this makes `required` authoritative: a property the
//     preset newly marks required will start failing validation for existing
//     rows that lack it. That is a deliberate, operator-visible choice.
//
// An absent or empty stored schema adopts the preset's schema wholesale —
// there is nothing to preserve and nothing that can conflict.
//
// The merge is idempotent: re-running it against its own output reports
// Changed=false, which is what keeps a restart from emitting an event per type
// per boot.
func reconcileAdditiveSchema(stored, preset json.RawMessage) (schemaReconciliation, error) {
	var out schemaReconciliation

	// Nothing declared in code means nothing to add. Never treat an empty
	// preset schema as an instruction to clear a stored one.
	if len(preset) == 0 {
		return out, nil
	}

	presetTop, presetProps, err := splitSchema(preset)
	if err != nil {
		return out, fmt.Errorf("preset schema: %w", err)
	}

	if len(stored) == 0 {
		out.Added = sortedKeys(presetProps)
		out.Schema = preset
		out.Changed = true
		return out, nil
	}

	storedTop, storedProps, err := splitSchema(stored)
	if err != nil {
		return out, fmt.Errorf("stored schema: %w", err)
	}

	mergedProps := make(map[string]json.RawMessage, len(storedProps)+len(presetProps))
	for name, def := range storedProps {
		mergedProps[name] = def
	}
	for _, name := range sortedKeys(presetProps) {
		existing, inStored := storedProps[name]
		if !inStored {
			mergedProps[name] = presetProps[name]
			out.Added = append(out.Added, name)
			continue
		}
		if !jsonEquivalent(existing, presetProps[name]) {
			out.Conflicts = append(out.Conflicts, name)
		}
	}

	// A non-additive divergence refuses the whole type. Reporting Schema as nil
	// makes it impossible for a caller to persist a partially-merged schema.
	if len(out.Conflicts) > 0 {
		return out, nil
	}

	mergedTop := make(map[string]json.RawMessage, len(storedTop)+len(presetTop))
	for key, val := range storedTop {
		mergedTop[key] = val
	}
	topChanged := false
	for key, val := range presetTop {
		if prev, ok := storedTop[key]; !ok || !jsonEquivalent(prev, val) {
			mergedTop[key] = val
			topChanged = true
		}
	}

	if len(out.Added) == 0 && !topChanged {
		return out, nil
	}

	propsRaw, err := json.Marshal(mergedProps)
	if err != nil {
		return out, fmt.Errorf("failed to marshal merged properties: %w", err)
	}
	mergedTop["properties"] = propsRaw

	merged, err := json.Marshal(mergedTop)
	if err != nil {
		return out, fmt.Errorf("failed to marshal merged schema: %w", err)
	}
	out.Schema = merged
	out.Changed = true
	return out, nil
}

// splitSchema decodes a JSON Schema into its top-level keywords (excluding
// `properties`) and its property map. A schema with no `properties` key yields
// an empty — not nil — property map so callers can range over it freely.
//
// `properties` is separated because it is the only keyword this reconcile
// merges key-by-key; everything else is compared and replaced whole.
func splitSchema(raw json.RawMessage) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, nil, fmt.Errorf("not a JSON object: %w", err)
	}
	props := map[string]json.RawMessage{}
	if rawProps, ok := top["properties"]; ok {
		if err := json.Unmarshal(rawProps, &props); err != nil {
			return nil, nil, fmt.Errorf("`properties` is not a JSON object: %w", err)
		}
	}
	delete(top, "properties")
	return top, props, nil
}

// sortedKeys returns a map's keys in a stable order so Added lists (and the
// log lines built from them) don't shuffle between runs.
func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
