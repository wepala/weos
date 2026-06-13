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

package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

var _ domain.EventStore = (*BigQueryEventStore)(nil)

// BigQueryEventStore is a BigQuery-based implementation of EventStore.
// Vendored from pericarp (removed upstream in akeemphilbert/pericarp#44);
// weos uses it as the secondary store in DualWriteEventStore.
//
// Concurrency model: the version check in Append uses a read-then-write approach.
// Two concurrent callers targeting the same aggregate can both pass the version
// check and insert events with duplicate sequence numbers, so strict optimistic
// concurrency is not guaranteed. Acceptable here because the dual-write path
// always appends with expectedVersion -1 and the primary store is the source
// of truth for reads and conflict detection.
type BigQueryEventStore struct {
	client    *bigquery.Client
	projectID string
	datasetID string
	tableID   string
}

// NewBigQueryEventStore creates a new BigQuery-based event store.
// The table must already exist — BigQuery tables are provisioned via IaC.
func NewBigQueryEventStore(client *bigquery.Client, projectID, datasetID, tableID string) *BigQueryEventStore {
	if client == nil {
		panic("bigquery client must not be nil")
	}
	if projectID == "" {
		panic("project ID must not be empty")
	}
	if datasetID == "" {
		panic("dataset ID must not be empty")
	}
	if tableID == "" {
		panic("table ID must not be empty")
	}
	return &BigQueryEventStore{
		client:    client,
		projectID: projectID,
		datasetID: datasetID,
		tableID:   tableID,
	}
}

func (s *BigQueryEventStore) table() string {
	return fullTableID(s.projectID, s.datasetID, s.tableID)
}

// Append appends events to the store for the given aggregate.
// If expectedVersion is not -1, optimistic concurrency control is enforced.
func (s *BigQueryEventStore) Append(
	ctx context.Context, aggregateID string, expectedVersion int,
	events ...domain.EventEnvelope[any],
) error {
	if len(events) == 0 {
		return nil
	}

	for _, event := range events {
		if event.AggregateID != aggregateID {
			return fmt.Errorf("%w: aggregate ID mismatch", domain.ErrInvalidEvent)
		}
		if event.ID == "" {
			return fmt.Errorf("%w: event ID is required", domain.ErrInvalidEvent)
		}
		if event.EventType == "" {
			return fmt.Errorf("%w: event type is required", domain.ErrInvalidEvent)
		}
	}

	if expectedVersion != -1 {
		currentVersion, err := s.GetCurrentVersion(ctx, aggregateID)
		if err != nil {
			return fmt.Errorf("failed to get current version for conflict check: %w", err)
		}
		if currentVersion != expectedVersion {
			return fmt.Errorf("%w: expected version %d", domain.ErrConcurrencyConflict, expectedVersion)
		}
	}

	return BatchInsertEvents(ctx, s.client, s.projectID, s.datasetID, s.tableID, events)
}

// GetEvents retrieves all events for the given aggregate ID.
func (s *BigQueryEventStore) GetEvents(
	ctx context.Context, aggregateID string,
) ([]domain.EventEnvelope[any], error) {
	q := s.client.Query(selectEventColumns + s.table() + " WHERE aggregate_id = @agg ORDER BY sequence_no ASC")
	q.Parameters = []bigquery.QueryParameter{
		{Name: "agg", Value: aggregateID},
	}

	return s.queryEnvelopes(ctx, q)
}

// GetEventsFromVersion retrieves events starting from the specified version.
func (s *BigQueryEventStore) GetEventsFromVersion(
	ctx context.Context, aggregateID string, fromVersion int,
) ([]domain.EventEnvelope[any], error) {
	q := s.client.Query(selectEventColumns + s.table() +
		" WHERE aggregate_id = @agg AND sequence_no >= @from_ver ORDER BY sequence_no ASC")
	q.Parameters = []bigquery.QueryParameter{
		{Name: "agg", Value: aggregateID},
		{Name: "from_ver", Value: fromVersion},
	}

	return s.queryEnvelopes(ctx, q)
}

// GetEventsRange retrieves events within a version range.
// If fromVersion is -1, it defaults to 1. If toVersion is -1, all events from fromVersion onwards are returned.
func (s *BigQueryEventStore) GetEventsRange(
	ctx context.Context, aggregateID string, fromVersion, toVersion int,
) ([]domain.EventEnvelope[any], error) {
	if fromVersion == -1 {
		fromVersion = 1
	}

	if toVersion == -1 {
		return s.GetEventsFromVersion(ctx, aggregateID, fromVersion)
	}

	q := s.client.Query(selectEventColumns + s.table() +
		" WHERE aggregate_id = @agg AND sequence_no >= @from_ver AND sequence_no <= @to_ver ORDER BY sequence_no ASC")
	q.Parameters = []bigquery.QueryParameter{
		{Name: "agg", Value: aggregateID},
		{Name: "from_ver", Value: fromVersion},
		{Name: "to_ver", Value: toVersion},
	}

	return s.queryEnvelopes(ctx, q)
}

// GetEventByID retrieves a specific event by its ID.
// Note: This performs a full table scan since id is not in the clustering key.
func (s *BigQueryEventStore) GetEventByID(
	ctx context.Context, eventID string,
) (domain.EventEnvelope[any], error) {
	q := s.client.Query(selectEventColumns + s.table() + " WHERE id = @event_id LIMIT 1")
	q.Parameters = []bigquery.QueryParameter{
		{Name: "event_id", Value: eventID},
	}

	it, err := q.Read(ctx)
	if err != nil {
		return domain.EventEnvelope[any]{}, fmt.Errorf("failed to query event by ID: %w", err)
	}

	var row bigqueryEventRow
	err = it.Next(&row)
	if err == iterator.Done {
		return domain.EventEnvelope[any]{}, domain.ErrEventNotFound
	}
	if err != nil {
		return domain.EventEnvelope[any]{}, fmt.Errorf("failed to read event row: %w", err)
	}

	return bigqueryRowToEnvelope(row)
}

