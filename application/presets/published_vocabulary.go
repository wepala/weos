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
		"image", "inLanguage", "isPartOf", "itemListElement", "keywords", "location", "mainEntity",
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
var namespaceAliases = map[string]string{
	"http://schema.org/": "https://schema.org/",
}

// termsPublishedForAnotherSubject holds names that DO resolve but whose
// published meaning is wrong for the WeOS use, mapped to what they actually
// mean. An allow-list can never catch these, because the term exists — which is
// exactly what makes them the half a 404 check misses.
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

// vocabularyWaivers names the violations that exist today and are accepted
// until their own ticket repairs them, each mapped to why it is still here.
//
// THE SWEEP ASSERTS THE VIOLATION SET EQUALS THIS MAP — not that it contains no
// meal-planning entry. Exact equality is what makes the list only ever shrink:
// a new offender anywhere fails on the day it is authored, which is the whole
// point of the guard, and repairing one fails until its line is deleted here.
//
// #535 repaired meal-planning. No meal-planning waiver survives it, and none
// may be added: a new meal-planning entry in this map means the repair
// regressed.
var vocabularyWaivers = map[string]string{
	// core — an avatar and a logo are URLs schema.org spells differently
	// (`image`), and `slug` it does not publish at all.
	"person.avatarURL":     "#535 waiver: core preset, not yet repaired",
	"organization.logoURL": "#535 waiver: core preset, not yet repaired",
	"organization.slug":    "#535 waiver: core preset, not yet repaired",

	// knowledge — SKOS publishes neither; both are Dublin Core terms.
	"concept-scheme.title":       "#535 waiver: knowledge preset, not yet repaired",
	"concept-scheme.description": "#535 waiver: knowledge preset, not yet repaired",

	// notifications — the largest group, and entirely house concepts that
	// were never schema.org's to name.
	"notification.actionLabel": "#535 waiver: notifications preset, not yet repaired",
	"notification.actionUrl":   "#535 waiver: notifications preset, not yet repaired",
	"notification.body":        "#535 waiver: notifications preset, not yet repaired",
	"notification.dedupeKey":   "#535 waiver: notifications preset, not yet repaired",
	"notification.kind":        "#535 waiver: notifications preset, not yet repaired",
	"notification.occurredAt":  "#535 waiver: notifications preset, not yet repaired",
	"notification.read":        "#535 waiver: notifications preset, not yet repaired",
	"notification.taskRef":     "#535 waiver: notifications preset, not yet repaired",
	"notification.title":       "#535 waiver: notifications preset, schema:title is a JobPosting term",

	// tasks — `dueDate` and `priority` are mints; both `status` entries are
	// the medical term, the same subject misuse #535 removed from
	// meal-occurrence and shopping-list.
	"task.dueDate":   "#535 waiver: tasks preset, not yet repaired",
	"task.priority":  "#535 waiver: tasks preset, not yet repaired",
	"task.status":    "#535 waiver: tasks preset, not yet repaired",
	"project.status": "#535 waiver: tasks preset, not yet repaired",

	// website — template machinery schema.org has no vocabulary for.
	"web-page.slug":                  "#535 waiver: website preset, not yet repaired",
	"web-page.template":              "#535 waiver: website preset, not yet repaired",
	"web-page-element.content":       "#535 waiver: website preset, not yet repaired",
	"web-page-template.slots":        "#535 waiver: website preset, not yet repaired",
	"web-page-template.templateBody": "#535 waiver: website preset, not yet repaired",
}

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
		vocab, forward := jsonld.ParseContext(pt.Context)
		for _, prop := range schemaPropertyNames(pt.Schema) {
			iri := jsonld.ResolvePredicateIRI(prop, vocab, forward)
			namespace, local, ok := splitPolicedIRI(iri)
			if !ok {
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

// schemaPropertyNames lists a JSON Schema's declared property names. A schema
// that will not parse yields none: the schema reconcile reports malformed
// schemas on its own, and duplicating that here would report one fault twice.
func schemaPropertyNames(schema json.RawMessage) []string {
	if len(schema) == 0 {
		return nil
	}
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(schema, &s) != nil {
		return nil
	}
	names := make([]string, 0, len(s.Properties))
	for name := range s.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ResolvedPredicateFor reports the predicate IRI a type's property states,
// whether it comes from an explicit term or from `@vocab`. Both routes end at
// an IRI, and callers that need to pin one should not have to know which route
// produced it.
func ResolvedPredicateFor(pt application.PresetResourceType, property string) string {
	vocab, forward := jsonld.ParseContext(pt.Context)
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
		vocab, forward := jsonld.ParseContext(pt.Context)
		for _, prop := range schemaPropertyNames(pt.Schema) {
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
