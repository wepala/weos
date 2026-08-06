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
	"testing"

	"github.com/cucumber/godog"
	"go.uber.org/fx"
	"gorm.io/gorm"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"
)

// TestProjectionColumnMigration is the acceptance suite for issue #379: a
// built-in preset that gains a property in a new build must reach a database
// provisioned by an older one, instead of silently dropping every write to the
// new field.
//
// The suite drives real restarts — each "restart" stops the Fx app and starts a
// fresh one against the SAME SQLite file with a differently-shaped preset
// registry — because the defect only exists across a boot boundary.
func TestProjectionColumnMigration(t *testing.T) {
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                "projection-column-migration",
		ScenarioInitializer: initProjectionMigrationScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features/projection_column_migration.feature"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("projection_column_migration acceptance scenarios failed")
	}
}

// projectionMigrationWorld holds one scenario's database and the shape the
// "catalog" preset declares on the next boot. Mutating properties then calling
// restart is how a scenario expresses "a new build of WeOS".
type projectionMigrationWorld struct {
	tmpDir string
	dsn    string

	app    *fx.App
	cancel context.CancelFunc

	rts application.ResourceTypeService
	rs  application.ResourceService
	db  *gorm.DB

	// properties is the widget type's JSON Schema properties, in declaration
	// order, as the preset would declare them on the next boot.
	properties []widgetProperty
	// fixtureCount is how many widget fixtures the preset seeds on create.
	fixtureCount int

	// createdIDs maps a widget's name to its resource ID so later steps can
	// read it back without re-querying by name.
	createdIDs map[string]string
	// lastReadRow backs the "it returns no value for X" step, which reads as a
	// continuation of the preceding read step.
	lastReadRow map[string]any
}

type widgetProperty struct {
	name    string
	jsonTyp string
}

func initProjectionMigrationScenario(sc *godog.ScenarioContext) {
	w := &projectionMigrationWorld{createdIDs: map[string]string{}}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a built-in preset "catalog" declaring a "widget" type with the properties:$`, w.aPresetDeclaring)
	sc.Step(`^the "catalog" preset seeds (\d+) "widget" fixtures$`, w.thePresetSeedsFixtures)
	sc.Step(`^a clean WeOS database provisioned by that build$`, w.aCleanDatabase)

	sc.Step(`^the "catalog" preset adds a "([^"]*)" (\w+) property to "widget"$`, w.thePresetAddsProperty)
	sc.Step(`^the twin restarts against the same database$`, w.theTwinRestarts)
	sc.Step(`^the twin restarts against the same database again$`, w.theTwinRestarts)

	sc.Step(`^the "widget" projection table has a "([^"]*)" column$`, w.theProjectionTableHasColumn)
	sc.Step(`^I create a "widget" named "([^"]*)" with "([^"]*)" set to "([^"]*)"$`, w.iCreateWidgetWith)
	sc.Step(`^a "widget" named "([^"]*)" exists$`, w.aWidgetExists)
	sc.Step(`^a "widget" named "([^"]*)" is created with an undeclared "([^"]*)" of "([^"]*)"$`,
		w.aWidgetIsCreatedWithUndeclared)

	sc.Step(`^reading the "widget" "([^"]*)" back over the API returns "([^"]*)" as "([^"]*)"$`,
		w.readingReturnsValue)
	sc.Step(`^reading the "widget" "([^"]*)" back over the API returns no value for "([^"]*)"$`,
		w.readingReturnsNoValue)
	sc.Step(`^reading the "widget" "([^"]*)" back over the API succeeds$`, w.readingSucceeds)
	sc.Step(`^it returns no value for "([^"]*)"$`, w.itReturnsNoValueForLastRead)
	sc.Step(`^the JSON-LD representation of the "widget" "([^"]*)" still carries "([^"]*)" as "([^"]*)"$`,
		w.canonicalStillCarries)

	sc.Step(`^no resource type update is recorded for "([^"]*)"$`, w.noUpdateRecorded)
	sc.Step(`^exactly one resource type update is recorded for "([^"]*)"$`, w.exactlyOneUpdateRecorded)
	sc.Step(`^there are still exactly (\d+) "widget" resources$`, w.thereAreExactlyNWidgets)

	sc.Step(`^the "widget" resource type is archived$`, w.theTypeIsArchived)
	sc.Step(`^the "widget" resource type is still archived$`, w.theTypeIsStillArchived)

	sc.Step(`^the operator reprojects the event feed$`, w.theOperatorReprojects)
}

