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

	"github.com/wepala/weos/v3/pkg/jsonld"
)

// contextReconciliation reports what an additive reconcile would do to one
// resource type's stored JSON-LD `@context` (issue #510).
//
// This is the sibling of schemaReconciliation, and deliberately so: issue #379
// taught the boot reconcile to merge the SCHEMA additively so a preset's new
// property reached an existing database, but left the stored `@context`
// untouched. A REFERENCE property — one the schema marks with
// `x-resource-type` — is stored as a JSON-LD edge keyed by its predicate IRI,
// and the readers map that IRI back to a property name through
// jsonld.BuildReverseMap: entities.SimplifyJSONLD for the API response and
// extractEdgeColumns for the projection's FK column. That map only ever sees
// EXPLICIT context entries (ParseContext skips every `@`-prefixed keyword, so
// `@vocab` never appears in it), and an edge whose IRI has no reverse entry is
// skipped by both.
//
// The write itself is NOT lost: BuildResourceGraph resolves the predicate
// through the `@vocab` fallback, so the edge is persisted. What is lost is
// every way of reading it back — invisible to the API, absent from the
// projection column — with a 201 returned the whole time. And because the
// merge only changes how LATER reads resolve, edges already stored under the
// `@vocab`-derived IRI stay unreachable until the operator reprojects, which
// is why that step exists in the acceptance suite.
//
// Literal properties are unaffected: they live in the graph's entity node,
// which the readers copy verbatim without consulting the context at all.
// That asymmetry is why only references need this.
//
// The merge rules mirror reconcileAdditiveSchema's PROPERTY rules, for the same
// reasons stated there — deliberately unlike its top-level keyword handling,
// where the preset wins; here `@vocab`, `@type` and prefixes are all held at
// their stored definition:
//
//   - An entry the preset declares and the stored context lacks is MERGED IN.
//     This is the whole point: it is what makes the reverse map resolve the new
//     reference's predicate, so writes to it stop being dropped.
//   - An entry the stored context has and the preset does not is PRESERVED.
//     There is no provenance on a stored context either, so an operator's own
//     term is indistinguishable from drift, and dropping it would break every
//     reference already written under it.
//   - An entry in BOTH whose definition differs is a CONFLICT: it is HELD at
//     its stored definition and reported. Overwriting it would repoint a
//     predicate that existing edges are already keyed by, orphaning them —
//     strictly worse than the drop this fixes. Refusal is per-entry, so one
//     diverging term never blocks the additive terms beside it.
//
// An absent or empty stored context adopts the preset's wholesale: there is
// nothing to preserve and nothing that can conflict.
//
// Keywords are merged by the same rules as terms, without special-casing.
// `@vocab`, `@base`, `@type` and prefix definitions are all just keys here, so
// a preset that changes one an operator already edited finds it HELD
// rather than overwritten. That is the conservative direction: `@vocab` is the
// fallback every unmapped property resolves through, so silently repointing it
// would move every literal's predicate at once.
//
// The merge is idempotent: re-running it against its own output reports
// Changed=false, which is what keeps a restart from emitting a
// ResourceTypeUpdated per type per boot.
type contextReconciliation struct {
	// Added names the terms present in the preset context but absent from the
	// stored one. These are the entries whose absence was dropping writes.
	Added []string
	// Conflicts names terms present in BOTH whose definitions differ. They are
	// left at their stored definition and reported for an operator to resolve.
	Conflicts []string
	// Moves names terms absent from the stored context whose ADDITION would
	// change how a predicate that already has data resolves. They are held for
	// the same reason a Conflict is, and are reported separately because the
	// operator's fix differs: a Conflict needs a decision about which IRI is
	// right, a Move needs a data migration before the new IRI can be adopted.
	Moves []movedPredicate
	// Context is the merged result. Nil unless Changed.
	Context json.RawMessage
	// Changed reports whether the stored context needs rewriting at all. False
	// means this type's context is a no-op for the boot. Conflicts alone never
	// make a context Changed — there is nothing to write if the only difference
	// is a term being held back.
	Changed bool
}

