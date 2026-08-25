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

package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/application"
)

// Issue #521: the operator-facing text around a held class. A sweep never
// adopts @type, so both the listing and the sweep's own outcome must name
// the command that does — "already up to date" while the boot keeps warning
// was the defect.
func TestPrintHeldTerms_AHeldClass(t *testing.T) {
	var out bytes.Buffer
	printHeldTerms(&out, "core", "person", "https://schema.org/Person", []application.HeldTerm{
		{Term: "@type", Kind: application.HeldTermAdded, Property: "@type", PresetIRI: "http://xmlns.com/foaf/0.1/Person",
			Moves: []application.HeldMove{{Property: "@type", PresetIRI: "http://xmlns.com/foaf/0.1/Person"}}},
	})
	text := out.String()
	for _, want := range []string{
		"class today     https://schema.org/Person (no class declared; the type name through @vocab)",
		"checkpoint reset oxigraph --truncate",
		"preset declares http://xmlns.com/foaf/0.1/Person",
		"Adopt with:  weos resource-type adopt-term core person --term @type",
		"Note:        a sweep never moves the class",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("held-terms output lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "--all") {
		t.Errorf("a lone held class must not be offered a sweep:\n%s", text)
	}
}

func TestPrintHeldTerms_ARepointedClassBesideATerm(t *testing.T) {
	var out bytes.Buffer
	printHeldTerms(&out, "catalog", "widget", "https://schema.org/Thing", []application.HeldTerm{
		{Term: "supplier", Kind: application.HeldTermAdded, Property: "supplier", StoredIRI: "https://schema.org/supplier",
			PresetIRI: "https://example.org/catalog#supplier", Moves: []application.HeldMove{{Property: "supplier",
				StoredIRI: "https://schema.org/supplier", PresetIRI: "https://example.org/catalog#supplier"}}},
		{Term: "@type", Kind: application.HeldTermRedefined, Property: "@type", StoredIRI: "https://schema.org/Thing",
			PresetIRI: "https://schema.org/Product", Moves: []application.HeldMove{{Property: "@type",
				StoredIRI: "https://schema.org/Thing", PresetIRI: "https://schema.org/Product"}}},
	})
	text := out.String()
	for _, want := range []string{
		"data written as https://schema.org/supplier",
		"class today     https://schema.org/Thing",
		"Adopt with:  weos resource-type adopt-term catalog widget --term @type && " +
			"weos resource-type adopt-term catalog widget --all",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("held-terms output lacks %q:\n%s", want, text)
		}
	}
}

func TestPrintAdoptOutcome_ASweepThatLeftTheClassSaysSo(t *testing.T) {
	var out bytes.Buffer
	printAdoptOutcome(&out, "core", "person", true, application.AdoptResult{},
		[]application.HeldTerm{{Term: "@type", Property: "@type", Moves: []application.HeldMove{{Property: "@type"}}}})
	text := out.String()
	if !strings.Contains(text, "is still held") || !strings.Contains(text, "--term @type") ||
		strings.Contains(text, "already up to date") {
		t.Errorf("a sweep that skipped the class must say so and name the command:\n%s", text)
	}
	out.Reset()
	printAdoptOutcome(&out, "core", "person", false, application.AdoptResult{Adopted: []string{"@type"},
		ClassMove: &application.HeldMove{Property: "@type", StoredIRI: "https://schema.org/Person",
			PresetIRI: "http://xmlns.com/foaf/0.1/Person"}}, nil)
	if !strings.Contains(out.String(), `Adopted for "person": @type`) ||
		!strings.Contains(out.String(), "moves from https://schema.org/Person to http://xmlns.com/foaf/0.1/Person") ||
		!strings.Contains(out.String(), "--restamp --type person --write") {
		t.Errorf("adopting a class by name reports the move and the re-stamp:\n%s", out.String())
	}
	out.Reset()
	printAdoptOutcome(&out, "catalog", "widget", true, application.AdoptResult{}, nil)
	if !strings.Contains(out.String(), "already up to date") {
		t.Errorf("a clean type is up to date:\n%s", out.String())
	}
}

