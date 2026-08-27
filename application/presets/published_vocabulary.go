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

package presets

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/pkg/jsonld"
)

// Issue #535: a preset property must not state a predicate that the vocabulary
// it lands in does not define.
//
// Every preset type sets an `@vocab`, and a property with no term of its own
// resolves through it. `quantity` on a shopping list item was therefore written
// as `https://schema.org/quantity` — an assertion that schema.org defines a term
// it does not. The IRI travels: anything reusing the type inherits the claim,
// and a consumer resolving the predicate gets nothing back.
//
// THE GUARD LOOKS AT EFFECTIVE PREDICATES, NOT AT UNTERMED PROPERTIES. That
// distinction is the whole reason this file exists rather than a smaller one.
// A property can reach a published vocabulary two ways — by riding `@vocab`,
// or by carrying an explicit term that points there — and the first sweep of
// this issue checked only the first kind. It missed four meal-planning
// properties that named the food ontology explicitly through an `fo:` prefix,
// none of which that ontology defines. Both routes end at an IRI, so the guard
// judges the IRI.
//
// THREE FAULTS WEAR THE SAME COSTUME, AND ONLY TWO ARE MECHANICALLY DETECTABLE:
//
//   - A MINT. The vocabulary answers nothing for the name. Caught by
//     policedVocabularies: the name is absent from the list of terms that
//     vocabulary publishes.
//   - A TERM PUBLISHED FOR ANOTHER SUBJECT. The name resolves, so no lookup
//     ever exposes it, but its published meaning is about something else —
//     `schema:status` is the status of a medical study. Caught only by
//     termsPublishedForAnotherSubject, because an allow-list cannot: the term
//     genuinely exists.
//   - AN OVER-CORRECTION — moving a genuine published name into the house
//     namespace. NOT caught here, and nothing else catches it either: every
//     read resolves through the type's own context either way, so no test
//     fails while the structured-data value quietly drains away. The
//     acceptance suite pins the specific names that must not move.
//
// THE LISTS ARE CURATED, AND DELIBERATELY SO. They hold the names the presets
// actually borrow, not every term each vocabulary publishes. One entry means
// one human resolved one IRI against the published vocabulary and decided the
// sense fits. A genuinely new schema.org property therefore fails this guard
// until somebody adds the row, and adding the row IS the verification step.
//
// THE LIMITS, STATED PLAINLY, because a guard whose limits are undocumented
// gets trusted past them:
//
//   - it is exactly as good as the human who curated the list;
//   - it cannot find a subject misuse nobody noticed. `schema:preparation`
//     sat undetected until this issue swept for it, and would have again;
//   - it never notices a vocabulary deprecating or moving a term;
//   - it does not police a namespace absent from policedVocabularies, so an
//     IRI in some fourth vocabulary is simply not this guard's business.
//
// It never touches the network. A vocabulary being slow or down must not redden
// CI, and a guard that only fires when the machine has DNS is not a guard.

// VocabularyFault distinguishes the two mechanically detectable faults, because
// they need different repairs: a mint needs a house term, while a term
// published for another subject needs a judgement about meaning.
type VocabularyFault string

const (
	// FaultUndefinedTerm — the vocabulary publishes no such name.
	FaultUndefinedTerm VocabularyFault = "states a term the vocabulary does not define"
	// FaultOtherSubject — the name resolves, but was published about something else.
	FaultOtherSubject VocabularyFault = "states a term the vocabulary publishes for another subject"
	// FaultCannotJudge — the guard could not read what the type states, so it
	// has no opinion. Reported rather than skipped: for a guard whose only
	// output is a list, silence and approval are the same signal, and the
	// input most likely to be malformed is exactly the input most likely to be
	// wrong. A type the guard cannot read is not a type it has cleared.
	FaultCannotJudge VocabularyFault = "could not be judged"
)

// VocabularyViolation is one property stating one predicate it should not.
type VocabularyViolation struct {
	Slug         string
	PropertyName string
	PredicateIRI string
	Fault        VocabularyFault
	// Detail carries the curated note for FaultOtherSubject — what the term
	// actually means — and is empty for a mint, where there is nothing to say
	// beyond its absence.
	Detail string
}

// Key identifies a violation for waiver lookup. Type slugs are unique across
// the registry, so slug+property needs no preset name and stays stable when a
// type moves between presets.
func (v VocabularyViolation) Key() string { return v.Slug + "." + v.PropertyName }

