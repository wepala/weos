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
	"os"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/wepala/weos/v3/infrastructure/models"
)

// The schemas below model the recipe-ingredient drift found on a production
// snapshot (wm-rk8abw): the type's projection table already exists without a
// `text` column, and a later schema declares `text`. EnsureTable must ALTER the
// column in. On SQLite it never did, because the glebarez migrator's HasColumn
// matches the column name with case-insensitive LIKE patterns over the CREATE
// statement, and every projection table's DDL holds "id TEXT PRIMARY KEY" — so
// a column named text always read as present and the ALTER was skipped. The
// same patterns match any name followed by a space anywhere in the DDL: "slug"
// matches "type_slug TEXT". text and slug are the cases that fail without the
// fix. "key" is a control case: the projection DDL writes "id TEXT PRIMARY
// KEY," with a comma after KEY, so the old check already added it; a real key
// false positive needs "PRIMARY KEY " followed by a space, which
// columnExistsCases covers through "primary".
var (
	keywordBeforeSchema = json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"}}}`)
	keywordAfterSchema = json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"},"text":{"type":"string"},"key":{"type":"string"},` +
		`"slug":{"type":"string"}}}`)
)

// physicalColumns reads a table's column names straight from the database
// catalog, so the assertion does not rest on the check under test.
func physicalColumns(t *testing.T, db *gorm.DB, table string) map[string]bool {
	t.Helper()
	var names []string
	var err error
	if db.Name() == "postgres" {
		err = db.Raw(`SELECT column_name FROM information_schema.columns
			WHERE table_schema = CURRENT_SCHEMA() AND table_name = ?`, table).Scan(&names).Error
	} else {
		err = db.Raw(`SELECT name FROM pragma_table_info(?)`, table).Scan(&names).Error
	}
	if err != nil {
		t.Fatalf("read the columns of %s: %v", table, err)
	}
	got := make(map[string]bool, len(names))
	for _, n := range names {
		got[n] = true
	}
	return got
}

// ensureKeywordColumnsAndWrite runs the drift sequence on db for slug: the
// table is made from the old schema, EnsureTable runs again with the schema
// that adds text, key and slug, and a resource carrying all three is saved.
func ensureKeywordColumnsAndWrite(t *testing.T, db *gorm.DB, slug string) {
	t.Helper()
	ctx := context.Background()
	pm := &projectionManager{db: db, logger: &testLogger{}}
	table := slugToTableName(slug)

	if err := pm.EnsureTable(ctx, slug, keywordBeforeSchema, nil); err != nil {
		t.Fatalf("EnsureTable (old schema): %v", err)
	}
	if cols := physicalColumns(t, db, table); cols["text"] || cols["key"] || cols["slug"] {
		t.Fatalf("precondition: %s already has text/key/slug columns: %v", table, cols)
	}

	if err := pm.EnsureTable(ctx, slug, keywordAfterSchema, nil); err != nil {
		t.Fatalf("EnsureTable (schema with text, key and slug): %v", err)
	}
	cols := physicalColumns(t, db, table)
	for _, want := range []string{"text", "key", "slug"} {
		if !cols[want] {
			t.Errorf("EnsureTable did not add the %q column to the existing %s table; columns: %v",
				want, table, cols)
		}
	}

	repo := &ResourceRepository{db: db, projMgr: pm, logger: &testLogger{}}
	id := "urn:" + slug + ":flour"
	entity := makeTestResource(t, id, slug,
		`{"name":"Flour","text":"2 cups all-purpose flour","key":"flour","slug":"flour-ap"}`)
	if err := repo.Save(ctx, entity); err != nil {
		t.Fatalf("Save of a resource carrying text, key and slug: %v", err)
	}
	var row struct {
		Text string
		Key  string
		Slug string
	}
	if err := db.Table(table).Select("text", "key", "slug").Where("id = ?", id).Take(&row).Error; err != nil {
		t.Fatalf("read back the projection row: %v", err)
	}
	if row.Text != "2 cups all-purpose flour" || row.Key != "flour" || row.Slug != "flour-ap" {
		t.Errorf("projection row = %+v, want text=%q key=%q slug=%q",
			row, "2 cups all-purpose flour", "flour", "flour-ap")
	}
}

