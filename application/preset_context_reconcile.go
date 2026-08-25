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
	if err != nil {
		return nil
	}
	// An EMPTY stored context is not a blank check. `{}` and `null` still
	// resolve a reference — to its bare property name, because there is no
	// `@vocab` to prefix it — so edges exist under those names and adopting the
	// preset's terms orphans them exactly as any other repointing would.
	// Clearing a context is a destructive operator act; the reconcile reports
	// it rather than silently papering over it. `preset install --update`
	// remains the explicit way to re-adopt.

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

// adoptTerms rewrites a stored context to take the preset's definition for the
// named terms, recording the IRI each property currently resolves to as an
// alias (issue #513).
//
// This is the sanctioned way out of a held term. The guard refuses at boot to
// adopt a term that would repoint a predicate with data under it, and that is
// correct — but on its own it leaves the property unreadable with no way
// forward. Adoption is safe only because the old IRI is recorded first: events
// are immutable and carry the write-time key, so a reproject reproduces it, and
// without the alias every existing edge would be orphaned permanently.
//
// Returns the merged context and the terms actually adopted. A term already
// matching the preset's definition is skipped rather than re-recorded, which is
// what makes running this twice a no-op.
func adoptTerms(
	stored, preset json.RawMessage, held []movedPredicate,
) (json.RawMessage, []string, error) {
	storedTerms, err := splitContext(stored)
	if err != nil {
		return nil, nil, fmt.Errorf("stored context: %w", err)
	}
	presetTerms, err := splitContext(preset)
	if err != nil {
		return nil, nil, fmt.Errorf("preset context: %w", err)
	}

	aliases := jsonld.TermAliases(stored)
	if aliases == nil {
		aliases = map[string][]string{}
	}
	merged := make(map[string]json.RawMessage, len(storedTerms)+len(held))
	for term, def := range storedTerms {
		merged[term] = def
	}

	var adopted []string
	for _, move := range held {
		presetDef, declared := presetTerms[move.Term]
		if !declared {
			return nil, nil, fmt.Errorf("the preset declares no %q term to adopt", move.Term)
		}
		if jsonEquivalent(storedTerms[move.Term], presetDef) {
			continue // already adopted; recording a second alias would be wrong
		}
		// The alias belongs to the PROPERTY whose resolution moves, which is not
		// always the term being adopted: adopting a prefix moves every stored
		// term written against it, and it is those properties whose edges carry
		// the old IRI.
		// A class IRI is not an edge key: the alias map is for predicates,
		// and a class recorded there would be handed back by BuildReverseMap
		// as a property named "@type" (issue #521).
		if move.Property != "" && move.Property != "@type" && move.StoredIRI != "" {
			aliases[move.Property] = appendMissing(aliases[move.Property], move.StoredIRI)
		}
		merged[move.Term] = presetDef
		adopted = append(adopted, move.Term)
	}
	if len(adopted) == 0 {
		return nil, nil, nil
	}

	encodedAliases, err := json.Marshal(aliases)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to encode term aliases: %w", err)
	}
	merged[jsonld.TermAliasesKeyword] = encodedAliases

	// Record WHICH terms were adopted, separately from the aliases. Adopting a
	// prefix records aliases against the properties it moves, so the alias map
	// alone cannot tell a re-run of the same command from a term that was never
	// held.
	adoptedAll := jsonld.AdoptedTerms(stored)
	for _, term := range adopted {
		adoptedAll = appendMissing(adoptedAll, term)
	}
	sort.Strings(adoptedAll)
	encodedAdopted, err := json.Marshal(adoptedAll)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to encode adopted terms: %w", err)
	}
	merged[jsonld.AdoptedTermsKeyword] = encodedAdopted
	encoded, err := json.Marshal(merged)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to encode the adopted context: %w", err)
	}
	sort.Strings(adopted)
	return encoded, adopted, nil
}

// appendMissing adds an IRI to a property's alias list unless it is already
// there, so repeated adoption cannot grow the list without bound.
func appendMissing(list []string, iri string) []string {
	for _, existing := range list {
		if existing == iri {
			return list
		}
	}
	return append(list, iri)
}

// ambiguousReferenceShape reports reference properties that no reader can tell
// apart: two on one type resolving to the same predicate AND pointing at the
// same target type.
//
// Sharing a predicate is legitimate on its own — an atomic relation like
// `associated` is meant to be reused, and a reader separates the values by the
// type each one points at. That stops working when two of them point at the
// same type: the graph genuinely does not distinguish them, and no projection
// can invent the difference. Storing them anyway means one silently standing in
// for the other.
//
// Two shapes are correct instead, and the report names both because which one
// is meant depends on intent, not on anything visible here:
//
//   - Two DIFFERENT relationships want different predicates. A primary author
//     and a reviewer are not the same relation to a question.
//   - One relationship with several targets wants a single ARRAY property, not
//     two scalar ones — that is a one-to-many, and saying so makes it one.
//
// The check reads the STORED schema and context, not the preset's declaration.
// A preset can only make a type ambiguous by ADDING a property whose term names
// an existing property's predicate: an existing term cannot be repointed,
// because reconcileAdditiveContext holds a diverging term at its stored
// definition. Reading the preset would therefore report a shape that was never
// installed, and miss the one that was.
func ambiguousReferenceShape(refs []ReferencePropertyDef) [][]string {
	type shape struct{ predicate, target string }
	byShape := map[shape][]string{}
	for _, ref := range refs {
		key := shape{predicate: ref.PredicateIRI, target: ref.TargetType}
		byShape[key] = append(byShape[key], ref.PropertyName)
	}
	var ambiguous [][]string
	for _, names := range byShape {
		if len(names) < 2 {
			continue
		}
		sort.Strings(names)
		ambiguous = append(ambiguous, names)
	}
	sort.Slice(ambiguous, func(i, j int) bool { return ambiguous[i][0] < ambiguous[j][0] })
	return ambiguous
}
