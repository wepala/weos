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
	"testing"
	"time"

	"weos/infrastructure/models"
	"weos/pkg/utils"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSlugToTableName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		slug string
		want string
	}{
		{"blog-post", "blog_posts"},
		{"product", "products"},
		{"faq", "faqs"},
		{"web-page-element", "web_page_elements"},
		{"category", "categories"},
	}
	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			t.Parallel()
			got := slugToTableName(tt.slug)
			if got != tt.want {
				t.Fatalf("slugToTableName(%q) = %q, want %q", tt.slug, got, tt.want)
			}
		})
	}
}

func TestCamelToSnake(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"firstName", "first_name"},
		{"lastName", "last_name"},
		{"name", "name"},
		{"dateOfBirth", "date_of_birth"},
		{"URL", "url"},
		{"price", "price"},
		{"isActive", "is_active"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := utils.CamelToSnake(tt.input)
			if got != tt.want {
				t.Fatalf("utils.CamelToSnake(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSchemaToColumns(t *testing.T) {
	t.Parallel()

	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"@id": {"type": "string"},
			"@type": {"type": "string"},
			"@context": {"type": "object"},
			"name": {"type": "string"},
			"price": {"type": "number"},
			"quantity": {"type": "integer"},
			"isActive": {"type": "boolean"},
			"tags": {"type": "array"},
			"metadata": {"type": "object"},
			"id": {"type": "string"},
			"status": {"type": "string"}
		}
	}`)

	cols := schemaToColumns(schema)
	colMap := make(map[string]string)
	for _, c := range cols {
		colMap[c.Name] = c.SQLType
	}

	// Should include these
	expectations := map[string]string{
		"name":      "TEXT",
		"price":     "REAL",
		"quantity":  "INTEGER",
		"is_active": "BOOLEAN",
		"tags":      "TEXT",
		"metadata":  "TEXT",
	}
	for name, sqlType := range expectations {
		got, ok := colMap[name]
		if !ok {
			t.Errorf("expected column %q but not found", name)
			continue
		}
		if got != sqlType {
			t.Errorf("column %q: got type %q, want %q", name, got, sqlType)
		}
	}

	// Should NOT include these (JSON-LD or standard)
	excluded := []string{"@id", "@type", "@context", "id", "status"}
	for _, name := range excluded {
		if _, ok := colMap[name]; ok {
			t.Errorf("column %q should have been excluded", name)
		}
	}
}

func TestSchemaToColumns_EmptySchema(t *testing.T) {
	t.Parallel()
	cols := schemaToColumns(nil)
	if len(cols) != 0 {
		t.Fatalf("expected 0 columns for nil schema, got %d", len(cols))
	}
}

func TestSchemaToColumns_InvalidJSON(t *testing.T) {
	t.Parallel()
	cols := schemaToColumns(json.RawMessage(`not json`))
	if len(cols) != 0 {
		t.Fatalf("expected 0 columns for invalid JSON, got %d", len(cols))
	}
}

type testLogger struct{}

func (l *testLogger) Info(_ context.Context, _ string, _ ...any)  {}
func (l *testLogger) Warn(_ context.Context, _ string, _ ...any)  {}
func (l *testLogger) Error(_ context.Context, _ string, _ ...any) {}

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	return db
}

func TestEnsureTable_CreatesTable(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}

	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"price": {"type": "number"}
		}
	}`)

	err := pm.EnsureTable(context.Background(), "product", schema, nil)
	if err != nil {
		t.Fatalf("EnsureTable failed: %v", err)
	}

	// Verify table exists by inserting a row
	err = db.Exec(`INSERT INTO products (id, type_slug, status, name, price)
		VALUES ('test-1', 'product', 'active', 'Widget', 9.99)`).Error
	if err != nil {
		t.Fatalf("failed to insert into products table: %v", err)
	}

	// Verify we can read back
	var result map[string]any
	err = db.Table("products").Where("id = ?", "test-1").Take(&result).Error
	if err != nil {
		t.Fatalf("failed to read from products table: %v", err)
	}
}

func TestEnsureTable_Idempotent(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}

	schema := json.RawMessage(`{"type": "object", "properties": {"name": {"type": "string"}}}`)

	err := pm.EnsureTable(context.Background(), "blog-post", schema, nil)
	if err != nil {
		t.Fatalf("first EnsureTable failed: %v", err)
	}

	err = pm.EnsureTable(context.Background(), "blog-post", schema, nil)
	if err != nil {
		t.Fatalf("second EnsureTable failed: %v", err)
	}
}

