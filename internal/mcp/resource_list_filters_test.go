package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// listRecordingResources records which listing method resource_list called,
// and with what filters.
type listRecordingResources struct {
	stubResourceService
	listCalls     int
	filteredCalls int
	gotFilters    []repositories.FilterCondition
	gotCursor     string
}

func (s *listRecordingResources) List(
	_ context.Context, _, _ string, _ int, _ repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	s.listCalls++
	return repositories.PaginatedResponse[*entities.Resource]{}, nil
}

func (s *listRecordingResources) ListWithFilters(
	_ context.Context, _ string, filters []repositories.FilterCondition, cursor string, _ int,
	_ repositories.SortOptions,
) (repositories.PaginatedResponse[*entities.Resource], error) {
	s.filteredCalls++
	s.gotFilters = filters
	s.gotCursor = cursor
	return repositories.PaginatedResponse[*entities.Resource]{}, nil
}

// schemaTypes serves one resource type with the given schema.
type schemaTypes struct {
	stubResourceTypeService
	schema string
}

func (s *schemaTypes) GetBySlug(_ context.Context, slug string) (*entities.ResourceType, error) {
	rt := &entities.ResourceType{}
	var schema json.RawMessage
	if s.schema != "" {
		schema = json.RawMessage(s.schema)
	}
	if err := rt.Restore("urn:type:"+slug, "Task", slug, "", "active",
		json.RawMessage(`{"@vocab":"https://schema.org/"}`), schema, time.Now(), 1); err != nil {
		return nil, err
	}
	return rt, nil
}

const taskSchema = `{"type":"object","properties":{"@type":{"type":"string"},"name":{"type":"string"},` +
	`"status":{"type":"string"},"priority":{"type":"string"},"dueDate":{"type":"string"}}}`

func callResourceList(t *testing.T, schema, args string) (*listRecordingResources, *mcp.CallToolResult) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc := &listRecordingResources{}
	server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	registerResourceTools(server, svc, &schemaTypes{schema: schema})
	cs := connectForGate(t, ctx, server)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "resource_list", Arguments: json.RawMessage(args)})
	if err != nil {
		t.Fatalf("call resource_list: %v", err)
	}
	return svc, res
}

