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
	"strconv"
	"strings"

	"github.com/wepala/weos/v3/domain/repositories"
)

// Fact predicate IRIs, matching the memory preset's context (bare full-IRI
// mappings — see application/presets/memory).
const (
	factClassIRI = "https://weos.io/vocab/memory#Fact"
	// legacyFactClassIRI is the class facts carried before the house
	// vocabulary moved to weos.io (issue #520). An existing install keeps it
	// until the held prefix is adopted and its records re-stamped, so recall
	// accepts both rather than going blind across the upgrade.
	legacyFactClassIRI    = "https://weos.org/vocab/memory#Fact"
	legacyFactConfidence  = "https://weos.org/vocab/memory#confidence"
	factStatementIRI      = "https://schema.org/text"
	factAboutIRI          = "https://schema.org/about"
	factConfidenceIRI     = "https://weos.io/vocab/memory#confidence"
	factGeneratedAtIRI    = "http://www.w3.org/ns/prov#generatedAtTime"
	factWasRevisionOfIRI  = "http://www.w3.org/ns/prov#wasRevisionOf"
	factWasDerivedFromIRI = "http://www.w3.org/ns/prov#wasDerivedFrom"
	factInvalidatedAtIRI  = "http://www.w3.org/ns/prov#invalidatedAtTime"
	defaultRecallLimit    = 20
	maxRecallLimit        = 100
)

// RecallQuery is a structured memory-recall request. Free-form SPARQL stays on
// kg_sparql_query; recall builds the query itself with supersession filtering
// always applied.
type RecallQuery struct {
	// About restricts results to facts about this entity URN.
	About string
	// Keyword is a case-insensitive substring match on the fact statement.
	Keyword string
	// Limit caps results (default 20, max 100).
	Limit int
	// IncludeProvenance adds the prov:wasDerivedFrom source event IDs.
	IncludeProvenance bool
}

// MemoryRecall answers structured recall queries against semantic memory,
// composing the knowledge graph with the working set so just-written facts
// appear the same turn. Superseded facts never appear.
type MemoryRecall interface {
	Recall(ctx context.Context, q RecallQuery) ([]RecalledFact, error)
}

type memoryRecall struct {
	kg      KnowledgeGraphService
	working WorkingMemory
}

// NewMemoryRecall builds the recall service. kg may be nil (Oxigraph not
// configured) — recall then serves from the working set alone rather than
// failing, and never blocks on any projection checkpoint.
func NewMemoryRecall(kg KnowledgeGraphService, working WorkingMemory) MemoryRecall {
	return &memoryRecall{kg: kg, working: working}
}