// reconcileAdditiveContext merges a preset's code-defined `@context` into the
// context currently stored for a resource type, additively. See the
// contextReconciliation doc comment for the rules and the reasoning.
//
// A context that is valid JSON but not an object — JSON-LD permits an array or
// a remote IRI string — is an error rather than something to merge blind. No
// preset or stored context in this tree uses those forms; reporting the type as
// Failed is honest, where guessing at a merge would not be.
func reconcileAdditiveContext(stored, preset, storedSchema json.RawMessage) (contextReconciliation, error) {
	var out contextReconciliation

	// Nothing declared in code means nothing to add. Never treat an empty
	// preset context as an instruction to clear a stored one.
	if len(preset) == 0 {
		return out, nil
	}

	presetTerms, err := splitContext(preset)
	if err != nil {
		return out, fmt.Errorf("preset context: %w", err)
	}

	if len(stored) == 0 {
		out.Added = sortedKeys(presetTerms)
		out.Context = preset
		out.Changed = true
		return out, nil
	}

	storedTerms, err := splitContext(stored)
	if err != nil {
		return out, fmt.Errorf("stored context: %w", err)
	}

	merged := make(map[string]json.RawMessage, len(storedTerms)+len(presetTerms))
	for term, def := range storedTerms {
		merged[term] = def
	}
	for _, term := range sortedKeys(presetTerms) {
		existing, inStored := storedTerms[term]
		if !inStored {
			// Additive by absence is not the same as safe. Adding a term can
			// repoint a predicate that already has edges keyed by the IRI it
			// resolved to before — see movesLivePredicate.
			if moved, ok := movesLivePredicate(term, presetTerms[term], stored, storedSchema); ok {
				out.Moves = append(out.Moves, moved)
				continue
			}
			merged[term] = presetTerms[term]
			out.Added = append(out.Added, term)
			continue
		}
		if !jsonEquivalent(existing, presetTerms[term]) {
			// Held at the stored definition: merged already carries it, so
			// simply not overwriting is what keeps the merge additive.
			out.Conflicts = append(out.Conflicts, term)
		}
	}

	if len(out.Added) == 0 {
		return out, nil
	}

	encoded, err := json.Marshal(merged)
	if err != nil {
		return out, fmt.Errorf("failed to marshal merged context: %w", err)
	}
	out.Context = encoded
	out.Changed = true
	return out, nil
}

// splitContext decodes a JSON-LD `@context` into its term map. A context that
// decodes as JSON `null` yields an empty — not nil — map so callers can range
// over it freely.
func splitContext(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var terms map[string]json.RawMessage
	if err := json.Unmarshal(raw, &terms); err != nil {
		return nil, fmt.Errorf("not a JSON object: %w", err)
	}
	if terms == nil {
		return map[string]json.RawMessage{}, nil
	}
	return terms, nil
}

// referencePropertiesWithoutContextEntry returns the reference properties the
// schema declares that the context gives no explicit term for — exactly the
// set whose writes both readers will drop.
//
// The check mirrors what the read path actually does rather than restating it:
// a reference survives only if BuildReverseMap can map its predicate IRI back
// to its property name, and that map is built solely from explicit terms. A
// property resolving through `@vocab` therefore does NOT count as covered, even
// though ResolvePredicateIRI happily produces an IRI for it.
//
// Two distinct properties mapped to the SAME IRI collide in the reverse map and
// only one survives, so the round-trip is verified per property rather than by
// counting terms.
func referencePropertiesWithoutContextEntry(schema, ldContext json.RawMessage) []string {
	refProps := ExtractReferenceProperties(schema, ldContext)
	if len(refProps) == 0 {
		return nil
	}
	reverse := jsonld.BuildReverseMap(ldContext)
	var dropped []string
	for _, ref := range refProps {
		if reverse[ref.PredicateIRI] != ref.PropertyName {
			dropped = append(dropped, ref.PropertyName)
		}
	}
	sort.Strings(dropped)
	return dropped
}

