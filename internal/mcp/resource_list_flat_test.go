package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// flatRecordingResources serves flat rows, or the error flatErr, and records
// every read resource_list makes, each with its own arguments, so a test can
// tell the flat read from a later read of documents. Its entity listings and
// its read by id return docs, one resource per identifier; the read by id
// returns only the identifiers it was asked for.
type flatRecordingResources struct {
	stubResourceService
	rows    []map[string]any
	flatErr error
	docs    map[string]string

	calls []string
	reads []resourceRead
}

// resourceRead is one call resource_list made to the service.
type resourceRead struct {
	method  string
	filters []repositories.FilterCondition
	cursor  string
	limit   int
	sort    repositories.SortOptions
	ids     []string
}

func (s *flatRecordingResources) record(r resourceRead) {
	s.calls = append(s.calls, r.method)
	s.reads = append(s.reads, r)
}

func (s *flatRecordingResources) ListFlat(
	_ context.Context, _, cursor string, limit int, sort repositories.SortOptions,
) (repositories.PaginatedResponse[map[string]any], error) {
	s.record(resourceRead{method: "ListFlat", cursor: cursor, limit: limit, sort: sort})
	return repositories.PaginatedResponse[map[string]any]{Data: s.rows, Cursor: "next", HasMore: true}, s.flatErr
}

func (s *flatRecordingResources) ListFlatWithFilters(
	_ context.Context, _ string, filters []repositories.FilterCondition, cursor string, limit int,
	sort repositories.SortOptions,
) (repositories.PaginatedResponse[map[string]any], error) {
	s.record(resourceRead{method: "ListFlatWithFilters", filters: filters, cursor: cursor, limit: limit, sort: sort})
	return repositories.PaginatedResponse[map[string]any]{Data: s.rows, Cursor: "next", HasMore: true}, s.flatErr
}

func (s *flatRecordingResources) List(
	_ context.Context, slug, cursor string, limit int, sort repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	s.record(resourceRead{method: "List", cursor: cursor, limit: limit, sort: sort})
	return s.entities(slug, nil)
}

func (s *flatRecordingResources) ListWithFilters(
	_ context.Context, slug string, filters []repositories.FilterCondition, cursor string, limit int,
	sort repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	s.record(resourceRead{method: "ListWithFilters", filters: filters, cursor: cursor, limit: limit, sort: sort})
	return s.entities(slug, nil)
}

func (s *flatRecordingResources) ListByIDs(
	_ context.Context, slug string, ids []string,
) ([]*entities.Resource, error) {
	s.record(resourceRead{method: "ListByIDs", ids: ids})
	page, err := s.entities(slug, ids)
	return page.Data, err
}

// entities returns a resource for each of docs, or, when only is not nil, for
// each of docs that only names.
func (s *flatRecordingResources) entities(
	slug string, only []string,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	page := repositories.PaginatedResponse[*entities.Resource]{}
	for id, doc := range s.docs {
		if only != nil && !slices.Contains(only, id) {
			continue
		}
		e := &entities.Resource{}
		if err := e.Restore(id, slug, "active", json.RawMessage(doc), "", "", time.Now(), 1); err != nil {
			return page, err
		}
		page.Data = append(page.Data, e)
	}
	return page, nil
}

func callFlatResourceList(
	t *testing.T, svc *flatRecordingResources, schema, args string,
) (map[string]json.RawMessage, *mcp.CallToolResult) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	registerResourceTools(server, svc, &schemaTypes{schema: schema})
	cs := connectForGate(t, ctx, server)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "resource_list", Arguments: json.RawMessage(args)})
	if err != nil {
		t.Fatalf("call resource_list: %v", err)
	}
	var answer map[string]json.RawMessage
	if !res.IsError {
		if err := json.Unmarshal([]byte(resultText(res)), &answer); err != nil {
			t.Fatalf("answer is not a JSON object: %v (%s)", err, resultText(res))
		}
	}
	return answer, res
}

// schemaWithoutShadowing declares no property a standard column shadows, so a
// flat page needs no second read.
const schemaWithoutShadowing = `{"type":"object","properties":{"name":{"type":"string"},"priority":{"type":"string"}}}`