func (v VocabularyViolation) String() string {
	if v.Detail != "" {
		return fmt.Sprintf("%s: %q %s (%s — %s)", v.Slug, v.PropertyName, v.Fault, v.PredicateIRI, v.Detail)
	}
	return fmt.Sprintf("%s: %q %s (%s)", v.Slug, v.PropertyName, v.Fault, v.PredicateIRI)
}

// policedVocabularies maps each namespace this guard polices to the local names
// the presets legitimately borrow from it. A predicate landing in one of these
// namespaces whose local name is absent is a violation; a predicate landing
// anywhere else is ignored.
//
// House vocabularies are deliberately absent. WeOS publishes
// https://weos.io/vocab/..., so WeOS defines whatever it names there, and
// #520's guard already polices that namespace's shape.
var policedVocabularies = map[string]map[string]bool{
	// schema.org, verified against the published vocabulary
	// (schemaorg-current-https.jsonld, 1521 rdf:Property entries).
	"https://schema.org/": namesToSet(
		"about", "address", "articleBody", "author", "availability", "brand", "byDay", "byMonth",
		"byMonthDay", "calories", "carbohydrateContent", "cookTime", "cssSelector", "datePublished",
		"description", "duration", "email", "endDate", "endTime", "exceptDate", "familyName",
		"fatContent", "fiberContent", "geo", "givenName", "hasPart", "headline", "identifier",
		"image", "inLanguage", "isPartOf", "itemListElement", "keywords", "location", "logo", "mainEntity",
		"maximumAttendeeCapacity", "name", "nutrition", "position", "prepTime", "price",
		"priceCurrency", "proteinContent", "provider", "purchaseDate", "recipeCategory",
		"recipeCuisine", "recipeIngredient", "recipeInstructions", "recipeYield", "recipient",
		"repeatCount", "repeatFrequency", "reviewBody", "reviewRating", "saturatedFatContent",
		"scheduleTimezone", "serviceType", "servingSize", "sku", "sodiumContent", "startDate",
		"startTime", "sugarContent", "suitableForDiet", "text", "thumbnailUrl", "totalTime",
		"url", "version",
	),
	// SKOS Core. Note that `title` and `description` are NOT here: SKOS
	// publishes neither, they are Dublin Core terms, and the knowledge preset
	// currently rides `@vocab` into both. That is the waived
	// concept-scheme entry below, and it is why this guard polices
	// namespaces rather than hard-coding schema.org.
	"http://www.w3.org/2004/02/skos/core#": namesToSet(
		"altLabel", "definition", "member", "prefLabel",
	),
	// Dublin Core Terms, policed from #537. The knowledge preset's
	// concept-scheme rides a SKOS `@vocab` and SKOS Core publishes neither
	// `title` nor `description` — both are Dublin Core, and the repair points
	// them here.
	//
	// POLICING THIS NAMESPACE IS THE REPAIR, NOT BOOKKEEPING AROUND IT. The
	// guard ignores any namespace absent from this map, so leaving dcterms out
	// would make those two properties unexamined and report the preset clean by
	// never asking — a silent pass indistinguishable from a fix.
	//
	// The legacy http://purl.org/dc/elements/1.1/ namespace publishes both
	// names too and is deliberately NOT policed: nothing resolves there, and an
	// entry for a namespace nothing uses is the rubber stamp
	// UnusedAllowListEntries exists to prevent.
	"http://purl.org/dc/terms/": namesToSet(
		"description", "title",
	),
	// W3C PROV-O.
	"http://www.w3.org/ns/prov#": namesToSet(
		"generatedAtTime", "invalidatedAtTime", "wasAttributedTo", "wasDerivedFrom", "wasRevisionOf",
	),
	// The food ontology publishes fourteen names, taken from
	// http://purl.org/foodontology rather than assumed. ELEVEN are listed here
	// — the vocabulary's properties. The other three (`Food`, `FoodAdditive`,
	// `Ingredient`) are CLASSES, and are deliberately absent: this guard
	// polices predicates, so a class used as one should be reported rather
	// than waved through. That is not hypothetical — `ingredient` reached
	// `fo:ShoppingCategory`, a class-shaped name, until #535.
	//
	// No preset predicate borrows any of the eleven any more: meal-planning
	// used four names this vocabulary never defined (`hasIngredient`,
	// `ingredient`, `ShoppingCategory`, `at_its_best`) and #535 repaired all
	// four. The namespace stays policed so the next `fo:` term is checked, and
	// the property list is complete, so a legitimate borrow needs no research
	// to approve.
	//
	// Provenance, because it bears on how far this list can be trusted:
	// purl.org now redirects to ITMO University's FoodOntology v0.0.9 (2015),
	// not Martin Hepp's. It is thinly maintained, which is a reason to check
	// rather than assume when a new `fo:` term is proposed.
	"http://purl.org/foodontology#": namesToSet(
		"carbohydratesPer100g", "carbohydratesPer100gAsDouble", "containsGMO",
		"containsIngredient", "energyPer100g", "energyPer100gAsDouble", "fatPer100g",
		"fatPer100gAsDouble", "ingredientsListAsText", "proteinsPer100g",
		"proteinsPer100gAsDouble",
	),
}

