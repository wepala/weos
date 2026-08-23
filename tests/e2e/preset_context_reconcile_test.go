// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/cucumber/godog"
	"go.uber.org/fx"
	"gorm.io/gorm"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/pkg/jsonld"
)

// TestPresetContextReconcile is the acceptance suite for issue #510: a built-in
// preset that gains a REFERENCE property must reach a database provisioned by
// an older build with its `@context` entry, not just its projection column.
//
// Issue #379's reconcile gave the column. Without the context entry the edge
// has no reverse mapping, FlattenGraph skips it, and the write is dropped while
// the API answers 201 — which is why this suite asserts round-trips, not just
// stored shape.
func TestPresetContextReconcile(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "preset-context-reconcile",
		ScenarioInitializer: initPresetContextScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/preset_context_reconcile.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("preset_context_reconcile acceptance scenarios failed")
	}
}

// contextWorld holds one scenario's database plus the shape the "catalog"
// preset declares on the next boot. Mutating the type shapes then restarting is
// how a scenario expresses "a new build of WeOS".
type contextWorld struct {
	tmpDir string
	dsn    string

	app    *fx.App
	cancel context.CancelFunc

	rts application.ResourceTypeService
	rs  application.ResourceService
	db  *gorm.DB

	// vendorProps and widgetProps are the two types' schema properties in
	// declaration order, as the preset would declare them on the next boot.
	vendorProps []contextProperty
	widgetProps []contextProperty

	// bootReport captures what the LAST boot's reconcile reported, read back
	// from the log lines reconcilePresetSchemas emits — the operator-facing
	// surface, since ReconcilePresetResult never leaves the boot hook.
	bootReport *reconcileLog

	createdIDs map[string]string
}

// contextProperty is one schema property. references is empty for a literal and
// names the target type slug for a reference. noContextEntry reproduces a
// preset packaging bug: a reference the preset's OWN context never declares, so
// there is nothing for an additive merge to add.
type contextProperty struct {
	name           string
	jsonTyp        string
	references     string
	noContextEntry bool
}

