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
		{Term: "@type", Property: "@type", PresetIRI: "http://xmlns.com/foaf/0.1/Person"},
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
		{Term: "supplier", Property: "supplier", StoredIRI: "https://schema.org/supplier",
			PresetIRI: "https://example.org/catalog#supplier"},
		{Term: "@type", Property: "@type", StoredIRI: "https://schema.org/Thing", PresetIRI: "https://schema.org/Product"},
	})
	text := out.String()
	for _, want := range []string{
		"data written as https://schema.org/supplier",
		"class today     https://schema.org/Thing",
		"Adopt with:  weos resource-type adopt-term catalog widget --all && " +
			"weos resource-type adopt-term catalog widget --term @type",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("held-terms output lacks %q:\n%s", want, text)
		}
	}
}

func TestPrintAdoptOutcome_ASweepThatLeftTheClassSaysSo(t *testing.T) {
	var out bytes.Buffer
	printAdoptOutcome(&out, "core", "person", true, nil, []application.HeldTerm{{Term: "@type", Property: "@type"}})
	text := out.String()
	if !strings.Contains(text, "@type is still held") || !strings.Contains(text, "--term @type") ||
		strings.Contains(text, "already up to date") {
		t.Errorf("a sweep that skipped the class must say so and name the command:\n%s", text)
	}
	out.Reset()
	printAdoptOutcome(&out, "core", "person", false, []string{"@type"}, nil)
	if !strings.Contains(out.String(), `Adopted for "person": @type`) {
		t.Errorf("adopting by name reports it:\n%s", out.String())
	}
	out.Reset()
	printAdoptOutcome(&out, "catalog", "widget", true, nil, nil)
	if !strings.Contains(out.String(), "already up to date") {
		t.Errorf("a clean type is up to date:\n%s", out.String())
	}
}

func TestPrintHeldTerms_APrefixThatMovesTheClass(t *testing.T) {
	var out bytes.Buffer
	printHeldTerms(&out, "core", "person", "https://schema.org/Person", []application.HeldTerm{
		{Term: "foaf", Property: "@type", StoredIRI: "https://schema.org/foaf:Person",
			PresetIRI: "http://xmlns.com/foaf/0.1/Person"},
	})
	if !strings.Contains(out.String(), "Adopt with:  weos resource-type adopt-term core person --term foaf") ||
		strings.Contains(out.String(), "--all") {
		t.Errorf("a prefix that moves the class must be named, never swept:\n%s", out.String())
	}
}
