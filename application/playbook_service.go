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
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wepala/weos/v3/domain/entities"
)

// PlaybookService records agent verdicts on playbooks (procedural memory).
// Counters are never written directly: RecordOutcome increments them through
// an ordinary event-sourced resource update, and the playbook behavior records
// the Playbook.Confirmed / Playbook.Rejected signal in the same commit — so
// replay reconstructs both counter state and audit trail.
type PlaybookService interface {
	RecordOutcome(ctx context.Context, id string, outcome entities.PlaybookOutcome, note string) (
		*entities.Resource, error)
}

// playbookResources is the narrow slice of ResourceService the playbook
// service uses.
type playbookResources interface {
	GetByID(ctx context.Context, id string) (*entities.Resource, error)
	Update(ctx context.Context, cmd UpdateResourceCommand) (*entities.Resource, error)
}

type playbookService struct {
	resources playbookResources
}

// NewPlaybookService wraps the resource service with playbook outcome
// recording.
func NewPlaybookService(resources ResourceService) PlaybookService {
	return &playbookService{resources: resources}
}

func (s *playbookService) RecordOutcome(
	ctx context.Context, id string, outcome entities.PlaybookOutcome, note string,
) (*entities.Resource, error) {
	if outcome != entities.PlaybookOutcomeConfirmed && outcome != entities.PlaybookOutcomeRejected {
		return nil, fmt.Errorf("playbook outcome must be %q or %q, got %q",
			entities.PlaybookOutcomeConfirmed, entities.PlaybookOutcomeRejected, outcome)
	}
	res, err := s.resources.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if res.TypeSlug() != "playbook" {
		return nil, fmt.Errorf("%s is a %s, not a playbook", id, res.TypeSlug())
	}
	var node map[string]any
	if err := json.Unmarshal(ExtractEntityNode(res.Data()), &node); err != nil {
		return nil, fmt.Errorf("playbook %s: parse entity node: %w", id, err)
	}
	flat := make(map[string]any, len(node))
	for k, v := range node {
		if strings.HasPrefix(k, "@") {
			continue
		}
		flat[k] = v
	}
	counter := "successCount"
	if outcome == entities.PlaybookOutcomeRejected {
		counter = "failureCount"
	}
	flat[counter] = counterValue(flat[counter]) + 1
	data, err := json.Marshal(flat)
	if err != nil {
		return nil, fmt.Errorf("playbook %s: marshal update: %w", id, err)
	}
	// The outcome marker makes the playbook behavior record the matching
	// signal event inside this update's UnitOfWork commit.
	ctx = entities.WithPlaybookOutcome(ctx, outcome, note)
	return s.resources.Update(ctx, UpdateResourceCommand{ID: id, Data: data})
}

// counterValue reads a counter that may arrive as float64 (JSON), int64
// (projection), or int; anything else counts as zero.
func counterValue(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}