func (r *memoryRecall) Recall(ctx context.Context, q RecallQuery) ([]RecalledFact, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = defaultRecallLimit
	}
	if limit > maxRecallLimit {
		limit = maxRecallLimit
	}
	// The about filter runs inside the projection query; keyword stays a
	// best-effort in-memory filter over the freshness window (no substring
	// operator on the flat store) — the graph side matches it completely.
	working, err := r.working.RecentFacts(ctx, q.About, limit)
	if err != nil {
		return nil, err
	}
	working = filterRecalled(working, q)

	graph, err := r.graphFacts(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	merged := MergeRecalledFacts(working, graph)
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

// graphFacts queries the knowledge graph, degrading to no results (not an
// error) when the graph is not configured.
func (r *memoryRecall) graphFacts(ctx context.Context, q RecallQuery, limit int) ([]RecalledFact, error) {
	if r.kg == nil || !r.kg.Active() {
		return nil, nil
	}
	res, err := r.kg.Query(ctx, buildRecallSPARQL(q, limit))
	if err != nil {
		if errors.Is(err, ErrKGUnavailable) {
			return nil, nil
		}
		return nil, fmt.Errorf("memory recall: graph query: %w", err)
	}
	facts := make([]RecalledFact, 0, len(res.Bindings))
	for _, row := range res.Bindings {
		facts = append(facts, factFromBinding(row))
	}
	return facts, nil
}

// buildRecallSPARQL generates the structured recall query. The supersession
// filter (no prov:invalidatedAtTime) is not optional — that is the point of
// the recall surface over raw kg_sparql_query.
func buildRecallSPARQL(q RecallQuery, limit int) string {
	var b strings.Builder
	// DISTINCT: a fact carrying both the current and the legacy class
	// mid-migration matches VALUES twice and must not halve the limit.
	b.WriteString("SELECT DISTINCT ?fact ?statement ?about ?confidence ?generatedAt ?revisionOf")
	if q.IncludeProvenance {
		b.WriteString(` (GROUP_CONCAT(DISTINCT ?src; separator=" ") AS ?sources)`)
	}
	b.WriteString(" WHERE {\n")
	fmt.Fprintf(&b, "  VALUES ?factClass { <%s> <%s> }\n", factClassIRI, legacyFactClassIRI)
	fmt.Fprintf(&b, "  ?fact a ?factClass ;\n        <%s> ?statement .\n", factStatementIRI)
	fmt.Fprintf(&b, "  OPTIONAL { ?fact <%s> ?about }\n", factAboutIRI)
	// One confidence per fact even when a record carries both predicates
	// mid-migration: two OPTIONALs coalesced, not an alternation, which
	// would yield a row per match.
	fmt.Fprintf(&b, "  OPTIONAL { ?fact <%s> ?confidenceNow }\n", factConfidenceIRI)
	fmt.Fprintf(&b, "  OPTIONAL { ?fact <%s> ?confidenceLegacy }\n", legacyFactConfidence)
	b.WriteString("  BIND(COALESCE(?confidenceNow, ?confidenceLegacy) AS ?confidence)\n")
	fmt.Fprintf(&b, "  OPTIONAL { ?fact <%s> ?generatedAt }\n", factGeneratedAtIRI)
	fmt.Fprintf(&b, "  OPTIONAL { ?fact <%s> ?revisionOf }\n", factWasRevisionOfIRI)
	if q.IncludeProvenance {
		fmt.Fprintf(&b, "  OPTIONAL { ?fact <%s> ?src }\n", factWasDerivedFromIRI)
	}
	fmt.Fprintf(&b, "  FILTER NOT EXISTS { ?fact <%s> ?invalidated }\n", factInvalidatedAtIRI)
	if q.About != "" {
		fmt.Fprintf(&b, "  FILTER(STR(?about) = \"%s\")\n", sparqlEscape(q.About))
	}
	if q.Keyword != "" {
		fmt.Fprintf(&b, "  FILTER(CONTAINS(LCASE(STR(?statement)), \"%s\"))\n",
			sparqlEscape(strings.ToLower(q.Keyword)))
	}
	b.WriteString("}\n")
	if q.IncludeProvenance {
		b.WriteString("GROUP BY ?fact ?statement ?about ?confidence ?generatedAt ?revisionOf\n")
	}
	fmt.Fprintf(&b, "ORDER BY DESC(?generatedAt)\nLIMIT %d", limit)
	return b.String()
}

// sparqlEscape escapes a value for embedding in a SPARQL string literal.
func sparqlEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	return s
}

func factFromBinding(row map[string]repositories.KGTerm) RecalledFact {
	f := RecalledFact{Source: "graph"}
	f.ID = row["fact"].Value
	f.Statement = row["statement"].Value
	f.About = row["about"].Value
	f.GeneratedAtTime = row["generatedAt"].Value
	f.WasRevisionOf = row["revisionOf"].Value
	if v := row["confidence"].Value; v != "" {
		if c, err := strconv.ParseFloat(v, 64); err == nil {
			f.Confidence = c
		}
	}
	if v := row["sources"].Value; v != "" {
		f.DerivedFrom = strings.Fields(v)
	}
	return f
}

// filterRecalled applies the recall query's filters to working-set facts so
// both stores answer the same question.
func filterRecalled(facts []RecalledFact, q RecallQuery) []RecalledFact {
	if q.About == "" && q.Keyword == "" {
		return facts
	}
	kw := strings.ToLower(q.Keyword)
	out := make([]RecalledFact, 0, len(facts))
	for _, f := range facts {
		if q.About != "" && f.About != q.About {
			continue
		}
		if kw != "" && !strings.Contains(strings.ToLower(f.Statement), kw) {
			continue
		}
		out = append(out, f)
	}
	return out
}