func initPresetContextScenario(sc *godog.ScenarioContext) {
	w := &contextWorld{createdIDs: map[string]string{}}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a built-in preset "catalog" declaring a "vendor" type with the properties:$`, w.aVendorTypeDeclaring)
	sc.Step(`^the "catalog" preset also declares a "widget" type with the properties:$`, w.aWidgetTypeDeclaring)
	sc.Step(`^a clean WeOS database provisioned by that build$`, w.aCleanDatabase)
	sc.Step(`^a "vendor" named "([^"]*)" exists$`, w.aVendorExists)

	sc.Step(`^the "catalog" preset adds a "([^"]*)" reference property to "widget" targeting "([^"]*)"$`,
		w.thePresetAddsReference)
	sc.Step(`^the "catalog" preset adds a "([^"]*)" reference property to "widget" targeting "([^"]*)" `+
		`without a context entry$`, w.thePresetAddsReferenceWithoutContextEntry)
	sc.Step(`^the "catalog" preset adds a "([^"]*)" (\w+) property to "widget"$`, w.thePresetAddsLiteral)

	sc.Step(`^the operator maps "([^"]*)" to "([^"]*)" in the stored "widget" context$`, w.theOperatorMapsTerm)
	sc.Step(`^the operator clears the stored "widget" context$`, w.theOperatorClearsContext)

	sc.Step(`^the twin restarts against the same database$`, w.theTwinRestarts)
	sc.Step(`^the twin restarts against the same database again$`, w.theTwinRestarts)
	sc.Step(`^the operator reprojects the event feed$`, w.theOperatorReprojects)

	sc.Step(`^I create a "widget" named "([^"]*)" with "([^"]*)" referring to the "vendor" "([^"]*)"$`,
		w.iCreateWidgetWithReference)
	sc.Step(`^I create a "widget" named "([^"]*)" with these references:$`, w.iCreateWidgetWithReferences)
	sc.Step(`^I create a "widget" named "([^"]*)" with "([^"]*)" set to "([^"]*)"$`, w.iCreateWidgetWithLiteral)
	sc.Step(`^a "widget" named "([^"]*)" is created with an undeclared "([^"]*)" referring to the "vendor" "([^"]*)"$`,
		w.aWidgetCreatedWithUndeclaredReference)

	sc.Step(`^reading the "widget" "([^"]*)" back over the API returns "([^"]*)" as the "vendor" "([^"]*)"$`,
		w.readingReturnsReference)
	sc.Step(`^reading the "widget" "([^"]*)" back over the API returns "([^"]*)" as "([^"]*)"$`, w.readingReturnsLiteral)
	sc.Step(`^reading the "widget" "([^"]*)" back over the API returns no value for "([^"]*)"$`, w.readingReturnsNoValue)
	sc.Step(`^the JSON-LD representation of the "widget" "([^"]*)" still carries a "([^"]*)" edge `+
		`to the "vendor" "([^"]*)"$`, w.canonicalStillCarriesEdge)

	sc.Step(`^the "widget" projection table has a "([^"]*)" column$`, w.theProjectionTableHasColumn)
	sc.Step(`^the stored "widget" context has an entry for "([^"]*)"$`, w.theStoredContextHasEntry)
	sc.Step(`^the stored "widget" context has no entry for "([^"]*)"$`, w.theStoredContextHasNoEntry)
	sc.Step(`^the stored "widget" context still maps "([^"]*)" to "([^"]*)"$`, w.theStoredContextStillMaps)
	sc.Step(`^every reference property the "catalog" preset declares for "widget" has a stored context entry$`,
		w.everyReferenceHasAStoredEntry)

	sc.Step(`^no resource type update is recorded for "([^"]*)"$`, w.noUpdateRecorded)
	sc.Step(`^exactly one resource type update is recorded for "([^"]*)"$`, w.exactlyOneUpdateRecorded)

	sc.Step(`^the boot reconcile reports "([^"]*)" held at its stored definition for "([^"]*)"$`, w.bootReportsHeld)
	sc.Step(`^the boot reconcile reports "([^"]*)" as updated$`, w.bootReportsUpdated)
	sc.Step(`^the boot reconcile does not report "([^"]*)" as updated$`, w.bootDoesNotReportUpdated)
	sc.Step(`^the boot reconcile names "([^"]*)" as a property whose writes are still dropped$`, w.bootNamesDropped)
}

// --- preset shaping ---

func (w *contextWorld) aVendorTypeDeclaring(table *godog.Table) error {
	props, err := readPropertyTable(table)
	if err != nil {
		return err
	}
	w.vendorProps = props
	return nil
}

func (w *contextWorld) aWidgetTypeDeclaring(table *godog.Table) error {
	props, err := readPropertyTable(table)
	if err != nil {
		return err
	}
	w.widgetProps = props
	return nil
}

// readPropertyTable reads a | property | type | references | table. The
// references column is optional so the same reader serves a literal-only type.
func readPropertyTable(table *godog.Table) ([]contextProperty, error) {
	if len(table.Rows) == 0 {
		return nil, fmt.Errorf("property table has no header row")
	}
	header := make([]string, 0, len(table.Rows[0].Cells))
	for _, cell := range table.Rows[0].Cells {
		header = append(header, strings.TrimSpace(cell.Value))
	}
	var props []contextProperty
	for _, row := range table.Rows[1:] {
		var p contextProperty
		for i, cell := range row.Cells {
			if i >= len(header) {
				return nil, fmt.Errorf("property row has more cells than the header declares")
			}
			value := strings.TrimSpace(cell.Value)
			switch header[i] {
			case "property":
				p.name = value
			case "type":
				p.jsonTyp = value
			case "references":
				p.references = value
			default:
				return nil, fmt.Errorf("unknown property table column %q", header[i])
			}
		}
		if p.name == "" {
			return nil, fmt.Errorf("a property row declares no name")
		}
		// "reference" is the table's word for a resource reference; the stored
		// JSON type of a reference is a string holding the target's URN.
		if p.references != "" {
			p.jsonTyp = "string"
		}
		props = append(props, p)
	}
	return props, nil
}

func (w *contextWorld) addWidgetProperty(p contextProperty) error {
	for _, existing := range w.widgetProps {
		if existing.name == p.name {
			return fmt.Errorf("property %q is already declared on widget", p.name)
		}
	}
	w.widgetProps = append(w.widgetProps, p)
	return nil
}

