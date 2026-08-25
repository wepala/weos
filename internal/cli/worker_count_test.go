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

func TestPrintIRIEdgeKeyCountReport_FailCarriesTheMarkers(t *testing.T) {
	report := application.IRIEdgeKeyCountReport{
		Types: map[string]*application.IRIEdgeKeyTypeCount{
			"widget": {Events: 2, Records: 3, Resolvable: 1, Ambiguous: 1, Unmapped: 1, Residue: 2},
			"vendor": {Events: 0, Records: 1},
		},
		EventsTotal: 2, RecordsTotal: 4, ResidueTotal: 2,
		Classified: []application.EdgeKeyClassification{
			{TypeSlug: "widget", ResourceID: "urn:widget:1", Key: "https://schema.org/associated",
				Class: application.EdgeKeyAmbiguous, Candidates: []string{"maker", "partner"}},
			{TypeSlug: "widget", ResourceID: "urn:widget:2", Key: "https://example.org/legacy#x",
				Class: application.EdgeKeyUnmapped},
		},
		ClassifiedTotal: 5,
		Skipped:         map[string]int{"canonical record is not a JSON object": 1},
	}
	var out bytes.Buffer
	printIRIEdgeKeyCountReport(&out, report, true)
	text := out.String()
	for _, want := range []string{
		"skipped 1 row(s): canonical record is not a JSON object",
		"1 / 1 / 1 / 0",
		"2 resource(s) hold an ambiguous, unmapped or colliding key",
		"ambiguous edge key https://schema.org/associated on urn:widget:1 (widget): candidates maker, partner",
		"unmapped edge key https://example.org/legacy#x on urn:widget:2 (widget)",
		"… and 3 more not listed",
		"check: FAIL",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report lacks %q:\n%s", want, text)
		}
	}
}

func TestPrintIRIEdgeKeyCountReport_TheReprojectWindow(t *testing.T) {
	report := application.IRIEdgeKeyCountReport{
		Types:        map[string]*application.IRIEdgeKeyTypeCount{"widget": {Records: 1}},
		RecordsTotal: 1,
	}
	var out bytes.Buffer
	printIRIEdgeKeyCountReport(&out, report, true)
	if !strings.Contains(out.String(), "run `weos worker reproject`") || !strings.Contains(out.String(), "check: FAIL") {
		t.Errorf("the reproject window is not named:\n%s", out.String())
	}
}

func TestPrintIRIEdgeKeyCountReport_Pass(t *testing.T) {
	var out bytes.Buffer
	printIRIEdgeKeyCountReport(&out, application.IRIEdgeKeyCountReport{}, true)
	if !strings.Contains(out.String(), "check: PASS") {
		t.Errorf("an empty report must pass:\n%s", out.String())
	}
}

func TestPrintIRIEdgeKeyCountReport_SkippedRowsNeverPass(t *testing.T) {
	report := application.IRIEdgeKeyCountReport{Skipped: map[string]int{"event names no resource type": 3}}
	if report.Passes() {
		t.Fatal("a report with skipped rows must not pass")
	}
	var out bytes.Buffer
	printIRIEdgeKeyCountReport(&out, report, true)
	if !strings.Contains(out.String(), "check: INCONCLUSIVE") || strings.Contains(out.String(), "check: PASS") {
		t.Errorf("skipped rows must print INCONCLUSIVE:\n%s", out.String())
	}
	out.Reset()
	printIRIEdgeKeyCountReport(&out, report, false)
	if strings.Contains(out.String(), "check:") {
		t.Errorf("a scan that did not finish must print no verdict:\n%s", out.String())
	}
}
