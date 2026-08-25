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
	"testing"
)

// Issue #522: the boot's completeness check walks the reverse map to decide
// whether a reference property's writes would be dropped. A control keyword
// that expanded onto the property's predicate could steal the reverse entry
// and make a healthy property look uncovered.
func TestReferencePropertiesWithoutContextEntry_IgnoresAControlKeyword(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{
	  "maker":{"type":"string","x-resource-type":"vendor"}}}`)
	ctx := json.RawMessage(`{"@vocab":"https://schema.org/",
	  "maker":{"@id":"https://example.org/catalog#madeBy","@type":"@id"},
	  "rdfs:subClassOf":"https://example.org/catalog#madeBy"}`)
	if dropped := referencePropertiesWithoutContextEntry(schema, ctx); len(dropped) != 0 {
		t.Errorf("maker is covered by its own term; reported as dropped: %v", dropped)
	}
}
