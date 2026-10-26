package gorm

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/models"
)

// newDocumentFilterRepo migrates the tables a filtered listing reads, makes
// the type's projection table and saves the records, each an id and its data.
func newDocumentFilterRepo(
	t *testing.T, db *gorm.DB, slug, schema string, records [][2]string,
) *ResourceRepository {
	t.Helper()
	if err := db.AutoMigrate(&models.Resource{}, &models.ResourcePermission{}); err != nil {
		t.Fatalf("migrate resources: %v", err)
	}
	pm := &projectionManager{db: db, logger: &testLogger{}}
	repo := &ResourceRepository{db: db, projMgr: pm, logger: &testLogger{}}
	var raw json.RawMessage
	if schema != "" {
		raw = json.RawMessage(schema)
	}
	if err := pm.EnsureTable(t.Context(), slug, raw, nil); err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		if err := repo.Save(t.Context(), makeTestResource(t, r[0], slug, r[1])); err != nil {
			t.Fatalf("save %s: %v", r[0], err)
		}
	}
	return repo
}

// listIDs runs one filtered listing, entity or flat, and returns the sorted
// identifiers it matched.
func listIDs(
	t *testing.T, repo *ResourceRepository, slug string, flat bool, filters ...repositories.FilterCondition,
) []string {
	t.Helper()
	got := []string{}
	if flat {
		page, err := repo.FindAllByTypeFlatWithFilters(
			t.Context(), slug, filters, "", 50, repositories.SortOptions{}, nil)
		if err != nil {
			t.Fatalf("filtered flat list: %v", err)
		}
		for _, row := range page.Data {
			got = append(got, fmt.Sprint(row["id"]))
		}
	} else {
		page, err := repo.FindAllByTypeWithFilters(
			t.Context(), slug, filters, "", 50, repositories.SortOptions{}, nil)
		if err != nil {
			t.Fatalf("filtered list: %v", err)
		}
		for _, e := range page.Data {
			got = append(got, e.GetID())
		}
	}
	sort.Strings(got)
	return got
}

// TestFindAllWithFilters_DocumentProperty pins the condition the MCP
// resource_list tool sends for a schema property a standard column shadows: a
// task's own "status" lives only in the stored document, never in the
// projection's lifecycle status column.
func TestFindAllWithFilters_DocumentProperty(t *testing.T) {
	t.Parallel()
	repo := newDocumentFilterRepo(t, newTestDB(t), "task",
		`{"type":"object","properties":{"name":{"type":"string"},"status":{"type":"string"}}}`,
		[][2]string{
			{"urn:task:1", `{"@graph":[{"@id":"urn:task:1","name":"Chase invoices","status":"open"}]}`},
			{"urn:task:2", `{"@graph":[{"@id":"urn:task:2","name":"Archive contracts","status":"done"}]}`},
			{"urn:task:3", `{"name":"Renew lease","status":"open"}`}, // a legacy flat record
		})

	open := repositories.FilterCondition{Field: "status", Operator: "eq", Value: "open", DocumentProperty: true}
	for _, flat := range []bool{false, true} {
		if got := listIDs(t, repo, "task", flat, open); strings.Join(got, ",") != "urn:task:1,urn:task:3" {
			t.Errorf("flat=%v: document status=open matched %v, want [urn:task:1 urn:task:3]", flat, got)
		}
	}

	// Without the mark, the same condition still targets the lifecycle column,
	// which is what the REST listing sends.
	column := repositories.FilterCondition{Field: "status", Operator: "eq", Value: "open"}
	if got := listIDs(t, repo, "task", false, column); len(got) != 0 {
		t.Errorf("column status=open matched %v, want none", got)
	}
}

// documentTypedSchema declares an integer and a boolean property whose names
// standard columns shadow, so both live only in the stored document.
const documentTypedSchema = `{"type":"object","properties":{"name":{"type":"string"},` +
	`"sequenceNo":{"type":"integer"},"status":{"type":"boolean"}}}`

// documentTypedRecords returns four questions under slug: three that carry a
// sequence number and a flag, and one that carries neither.
func documentTypedRecords(slug string) [][2]string {
	id := func(n int) string { return fmt.Sprintf("urn:%s:%d", slug, n) }
	return [][2]string{
		{id(1), fmt.Sprintf(`{"@graph":[{"@id":%q,"name":"Fractions","sequenceNo":3,"status":true}]}`, id(1))},
		{id(2), fmt.Sprintf(`{"@graph":[{"@id":%q,"name":"Decimals","sequenceNo":7,"status":false}]}`, id(2))},
		{id(3), `{"name":"Ratios","sequenceNo":12,"status":true}`}, // a legacy flat record
		{id(4), fmt.Sprintf(`{"@graph":[{"@id":%q,"name":"Percentages"}]}`, id(4))},
	}
}

