package events

import (
	"context"
	"fmt"

	"cloud.google.com/go/bigquery"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/infrastructure"

	"github.com/wepala/weos/v3/internal/config"
)

// ProvideBigQueryEventStore creates a BigQuery event store from config.
// Returns nil if BigQuery is not configured.
func ProvideBigQueryEventStore(cfg config.Config) (*infrastructure.BigQueryEventStore, error) {
	if cfg.BigQueryProjectID == "" {
		return nil, nil
	}

	ctx := context.Background()
	client, err := bigquery.NewClient(ctx, cfg.BigQueryProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create BigQuery client: %w", err)
	}

	datasetID := cfg.BigQueryDatasetID
	if datasetID == "" {
		datasetID = "weos_events"
	}
	tableID := cfg.BigQueryTableID
	if tableID == "" {
		tableID = "events"
	}

	return infrastructure.NewBigQueryEventStore(
		client, cfg.BigQueryProjectID, datasetID, tableID,
	), nil
}