// movedPredicate records a term held because adding it would repoint a
// predicate that already has data written under the IRI it resolves to today.
type movedPredicate struct {
	// Term is the context key the preset declares and the stored context lacks.
	Term string
	// StoredIRI is what the affected predicate resolves to right now — the IRI
	// existing edges are keyed by.
	StoredIRI string
	// PresetIRI is what it would resolve to if the term were merged.
	PresetIRI string
	// Property names the thing whose resolution moves. Usually Term itself; for
	// a prefix it is the stored term that expands through that prefix.
	Property string
}

// movesLivePredicate reports whether merging one new term would change how a
// predicate that ALREADY HAS DATA resolves (issue #513).
//
// The merge rules make a term the stored context lacks look purely additive.
// It is not. Three ways an addition repoints something live:
//
//   - A reference property the STORED schema already declares, with no term
//     yet, is written under the `@vocab`-derived IRI. Giving it a term that
//     names a different IRI leaves every existing edge keyed by an IRI nothing
//     reverse-maps — unreadable, and reprojection cannot recover it because the
//     reprojector resolves through the new term too.
//   - A PREFIX changes the expansion of every stored term written against it.
//     `"maker":"cat:madeBy"` with no `cat` resolves through `@vocab`; adding
//     `cat` moves it.
//   - `@type` (or a prefix `@type` uses) moves the type's RDF class, so
//     resources written before the boot and after it land in different classes.
//
// The check keys off the STORED schema deliberately. Against the MERGED schema
// it would also fire on the case issue #510 exists to fix — a preset adding a
// reference property and its term in the same build — holding the term and
// re-breaking that repair. A property the stored schema does not yet declare
// has no data under any IRI, so nothing can be orphaned.
func movesLivePredicate(term string, definition, stored, storedSchema json.RawMessage) (movedPredicate, bool) {
	if len(stored) == 0 {
		return movedPredicate{}, false
	}
	storedTerms, err := splitContext(stored)
	if err != nil {
		return movedPredicate{}, false
	}
	// A stored context that defines nothing — including one an operator
	// cleared — has no resolution for anything to move away from, so the
	// preset's terms are adopted wholesale, as they are for an absent context.
	if len(storedTerms) == 0 {
		return movedPredicate{}, false
	}
	candidate := make(map[string]json.RawMessage, len(storedTerms)+1)
	for k, v := range storedTerms {
		candidate[k] = v
	}
	candidate[term] = definition
	candidateRaw, err := json.Marshal(candidate)
	if err != nil {
		return movedPredicate{}, false
	}

	for _, property := range livePredicates(stored, storedSchema) {
		before := resolveIn(stored, property)
		after := resolveIn(candidateRaw, property)
		if before != after {
			return movedPredicate{
				Term: term, StoredIRI: before, PresetIRI: after, Property: property,
			}, true
		}
	}
	return movedPredicate{}, false
}

// livePredicates names everything whose resolution must not move: every term
// the stored context already defines, every reference property the stored
// schema declares, and the type's own `@type`.
func livePredicates(stored, storedSchema json.RawMessage) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}

	if terms, err := splitContext(stored); err == nil {
		for name := range terms {
			// A prefix definition is not itself a predicate; it matters only
			// through the terms that expand against it, which are added here
			// in their own right.
			add(name)
		}
	}
	for _, ref := range ExtractReferenceProperties(storedSchema, stored) {
		add(ref.PropertyName)
	}
	add("@type")
	sort.Strings(out)
	return out
}

// resolveIn returns the IRI a property resolves to under one context — the
// same resolution the write path performs, so a change here is a change to
// where an edge is keyed.
func resolveIn(ldContext json.RawMessage, property string) string {
	vocab, terms := jsonld.ParseContext(ldContext)
	if property == "@type" {
		var raw map[string]any
		if json.Unmarshal(ldContext, &raw) != nil {
			return ""
		}
		typeVal, _ := raw["@type"].(string)
		if typeVal == "" {
			return ""
		}
		return jsonld.ExpandIRI(typeVal, vocab, raw)
	}
	return jsonld.ResolvePredicateIRI(property, vocab, terms)
}
