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
	// It is populated even when the reconcile is ultimately refused, so a caller
	// can report which additive properties a refusal also blocked.
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
//   - `required` follows the preset, EXCEPT that requirements naming
//     stored-only properties are carried over. The preset has no opinion about a
//     property it does not declare, so dropping such an entry would silently
//     weaken a constraint the operator set — the mirror of the property-
//     preservation rule above. Note the tightening direction is deliberate and
//     load-bearing: a property the preset newly marks required will start
//     failing validation for existing rows that lack it.
//   - Every other top-level keyword (`type`, `additionalProperties`, …) follows
//     the PRESET where the preset declares it; stored-only keywords are preserved.
//
// An absent or empty stored schema adopts the preset's schema wholesale —
// there is nothing to preserve and nothing that can conflict.
//
// SCOPE OF THE CONFLICT CHECK: property definitions are compared verbatim. A
// schema that reaches its types through indirection — `$ref` into `$defs` or
// `definitions`, or `allOf`/`patternProperties` — can therefore be retyped
// without tripping the conflict guard, because the change lands in a top-level
// keyword the preset simply wins. No preset in the tree uses those keywords
// today; if one starts to, the conflict check must resolve indirection before
// it can honestly claim to refuse every non-additive divergence.
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

	presetDoc, err := splitSchema(preset)
	if err != nil {
		return out, fmt.Errorf("preset schema: %w", err)
	}

	if len(stored) == 0 {
		out.Added = sortedKeys(presetDoc.props)
		out.Schema = preset
		out.Changed = true
		return out, nil
	}

	storedDoc, err := splitSchema(stored)
	if err != nil {
		return out, fmt.Errorf("stored schema: %w", err)
	}

	mergedProps := make(map[string]json.RawMessage, len(storedDoc.props)+len(presetDoc.props))
	for name, def := range storedDoc.props {
		mergedProps[name] = def
	}
	for _, name := range sortedKeys(presetDoc.props) {
		existing, inStored := storedDoc.props[name]
		if !inStored {
			mergedProps[name] = presetDoc.props[name]
			out.Added = append(out.Added, name)
			continue
		}
		if !jsonEquivalent(existing, presetDoc.props[name]) {
			out.Conflicts = append(out.Conflicts, name)
		}
	}

	// A non-additive divergence refuses the whole type. Reporting Schema as nil
	// makes it impossible for a caller to persist a partially-merged schema.
	// Added survives so the caller can name what the refusal also blocked.
	if len(out.Conflicts) > 0 {
		return out, nil
	}

	mergedTop, topChanged, err := mergeTopLevel(storedDoc, presetDoc)
	if err != nil {
		return out, err
	}

	if len(out.Added) == 0 && !topChanged {
		return out, nil
	}

	// Only carry a `properties` key if one side actually had one. Writing an
	// empty object unconditionally would put a keyword in the stored schema
	// that neither the preset nor the operator declared.
	if len(mergedProps) > 0 || storedDoc.hadProps || presetDoc.hadProps {
		propsRaw, mErr := json.Marshal(mergedProps)
		if mErr != nil {
			return out, fmt.Errorf("failed to marshal merged properties: %w", mErr)
		}
		mergedTop["properties"] = propsRaw
	}

	merged, err := json.Marshal(mergedTop)
	if err != nil {
		return out, fmt.Errorf("failed to marshal merged schema: %w", err)
	}
	out.Schema = merged
	out.Changed = true
	return out, nil
}