func TestEnsureTable_AddsNewColumns(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}

	schema1 := json.RawMessage(`{"type": "object", "properties": {"name": {"type": "string"}}}`)
	err := pm.EnsureTable(context.Background(), "event", schema1, nil)
	if err != nil {
		t.Fatalf("first EnsureTable failed: %v", err)
	}

	schema2 := json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"startDate": {"type": "string"}
		}
	}`)
	err = pm.EnsureTable(context.Background(), "event", schema2, nil)
	if err != nil {
		t.Fatalf("second EnsureTable failed: %v", err)
	}

	// Verify new column works
	err = db.Exec(`INSERT INTO events (id, type_slug, status, name, start_date)
		VALUES ('e-1', 'event', 'active', 'Concert', '2026-01-01')`).Error
	if err != nil {
		t.Fatalf("insert with new column failed: %v", err)
	}
}

func TestEnsureTable_NoSchema(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}

	err := pm.EnsureTable(context.Background(), "note", nil, nil)
	if err != nil {
		t.Fatalf("EnsureTable with nil schema failed: %v", err)
	}

	// Should still have standard columns
	err = db.Exec(`INSERT INTO notes (id, type_slug, status)
		VALUES ('n-1', 'note', 'active')`).Error
	if err != nil {
		t.Fatalf("insert into notes failed: %v", err)
	}
}

func TestHasProjectionTable(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}

	if pm.HasProjectionTable("product") {
		t.Fatal("expected false before EnsureTable")
	}

	err := pm.EnsureTable(context.Background(), "product", nil, nil)
	if err != nil {
		t.Fatalf("EnsureTable failed: %v", err)
	}

	if !pm.HasProjectionTable("product") {
		t.Fatal("expected true after EnsureTable")
	}
}

func TestTableName(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}

	// Before EnsureTable, still returns computed name
	if got := pm.TableName("blog-post"); got != "blog_posts" {
		t.Fatalf("TableName = %q, want %q", got, "blog_posts")
	}

	// After EnsureTable, returns cached name
	err := pm.EnsureTable(context.Background(), "blog-post", nil, nil)
	if err != nil {
		t.Fatalf("EnsureTable failed: %v", err)
	}
	if got := pm.TableName("blog-post"); got != "blog_posts" {
		t.Fatalf("TableName after EnsureTable = %q, want %q", got, "blog_posts")
	}
}

func TestExtractFlatColumns(t *testing.T) {
	t.Parallel()

	data := json.RawMessage(`{
		"@id": "urn:product:abc",
		"@type": "Product",
		"@context": "https://schema.org",
		"name": "Widget",
		"price": 9.99,
		"isActive": true,
		"tags": ["a", "b"],
		"metadata": {"key": "val"}
	}`)

	row := map[string]any{}
	ExtractFlatColumns(data, nil, row)

	if row["name"] != "Widget" {
		t.Errorf("expected name=Widget, got %v", row["name"])
	}
	if row["price"] != 9.99 {
		t.Errorf("expected price=9.99, got %v", row["price"])
	}
	if row["is_active"] != true {
		t.Errorf("expected is_active=true, got %v", row["is_active"])
	}

	// @-prefixed keys should be excluded
	for _, key := range []string{"@id", "@type", "@context"} {
		if _, ok := row[key]; ok {
			t.Errorf("key %q should have been excluded", key)
		}
	}

	// Complex types should be JSON strings
	if _, ok := row["tags"]; !ok {
		t.Error("expected tags column")
	}
	if _, ok := row["metadata"]; !ok {
		t.Error("expected metadata column")
	}
}

func newTestDBWithTypes(t *testing.T) *gorm.DB {
	t.Helper()
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.ResourceType{}); err != nil {
		t.Fatalf("failed to migrate resource_types: %v", err)
	}
	return db
}

func TestAncestorSlugs_SingleLevel(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	parentCtx := json.RawMessage(`{"@vocab":"https://schema.org/","weos:abstract":true}`)
	childCtx := json.RawMessage(`{"@vocab":"https://schema.org/","rdfs:subClassOf":"financial-instrument"}`)

	if err := pm.EnsureTable(ctx, "financial-instrument", nil, parentCtx); err != nil {
		t.Fatalf("EnsureTable parent: %v", err)
	}
	if err := pm.EnsureTable(ctx, "loan", nil, childCtx); err != nil {
		t.Fatalf("EnsureTable child: %v", err)
	}

	ancestors := pm.AncestorSlugs("loan")
	if len(ancestors) != 1 || ancestors[0] != "financial-instrument" {
		t.Fatalf("AncestorSlugs(loan) = %v, want [financial-instrument]", ancestors)
	}
}

func TestAncestorSlugs_MultiLevel(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	ctxC := json.RawMessage(`{"@vocab":"https://schema.org/"}`)
	ctxB := json.RawMessage(`{"@vocab":"https://schema.org/","rdfs:subClassOf":"type-c"}`)
	ctxA := json.RawMessage(`{"@vocab":"https://schema.org/","rdfs:subClassOf":"type-b"}`)

	if err := pm.EnsureTable(ctx, "type-c", nil, ctxC); err != nil {
		t.Fatal(err)
	}
	if err := pm.EnsureTable(ctx, "type-b", nil, ctxB); err != nil {
		t.Fatal(err)
	}
	if err := pm.EnsureTable(ctx, "type-a", nil, ctxA); err != nil {
		t.Fatal(err)
	}

	ancestors := pm.AncestorSlugs("type-a")
	if len(ancestors) != 2 || ancestors[0] != "type-b" || ancestors[1] != "type-c" {
		t.Fatalf("AncestorSlugs(type-a) = %v, want [type-b, type-c]", ancestors)
	}
}

func TestAncestorSlugs_NoParent(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	if err := pm.EnsureTable(ctx, "product", nil, nil); err != nil {
		t.Fatal(err)
	}

	ancestors := pm.AncestorSlugs("product")
	if len(ancestors) != 0 {
		t.Fatalf("AncestorSlugs(product) = %v, want nil", ancestors)
	}
}

func TestAncestorSlugs_CircularBreaks(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	ctxA := json.RawMessage(`{"@vocab":"https://schema.org/","rdfs:subClassOf":"type-b"}`)
	ctxB := json.RawMessage(`{"@vocab":"https://schema.org/","rdfs:subClassOf":"type-a"}`)

	if err := pm.EnsureTable(ctx, "type-a", nil, ctxA); err != nil {
		t.Fatal(err)
	}
	if err := pm.EnsureTable(ctx, "type-b", nil, ctxB); err != nil {
		t.Fatal(err)
	}

	// Should not infinite loop — visited set breaks the cycle.
	ancestors := pm.AncestorSlugs("type-a")
	if len(ancestors) != 1 || ancestors[0] != "type-b" {
		t.Fatalf("AncestorSlugs(type-a) = %v, want [type-b]", ancestors)
	}
}

func TestEnsureTable_EachTypeGetsOwnTable(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	abstractCtx := json.RawMessage(`{"@vocab":"https://schema.org/","weos:abstract":true}`)
	parentSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	childCtx := json.RawMessage(`{"@vocab":"https://schema.org/","rdfs:subClassOf":"instrument"}`)
	childSchema := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"},"interestRate":{"type":"number"}}}`)

	if err := pm.EnsureTable(ctx, "instrument", parentSchema, abstractCtx); err != nil {
		t.Fatal(err)
	}
	if err := pm.EnsureTable(ctx, "loan", childSchema, childCtx); err != nil {
		t.Fatal(err)
	}

	// Both types should have their OWN tables (not shared).
	if pm.TableName("instrument") != "instruments" {
		t.Fatalf("TableName(instrument) = %q", pm.TableName("instrument"))
	}
	if pm.TableName("loan") != "loans" {
		t.Fatalf("TableName(loan) = %q, want loans", pm.TableName("loan"))
	}

	// Verify both tables exist and have their own columns.
	if err := db.Exec(`INSERT INTO instruments (id, type_slug, status, name)
		VALUES ('i1', 'instrument', 'active', 'Test')`).Error; err != nil {
		t.Fatalf("insert into instruments: %v", err)
	}
	if err := db.Exec(`INSERT INTO loans (id, type_slug, status, name, interest_rate)
		VALUES ('l1', 'loan', 'active', 'Home Loan', 3.5)`).Error; err != nil {
		t.Fatalf("insert into loans: %v", err)
	}
}

