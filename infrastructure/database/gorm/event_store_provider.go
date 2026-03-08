package gorm

import (
	"fmt"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/infrastructure"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

// EventStoreResult holds the event store result for Fx injection.
type EventStoreResult struct {
	fx.Out
	EventStore domain.EventStore
}

// ProvideEventStore creates a GORM-backed event store.
// This auto-migrates the events table.
func ProvideEventStore(params struct {
	fx.In
	DB *gorm.DB
}) (EventStoreResult, error) {
	store, err := infrastructure.NewGormEventStore(params.DB)
	if err != nil {
		return EventStoreResult{}, fmt.Errorf("failed to create event store: %w", err)
	}
	return EventStoreResult{EventStore: store}, nil
}