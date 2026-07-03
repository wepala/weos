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

import (
	"context"
	"time"
)

// EventLogFilter narrows an event-log query. Zero-value fields are not
// applied, so filters combine freely.
type EventLogFilter struct {
	// From/Until bound the occurred-at window (inclusive from, exclusive
	// until). Zero times leave that side unbounded.
	From  time.Time
	Until time.Time
	// Anchors restricts to events involving any of these resource URNs — as
	// the event's aggregate or referenced in its payload (via the
	// event-reference projection). Multiple anchors union (OR).
	Anchors []string
	// EventType restricts to one stored event-type string (e.g. "Resource.Created").
	EventType string
	// TypeSlug restricts to aggregates of one resource type via the
	// urn:<typeSlug>:<ksuid> URN prefix.
	TypeSlug string
	// Limit caps the page size; callers are expected to clamp it first.
	Limit int
	// Cursor continues a previous page (opaque, produced by the repository).
	Cursor string
}

// EventLogEntry is one persisted event, as read from the event store's table.
// Payload is the raw stored payload map — summarization happens above the
// repository so the compact shape stays a single application-layer concern.
type EventLogEntry struct {
	ID          string
	AggregateID string
	EventType   string
	CreatedAt   time.Time
	Payload     map[string]any
}

// EventLogRepository is the read-only, time-ordered query surface over the
// event store's table. It never writes: events are immutable and derived data
// belongs in projections, not the log.
type EventLogRepository interface {
	// Query returns events matching the filter in ascending (created_at, id)
	// order — deterministic across identical calls.
	Query(ctx context.Context, filter EventLogFilter) (*PaginatedResponse[EventLogEntry], error)
	// GetByID returns one event by its raw store ID, or nil when unknown.
	GetByID(ctx context.Context, id string) (*EventLogEntry, error)
	// Recent returns up to max events in descending (created_at, id) order —
	// the similarity ranking's deterministic candidate window.
	Recent(ctx context.Context, max int) ([]EventLogEntry, error)
}
