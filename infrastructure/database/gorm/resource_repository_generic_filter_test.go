package gorm

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/models"

	"gorm.io/gorm"
)

// newGenericFilterRepo stores a resource type whose projection table cannot be
// made — a view holds its table name, so EnsureTable fails — and saves the
// records, which then live in the canonical resources table only. A filtered
// listing of the type takes the generic path.
func newGenericFilterRepo(t *testing.T, slug, schema string, records [][2]string) *ResourceRepository {
	t.Helper()
	return newGenericFilterRepoOn(t, newTestDB(t), slug, schema, records)
}

// newGenericFilterRepoOn is newGenericFilterRepo against db, which may be
// SQLite or PostgreSQL.
func newGenericFilterRepoOn(t *testing.T, db *gorm.DB, slug, schema string, records [][2]string) *ResourceRepository {
	t.Helper()
	if err := db.AutoMigrate(&models.ResourceType{}, &models.Resource{}, &models.ResourcePermission{}); err != nil {
		t.Fatalf("migrate resources: %v", err)
	}
	if err := db.Exec(fmt.Sprintf(`CREATE VIEW %q AS SELECT '' AS id WHERE 1 = 0`, slugToTableName(slug))).Error; err != nil {
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

// genericBriefingRecords are three records of the type slug, named
// urn:<slug>:1 to urn:<slug>:3.
func genericBriefingRecords(slug string) [][2]string {
	id := func(n string) string { return "urn:" + slug + ":" + n }
	return [][2]string{
		{id("1"), `{"@graph":[{"@id":"` + id("1") + `","name":"Monday partner briefing",` +
			`"status":"open","priority":"high","attendees":3}]}`},
		{id("2"), `{"@graph":[{"@id":"` + id("2") + `","name":"Quarterly lender briefing",` +
			`"status":"done","attendees":12}]}`},
		{id("3"), `{"name":"Board pre-read","status":"open","priority":"normal","attendees":7}`}, // legacy flat
	}
}

// TestFindAllWithFilters_GenericPathHonorsTheMCPFlags pins that a type with no
// projection table applies a resource_list condition the way a projection
// does (wm-ugv8y7): a property is read from the entity node or the top level
// of the document, compared in its JSON type, and ne includes a resource with
// no value when the condition asks for it.
func TestFindAllWithFilters_GenericPathHonorsTheMCPFlags(t *testing.T) {
	t.Parallel()
	repo := newGenericFilterRepo(t, "briefing", genericBriefingSchema, genericBriefingRecords("briefing"))
	runGenericFlagCases(t, repo, "briefing")
}

// TestFindAllWithFilters_GenericPathHonorsTheMCPFlags_Postgres runs the same
// cases, and the reference refusal, against a real PostgreSQL database
// (wm-em584x): there a property is read as jsonb and a standard column is
// compared with a bound string, which no SQLite run exercises. Set
// TEST_POSTGRES_DSN to run it.
func TestFindAllWithFilters_GenericPathHonorsTheMCPFlags_Postgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set — the PostgreSQL generic filter path is skipped")
	}
	db := openPostgresReplica(t, dsn)
	slug := fmt.Sprintf("genfilter%d", time.Now().UnixNano())
	t.Cleanup(func() {
		// Best-effort cleanup of a shared database: whatever is left sits
		// under a slug no other test uses.
		_ = db.Exec(fmt.Sprintf(`DROP VIEW IF EXISTS %q`, slugToTableName(slug))).Error
		_ = db.Where("type_slug = ?", slug).Delete(&models.Resource{}).Error
		_ = db.Unscoped().Where("slug = ?", slug).Delete(&models.ResourceType{}).Error
	})
	repo := newGenericFilterRepoOn(t, db, slug, genericBriefingSchema, genericBriefingRecords(slug))
	if repo.db.Name() != "postgres" {
		t.Fatalf("dialect = %q, want postgres", repo.db.Name())
	}
	runGenericFlagCases(t, repo, slug)
	if _, err := genericListIDs(t, repo, slug, repositories.FilterCondition{Field: "owner", Operator: "eq",
		Value: "urn:person:1", ValueType: repositories.FilterValueString, NeIncludesMissing: true}); err == nil {
		t.Error("expected the reference condition to be refused on PostgreSQL")
	}
}