// namespaceAliases maps an alternative spelling of a policed namespace onto
// the canonical one.
//
// schema.org serves its vocabulary over both http and https and treats the two
// as the same namespace, so `"@vocab":"http://schema.org/"` mints exactly the
// same false claims as the https form. Policing only one spelling would leave a
// hole that costs a single character to walk through — and the miss would be
// invisible rather than noisy, because the guard would report such a type
// CLEAN rather than unpoliced.
//
// Dublin Core has the same split in the opposite direction. DCMI publishes the
// terms namespace over http, and http://purl.org/dc/terms/ is the canonical
// spelling, but the https form is live and widely written. Policing only one
// would leave the newly policed namespace unguarded one character away — the
// same "clean by never asking" failure the dcterms row above exists to close.
var namespaceAliases = map[string]string{
	"http://schema.org/":         "https://schema.org/",
	"https://purl.org/dc/terms/": "http://purl.org/dc/terms/",
}

// termsPublishedForAnotherSubject holds names that DO resolve but whose
// published meaning is wrong for the WeOS use, mapped to what they actually
// mean. An allow-list can never catch these, because the term exists — which is
// exactly what makes them the half a 404 check misses.
// AND NO ROW HERE MAY BE DELETED FOR LOOKING UNUSED. After #537 no preset
// resolves `schema:status`, `schema:title` or `schema:preparation` at all, so
// this list reads dead. It is not: it is what makes the NEXT type that adds an
// untermed `status` fail on the day it is authored, which is the whole reason a
// deny-list exists rather than an allow-list. UnusedAllowListEntries sweeps
// policedVocabularies only, and deliberately never prunes this map.
var termsPublishedForAnotherSubject = map[string]map[string]string{
	"https://schema.org/": {
		"status":      "the status of a MedicalCondition, MedicalProcedure or MedicalStudy",
		"preparation": "the preparation a patient undergoes before a MedicalProcedure",
		// "The title of the job", published for JobPosting. A notification's
		// title is not a job title. This one was caught late: it sat in the
		// allow-list because a preset already used it, which is precisely the
		// reasoning this list exists to replace.
		"title": "the title of a JobPosting",
	},
}

// vocabularyWaivers names violations that are accepted for now, each mapped to
// the ticket that owns it.
//
// IT IS EMPTY, AND KEEPING IT EMPTY IS THE POINT. #535 shipped it holding 23
// entries — every preset property outside meal-planning that stated a predicate
// its vocabulary would not confirm — and #537 repaired all 23. The map stays
// because the sweep asserts the violation set EQUALS it: empty means a single
// new offender anywhere fails on the day it is authored, which is the ratchet
// the guard exists to be.
//
// A line added here is a standing permission for one property to state a
// predicate nobody defines, so it needs a ticket rather than a promise. A
// waiver with no owner is a to-do list nobody is holding, and it silently
// re-permits that name the moment somebody reuses the type.
//
// Both halves of the equality still run against an empty map, and both matter:
// a violation with no waiver fails, and a waiver naming no violation fails too.
// Do not delete TestPresets_NoWaiverOutlivesItsViolation along with the waivers
// it policed — it is what stops a stale line being added back unnoticed.
var vocabularyWaivers = map[string]string{}

func namesToSet(names ...string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}

// VocabularyWaivers returns a copy of the waiver map, so a test can assert
// exact equality against the sweep without being able to mutate the source.
func VocabularyWaivers() map[string]string {
	out := make(map[string]string, len(vocabularyWaivers))
	for k, v := range vocabularyWaivers {
		out[k] = v
	}
	return out
}

