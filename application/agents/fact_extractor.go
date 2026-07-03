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

package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wepala/weos/v3/domain/entities"

	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

const factExtractionInstruction = `You consolidate episodic events from a personal knowledge graph into durable
semantic facts (CoALA-style memory).

Given one observation (a resource that was just created or updated) and the facts already known about that
resource, return the NEW stable facts this observation supports.

Rules:
- Each fact is ONE declarative sentence in third person that will still be useful months from now.
- Only state what the observation clearly supports; never speculate.
- If a new fact contradicts an existing fact, set supersedesId to that existing fact's id.
- Skip anything an existing fact already covers. Return an empty array when nothing new is worth remembering.
- confidence is 0..1: how strongly the observation supports the fact.`

// factCandidateSchema constrains the model to a JSON array of fact candidates.
var factCandidateSchema = &genai.Schema{
	Type: genai.TypeArray,
	Items: &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"statement":    {Type: genai.TypeString},
			"about":        {Type: genai.TypeString},
			"confidence":   {Type: genai.TypeNumber},
			"supersedesId": {Type: genai.TypeString},
		},
		Required: []string{"statement"},
	},
}

// GeminiFactExtractor implements entities.FactExtractor on Google ADK/Gemini.
// It is the first BYOK provider; alternatives implement the same port.
type GeminiFactExtractor struct {
	model model.LLM
}

// NewGeminiFactExtractor wraps a configured ADK model as a FactExtractor.
func NewGeminiFactExtractor(m model.LLM) *GeminiFactExtractor {
	return &GeminiFactExtractor{model: m}
}

// ExtractFacts prompts the model with the observation plus known facts and
// parses the structured reply.
func (g *GeminiFactExtractor) ExtractFacts(
	ctx context.Context, obs entities.EpisodeObservation, related []entities.ExistingFact,
) ([]entities.FactCandidate, error) {
	a, err := llmagent.New(llmagent.Config{
		Name:        "fact-extractor",
		Description: "Distills episodic events into semantic memory facts",
		Model:       g.model,
		Instruction: factExtractionInstruction,
		GenerateContentConfig: &genai.GenerateContentConfig{
			ResponseSchema:   factCandidateSchema,
			ResponseMIMEType: "application/json",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create fact extraction agent: %w", err)
	}
	input, err := json.Marshal(map[string]any{
		"observation":   obs,
		"existingFacts": related,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal fact extraction input: %w", err)
	}
	sessionID := "consolidation"
	if len(obs.EventIDs) > 0 {
		sessionID = obs.EventIDs[0]
	}
	out, err := RunAgent(ctx, a, DefaultAppName, "consolidation", sessionID, string(input), nil)
	if err != nil {
		return nil, fmt.Errorf("run fact extraction agent: %w", err)
	}
	return ParseFactCandidates(out)
}

// ParseFactCandidates parses the model's JSON reply into fact candidates,
// tolerating markdown code fences some models wrap JSON in.
func ParseFactCandidates(out string) ([]entities.FactCandidate, error) {
	s := strings.TrimSpace(out)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var candidates []entities.FactCandidate
	if err := json.Unmarshal([]byte(s), &candidates); err != nil {
		return nil, fmt.Errorf("parse fact candidates: %w (raw: %.200s)", err, s)
	}
	return candidates, nil
}