// --- preset shaping ---

func (w *projectionMigrationWorld) aPresetDeclaring(table *godog.Table) error {
	w.properties = nil
	// Row 0 is the header (| property | type |).
	for _, row := range table.Rows[1:] {
		if len(row.Cells) != 2 {
			return fmt.Errorf("expected 2 columns per property row, got %d", len(row.Cells))
		}
		w.properties = append(w.properties, widgetProperty{
			name:    strings.TrimSpace(row.Cells[0].Value),
			jsonTyp: strings.TrimSpace(row.Cells[1].Value),
		})
	}
	return nil
}

func (w *projectionMigrationWorld) thePresetSeedsFixtures(count int) error {
	w.fixtureCount = count
	return nil
}

func (w *projectionMigrationWorld) thePresetAddsProperty(name, jsonTyp string) error {
	for _, p := range w.properties {
		if p.name == name {
			return fmt.Errorf("property %q is already declared", name)
		}
	}
	w.properties = append(w.properties, widgetProperty{name: name, jsonTyp: jsonTyp})
	return nil
}

// catalogRegistry builds the preset registry for the next boot from the
// world's current property list. AutoInstall is what routes it through
// ensureBuiltInResourceTypes — the startup path under test.
func (w *projectionMigrationWorld) catalogRegistry() *application.PresetRegistry {
	props := make([]string, 0, len(w.properties))
	for _, p := range w.properties {
		props = append(props, fmt.Sprintf("%q:{\"type\":%q}", p.name, p.jsonTyp))
	}
	schema := fmt.Sprintf(`{"type":"object","properties":{%s}}`, strings.Join(props, ","))

	fixtures := make([]json.RawMessage, 0, w.fixtureCount)
	for i := 0; i < w.fixtureCount; i++ {
		fixtures = append(fixtures, json.RawMessage(fmt.Sprintf(`{"name":"fixture-%d"}`, i)))
	}

	registry := application.NewPresetRegistry()
	registry.MustAdd(application.PresetDefinition{
		Name:        "catalog",
		Description: "issue #379 acceptance preset",
		AutoInstall: true,
		Types: []application.PresetResourceType{{
			Name:        "Widget",
			Slug:        "widget",
			Description: "A widget",
			Context:     json.RawMessage(`{"@vocab":"https://schema.org/"}`),
			Schema:      json.RawMessage(schema),
			Fixtures:    fixtures,
		}},
	})
	return registry
}

// --- boot / restart ---

func (w *projectionMigrationWorld) aCleanDatabase() error {
	dir, err := os.MkdirTemp("", "weos-projection-migration-e2e-")
	if err != nil {
		return err
	}
	w.tmpDir = dir
	w.dsn = filepath.Join(dir, "test.db")
	return w.boot()
}

// boot starts a fresh Fx app against the world's existing database file. It is
// the "restart" primitive: the process and its preset registry are new, the
// data is not.
func (w *projectionMigrationWorld) boot() error {
	cfg := config.Default()
	cfg.DatabaseDSN = w.dsn
	cfg.LogLevel = "error"
	if lvl := os.Getenv("E2E_LOG_LEVEL"); lvl != "" {
		cfg.LogLevel = lvl
	}

	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel

	var rts application.ResourceTypeService
	var rs application.ResourceService
	var db *gorm.DB

	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, w.catalogRegistry()),
		fx.Populate(&rts, &rs, &db),
	)
	startCtx, startCancel := context.WithTimeout(ctx, fx.DefaultTimeout)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}
	w.app, w.rts, w.rs, w.db = app, rts, rs, db
	return nil
}