func (w *contextWorld) thePresetAddsReference(name, target string) error {
	return w.addWidgetProperty(contextProperty{name: name, jsonTyp: "string", references: target})
}

// thePresetAddsReferenceWithoutContextEntry reproduces a preset packaging bug:
// the schema marks the property as a reference but the preset's own context
// never declares a term for it, so an additive merge has nothing to add and the
// writes stay dropped.
func (w *contextWorld) thePresetAddsReferenceWithoutContextEntry(name, target string) error {
	return w.addWidgetProperty(
		contextProperty{name: name, jsonTyp: "string", references: target, noContextEntry: true})
}

func (w *contextWorld) thePresetAddsLiteral(name, jsonTyp string) error {
	return w.addWidgetProperty(contextProperty{name: name, jsonTyp: jsonTyp})
}

// presetType renders one type's schema and context from its property list. A
// reference contributes an x-resource-type marker to the schema AND a term to
// the context — declaring one without the other is exactly the defect, so the
// two are built side by side here where the asymmetry is visible.
func presetType(name, slug string, props []contextProperty) application.PresetResourceType {
	schemaProps := make([]string, 0, len(props))
	terms := []string{`"@vocab":"https://schema.org/"`}
	for _, p := range props {
		if p.references == "" {
			schemaProps = append(schemaProps, fmt.Sprintf("%q:{\"type\":%q}", p.name, p.jsonTyp))
			continue
		}
		schemaProps = append(schemaProps,
			fmt.Sprintf("%q:{\"type\":\"string\",\"x-resource-type\":%q,\"x-display-property\":\"name\"}",
				p.name, p.references))
		if p.noContextEntry {
			continue
		}
		terms = append(terms,
			fmt.Sprintf("%q:{\"@id\":\"https://weos.org/vocab/catalog#%s\",\"@type\":\"@id\"}", p.name, p.name))
	}
	return application.PresetResourceType{
		Name:        name,
		Slug:        slug,
		Description: "issue #510 acceptance type",
		Context:     json.RawMessage("{" + strings.Join(terms, ",") + "}"),
		Schema: json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{%s}}`,
			strings.Join(schemaProps, ","))),
	}
}

// catalogRegistry builds the preset registry for the next boot from the world's
// current type shapes. AutoInstall routes it through ensureBuiltInResourceTypes
// — the startup path under test. Vendor is declared first so it exists before
// widget's references point at it.
func (w *contextWorld) catalogRegistry() *application.PresetRegistry {
	registry := application.NewPresetRegistry()
	registry.MustAdd(application.PresetDefinition{
		Name:        "catalog",
		Description: "issue #510 acceptance preset",
		AutoInstall: true,
		Types: []application.PresetResourceType{
			presetType("Vendor", "vendor", w.vendorProps),
			presetType("Widget", "widget", w.widgetProps),
		},
	})
	return registry
}

// --- boot / restart ---

// reconcileLog captures the boot reconcile's operator-facing log lines. The
// scenarios assert on what an operator would SEE, so reading the log is the
// honest surface: ReconcilePresetResult itself never leaves the boot hook.
type reconcileLog struct {
	mu sync.Mutex
	// updated, held and dropped are keyed by slug.
	updated map[string]bool
	held    map[string][]string
	dropped map[string]string
}

func newReconcileLog() *reconcileLog {
	return &reconcileLog{updated: map[string]bool{}, held: map[string][]string{}, dropped: map[string]string{}}
}

// capturingLogger records the reconcile lines and discards everything else.
type capturingLogger struct {
	inner entities.Logger
	log   *reconcileLog
}

func (c *capturingLogger) Debug(ctx context.Context, msg string, fields ...interface{}) {
	c.inner.Debug(ctx, msg, fields...)
}

func (c *capturingLogger) Info(ctx context.Context, msg string, fields ...interface{}) {
	if strings.Contains(msg, "reconciled resource type schema from preset") {
		if slug, ok := fieldValue(fields, "slug"); ok {
			c.log.mu.Lock()
			c.log.updated[slug] = true
			c.log.mu.Unlock()
		}
	}
	c.inner.Info(ctx, msg, fields...)
}

func (c *capturingLogger) Warn(ctx context.Context, msg string, fields ...interface{}) {
	if strings.Contains(msg, "held at their stored definition") {
		if slug, ok := fieldValue(fields, "slug"); ok {
			terms, _ := fieldValue(fields, "heldContextTerms")
			props, _ := fieldValue(fields, "heldProperties")
			c.log.mu.Lock()
			c.log.held[slug] = append(c.log.held[slug], terms, props)
			c.log.mu.Unlock()
		}
	}
	c.inner.Warn(ctx, msg, fields...)
}

func (c *capturingLogger) Error(ctx context.Context, msg string, fields ...interface{}) {
	if strings.Contains(msg, "resource type NOT reconciled") {
		if slug, ok := fieldValue(fields, "slug"); ok {
			reason, _ := fieldValue(fields, "reason")
			c.log.mu.Lock()
			c.log.dropped[slug] = reason
			c.log.mu.Unlock()
		}
	}
	c.inner.Error(ctx, msg, fields...)
}

// fieldValue reads a key's value out of the variadic key-value pairs the Logger
// interface takes, rendering it as a string for substring assertions.
func fieldValue(fields []interface{}, key string) (string, bool) {
	for i := 0; i+1 < len(fields); i += 2 {
		if name, ok := fields[i].(string); ok && name == key {
			return fmt.Sprintf("%v", fields[i+1]), true
		}
	}
	return "", false
}

func (w *contextWorld) aCleanDatabase() error {
	dir, err := os.MkdirTemp("", "weos-preset-context-e2e-")
	if err != nil {
		return err
	}
	w.tmpDir = dir
	w.dsn = filepath.Join(dir, "test.db")
	return w.boot()
}

// boot starts a fresh Fx app against the world's existing database file, with
// the reconcile's log lines captured. It is the "restart" primitive: the
// process and its preset registry are new, the data is not.
func (w *contextWorld) boot() error {
	cfg := config.Default()
	cfg.DatabaseDSN = w.dsn
	cfg.LogLevel = "error"
	if lvl := os.Getenv("E2E_LOG_LEVEL"); lvl != "" {
		cfg.LogLevel = lvl
	}

	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel

	captured := newReconcileLog()
	var rts application.ResourceTypeService
	var rs application.ResourceService
	var db *gorm.DB

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, w.catalogRegistry()),
		// Decorating the Logger is what makes the boot reconcile observable
		// without a production seam existing only for the tests.
		fx.Decorate(func(inner entities.Logger) entities.Logger {
			return &capturingLogger{inner: inner, log: captured}
		}),
		fx.Populate(&rts, &rs, &db),
	)
	startCtx, startCancel := context.WithTimeout(ctx, fx.DefaultTimeout)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}
	w.app, w.rts, w.rs, w.db = app, rts, rs, db
	w.bootReport = captured
	return nil
}

func (w *contextWorld) stop() {
	if w.app != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		_ = w.app.Stop(stopCtx)
		stopCancel()
		w.app = nil
	}
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
}

func (w *contextWorld) theTwinRestarts() error {
	w.stop()
	return w.boot()
}

func (w *contextWorld) teardown() {
	w.stop()
	if w.tmpDir != "" {
		_ = os.RemoveAll(w.tmpDir)
		w.tmpDir = ""
	}
}

// --- operator customisation of the STORED context ---

// storedContextOf reads a type's context as a term map.
func (w *contextWorld) storedContextOf(slug string) (map[string]any, error) {
	rt, err := w.rts.GetBySlug(context.Background(), slug)
	if err != nil {
		return nil, fmt.Errorf("failed to load the %q type: %w", slug, err)
	}
	terms := map[string]any{}
	if len(rt.Context()) == 0 {
		return terms, nil
	}
	if err := json.Unmarshal(rt.Context(), &terms); err != nil {
		return nil, fmt.Errorf("stored context for %q is not a JSON object: %w", slug, err)
	}
	return terms, nil
}

// writeStoredContext rewrites a type's stored context in place, standing in for
// an operator editing it through the API. Everything else about the type is
// read back and passed through so only the context changes.
func (w *contextWorld) writeStoredContext(slug string, terms map[string]any) error {
	rt, err := w.rts.GetBySlug(context.Background(), slug)
	if err != nil {
		return fmt.Errorf("failed to load the %q type: %w", slug, err)
	}
	var raw json.RawMessage
	if terms != nil {
		encoded, mErr := json.Marshal(terms)
		if mErr != nil {
			return fmt.Errorf("failed to encode the operator's context: %w", mErr)
		}
		raw = encoded
	}
	_, err = w.rts.Update(context.Background(), application.UpdateResourceTypeCommand{
		ID:          rt.GetID(),
		Name:        rt.Name(),
		Slug:        rt.Slug(),
		Description: rt.Description(),
		Status:      rt.Status(),
		Context:     raw,
		Schema:      rt.Schema(),
	})
	if err != nil {
		return fmt.Errorf("failed to rewrite the stored context for %q: %w", slug, err)
	}
	return nil
}

func (w *contextWorld) theOperatorMapsTerm(term, iri string) error {
	terms, err := w.storedContextOf("widget")
	if err != nil {
		return err
	}
	terms[term] = iri
	return w.writeStoredContext("widget", terms)
}

func (w *contextWorld) theOperatorClearsContext() error {
	return w.writeStoredContext("widget", map[string]any{})
}

// --- writes ---

func (w *contextWorld) createResource(slug, name string, data map[string]any) error {
	if name == "" {
		return fmt.Errorf("a %s must be created with a name", slug)
	}
	data["name"] = name
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	res, err := w.rs.Create(context.Background(), application.CreateResourceCommand{TypeSlug: slug, Data: raw})
	if err != nil {
		return fmt.Errorf("failed to create %s %q: %w", slug, name, err)
	}
	w.createdIDs[slug+"/"+name] = res.GetID()
	return nil
}

func (w *contextWorld) aVendorExists(name string) error {
	return w.createResource("vendor", name, map[string]any{})
}

func (w *contextWorld) vendorID(name string) (string, error) {
	id, ok := w.createdIDs["vendor/"+name]
	if !ok {
		return "", fmt.Errorf("no vendor named %q was created in this scenario", name)
	}
	return id, nil
}

func (w *contextWorld) iCreateWidgetWithReference(name, property, vendorName string) error {
	id, err := w.vendorID(vendorName)
	if err != nil {
		return err
	}
	return w.createResource("widget", name, map[string]any{property: id})
}

func (w *contextWorld) iCreateWidgetWithReferences(name string, table *godog.Table) error {
	if len(table.Rows) == 0 {
		return fmt.Errorf("reference table has no header row")
	}
	data := map[string]any{}
	for _, row := range table.Rows[1:] {
		if len(row.Cells) != 2 {
			return fmt.Errorf("expected 2 columns per reference row, got %d", len(row.Cells))
		}
		property := strings.TrimSpace(row.Cells[0].Value)
		id, err := w.vendorID(strings.TrimSpace(row.Cells[1].Value))
		if err != nil {
			return err
		}
		data[property] = id
	}
	return w.createResource("widget", name, data)
}

func (w *contextWorld) iCreateWidgetWithLiteral(name, property, value string) error {
	return w.createResource("widget", name, map[string]any{property: value})
}

// aWidgetCreatedWithUndeclaredReference writes a reference the CURRENT schema
// does not declare — the state the bug leaves behind: the value reaches the
// canonical record with nowhere in the projection to land.
func (w *contextWorld) aWidgetCreatedWithUndeclaredReference(name, property, vendorName string) error {
	return w.iCreateWidgetWithReference(name, property, vendorName)
}

// --- reads and assertions ---

// flatRead reads a widget back through the projection (GetFlat), which is the
// surface the silent drop actually shows up on.
func (w *contextWorld) flatRead(name string) (map[string]any, error) {
	id, ok := w.createdIDs["widget/"+name]
	if !ok {
		return nil, fmt.Errorf("no widget named %q was created in this scenario", name)
	}
	row, err := w.rs.GetFlat(context.Background(), "widget", id)
	if err != nil {
		return nil, fmt.Errorf("failed to read widget %q back: %w", name, err)
	}
	return row, nil
}

func (w *contextWorld) readingReturnsReference(name, property, vendorName string) error {
	want, err := w.vendorID(vendorName)
	if err != nil {
		return err
	}
	row, err := w.flatRead(name)
	if err != nil {
		return err
	}
	got, ok := row[property]
	if !ok {
		return fmt.Errorf("read of %q carries no %q key — the reference was dropped (keys: %s)",
			name, property, mapKeys(row))
	}
	if fmt.Sprintf("%v", got) != want {
		return fmt.Errorf("read of %q returned %s = %v, want the %q vendor %s", name, property, got, vendorName, want)
	}
	return nil
}

func (w *contextWorld) readingReturnsLiteral(name, property, want string) error {
	row, err := w.flatRead(name)
	if err != nil {
		return err
	}
	got, ok := row[property]
	if !ok {
		return fmt.Errorf("read of %q carries no %q key (keys: %s)", name, property, mapKeys(row))
	}
	if fmt.Sprintf("%v", got) != want {
		return fmt.Errorf("read of %q returned %s = %v, want %q", name, property, got, want)
	}
	return nil
}

func (w *contextWorld) readingReturnsNoValue(name, property string) error {
	row, err := w.flatRead(name)
	if err != nil {
		return err
	}
	return assertEmpty(row, property)
}

// canonicalStillCarriesEdge reads the canonical JSON-LD blob rather than the
// projection, proving the event store never lost the reference even while the
// context entry was missing.
func (w *contextWorld) canonicalStillCarriesEdge(name, property, vendorName string) error {
	want, err := w.vendorID(vendorName)
	if err != nil {
		return err
	}
	id, ok := w.createdIDs["widget/"+name]
	if !ok {
		return fmt.Errorf("no widget named %q was created in this scenario", name)
	}
	res, err := w.rs.GetByID(context.Background(), id)
	if err != nil {
		return fmt.Errorf("failed to read widget %q by id: %w", name, err)
	}
	rt, err := w.rts.GetBySlug(context.Background(), "widget")
	if err != nil {
		return fmt.Errorf("failed to load the widget type: %w", err)
	}
	for _, got := range application.EdgeValues(res.Data(), rt.Context(), property) {
		if got == want {
			return nil
		}
	}
	// A value written BEFORE the schema declared the property was never a
	// reference at write time, so it landed in the entity node as a plain
	// value rather than the edges node — EdgeValues cannot see it. Look it up
	// by name instead. Deliberately NOT a substring search over the payload:
	// that would pass on the URN appearing under any other key, which is the
	// exact loss this step exists to catch.
	var data map[string]any
	if err := json.Unmarshal(res.Data(), &data); err != nil {
		return fmt.Errorf("canonical data for %q is not a JSON object: %w", name, err)
	}
	if got, ok := jsonLDField(data, property); ok && fmt.Sprintf("%v", got) == want {
		return nil
	}
	return fmt.Errorf("canonical record for %q lost its %q edge to %s (payload: %s)",
		name, property, want, res.Data())
}

func (w *contextWorld) theProjectionTableHasColumn(column string) error {
	if !w.db.Migrator().HasColumn("widgets", column) {
		return fmt.Errorf("projection table `widgets` has no %q column", column)
	}
	return nil
}

func (w *contextWorld) theStoredContextHasEntry(term string) error {
	terms, err := w.storedContextOf("widget")
	if err != nil {
		return err
	}
	if _, ok := terms[term]; !ok {
		return fmt.Errorf("stored widget context has no entry for %q (terms: %v)", term, sortedTermNames(terms))
	}
	return nil
}

func (w *contextWorld) theStoredContextHasNoEntry(term string) error {
	terms, err := w.storedContextOf("widget")
	if err != nil {
		return err
	}
	if _, ok := terms[term]; ok {
		return fmt.Errorf("stored widget context unexpectedly has an entry for %q", term)
	}
	return nil
}

func (w *contextWorld) theStoredContextStillMaps(term, iri string) error {
	terms, err := w.storedContextOf("widget")
	if err != nil {
		return err
	}
	got, ok := terms[term]
	if !ok {
		return fmt.Errorf("stored widget context lost its entry for %q", term)
	}
	if fmt.Sprintf("%v", got) != iri {
		return fmt.Errorf("stored widget context maps %q to %v, want %q — the operator's edit was overwritten",
			term, got, iri)
	}
	return nil
}

// everyReferenceHasAStoredEntry mirrors the read path: a reference survives only
// if the reverse map resolves its predicate IRI back to its own property name.
func (w *contextWorld) everyReferenceHasAStoredEntry() error {
	rt, err := w.rts.GetBySlug(context.Background(), "widget")
	if err != nil {
		return fmt.Errorf("failed to load the widget type: %w", err)
	}
	reverse := jsonld.BuildReverseMap(rt.Context())
	var dropped []string
	for _, ref := range application.ExtractReferenceProperties(rt.Schema(), rt.Context()) {
		if reverse[ref.PredicateIRI] != ref.PropertyName {
			dropped = append(dropped, ref.PropertyName)
		}
	}
	if len(dropped) > 0 {
		return fmt.Errorf("reference properties %v have no usable stored context entry, so their writes are dropped",
			dropped)
	}
	return nil
}

func sortedTermNames(terms map[string]any) string {
	names := make([]string, 0, len(terms))
	for k := range terms {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// --- event-store and boot-report assertions ---

func (w *contextWorld) countTypeUpdates(slug string) (int64, error) {
	var n int64
	err := w.db.Table("events").
		Where("aggregate_id = ? AND event_type = ?", "urn:type:"+slug, "ResourceType.Updated").
		Count(&n).Error
	if err != nil {
		return 0, fmt.Errorf("failed to count ResourceType.Updated events: %w", err)
	}
	return n, nil
}

func (w *contextWorld) noUpdateRecorded(slug string) error {
	n, err := w.countTypeUpdates(slug)
	if err != nil {
		return err
	}
	if n != 0 {
		return fmt.Errorf("expected no ResourceType.Updated events for %q, got %d", slug, n)
	}
	return nil
}

func (w *contextWorld) exactlyOneUpdateRecorded(slug string) error {
	n, err := w.countTypeUpdates(slug)
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("expected exactly 1 ResourceType.Updated event for %q, got %d", slug, n)
	}
	return nil
}

func (w *contextWorld) report() (*reconcileLog, error) {
	if w.bootReport == nil {
		return nil, fmt.Errorf("no boot has been observed in this scenario")
	}
	return w.bootReport, nil
}

func (w *contextWorld) bootReportsHeld(term, slug string) error {
	r, err := w.report()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, line := range r.held[slug] {
		if strings.Contains(line, term) {
			return nil
		}
	}
	return fmt.Errorf("boot reconcile did not report %q held for %q (held lines: %v)", term, slug, r.held[slug])
}

func (w *contextWorld) bootReportsUpdated(slug string) error {
	r, err := w.report()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.updated[slug] {
		return fmt.Errorf("boot reconcile did not report %q as updated (dropped: %v)", slug, r.dropped[slug])
	}
	return nil
}

func (w *contextWorld) bootDoesNotReportUpdated(slug string) error {
	r, err := w.report()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updated[slug] {
		return fmt.Errorf("boot reconcile reported %q as updated, but writes to it are still dropped", slug)
	}
	return nil
}

func (w *contextWorld) bootNamesDropped(property string) error {
	r, err := w.report()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for slug, reason := range r.dropped {
		if strings.Contains(reason, property) {
			return nil
		}
		_ = slug
	}
	return fmt.Errorf("boot reconcile never named %q as a property whose writes are dropped (reported: %v)",
		property, r.dropped)
}

// theOperatorReprojects runs the same replay rail `weos worker reproject`
// drives, against the stopped-then-restarted database.
func (w *contextWorld) theOperatorReprojects() error {
	w.stop()

	cfg := config.Default()
	cfg.DatabaseDSN = w.dsn
	cfg.LogLevel = "error"

	var rt application.ReprojectRuntime
	app := fx.New(fx.NopLogger, application.ReprojectModule(cfg), fx.Populate(&rt))
	startCtx, startCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start the reproject runtime: %w", err)
	}
	_, err := application.Reproject(context.Background(), rt, application.ReprojectOptions{})
	stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	_ = app.Stop(stopCtx)
	stopCancel()
	if err != nil {
		return fmt.Errorf("reproject failed: %w", err)
	}
	return w.boot()
}
