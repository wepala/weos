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

func TestRegisterSubtype_MergesColumnsIntoParent(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	// Create abstract parent projection table with a "name" column.
	parentSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	if err := pm.EnsureTable(ctx, "financial-instrument", parentSchema, nil); err != nil {
		t.Fatalf("EnsureTable for parent failed: %v", err)
	}

	// Register "loan" as subtype with extra "interestRate" column.
	childSchema := json.RawMessage(`{"type":"object","properties":{
		"name":{"type":"string"},
		"interestRate":{"type":"number"}
	}}`)
	if err := pm.RegisterSubtype(ctx, "loan", "financial-instrument", childSchema, nil); err != nil {
		t.Fatalf("RegisterSubtype failed: %v", err)
	}

	// Verify the child column was added to the parent table.
	err := db.Exec(`INSERT INTO financial_instruments (id, type_slug, status, name, interest_rate)
		VALUES ('loan-1', 'loan', 'active', 'Home Loan', 3.5)`).Error
	if err != nil {
		t.Fatalf("insert with child column failed: %v", err)
	}

	var result map[string]any
	err = db.Table("financial_instruments").Where("id = ?", "loan-1").Take(&result).Error
	if err != nil {
		t.Fatalf("read back failed: %v", err)
	}
}

func TestIsSubtype(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	if err := pm.EnsureTable(ctx, "commitment", nil, nil); err != nil {
		t.Fatalf("EnsureTable failed: %v", err)
	}
	if err := pm.RegisterSubtype(ctx, "invoice", "commitment", nil, nil); err != nil {
		t.Fatalf("RegisterSubtype failed: %v", err)
	}

	if !pm.IsSubtype("invoice") {
		t.Fatal("expected invoice to be a subtype")
	}
	if pm.IsSubtype("commitment") {
		t.Fatal("expected commitment NOT to be a subtype")
	}
	if pm.IsSubtype("unknown") {
		t.Fatal("expected unknown NOT to be a subtype")
	}
}

func TestParentSlug(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	if err := pm.EnsureTable(ctx, "commitment", nil, nil); err != nil {
		t.Fatalf("EnsureTable failed: %v", err)
	}
	if err := pm.RegisterSubtype(ctx, "invoice", "commitment", nil, nil); err != nil {
		t.Fatalf("RegisterSubtype failed: %v", err)
	}

	if got := pm.ParentSlug("invoice"); got != "commitment" {
		t.Fatalf("ParentSlug(invoice) = %q, want %q", got, "commitment")
	}
	if got := pm.ParentSlug("commitment"); got != "" {
		t.Fatalf("ParentSlug(commitment) = %q, want empty", got)
	}
}

func TestSubtype_TableNameResolvesToParent(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	if err := pm.EnsureTable(ctx, "financial-instrument", nil, nil); err != nil {
		t.Fatalf("EnsureTable failed: %v", err)
	}
	if err := pm.RegisterSubtype(ctx, "loan", "financial-instrument", nil, nil); err != nil {
		t.Fatalf("RegisterSubtype failed: %v", err)
	}

	// TableName for the subtype should resolve to the parent's table.
	if got := pm.TableName("loan"); got != "financial_instruments" {
		t.Fatalf("TableName(loan) = %q, want %q", got, "financial_instruments")
	}
	// Parent's own TableName should still work.
	if got := pm.TableName("financial-instrument"); got != "financial_instruments" {
		t.Fatalf("TableName(financial-instrument) = %q, want %q", got, "financial_instruments")
	}
}

func TestSubtype_HasProjectionTable(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	if err := pm.EnsureTable(ctx, "commitment", nil, nil); err != nil {
		t.Fatalf("EnsureTable failed: %v", err)
	}
	if err := pm.RegisterSubtype(ctx, "invoice", "commitment", nil, nil); err != nil {
		t.Fatalf("RegisterSubtype failed: %v", err)
	}

	if !pm.HasProjectionTable("invoice") {
		t.Fatal("expected HasProjectionTable(invoice) = true")
	}
	if !pm.HasProjectionTable("commitment") {
		t.Fatal("expected HasProjectionTable(commitment) = true")
	}
}

