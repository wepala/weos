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
	"strings"

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

	// Additive by absence is not the same as safe: see holdMovingTerms. This
	// runs on the WHOLE merged context rather than term by term, because a
	// preset that adds a prefix and a term using it in the same build resolves
	// that term only once both are present — judging the term against the
	// stored context alone reads its IRI with the prefix unexpanded and holds
	// a term that never moved anything.
	out.Moves = holdMovingTerms(stored, storedSchema, merged, &out.Added)

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

// holdMovingTerms removes, from an already-merged context, every added term
// whose presence changes how a predicate that ALREADY HAS DATA resolves, and
// reports what it removed (issue #513).
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
//
// Removing a term can itself change what other terms resolve to, so the pass
// repeats until nothing more moves. It is bounded by the number of added terms:
// each round removes at least one, or stops.
func holdMovingTerms(
	stored, storedSchema json.RawMessage, merged map[string]json.RawMessage, added *[]string,
) []movedPredicate {
	if len(stored) == 0 {
		return nil
	}
	storedTerms, err := splitContext(stored)
	if err != nil || len(storedTerms) == 0 {
		// A stored context that defines nothing — including one an operator
		// cleared — has no resolution for anything to move away from, so the
		// preset's terms are adopted wholesale, as for an absent context.
		return nil
	}

	predicates := livePredicates(stored, storedSchema)
	var held []movedPredicate
	for round := 0; round <= len(*added); round++ {
		culprits, moved := movedBy(stored, merged, storedTerms, predicates)
		if len(culprits) == 0 {
			break
		}
		for _, term := range culprits {
			delete(merged, term)
			*added = removeString(*added, term)
		}
		held = append(held, moved...)
	}
	held = append(held, dropDanglingPrefixTerms(merged, storedTerms, added)...)
	sort.Slice(held, func(i, j int) bool { return held[i].Term < held[j].Term })
	return held
}

// movedBy compares every live predicate's resolution under the stored context
// with its resolution under the candidate, and blames the added terms
// responsible for each difference.
func movedBy(
	stored json.RawMessage, merged, storedTerms map[string]json.RawMessage, predicates []string,
) ([]string, []movedPredicate) {
	candidateRaw, err := json.Marshal(merged)
	if err != nil {
		return nil, nil
	}
	seen := map[string]bool{}
	var culprits []string
	var moved []movedPredicate
	for _, property := range predicates {
		before := resolveIn(stored, property)
		after := resolveIn(candidateRaw, property)
		if before == after {
			continue
		}
		blame := blameFor(property, merged, storedTerms)
		if len(blame) == 0 {
			// A move nobody can be blamed for must never be discarded — that is
			// the silent orphaning this guard exists to prevent, reached through
			// the guard's own bookkeeping. Hold every added term instead: the
			// merge is refused for this type and reported, which is
			// conservative and reversible, where letting it through is not.
			for term := range merged {
				if _, isStored := storedTerms[term]; isStored {
					continue
				}
				blame = append(blame, term)
			}
			sort.Strings(blame)
		}
		for _, term := range blame {
			if seen[term] {
				continue
			}
			seen[term] = true
			culprits = append(culprits, term)
			moved = append(moved, movedPredicate{
				Term: term, StoredIRI: before, PresetIRI: after, Property: property,
			})
		}
	}
	return culprits, moved
}

// blameFor names the added terms responsible for one predicate's resolution
// changing: the term for the property itself, and any prefix its definition
// expands through.
func blameFor(property string, merged, storedTerms map[string]json.RawMessage) []string {
	// Blame the property's own term first and stop there. Holding the term is
	// enough to leave the predicate where it was, and a prefix held alongside
	// it would be collateral: nothing else may need it moved, and future terms
	// legitimately expand through it.
	if _, isStored := storedTerms[property]; !isStored {
		if _, added := merged[property]; added {
			return []string{property}
		}
	}
	var blame []string
	// Otherwise the move came from a prefix the definition expands through,
	// which repoints the property without being named by it.
	for _, def := range []json.RawMessage{storedTerms[property], merged[property]} {
		prefix := prefixOf(def)
		if prefix == "" {
			continue
		}
		if _, isStored := storedTerms[prefix]; isStored {
			continue
		}
		if _, added := merged[prefix]; added {
			blame = append(blame, prefix)
		}
	}
	if len(blame) > 0 {
		return blame
	}
	// Nothing named it, so the mover is the global fallback: a stored context
	// with no `@vocab` resolves an untermed reference to its bare name, and
	// adding one moves every such reference at once. Without this the move is
	// detected, nobody is blamed, no term is removed, and it is allowed
	// through — the silent drop this guard exists to prevent.
	if _, isStored := storedTerms[vocabKeyword]; !isStored {
		if _, added := merged[vocabKeyword]; added {
			return []string{vocabKeyword}
		}
	}
	return nil
}

const vocabKeyword = "@vocab"

// prefixOf returns the prefix a compact term definition expands through, or ""
// when the definition is absent, absolute, or has no prefix.
func prefixOf(def json.RawMessage) string {
	if len(def) == 0 {
		return ""
	}
	var iri string
	if json.Unmarshal(def, &iri) != nil {
		var obj struct {
			ID string `json:"@id"`
		}
		if json.Unmarshal(def, &obj) != nil {
			return ""
		}
		iri = obj.ID
	}
	if iri == "" || strings.HasPrefix(iri, "http://") || strings.HasPrefix(iri, "https://") {
		return ""
	}
	before, _, found := strings.Cut(iri, ":")
	if !found {
		return ""
	}
	return before
}

func removeString(list []string, want string) []string {
	out := list[:0]
	for _, s := range list {
		if s != want {
			out = append(out, s)
		}
	}
	return out
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

// dropDanglingPrefixTerms removes added terms that expand through a prefix the
// merged context does not define, and reports them.
//
// Holding a prefix does not automatically hold the terms written against it.
// Left merged, `"recipeIngredient":"fo:hasIngredient"` without a defined `fo`
// resolves through `@vocab` to `https://schema.org/fo:hasIngredient` — an IRI
// nobody meant, which reads and writes then agree on. Nothing is dropped, so
// the reconcile calls the type Updated, while the triple store and every `kg_*`
// tool carry a fabricated predicate. Better to keep the property untermed and
// say so than to invent a namespace for it.
func dropDanglingPrefixTerms(
	merged, storedTerms map[string]json.RawMessage, added *[]string,
) []movedPredicate {
	var dropped []movedPredicate
	for _, term := range append([]string(nil), *added...) {
		prefix := prefixOf(merged[term])
		if prefix == "" {
			continue
		}
		if _, defined := merged[prefix]; defined {
			continue
		}
		if _, defined := storedTerms[prefix]; defined {
			continue
		}
		dropped = append(dropped, movedPredicate{
			Term:      term,
			Property:  term,
			StoredIRI: "",
			PresetIRI: fmt.Sprintf("<undefined prefix %q>", prefix),
		})
		delete(merged, term)
		*added = removeString(*added, term)
	}
	return dropped
}
