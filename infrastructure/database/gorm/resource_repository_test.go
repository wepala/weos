package gorm

import (
	"context"
	"encoding/json"
	"testing"

	"weos/domain/repositories"
)

func TestFindAllFlatFromProjection_TypeSlugFiltering(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	pm := &projectionManager{db: db, logger: &testLogger{}}
	ctx := context.Background()

	// Create abstract parent with "name" column.
	parentSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	abstractCtx := json.RawMessage(`{"@vocab":"https://schema.org/","weos:abstract":true}`)
	if err := pm.EnsureTable(ctx, "instrument", parentSchema, abstractCtx); err != nil {
		t.Fatalf("EnsureTable parent: %v", err)
	}

	// Register two subtypes.
	loanSchema := json.RawMessage(
		`{"type":"object","properties":{"name":{"type":"string"},"interestRate":{"type":"number"}}}`)
	if err := pm.RegisterSubtype(ctx, "loan", "instrument", loanSchema, nil); err != nil {
		t.Fatalf("RegisterSubtype loan: %v", err)
	}
	depositSchema := json.RawMessage(
		`{"type":"object","properties":{"name":{"type":"string"},"minBalance":{"type":"number"}}}`)
	if err := pm.RegisterSubtype(ctx, "deposit", "instrument", depositSchema, nil); err != nil {
		t.Fatalf("RegisterSubtype deposit: %v", err)
	}

	// Insert rows with different type_slug values.
	if err := db.Exec(`INSERT INTO instruments (id, type_slug, status, name, interest_rate)
		VALUES ('loan-1', 'loan', 'active', 'Home Loan', 3.5)`).Error; err != nil {
		t.Fatalf("insert loan-1 fixture: %v", err)
	}
	if err := db.Exec(`INSERT INTO instruments (id, type_slug, status, name, interest_rate)
		VALUES ('loan-2', 'loan', 'active', 'Car Loan', 5.0)`).Error; err != nil {
		t.Fatalf("insert loan-2 fixture: %v", err)
	}
	if err := db.Exec(`INSERT INTO instruments (id, type_slug, status, name, min_balance)
		VALUES ('dep-1', 'deposit', 'active', 'Savings', 100)`).Error; err != nil {
		t.Fatalf("insert dep-1 fixture: %v", err)
	}

	repo := &ResourceRepository{db: db, projMgr: pm}

	// Query for loans only.
	loanResult, err := repo.findAllFlatFromProjection(
		ctx, "loan", nil, "", 20,
		repositories.SortOptions{}, nil,
	)
	if err != nil {
		t.Fatalf("findAllFlatFromProjection(loan) failed: %v", err)
	}
	if len(loanResult.Data) != 2 {
		t.Fatalf("expected 2 loan rows, got %d", len(loanResult.Data))
	}

	// Query for deposits only.
	depositResult, err := repo.findAllFlatFromProjection(
		ctx, "deposit", nil, "", 20,
		repositories.SortOptions{}, nil,
	)
	if err != nil {
		t.Fatalf("findAllFlatFromProjection(deposit) failed: %v", err)
	}
	if len(depositResult.Data) != 1 {
		t.Fatalf("expected 1 deposit row, got %d", len(depositResult.Data))
	}
}