// GetEventsByTransactionID retrieves all events with the given transaction ID.
func (s *BigQueryEventStore) GetEventsByTransactionID(
	ctx context.Context, transactionID string,
) ([]domain.EventEnvelope[any], error) {
	if transactionID == "" {
		return nil, fmt.Errorf("%w: transaction ID must not be empty", domain.ErrInvalidEvent)
	}

	q := s.client.Query(selectEventColumns + s.table() +
		" WHERE transaction_id = @txid ORDER BY aggregate_id ASC, sequence_no ASC")
	q.Parameters = []bigquery.QueryParameter{
		{Name: "txid", Value: transactionID},
	}

	return s.queryEnvelopes(ctx, q)
}

// GetCurrentVersion returns the current version for the aggregate.
// Returns 0 if the aggregate doesn't exist.
func (s *BigQueryEventStore) GetCurrentVersion(ctx context.Context, aggregateID string) (int, error) {
	q := s.client.Query("SELECT COALESCE(MAX(sequence_no), 0) AS max_seq FROM " + s.table() +
		" WHERE aggregate_id = @agg")
	q.Parameters = []bigquery.QueryParameter{
		{Name: "agg", Value: aggregateID},
	}

	it, err := q.Read(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to query current version: %w", err)
	}

	var result struct {
		MaxSeq int64 `bigquery:"max_seq"`
	}
	err = it.Next(&result)
	if err == iterator.Done {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to read current version: %w", err)
	}

	return int(result.MaxSeq), nil
}

// ReadAfter is not supported: the BigQuery sink has no global, cross-aggregate
// write ordering to assign positions from (it is a write-only analytics copy;
// the primary store owns the ordered feed). It always returns
// ErrGlobalOrderingNotSupported.
func (s *BigQueryEventStore) ReadAfter(
	_ context.Context, _ int64, _ int,
) ([]domain.EventEnvelope[any], error) {
	return nil, domain.ErrGlobalOrderingNotSupported
}

// HeadPosition is not supported for the same reason as ReadAfter. It always
// returns ErrGlobalOrderingNotSupported.
func (s *BigQueryEventStore) HeadPosition(_ context.Context) (int64, error) {
	return 0, domain.ErrGlobalOrderingNotSupported
}

// Close closes the BigQuery event store (no-op since client is managed externally).
func (s *BigQueryEventStore) Close() error {
	return nil
}

const selectEventColumns = "SELECT id, aggregate_id, event_type, sequence_no, transaction_id, " +
	"payload, metadata, created_at FROM "

func (s *BigQueryEventStore) queryEnvelopes(
	ctx context.Context, q *bigquery.Query,
) ([]domain.EventEnvelope[any], error) {
	it, err := q.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}

	var envelopes []domain.EventEnvelope[any]
	for {
		var row bigqueryEventRow
		err := it.Next(&row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read event row: %w", err)
		}

		env, err := bigqueryRowToEnvelope(row)
		if err != nil {
			return nil, fmt.Errorf("event row conversion failed (aggregate=%s, seq=%d): %w",
				row.AggregateID, row.SequenceNo, err)
		}
		envelopes = append(envelopes, env)
	}

	if envelopes == nil {
		return []domain.EventEnvelope[any]{}, nil
	}
	return envelopes, nil
}

// bigqueryEventRow is the persistence-layer DTO for BigQuery rows.
// Fields must stay in sync with the BigQuery table schema.
// TransactionID uses bigquery.NullString so that existing rows with NULL
// transaction_id decode without error.
type bigqueryEventRow struct {
	ID            string              `bigquery:"id"`
	AggregateID   string              `bigquery:"aggregate_id"`
	EventType     string              `bigquery:"event_type"`
	SequenceNo    int64               `bigquery:"sequence_no"`
	TransactionID bigquery.NullString `bigquery:"transaction_id"`
	Payload       string              `bigquery:"payload"`
	Metadata      string              `bigquery:"metadata"`
	CreatedAt     time.Time           `bigquery:"created_at"`
}

func bigqueryRowToEnvelope(row bigqueryEventRow) (domain.EventEnvelope[any], error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		return domain.EventEnvelope[any]{},
			fmt.Errorf("%w: failed to unmarshal payload for event %s: %v", domain.ErrInvalidEvent, row.ID, err)
	}

	var metadata map[string]any
	if row.Metadata != "" {
		if err := json.Unmarshal([]byte(row.Metadata), &metadata); err != nil {
			return domain.EventEnvelope[any]{},
				fmt.Errorf("%w: failed to unmarshal metadata for event %s: %v", domain.ErrInvalidEvent, row.ID, err)
		}
	}
	if metadata == nil {
		metadata = make(map[string]any)
	}

	txID := ""
	if row.TransactionID.Valid {
		txID = row.TransactionID.StringVal
	}

	return domain.EventEnvelope[any]{
		ID:            row.ID,
		AggregateID:   row.AggregateID,
		EventType:     row.EventType,
		Payload:       payload,
		Created:       row.CreatedAt,
		SequenceNo:    int(row.SequenceNo),
		TransactionID: txID,
		Metadata:      metadata,
	}, nil
}