func (w *projectionMigrationWorld) stop() {
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

func (w *projectionMigrationWorld) theTwinRestarts() error {
	w.stop()
	return w.boot()
}

func (w *projectionMigrationWorld) teardown() {
	w.stop()
	if w.tmpDir != "" {
		_ = os.RemoveAll(w.tmpDir)
		w.tmpDir = ""
	}
}

// --- assertions ---

func (w *projectionMigrationWorld) theProjectionTableHasColumn(column string) error {
	if !w.db.Migrator().HasColumn("widgets", column) {
		return fmt.Errorf("projection table `widgets` has no %q column (columns: %s)",
			column, w.widgetColumns())
	}
	return nil
}

// widgetColumns renders the projection table's actual columns for failure
// messages — the single most useful thing to see when this suite goes red.
func (w *projectionMigrationWorld) widgetColumns() string {
	types, err := w.db.Migrator().ColumnTypes("widgets")
	if err != nil {
		return fmt.Sprintf("<unreadable: %v>", err)
	}
	names := make([]string, 0, len(types))
	for _, ct := range types {
		names = append(names, ct.Name())
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func (w *projectionMigrationWorld) createWidget(data map[string]any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	res, err := w.rs.Create(context.Background(), application.CreateResourceCommand{
		TypeSlug: "widget",
		Data:     raw,
	})
	if err != nil {
		return fmt.Errorf("failed to create widget: %w", err)
	}
	name, _ := data["name"].(string)
	w.createdIDs[name] = res.GetID()
	return nil
}

func (w *projectionMigrationWorld) iCreateWidgetWith(name, field, value string) error {
	return w.createWidget(map[string]any{"name": name, field: value})
}

func (w *projectionMigrationWorld) aWidgetExists(name string) error {
	return w.createWidget(map[string]any{"name": name})
}

// aWidgetIsCreatedWithUndeclared writes a field the CURRENT schema does not
// declare — the state the bug leaves behind: the value reaches the canonical
// resources row but has no projection column to land in.
func (w *projectionMigrationWorld) aWidgetIsCreatedWithUndeclared(name, field, value string) error {
	return w.createWidget(map[string]any{"name": name, field: value})
}

// flatRead reads a widget back through the projection (GetFlat), which is the
// surface the silent drop actually shows up on.
func (w *projectionMigrationWorld) flatRead(name string) (map[string]any, error) {
	id, ok := w.createdIDs[name]
	if !ok {
		return nil, fmt.Errorf("no widget named %q was created in this scenario", name)
	}
	row, err := w.rs.GetFlat(context.Background(), "widget", id)
	if err != nil {
		return nil, fmt.Errorf("failed to read widget %q back: %w", name, err)
	}
	return row, nil
}

func (w *projectionMigrationWorld) readingReturnsValue(name, field, want string) error {
	row, err := w.flatRead(name)
	if err != nil {
		return err
	}
	w.lastReadRow = row
	got, ok := row[field]
	if !ok {
		return fmt.Errorf("read of %q carries no %q key (keys: %s)", name, field, mapKeys(row))
	}
	if fmt.Sprintf("%v", got) != want {
		return fmt.Errorf("read of %q returned %s = %v, want %q", name, field, got, want)
	}
	return nil
}

func (w *projectionMigrationWorld) readingReturnsNoValue(name, field string) error {
	row, err := w.flatRead(name)
	if err != nil {
		return err
	}
	w.lastReadRow = row
	return assertEmpty(row, field)
}

func (w *projectionMigrationWorld) readingSucceeds(name string) error {
	row, err := w.flatRead(name)
	if err != nil {
		return err
	}
	w.lastReadRow = row
	return nil
}

func (w *projectionMigrationWorld) itReturnsNoValueForLastRead(field string) error {
	if w.lastReadRow == nil {
		return fmt.Errorf("no resource has been read yet")
	}
	return assertEmpty(w.lastReadRow, field)
}

// assertEmpty tolerates both an absent key and a present-but-null/empty value:
// a column added by ALTER TABLE is NULL on every pre-existing row, and the flat
// projection may or may not carry the key depending on how the row was built.
func assertEmpty(row map[string]any, field string) error {
	got, ok := row[field]
	if !ok || got == nil || got == "" {
		return nil
	}
	return fmt.Errorf("expected no value for %q, got %v", field, got)
}

func mapKeys(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// canonicalStillCarries reads the canonical JSON-LD blob rather than the
// projection — proving the event store never lost the field even while the
// projection column was missing.
func (w *projectionMigrationWorld) canonicalStillCarries(name, field, want string) error {
	id, ok := w.createdIDs[name]
	if !ok {
		return fmt.Errorf("no widget named %q was created in this scenario", name)
	}
	res, err := w.rs.GetByID(context.Background(), id)
	if err != nil {
		return fmt.Errorf("failed to read widget %q by id: %w", name, err)
	}
	var data map[string]any
	if err := json.Unmarshal(res.Data(), &data); err != nil {
		return fmt.Errorf("canonical data for %q is not a JSON object: %w", name, err)
	}
	got, ok := jsonLDField(data, field)
	if !ok {
		return fmt.Errorf("canonical record for %q lost %q (payload: %s)", name, field, res.Data())
	}
	if fmt.Sprintf("%v", got) != want {
		return fmt.Errorf("canonical record for %q has %s = %v, want %q", name, field, got, want)
	}
	return nil
}

// jsonLDField looks a property up in a stored JSON-LD blob, which may carry it
// at the top level or inside an @graph node depending on how the document was
// framed.
func jsonLDField(data map[string]any, field string) (any, bool) {
	if v, ok := data[field]; ok {
		return v, true
	}
	graph, ok := data["@graph"]
	if !ok {
		return nil, false
	}
	switch g := graph.(type) {
	case map[string]any:
		v, ok := g[field]
		return v, ok
	case []any:
		for _, node := range g {
			n, isMap := node.(map[string]any)
			if !isMap {
				continue
			}
			if v, ok := n[field]; ok {
				return v, true
			}
		}
	}
	return nil, false
}

// countTypeUpdates counts ResourceType.Updated events in the event store for a
// slug's aggregate. Reading the store (not a live subscription) is deliberate:
// the count has to survive the restarts these scenarios perform.
func (w *projectionMigrationWorld) countTypeUpdates(slug string) (int64, error) {
	var n int64
	err := w.db.Table("events").
		Where("aggregate_id = ? AND event_type = ?", "urn:type:"+slug, "ResourceType.Updated").
		Count(&n).Error
	if err != nil {
		return 0, fmt.Errorf("failed to count ResourceType.Updated events: %w", err)
	}
	return n, nil
}

func (w *projectionMigrationWorld) noUpdateRecorded(slug string) error {
	n, err := w.countTypeUpdates(slug)
	if err != nil {
		return err
	}
	if n != 0 {
		return fmt.Errorf("expected no ResourceType.Updated events for %q, got %d", slug, n)
	}
	return nil
}

func (w *projectionMigrationWorld) exactlyOneUpdateRecorded(slug string) error {
	n, err := w.countTypeUpdates(slug)
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("expected exactly 1 ResourceType.Updated event for %q, got %d", slug, n)
	}
	return nil
}

func (w *projectionMigrationWorld) thereAreExactlyNWidgets(want int) error {
	page, err := w.rs.List(context.Background(), "widget", "", 100, repositories.SortOptions{})
	if err != nil {
		return fmt.Errorf("failed to list widgets: %w", err)
	}
	if got := len(page.Data); got != want {
		return fmt.Errorf("expected exactly %d widget resources, got %d", want, got)
	}
	return nil
}

func (w *projectionMigrationWorld) theTypeIsArchived() error {
	existing, err := w.rts.GetBySlug(context.Background(), "widget")
	if err != nil {
		return fmt.Errorf("failed to load the widget type: %w", err)
	}
	_, err = w.rts.Update(context.Background(), application.UpdateResourceTypeCommand{
		ID:          existing.GetID(),
		Name:        existing.Name(),
		Slug:        existing.Slug(),
		Description: existing.Description(),
		Status:      "archived",
		Context:     existing.Context(),
		Schema:      existing.Schema(),
	})
	if err != nil {
		return fmt.Errorf("failed to archive the widget type: %w", err)
	}
	return nil
}

func (w *projectionMigrationWorld) theTypeIsStillArchived() error {
	existing, err := w.rts.GetBySlug(context.Background(), "widget")
	if err != nil {
		return fmt.Errorf("failed to load the widget type: %w", err)
	}
	if existing.Status() != "archived" {
		return fmt.Errorf("expected the widget type to still be archived, got status %q", existing.Status())
	}
	return nil
}

// theOperatorReprojects runs the same replay rail `weos worker reproject`
// drives, against the stopped-then-restarted database. The app is stopped for
// the duration so the replay is the one-shot operator pass it is designed to be
// rather than racing the live synchronous projections.
func (w *projectionMigrationWorld) theOperatorReprojects() error {
	w.stop()

	cfg := config.Default()
	cfg.DatabaseDSN = w.dsn
	cfg.LogLevel = "error"

	var rt application.ReprojectRuntime
	app := fx.New(
		fx.NopLogger,
		application.ReprojectModule(cfg),
		fx.Populate(&rt),
	)
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
