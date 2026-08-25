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
	"strings"
	"testing"
)

// Issue #520: an existing install keeps the weos.org Fact class until the
// held prefix is adopted and its records re-stamped, so recall must find
// facts under either class and read confidence from either predicate — once.
func TestBuildRecallSPARQL_AcceptsBothHouseDomains(t *testing.T) {
	q := buildRecallSPARQL(RecallQuery{}, 10)
	for _, want := range []string{
		"VALUES ?factClass { <https://weos.io/vocab/memory#Fact> <https://weos.org/vocab/memory#Fact> }",
		"?fact a ?factClass",
		"<https://weos.io/vocab/memory#confidence> ?confidenceNow",
		"<https://weos.org/vocab/memory#confidence> ?confidenceLegacy",
		"BIND(COALESCE(?confidenceNow, ?confidenceLegacy) AS ?confidence)",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("recall SPARQL lacks %q:\n%s", want, q)
		}
	}
}
