//go:build oxigraph_embedded

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

package oxigraph_test

import (
	"context"
	"encoding/json"
	"testing"
)

// Issue #521: the ontology projection loads a resource type's context as a
// CONTEXT-ONLY document with @type and the WeOS control entries removed — the
// shape application.ontologyDocument produces for the post-adoption core
// person context. The store must take it without error, and it must add no
// triples (the class comes from the explicit ontology triples), so a boot
// cannot mint a blank node per type any more.
func TestContextOnlyOntologyDocumentLoadsAndAddsNothing(t *testing.T) {
	store := newStore(t)
	doc := json.RawMessage(`{"@context":{"@vocab":"https://schema.org/","foaf":"http://xmlns.com/foaf/0.1/",` +
		`"givenName":"https://schema.org/givenName"}}`)
	if err := store.LoadOntology(context.Background(), "application/ld+json", doc); err != nil {
		t.Fatalf("the store refused the context-only ontology document: %v", err)
	}
	rows, err := store.Query(context.Background(), "SELECT (COUNT(*) AS ?n) WHERE { ?s ?p ?o }")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["n"] != "0" {
		t.Errorf("a context-only document must add no triples, got %v", rows)
	}
}
