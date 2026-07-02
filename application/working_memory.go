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

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"go.uber.org/fx"
)

// defaultWorkingSetSize bounds how many recent facts the working set surfaces.
const defaultWorkingSetSize = 20

// RecalledFact is the recall read-model for one fact, whichever store it came
// from: the knowledge graph or the working set.
type RecalledFact struct {
	ID              string  `json:"id"`
	Statement       string  `json:"statement"`
	About           string  `json:"about,omitempty"`
	Confidence      float64 `json:"confidence,omitempty"`
	AttributedTo    string  `json:"attributedTo,omitempty"`
	GeneratedAtTime string  `json:"generatedAtTime,omitempty"`
	WasRevisionOf   string  `json:"wasRevisionOf,omitempty"`
	// Source is "working" for facts read from the synchronous SQL projection
	// (may not be in the graph yet) or "graph" for knowledge-graph results.
	Source string `json:"source,omitempty"`
}

// WorkingMemory surfaces just-written facts during the projection-lag window.
// The SQL fact projection is written synchronously on commit (read-your-writes)
// while the Oxigraph projection catches up on its own checkpoint, so a fact
// recorded this turn is visible here before it is queryable in the graph.
// Reads never block on any checkpoint.
type WorkingMemory interface {
	// RecentFacts returns the newest non-superseded facts from the SQL
	// projection, freshest first. limit <= 0 uses the default working-set size.
	RecentFacts(ctx context.Context, limit int) ([]RecalledFact, error)
}

// factFlatLister is the narrow slice of ResourceService the working set reads
// through.
type factFlatLister interface {
	ListFlat(ctx context.Context, typeSlug, cursor string, limit int, sort repositories.SortOptions) (
		repositories.PaginatedResponse[map[string]any], error)
}

type workingMemory struct {
	facts  factFlatLister
	logger entities.Logger
}

// WorkingMemoryParams bundles the working memory's dependencies.
type WorkingMemoryParams struct {
	fx.In
	Service ResourceService
	Logger  entities.Logger
}

// ProvideWorkingMemory supplies the WorkingMemory read model.
func ProvideWorkingMemory(p WorkingMemoryParams) WorkingMemory {
	return &workingMemory{facts: p.Service, logger: p.Logger}
}

func (w *workingMemory) RecentFacts(ctx context.Context, limit int) ([]RecalledFact, error) {
	if limit <= 0 {
		limit = defaultWorkingSetSize
	}
	page, err := w.facts.ListFlat(ctx, "fact", "", limit,
		repositories.SortOptions{SortBy: "updatedAt", SortOrder: "desc"})
	if err != nil {
		// No projection table means the memory preset isn't installed — an
		// empty working set, not a failure.
		if errors.Is(err, repositories.ErrNoProjectionTable) {
			return nil, nil
		}
		return nil, err
	}
	facts := make([]RecalledFact, 0, len(page.Data))
	for _, row := range page.Data {
		if invalidated, _ := row["invalidatedAtTime"].(string); invalidated != "" {
			continue // superseded facts are excluded from recall
		}
		facts = append(facts, recalledFactFromRow(row))
	}
	return facts, nil
}

// recalledFactFromRow maps a flat projection row (camelCase keys) to the
// recall read-model.
func recalledFactFromRow(row map[string]any) RecalledFact {
	f := RecalledFact{Source: "working"}
	f.ID, _ = row["id"].(string)                           //nolint:errcheck // absent → ""
	f.Statement, _ = row["statement"].(string)             //nolint:errcheck // absent → ""
	f.About, _ = row["about"].(string)                     //nolint:errcheck // absent → ""
	f.AttributedTo, _ = row["attributedTo"].(string)       //nolint:errcheck // absent → ""
	f.GeneratedAtTime, _ = row["generatedAtTime"].(string) //nolint:errcheck // absent → ""
	f.WasRevisionOf, _ = row["wasRevisionOf"].(string)     //nolint:errcheck // absent → ""
	switch c := row["confidence"].(type) {
	case float64:
		f.Confidence = c
	case int64:
		f.Confidence = float64(c)
	}
	return f
}

// MergeRecalledFacts combines working-set facts with graph recall results,
// deduplicating by URN. Working-set entries win on conflict — they reflect the
// synchronous projection, which is never behind the graph.
func MergeRecalledFacts(working, graph []RecalledFact) []RecalledFact {
	merged := make([]RecalledFact, 0, len(working)+len(graph))
	seen := make(map[string]bool, len(working))
	for _, f := range working {
		if f.ID == "" || seen[f.ID] {
			continue
		}
		seen[f.ID] = true
		merged = append(merged, f)
	}
	for _, f := range graph {
		if f.ID == "" || seen[f.ID] {
			continue
		}
		seen[f.ID] = true
		merged = append(merged, f)
	}
	return merged
}
