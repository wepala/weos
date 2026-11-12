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
	"sort"
	"strings"

	"github.com/wepala/weos/v3/domain/lexical"
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
// knowledge graph's label search. Either way the answer holds only resources
// the caller on ctx may read: the index applies the FindAccessibleIDs rule in
// its query, and the graph applies its own access filter to label search.
type LexicalSearch interface {
	// Search returns matches plus the mode that produced them (a full-text
	// mode — see IsFullTextMode — or LexicalModeGraphLabels). When
	// q.TypeSlugs is non-empty only resources of those types are returned, on
	// either path, and the limit is spent on those types alone. q.Match picks
	// all words (the default) or any content word, ranked by how many each
	// hit holds; see repositories.LexicalIndex.Search. A limit of 0 or less
	// is the default, and a limit above the maximum is the maximum.
	Search(ctx context.Context, q repositories.LexicalQuery) ([]repositories.LexicalHit, string, error)
}

// LexicalIndexProgress is implemented by a LexicalSearch whose full-text
// index can say how far it is behind the event history. It is a separate
// interface so a LexicalSearch written elsewhere keeps compiling.
type LexicalIndexProgress interface {
	// IndexLag is LexicalIndex.Lag for the index the search answers from, or
	// 0 when no full-text index is active.
	IndexLag(ctx context.Context) (int64, error)
}

// SearchWithRebuildNotice runs search and returns, beside its answer, the
// notice memory_search puts next to the mode when the full-text index has not
// caught up with the event history, so a caller can tell a partial answer from
// a full one (wm-4db1v2). That is the case while the index refills after an
// upgrade or a rebuild, and for a moment after any write. The notice is ""
// when the index had caught up, when the answer did not come from the
// full-text index (graph-labels reads the graph), and when search cannot
// report progress.
//
// Progress is read on both sides of the search, and the notice reports the
// larger lag. Read after the search alone, a subscriber that committed its
// final batch between the two reads would make a partial answer look complete;
// read before it alone, an index cleared for a rebuild during the search
// would. The checkpoint only moves forward between a clear and the next, so a
// lag of 0 on both sides means the search saw every event up to the first
// read (Copilot review 5869602749).
func SearchWithRebuildNotice(
	ctx context.Context, search LexicalSearch, q repositories.LexicalQuery,
) ([]repositories.LexicalHit, string, string, error) {
	progress, reportsProgress := search.(LexicalIndexProgress)
	var lagBefore int64
	var errBefore error
	if reportsProgress {
		lagBefore, errBefore = progress.IndexLag(ctx)
	}
	hits, mode, err := search.Search(ctx, q)
	if err != nil {
		return nil, mode, "", err
	}
	if !reportsProgress || !IsFullTextMode(mode) {
		return hits, mode, "", nil
	}
	lagAfter, errAfter := progress.IndexLag(ctx)
	if errBefore != nil || errAfter != nil {
		// The notice is how the failure surfaces: the caller is told the
		// answer may be partial. The database error itself is not quoted to
		// an MCP client.
		return hits, mode, "the full-text index could not report whether it has caught up with the " +
			"event history, so this answer may be incomplete", nil
	}
	return hits, mode, lexicalRebuildNotice(max(lagBefore, lagAfter)), nil
}

// lexicalRebuildNotice is the notice for an index lag behind the event
// history, or "" when the index has caught up.
func lexicalRebuildNotice(lag int64) string {
	if lag <= 0 {
		return ""
	}
	return fmt.Sprintf("the full-text index is rebuilding: it is about %d events behind the event history, "+
		"so this answer holds only what is indexed so far and may be incomplete. "+
		"Repeat the search later for a complete answer", lag)
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
	ctx context.Context, q repositories.LexicalQuery,
) ([]repositories.LexicalHit, string, error) {
	if q.Limit <= 0 {
		q.Limit = defaultRecallLimit
	}
	if q.Limit > maxRecallLimit {
		q.Limit = maxRecallLimit
	}
	q.TypeSlugs = typeSlugFilter(q.TypeSlugs)
	if s.index != nil && s.index.Active() {
		// The caller's scope goes into the index query itself, never onto
		// its answer: filtering after the LIMIT would let another account's
		// better-ranked matches spend the limit, and the snippets are built
		// in the same query.
		hits, err := s.index.Search(ctx, q, visibilityScopeFromContext(ctx))
		return hits, s.index.Engine(), err
	}
	if s.kg == nil || !s.kg.Active() {
		return nil, "", ErrLexicalUnavailable
	}
	var ids []string
	var err error
	if q.Match == repositories.MatchAny {
		ids, err = s.labelSearchAny(ctx, q)
	} else {
		ids, err = s.labelSearchIDs(ctx, q.Text, q.Limit, q.TypeSlugs)
	}
	if err != nil {
		return nil, "", err
	}
	hits := make([]repositories.LexicalHit, 0, len(ids))
	for _, id := range ids {
		// The graph applied the type filter already where it could; this
		// keeps a graph that cannot (see labelSearch) to the narrowed types.
		if len(q.TypeSlugs) > 0 && !urnHasType(id, q.TypeSlugs) {
			continue
		}
		hits = append(hits, repositories.LexicalHit{ID: id})
	}
	hits, err = s.dropSupersededFacts(ctx, hits)
	if err != nil {
		return nil, "", err
	}
	if len(hits) > q.Limit {
		hits = hits[:q.Limit]
	}
	return hits, LexicalModeGraphLabels, nil
}

