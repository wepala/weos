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
	"strings"

	"github.com/wepala/weos/v3/domain/repositories"
)

// FetchedEvent is one event with its full stored payload — the explicit
// drill-in complement to the compact recall shape (epic #409, story #414).
// Payload is returned exactly as stored; recall stays token-frugal by default
// and this fetch is the only episodic surface that returns it.
type FetchedEvent struct {
	RecalledEvent
	Payload map[string]any `json:"payload"`
}

// ErrUnknownEvent distinguishes a well-formed event URN that matches no
// stored event — a clear error, never an empty success.
var ErrUnknownEvent = fmt.Errorf("the event is unknown")

// EventByURN returns one event's full payload by its urn:event: URN.
func (r *episodicRecall) EventByURN(ctx context.Context, urn string) (*FetchedEvent, error) {
	id, err := parseEventURN(urn)
	if err != nil {
		return nil, err
	}
	entry, err := r.events.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, fmt.Errorf("%w: no event %q in the log", ErrUnknownEvent, "urn:event:"+id)
	}
	referenced, err := r.referencesFor(ctx, []repositories.EventLogEntry{*entry})
	if err != nil {
		return nil, err
	}
	payload := entry.Payload
	if payload == nil {
		// A NULL-stored payload must still satisfy the tool's object-typed
		// output schema; an empty object is the honest "no payload stored".
		payload = map[string]any{}
	}
	fetched := &FetchedEvent{RecalledEvent: compactEvent(*entry), Payload: payload}
	fetched.ReferencedResources = referenced[entry.ID]
	return fetched, nil
}

// parseEventURN validates the urn:event:<id> shape and returns the raw ID.
func parseEventURN(urn string) (string, error) {
	urn = strings.TrimSpace(urn)
	id, ok := strings.CutPrefix(urn, "urn:event:")
	if !ok || id == "" || strings.Contains(id, ":") {
		return "", fmt.Errorf(
			"validation error: the identifier must be an event URN (urn:event:<id>), got %q", urn)
	}
	return id, nil
}
