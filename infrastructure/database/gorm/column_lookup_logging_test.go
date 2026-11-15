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
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/models"
)

// The tests below pin the log lines the exact column check made necessary
// (wm-kv7szf): each marks a path where a missing column now changes the
// result without an error, so the log is the only trace of it.

// fieldEntry is one log call, with its fields kept as key/value pairs.
type fieldEntry struct {
	level, msg string
	fields     map[string]any
}

// fieldLogger keeps every call with its level and fields, so a test can ask
// for "a Warn naming table X and column Y" rather than match formatted text.
type fieldLogger struct {
	mu      sync.Mutex
	entries []fieldEntry
}

func (l *fieldLogger) record(level, msg string, kv ...any) {
	fields := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		fields[fmt.Sprint(kv[i])] = kv[i+1]
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, fieldEntry{level: level, msg: msg, fields: fields})
}

func (l *fieldLogger) Debug(_ context.Context, msg string, kv ...any) { l.record("debug", msg, kv...) }
func (l *fieldLogger) Info(_ context.Context, msg string, kv ...any)  { l.record("info", msg, kv...) }
func (l *fieldLogger) Warn(_ context.Context, msg string, kv ...any)  { l.record("warn", msg, kv...) }
func (l *fieldLogger) Error(_ context.Context, msg string, kv ...any) { l.record("error", msg, kv...) }

// find returns the entries at level whose message holds text and whose fields
// hold every pair in want.
func (l *fieldLogger) find(level, text string, want map[string]any) []fieldEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []fieldEntry
	for _, e := range l.entries {
		if e.level != level || !strings.Contains(e.msg, text) {
			continue
		}
		match := true
		for k, v := range want {
			if e.fields[k] != v {
				match = false
				break
			}
		}
		if match {
			out = append(out, e)
		}
	}
	return out
}

func TestAddMissingColumns_LogsEachColumnActuallyAdded(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	logger := &fieldLogger{}
	pm := &projectionManager{db: db, logger: logger}
	ctx := context.Background()
	const added = "projection column added"

	// A new table is created with all its columns, so nothing is "added".
	if err := pm.EnsureTable(ctx, "recipe-ingredient", keywordBeforeSchema, nil); err != nil {
		t.Fatalf("EnsureTable (old schema): %v", err)
	}
	if got := logger.find("info", added, nil); len(got) != 0 {
		t.Fatalf("creating a table logged %d added columns, want 0: %+v", len(got), got)
	}

	if err := pm.EnsureTable(ctx, "recipe-ingredient", keywordAfterSchema, nil); err != nil {
		t.Fatalf("EnsureTable (schema with text, key and slug): %v", err)
	}
	for _, col := range []string{"text", "key", "slug"} {
		got := logger.find("info", added, map[string]any{"table": "recipe_ingredients", "column": col})
		if len(got) != 1 {
			t.Errorf("Info lines for the added %q column = %d, want 1", col, len(got))
			continue
		}
		if !strings.Contains(got[0].msg, "weos worker reproject") {
			t.Errorf("the added-column line does not name the reproject: %q", got[0].msg)
		}
	}
	if got := logger.find("info", added, map[string]any{"column": "name"}); len(got) != 0 {
		t.Errorf("logged the existing name column as added: %+v", got)
	}

	// A second run over the same schema adds nothing and logs nothing new.
	before := len(logger.find("info", added, nil))
	if err := pm.EnsureTable(ctx, "recipe-ingredient", keywordAfterSchema, nil); err != nil {
		t.Fatalf("EnsureTable (again): %v", err)
	}
	if after := len(logger.find("info", added, nil)); after != before {
		t.Errorf("a no-op EnsureTable logged %d more added columns, want 0", after-before)
	}
}

func TestColumnExists_LogsTheQueryError(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	logger := &fieldLogger{}
	if columnExists(context.Background(), db, logger, "widgets", "text") {
		t.Fatal("columnExists on a closed database = true, want false")
	}
	got := logger.find("warn", "column lookup failed", map[string]any{"table": "widgets", "column": "text"})
	if len(got) != 1 || got[0].fields["error"] == nil {
		t.Errorf("want one Warn naming widgets.text with the error; got %+v", logger.entries)
	}
}

