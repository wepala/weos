package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/application"

	"github.com/cucumber/godog"
)

// TestMCPResourceListFlatRows runs the resource_list flat-rows acceptance
// scenarios (epic wm-xtpky, story wm-xtpky.2): asking for flat rows answers
// with projection rows instead of JSON-LD documents, pages and filters the
// same way, and a type with no projection table answers from the entity path.
// Filter on demand with:
// GODOG_TAGS=@wip go test ./tests/e2e/ -run TestMCPResourceListFlatRows -v
func TestMCPResourceListFlatRows(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "mcp-resource-list-flat-rows",
		ScenarioInitializer: initResourceListFlatRowsScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/resource_list_flat_rows.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("resource_list flat-rows acceptance scenarios failed")
	}
}

func initResourceListFlatRowsScenario(sc *godog.ScenarioContext) {
	w := &listFiltersWorld{mcpWorld: &mcpWorld{}, ids: map[string]string{}}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	// Reused from the filter story's library.
	sc.Step(`^a clean WeOS knowledge graph$`, w.aCleanKnowledgeGraph)
	sc.Step(`^the "([^"]*)" preset is installed$`, w.presetIsInstalled)
	sc.Step(`^the following "([^"]*)" resources exist:$`, w.theFollowingResourcesExist)
	sc.Step(`^(\d+) "([^"]*)" resources with status "([^"]*)" exist$`, w.resourcesWithStatusExist)
	sc.Step(`^I call resource_list for type "([^"]*)"$`, w.iCallResourceListUnfiltered)
	sc.Step(`^the call succeeds$`, w.theCallSucceeds)
	sc.Step(`^the call fails with a validation error$`, w.theCallFailsWithValidationError)
	sc.Step(`^the error names the field "([^"]*)" as not filterable$`, w.errorNamesFieldNotFilterable)
	sc.Step(`^the listing contains exactly these resources:$`, w.listingContainsExactlyTable)
	sc.Step(`^the listing provides a cursor for the next page$`, w.listingProvidesCursor)

	sc.Step(`^a resource type "([^"]*)" exists with no projection table$`, w.typeWithNoProjectionTable)
	sc.Step(`^the "([^"]*)" type declares a "([^"]*)" property$`, w.typeDeclaresProperty)
	sc.Step(`^I have listed the first page of "([^"]*)" flat rows$`, w.iHaveListedTheFirstFlatPage)
	sc.Step(`^I have listed the first page of "([^"]*)" flat rows with "([^"]*)" equal to "([^"]*)"$`,
		w.iHaveListedTheFirstFlatPageWith)

	sc.Step(`^I call resource_list for type "([^"]*)" asking for flat rows$`, w.iCallResourceListFlat)
	sc.Step(`^I call resource_list for type "([^"]*)" asking for flat rows with the filters:$`,
		w.iCallResourceListFlatWithTable)
	sc.Step(`^I call resource_list for type "([^"]*)" with flat rows turned off$`, w.iCallResourceListFlatOff)
	sc.Step(`^I call resource_list with the cursor from the first page asking for flat rows$`,
		w.iCallResourceListFlatWithTheCursor)
	sc.Step(`^I call resource_list with the cursor from the first page and the same filters asking for flat rows$`,
		w.iCallResourceListFlatWithTheCursor)

	sc.Step(`^the listing reports its results as rows$`, w.listingReportsRows)
	sc.Step(`^the listing reports no JSON-LD documents$`, w.listingReportsNoDocuments)
	sc.Step(`^the listing reports JSON-LD documents rather than rows$`, w.listingReportsDocumentsNotRows)
	sc.Step(`^the listing returns no rows$`, w.listingReturnsNoRows)
	sc.Step(`^the listing returns (\d+) rows$`, w.listingReturnsNRows)
	sc.Step(`^the listing contains exactly these rows:$`, w.listingContainsExactlyRows)
	sc.Step(`^the row for "([^"]*)" carries these column values:$`, w.rowCarriesColumnValues)
	sc.Step(`^every returned row carries these columns:$`, w.everyRowCarriesColumns)
	sc.Step(`^every returned row has status "([^"]*)"$`, w.everyRowHasStatus)
	sc.Step(`^no row from the first page is repeated$`, w.noRowFromFirstPageRepeated)
	sc.Step(`^no rows are returned$`, w.noRowsAreReturned)
}