func TestEnsureTable_AddsKeywordNamedColumnsToExistingTable(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Resource{}, &models.ResourcePermission{}); err != nil {
		t.Fatalf("migrate resources: %v", err)
	}
	ensureKeywordColumnsAndWrite(t, db, "recipe-ingredient")
}

// TestEnsureTable_AddsKeywordNamedColumnsToExistingTable_Postgres runs the same
// sequence against PostgreSQL, where the migrator reads information_schema and
// was already exact; the shared check must keep it so. Set TEST_POSTGRES_DSN to
// run it.
func TestEnsureTable_AddsKeywordNamedColumnsToExistingTable_Postgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set — the PostgreSQL keyword-column path is skipped")
	}
	db := openPostgresReplica(t, dsn)
	if err := db.AutoMigrate(&models.Resource{}, &models.ResourcePermission{}); err != nil {
		t.Fatalf("migrate resources: %v", err)
	}
	slug := fmt.Sprintf("kwingredient%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_ = db.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %q`, slugToTableName(slug))).Error
		_ = db.Where("type_slug = ?", slug).Delete(&models.Resource{}).Error
	})
	ensureKeywordColumnsAndWrite(t, db, slug)
}

// columnExistsCases is the truth table for columnExists against the table
// columnExistsDDL creates. Every "false" row is a name the glebarez migrator's
// HasColumn reported as present on that table.
var columnExistsCases = []struct {
	column string
	want   bool
}{
	{"id", true},
	{"type_slug", true},
	{"status", true},
	{"rich_text", true},
	{"accountxid", true},
	{"text", false},       // "id TEXT PRIMARY KEY"
	{"slug", false},       // "type_slug TEXT"
	{"primary", false},    // "PRIMARY KEY"
	{"not", false},        // "NOT NULL"
	{"default", false},    // "DEFAULT 'active'"
	{"rich", false},       // a prefix of rich_text is not rich_text
	{"account_id", false}, // '_' is a LIKE wildcard: matches "accountxid "
	{"", false},
}

const columnExistsDDL = `CREATE TABLE %s (
  id TEXT PRIMARY KEY, type_slug TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active', rich_text TEXT, accountxid TEXT)`

func runColumnExistsCases(t *testing.T, db *gorm.DB, table string) {
	t.Helper()
	for _, tc := range columnExistsCases {
		if got := columnExists(context.Background(), db, &testLogger{}, table, tc.column); got != tc.want {
			t.Errorf("columnExists(%s, %q) = %v, want %v", table, tc.column, got, tc.want)
		}
	}
	if columnExists(context.Background(), db, &testLogger{}, table+"_missing", "id") {
		t.Errorf("columnExists on a table that does not exist = true, want false")
	}
}

func TestColumnExists_ExactNameOnSQLite(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := db.Exec(fmt.Sprintf(columnExistsDDL, "widgets")).Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	runColumnExistsCases(t, db, "widgets")

	// SQLite identifiers ignore case, so a differently cased name is the same
	// column — ADD COLUMN of it would fail as a duplicate.
	if !columnExists(context.Background(), db, &testLogger{}, "widgets", "Status") {
		t.Error(`columnExists(widgets, "Status") = false, want true (SQLite identifiers ignore case)`)
	}
}

func TestColumnExists_ExactNameOnPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set — the PostgreSQL columnExists path is skipped")
	}
	db := openPostgresReplica(t, dsn)
	table := fmt.Sprintf("colexists%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = db.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %q`, table)).Error })
	if err := db.Exec(fmt.Sprintf(columnExistsDDL, table)).Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	runColumnExistsCases(t, db, table)
}
