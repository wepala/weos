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

package entities

import (
	"context"
	"encoding/json"
)

// EpisodeObservation is one episodic occurrence handed to the fact extractor:
// a resource that was created or updated, together with the event IDs the
// observation derives from (facts record these as prov:wasDerivedFrom).
type EpisodeObservation struct {
	EventIDs   []string        `json:"eventIds"`
	ResourceID string          `json:"resourceId"`
	TypeSlug   string          `json:"typeSlug"`
	Data       json.RawMessage `json:"data"`
}

// ExistingFact summarizes an already-consolidated fact so the extractor can
// avoid duplicates and detect contradictions.
type ExistingFact struct {
	ID        string `json:"id"`
	Statement string `json:"statement"`
	About     string `json:"about,omitempty"`
}

// FactCandidate is one fact the extractor distilled from an observation.
// SupersedesID, when set, names the ExistingFact the candidate contradicts.
type FactCandidate struct {
	Statement    string  `json:"statement"`
	About        string  `json:"about,omitempty"`
	Confidence   float64 `json:"confidence,omitempty"`
	SupersedesID string  `json:"supersedesId,omitempty"`
}

// FactExtractor is the provider-agnostic BYOK LLM port for memory
// consolidation. Implementations (ADK/Gemini today) must not leak provider
// types through this interface; the consolidation policy depends only on it.
type FactExtractor interface {
	ExtractFacts(ctx context.Context, obs EpisodeObservation, related []ExistingFact) ([]FactCandidate, error)
}