type documentFilterCase struct {
	name string
	cond repositories.FilterCondition
	want []string
}

// documentTypedCases are the comparisons a stored number and boolean must
// answer in their JSON type. A text comparison fails every one: "3" is not 3,
// "12" sorts before "3", and true is not "1".
func documentTypedCases(slug string) []documentFilterCase {
	id := func(ns ...int) []string {
		out := make([]string, 0, len(ns))
		for _, n := range ns {
			out = append(out, fmt.Sprintf("urn:%s:%d", slug, n))
		}
		return out
	}
	num := func(op, v string) repositories.FilterCondition {
		return repositories.FilterCondition{Field: "sequenceNo", Operator: op, Value: v,
			DocumentProperty: true, ValueType: repositories.FilterValueNumber}
	}
	flag := func(op, v string) repositories.FilterCondition {
		return repositories.FilterCondition{Field: "status", Operator: op, Value: v,
			DocumentProperty: true, ValueType: repositories.FilterValueBoolean}
	}
	withMissing := func(c repositories.FilterCondition) repositories.FilterCondition {
		c.NeIncludesMissing = true
		return c
	}
	return []documentFilterCase{
		{"integer eq", num("eq", "3"), id(1)},
		{"integer gt", num("gt", "3"), id(2, 3)},
		{"integer lte", num("lte", "7"), id(1, 2)},
		{"integer ne", num("ne", "3"), id(2, 3)},
		{"integer ne including missing", withMissing(num("ne", "3")), id(2, 3, 4)},
		{"boolean eq true", flag("eq", "1"), id(1, 3)},
		{"boolean eq false", flag("eq", "0"), id(2)},
		{"boolean ne true including missing", withMissing(flag("ne", "1")), id(2, 4)},
	}
}

func runDocumentFilterCases(t *testing.T, repo *ResourceRepository, slug string, cases []documentFilterCase) {
	t.Helper()
	for _, tc := range cases {
		for _, flat := range []bool{false, true} {
			if got := listIDs(t, repo, slug, flat, tc.cond); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("%s (flat=%v): matched %v, want %v", tc.name, flat, got, tc.want)
			}
		}
	}
}

// TestFindAllWithFilters_DocumentPropertyComparesInItsJSONType pins that a
// document property is compared as the number or boolean the document holds.
func TestFindAllWithFilters_DocumentPropertyComparesInItsJSONType(t *testing.T) {
	t.Parallel()
	repo := newDocumentFilterRepo(t, newTestDB(t), "question", documentTypedSchema,
		documentTypedRecords("question"))
	runDocumentFilterCases(t, repo, "question", documentTypedCases("question"))
}

// TestFindAllWithFilters_ColumnNeIncludesMissing pins that ne on a projection
// column matches an empty column only when the condition asks for it, so the
// REST listing, which never asks, returns what it always did.
func TestFindAllWithFilters_ColumnNeIncludesMissing(t *testing.T) {
	t.Parallel()
	repo := newDocumentFilterRepo(t, newTestDB(t), "task",
		`{"type":"object","properties":{"name":{"type":"string"},"priority":{"type":"string"}}}`,
		[][2]string{
			{"urn:task:1", `{"@graph":[{"@id":"urn:task:1","name":"Chase invoices","priority":"high"}]}`},
			{"urn:task:2", `{"@graph":[{"@id":"urn:task:2","name":"Draft summary","priority":"normal"}]}`},
			{"urn:task:3", `{"@graph":[{"@id":"urn:task:3","name":"Renew lease"}]}`},
		})
	mcp := repositories.FilterCondition{Field: "priority", Operator: "ne", Value: "high", NeIncludesMissing: true}
	rest := repositories.FilterCondition{Field: "priority", Operator: "ne", Value: "high"}
	for _, flat := range []bool{false, true} {
		if got := listIDs(t, repo, "task", flat, mcp); strings.Join(got, ",") != "urn:task:2,urn:task:3" {
			t.Errorf("flat=%v: ne including missing matched %v, want [urn:task:2 urn:task:3]", flat, got)
		}
		if got := listIDs(t, repo, "task", flat, rest); strings.Join(got, ",") != "urn:task:2" {
			t.Errorf("flat=%v: REST ne matched %v, want [urn:task:2]", flat, got)
		}
	}
}
