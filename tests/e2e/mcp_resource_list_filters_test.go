package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/application"

	"github.com/cucumber/godog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPResourceListFilters runs the resource_list filter acceptance
// scenarios (epic wm-xtpky, story wm-xtpky.1): conditions on projection
// columns narrow the listing, and a condition the projection cannot apply is
// refused with the list of what it would accept instead of being dropped.
// Filter on demand with:
// GODOG_TAGS=@wip go test ./tests/e2e/ -run TestMCPResourceListFilters -v
func TestMCPResourceListFilters(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "mcp-resource-list-filters",
		ScenarioInitializer: initResourceListFiltersScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/resource_list_filters.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("resource_list filter acceptance scenarios failed")
	}
}

// listFiltersWorld extends the shared MCP world with seeding by table and the
// state a filtered, paged listing needs between steps.
type listFiltersWorld struct {
	*mcpWorld
	// ids maps a seeded resource's name to its identifier, so a step can name
	// a referenced resource the way a person would.
	ids map[string]string
	// lastFilters is the raw filters array of the most recent filtered call,
	// re-sent with the cursor when the next page is requested.
	lastFilters string
	lastType    string
	firstPage   []string
}

func initResourceListFiltersScenario(sc *godog.ScenarioContext) {
	w := &listFiltersWorld{mcpWorld: &mcpWorld{}, ids: map[string]string{}}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a clean WeOS knowledge graph$`, w.aCleanKnowledgeGraph)
	sc.Step(`^the "([^"]*)" preset is installed$`, w.presetIsInstalled)

	sc.Step(`^the following "([^"]*)" resources exist:$`, w.theFollowingResourcesExist)
	sc.Step(`^a "([^"]*)" resource named "([^"]*)" exists$`, w.aResourceNamedExists)
	sc.Step(`^(\d+) "([^"]*)" resources with status "([^"]*)" exist$`, w.resourcesWithStatusExist)
	sc.Step(`^a resource type "([^"]*)" exists with a numeric "([^"]*)" property$`, w.typeWithNumericProperty)
	sc.Step(`^a resource type "([^"]*)" exists with a boolean "([^"]*)" property$`, w.typeWithBooleanProperty)
	sc.Step(`^a resource type "([^"]*)" exists with no JSON Schema$`, w.typeWithNoSchema)
	sc.Step(`^I have listed the first page of "([^"]*)" resources with "([^"]*)" equal to "([^"]*)"$`,
		w.iHaveListedTheFirstPage)

	sc.Step(`^I call resource_list for type "([^"]*)"$`, w.iCallResourceListUnfiltered)
	sc.Step(`^I call resource_list for type "([^"]*)" with the filters:$`, w.iCallResourceListWithTable)
	sc.Step(`^I call resource_list for type "([^"]*)" with the JSON filters:$`, w.iCallResourceListWithJSON)
	sc.Step(`^I call resource_list for type "([^"]*)" with an "([^"]*)" condition on "([^"]*)" matching "([^"]*)"$`,
		w.iCallResourceListMatchingReference)
	sc.Step(`^I call resource_list with the cursor from the first page and the same filters$`,
		w.iCallResourceListWithTheCursor)

	sc.Step(`^the call succeeds$`, w.theCallSucceeds)
	sc.Step(`^the call fails with a validation error$`, w.theCallFailsWithValidationError)
	sc.Step(`^the listing contains exactly these resources:$`, w.listingContainsExactlyTable)
	sc.Step(`^the listing contains exactly the resources named "([^"]*)"$`, w.listingContainsExactlyNamed)
	sc.Step(`^the listing returns (\d+) resources$`, w.listingReturnsN)
	sc.Step(`^the listing returns no resources$`, w.listingReturnsNone)
	sc.Step(`^no resources are returned$`, w.noResourcesAreReturned)
	sc.Step(`^every returned resource has status "([^"]*)"$`, w.everyReturnedResourceHasStatus)
	sc.Step(`^the listing provides a cursor for the next page$`, w.listingProvidesCursor)
	sc.Step(`^no resource from the first page is repeated$`, w.noResourceFromFirstPageRepeated)
	sc.Step(`^the error names the field "([^"]*)" as not filterable$`, w.errorNamesFieldNotFilterable)
	sc.Step(`^the error lists these filterable fields:$`, w.errorListsFilterableFields)
	sc.Step(`^the error names the supported operators:$`, w.errorNamesSupportedOperators)
	sc.Step(`^the error states that a condition value must be a string, a number or a boolean$`,
		w.errorStatesValueMustBeScalar)
	sc.Step(`^the tool does not refuse the condition as an unfilterable field$`, w.toolDoesNotRefuseField)
}

