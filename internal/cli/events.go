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

package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/spf13/cobra"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/infrastructure"

	bqevents "weos/infrastructure/events"
)

var eventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Manage event store",
}

var syncBigQueryCmd = &cobra.Command{
	Use:   "sync-bigquery",
	Short: "Copy events from database to BigQuery",
	Long:  `Copies events created before the specified timestamp from the database to BigQuery. Idempotent — skips events already present in BigQuery.`,
	RunE:  runSyncBigQuery,
}

func init() {
	syncBigQueryCmd.Flags().String("before", "", "Sync events created before this time (RFC3339, e.g. 2026-03-29T00:00:00Z)")
	_ = syncBigQueryCmd.MarkFlagRequired("before")
	syncBigQueryCmd.Flags().Int("batch-size", 500, "Number of events per BigQuery insert")
	syncBigQueryCmd.Flags().Bool("dry-run", false, "Count events to sync without writing")

	eventsCmd.AddCommand(syncBigQueryCmd)
	rootCmd.AddCommand(eventsCmd)
}

func runSyncBigQuery(cmd *cobra.Command, _ []string) error {
	cliCfg := GetConfig()
	appCfg := cliCfg.Config

	if appCfg.BigQueryProjectID == "" {
		return fmt.Errorf("BigQuery not configured: set BIGQUERY_PROJECT_ID environment variable")
	}

	beforeStr, _ := cmd.Flags().GetString("before")
	before, err := time.Parse(time.RFC3339, beforeStr)
	if err != nil {
		return fmt.Errorf("invalid --before timestamp (expected RFC3339): %w", err)
	}

	batchSize, _ := cmd.Flags().GetInt("batch-size")
	if batchSize < 1 || batchSize > 1000 {
		return fmt.Errorf("--batch-size must be between 1 and 1000")
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	datasetID := appCfg.BigQueryDatasetID
	if datasetID == "" {
		datasetID = "weos_events"
	}
	tableID := appCfg.BigQueryTableID
	if tableID == "" {
		tableID = "events"
	}

	ctx := cmd.Context()

	db, err := openDB(appCfg.DatabaseDSN)
	if err != nil {
		return err
	}
	sqlDB, _ := db.DB()
	defer func() { _ = sqlDB.Close() }()

	bqClient, err := bigquery.NewClient(ctx, appCfg.BigQueryProjectID)
	if err != nil {
		return fmt.Errorf("failed to create BigQuery client: %w", err)
	}
	defer func() { _ = bqClient.Close() }()

	fmt.Fprintf(os.Stderr, "Fetching existing event IDs from BigQuery...\n")
	existingIDs, err := bqevents.GetExistingEventIDs(ctx, bqClient, appCfg.BigQueryProjectID, datasetID, tableID, before)
	if err != nil {
		return fmt.Errorf("failed to fetch existing BigQuery events: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Found %d events already in BigQuery\n", len(existingIDs))

	var totalCount int64
	if err := db.Model(&infrastructure.GormEventModel{}).Where("created_at < ?", before).Count(&totalCount).Error; err != nil {
		return fmt.Errorf("failed to count events: %w", err)
	}

	if dryRun {
		_, _ = fmt.Fprintf(os.Stdout, "Dry run: %d events in database before %s, %d already in BigQuery, up to %d to sync\n",
			totalCount, before.Format(time.RFC3339), len(existingIDs), totalCount-int64(len(existingIDs)))
		return nil
	}

	fmt.Fprintf(os.Stderr, "Syncing up to %d events (batch size %d)...\n", totalCount, batchSize)

	return syncLoop(ctx, os.Stderr, db, bqClient, syncConfig{
		projectID:   appCfg.BigQueryProjectID,
		datasetID:   datasetID,
		tableID:     tableID,
		before:      before,
		batchSize:   batchSize,
		totalCount:  totalCount,
		existingIDs: existingIDs,
	})
}

type syncConfig struct {
	projectID   string
	datasetID   string
	tableID     string
	before      time.Time
	batchSize   int
	totalCount  int64
	existingIDs map[string]struct{}
}

func syncLoop(
	ctx context.Context, w io.Writer, db *gorm.DB, bqClient *bigquery.Client, sc syncConfig,
) error {
	cursor := ""
	var synced, skipped int64

	for {
		var models []infrastructure.GormEventModel
		q := db.Where("created_at < ? AND id > ?", sc.before, cursor).
			Order("id ASC").
			Limit(sc.batchSize).
			Find(&models)
		if q.Error != nil {
			return fmt.Errorf("failed to read events: %w", q.Error)
		}
		if len(models) == 0 {
			break
		}

		var toInsert []domain.EventEnvelope[any]
		for _, m := range models {
			if _, exists := sc.existingIDs[m.ID]; exists {
				skipped++
				continue
			}
			toInsert = append(toInsert, modelToEnvelope(m))
		}

		if len(toInsert) > 0 {
			if err := bqevents.BatchInsertEvents(ctx, bqClient, sc.projectID, sc.datasetID, sc.tableID, toInsert); err != nil {
				return fmt.Errorf("failed to insert batch at cursor %s: %w", cursor, err)
			}
			synced += int64(len(toInsert))
		}

		cursor = models[len(models)-1].ID
		_, _ = fmt.Fprintf(w, "  Progress: %d synced, %d skipped (of %d total)\n",
			synced, skipped, sc.totalCount)
	}

	_, _ = fmt.Fprintf(w, "Sync complete: %d events inserted, %d skipped (already existed)\n", synced, skipped)
	return nil
}

func modelToEnvelope(m infrastructure.GormEventModel) domain.EventEnvelope[any] {
	metadata := map[string]any(m.Metadata)
	if metadata == nil {
		metadata = make(map[string]any)
	}
	return domain.EventEnvelope[any]{
		ID:            m.ID,
		AggregateID:   m.AggregateID,
		EventType:     m.EventType,
		Payload:       map[string]any(m.Payload),
		Created:       m.CreatedAt,
		SequenceNo:    m.SequenceNo,
		TransactionID: m.TransactionID,
		Metadata:      metadata,
	}
}

func openDB(dsn string) (*gorm.DB, error) {
	if strings.HasPrefix(dsn, "host=") || strings.Contains(dsn, "postgres://") || strings.Contains(dsn, "postgresql://") {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if err != nil {
			return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
		}
		return db, nil
	}
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SQLite: %w", err)
	}
	return db, nil
}
