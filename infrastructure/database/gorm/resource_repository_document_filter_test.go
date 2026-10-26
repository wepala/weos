package gorm

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/models"
)

// TestFindAllWithFilters_DocumentProperty pins the condition the MCP
// resource_list tool sends for a schema property a standard column shadows: a
// task's own "status" lives only in the stored document, never in the
// projection's lifecycle status column.
func TestFindAllWithFilters_DocumentProperty(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Resource{}, &models.ResourcePermission{}); err != nil {
		t.Fatalf("migrate resources: %v", err)
	}
	pm := &projectionManager{db: db, logger: &testLogger{}}
	repo := &ResourceRepository{db: db, projMgr: pm, logger: &testLogger{}}
	ctx := t.Context()
	schema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"status":{"type":"string"}}}`)
	if err := pm.EnsureTable(ctx, "task", schema, nil); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct{ id, data string }{
		{"urn:task:1", `{"@graph":[{"@id":"urn:task:1","name":"Chase invoices","status":"open"}]}`},
		{"urn:task:2", `{"@graph":[{"@id":"urn:task:2","name":"Archive contracts","status":"done"}]}`},
		{"urn:task:3", `{"name":"Renew lease","status":"open"}`}, // a legacy flat record
	} {
		if err := repo.Save(ctx, makeTestResource(t, r.id, "task", r.data)); err != nil {
			t.Fatalf("save %s: %v", r.id, err)
		}
	}

	open := []repositories.FilterCondition{
		{Field: "status", Operator: "eq", Value: "open", DocumentProperty: true},
	}
	page, err := repo.FindAllByTypeWithFilters(ctx, "task", open, "", 10, repositories.SortOptions{}, nil)
	if err != nil {
		t.Fatalf("filtered list: %v", err)
	}
	got := make([]string, 0, len(page.Data))
	for _, e := range page.Data {
		got = append(got, e.GetID())
	}
	sort.Strings(got)
	if len(got) != 2 || got[0] != "urn:task:1" || got[1] != "urn:task:3" {
		t.Errorf("document status=open matched %v, want [urn:task:1 urn:task:3]", got)
	}

	flat, err := repo.FindAllByTypeFlatWithFilters(ctx, "task", open, "", 10, repositories.SortOptions{}, nil)
	if err != nil {
		t.Fatalf("filtered flat list: %v", err)
	}
	if len(flat.Data) != 2 {
		t.Errorf("flat document status=open matched %d rows, want 2", len(flat.Data))
	}

	// Without the mark, the same condition still targets the lifecycle column,
	// which is what the REST listing sends.
	column := []repositories.FilterCondition{{Field: "status", Operator: "eq", Value: "open"}}
	page, err = repo.FindAllByTypeWithFilters(ctx, "task", column, "", 10, repositories.SortOptions{}, nil)
	if err != nil {
		t.Fatalf("column list: %v", err)
	}
	if len(page.Data) != 0 {
		t.Errorf("column status=open matched %d resources, want 0", len(page.Data))
	}
}
