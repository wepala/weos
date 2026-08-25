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

func TestPrintNormalizeEdgeKeysReport_DryRunCarriesTheMarkers(t *testing.T) {
	report := application.NormalizeEdgeKeysReport{
		DryRun: true, Scanned: 3, Rewritten: 1,
		Types: map[string]*application.EdgeKeyTypeReport{
			"widget": {Scanned: 2, Rewritten: 1},
			"vendor": {Scanned: 1, Rewritten: 0},
		},
		Ambiguous: []application.EdgeKeyProblem{{TypeSlug: "widget", ResourceID: "urn:widget:1",
			EventID: "evt-1", Position: 7,
			Key: "https://schema.org/associated", Candidates: []string{"maker", "partner"}, Reason: "decide"}},
		AmbiguousTotal: 3,
		Unresolved: []application.EdgeKeyProblem{{TypeSlug: "vendor", ResourceID: "urn:vendor:1",
			EventID: "evt-2", Position: 9, Key: "https://example.org/legacy#x", Reason: "nothing names it"}},
		UnresolvedTotal: 1,
		Collisions: []application.EdgeKeyProblem{{TypeSlug: "widget", ResourceID: "urn:widget:2",
			EventID: "evt-3", Position: 11, Key: "https://schema.org/maker", Candidates: []string{"maker"},
			Reason: "taken"}},
		CollisionTotal: 1,
		Skipped:        map[string]int{"Data is not a JSON object": 2},
	}
	var out bytes.Buffer
	printNormalizeEdgeKeysReport(&out, report)
	text := out.String()
	for _, want := range []string{
		"DRY RUN",
		"skipped 2 event(s): Data is not a JSON object",
		"would rewrite 1 of 2 event(s); 1 ambiguous edge(s); 1 colliding edge(s)",
		"vendor", "1 unresolved edge(s)",
		"ambiguous edge key https://schema.org/associated on urn:widget:1 (widget) in event evt-1 at position 7: " +
			"candidates maker, partner",
		"… and 2 more ambiguous edge key line(s) not listed",
		"unresolved edge key https://example.org/legacy#x on urn:vendor:1 (vendor) in event evt-2 at position 9",
		"colliding edge key https://schema.org/maker on urn:widget:2 (widget) in event evt-3 at position 11",
		"5 edge(s) were not rewritten",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Next (server stopped)") {
		t.Error("a dry run must not tell the operator to reproject")
	}
}

func TestPrintNormalizeEdgeKeysReport_WriteNamesTheNextStep(t *testing.T) {
	report := application.NormalizeEdgeKeysReport{
		Scanned: 1, Rewritten: 1,
		Types: map[string]*application.EdgeKeyTypeReport{"widget": {Scanned: 1, Rewritten: 1}},
	}
	var out bytes.Buffer
	printNormalizeEdgeKeysReport(&out, report)
	text := out.String()
	if !strings.Contains(text, "rewrote 1 of 1 event(s)") ||
		!strings.Contains(text, "weos worker reproject") ||
		!strings.Contains(text, "checkpoint reset oxigraph --truncate") {
		t.Errorf("write report is incomplete:\n%s", text)
	}
}

func TestPrintNormalizeEdgeKeysReport_NothingToDo(t *testing.T) {
	var out bytes.Buffer
	printNormalizeEdgeKeysReport(&out, application.NormalizeEdgeKeysReport{DryRun: true})
	if !strings.Contains(out.String(), "nothing to rewrite") {
		t.Errorf("empty report should say nothing to rewrite:\n%s", out.String())
	}
}

func TestPrintNormalizeEdgeKeysReport_Restamp(t *testing.T) {
	report := application.NormalizeEdgeKeysReport{
		Restamp: true, Scanned: 4, Restamped: 2, TriplesMoved: 3,
		Types: map[string]*application.EdgeKeyTypeReport{
			"food-item": {Scanned: 2, Restamped: 2, TriplesMoved: 3},
			"pantry":    {Scanned: 2},
		},
	}
	var out bytes.Buffer
	printNormalizeEdgeKeysReport(&out, report)
	text := out.String()
	for _, want := range []string{
		"food-item", "re-stamped 2 (+3 triple event(s) moved)",
		"pantry", "re-stamped 0",
		"Next (server stopped)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("re-stamp report lacks %q:\n%s", want, text)
		}
	}
	out.Reset()
	printNormalizeEdgeKeysReport(&out, application.NormalizeEdgeKeysReport{Restamp: true, DryRun: true,
		Types: map[string]*application.EdgeKeyTypeReport{"pantry": {Scanned: 1}}})
	if !strings.Contains(out.String(), "nothing to rewrite or re-stamp") {
		t.Errorf("a re-stamp that found nothing must say so:\n%s", out.String())
	}
}