func TestProjectionListing_LogsDroppedFilterAndSortFallback(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Resource{}, &models.ResourcePermission{}); err != nil {
		t.Fatalf("migrate resources: %v", err)
	}
	ctx := context.Background()
	logger := &fieldLogger{}
	pm := &projectionManager{db: db, logger: &testLogger{}}
	schema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	if err := pm.EnsureTable(ctx, "widget", schema, nil); err != nil {
		t.Fatalf("EnsureTable: %v", err)
	}
	repo := &ResourceRepository{db: db, projMgr: pm, logger: logger}
	filters := []repositories.FilterCondition{
		{Field: "color", Operator: "eq", Value: "red"},
		{Field: "name", Operator: "eq", Value: "Sprocket"},
	}
	sort := repositories.SortOptions{SortBy: "weightKg", SortOrder: "asc"}

	if _, err := repo.FindAllByTypeWithFilters(ctx, "widget", filters, "", 10, sort, nil); err != nil {
		t.Fatalf("FindAllByTypeWithFilters: %v", err)
	}
	if _, err := repo.FindAllByTypeFlatWithFilters(ctx, "widget", filters, "", 10, sort, nil); err != nil {
		t.Fatalf("FindAllByTypeFlatWithFilters: %v", err)
	}
	if _, err := repo.FindAllByType(ctx, "widget", "", 10, sort, nil); err != nil {
		t.Fatalf("FindAllByType: %v", err)
	}

	drops := logger.find("warn", "projection filter dropped", map[string]any{"table": "widgets", "column": "color"})
	if len(drops) != 2 {
		t.Errorf("Warn lines for the dropped color filter = %d, want 2 (filtered and flat listing)", len(drops))
	}
	if got := logger.find("warn", "projection filter dropped", map[string]any{"column": "name"}); len(got) != 0 {
		t.Errorf("logged the name filter, which has a column, as dropped: %+v", got)
	}
	falls := logger.find("warn", "projection sort falls back to id", map[string]any{"table": "widgets", "column": "weight_kg"})
	if len(falls) != 3 {
		t.Errorf("Warn lines for the weight_kg sort fallback = %d, want 3 (one per listing)", len(falls))
	}

	// A sort on a real or standard column, or on data, logs nothing.
	before := len(logger.entries)
	for _, by := range []string{"name", "createdAt", "id", "data"} {
		s := repositories.SortOptions{SortBy: by}
		if _, err := repo.FindAllByType(ctx, "widget", "", 10, s, nil); err != nil {
			t.Fatalf("FindAllByType sorted by %s: %v", by, err)
		}
	}
	if extra := logger.entries[before:]; len(extra) != 0 {
		t.Errorf("a sort on an existing column logged: %+v", extra)
	}
}

func TestAccountPurger_LogsProjectionTablesWithNoAccountID(t *testing.T) {
	f := newPurgeFixture(t)
	f.account("acct-harbor")
	// The agent type's projection shares the auth identity table by name, and
	// that table carries no account_id column, so the purge must skip it.
	f.must(f.db.Create(&models.ResourceType{ID: "urn:type:agent", Name: "Agent", Slug: "agent", Status: "active"}).Error)
	if columnExists(context.Background(), f.db, &testLogger{}, "agents", "account_id") {
		t.Fatal("precondition: the agents table has an account_id column")
	}

	logger := &fieldLogger{}
	if _, err := NewAccountPurgerForTest(f.db, f.pm, logger).Purge(context.Background(), "acct-harbor"); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	const skip = "skipping projection table with no account_id"
	if got := logger.find("info", skip, map[string]any{"table": "agents", "slug": "agent"}); len(got) != 1 {
		t.Errorf("Info lines for the skipped agents table = %d, want 1; log: %+v", len(got), logger.entries)
	}
	if got := logger.find("info", skip, map[string]any{"table": "recipes"}); len(got) != 0 {
		t.Errorf("logged the recipes table, which has account_id, as skipped: %+v", got)
	}
}
