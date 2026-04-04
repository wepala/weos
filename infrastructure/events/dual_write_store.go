package events

import (
	"context"
	"log"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

// DualWriteEventStore writes events to both a primary and secondary event store.
// The primary store (PostgreSQL) handles concurrency control and all reads.
// The secondary store (BigQuery) receives a synchronous copy of writes.
// If the secondary write fails, the error is logged but the operation succeeds.
type DualWriteEventStore struct {
	primary   domain.EventStore
	secondary domain.EventStore
}

func NewDualWriteEventStore(primary, secondary domain.EventStore) *DualWriteEventStore {
	return &DualWriteEventStore{primary: primary, secondary: secondary}
}

func (s *DualWriteEventStore) Append(
	ctx context.Context, aggregateID string, expectedVersion int,
	events ...domain.EventEnvelope[any],
) error {
	if err := s.primary.Append(ctx, aggregateID, expectedVersion, events...); err != nil {
		return err
	}
	// Write to secondary with no version check (-1) since primary is the source of truth.
	if err := s.secondary.Append(ctx, aggregateID, -1, events...); err != nil {
		log.Printf("WARNING: BigQuery dual-write failed for %s: %v", aggregateID, err)
	}
	return nil
}

func (s *DualWriteEventStore) GetEvents(
	ctx context.Context, aggregateID string,
) ([]domain.EventEnvelope[any], error) {
	return s.primary.GetEvents(ctx, aggregateID)
}

func (s *DualWriteEventStore) GetEventsFromVersion(
	ctx context.Context, aggregateID string, fromVersion int,
) ([]domain.EventEnvelope[any], error) {
	return s.primary.GetEventsFromVersion(ctx, aggregateID, fromVersion)
}

func (s *DualWriteEventStore) GetEventsRange(
	ctx context.Context, aggregateID string, fromVersion, toVersion int,
) ([]domain.EventEnvelope[any], error) {
	return s.primary.GetEventsRange(ctx, aggregateID, fromVersion, toVersion)
}

func (s *DualWriteEventStore) GetEventByID(
	ctx context.Context, eventID string,
) (domain.EventEnvelope[any], error) {
	return s.primary.GetEventByID(ctx, eventID)
}

func (s *DualWriteEventStore) GetEventsByTransactionID(
	ctx context.Context, transactionID string,
) ([]domain.EventEnvelope[any], error) {
	return s.primary.GetEventsByTransactionID(ctx, transactionID)
}

func (s *DualWriteEventStore) GetCurrentVersion(
	ctx context.Context, aggregateID string,
) (int, error) {
	return s.primary.GetCurrentVersion(ctx, aggregateID)
}

func (s *DualWriteEventStore) Close() error {
	pErr := s.primary.Close()
	sErr := s.secondary.Close()
	if pErr != nil {
		return pErr
	}
	return sErr
}