var flatOn, flatOff = true, false

// --- Given ---

// typeWithNoProjectionTable declares a type whose projection table cannot be
// created, which is the state the entity-path fallback exists for. Every type
// the application creates normally gets a table, so a view is put where the
// table would go first: CREATE TABLE IF NOT EXISTS then leaves the view alone,
// adding the base columns to it fails, and the projection manager reports no
// table for the type from then on. The type row itself is still stored before
// the projection is attempted, so the type exists and resources of it are kept
// in the canonical store only.
func (w *listFiltersWorld) typeWithNoProjectionTable(ctx context.Context, slug string) error {
	table := w.projMgr.TableName(slug)
	if err := w.db.Exec(fmt.Sprintf(`CREATE VIEW %q AS SELECT '' AS id WHERE 0`, table)).Error; err != nil {
		return fmt.Errorf("failed to hold the projection table name %q: %w", table, err)
	}
	// The create reports the failed projection; the type is stored anyway,
	// and the checks below are what this fixture promises.
	createErr := w.createType(ctx, slug,
		`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)
	if rt, err := w.rts.GetBySlug(ctx, slug); err != nil || rt == nil {
		return fmt.Errorf("resource type %q was not stored (create: %v, lookup: %v)", slug, createErr, err)
	}
	if w.projMgr.HasProjectionTable(slug) {
		return fmt.Errorf("resource type %q has a projection table; the fixture needs one without", slug)
	}
	return nil
}

// typeDeclaresProperty adds a string property to a type's schema.
func (w *listFiltersWorld) typeDeclaresProperty(ctx context.Context, slug, prop string) error {
	rt, err := w.rts.GetBySlug(ctx, slug)
	if err != nil {
		return fmt.Errorf("resource type %q: %w", slug, err)
	}
	var schema map[string]any
	if err := json.Unmarshal(rt.Schema(), &schema); err != nil {
		return fmt.Errorf("resource type %q schema: %w", slug, err)
	}
	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
		schema["properties"] = props
	}
	props[prop] = map[string]any{"type": "string"}
	raw, err := json.Marshal(schema)
	if err != nil {
		return err
	}
	// As at create, a type with no projection table reports the failed
	// projection on update; the stored schema is what is checked.
	updateErr := func() error {
		_, err := w.rts.Update(ctx, application.UpdateResourceTypeCommand{
			ID: rt.GetID(), Name: rt.Name(), Slug: slug, Description: rt.Description(),
			Context: rt.Context(), Schema: raw, Status: rt.Status(),
		})
		return err
	}()
	types, err := w.propertyTypes(ctx, slug)
	if err != nil {
		return err
	}
	if _, ok := types[prop]; !ok {
		return fmt.Errorf("resource type %q does not declare %q after the update (update: %v)", slug, prop, updateErr)
	}
	return nil
}

func (w *listFiltersWorld) iHaveListedTheFirstFlatPage(ctx context.Context, typeSlug string) error {
	w.callListAs(ctx, typeSlug, "", "", &flatOn)
	return w.rememberFirstFlatPage()
}

func (w *listFiltersWorld) iHaveListedTheFirstFlatPageWith(ctx context.Context, typeSlug, field, value string) error {
	w.callListAs(ctx, typeSlug, fmt.Sprintf(`[{"field":%q,"operator":"eq","value":%q}]`, field, value), "", &flatOn)
	return w.rememberFirstFlatPage()
}

func (w *listFiltersWorld) rememberFirstFlatPage() error {
	out, err := w.listing()
	if err != nil {
		return err
	}
	if out.Rows == nil {
		return fmt.Errorf("the first page carried no rows: %s", w.lastText)
	}
	w.firstPage = w.firstPage[:0]
	for _, row := range out.Rows {
		w.firstPage = append(w.firstPage, fmt.Sprint(row["id"]))
	}
	if out.Cursor == "" {
		return fmt.Errorf("the first page carried no cursor: %s", w.lastText)
	}
	return nil
}

// --- When ---

func (w *listFiltersWorld) iCallResourceListFlat(ctx context.Context, typeSlug string) error {
	w.callListAs(ctx, typeSlug, "", "", &flatOn)
	return nil
}

func (w *listFiltersWorld) iCallResourceListFlatOff(ctx context.Context, typeSlug string) error {
	w.callListAs(ctx, typeSlug, "", "", &flatOff)
	return nil
}

func (w *listFiltersWorld) iCallResourceListFlatWithTable(
	ctx context.Context, typeSlug string, table *godog.Table,
) error {
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
	w.callListAs(ctx, typeSlug, string(raw), "", &flatOn)
	return nil
}

// iCallResourceListFlatWithTheCursor re-sends whatever filters the first page
// was listed with, which is none for an unfiltered first page.
func (w *listFiltersWorld) iCallResourceListFlatWithTheCursor(ctx context.Context) error {
	out, err := w.listing()
	if err != nil {
		return err
	}
	w.callListAs(ctx, w.lastType, w.lastFilters, out.Cursor, &flatOn)
	return nil
}

// --- Then ---

// answerKeys returns the top-level keys of the listing answer.
func (w *listFiltersWorld) answerKeys() (map[string]json.RawMessage, error) {
	if err := w.theCallSucceeds(); err != nil {
		return nil, err
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(w.lastText), &keys); err != nil {
		return nil, fmt.Errorf("listing is not a JSON object: %w (text=%s)", err, w.lastText)
	}
	return keys, nil
}

func (w *listFiltersWorld) listingReportsRows() error {
	keys, err := w.answerKeys()
	if err != nil {
		return err
	}
	raw, ok := keys["rows"]
	var rows []map[string]any
	if !ok || json.Unmarshal(raw, &rows) != nil || rows == nil {
		return fmt.Errorf("expected the listing to carry a rows array, got: %s", w.lastText)
	}
	return nil
}

// listingReportsNoDocuments checks the answer carries no data array and that
// no row is a JSON-LD document in disguise.
func (w *listFiltersWorld) listingReportsNoDocuments() error {
	keys, err := w.answerKeys()
	if err != nil {
		return err
	}
	if raw, ok := keys["data"]; ok && string(raw) != "null" {
		return fmt.Errorf("expected no JSON-LD documents, got data: %s", raw)
	}
	out, err := w.listing()
	if err != nil {
		return err
	}
	for _, row := range out.Rows {
		for _, k := range []string{"@context", "@graph", "@id", "data"} {
			if _, ok := row[k]; ok {
				return fmt.Errorf("row %q carries the JSON-LD key %q", row["name"], k)
			}
		}
	}
	return nil
}

func (w *listFiltersWorld) listingReportsDocumentsNotRows() error {
	keys, err := w.answerKeys()
	if err != nil {
		return err
	}
	if raw, ok := keys["rows"]; ok && string(raw) != "null" {
		return fmt.Errorf("expected JSON-LD documents rather than rows, got rows: %s", raw)
	}
	out, err := w.listing()
	if err != nil {
		return err
	}
	if len(out.Data) == 0 {
		return fmt.Errorf("expected JSON-LD documents, got none: %s", w.lastText)
	}
	for _, item := range out.Data {
		_, hasContext := item.Data["@context"]
		_, hasGraph := item.Data["@graph"]
		if !hasContext && !hasGraph {
			return fmt.Errorf("resource %q is not a JSON-LD document: %v", item.ID, item.Data)
		}
	}
	return nil
}

func (w *listFiltersWorld) listingReturnsNoRows() error {
	if err := w.listingReportsRows(); err != nil {
		return err
	}
	return w.listingReturnsNRows(0)
}

func (w *listFiltersWorld) listingReturnsNRows(n int) error {
	out, err := w.listing()
	if err != nil {
		return err
	}
	if out.Rows == nil {
		return fmt.Errorf("expected %d rows, got an answer with no rows: %s", n, w.lastText)
	}
	if len(out.Rows) != n {
		return fmt.Errorf("expected %d rows, got %d: %v", n, len(out.Rows), rowNames(out.Rows))
	}
	return nil
}

func (w *listFiltersWorld) listingContainsExactlyRows(table *godog.Table) error {
	out, err := w.listing()
	if err != nil {
		return err
	}
	want := make([]string, 0, len(table.Rows))
	for _, row := range table.Rows[1:] {
		want = append(want, row.Cells[0].Value)
	}
	sort.Strings(want)
	got := rowNames(out.Rows)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		return fmt.Errorf("expected exactly the rows %v, got %v", want, got)
	}
	return nil
}

func (w *listFiltersWorld) rowCarriesColumnValues(name string, table *godog.Table) error {
	out, err := w.listing()
	if err != nil {
		return err
	}
	var row map[string]any
	for _, r := range out.Rows {
		if fmt.Sprint(r["name"]) == name {
			row = r
			break
		}
	}
	if row == nil {
		return fmt.Errorf("no row named %q among %v", name, rowNames(out.Rows))
	}
	for _, line := range table.Rows[1:] {
		col, want := line.Cells[0].Value, line.Cells[1].Value
		got, ok := row[col]
		if !ok {
			return fmt.Errorf("row %q has no column %q: %v", name, col, row)
		}
		if fmt.Sprint(got) != want {
			return fmt.Errorf("row %q has %s %q, want %q", name, col, fmt.Sprint(got), want)
		}
	}
	return nil
}

func (w *listFiltersWorld) everyRowCarriesColumns(table *godog.Table) error {
	out, err := w.listing()
	if err != nil {
		return err
	}
	if len(out.Rows) == 0 {
		return fmt.Errorf("expected rows to check, got none: %s", w.lastText)
	}
	for _, row := range out.Rows {
		for _, line := range table.Rows[1:] {
			if _, ok := row[line.Cells[0].Value]; !ok {
				return fmt.Errorf("row %q has no column %q: %v", row["name"], line.Cells[0].Value, row)
			}
		}
	}
	return nil
}

func (w *listFiltersWorld) everyRowHasStatus(status string) error {
	out, err := w.listing()
	if err != nil {
		return err
	}
	for _, row := range out.Rows {
		if got := fmt.Sprint(row["status"]); got != status {
			return fmt.Errorf("row %q has status %q, want %q", row["name"], got, status)
		}
	}
	return nil
}

func (w *listFiltersWorld) noRowFromFirstPageRepeated() error {
	out, err := w.listing()
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(w.firstPage))
	for _, id := range w.firstPage {
		seen[id] = true
	}
	for _, row := range out.Rows {
		if seen[fmt.Sprint(row["id"])] {
			return fmt.Errorf("row %q was on the first page too", row["name"])
		}
	}
	return nil
}

// noRowsAreReturned checks a refused call carries no rows beside the error.
func (w *listFiltersWorld) noRowsAreReturned() error {
	if w.lastResult != nil && w.lastResult.StructuredContent != nil {
		raw, err := json.Marshal(w.lastResult.StructuredContent)
		if err == nil {
			var out listOutput
			if json.Unmarshal(raw, &out) == nil && len(out.Rows) > 0 {
				return fmt.Errorf("expected no rows beside the refusal, got %v", rowNames(out.Rows))
			}
		}
	}
	var out listOutput
	if json.Unmarshal([]byte(w.lastText), &out) == nil && len(out.Rows) > 0 {
		return fmt.Errorf("expected no rows beside the refusal, got %v", rowNames(out.Rows))
	}
	return nil
}

func rowNames(rows []map[string]any) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, fmt.Sprint(row["name"]))
	}
	sort.Strings(names)
	return names
}