func runGenericFlagCases(t *testing.T, repo *ResourceRepository, slug string) {
	t.Helper()
	id := func(n string) string { return "urn:" + slug + ":" + n }
	cases := []struct {
		name string
		cond repositories.FilterCondition
		want []string
	}{
		{"shadowed property eq", repositories.FilterCondition{Field: "status", Operator: "eq", Value: "open",
			DocumentProperty: true, ValueType: repositories.FilterValueString, NeIncludesMissing: true},
			[]string{id("1"), id("3")}},
		{"declared property ne including missing", repositories.FilterCondition{Field: "priority",
			Operator: "ne", Value: "high", ValueType: repositories.FilterValueString, NeIncludesMissing: true},
			[]string{id("2"), id("3")}},
		{"number compared as a number", repositories.FilterCondition{Field: "attendees", Operator: "gt",
			Value: "5", ValueType: repositories.FilterValueNumber, NeIncludesMissing: true},
			[]string{id("2"), id("3")}},
		{"standard column", repositories.FilterCondition{Field: "id", Operator: "ne", Value: id("2"),
			ValueType: repositories.FilterValueString, NeIncludesMissing: true},
			[]string{id("1"), id("3")}},
		{"standard number column", repositories.FilterCondition{Field: "sequenceNo", Operator: "gte",
			Value: "1", ValueType: repositories.FilterValueNumber, NeIncludesMissing: true},
			[]string{id("1"), id("2"), id("3")}},
		{"standard time column", repositories.FilterCondition{Field: "createdAt", Operator: "lt",
			Value: "2999-01-01T00:00:00Z", ValueType: repositories.FilterValueString, NeIncludesMissing: true},
			[]string{id("1"), id("2"), id("3")}},
	}
	for _, tc := range cases {
		got, err := genericListIDs(t, repo, slug, tc.cond)
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
	repo := newGenericFilterRepo(t, "briefing", genericBriefingSchema, genericBriefingRecords("briefing"))
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
	repo := newGenericFilterRepo(t, "briefing", genericBriefingSchema, genericBriefingRecords("briefing"))
	_, err := genericListIDs(t, repo, "briefing", repositories.FilterCondition{Field: "owner", Operator: "eq",
		Value: "urn:person:1", ValueType: repositories.FilterValueString, NeIncludesMissing: true})
	if err == nil || !strings.Contains(err.Error(), `"owner"`) || !strings.Contains(err.Error(), "reference") {
		t.Fatalf("expected the reference condition to be refused, got %v", err)
	}
}

// TestGenericFilterClause_Postgres pins the SQL and arguments the generic path
// builds on PostgreSQL (wm-em584x), as TestDocumentFilterClause_Postgres does
// for the projection path: a standard column is compared with the value bound
// as is, ne including missing adds IS NULL, and any other field is read from
// the document as jsonb.
func TestGenericFilterClause_Postgres(t *testing.T) {
	t.Parallel()
	const doc = "data::jsonb"
	val := "COALESCE(jsonb_extract_path(" + doc + ", ?, ?, ?), jsonb_extract_path(" + doc + ", ?))"
	notReference := func(string) (bool, error) { return false, nil }
	cases := []struct {
		name     string
		cond     repositories.FilterCondition
		wantSQL  string
		wantArgs []any
	}{
		{
			"standard number column",
			repositories.FilterCondition{Field: "sequenceNo", Operator: "gt", Value: "3",
				ValueType: repositories.FilterValueNumber, NeIncludesMissing: true},
			"sequence_no > ?",
			[]any{"3"},
		},
		{
			"standard time column",
			repositories.FilterCondition{Field: "createdAt", Operator: "lt", Value: "2026-10-01T00:00:00Z",
				ValueType: repositories.FilterValueString, NeIncludesMissing: true},
			"created_at < ?",
			[]any{"2026-10-01T00:00:00Z"},
		},
		{
			"standard column ne including missing",
			repositories.FilterCondition{Field: "accountId", Operator: "ne", Value: "acct-1",
				ValueType: repositories.FilterValueString, NeIncludesMissing: true},
			"(account_id IS NULL OR account_id != ?)",
			[]any{"acct-1"},
		},
		{
			"shadowed property",
			repositories.FilterCondition{Field: "status", Operator: "eq", Value: "open", DocumentProperty: true,
				ValueType: repositories.FilterValueString, NeIncludesMissing: true},
			"(jsonb_typeof(" + val + ") IN ('string') AND " + val + " = CAST(? AS jsonb))",
			[]any{"@graph", "0", "status", "status", "@graph", "0", "status", "status", `"open"`},
		},
		{
			"declared number property",
			repositories.FilterCondition{Field: "attendees", Operator: "gte", Value: "5",
				ValueType: repositories.FilterValueNumber, NeIncludesMissing: true},
			"(jsonb_typeof(" + val + ") IN ('number') AND " + val + " >= CAST(? AS jsonb))",
			[]any{"@graph", "0", "attendees", "attendees", "@graph", "0", "attendees", "attendees", "5"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotSQL, gotArgs, ok, err := genericFilterClause("postgres", "briefing", tc.cond, notReference)
			if err != nil || !ok {
				t.Fatalf("ok = %v, err = %v; want a clause", ok, err)
			}
			if gotSQL != tc.wantSQL {
				t.Errorf("SQL\n got: %s\nwant: %s", gotSQL, tc.wantSQL)
			}
			if !reflect.DeepEqual(gotArgs, tc.wantArgs) {
				t.Errorf("args\n got: %#v\nwant: %#v", gotArgs, tc.wantArgs)
			}
		})
	}
}

// TestGenericFilterClause_SkipsAndRefuses pins the two other outcomes: an
// unknown operator is skipped, as the REST listing has always had it, and a
// condition from the MCP tool on a reference is refused.
func TestGenericFilterClause_SkipsAndRefuses(t *testing.T) {
	t.Parallel()
	isReference := func(field string) (bool, error) { return field == "owner", nil }
	if _, _, ok, err := genericFilterClause("postgres", "briefing",
		repositories.FilterCondition{Field: "name", Operator: "like", Value: "x"}, isReference); ok || err != nil {
		t.Errorf("unknown operator: ok = %v, err = %v; want skipped", ok, err)
	}
	_, _, _, err := genericFilterClause("postgres", "briefing", repositories.FilterCondition{Field: "owner",
		Operator: "eq", Value: "urn:person:1", ValueType: repositories.FilterValueString}, isReference)
	if err == nil || !strings.Contains(err.Error(), `"owner"`) {
		t.Errorf("reference: err = %v, want a refusal naming owner", err)
	}
}