func TestPrintHeldTerms_APrefixThatMovesTheClass(t *testing.T) {
	var out bytes.Buffer
	printHeldTerms(&out, "core", "person", "https://schema.org/Person", []application.HeldTerm{
		{Term: "foaf", Kind: application.HeldTermAdded, Property: "@type", StoredIRI: "https://schema.org/foaf:Person",
			PresetIRI: "http://xmlns.com/foaf/0.1/Person", Moves: []application.HeldMove{{Property: "@type",
				StoredIRI: "https://schema.org/foaf:Person", PresetIRI: "http://xmlns.com/foaf/0.1/Person"}}},
	})
	if !strings.Contains(out.String(), "Adopt with:  weos resource-type adopt-term core person --term foaf") ||
		strings.Contains(out.String(), "--all") {
		t.Errorf("a prefix that moves the class must be named, never swept:\n%s", out.String())
	}
}

func TestPrintAdoptOutcome_ASweepThatLeftAClassMovingPrefixNamesIt(t *testing.T) {
	var out bytes.Buffer
	printAdoptOutcome(&out, "core", "person", true, application.AdoptResult{},
		[]application.HeldTerm{{Term: "foaf", Property: "@type", Moves: []application.HeldMove{{Property: "@type"}}}})
	text := out.String()
	if !strings.Contains(text, "foaf (the class) is still held") || !strings.Contains(text, "--term foaf") ||
		strings.Contains(text, "--term @type") {
		t.Errorf("a sweep that skipped a class-moving prefix must name that prefix:\n%s", text)
	}
}

func TestPrintAdoptOutcome_EveryAdoptionNamesTheRestampRoute(t *testing.T) {
	var out bytes.Buffer
	printAdoptOutcome(&out, "catalog", "widget", false, application.AdoptResult{Adopted: []string{"maker"}}, nil)
	text := out.String()
	if !strings.Contains(text, "--restamp --type widget --write") || !strings.Contains(text, "checkpoint reset oxigraph") {
		t.Errorf("a plain adoption must still name the re-stamp route:\n%s", text)
	}
}

func TestPrintAdoptOutcome_ASweepThatLeftVocabNamesIt(t *testing.T) {
	var out bytes.Buffer
	printAdoptOutcome(&out, "memory", "fact", true, application.AdoptResult{},
		[]application.HeldTerm{{Term: "@vocab", Property: "confidence", Moves: []application.HeldMove{{Property: "confidence"}}}})
	text := out.String()
	if !strings.Contains(text, "@vocab is still held") || !strings.Contains(text, "--term @vocab") ||
		strings.Contains(text, "--all") {
		t.Errorf("a sweep that skipped @vocab must name --term @vocab:\n%s", text)
	}
}

func TestPrintHeldTerms_ARedefinedPrefixListsEveryMove(t *testing.T) {
	var out bytes.Buffer
	printHeldTerms(&out, "catalog", "widget", "", []application.HeldTerm{
		{Term: "cat", Kind: application.HeldTermRedefined, Property: "maker",
			StoredIRI: "https://schema.org/", PresetIRI: "https://example.org/catalog#",
			Moves: []application.HeldMove{
				{Property: "maker", StoredIRI: "https://schema.org/madeBy", PresetIRI: "https://example.org/catalog#madeBy"},
				{Property: "@type", StoredIRI: "https://schema.org/Widget", PresetIRI: "https://example.org/catalog#Widget"},
			}},
		{Term: "empty", Kind: application.HeldTermRedefined},
	})
	text := out.String()
	for _, want := range []string{
		"stored as       https://schema.org/", "preset wants    https://example.org/catalog#",
		"property        maker", "adopting keeps edges under https://schema.org/madeBy readable",
		"moves the class https://schema.org/Widget -> https://example.org/catalog#Widget",
		"--term cat",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

func TestPrintAdoptOutcome_ASweepThatLeftATermBehindItsPrefixSaysSo(t *testing.T) {
	var out bytes.Buffer
	printAdoptOutcome(&out, "catalog", "widget", true, application.AdoptResult{StillHeld: []string{"cat", "supplier"}},
		[]application.HeldTerm{{Term: "cat", Property: "@type", Moves: []application.HeldMove{{Property: "@type"}}}})
	text := out.String()
	if !strings.Contains(text, "cat, supplier (the class) is still held") ||
		!strings.Contains(text, "prefix it left held") ||
		!strings.Contains(text, "--term cat && weos resource-type adopt-term catalog widget --all") {
		t.Errorf("the prefix must be named before the sweep:\n%s", text)
	}
}