// PublishedVocabularyViolations reports every property of the given types whose
// effective predicate lands in a policed vocabulary that does not define it, or
// that borrows a term published for another subject.
//
// It reports RAW violations — waived ones included — because the built-in
// sweep asserts set equality against the waivers, and filtering here would make
// a stale waiver invisible. ContextGuardViolations does the filtering for
// callers that want only what is unaccounted for.
//
// Exported so a private preset registry (the overlay build) can sweep its own
// types in its own CI, the way it already inherits the #517 rules.
func PublishedVocabularyViolations(types []application.PresetResourceType) []VocabularyViolation {
	var out []VocabularyViolation
	for _, pt := range types {
		if len(pt.Context) > 0 && !json.Valid(pt.Context) {
			out = append(out, VocabularyViolation{Slug: pt.Slug, PropertyName: "@context",
				Fault: FaultCannotJudge, Detail: "the @context is not valid JSON, so every property of this type resolves to nothing"})
			continue
		}
		props, readable := schemaPropertyNames(pt.Schema)
		if !readable {
			out = append(out, VocabularyViolation{Slug: pt.Slug, PropertyName: "@schema",
				Fault: FaultCannotJudge, Detail: "the JSON Schema is not valid JSON, so no property of this type was checked"})
			continue
		}
		vocab, forward := jsonld.ParseContext(normalizedContext(pt.Context))
		for _, prop := range props {
			iri := jsonld.ResolvePredicateIRI(prop, vocab, forward)
			if !isAbsoluteIRI(iri) {
				// No `@vocab` and no term: in JSON-LD this property maps to no
				// predicate at all, so there is nothing for this guard to
				// judge — and nothing for a consumer to resolve either.
				out = append(out, VocabularyViolation{Slug: pt.Slug, PropertyName: prop, PredicateIRI: iri,
					Fault: FaultCannotJudge, Detail: "resolves to no absolute IRI, so it states no predicate"})
				continue
			}
			namespace, local, ok := splitPolicedIRI(iri)
			if !ok {
				if ns, deeper := belowTermLevel(iri); deeper {
					out = append(out, VocabularyViolation{Slug: pt.Slug, PropertyName: prop, PredicateIRI: iri,
						Fault: FaultCannotJudge, Detail: "sits under " + ns + " but below its term level, so the guard cannot say whether it is defined"})
				}
				continue
			}
			if meaning, borrowed := termsPublishedForAnotherSubject[namespace][local]; borrowed {
				out = append(out, VocabularyViolation{
					Slug: pt.Slug, PropertyName: prop, PredicateIRI: iri,
					Fault: FaultOtherSubject, Detail: meaning,
				})
				continue
			}
			if !policedVocabularies[namespace][local] {
				out = append(out, VocabularyViolation{
					Slug: pt.Slug, PropertyName: prop, PredicateIRI: iri,
					Fault: FaultUndefinedTerm,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

// isAbsoluteIRI reports whether a resolved predicate names a scheme, and so
// states a predicate at all.
//
// Testing for "://" is not the same question and gets one case wrong: a URN,
// a mailto: or a did: is absolute without an authority component, so
// `urn:weos:something` would be judged to state no predicate when it plainly
// does. The sibling guard in context_guards.go already contemplates exactly
// those shapes, so they are not hypothetical here.
//
// A bare property name — the no-@vocab case this exists to catch — has no
// scheme and is correctly rejected. An unexpanded compact IRI cannot reach
// here: an undeclared prefix is absorbed by `@vocab` into a full URL long
// before this point.
func isAbsoluteIRI(iri string) bool {
	colon := strings.IndexByte(iri, ':')
	if colon <= 0 {
		return false
	}
	// RFC 3986: scheme = ALPHA *( ALPHA / DIGIT / "+" / "-" / "." )
	for i := 0; i < colon; i++ {
		c := iri[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'):
		default:
			return false
		}
	}
	return true
}

// normalizedContext rewrites a `@context` stored as a bare IRI string into the
// equivalent object form, so the guard reads what the graph reads.
//
// `"https://schema.org/"` is legal JSON-LD and this codebase supports it
// deliberately: jsonld.InlineVocabContext performs the same rewrite on the
// projection path (application/oxigraph_handler.go), because an embedded graph
// store has no network to fetch a remote context and `@vocab` expands the bare
// terms identically. Without the same rewrite here the guard reads no
// vocabulary, every property resolves to a bare name, and a type whose terms
// ARE judgeable gets reported unjudgeable — the guard disagreeing with the
// write path about what a document says.
//
// No built-in preset uses the string form today. The overlay registry this
// function is exported for may, and legal input should not need luck.
func normalizedContext(ldContext json.RawMessage) json.RawMessage {
	var iri string
	if json.Unmarshal(ldContext, &iri) != nil || iri == "" {
		return ldContext // not a bare string; use it as it stands
	}
	inlined, err := json.Marshal(map[string]string{"@vocab": iri})
	if err != nil {
		return ldContext
	}
	return inlined
}

// canonicalIRI rewrites an IRI whose namespace has an alternative spelling
// onto the canonical one, so every later comparison sees one form.
func canonicalIRI(iri string) string {
	for alias, canonical := range namespaceAliases {
		if strings.HasPrefix(iri, alias) {
			return canonical + iri[len(alias):]
		}
	}
	return iri
}

// splitPolicedIRI splits an IRI into a policed namespace and its local name.
// It matches the longest declared namespace so a vocabulary nested under
// another's prefix cannot be attributed to the wrong one.
func splitPolicedIRI(iri string) (namespace, local string, ok bool) {
	iri = canonicalIRI(iri)
	for ns := range policedVocabularies {
		if strings.HasPrefix(iri, ns) && len(ns) > len(namespace) {
			namespace, local, ok = ns, iri[len(ns):], true
		}
	}
	// A local name still carrying a path or fragment separator belongs to a
	// deeper namespace this guard does not police, not to this one.
	if ok && strings.ContainsAny(local, "/#") {
		return "", "", false
	}
	return namespace, local, ok
}

// belowTermLevel reports whether an IRI sits inside a policed namespace but
// deeper than its terms live, e.g. https://schema.org/docs/thing. The guard
// cannot say whether such a name is defined — it may belong to a sub-namespace
// nobody declared here — so it says so rather than passing it.
func belowTermLevel(iri string) (namespace string, deeper bool) {
	iri = canonicalIRI(iri)
	for ns := range policedVocabularies {
		if strings.HasPrefix(iri, ns) && strings.ContainsAny(iri[len(ns):], "/#") {
			return ns, true
		}
	}
	return "", false
}

// schemaPropertyNames lists a JSON Schema's declared property names, and
// reports whether the schema could be read at all. An unreadable schema is NOT
// an empty one: the caller turns that into a FaultCannotJudge rather than
// sweeping a type and finding it clean by never looking at it.
func schemaPropertyNames(schema json.RawMessage) (names []string, readable bool) {
	if len(schema) == 0 {
		return nil, true
	}
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(schema, &s) != nil {
		return nil, false
	}
	names = make([]string, 0, len(s.Properties))
	for name := range s.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, true
}

// ResolvedPredicateFor reports the predicate IRI a type's property states,
// whether it comes from an explicit term or from `@vocab`. Both routes end at
// an IRI, and callers that need to pin one should not have to know which route
// produced it.
func ResolvedPredicateFor(pt application.PresetResourceType, property string) string {
	vocab, forward := jsonld.ParseContext(normalizedContext(pt.Context))
	return jsonld.ResolvePredicateIRI(property, vocab, forward)
}

// UnusedAllowListEntries reports curated names no type resolves any more. An
// allow-list nobody prunes becomes a rubber stamp: every stale entry is a name
// a future property could adopt without anyone re-checking that it fits.
//
// The food ontology is exempt from the check. Its list is the vocabulary's
// complete published set rather than a record of what the presets borrow, so
// an unused entry there is the list being complete, not the list rotting.
func UnusedAllowListEntries(types []application.PresetResourceType) []string {
	used := map[string]bool{}
	for _, pt := range types {
		vocab, forward := jsonld.ParseContext(normalizedContext(pt.Context))
		props, _ := schemaPropertyNames(pt.Schema)
		for _, prop := range props {
			used[canonicalIRI(jsonld.ResolvePredicateIRI(prop, vocab, forward))] = true
		}
	}
	var stale []string
	for ns, names := range policedVocabularies {
		if ns == "http://purl.org/foodontology#" {
			continue
		}
		for name := range names {
			if !used[ns+name] {
				stale = append(stale, ns+name)
			}
		}
	}
	sort.Strings(stale)
	return stale
}