func TestSubtype_MultipleChildrenShareParentTable(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	parentSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	if err := pm.EnsureTable(ctx, "financial-instrument", parentSchema, nil); err != nil {
		t.Fatalf("EnsureTable failed: %v", err)
	}

	loanSchema := json.RawMessage(`{"type":"object","properties":{
		"name":{"type":"string"},"interestRate":{"type":"number"}}}`)
	depositSchema := json.RawMessage(`{"type":"object","properties":{
		"name":{"type":"string"},"balance":{"type":"number"}}}`)

	if err := pm.RegisterSubtype(ctx, "loan", "financial-instrument", loanSchema, nil); err != nil {
		t.Fatalf("RegisterSubtype(loan) failed: %v", err)
	}
	if err := pm.RegisterSubtype(ctx, "deposit-account", "financial-instrument", depositSchema, nil); err != nil {
		t.Fatalf("RegisterSubtype(deposit-account) failed: %v", err)
	}

	// Insert rows for both subtypes in the same table.
	err := db.Exec(`INSERT INTO financial_instruments (id, type_slug, status, name, interest_rate)
		VALUES ('loan-1', 'loan', 'active', 'Home Loan', 3.5)`).Error
	if err != nil {
		t.Fatalf("insert loan failed: %v", err)
	}
	err = db.Exec(`INSERT INTO financial_instruments (id, type_slug, status, name, balance)
		VALUES ('dep-1', 'deposit-account', 'active', 'Savings', 1000.0)`).Error
	if err != nil {
		t.Fatalf("insert deposit failed: %v", err)
	}

	// Both should be in the same table with different type_slug values.
	var count int64
	db.Table("financial_instruments").Count(&count)
	if count != 2 {
		t.Fatalf("expected 2 rows in financial_instruments, got %d", count)
	}

	// Filter by type_slug = 'loan' should return 1.
	var loanCount int64
	db.Table("financial_instruments").Where("type_slug = ?", "loan").Count(&loanCount)
	if loanCount != 1 {
		t.Fatalf("expected 1 loan row, got %d", loanCount)
	}

	// Filter by type_slug = 'deposit-account' should return 1.
	var depCount int64
	db.Table("financial_instruments").Where("type_slug = ?", "deposit-account").Count(&depCount)
	if depCount != 1 {
		t.Fatalf("expected 1 deposit-account row, got %d", depCount)
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

func TestEnsureExistingTables_TwoPassOrdering(t *testing.T) {
	t.Parallel()
	db := newTestDBWithTypes(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	abstractCtx := `{"@vocab":"https://schema.org/","weos:abstract":true}`
	childCtx := `{"@vocab":"https://schema.org/","rdfs:subClassOf":"instrument"}`

	// Insert types: child BEFORE parent to test ordering resilience.
	db.Create(&models.ResourceType{
		ID: "child-1", Name: "Loan", Slug: "loan", Status: "active",
		Context: childCtx,
		Schema:  `{"type":"object","properties":{"interestRate":{"type":"number"}}}`,
	})
	db.Create(&models.ResourceType{
		ID: "parent-1", Name: "Instrument", Slug: "instrument", Status: "active",
		Context: abstractCtx,
		Schema:  `{"type":"object","properties":{"name":{"type":"string"}}}`,
	})

	if err := pm.EnsureExistingTables(ctx); err != nil {
		t.Fatalf("EnsureExistingTables failed: %v", err)
	}

	// Abstract parent should have its own table.
	if !pm.HasProjectionTable("instrument") {
		t.Fatal("expected instrument to have a projection table")
	}

	// Child should be registered as a subtype.
	if !pm.IsSubtype("loan") {
		t.Fatal("expected loan to be registered as subtype")
	}
	if pm.ParentSlug("loan") != "instrument" {
		t.Fatalf("expected loan's parent to be 'instrument', got %q", pm.ParentSlug("loan"))
	}

	// Verify child columns were merged into parent table.
	err := db.Exec(`INSERT INTO instruments (id, type_slug, status, interest_rate)
		VALUES ('l1', 'loan', 'active', 5.5)`).Error
	if err != nil {
		t.Fatalf("insert with child column failed: %v", err)
	}
}

func TestEnsureExistingTables_OrphanedSubClassOf(t *testing.T) {
	t.Parallel()
	db := newTestDBWithTypes(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	// Child references a parent that doesn't exist in the DB.
	orphanCtx := `{"@vocab":"https://schema.org/","rdfs:subClassOf":"nonexistent"}`
	db.Create(&models.ResourceType{
		ID: "orphan-1", Name: "Orphan", Slug: "orphan", Status: "active",
		Context: orphanCtx,
		Schema:  `{"type":"object","properties":{"name":{"type":"string"}}}`,
	})

	// Should not error — orphan gets its own standalone table.
	if err := pm.EnsureExistingTables(ctx); err != nil {
		t.Fatalf("EnsureExistingTables failed: %v", err)
	}

	if !pm.HasProjectionTable("orphan") {
		t.Fatal("orphan should have its own projection table")
	}
	if pm.IsSubtype("orphan") {
		t.Fatal("orphan should NOT be registered as a subtype")
	}
}

func TestEnsureExistingTables_AbstractGetsOwnTable(t *testing.T) {
	t.Parallel()
	db := newTestDBWithTypes(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	abstractCtx := `{"@vocab":"https://schema.org/","weos:abstract":true}`
	db.Create(&models.ResourceType{
		ID: "abs-1", Name: "Shape", Slug: "shape", Status: "active",
		Context: abstractCtx,
		Schema:  `{"type":"object","properties":{"color":{"type":"string"}}}`,
	})

	if err := pm.EnsureExistingTables(ctx); err != nil {
		t.Fatalf("EnsureExistingTables failed: %v", err)
	}

	if !pm.HasProjectionTable("shape") {
		t.Fatal("abstract type 'shape' should have its own projection table")
	}

	// Verify table was actually created.
	err := db.Exec(`INSERT INTO shapes (id, type_slug, status, color)
		VALUES ('s1', 'shape', 'active', 'red')`).Error
	if err != nil {
		t.Fatalf("insert into shapes failed: %v", err)
	}
}

func TestCircularSubClassOf_NoPanic(t *testing.T) {
	t.Parallel()
	db := newTestDBWithTypes(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}

	// Create circular reference: A -> B -> A, both abstract.
	db.Create(&models.ResourceType{
		ID: "a-1", Name: "TypeA", Slug: "type-a", Status: "active",
		Context: `{"@vocab":"https://schema.org/","weos:abstract":true,"rdfs:subClassOf":"type-b"}`,
		Schema:  `{"type":"object","properties":{"fieldA":{"type":"string"}}}`,
	})
	db.Create(&models.ResourceType{
		ID: "b-1", Name: "TypeB", Slug: "type-b", Status: "active",
		Context: `{"@vocab":"https://schema.org/","weos:abstract":true,"rdfs:subClassOf":"type-a"}`,
		Schema:  `{"type":"object","properties":{"fieldB":{"type":"string"}}}`,
	})

	// Should not stack overflow. HasProjectionTable should return false or true
	// but must not panic.
	_ = pm.HasProjectionTable("type-a")
	_ = pm.HasProjectionTable("type-b")

	// Also test via EnsureExistingTables which processes all types.
	ctx := context.Background()
	if err := pm.EnsureExistingTables(ctx); err != nil {
		t.Fatalf("EnsureExistingTables with circular refs should not error: %v", err)
	}
}

func TestRegisterSubtype_FailsWithoutParentTable(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	childSchema := json.RawMessage(`{"type":"object","properties":{"rate":{"type":"number"}}}`)
	err := pm.RegisterSubtype(ctx, "loan", "nonexistent-parent", childSchema, nil)
	if err == nil {
		t.Fatal("expected error when parent table doesn't exist")
	}
}
