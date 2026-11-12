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

package application

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/graph/oxigraph"
)

// On a real graph with more than the label-search maximum of matches for each
// query word, the any-word search on the degraded path still counts every
// word a resource's labels hold: the one resource named for both words comes
// first, and a floor of two returns it alone (Copilot on PR #583).
func TestEmbeddedLabelSearch_AnyModeCountsCoveragePastEachWordsMaximum(t *testing.T) {
	store, err := oxigraph.NewEmbeddedStore(filepath.Join(t.TempDir(), "store"), nil)
	if err != nil {
		t.Fatalf("NewEmbeddedStore: %v", err)
	}
	t.Cleanup(func() {
		if c, ok := store.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	})

	ctx := context.Background()
	var triples []repositories.Triple
	for i := 0; i < 3*maxRecallLimit/2; i++ {
		triples = append(triples,
			repositories.Triple{
				Subject: fmt.Sprintf("urn:task:harbor%03d", i), Predicate: "https://schema.org/name",
				Object: fmt.Sprintf(`"Harbor berth %d"`, i),
			},
			repositories.Triple{
				Subject: fmt.Sprintf("urn:task:invoice%03d", i), Predicate: "https://schema.org/name",
				Object: fmt.Sprintf(`"Invoice %d"`, i),
			})
	}
	triples = append(triples, repositories.Triple{
		Subject: "urn:task:zz-both", Predicate: "https://schema.org/name", Object: `"Harbor invoice"`,
	})
	if err := store.AddTriples(ctx, triples); err != nil {
		t.Fatalf("AddTriples: %v", err)
	}
	s := NewLexicalSearch(nil, newKGSvc(store))

	hits, mode, err := s.Search(ctx, repositories.LexicalQuery{
		Text: "harbor invoice", Limit: 5, Match: repositories.MatchAny,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if mode != LexicalModeGraphLabels {
		t.Errorf("mode = %q", mode)
	}
	if len(hits) != 5 || hits[0].ID != "urn:task:zz-both" {
		t.Errorf("hits = %s, want 5 led by urn:task:zz-both", hitList(hits))
	}

	hits, _, err = s.Search(ctx, repositories.LexicalQuery{
		Text: "harbor invoice", Limit: 5, Match: repositories.MatchAny, MinMatch: 2,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := hitList(hits); got != "urn:task:zz-both" {
		t.Errorf("MinMatch 2 hits = %s, want urn:task:zz-both alone", got)
	}
}
