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
	"fmt"
	"strings"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/pkg/jsonld"
)

// ContextGuardViolations checks a set of preset types against the epic's
// (#517) vocabulary rules and returns one line per violation, empty when the
// set is clean:
//
//   - every reference property reverse-maps to its own name — two properties
//     collapsing onto one predicate read back as one. A reference with no
//     term of its own resolves through `@vocab` + its name, which the read
//     path (jsonld.EdgeProperty) and the boot's completeness check both
//     accept; that shape passes here for the same reason;
//   - no expanded predicate IRI keeps an undeclared compact prefix in its
//     local name (`https://schema.org/foaf:knows` is what a compact IRI
//     becomes when @vocab absorbs an undeclared prefix);
//   - no control keyword enters the term map or claims a predicate (#522);
//   - no property states a predicate the vocabulary it lands in does not
//     define, or borrows one published for another subject (#535). Waived
//     violations are excluded here — this function reports what is
//     unaccounted for, while the built-in sweep beside it asserts the raw set
//     equals the waivers.
//
// The built-in registry is swept by the tests beside this file; it is
// exported so a private preset registry (the overlay build) can sweep its own
// types in its own CI — the population that actually declares
// rdfs:subClassOf is not the built-in one.
//
// AN OVERLAY REGISTRY SHOULD NOT USE THIS FUNCTION FOR THE #535 SWEEP. The
// waivers it filters by are CORE's, and they are keyed by type slug alone.
// Slug uniqueness is asserted across the built-in registry only, so a private
// type that happens to reuse a built-in slug would have its violation
// silently waived by core's line for a different type. The #517 rules above
// are per-type and carry no such coupling, which is why the whole function is
// still worth calling for those.
//
// An overlay wanting the #535 sweep should call PublishedVocabularyViolations
// directly — it reports raw violations and filters nothing — and apply its own
// waiver list. That is the seam; there is no way to pass waivers through here
// without making core's list mean two different things.
func ContextGuardViolations(types []application.PresetResourceType) []string {
	var out []string
	for _, pt := range types {
		out = append(out, referenceGuard(pt)...)
		out = append(out, compactPrefixGuard(pt)...)
		out = append(out, controlKeywordGuard(pt)...)
	}
	out = append(out, publishedVocabularyGuard(types)...)
	return out
}

// publishedVocabularyGuard reports the #535 violations that no CORE waiver
// accounts for. It sweeps the whole set at once rather than per type because a
// waiver is keyed by slug, which only means anything across the set.
//
// The waivers are core's own, so this is correct for the built-in registry and
// misleading for anything else — see the caveat on ContextGuardViolations.
func publishedVocabularyGuard(types []application.PresetResourceType) []string {
	var out []string
	for _, v := range PublishedVocabularyViolations(types) {
		if _, waived := vocabularyWaivers[v.Key()]; waived {
			continue
		}
		out = append(out, v.String())
	}
	return out
}

func referenceGuard(pt application.PresetResourceType) []string {
	var out []string
	reverse := jsonld.BuildReverseMap(pt.Context)
	vocab, _ := jsonld.ParseContext(pt.Context)
	for _, ref := range application.ExtractReferenceProperties(pt.Schema, pt.Context) {
		got, ok := reverse[ref.PredicateIRI]
		switch {
		case ok && got != ref.PropertyName:
			out = append(out, fmt.Sprintf("%s: %s reverse-maps to %q, not %q", pt.Slug, ref.PredicateIRI, got, ref.PropertyName))
		case !ok && (vocab == "" || ref.PredicateIRI != vocab+ref.PropertyName):
			out = append(out, fmt.Sprintf("%s: %s (property %q) has no reverse entry",
				pt.Slug, ref.PredicateIRI, ref.PropertyName))
		}
	}
	return out
}

func compactPrefixGuard(pt application.PresetResourceType) []string {
	var out []string
	_, forward := jsonld.ParseContext(pt.Context)
	for term, iri := range forward {
		if strings.HasSuffix(iri, "#") || strings.HasSuffix(iri, "/") {
			continue // a prefix definition maps a prefix to a namespace
		}
		if !strings.Contains(iri, "://") {
			continue // a URN, mailto: or did: term has no local name to test
		}
		local := iri
		if i := strings.LastIndexAny(iri, "#/"); i >= 0 {
			local = iri[i+1:]
		}
		if strings.Contains(local, ":") {
			out = append(out, fmt.Sprintf("%s: %q resolves to %s, whose local name keeps an undeclared prefix",
				pt.Slug, term, iri))
		}
	}
	return out
}

func controlKeywordGuard(pt application.PresetResourceType) []string {
	var out []string
	_, forward := jsonld.ParseContext(pt.Context)
	for key := range jsonld.ControlKeywords {
		if iri, present := forward[key]; present {
			out = append(out, fmt.Sprintf("%s: control keyword %s entered the term map as %s", pt.Slug, key, iri))
		}
	}
	for iri, name := range jsonld.BuildReverseMap(pt.Context) {
		if jsonld.ControlKeywords[name] {
			out = append(out, fmt.Sprintf("%s: %s reverse-maps to the control keyword %s", pt.Slug, iri, name))
		}
	}
	return out
}
