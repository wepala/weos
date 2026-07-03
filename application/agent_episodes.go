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
	"errors"
	"fmt"

	appagents "github.com/wepala/weos/v3/application/agents"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
)

// NoteTypeSlug is the memory preset's episodic input type — the only type
// background consolidation (#386) distills facts from by default.
const NoteTypeSlug = "note"

// ResourceCreator is the slice of ResourceService episode recording needs.
type ResourceCreator interface {
	Create(ctx context.Context, cmd CreateResourceCommand) (*entities.Resource, error)
}

// RecordAgentTurn returns an EpisodeRecorder that persists each completed
// in-app agent turn as a note resource. The note flows through the normal
// UnitOfWork → events → projection pipeline, so it is episodic memory:
// consolidation can distill durable facts from what users discuss with
// their app, exactly as it does for hand-written notes.
func RecordAgentTurn(resources ResourceCreator) appagents.EpisodeRecorder {
	return func(ctx context.Context, conversationID, userID, message, reply string) error {
		data, err := json.Marshal(map[string]any{
			"name":    "Agent conversation turn",
			"content": fmt.Sprintf("User (%s) asked: %s\n\nAgent replied: %s", userID, message, reply),
			"about":   "urn:agent-conversation:" + conversationID,
		})
		if err != nil {
			return fmt.Errorf("marshal conversation note: %w", err)
		}
		if _, err := resources.Create(ctx, CreateResourceCommand{TypeSlug: NoteTypeSlug, Data: data}); err != nil {
			// A missing note type means the memory preset isn't installed —
			// episodic memory is off, not broken.
			if errors.Is(err, repositories.ErrNotFound) {
				return appagents.ErrEpisodicMemoryUnavailable
			}
			return fmt.Errorf("record conversation note: %w", err)
		}
		return nil
	}
}
