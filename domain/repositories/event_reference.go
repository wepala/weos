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

package repositories

import "context"

// EventReferenceRepository is the event-reference projection's persistence:
// which resources each event references (the aggregate itself plus resource
// URNs in the payload). The projection is a rebuildable read model derived
// from the event log — rows are inserted idempotently and only Clear (the
// rebuild path) ever removes them; later events never erase history.
type EventReferenceRepository interface {
	// SaveForEvent records the resource URNs one event references.
	// Idempotent: replaying the same event is a no-op.
	SaveForEvent(ctx context.Context, eventID string, resourceURNs []string) error
	// ForEvents returns the referenced resource URNs per event ID for a batch
	// of events, each list in deterministic order.
	ForEvents(ctx context.Context, eventIDs []string) (map[string][]string, error)
	// Clear truncates the projection so a checkpoint reset rebuilds it.
	Clear(ctx context.Context) error
}
