package gorm

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"weos/domain/entities"
	"weos/infrastructure/models"
)

func setupDualProjectionTest(t *testing.T) (
	*ResourceRepository, *projectionManager, context.Context,
) {
	t.Helper()
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Resource{}); err != nil {
		t.Fatalf("migrate resources: %v", err)
	}
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	parentCtx := json.RawMessage(`{"@vocab":"https://schema.org/","weos:abstract":true}`)
	parentSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	childCtx := json.RawMessage(
		`{"@vocab":"https://schema.org/","rdfs:subClassOf":"instrument"}`)
	childSchema := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"},"interestRate":{"type":"number"}}}`)

	if err := pm.EnsureTable(ctx, "instrument", parentSchema, parentCtx); err != nil {
		t.Fatal(err)
	}
	if err := pm.EnsureTable(ctx, "loan", childSchema, childCtx); err != nil {
		t.Fatal(err)
	}

	repo := &ResourceRepository{db: db, projMgr: pm}
	return repo, pm, ctx
}

func makeTestResource(t *testing.T, id, typeSlug, dataJSON string) *entities.Resource {
	t.Helper()
	e := &entities.Resource{}
	if err := e.Restore(
		id, typeSlug, "active",
		json.RawMessage(dataJSON),
		"user-1", "acct-1",
		time.Now(), 1,
	); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	return e
}

func TestDualProjection_SavePopulatesBothTables(t *testing.T) {
	t.Parallel()
	repo, _, ctx := setupDualProjectionTest(t)

	entity := makeTestResource(t, "urn:loan:001", "loan",
		`{"name":"Home Loan","interestRate":3.5}`)

	if err := repo.Save(ctx, entity); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify loan exists in its own table.
	var loanCount int64
	repo.db.Table("loans").Count(&loanCount)
	if loanCount != 1 {
		t.Fatalf("expected 1 row in loans, got %d", loanCount)
	}

	// Verify loan also projected into ancestor table.
	var instrCount int64
	repo.db.Table("instruments").Count(&instrCount)
	if instrCount != 1 {
		t.Fatalf("expected 1 row in instruments, got %d", instrCount)
	}

	// Verify ancestor row has parent-schema columns but NOT child-only columns.
	var instrRow map[string]any
	repo.db.Table("instruments").Where("id = ?", "urn:loan:001").Take(&instrRow)
	if instrRow["name"] != "Home Loan" {
		t.Fatalf("ancestor name = %v, want 'Home Loan'", instrRow["name"])
	}
	// interest_rate column should NOT exist in ancestor table.
	if repo.db.Migrator().HasColumn("instruments", "interest_rate") {
		t.Fatal("ancestor table should NOT have child-specific interest_rate column")
	}
}

func TestDualProjection_UpdatePropagates(t *testing.T) {
	t.Parallel()
	repo, _, ctx := setupDualProjectionTest(t)

	entity := makeTestResource(t, "urn:loan:002", "loan",
		`{"name":"Car Loan","interestRate":5.0}`)
	if err := repo.Save(ctx, entity); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Update the resource with new data.
	updated := makeTestResource(t, "urn:loan:002", "loan",
		`{"name":"Updated Car Loan","interestRate":4.5}`)
	if err := repo.Update(ctx, updated); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Verify ancestor table has updated name.
	var instrRow map[string]any
	repo.db.Table("instruments").Where("id = ?", "urn:loan:002").Take(&instrRow)
	if instrRow["name"] != "Updated Car Loan" {
		t.Fatalf("ancestor name = %v, want 'Updated Car Loan'", instrRow["name"])
	}
}

func TestDualProjection_DeleteRemovesFromBothTables(t *testing.T) {
	t.Parallel()
	repo, _, ctx := setupDualProjectionTest(t)

	entity := makeTestResource(t, "urn:loan:003", "loan",
		`{"name":"Delete Me","interestRate":2.0}`)
	if err := repo.Save(ctx, entity); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify rows exist in both tables before delete.
	var loanCount, instrCount int64
	repo.db.Table("loans").Count(&loanCount)
	repo.db.Table("instruments").Count(&instrCount)
	if loanCount != 1 || instrCount != 1 {
		t.Fatalf("pre-delete: loans=%d instruments=%d, want 1,1", loanCount, instrCount)
	}

	if err := repo.Delete(ctx, "urn:loan:003"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify rows removed from both tables.
	repo.db.Table("loans").Count(&loanCount)
	repo.db.Table("instruments").Count(&instrCount)
	if loanCount != 0 {
		t.Fatalf("post-delete: loans=%d, want 0", loanCount)
	}
	if instrCount != 0 {
		t.Fatalf("post-delete: instruments=%d, want 0", instrCount)
	}
}

func TestDualProjection_AncestorUsesOwnSchema(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	parentCtx := json.RawMessage(`{"@vocab":"https://schema.org/","weos:abstract":true}`)
	parentSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	childCtx := json.RawMessage(
		`{"@vocab":"https://schema.org/","rdfs:subClassOf":"instrument"}`)
	childSchema := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string"},"interestRate":{"type":"number"}}}`)

	if err := pm.EnsureTable(ctx, "instrument", parentSchema, parentCtx); err != nil {
		t.Fatal(err)
	}
	if err := pm.EnsureTable(ctx, "loan", childSchema, childCtx); err != nil {
		t.Fatal(err)
	}

	// Ancestor table: has "name" but NOT "interest_rate".
	if db.Migrator().HasColumn("instruments", "interest_rate") {
		t.Fatal("ancestor table should NOT have child-specific column")
	}
	if !db.Migrator().HasColumn("instruments", "name") {
		t.Fatal("ancestor table should have 'name' from its own schema")
	}

	// Child table: has both.
	if !db.Migrator().HasColumn("loans", "interest_rate") {
		t.Fatal("child table should have interest_rate")
	}
	if !db.Migrator().HasColumn("loans", "name") {
		t.Fatal("child table should have name")
	}
}
