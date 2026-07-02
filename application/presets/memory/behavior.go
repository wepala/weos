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

package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
)

// factBehavior records the Fact.Recorded / Fact.Superseded signal events in
// the same UnitOfWork commit as the fact's own creation, whoever the writer is
// (consolidation policy, MCP, HTTP). The events carry provenance for
// subscribers and audit; they never change entity state (see
// Resource.ApplyEvent's no-op case).
type factBehavior struct {
	entities.DefaultBehavior
}

// FactBehavior constructs the fact behavior. It needs no services — it only
// inspects the entity being committed.
func FactBehavior(_ application.BehaviorServices) entities.ResourceBehavior {
	return &factBehavior{}
}

// BeforeCreateCommit records Fact.Recorded, plus Fact.Superseded when the new
// fact revises a predecessor — atomically with the fact's ResourceCreated.
func (b *factBehavior) BeforeCreateCommit(_ context.Context, resource *entities.Resource) error {
	var node struct {
		Statement      string   `json:"statement"`
		WasDerivedFrom []string `json:"wasDerivedFrom"`
	}
	if err := json.Unmarshal(application.ExtractEntityNode(resource.Data()), &node); err != nil {
		return fmt.Errorf("fact behavior: parse entity node: %w", err)
	}
	now := time.Now()
	recorded := entities.FactRecorded{
		FactID:      resource.GetID(),
		Statement:   node.Statement,
		DerivedFrom: node.WasDerivedFrom,
		Timestamp:   now,
	}
	if err := resource.RecordEvent(recorded, recorded.EventType()); err != nil {
		return fmt.Errorf("fact behavior: record Fact.Recorded: %w", err)
	}
	prior := application.EdgeValue(resource.Data(), json.RawMessage(factContext), "wasRevisionOf")
	if prior == "" {
		return nil
	}
	superseded := entities.FactSuperseded{
		FactID:       prior,
		SupersededBy: resource.GetID(),
		Timestamp:    now,
	}
	if err := resource.RecordEvent(superseded, superseded.EventType()); err != nil {
		return fmt.Errorf("fact behavior: record Fact.Superseded: %w", err)
	}
	return nil
}

// playbookBehavior records Playbook.Confirmed / Playbook.Rejected signal
// events when an update commit carries the outcome marker set by
// PlaybookService.RecordOutcome. Plain edits carry no marker and record no
// signal.
type playbookBehavior struct {
	entities.DefaultBehavior
}

// PlaybookBehavior constructs the playbook behavior.
func PlaybookBehavior(_ application.BehaviorServices) entities.ResourceBehavior {
	return &playbookBehavior{}
}

// BeforeUpdateCommit records the outcome signal atomically with the
// counter-bearing update.
func (b *playbookBehavior) BeforeUpdateCommit(ctx context.Context, resource *entities.Resource) error {
	outcome, note, ok := entities.PlaybookOutcomeFromCtx(ctx)
	if !ok {
		return nil
	}
	now := time.Now()
	switch outcome {
	case entities.PlaybookOutcomeConfirmed:
		ev := entities.PlaybookConfirmed{PlaybookID: resource.GetID(), Note: note, Timestamp: now}
		if err := resource.RecordEvent(ev, ev.EventType()); err != nil {
			return fmt.Errorf("playbook behavior: record Playbook.Confirmed: %w", err)
		}
	case entities.PlaybookOutcomeRejected:
		ev := entities.PlaybookRejected{PlaybookID: resource.GetID(), Note: note, Timestamp: now}
		if err := resource.RecordEvent(ev, ev.EventType()); err != nil {
			return fmt.Errorf("playbook behavior: record Playbook.Rejected: %w", err)
		}
	}
	return nil
}
