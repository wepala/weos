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

package gorm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wepala/weos/v3/domain/repositories"

	pericarpdomain "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/infrastructure"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

// eventLogIndexes are the read-model indexes the episodic query surface needs.
// CREATE INDEX IF NOT EXISTS is supported by both SQLite and Postgres, and the
// events table itself is owned (and migrated) by pericarp, so plain idempotent
// DDL mirrors how pericarp manages that table's own indexes.
var eventLogIndexes = []string{
	"CREATE INDEX IF NOT EXISTS idx_events_created_at ON events (created_at)",
	"CREATE INDEX IF NOT EXISTS idx_events_event_type ON events (event_type)",
}

// EventLogRepository queries the pericarp events table read-only.
type EventLogRepository struct {
	db *gorm.DB
}

// EventLogRepositoryResult holds the repository for Fx injection.
type EventLogRepositoryResult struct {
	fx.Out
	Repository repositories.EventLogRepository
}

// ProvideEventLogRepository builds the read-only event-log repository and
// ensures its read-model indexes exist. It must construct after the event
// store so the events table is already migrated.
func ProvideEventLogRepository(params struct {
	fx.In
	DB *gorm.DB
	// EventStore is unused directly; depending on it orders construction
	// after pericarp has migrated the events table the indexes target.
	EventStore pericarpdomain.EventStore
}) (EventLogRepositoryResult, error) {
	for _, ddl := range eventLogIndexes {
		if err := params.DB.Exec(ddl).Error; err != nil {
			return EventLogRepositoryResult{}, fmt.Errorf("failed to create event log index: %w", err)
		}
	}
	return EventLogRepositoryResult{Repository: &EventLogRepository{db: params.DB}}, nil
}

// Query returns events matching the filter in ascending (created_at, id)
// order with cursor pagination.
func (r *EventLogRepository) Query(
	ctx context.Context, filter repositories.EventLogFilter,
) (*repositories.PaginatedResponse[repositories.EventLogEntry], error) {
	limit := filter.Limit
	if limit <= 0 {
		return nil, fmt.Errorf("event log query limit must be positive, got %d", limit)
	}

	q := r.db.WithContext(ctx).Model(&infrastructure.GormEventModel{})
	q = applyEventLogFilter(q, filter)
	if filter.Cursor != "" {
		cd, err := decodeCursor(filter.Cursor)
		if err != nil {
			return nil, fmt.Errorf("validation error: invalid cursor: %w", err)
		}
		after, err := time.Parse(time.RFC3339Nano, cd.Value)
		if err != nil {
			return nil, fmt.Errorf("validation error: invalid cursor: %w", err)
		}
		q = q.Where("created_at > ? OR (created_at = ? AND id > ?)", after, after, cd.ID)
	}

	var rows []infrastructure.GormEventModel
	if err := q.Order("created_at ASC, id ASC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("event log query failed: %w", err)
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	resp := &repositories.PaginatedResponse[repositories.EventLogEntry]{
		Data:    make([]repositories.EventLogEntry, 0, len(rows)),
		Limit:   limit,
		HasMore: hasMore,
	}
	for _, m := range rows {
		resp.Data = append(resp.Data, repositories.EventLogEntry{
			ID:          m.ID,
			AggregateID: m.AggregateID,
			EventType:   m.EventType,
			CreatedAt:   m.CreatedAt,
			Payload:     m.Payload,
		})
	}
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		resp.Cursor = encodeCursor(last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID)
	}
	return resp, nil
}

// applyEventLogFilter adds the combinable WHERE clauses. The resource-type
// filter leans only on the urn:<typeSlug>:<ksuid> URN shape — no payload
// indexing, which diverges between SQLite and Postgres.
func applyEventLogFilter(q *gorm.DB, filter repositories.EventLogFilter) *gorm.DB {
	if !filter.From.IsZero() {
		q = q.Where("created_at >= ?", filter.From)
	}
	if !filter.Until.IsZero() {
		q = q.Where("created_at < ?", filter.Until)
	}
	if len(filter.Anchors) > 0 {
		// An anchor matches as the aggregate directly (synchronous — no
		// projection lag for an aggregate's own events) or through the
		// event-reference projection for payload references.
		q = q.Where(
			"aggregate_id IN ? OR id IN (SELECT event_id FROM event_references WHERE resource_urn IN ?)",
			filter.Anchors, filter.Anchors)
	}
	if filter.EventType != "" {
		q = q.Where("event_type = ?", filter.EventType)
	}
	if filter.TypeSlug != "" {
		q = q.Where(`aggregate_id LIKE ? ESCAPE '\'`, "urn:"+escapeLike(filter.TypeSlug)+":%")
	}
	return q
}

// escapeLike escapes LIKE wildcards in a user-supplied slug so slugs like
// blog_post match literally. The explicit ESCAPE '\' clause in the query
// makes the escaping portable across SQLite and Postgres.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	return strings.ReplaceAll(s, "_", `\_`)
}