// mergeTopLevel merges every schema keyword except `properties`: the preset
// wins where it declares a keyword, stored-only keywords are preserved, and
// `required` gets the carry-over treatment described on reconcileAdditiveSchema.
// Reports whether anything actually changed.
func mergeTopLevel(storedDoc, presetDoc schemaDoc) (map[string]json.RawMessage, bool, error) {
	merged := make(map[string]json.RawMessage, len(storedDoc.top)+len(presetDoc.top))
	for key, val := range storedDoc.top {
		merged[key] = val
	}
	changed := false
	for key, val := range presetDoc.top {
		if key == requiredKeyword {
			continue // handled below, where stored-only properties are known
		}
		if prev, ok := storedDoc.top[key]; !ok || !jsonEquivalent(prev, val) {
			merged[key] = val
			changed = true
		}
	}

	requiredRaw, reqChanged, err := mergeRequired(storedDoc, presetDoc)
	if err != nil {
		return nil, false, err
	}
	if requiredRaw != nil {
		merged[requiredKeyword] = requiredRaw
	}
	return merged, changed || reqChanged, nil
}

const requiredKeyword = "required"

// mergeRequired computes the reconciled `required` array: the preset's list,
// plus any stored entry naming a property the preset does not declare.
//
// Without that carry-over, a preset whose `required` omits an operator's own
// property would silently drop that property's requirement — the property is
// preserved but its constraint is not, leaving a schema neither side authored.
// Returns a nil slice when the preset declares no `required` at all, meaning
// "leave whatever is stored alone".
func mergeRequired(storedDoc, presetDoc schemaDoc) (json.RawMessage, bool, error) {
	presetRaw, presetDeclares := presetDoc.top[requiredKeyword]
	if !presetDeclares {
		return nil, false, nil
	}
	presetReq, err := decodeStringArray(presetRaw)
	if err != nil {
		return nil, false, fmt.Errorf("preset `required`: %w", err)
	}
	storedReq, err := decodeStringArray(storedDoc.top[requiredKeyword])
	if err != nil {
		return nil, false, fmt.Errorf("stored `required`: %w", err)
	}

	seen := make(map[string]bool, len(presetReq))
	out := make([]string, 0, len(presetReq)+len(storedReq))
	for _, name := range presetReq {
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, name := range storedReq {
		if seen[name] {
			continue
		}
		// Only carry over requirements the preset has no opinion about — i.e.
		// those naming properties it does not declare.
		if _, presetHas := presetDoc.props[name]; presetHas {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, false, fmt.Errorf("failed to marshal merged `required`: %w", err)
	}
	return encoded, !jsonEquivalent(storedDoc.top[requiredKeyword], encoded), nil
}

// decodeStringArray decodes a JSON string array, tolerating an absent or null
// value as empty. A non-array (or non-string element) is an error rather than a
// silent empty, so a malformed `required` can't quietly erase requirements.
func decodeStringArray(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("not an array of strings: %w", err)
	}
	return out, nil
}

// schemaDoc is a JSON Schema split into its property map and everything else,
// because `properties` is the only keyword this reconcile merges key-by-key.
type schemaDoc struct {
	top   map[string]json.RawMessage
	props map[string]json.RawMessage
	// hadProps records whether the document declared a `properties` keyword at
	// all, so the merge can avoid inventing one.
	hadProps bool
}

// splitSchema decodes a JSON Schema into a schemaDoc. A schema with no
// `properties` key yields an empty — not nil — property map so callers can
// range over it freely. A schema that is valid JSON but not an object (`true`,
// `[1,2]`) is an error; the caller reports it rather than reconciling blind.
func splitSchema(raw json.RawMessage) (schemaDoc, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return schemaDoc{}, fmt.Errorf("not a JSON object: %w", err)
	}
	doc := schemaDoc{top: top, props: map[string]json.RawMessage{}}
	if doc.top == nil {
		// The schema decoded as JSON `null`. Treat it as an empty object rather
		// than persisting `null` back as a schema.
		doc.top = map[string]json.RawMessage{}
		return doc, nil
	}
	if rawProps, ok := doc.top["properties"]; ok {
		doc.hadProps = true
		if err := json.Unmarshal(rawProps, &doc.props); err != nil {
			return schemaDoc{}, fmt.Errorf("`properties` is not a JSON object: %w", err)
		}
		if doc.props == nil {
			doc.props = map[string]json.RawMessage{}
		}
	}
	delete(doc.top, "properties")
	return doc, nil
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
