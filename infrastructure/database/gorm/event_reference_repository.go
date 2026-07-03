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

	"github.com/wepala/weos/v3/domain/repositories"
	weosmodels "github.com/wepala/weos/v3/infrastructure/models"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/subscriptions"
	"go.uber.org/fx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EventReferenceRepository persists the event-reference projection.
type EventReferenceRepository struct {
	db *gorm.DB
}

// EventReferenceRepositoryResult holds the repository for Fx injection.
type EventReferenceRepositoryResult struct {
	fx.Out
	Repository repositories.EventReferenceRepository
}

// ProvideEventReferenceRepository builds the event-reference projection
// repository. The table itself is AutoMigrated with the other GORM models.
func ProvideEventReferenceRepository(params struct {
	fx.In
	DB *gorm.DB
}) (EventReferenceRepositoryResult, error) {
	return EventReferenceRepositoryResult{Repository: &EventReferenceRepository{db: params.DB}}, nil
}

// SaveForEvent inserts one row per referenced URN, ignoring rows that already
// exist — ON CONFLICT DO NOTHING works on both SQLite and Postgres, which
// makes event replay idempotent with no read-before-write.
func (r *EventReferenceRepository) SaveForEvent(
	ctx context.Context, eventID string, resourceURNs []string,
) error {
	if eventID == "" || len(resourceURNs) == 0 {
		return nil
	}
	rows := make([]weosmodels.EventReference, 0, len(resourceURNs))
	for _, urn := range resourceURNs {
		rows = append(rows, weosmodels.EventReference{EventID: eventID, ResourceURN: urn})
	}
	err := r.writer(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
	if err != nil {
		return fmt.Errorf("failed to save event references for %s: %w", eventID, err)
	}
	return nil
}

// writer joins the subscriber batch transaction when one is on the context —
// projection rows then commit atomically with the checkpoint, and on SQLite a
// separate connection would deadlock against the batch's write lock. Outside
// a batch (tests, tooling) it falls back to the pooled handle.
func (r *EventReferenceRepository) writer(ctx context.Context) *gorm.DB {
	if tx := subscriptions.TxFromContext(ctx); tx != nil {
		return tx
	}
	return r.db.WithContext(ctx)
}

// ForEvents returns referenced URNs grouped by event ID, each group in
// ascending URN order so identical queries recall identical shapes.
func (r *EventReferenceRepository) ForEvents(
	ctx context.Context, eventIDs []string,
) (map[string][]string, error) {
	result := make(map[string][]string, len(eventIDs))
	if len(eventIDs) == 0 {
		return result, nil
	}
	var rows []weosmodels.EventReference
	err := r.db.WithContext(ctx).
		Where("event_id IN ?", eventIDs).
		Order("event_id ASC, resource_urn ASC").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to load event references: %w", err)
	}
	for _, row := range rows {
		result[row.EventID] = append(result[row.EventID], row.ResourceURN)
	}
	return result, nil
}

// Clear truncates the projection for rebuild (checkpoint reset --truncate).
func (r *EventReferenceRepository) Clear(ctx context.Context) error {
	if err := r.db.WithContext(ctx).Exec("DELETE FROM event_references").Error; err != nil {
		return fmt.Errorf("failed to clear event references: %w", err)
	}
	return nil
}