// labelSearchIDs is labelSearch's answer as resource URNs, in its order.
func (s *lexicalSearch) labelSearchIDs(
	ctx context.Context, query string, limit int, types []string,
) ([]string, error) {
	terms, err := s.labelSearch(ctx, query, limit, types)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(terms))
	for _, t := range terms {
		ids = append(ids, t.Value)
	}
	return ids, nil
}

// labelSearchAny is the MatchAny search on the degraded path. A resource's
// coverage is the number of content words its labels hold; resources below
// the MinMatch floor are dropped and the rest ordered by coverage. A graph
// that implements AnyWordLabelSearch counts coverage, applies the floor and
// ranks in one query, before its LIMIT, so no word's many matches can hide a
// resource holding several words. Any other graph is searched one word at a
// time, each word asked for the maximum, and a resource's coverage is the
// number of words whose search found it, keeping label search's own order
// within one coverage; past the maximum for a word that undercounts, as
// labelSearch's type filter does for such a graph. A query with no content
// words matches nothing.
func (s *lexicalSearch) labelSearchAny(ctx context.Context, q repositories.LexicalQuery) ([]string, error) {
	words := lexical.ContentWords(q.Text)
	if len(words) == 0 {
		return nil, nil
	}
	floor := lexical.Floor(q.MinMatch, len(words))
	if ranked, ok := s.kg.(AnyWordLabelSearch); ok {
		terms, err := ranked.SearchEntitiesAnyWord(ctx, words, floor, q.TypeSlugs, maxRecallLimit)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(terms))
		for _, t := range terms {
			ids = append(ids, t.Value)
		}
		return ids, nil
	}
	coverage := map[string]int{}
	var order []string
	for _, w := range words {
		ids, err := s.labelSearchIDs(ctx, w, maxRecallLimit, q.TypeSlugs)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			if coverage[id] == 0 {
				order = append(order, id)
			}
			coverage[id]++
		}
	}
	kept := order[:0]
	for _, id := range order {
		if coverage[id] >= floor {
			kept = append(kept, id)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return coverage[kept[i]] > coverage[kept[j]] })
	return kept, nil
}

// labelSearch runs the graph's label search. Label search knows classes, not
// resource types, so a type filter is applied to the resource URN
// (urn:<typeSlug>:<id>). A graph that implements TypeScopedLabelSearch applies
// it inside its query, before the LIMIT, so other types' labels can never
// spend the limit. Any other graph is asked for its maximum and filtered
// afterwards, which still misses a narrowed type past that many matches.
func (s *lexicalSearch) labelSearch(
	ctx context.Context, query string, limit int, types []string,
) ([]repositories.KGTerm, error) {
	if len(types) == 0 {
		return s.kg.SearchEntities(ctx, query, "", limit)
	}
	if typed, ok := s.kg.(TypeScopedLabelSearch); ok {
		return typed.SearchEntitiesOfTypes(ctx, query, types, limit)
	}
	return s.kg.SearchEntities(ctx, query, "", maxRecallLimit)
}

// IndexLag implements LexicalIndexProgress.
func (s *lexicalSearch) IndexLag(ctx context.Context) (int64, error) {
	if s.index == nil || !s.index.Active() {
		return 0, nil
	}
	return s.index.Lag(ctx)
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
