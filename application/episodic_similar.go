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
	"sort"
	"strings"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/pkg/identity"
)

// Similarity scoring weights (epic #409, story #413). Fixed and documented —
// no learned ranking, no embeddings. Shared referenced resources DOMINATE:
// one shared resource (weightSharedReference) outweighs a same-event-type,
// same-resource-type candidate at maximal temporal proximity combined
// (weightSameEventType + weightSameResourceType + weightProximityMax = 19).
// Overlap scales with the number of shared resources. Temporal proximity
// decays per day of distance from the seed and only refines the order of
// structurally related events — a candidate with no structural affinity at
// all (no shared reference, different event type, different resource type)
// is not ranked, so config noise like type registrations never surfaces as
// "similar". Ties break on ascending event ID.
const (
	weightSharedReference  = 100.0
	weightSameEventType    = 10.0
	weightSameResourceType = 5.0
	weightProximityMax     = 4.0
	// similarCandidateWindow bounds the ranking to the most recent events —
	// sized for single-user local instances and stated in the tool description.
	similarCandidateWindow = 1000
	hoursPerDay            = 24
)

// SimilarQuery asks for events structurally similar to a seed event.
type SimilarQuery struct {
	// Seed is the seed event URN (urn:event:<id>).
	Seed string
	// Limit caps results (default 20, max 100 — over-asks are capped).
	Limit int
}

// SimilarEvent is one ranked result: the compact event shape plus its
// deterministic similarity score.
type SimilarEvent struct {
	RecalledEvent
	Similarity float64 `json:"similarity"`
}

// SimilarResult is the ranked, limit-bounded result set.
type SimilarResult struct {
	Events []SimilarEvent
}

// ErrUnknownSeedEvent distinguishes a well-formed seed that matches no stored
// event: similarity is undefined without a seed, so this is an error, not an
// empty success.
var ErrUnknownSeedEvent = fmt.Errorf("the seed event is unknown")

func (r *episodicRecall) Similar(ctx context.Context, q SimilarQuery) (*SimilarResult, error) {
	seedID, err := parseSeedURN(q.Seed)
	if err != nil {
		return nil, err
	}
	seed, err := r.events.GetByID(ctx, seedID)
	if err != nil {
		return nil, err
	}
	if seed == nil {
		return nil, fmt.Errorf("%w: no event %q in the log", ErrUnknownSeedEvent, q.Seed)
	}

	limit := q.Limit
	if limit <= 0 {
		limit = defaultEpisodicLimit
	}
	if limit > maxEpisodicLimit {
		limit = maxEpisodicLimit
	}

	candidates, err := r.events.Recent(ctx, similarCandidateWindow)
	if err != nil {
		return nil, err
	}
	referenced, err := r.referencesFor(ctx, append(candidates, *seed))
	if err != nil {
		return nil, err
	}

	ranked := rankBySimilarity(*seed, candidates, referenced)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return &SimilarResult{Events: ranked}, nil
}

// parseSeedURN validates the urn:event:<id> shape and returns the raw ID.
func parseSeedURN(seed string) (string, error) {
	seed = strings.TrimSpace(seed)
	id, ok := strings.CutPrefix(seed, "urn:event:")
	if !ok || id == "" || strings.Contains(id, ":") {
		return "", fmt.Errorf(
			"validation error: the seed must be an event URN (urn:event:<id>), got %q", seed)
	}
	return id, nil
}

// rankBySimilarity scores every candidate against the seed and orders by
// descending score with an ascending-ID tie-break — same seed, same log,
// same ranking, always.
func rankBySimilarity(
	seed repositories.EventLogEntry,
	candidates []repositories.EventLogEntry,
	referenced map[string][]string,
) []SimilarEvent {
	seedRefs := make(map[string]bool, len(referenced[seed.ID]))
	for _, urn := range referenced[seed.ID] {
		seedRefs[urn] = true
	}
	seedSlug := resourceTypeOf(seed)

	ranked := make([]SimilarEvent, 0, len(candidates))
	for _, c := range candidates {
		if c.ID == seed.ID {
			continue
		}
		structural := structuralScore(seed, seedRefs, seedSlug, c, referenced[c.ID])
		if structural == 0 {
			continue
		}
		recalled := compactEvent(c)
		recalled.ReferencedResources = referenced[c.ID]
		ranked = append(ranked, SimilarEvent{
			RecalledEvent: recalled,
			Similarity:    structural + proximityScore(seed, c),
		})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Similarity != ranked[j].Similarity {
			return ranked[i].Similarity > ranked[j].Similarity
		}
		return ranked[i].ID < ranked[j].ID
	})
	return ranked
}

// structuralScore applies the structural weights to one candidate; zero means
// no affinity and the candidate is not ranked at all.
func structuralScore(
	seed repositories.EventLogEntry,
	seedRefs map[string]bool,
	seedSlug string,
	candidate repositories.EventLogEntry,
	candidateRefs []string,
) float64 {
	score := 0.0
	for _, urn := range candidateRefs {
		if seedRefs[urn] {
			score += weightSharedReference
		}
	}
	if candidate.EventType == seed.EventType {
		score += weightSameEventType
	}
	if seedSlug != "" && resourceTypeOf(candidate) == seedSlug {
		score += weightSameResourceType
	}
	return score
}

// proximityScore is the temporal refinement, decaying per day of distance.
func proximityScore(seed, candidate repositories.EventLogEntry) float64 {
	days := seed.CreatedAt.Sub(candidate.CreatedAt).Hours() / hoursPerDay
	if days < 0 {
		days = -days
	}
	return weightProximityMax / (1 + days)
}

// resourceTypeOf mirrors compactEvent's slug derivation for scoring.
func resourceTypeOf(e repositories.EventLogEntry) string {
	if slug := payloadString(e.Payload, "TypeSlug"); slug != "" {
		return slug
	}
	return identity.ExtractResourceTypeSlug(e.AggregateID)
}
