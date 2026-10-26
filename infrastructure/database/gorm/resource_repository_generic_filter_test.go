package gorm

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/models"
)

// newGenericFilterRepo stores a resource type whose projection table cannot be
// made — a view holds its table name, so EnsureTable fails — and saves the
// records, which then live in the canonical resources table only. A filtered
// listing of the type takes the generic path.
func newGenericFilterRepo(t *testing.T, slug, schema string, records [][2]string) *ResourceRepository {
	t.Helper()
	db := newTestDBWithTypes(t)
	if err := db.AutoMigrate(&models.Resource{}, &models.ResourcePermission{}); err != nil {
		t.Fatalf("migrate resources: %v", err)
	}
	if err := db.Exec(fmt.Sprintf(`CREATE VIEW %q AS SELECT '' AS id WHERE 0`, slugToTableName(slug))).Error; err != nil {
		t.Fatalf("hold the projection table name: %v", err)
	}
	if err := db.Create(&models.ResourceType{
		ID: "urn:type:" + slug, Name: slug, Slug: slug, Schema: schema, Status: "active",
	}).Error; err != nil {
		t.Fatalf("store type: %v", err)
	}
	pm := &projectionManager{db: db, logger: &testLogger{}}
	if pm.HasProjectionTable(slug) {
		t.Fatalf("type %q has a projection table; the test needs one without", slug)
	}
	repo := &ResourceRepository{db: db, projMgr: pm, logger: &testLogger{}}
	for _, r := range records {
		if err := repo.Save(t.Context(), makeTestResource(t, r[0], slug, r[1])); err != nil {
			t.Fatalf("save %s: %v", r[0], err)
		}
	}
	return repo
}

func genericListIDs(
	t *testing.T, repo *ResourceRepository, slug string, filters ...repositories.FilterCondition,
) ([]string, error) {
	t.Helper()
	page, err := repo.FindAllByTypeWithFilters(t.Context(), slug, filters, "", 50, repositories.SortOptions{}, nil)
	if err != nil {
		return nil, err
	}
	got := []string{}
	for _, e := range page.Data {
		got = append(got, e.GetID())
	}
	return got, nil
}

const genericBriefingSchema = `{"type":"object","properties":{"name":{"type":"string"},` +
	`"status":{"type":"string"},"priority":{"type":"string"},"attendees":{"type":"integer"},` +
	`"owner":{"type":"string","x-resource-type":"person"}}}`

func genericBriefingRecords() [][2]string {
	return [][2]string{
		{"urn:briefing:1", `{"@graph":[{"@id":"urn:briefing:1","name":"Monday partner briefing",` +
			`"status":"open","priority":"high","attendees":3}]}`},
		{"urn:briefing:2", `{"@graph":[{"@id":"urn:briefing:2","name":"Quarterly lender briefing",` +
			`"status":"done","attendees":12}]}`},
		{"urn:briefing:3", `{"name":"Board pre-read","status":"open","priority":"normal","attendees":7}`}, // legacy flat
	}
}

// TestFindAllWithFilters_GenericPathHonorsTheMCPFlags pins that a type with no
// projection table applies a resource_list condition the way a projection
// does (wm-ugv8y7): a property is read from the entity node or the top level
// of the document, compared in its JSON type, and ne includes a resource with
// no value when the condition asks for it.
func TestFindAllWithFilters_GenericPathHonorsTheMCPFlags(t *testing.T) {
	t.Parallel()
	repo := newGenericFilterRepo(t, "briefing", genericBriefingSchema, genericBriefingRecords())
	cases := []struct {
		name string
		cond repositories.FilterCondition
		want []string
	}{
		{"shadowed property eq", repositories.FilterCondition{Field: "status", Operator: "eq", Value: "open",
			DocumentProperty: true, ValueType: repositories.FilterValueString, NeIncludesMissing: true},
			[]string{"urn:briefing:1", "urn:briefing:3"}},
		{"declared property ne including missing", repositories.FilterCondition{Field: "priority",
			Operator: "ne", Value: "high", ValueType: repositories.FilterValueString, NeIncludesMissing: true},
			[]string{"urn:briefing:2", "urn:briefing:3"}},
		{"number compared as a number", repositories.FilterCondition{Field: "attendees", Operator: "gt",
			Value: "5", ValueType: repositories.FilterValueNumber, NeIncludesMissing: true},
			[]string{"urn:briefing:2", "urn:briefing:3"}},
		{"standard column", repositories.FilterCondition{Field: "id", Operator: "ne", Value: "urn:briefing:2",
			ValueType: repositories.FilterValueString, NeIncludesMissing: true},
			[]string{"urn:briefing:1", "urn:briefing:3"}},
	}
	for _, tc := range cases {
		got, err := genericListIDs(t, repo, "briefing", tc.cond)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: matched %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestFindAllWithFilters_GenericPathKeepsTheRESTReading pins that a condition
// with none of the MCP flags — what the REST listing sends — still reads the
// top level of the document, so REST results do not change.
func TestFindAllWithFilters_GenericPathKeepsTheRESTReading(t *testing.T) {
	t.Parallel()
	repo := newGenericFilterRepo(t, "briefing", genericBriefingSchema, genericBriefingRecords())
	got, err := genericListIDs(t, repo, "briefing",
		repositories.FilterCondition{Field: "status", Operator: "eq", Value: "open"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "urn:briefing:3" {
		t.Errorf("REST status=open matched %v, want [urn:briefing:3]", got)
	}
}

// TestFindAllWithFilters_GenericPathRefusesAReference pins that a resource_list
// condition on a reference is refused for a type with no projection table:
// the document keeps a reference in its edges node, which the generic path
// does not read, so applying the condition would quietly match nothing.
func TestFindAllWithFilters_GenericPathRefusesAReference(t *testing.T) {
	t.Parallel()
	repo := newGenericFilterRepo(t, "briefing", genericBriefingSchema, genericBriefingRecords())
	_, err := genericListIDs(t, repo, "briefing", repositories.FilterCondition{Field: "owner", Operator: "eq",
		Value: "urn:person:1", ValueType: repositories.FilterValueString, NeIncludesMissing: true})
	if err == nil || !strings.Contains(err.Error(), `"owner"`) || !strings.Contains(err.Error(), "reference") {
		t.Fatalf("expected the reference condition to be refused, got %v", err)
	}
}