func TestResourceList_FlatAnswersWithRows(t *testing.T) {
	t.Parallel()
	svc := &flatRecordingResources{rows: []map[string]any{{"id": "urn:task:1", "name": "Chase invoices"}}}
	answer, res := callFlatResourceList(t, svc, schemaWithoutShadowing, `{"type_slug":"task","flat":true}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if strings.Join(svc.calls, ",") != "ListFlat" {
		t.Errorf("calls = %v, want [ListFlat]", svc.calls)
	}
	if _, ok := answer["data"]; ok {
		t.Errorf("a flat answer carries data: %s", answer["data"])
	}
	var rows []map[string]any
	if err := json.Unmarshal(answer["rows"], &rows); err != nil || len(rows) != 1 || rows[0]["name"] != "Chase invoices" {
		t.Errorf("rows = %s, want the one row", answer["rows"])
	}
	if string(answer["cursor"]) != `"next"` || string(answer["has_more"]) != "true" {
		t.Errorf("cursor = %s, has_more = %s; want the page's", answer["cursor"], answer["has_more"])
	}
}

func TestResourceList_FlatEmptyPageIsAnEmptyRowsArray(t *testing.T) {
	t.Parallel()
	answer, res := callFlatResourceList(t, &flatRecordingResources{}, schemaWithoutShadowing,
		`{"type_slug":"task","flat":true}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if string(answer["rows"]) != "[]" {
		t.Errorf("rows = %s, want []", answer["rows"])
	}
}

// TestResourceList_DocumentsAnswerKeepsItsShape pins that a call without flat
// answers as it did before: data, [] when empty, and no rows key.
func TestResourceList_DocumentsAnswerKeepsItsShape(t *testing.T) {
	t.Parallel()
	for _, args := range []string{`{"type_slug":"task"}`, `{"type_slug":"task","flat":false}`} {
		svc := &flatRecordingResources{}
		answer, res := callFlatResourceList(t, svc, schemaWithoutShadowing, args)
		if res.IsError {
			t.Fatalf("%s: unexpected tool error: %s", args, resultText(res))
		}
		if strings.Join(svc.calls, ",") != "List" {
			t.Errorf("%s: calls = %v, want [List]", args, svc.calls)
		}
		if string(answer["data"]) != "[]" {
			t.Errorf("%s: data = %s, want []", args, answer["data"])
		}
		if _, ok := answer["rows"]; ok {
			t.Errorf("%s: a documents answer carries rows: %s", args, answer["rows"])
		}
	}
}

func TestResourceList_FlatWithFiltersPassesTheConditions(t *testing.T) {
	t.Parallel()
	svc := &flatRecordingResources{}
	_, res := callFlatResourceList(t, svc, schemaWithoutShadowing,
		`{"type_slug":"task","flat":true,"filters":[{"field":"priority","operator":"ne","value":"high"}]}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if strings.Join(svc.calls, ",") != "ListFlatWithFilters" {
		t.Fatalf("calls = %v, want [ListFlatWithFilters]", svc.calls)
	}
	want := repositories.FilterCondition{Field: "priority", Operator: "ne", Value: "high",
		ValueType: repositories.FilterValueString, NeIncludesMissing: true}
	if got := svc.reads[0].filters; len(got) != 1 || got[0] != want {
		t.Errorf("filters = %+v, want [%+v]", got, want)
	}
}

// TestResourceList_FlatFallbackKeepsTheFilters pins the table-less fallback:
// the entity path answers with documents, and it gets the same conditions —
// never the plain listing.
func TestResourceList_FlatFallbackKeepsTheFilters(t *testing.T) {
	t.Parallel()
	svc := &flatRecordingResources{
		flatErr: fmt.Errorf("%w: %q", repositories.ErrNoProjectionTable, "briefing"),
		docs:    map[string]string{"urn:briefing:1": `{"@graph":[{"@id":"urn:briefing:1","name":"Monday"}]}`},
	}
	answer, res := callFlatResourceList(t, svc, schemaWithoutShadowing,
		`{"type_slug":"briefing","flat":true,"filters":[{"field":"priority","operator":"eq","value":"high"}]}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if strings.Join(svc.calls, ",") != "ListFlatWithFilters,ListWithFilters" {
		t.Fatalf("calls = %v, want [ListFlatWithFilters ListWithFilters]", svc.calls)
	}
	if got := svc.reads[1].filters; len(got) != 1 || got[0].Field != "priority" {
		t.Errorf("the fallback got filters %+v, want the priority condition", got)
	}
	if _, ok := answer["rows"]; ok {
		t.Errorf("the fallback answer carries rows: %s", answer["rows"])
	}
	if !strings.Contains(string(answer["data"]), "urn:briefing:1") {
		t.Errorf("data = %s, want the briefing document", answer["data"])
	}
}

// TestResourceList_FlatReportsOtherFailures pins that only a missing
// projection table falls back: any other failure is the call's error, not a
// quietly different answer.
func TestResourceList_FlatReportsOtherFailures(t *testing.T) {
	t.Parallel()
	svc := &flatRecordingResources{flatErr: errors.New("database is locked")}
	_, res := callFlatResourceList(t, svc, schemaWithoutShadowing, `{"type_slug":"task","flat":true}`)
	if !res.IsError || !strings.Contains(resultText(res), "database is locked") {
		t.Fatalf("expected the failure to be reported, got: %s", resultText(res))
	}
	if strings.Join(svc.calls, ",") != "ListFlat" {
		t.Errorf("calls = %v, want [ListFlat] only", svc.calls)
	}
}

// TestResourceList_FlatRowCarriesTheShadowedProperty pins that a row's column
// named like a shadowed schema property holds the property from the document,
// as a condition on that name reads it, and null when the document has none.
func TestResourceList_FlatRowCarriesTheShadowedProperty(t *testing.T) {
	t.Parallel()
	svc := &flatRecordingResources{
		rows: []map[string]any{
			{"id": "urn:task:1", "name": "Chase invoices", "status": "active"},
			{"id": "urn:task:2", "name": "Renew lease", "status": "active"},
			{"id": "urn:task:3", "name": "Draft summary", "status": "active"},
		},
		docs: map[string]string{
			"urn:task:1": `{"@graph":[{"@id":"urn:task:1","name":"Chase invoices","status":"open"}]}`,
			"urn:task:2": `{"name":"Renew lease","status":"done"}`,
			"urn:task:3": `{"@graph":[{"@id":"urn:task:3","name":"Draft summary"}]}`,
		},
	}
	answer, res := callFlatResourceList(t, svc, taskSchema, `{"type_slug":"task","flat":true}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	var rows []map[string]any
	if err := json.Unmarshal(answer["rows"], &rows); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"urn:task:1": "open", "urn:task:2": "done", "urn:task:3": nil}
	for _, row := range rows {
		if got := row["status"]; got != want[fmt.Sprint(row["id"])] {
			t.Errorf("row %v has status %v, want %v", row["id"], got, want[fmt.Sprint(row["id"])])
		}
	}
	if strings.Join(svc.calls, ",") != "ListFlat,ListByIDs" {
		t.Errorf("calls = %v, want one flat read and one read of the page's documents by id", svc.calls)
	}
}

// TestResourceList_FlatReadsThePagesDocumentsByID pins wm-yh1xt5: the
// documents for a filtered flat page are read by the page's identifiers —
// exactly those — and the filtered listing is not run a second time, so a
// write landing between the two reads cannot move a row off the page.
func TestResourceList_FlatReadsThePagesDocumentsByID(t *testing.T) {
	t.Parallel()
	svc := &flatRecordingResources{
		rows: []map[string]any{
			{"id": "urn:task:7", "name": "Chase invoices", "status": "active"},
			{"id": "urn:task:9", "name": "Renew lease", "status": "active"},
		},
		docs: map[string]string{
			"urn:task:7": `{"@graph":[{"@id":"urn:task:7","status":"open"}]}`,
			"urn:task:8": `{"@graph":[{"@id":"urn:task:8","status":"open"}]}`,
			"urn:task:9": `{"@graph":[{"@id":"urn:task:9","status":"open"}]}`,
		},
	}
	_, res := callFlatResourceList(t, svc, taskSchema, `{"type_slug":"task","flat":true,"cursor":"c1","limit":2,`+
		`"sort_by":"name","filters":[{"field":"status","operator":"eq","value":"open"}]}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if strings.Join(svc.calls, ",") != "ListFlatWithFilters,ListByIDs" {
		t.Fatalf("calls = %v, want [ListFlatWithFilters ListByIDs]", svc.calls)
	}
	flat := svc.reads[0]
	if flat.cursor != "c1" || flat.limit != 2 || flat.sort.SortBy != "name" || len(flat.filters) != 1 {
		t.Errorf("the flat read got %+v, want the call's cursor, limit, sort and filter", flat)
	}
	if got := svc.reads[1].ids; strings.Join(got, ",") != "urn:task:7,urn:task:9" {
		t.Errorf("the documents were read for %v, want exactly the page's [urn:task:7 urn:task:9]", got)
	}
}

// TestResourceList_FlatRefusesARowWithNoDocument pins that a row whose
// resource the read by id does not return — deleted between the two reads —
// fails the call naming it, rather than answering with a null property.
func TestResourceList_FlatRefusesARowWithNoDocument(t *testing.T) {
	t.Parallel()
	svc := &flatRecordingResources{
		rows: []map[string]any{{"id": "urn:task:1", "status": "active"}, {"id": "urn:task:2", "status": "active"}},
		docs: map[string]string{"urn:task:1": `{"@graph":[{"@id":"urn:task:1","status":"open"}]}`},
	}
	_, res := callFlatResourceList(t, svc, taskSchema, `{"type_slug":"task","flat":true}`)
	if !res.IsError || !strings.Contains(resultText(res), "urn:task:2") {
		t.Fatalf("expected an error naming urn:task:2, got: %s", resultText(res))
	}
}

// listTypedSchema gives a property a list of types, so the projection reads
// no property columns from it and its table holds only the standard columns.
const listTypedSchema = `{"type":"object","properties":{"name":{"type":"string"},` +
	`"amount":{"type":["number","null"]}}}`

// TestResourceList_FlatRowCarriesEveryPropertyOfAColumnlessProjection pins
// wm-ndlt9y: when the projection keeps no property columns for a type, a row
// carries every schema property from the stored document — not a row of
// standard columns that a sum then reads as nothing.
func TestResourceList_FlatRowCarriesEveryPropertyOfAColumnlessProjection(t *testing.T) {
	t.Parallel()
	svc := &flatRecordingResources{
		rows: []map[string]any{
			{"id": "urn:payment:1", "status": "active"},
			{"id": "urn:payment:2", "status": "active"},
		},
		docs: map[string]string{
			"urn:payment:1": `{"@graph":[{"@id":"urn:payment:1","name":"Rent","amount":1200.5}]}`,
			"urn:payment:2": `{"name":"Power","amount":null}`,
		},
	}
	answer, res := callFlatResourceList(t, svc, listTypedSchema, `{"type_slug":"payment","flat":true}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	var rows []map[string]any
	if err := json.Unmarshal(answer["rows"], &rows); err != nil || len(rows) != 2 {
		t.Fatalf("rows = %s, want the two rows", answer["rows"])
	}
	want := map[string][2]any{"urn:payment:1": {"Rent", 1200.5}, "urn:payment:2": {"Power", nil}}
	for _, row := range rows {
		w := want[fmt.Sprint(row["id"])]
		if _, has := row["amount"]; !has || row["name"] != w[0] || row["amount"] != w[1] {
			t.Errorf("row %v = %v, want name %v and amount %v", row["id"], row, w[0], w[1])
		}
		if row["status"] != "active" {
			t.Errorf("row %v has status %v, want the column's active", row["id"], row["status"])
		}
	}
}

// TestResourceList_FlatAnswersATypeWithNoSchemaWithDocuments pins wm-ndlt9y
// for a type with no JSON Schema: its projection has only the standard
// columns and nothing names its properties, so a flat call answers with its
// documents, keeping the conditions, rather than with rows that carry none of
// its values.
func TestResourceList_FlatAnswersATypeWithNoSchemaWithDocuments(t *testing.T) {
	t.Parallel()
	svc := &flatRecordingResources{
		rows: []map[string]any{{"id": "urn:note:1", "status": "active"}},
		docs: map[string]string{"urn:note:1": `{"@graph":[{"@id":"urn:note:1","topic":"pricing"}]}`},
	}
	answer, res := callFlatResourceList(t, svc, "",
		`{"type_slug":"note","flat":true,"filters":[{"field":"topic","operator":"eq","value":"pricing"}]}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if strings.Join(svc.calls, ",") != "ListWithFilters" {
		t.Fatalf("calls = %v, want [ListWithFilters]", svc.calls)
	}
	if got := svc.reads[0].filters; len(got) != 1 || got[0].Field != "topic" {
		t.Errorf("the documents read got filters %+v, want the topic condition", got)
	}
	if _, ok := answer["rows"]; ok {
		t.Errorf("a type with no schema answered with rows: %s", answer["rows"])
	}
	if !strings.Contains(string(answer["data"]), "pricing") {
		t.Errorf("data = %s, want the note's document", answer["data"])
	}
}

// TestResourceList_FlatAnswersAColumnlessReferenceWithDocuments pins that a
// reference the projection keeps no column for sends a flat call to the
// documents: the stored document keeps a reference as an edge the row cannot
// read, so the row would carry null for it.
func TestResourceList_FlatAnswersAColumnlessReferenceWithDocuments(t *testing.T) {
	t.Parallel()
	const schema = `{"type":"object","properties":{"amount":{"type":["number","null"]},` +
		`"payee":{"type":"string","x-resource-type":"person"}}}`
	svc := &flatRecordingResources{docs: map[string]string{"urn:payment:1": `{"amount":3}`}}
	answer, res := callFlatResourceList(t, svc, schema, `{"type_slug":"payment","flat":true}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if strings.Join(svc.calls, ",") != "List" {
		t.Fatalf("calls = %v, want [List]", svc.calls)
	}
	if !strings.Contains(string(answer["data"]), "urn:payment:1") {
		t.Errorf("data = %s, want the payment's document", answer["data"])
	}
}

// TestResourceList_FlatRefusesADocumentThatDoesNotParse pins wm-3vzk32: a
// stored document that is not JSON is the call's error, naming the resource,
// not a row whose shadowed property quietly reads null.
func TestResourceList_FlatRefusesADocumentThatDoesNotParse(t *testing.T) {
	t.Parallel()
	svc := &flatRecordingResources{
		rows: []map[string]any{{"id": "urn:task:1", "name": "Chase invoices", "status": "active"}},
		docs: map[string]string{"urn:task:1": `{"@graph":[{"@id":"urn:task:1","status":"open"`},
	}
	_, res := callFlatResourceList(t, svc, taskSchema, `{"type_slug":"task","flat":true}`)
	if !res.IsError || !strings.Contains(resultText(res), "urn:task:1") {
		t.Fatalf("expected an error naming urn:task:1, got: %s", resultText(res))
	}
}

// TestResourceList_DescriptionTellsWhenToPreferFlat pins the fourth
// acceptance criterion of story wm-xtpky.2: the tool description says what
// flat answers with and when a model should ask for it.
func TestResourceList_DescriptionTellsWhenToPreferFlat(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"`flat`", "`rows`", "large result set", "counting or summing"} {
		if !strings.Contains(resourceListDescription, want) {
			t.Errorf("the resource_list description does not say %q", want)
		}
	}
}

// TestResourceList_DescriptionSaysACountNeedsEveryPage pins wm-2nzj3z: a
// model steered to flat for a count answers from the first page unless the
// description says to follow the cursor to the end.
func TestResourceList_DescriptionSaysACountNeedsEveryPage(t *testing.T) {
	t.Parallel()
	const want = "follow `cursor` until `has_more` is false"
	if !strings.Contains(resourceListDescription, want) {
		t.Errorf("the resource_list description does not say %q", want)
	}
}

// TestResourceList_RowsMayBeAbsentUnderFlat pins wm-i7zc78: a flat call can
// answer under data, so neither the output schema nor the description may
// promise rows whenever flat is set. A client that reads only rows would
// report an empty listing over a full one.
func TestResourceList_RowsMayBeAbsentUnderFlat(t *testing.T) {
	t.Parallel()
	field, _ := reflect.TypeOf(ListResourcesOutput{}).FieldByName("Rows")
	tag := field.Tag.Get("jsonschema")
	for _, want := range []string{"may be absent", "data"} {
		if !strings.Contains(tag, want) {
			t.Errorf("the rows schema line %q does not say %q", tag, want)
		}
	}
	for _, want := range []string{"`rows` may be absent", "read `data`", "no JSON Schema"} {
		if !strings.Contains(resourceListDescription, want) {
			t.Errorf("the resource_list description does not say %q", want)
		}
	}
}

var (
	_ application.ResourceService  = (*flatRecordingResources)(nil)
	_ application.ResourceIDLister = (*flatRecordingResources)(nil)
)
