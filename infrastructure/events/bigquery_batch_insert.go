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
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
)

func nullableString(s string) bigquery.NullString {
	return bigquery.NullString{StringVal: s, Valid: s != ""}
}

func fullTableID(projectID, datasetID, tableID string) string {
	return fmt.Sprintf("`%s.%s.%s`", projectID, datasetID, tableID)
}

// BatchInsertEvents inserts a batch of events into BigQuery in a single INSERT statement.
// Events may belong to different aggregates. Skips version checks.
func BatchInsertEvents(
	ctx context.Context,
	client *bigquery.Client,
	projectID, datasetID, tableID string,
	events []domain.EventEnvelope[any],
) error {
	if len(events) == 0 {
		return nil
	}

	var valuePlaceholders []string
	var params []bigquery.QueryParameter

	for i, event := range events {
		payloadJSON, metadataJSON, err := marshalEvent(event)
		if err != nil {
			return fmt.Errorf("failed to marshal event %s: %w", event.ID, err)
		}

		idP := fmt.Sprintf("id_%d", i)
		aggP := fmt.Sprintf("agg_%d", i)
		typeP := fmt.Sprintf("type_%d", i)
		seqP := fmt.Sprintf("seq_%d", i)
		txP := fmt.Sprintf("tx_%d", i)
		payP := fmt.Sprintf("pay_%d", i)
		metaP := fmt.Sprintf("meta_%d", i)
		tsP := fmt.Sprintf("ts_%d", i)

		valuePlaceholders = append(valuePlaceholders,
			fmt.Sprintf("(@%s, @%s, @%s, @%s, @%s, PARSE_JSON(@%s), PARSE_JSON(@%s), @%s)",
				idP, aggP, typeP, seqP, txP, payP, metaP, tsP))

		params = append(params,
			bigquery.QueryParameter{Name: idP, Value: event.ID},
			bigquery.QueryParameter{Name: aggP, Value: event.AggregateID},
			bigquery.QueryParameter{Name: typeP, Value: event.EventType},
			bigquery.QueryParameter{Name: seqP, Value: event.SequenceNo},
			bigquery.QueryParameter{Name: txP, Value: nullableString(event.TransactionID)},
			bigquery.QueryParameter{Name: payP, Value: payloadJSON},
			bigquery.QueryParameter{Name: metaP, Value: metadataJSON},
			bigquery.QueryParameter{Name: tsP, Value: event.Created.UTC()},
		)
	}

	querySQL := fmt.Sprintf(
		"INSERT INTO %s (id, aggregate_id, event_type, sequence_no, transaction_id, payload, metadata, created_at) VALUES %s",
		fullTableID(projectID, datasetID, tableID),
		strings.Join(valuePlaceholders, ", "),
	)

	q := client.Query(querySQL)
	q.Parameters = params

	job, err := q.Run(ctx)
	if err != nil {
		return fmt.Errorf("failed to run batch insert: %w", err)
	}

	status, err := job.Wait(ctx)
	if err != nil {
		return fmt.Errorf("failed to wait for batch insert: %w", err)
	}
	if err := status.Err(); err != nil {
		return fmt.Errorf("batch insert failed: %w", err)
	}

	return nil
}

// GetExistingEventIDs returns the set of event IDs already in BigQuery
// with created_at before the given timestamp. Used for idempotent sync.
func GetExistingEventIDs(
	ctx context.Context,
	client *bigquery.Client,
	projectID, datasetID, tableID string,
	before time.Time,
) (map[string]struct{}, error) {
	querySQL := fmt.Sprintf("SELECT id FROM %s WHERE created_at < @before",
		fullTableID(projectID, datasetID, tableID))

	q := client.Query(querySQL)
	q.Parameters = []bigquery.QueryParameter{
		{Name: "before", Value: before.UTC()},
	}

	it, err := q.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query existing event IDs: %w", err)
	}

	ids := make(map[string]struct{})
	for {
		var row struct {
			ID string `bigquery:"id"`
		}
		err := it.Next(&row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read event ID row: %w", err)
		}
		ids[row.ID] = struct{}{}
	}

	return ids, nil
}

func marshalEvent(env domain.EventEnvelope[any]) (string, string, error) {
	payload, ok := env.Payload.(map[string]any)
	if !ok {
		raw, err := json.Marshal(env.Payload)
		if err != nil {
			return "", "", fmt.Errorf("failed to marshal payload: %w", err)
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return "", "", fmt.Errorf("failed to convert payload to map: %w", err)
		}
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	metadata := env.Metadata
	if metadata == nil {
		metadata = make(map[string]any)
	}

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return string(payloadJSON), string(metadataJSON), nil
}