// --- Given ---

// theFollowingResourcesExist seeds one resource per row. Each cell is typed
// by the property's declared JSON Schema type, so a numeric or boolean column
// is stored as a JSON number or boolean. Two columns are special: `project`
// holds the name of a seeded project and is stored as its identifier, and
// `created-at` backdates the resource's createdAt column. Empty cells are
// left out of the document.
func (w *listFiltersWorld) theFollowingResourcesExist(ctx context.Context, typeSlug string, table *godog.Table) error {
	if len(table.Rows) < 2 {
		return fmt.Errorf("the table needs a header row and at least one resource")
	}
	types, err := w.propertyTypes(ctx, typeSlug)
	if err != nil {
		return err
	}
	header := table.Rows[0].Cells
	for _, row := range table.Rows[1:] {
		data := map[string]any{}
		var createdAt string
		for i, cell := range row.Cells {
			col, val := header[i].Value, cell.Value
			if val == "" {
				continue
			}
			switch col {
			case "created-at":
				createdAt = val
				continue
			case "project":
				id, ok := w.ids[val]
				if !ok {
					return fmt.Errorf("no seeded resource named %q to reference", val)
				}
				data[col] = id
				continue
			}
			typed, err := typedCell(types[col], val)
			if err != nil {
				return fmt.Errorf("column %q: %w", col, err)
			}
			data[col] = typed
		}
		id, err := w.createResource(ctx, typeSlug, data)
		if err != nil {
			return err
		}
		if createdAt != "" {
			if err := w.backdate(typeSlug, id, createdAt); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *listFiltersWorld) aResourceNamedExists(ctx context.Context, typeSlug, name string) error {
	_, err := w.createResource(ctx, typeSlug, map[string]any{"name": name})
	return err
}

func (w *listFiltersWorld) resourcesWithStatusExist(ctx context.Context, n int, typeSlug, status string) error {
	for i := 1; i <= n; i++ {
		name := fmt.Sprintf("Follow up with supplier %02d (%s)", i, status)
		if _, err := w.createResource(ctx, typeSlug, map[string]any{"name": name, "status": status}); err != nil {
			return err
		}
	}
	return nil
}

func (w *listFiltersWorld) typeWithNumericProperty(ctx context.Context, slug, prop string) error {
	return w.createType(ctx, slug, fmt.Sprintf(
		`{"type":"object","properties":{"name":{"type":"string"},%q:{"type":"number"}},"required":["name"]}`, prop))
}

func (w *listFiltersWorld) typeWithBooleanProperty(ctx context.Context, slug, prop string) error {
	return w.createType(ctx, slug, fmt.Sprintf(
		`{"type":"object","properties":{"name":{"type":"string"},%q:{"type":"boolean"}},"required":["name"]}`, prop))
}

func (w *listFiltersWorld) typeWithNoSchema(ctx context.Context, slug string) error {
	return w.createType(ctx, slug, "")
}

func (w *listFiltersWorld) iHaveListedTheFirstPage(ctx context.Context, typeSlug, field, value string) error {
	filters := fmt.Sprintf(`[{"field":%q,"operator":"eq","value":%q}]`, field, value)
	w.callList(ctx, typeSlug, filters, "")
	if err := w.theCallSucceeds(); err != nil {
		return err
	}
	out, err := w.listing()
	if err != nil {
		return err
	}
	w.firstPage = w.firstPage[:0]
	for _, item := range out.Data {
		w.firstPage = append(w.firstPage, item.ID)
	}
	if out.Cursor == "" {
		return fmt.Errorf("the first page carried no cursor: %s", w.lastText)
	}
	return nil
}

// --- When ---

// iCallResourceListUnfiltered sends no filters and no limit, so the call is
// exactly the one a client made before filters existed.
func (w *listFiltersWorld) iCallResourceListUnfiltered(ctx context.Context, typeSlug string) error {
	w.callList(ctx, typeSlug, "", "")
	return nil
}

func (w *listFiltersWorld) iCallResourceListWithTable(ctx context.Context, typeSlug string, table *godog.Table) error {
	conds := make([]map[string]string, 0, len(table.Rows))
	for _, row := range table.Rows[1:] {
		conds = append(conds, map[string]string{
			"field": row.Cells[0].Value, "operator": row.Cells[1].Value, "value": row.Cells[2].Value,
		})
	}
	raw, err := json.Marshal(conds)
	if err != nil {
		return err
	}
	w.callList(ctx, typeSlug, string(raw), "")
	return nil
}

func (w *listFiltersWorld) iCallResourceListWithJSON(ctx context.Context, typeSlug string, doc *godog.DocString) error {
	w.callList(ctx, typeSlug, strings.TrimSpace(doc.Content), "")
	return nil
}

func (w *listFiltersWorld) iCallResourceListMatchingReference(
	ctx context.Context, typeSlug, operator, field, name string,
) error {
	id, ok := w.ids[name]
	if !ok {
		return fmt.Errorf("no seeded resource named %q", name)
	}
	w.callList(ctx, typeSlug, fmt.Sprintf(`[{"field":%q,"operator":%q,"value":%q}]`, field, operator, id), "")
	return nil
}

func (w *listFiltersWorld) iCallResourceListWithTheCursor(ctx context.Context) error {
	out, err := w.listing()
	if err != nil {
		return err
	}
	w.callList(ctx, w.lastType, w.lastFilters, out.Cursor)
	return nil
}

// --- Then ---

func (w *listFiltersWorld) listingContainsExactlyTable(table *godog.Table) error {
	want := make([]string, 0, len(table.Rows))
	for _, row := range table.Rows[1:] {
		want = append(want, row.Cells[0].Value)
	}
	return w.listingNamesExactly(want)
}

func (w *listFiltersWorld) listingContainsExactlyNamed(names string) error {
	var want []string
	for _, n := range strings.Split(names, ",") {
		if n = strings.TrimSpace(n); n != "" {
			want = append(want, n)
		}
	}
	return w.listingNamesExactly(want)
}

func (w *listFiltersWorld) listingReturnsN(n int) error {
	out, err := w.listing()
	if err != nil {
		return err
	}
	if len(out.Data) != n {
		return fmt.Errorf("expected %d resources, got %d: %v", n, len(out.Data), w.names(out))
	}
	return nil
}

func (w *listFiltersWorld) listingReturnsNone() error {
	if err := w.theCallSucceeds(); err != nil {
		return err
	}
	return w.listingReturnsN(0)
}

// noResourcesAreReturned guards against the old failure: a refused call must
// carry no listing at all, not the unfiltered page beside an error.
func (w *listFiltersWorld) noResourcesAreReturned() error {
	if w.lastResult != nil && w.lastResult.StructuredContent != nil {
		raw, err := json.Marshal(w.lastResult.StructuredContent)
		if err == nil {
			var out listOutput
			if json.Unmarshal(raw, &out) == nil && len(out.Data) > 0 {
				return fmt.Errorf("expected no resources beside the refusal, got %v", w.names(out))
			}
		}
	}
	var out listOutput
	if json.Unmarshal([]byte(w.lastText), &out) == nil && len(out.Data) > 0 {
		return fmt.Errorf("expected no resources beside the refusal, got %v", w.names(out))
	}
	return nil
}

func (w *listFiltersWorld) everyReturnedResourceHasStatus(status string) error {
	out, err := w.listing()
	if err != nil {
		return err
	}
	for _, item := range out.Data {
		if got := propertyOf(item.Data, "status"); got != status {
			return fmt.Errorf("resource %q has status %q, want %q", propertyOf(item.Data, "name"), got, status)
		}
	}
	return nil
}

func (w *listFiltersWorld) listingProvidesCursor() error {
	out, err := w.listing()
	if err != nil {
		return err
	}
	if out.Cursor == "" || !out.HasMore {
		return fmt.Errorf("expected a cursor and has_more, got cursor=%q has_more=%v", out.Cursor, out.HasMore)
	}
	return nil
}

func (w *listFiltersWorld) noResourceFromFirstPageRepeated() error {
	out, err := w.listing()
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(w.firstPage))
	for _, id := range w.firstPage {
		seen[id] = true
	}
	for _, item := range out.Data {
		if seen[item.ID] {
			return fmt.Errorf("resource %q was on the first page too", propertyOf(item.Data, "name"))
		}
	}
	return nil
}

func (w *listFiltersWorld) errorNamesFieldNotFilterable(field string) error {
	msg := w.errMessage()
	if !strings.Contains(msg, fmt.Sprintf("%q", field)) || !strings.Contains(msg, "not filterable") {
		return fmt.Errorf("expected the error to name %q as not filterable, got: %s", field, msg)
	}
	return nil
}

func (w *listFiltersWorld) errorListsFilterableFields(table *godog.Table) error {
	return w.errorListsExactly("filterable fields:", table)
}

func (w *listFiltersWorld) errorNamesSupportedOperators(table *godog.Table) error {
	return w.errorListsExactly("supported operators:", table)
}

func (w *listFiltersWorld) errorStatesValueMustBeScalar() error {
	msg := w.errMessage()
	if !strings.Contains(msg, "must be a string, a number or a boolean") {
		return fmt.Errorf("expected the error to say a value must be a string, a number or a boolean, got: %s", msg)
	}
	return nil
}

// toolDoesNotRefuseField checks the call reached the tool, was not refused
// for its field, and returned a listing in which every resource meets each eq
// condition — the condition was applied, not dropped. The scenario seeds no
// resource, so the matching rows themselves are pinned by
// TestMCPResourceListFiltersSchemalessTypeFiltersTheRows below.
func (w *listFiltersWorld) toolDoesNotRefuseField() error {
	if w.lastErr != nil {
		return fmt.Errorf("the call did not reach the tool: %v", w.lastErr)
	}
	if msg := w.errMessage(); strings.Contains(msg, "not filterable") {
		return fmt.Errorf("the condition was refused as an unfilterable field: %s", msg)
	}
	out, err := w.listing()
	if err != nil {
		return err
	}
	var conds []struct {
		Field    string `json:"field"`
		Operator string `json:"operator"`
		Value    any    `json:"value"`
	}
	if err := json.Unmarshal([]byte(w.lastFilters), &conds); err != nil {
		return fmt.Errorf("the filters sent are not JSON: %w", err)
	}
	for _, item := range out.Data {
		for _, c := range conds {
			if c.Operator == "eq" && propertyOf(item.Data, c.Field) != fmt.Sprint(c.Value) {
				return fmt.Errorf("resource %q does not meet %s eq %v: the condition was dropped",
					propertyOf(item.Data, "name"), c.Field, c.Value)
			}
		}
	}
	return nil
}

// TestMCPResourceListFiltersSchemalessTypeFiltersTheRows pins what the
// scenario "A type that declares no JSON Schema is not refused a condition on
// an undeclared field" cannot: with two resources of such a type stored, the
// condition returns the one that matches and not the other (wm-iqtlo5). The
// scenario's Gherkin seeds no resource, and the contract is not edited here.
func TestMCPResourceListFiltersSchemalessTypeFiltersTheRows(t *testing.T) {
	ctx := context.Background()
	w := &listFiltersWorld{mcpWorld: &mcpWorld{}, ids: map[string]string{}}
	t.Cleanup(w.teardown)
	if err := w.aCleanKnowledgeGraph(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.typeWithNoSchema(ctx, "scratchpad"); err != nil {
		t.Fatal(err)
	}
	for _, r := range []map[string]any{
		{"name": "Tier ideas for the annual plan", "topic": "pricing"},
		{"name": "Interview notes for the support lead", "topic": "hiring"},
	} {
		if _, err := w.createResource(ctx, "scratchpad", r); err != nil {
			t.Fatal(err)
		}
	}
	w.callList(ctx, "scratchpad", `[{"field":"topic","operator":"eq","value":"pricing"}]`, "")
	if err := w.listingNamesExactly([]string{"Tier ideas for the annual plan"}); err != nil {
		t.Error(err)
	}
	w.callList(ctx, "scratchpad", `[{"field":"topic","operator":"ne","value":"pricing"}]`, "")
	if err := w.listingNamesExactly([]string{"Interview notes for the support lead"}); err != nil {
		t.Error(err)
	}
}

// --- Helpers ---

type listOutput struct {
	Data []struct {
		ID   string         `json:"id"`
		Data map[string]any `json:"data"`
	} `json:"data"`
	Cursor  string `json:"cursor"`
	HasMore bool   `json:"has_more"`
}

func (w *listFiltersWorld) callList(ctx context.Context, typeSlug, filters, cursor string) {
	args := map[string]any{"type_slug": typeSlug}
	if filters != "" {
		args["filters"] = json.RawMessage(filters)
	}
	if cursor != "" {
		args["cursor"] = cursor
	}
	raw, err := json.Marshal(args)
	if err != nil {
		w.lastResult, w.lastErr, w.lastText = nil, err, ""
		return
	}
	w.lastType, w.lastFilters = typeSlug, filters
	res, err := w.client.CallTool(ctx, &mcp.CallToolParams{Name: "resource_list", Arguments: json.RawMessage(raw)})
	w.lastResult = res
	w.lastErr = err
	w.lastText = textOf(res)
}

func (w *listFiltersWorld) listing() (listOutput, error) {
	if err := w.theCallSucceeds(); err != nil {
		return listOutput{}, err
	}
	var out listOutput
	if err := json.Unmarshal([]byte(w.lastText), &out); err != nil {
		return listOutput{}, fmt.Errorf("listing is not JSON: %w (text=%s)", err, w.lastText)
	}
	return out, nil
}

func (w *listFiltersWorld) names(out listOutput) []string {
	names := make([]string, 0, len(out.Data))
	for _, item := range out.Data {
		names = append(names, propertyOf(item.Data, "name"))
	}
	sort.Strings(names)
	return names
}

func (w *listFiltersWorld) listingNamesExactly(want []string) error {
	out, err := w.listing()
	if err != nil {
		return err
	}
	got := w.names(out)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		return fmt.Errorf("expected exactly %v, got %v", want, got)
	}
	return nil
}

// errorListsExactly reads the comma-separated list that follows label in the
// error and compares it, order-insensitive, with the table's first column.
func (w *listFiltersWorld) errorListsExactly(label string, table *godog.Table) error {
	msg := w.errMessage()
	idx := strings.Index(msg, label)
	if idx < 0 {
		return fmt.Errorf("expected the error to carry %q, got: %s", label, msg)
	}
	rest := msg[idx+len(label):]
	if nl := strings.IndexAny(rest, "\n;"); nl >= 0 {
		rest = rest[:nl]
	}
	var got []string
	for _, item := range strings.Split(rest, ",") {
		if item = strings.TrimSpace(item); item != "" {
			got = append(got, item)
		}
	}
	want := make([]string, 0, len(table.Rows))
	for _, row := range table.Rows[1:] {
		want = append(want, row.Cells[0].Value)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		return fmt.Errorf("expected %s %v, got %v (error: %s)", label, want, got, msg)
	}
	return nil
}

func (w *listFiltersWorld) createType(ctx context.Context, slug, schema string) error {
	if w.rts == nil {
		return fmt.Errorf("application not booted")
	}
	cmd := application.CreateResourceTypeCommand{
		Name:        titleCase(slug),
		Slug:        slug,
		Description: "A resource type declared by the filter scenarios",
		Context:     json.RawMessage(`{"@vocab":"https://schema.org/"}`),
	}
	if schema != "" {
		cmd.Schema = json.RawMessage(schema)
	}
	if _, err := w.rts.Create(ctx, cmd); err != nil {
		return fmt.Errorf("failed to create resource type %q: %w", slug, err)
	}
	return nil
}

func (w *listFiltersWorld) createResource(ctx context.Context, typeSlug string, data map[string]any) (string, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	e, err := w.rs.Create(ctx, application.CreateResourceCommand{TypeSlug: typeSlug, Data: raw})
	if err != nil {
		return "", fmt.Errorf("failed to create %s %v: %w", typeSlug, data, err)
	}
	if name, ok := data["name"].(string); ok {
		w.ids[name] = e.GetID()
	}
	return e.GetID(), nil
}

// backdate sets a seeded resource's createdAt column. No command can create a
// resource in the past, and a scenario about createdAt needs rows on chosen
// dates, so this one fixture writes the projection row directly.
func (w *listFiltersWorld) backdate(typeSlug, id, value string) error {
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return fmt.Errorf("created-at %q is not RFC 3339: %w", value, err)
	}
	return w.db.Table(w.projMgr.TableName(typeSlug)).Where("id = ?", id).
		Update("created_at", at.UTC()).Error
}

