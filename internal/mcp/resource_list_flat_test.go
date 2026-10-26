package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// flatRecordingResources serves flat rows, or the error flatErr, and records
// which listing method resource_list called and with what filters. Its entity
// listings return docs, one resource per identifier.
type flatRecordingResources struct {
	stubResourceService
	rows    []map[string]any
	flatErr error
	docs    map[string]string

	calls      []string
	gotFilters []repositories.FilterCondition
}

func (s *flatRecordingResources) ListFlat(
	_ context.Context, _, _ string, _ int, _ repositories.SortOptions,
) (repositories.PaginatedResponse[map[string]any], error) {
	s.calls = append(s.calls, "ListFlat")
	return repositories.PaginatedResponse[map[string]any]{Data: s.rows, Cursor: "next", HasMore: true}, s.flatErr
}

func (s *flatRecordingResources) ListFlatWithFilters(
	_ context.Context, _ string, filters []repositories.FilterCondition, _ string, _ int,
	_ repositories.SortOptions,
) (repositories.PaginatedResponse[map[string]any], error) {
	s.calls = append(s.calls, "ListFlatWithFilters")
	s.gotFilters = filters
	return repositories.PaginatedResponse[map[string]any]{Data: s.rows, Cursor: "next", HasMore: true}, s.flatErr
}

func (s *flatRecordingResources) List(
	_ context.Context, slug, _ string, _ int, _ repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	s.calls = append(s.calls, "List")
	return s.entities(slug)
}

func (s *flatRecordingResources) ListWithFilters(
	_ context.Context, slug string, filters []repositories.FilterCondition, _ string, _ int,
	_ repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	s.calls = append(s.calls, "ListWithFilters")
	s.gotFilters = filters
	return s.entities(slug)
}

func (s *flatRecordingResources) entities(slug string) (repositories.PaginatedResponse[*entities.Resource], error) {
	page := repositories.PaginatedResponse[*entities.Resource]{}
	for id, doc := range s.docs {
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
	if len(svc.gotFilters) != 1 || svc.gotFilters[0] != want {
		t.Errorf("filters = %+v, want [%+v]", svc.gotFilters, want)
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
	if len(svc.gotFilters) != 1 || svc.gotFilters[0].Field != "priority" {
		t.Errorf("the fallback got filters %+v, want the priority condition", svc.gotFilters)
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
	if strings.Join(svc.calls, ",") != "ListFlat,ListWithFilters" {
		t.Errorf("calls = %v, want one flat read and one read of the page's documents", svc.calls)
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

var _ application.ResourceService = (*flatRecordingResources)(nil)
