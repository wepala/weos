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
	"errors"
	"fmt"
	"strings"

	"github.com/wepala/weos/v3/domain/repositories"
)

// Lexical search modes reported to callers so agents know whether results
// came from full text or the degraded label search.
const (
	LexicalModeFTS5        = "fts5"
	LexicalModeGraphLabels = "graph-labels"
)

// ErrLexicalUnavailable is returned when neither the FTS5 index nor the
// knowledge graph is available to answer a lexical search.
var ErrLexicalUnavailable = errors.New(
	"lexical search unavailable: no FTS5 index and no knowledge graph configured")

// LexicalSearch finds resources by keyword. FTS5-first; on installations
// without FTS5 (PostgreSQL, SQLite builds lacking the sqlite_fts5 tag) it
// degrades to the knowledge graph's label search.
type LexicalSearch interface {
	// Search returns matches plus the mode that produced them
	// (LexicalModeFTS5 or LexicalModeGraphLabels).
	Search(ctx context.Context, query string, limit int) ([]repositories.LexicalHit, string, error)
}

type lexicalSearch struct {
	index repositories.LexicalIndex
	kg    KnowledgeGraphService
}

// NewLexicalSearch builds the lexical search service. kg may be nil.
func NewLexicalSearch(index repositories.LexicalIndex, kg KnowledgeGraphService) LexicalSearch {
	return &lexicalSearch{index: index, kg: kg}
}

func (s *lexicalSearch) Search(
	ctx context.Context, query string, limit int,
) ([]repositories.LexicalHit, string, error) {
	if limit <= 0 {
		limit = defaultRecallLimit
	}
	if limit > maxRecallLimit {
		limit = maxRecallLimit
	}
	if s.index != nil && s.index.Active() {
		hits, err := s.index.Search(ctx, query, limit)
		return hits, LexicalModeFTS5, err
	}
	if s.kg == nil || !s.kg.Active() {
		return nil, "", ErrLexicalUnavailable
	}
	terms, err := s.kg.SearchEntities(ctx, query, "", limit)
	if err != nil {
		return nil, "", err
	}
	hits := make([]repositories.LexicalHit, 0, len(terms))
	for _, t := range terms {
		hits = append(hits, repositories.LexicalHit{ID: t.Value})
	}
	hits, err = s.dropSupersededFacts(ctx, hits)
	if err != nil {
		return nil, "", err
	}
	return hits, LexicalModeGraphLabels, nil
}

// dropSupersededFacts upholds the "superseded facts are never returned"
// guarantee on the fallback path: SearchEntities knows nothing about
// prov:invalidatedAtTime, so any fact hits are checked against the graph and
// invalidated ones removed.
func (s *lexicalSearch) dropSupersededFacts(
	ctx context.Context, hits []repositories.LexicalHit,
) ([]repositories.LexicalHit, error) {
	var factIDs []string
	for _, h := range hits {
		if strings.HasPrefix(h.ID, "urn:fact:") {
			factIDs = append(factIDs, h.ID)
		}
	}
	if len(factIDs) == 0 {
		return hits, nil
	}
	var b strings.Builder
	b.WriteString("SELECT ?f WHERE { VALUES ?f {")
	for _, id := range factIDs {
		fmt.Fprintf(&b, " <%s>", id)
	}
	fmt.Fprintf(&b, " } ?f <%s> ?invalidated }", factInvalidatedAtIRI)
	res, err := s.kg.Query(ctx, b.String())
	if err != nil {
		return nil, fmt.Errorf("lexical search: supersession check: %w", err)
	}
	invalidated := make(map[string]bool, len(res.Bindings))
	for _, row := range res.Bindings {
		invalidated[row["f"].Value] = true
	}
	kept := hits[:0]
	for _, h := range hits {
		if !invalidated[h.ID] {
			kept = append(kept, h)
		}
	}
	return kept, nil
}