func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestResourceList_ValidFilterReachesListWithFilters(t *testing.T) {
	t.Parallel()
	svc, res := callResourceList(t, taskSchema, `{"type_slug":"task","cursor":"c1","filters":[`+
		`{"field":"priority","operator":"eq","value":"high"},`+
		`{"field":"createdAt","operator":"gte","value":"2026-06-01T00:00:00Z"}]}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if svc.filteredCalls != 1 || svc.listCalls != 0 {
		t.Fatalf("ListWithFilters calls = %d, List calls = %d; want 1 and 0", svc.filteredCalls, svc.listCalls)
	}
	want := []repositories.FilterCondition{
		{Field: "priority", Operator: "eq", Value: "high",
			ValueType: repositories.FilterValueString, NeIncludesMissing: true},
		{Field: "createdAt", Operator: "gte", Value: "2026-06-01T00:00:00Z",
			ValueType: repositories.FilterValueString, NeIncludesMissing: true},
	}
	if len(svc.gotFilters) != len(want) {
		t.Fatalf("filters = %+v, want %+v", svc.gotFilters, want)
	}
	for i := range want {
		if svc.gotFilters[i] != want[i] {
			t.Errorf("filter %d = %+v, want %+v", i, svc.gotFilters[i], want[i])
		}
	}
	if svc.gotCursor != "c1" {
		t.Errorf("cursor = %q, want c1", svc.gotCursor)
	}
}

func TestResourceList_UnknownFieldErrorsWithoutCallingService(t *testing.T) {
	t.Parallel()
	svc, res := callResourceList(t, taskSchema,
		`{"type_slug":"task","filters":[{"field":"assignee","operator":"eq","value":"dana"}]}`)
	if !res.IsError {
		t.Fatalf("expected a tool error, got: %s", resultText(res))
	}
	if svc.filteredCalls != 0 || svc.listCalls != 0 {
		t.Errorf("the service was called (filtered %d, unfiltered %d); want no call", svc.filteredCalls, svc.listCalls)
	}
	msg := resultText(res)
	for _, want := range []string{`"assignee" is not filterable`,
		"filterable fields: dueDate, name, priority, status, id, createdAt, createdBy, accountId, sequenceNo, updatedAt"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not contain %q", msg, want)
		}
	}
	if strings.Contains(msg, "@type") {
		t.Errorf("error lists the JSON-LD key @type as filterable: %q", msg)
	}
}

func TestResourceList_FilterRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, filter, want string
	}{
		{"unsupported operator", `{"field":"status","operator":"contains","value":"op"}`,
			"supported operators: eq, ne, gt, gte, lt, lte"},
		{"missing operator", `{"field":"status","operator":"","value":"open"}`,
			"supported operators: eq, ne, gt, gte, lt, lte"},
		{"JSON-LD key", `{"field":"@type","operator":"eq","value":"Action"}`, `"@type" is not filterable`},
		{"object value", `{"field":"status","operator":"eq","value":{"state":"open"}}`,
			"value must be a string, a number or a boolean"},
		{"array value", `{"field":"status","operator":"eq","value":["open"]}`,
			"value must be a string, a number or a boolean"},
		{"null value", `{"field":"status","operator":"eq","value":null}`,
			"value must be a string, a number or a boolean"},
		{"empty field", `{"field":"","operator":"eq","value":"open"}`, "has no field"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc, res := callResourceList(t, taskSchema, `{"type_slug":"task","filters":[`+tc.filter+`]}`)
			if !res.IsError {
				t.Fatalf("expected a tool error, got: %s", resultText(res))
			}
			msg := resultText(res)
			if !strings.Contains(msg, "validation") || !strings.Contains(msg, tc.want) {
				t.Errorf("error %q does not name a validation failure containing %q", msg, tc.want)
			}
			if svc.filteredCalls != 0 || svc.listCalls != 0 {
				t.Errorf("the service was called for a refused condition")
			}
		})
	}
}

func TestResourceList_SchemaPropertyShadowingAStandardColumnTargetsTheDocument(t *testing.T) {
	t.Parallel()
	svc, res := callResourceList(t, taskSchema,
		`{"type_slug":"task","filters":[{"field":"status","operator":"eq","value":"open"}]}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if len(svc.gotFilters) != 1 || !svc.gotFilters[0].DocumentProperty {
		t.Errorf("filters = %+v; want one condition marked DocumentProperty", svc.gotFilters)
	}
}

func TestResourceList_StandardColumnNotInSchemaTargetsTheColumn(t *testing.T) {
	t.Parallel()
	svc, res := callResourceList(t, `{"type":"object","properties":{"name":{"type":"string"}}}`,
		`{"type_slug":"note","filters":[{"field":"status","operator":"eq","value":"active"}]}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if len(svc.gotFilters) != 1 || svc.gotFilters[0].DocumentProperty {
		t.Errorf("filters = %+v; want one column condition", svc.gotFilters)
	}
}

// TestResourceList_SchemalessTypeSkipsTheFieldCheck pins that a type with no
// schema is not refused a field, and that the condition still filters: its
// projection has only the standard columns, so any other field is read from
// the stored document rather than dropped for want of a column.
func TestResourceList_SchemalessTypeSkipsTheFieldCheck(t *testing.T) {
	t.Parallel()
	svc, res := callResourceList(t, "", `{"type_slug":"scratchpad","filters":[`+
		`{"field":"topic","operator":"eq","value":"pricing"},{"field":"status","operator":"eq","value":"active"}]}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if svc.filteredCalls != 1 || len(svc.gotFilters) != 2 {
		t.Fatalf("ListWithFilters calls = %d with %+v, want 1 call with 2 conditions", svc.filteredCalls, svc.gotFilters)
	}
	if !svc.gotFilters[0].DocumentProperty {
		t.Errorf("topic condition = %+v; want it marked DocumentProperty", svc.gotFilters[0])
	}
	if svc.gotFilters[1].DocumentProperty {
		t.Errorf("status condition = %+v; want the standard column", svc.gotFilters[1])
	}
}

func TestResourceList_NumberAndBooleanValuesAreFormatted(t *testing.T) {
	t.Parallel()
	svc, res := callResourceList(t,
		`{"type":"object","properties":{"amountDue":{"type":"number"},"paid":{"type":"boolean"}}}`,
		`{"type_slug":"invoice","filters":[{"field":"amountDue","operator":"gte","value":1000.5},`+
			`{"field":"paid","operator":"eq","value":true},{"field":"paid","operator":"ne","value":false}]}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	got := make([]string, 0, len(svc.gotFilters))
	for _, f := range svc.gotFilters {
		got = append(got, f.Value)
	}
	if strings.Join(got, ",") != "1000.5,1,0" {
		t.Errorf("values = %v, want [1000.5 1 0]", got)
	}
}

// questionSchema declares an integer and a boolean property whose names
// standard columns shadow, so the repository reads both from the document.
const questionSchema = `{"type":"object","properties":{"name":{"type":"string"},` +
	`"sequenceNo":{"type":"integer"},"status":{"type":"boolean"},"marks":{"type":["number","null"]}}}`

func TestResourceList_ValueTypeFollowsTheDeclaredPropertyType(t *testing.T) {
	t.Parallel()
	svc, res := callResourceList(t, questionSchema, `{"type_slug":"question","filters":[`+
		`{"field":"sequenceNo","operator":"eq","value":3},`+
		`{"field":"sequenceNo","operator":"gt","value":"12"},`+
		`{"field":"status","operator":"eq","value":true},`+
		`{"field":"status","operator":"ne","value":"false"},`+
		`{"field":"marks","operator":"gte","value":2.5},`+
		`{"field":"name","operator":"eq","value":42},`+
		`{"field":"createdBy","operator":"eq","value":"dana"}]}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	type typed struct {
		value string
		vt    repositories.FilterValueType
		doc   bool
	}
	want := []typed{
		{"3", repositories.FilterValueNumber, true},
		{"12", repositories.FilterValueNumber, true},
		{"1", repositories.FilterValueBoolean, true},
		{"0", repositories.FilterValueBoolean, true},
		{"2.5", repositories.FilterValueNumber, false},
		{"42", repositories.FilterValueString, false},
		{"dana", repositories.FilterValueString, false},
	}
	if len(svc.gotFilters) != len(want) {
		t.Fatalf("filters = %+v, want %d conditions", svc.gotFilters, len(want))
	}
	for i, w := range want {
		f := svc.gotFilters[i]
		if got := (typed{f.Value, f.ValueType, f.DocumentProperty}); got != w {
			t.Errorf("condition %d on %q = %+v, want %+v", i+1, f.Field, got, w)
		}
	}
}

func TestResourceList_ValueTheDeclaredTypeCannotHoldIsRefused(t *testing.T) {
	t.Parallel()
	cases := []struct{ filter, want string }{
		{`{"field":"sequenceNo","operator":"eq","value":"three"}`, "value must be a number"},
		{`{"field":"sequenceNo","operator":"eq","value":true}`, "value must be a number"},
		{`{"field":"status","operator":"eq","value":"maybe"}`, "value must be a boolean"},
		{`{"field":"status","operator":"eq","value":1}`, "value must be a boolean"},
	}
	for _, tc := range cases {
		svc, res := callResourceList(t, questionSchema, `{"type_slug":"question","filters":[`+tc.filter+`]}`)
		if !res.IsError {
			t.Errorf("%s: expected a tool error, got: %s", tc.filter, resultText(res))
			continue
		}
		if msg := resultText(res); !strings.Contains(msg, "filter validation failed") || !strings.Contains(msg, tc.want) {
			t.Errorf("%s: error %q does not contain %q", tc.filter, msg, tc.want)
		}
		if svc.filteredCalls != 0 {
			t.Errorf("%s: the service was called for a refused condition", tc.filter)
		}
	}
}

// TestResourceList_NeIncludesMissing pins that ne from the tool also matches
// a resource with no value: "status ne done" must keep a task nobody has set a
// status on.
func TestResourceList_NeIncludesMissing(t *testing.T) {
	t.Parallel()
	svc, res := callResourceList(t, taskSchema, `{"type_slug":"task","filters":[`+
		`{"field":"priority","operator":"ne","value":"low"},{"field":"status","operator":"ne","value":"done"}]}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	for _, f := range svc.gotFilters {
		if !f.NeIncludesMissing {
			t.Errorf("condition on %q does not include missing values: %+v", f.Field, f)
		}
	}
	if !strings.Contains(resourceListDescription, "ne also matches") {
		t.Errorf("the tool description does not say that ne matches a resource with no value")
	}
}

// listSchema declares an array and an object property, which the projection
// stores as JSON text, beside a scalar and a list-valued reference.
const listSchema = `{"type":"object","properties":{"name":{"type":"string"},` +
	`"tags":{"type":"array","items":{"type":"string"}},"address":{"type":"object"},` +
	`"suppliers":{"type":"array","x-resource-type":"vendor","items":{"type":"string"}},` +
	`"project":{"type":"string","x-resource-type":"project"}}}`

// TestResourceList_ArrayOrObjectPropertyIsRefused pins that a condition on a
// property stored as JSON text is refused: eq on one element never matches
// the whole array, so the listing would come back empty with no word why.
func TestResourceList_ArrayOrObjectPropertyIsRefused(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"tags", "address"} {
		svc, res := callResourceList(t, listSchema,
			`{"type_slug":"task","filters":[{"field":"`+field+`","operator":"eq","value":"urgent"}]}`)
		if !res.IsError {
			t.Errorf("%s: expected a tool error, got: %s", field, resultText(res))
			continue
		}
		msg := resultText(res)
		for _, want := range []string{"filter validation failed", `"` + field + `" is not filterable`,
			"filterable fields: name, project, suppliers, id, status, createdAt, createdBy, accountId, sequenceNo, updatedAt"} {
			if !strings.Contains(msg, want) {
				t.Errorf("%s: error %q does not contain %q", field, msg, want)
			}
		}
		if svc.filteredCalls != 0 {
			t.Errorf("%s: the service was called for a refused condition", field)
		}
	}
}

// TestResourceList_ListValuedReferenceIsFilterable pins the exception: a
// reference property may hold a JSON array of identifiers, and eq on it
// matches a resource whose array holds the value.
func TestResourceList_ListValuedReferenceIsFilterable(t *testing.T) {
	t.Parallel()
	svc, res := callResourceList(t, listSchema,
		`{"type_slug":"task","filters":[{"field":"suppliers","operator":"eq","value":"urn:vendor:1"}]}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if len(svc.gotFilters) != 1 || svc.gotFilters[0].DocumentProperty ||
		svc.gotFilters[0].ValueType != repositories.FilterValueString {
		t.Errorf("filters = %+v; want one string column condition", svc.gotFilters)
	}
}

// TestResourceList_DescriptionSaysToResendTheFilters pins the guidance a
// paged, filtered listing depends on: the cursor does not carry the filters,
// so a model that sends the cursor alone reads unfiltered rows.
func TestResourceList_DescriptionSaysToResendTheFilters(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"send the cursor together with the same filters", "does not carry them"} {
		if !strings.Contains(resourceListDescription, want) {
			t.Errorf("the resource_list description does not say %q", want)
		}
	}
}

func TestResourceList_NoFiltersUsesTheUnfilteredList(t *testing.T) {
	t.Parallel()
	for _, args := range []string{`{"type_slug":"task"}`, `{"type_slug":"task","filters":[]}`} {
		svc, res := callResourceList(t, taskSchema, args)
		if res.IsError {
			t.Fatalf("%s: unexpected tool error: %s", args, resultText(res))
		}
		if svc.listCalls != 1 || svc.filteredCalls != 0 {
			t.Errorf("%s: List calls = %d, ListWithFilters calls = %d; want 1 and 0",
				args, svc.listCalls, svc.filteredCalls)
		}
	}
}

// Compile-time check that the recording stubs still satisfy the interfaces.
var (
	_ application.ResourceService     = (*listRecordingResources)(nil)
	_ application.ResourceTypeService = (*schemaTypes)(nil)
)