// propertyTypes returns each declared property's JSON Schema type.
func (w *listFiltersWorld) propertyTypes(ctx context.Context, typeSlug string) (map[string]string, error) {
	rt, err := w.rts.GetBySlug(ctx, typeSlug)
	if err != nil {
		return nil, fmt.Errorf("resource type %q: %w", typeSlug, err)
	}
	var schema struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	types := map[string]string{}
	if len(rt.Schema()) == 0 {
		return types, nil
	}
	if err := json.Unmarshal(rt.Schema(), &schema); err != nil {
		return nil, fmt.Errorf("resource type %q schema: %w", typeSlug, err)
	}
	for name, p := range schema.Properties {
		types[name] = p.Type
	}
	return types, nil
}

func typedCell(jsonType, val string) (any, error) {
	switch jsonType {
	case "number", "integer":
		return strconv.ParseFloat(val, 64)
	case "boolean":
		return strconv.ParseBool(val)
	default:
		return val, nil
	}
}

// propertyOf reads a property from a listed resource's JSON-LD document,
// which holds it on the entity node of an @graph or, for a legacy record, at
// the top level.
func propertyOf(doc map[string]any, key string) string {
	if graph, ok := doc["@graph"].([]any); ok && len(graph) > 0 {
		if node, ok := graph[0].(map[string]any); ok {
			if v, ok := node[key]; ok {
				return fmt.Sprint(v)
			}
		}
	}
	if v, ok := doc[key]; ok {
		return fmt.Sprint(v)
	}
	return ""
}