func TestEnsureExistingTables_AllTypesGetTables(t *testing.T) {
	t.Parallel()
	db := newTestDBWithTypes(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	db.Create(&models.ResourceType{
		ID: "1", Name: "Shape", Slug: "shape", Status: "active",
		Context: `{"@vocab":"https://schema.org/","weos:abstract":true}`,
		Schema:  `{"type":"object","properties":{"color":{"type":"string"}}}`,
	})
	db.Create(&models.ResourceType{
		ID: "2", Name: "Circle", Slug: "circle", Status: "active",
		Context: `{"@vocab":"https://schema.org/","rdfs:subClassOf":"shape"}`,
		Schema:  `{"type":"object","properties":{"color":{"type":"string"},"radius":{"type":"number"}}}`,
	})

	if err := pm.EnsureExistingTables(ctx); err != nil {
		t.Fatal(err)
	}

	if !pm.HasProjectionTable("shape") {
		t.Fatal("shape should have projection table")
	}
	if !pm.HasProjectionTable("circle") {
		t.Fatal("circle should have its own projection table")
	}

	// Verify ancestor chain.
	ancestors := pm.AncestorSlugs("circle")
	if len(ancestors) != 1 || ancestors[0] != "shape" {
		t.Fatalf("AncestorSlugs(circle) = %v, want [shape]", ancestors)
	}
}

func TestEnsureExistingTables_SkipsDeletedTypes(t *testing.T) {
	t.Parallel()
	db := newTestDBWithTypes(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	now := time.Now()
	db.Create(&models.ResourceType{
		ID: "1", Name: "Deleted", Slug: "deleted-type", Status: "active",
		Schema:    `{"type":"object","properties":{"name":{"type":"string"}}}`,
		DeletedAt: &now,
	})

	if err := pm.EnsureExistingTables(ctx); err != nil {
		t.Fatal(err)
	}

	if pm.HasProjectionTable("deleted-type") {
		t.Fatal("deleted type should not have a projection table")
	}
}
