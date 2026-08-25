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
//     collapsing onto one predicate read back as one;
//   - no expanded predicate IRI keeps an undeclared compact prefix in its
//     local name (`https://schema.org/foaf:knows` is what a compact IRI
//     becomes when @vocab absorbs an undeclared prefix);
//   - no control keyword enters the term map or claims a predicate (#522).
//
// The built-in registry is swept by the tests beside this file; it is
// exported so a private preset registry (the overlay build) can sweep its own
// types in its own CI — the population that actually declares
// rdfs:subClassOf is not the built-in one.
func ContextGuardViolations(types []application.PresetResourceType) []string {
	var out []string
	for _, pt := range types {
		out = append(out, referenceGuard(pt)...)
		out = append(out, compactPrefixGuard(pt)...)
		out = append(out, controlKeywordGuard(pt)...)
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
