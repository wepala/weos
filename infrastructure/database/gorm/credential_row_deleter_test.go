package gorm

import (
	"context"
	"testing"
)

func TestCredentialRowDeleterDeletesOnlyTheNamedRow(t *testing.T) {
	ctx := context.Background()
	db := newFeatureTestDB(t)
	if err := db.Exec(`CREATE TABLE credentials (
		id TEXT PRIMARY KEY, agent_id TEXT NOT NULL, provider TEXT NOT NULL,
		provider_user_id TEXT NOT NULL, email TEXT, active NUMERIC NOT NULL DEFAULT 1)`).Error; err != nil {
		t.Fatalf("create credentials: %v", err)
	}
	for _, id := range []string{"cred-linked", "cred-kept"} {
		if err := db.Exec(`INSERT INTO credentials (id, agent_id, provider, provider_user_id, email) VALUES (?, 'agent-dana', 'apple', ?, 'dana.whitfield@harborlegal.example')`,
			id, "sub-"+id).Error; err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	deleter := ProvideCredentialRowDeleter(db)

	if err := deleter.DeleteCredentialRow(ctx, "cred-linked"); err != nil {
		t.Fatalf("DeleteCredentialRow: %v", err)
	}
	var ids []string
	if err := db.Table("credentials").Order("id").Pluck("id", &ids).Error; err != nil {
		t.Fatalf("read the rows left: %v", err)
	}
	if len(ids) != 1 || ids[0] != "cred-kept" {
		t.Fatalf("rows left = %v, want only cred-kept", ids)
	}

	// A row that is already gone is not an error: a retried take-back succeeds.
	if err := deleter.DeleteCredentialRow(ctx, "cred-linked"); err != nil {
		t.Fatalf("deleting a row that is gone: %v", err)
	}
	if err := deleter.DeleteCredentialRow(ctx, ""); err == nil {
		t.Fatalf("an empty id was accepted, which could only ever match the wrong rows")
	}
}

func TestCredentialRowDeleterReportsADatabaseFailure(t *testing.T) {
	db := newFeatureTestDB(t) // no credentials table: every delete fails
	if err := ProvideCredentialRowDeleter(db).DeleteCredentialRow(context.Background(), "cred-linked"); err == nil {
		t.Fatalf("a delete against a missing table reported success")
	}
}
