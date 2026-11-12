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
// came from full text or the degraded label search. Each full-text mode names
// its engine; use IsFullTextMode rather than comparing against one of them.
const (
	LexicalModeFTS5        = repositories.LexicalEngineFTS5
	LexicalModePostgresFTS = repositories.LexicalEnginePostgres
	LexicalModeGraphLabels = "graph-labels"
)

// IsFullTextMode reports whether a lexical search mode is a full-text index
// (any engine) rather than the degraded graph label search.
func IsFullTextMode(mode string) bool {
	return mode == LexicalModeFTS5 || mode == LexicalModePostgresFTS
}

// ErrLexicalUnavailable is returned when neither the full-text index nor the
// knowledge graph is available to answer a lexical search.
var ErrLexicalUnavailable = errors.New(
	"lexical search unavailable: no full-text index and no knowledge graph configured")

// LexicalSearch finds resources by keyword. Full-text first (SQLite FTS5, or a
// PostgreSQL tsvector index); where the index is inactive it degrades to the
// knowledge graph's label search.
type LexicalSearch interface {
	// Search returns matches plus the mode that produced them (a full-text
	// mode — see IsFullTextMode — or LexicalModeGraphLabels). When typeSlugs
	// is non-empty only resources of those types are returned, on either
	// path, and the limit is spent on those types alone.
	Search(ctx context.Context, query string, limit int, typeSlugs ...string) ([]repositories.LexicalHit, string, error)
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
	ctx context.Context, query string, limit int, typeSlugs ...string,
) ([]repositories.LexicalHit, string, error) {
	if limit <= 0 {
		limit = defaultRecallLimit
	}
	if limit > maxRecallLimit {
		limit = maxRecallLimit
	}
	types := typeSlugFilter(typeSlugs)
	if s.index != nil && s.index.Active() {
		hits, err := s.index.Search(ctx, query, limit, types...)
		return hits, s.index.Engine(), err
	}
	if s.kg == nil || !s.kg.Active() {
		return nil, "", ErrLexicalUnavailable
	}
	// Label search knows classes, not resource types, so the type filter is
	// applied to the resource URN (urn:<typeSlug>:<id>). A narrowed search
	// asks the graph for its maximum so the other types' labels cannot spend
	// the limit before the narrowed type is reached.
	fetch := limit
	if len(types) > 0 {
		fetch = maxRecallLimit
	}
	terms, err := s.kg.SearchEntities(ctx, query, "", fetch)
	if err != nil {
		return nil, "", err
	}
	hits := make([]repositories.LexicalHit, 0, len(terms))
	for _, t := range terms {
		if len(types) > 0 && !urnHasType(t.Value, types) {
			continue
		}
		hits = append(hits, repositories.LexicalHit{ID: t.Value})
	}
	hits, err = s.dropSupersededFacts(ctx, hits)
	if err != nil {
		return nil, "", err
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, LexicalModeGraphLabels, nil
}

// typeSlugFilter trims and lowercases type slugs, which are always stored
// lowercase, so "Task " filters to tasks. Blank entries are dropped, so a
// filter of only blanks is no filter.
func typeSlugFilter(values []string) []string {
	var out []string
	for _, v := range values {
		if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// urnHasType reports whether a resource URN (urn:<typeSlug>:<id>) names one
// of the given types.
func urnHasType(urn string, types []string) bool {
	for _, t := range types {
		if strings.HasPrefix(urn, "urn:"+t+":") {
			return true
		}
	}
	return false
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
