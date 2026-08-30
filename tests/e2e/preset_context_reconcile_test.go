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
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/pkg/jsonld"
)

// TestPresetContextReconcile is the acceptance suite for issue #510: a built-in
// preset that gains a REFERENCE property must reach a database provisioned by
// an older build with its `@context` entry, not just its projection column.
//
// Issue #379's reconcile gave the column. Without the context entry the edge
// has no reverse mapping, so SimplifyJSONLD (the API read path) and
// extractEdgeColumns (the projection FK column) both skip it and the write is
// dropped while the API answers 201 — which is why this suite asserts
// round-trips, not just stored shape.
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

// TestReferencePropertyRoundTrip is the acceptance suite for issue #513's P0-1:
// a schema may mark an ARRAY property `x-resource-type`, the write path stores
// it as an array of {"@id":…} refs, and both read paths unwrap a single map
// only. A type can therefore hold its context term, satisfy the packaging guard
// and be reported reconciled while that reference never reads back.
//
// It shares its step world with TestPresetContextReconcile: the shapes are the
// same, only the cardinality differs.
func TestReferencePropertyRoundTrip(t *testing.T) {
	runContextFeature(t, "reference-property-round-trip", "features/reference_property_round_trip.feature")
}

// TestPresetContextGuards is the acceptance suite for issue #513's remaining
// P0s: the boot's additive `@context` merge must never move a predicate that
// already has data behind it (P0-2, P0-3, P0-5), and a stored context it cannot
// merge must not take the SCHEMA merge down with it (P0-4).
func TestPresetContextGuards(t *testing.T) {
	runContextFeature(t, "preset-context-guards", "features/preset_context_guards.feature")
}

// TestReferenceClearProjection is the acceptance suite for issue #550: an unset
// of a reference property never reaches the flat projection. The event store
// records the clear; the projection row keeps the old value forever.
//
// It shares its step world with TestPresetContextReconcile: the shapes are the
// same, and only the update path is new.
func TestReferenceClearProjection(t *testing.T) {
	runContextFeature(t, "reference-clear-projection", "features/reference_clear_projection.feature")
}

// runContextFeature runs one feature file against the shared context step world.
func runContextFeature(t *testing.T, name, path string) {
	t.Helper()
	runFeatureWith(t, name, path, initPresetContextScenario)
}

// runFeatureWith runs one feature file with the given scenario initializer,
// honoring the suite-wide tag convention (@wip excluded unless GODOG_TAGS
// overrides it).
func runFeatureWith(t *testing.T, name, path string, init func(*godog.ScenarioContext)) {
	t.Helper()
	tags := "~@wip"
	if override := os.Getenv("GODOG_TAGS"); override != "" {
		tags = override
	}
	suite := godog.TestSuite{
		Name:                name,
		ScenarioInitializer: init,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{path},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatalf("%s acceptance scenarios failed", name)
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

	// resRepo and tripleRepo are the two surfaces issue #515 asserts against
	// that no service exposes: UpdateData rewrites a resource's canonical
	// record to the pre-#515 expanded shape (and re-runs the projection over
	// it), and the triple repository proves the ontology did not move.
	resRepo    repositories.ResourceRepository
	tripleRepo repositories.TripleRepository

	// Issue #523: the normalization run's report and the event feed as it
	// stood on either side of it, so a scenario can prove what the run did
	// and did not touch.
	normalizeReport       *application.NormalizeEdgeKeysReport
	eventsBeforeNormalize []storedEvent
	eventsAfterNormalize  []storedEvent
	capturedReads         map[string]string

	// registry, when set, is the preset registry the next boot runs on
	// instead of the synthetic catalog — the real built-in presets (issue
	// #520), possibly rewritten to look like an older build.
	registry func() *application.PresetRegistry

	// Issue #518: the last adoption's outcome.
	lastAdoption *application.AdoptResult

	// Issue #519: the count's report and the canonical records on either
	// side of it.
	countReport        *application.IRIEdgeKeyCountReport
	recordsBeforeCount map[string]string
	recordsAfterCount  map[string]string

	// vendorProps and widgetProps are the two types' schema properties in
	// declaration order, as the preset would declare them on the next boot.
	vendorProps []contextProperty
	widgetProps []contextProperty

	// widgetContextExtras are `@context` entries the next build declares that
	// no property implies — a prefix definition, an `@type`, or a term named
	// at an explicit IRI. Issue #513's guards are all about entries of this
	// shape, because they change how OTHER terms resolve.
	widgetContextExtras map[string]string

	// schemaBeforeRestart is the stored "widget" schema as it was immediately
	// before the most recent restart, so a scenario can assert a context-only
	// reconcile left it untouched.
	schemaBeforeRestart json.RawMessage

	// bootReport captures what the LAST boot's reconcile reported, read back
	// from the log lines reconcilePresetSchemas emits — the operator-facing
	// surface, since ReconcilePresetResult never leaves the boot hook.
	bootReport *reconcileLog

	createdIDs map[string]string

	// createdData is the flat data each resource was last written with, keyed
	// like createdIDs. The update steps rebuild a whole document from it because
	// the update path replaces a resource's data WHOLESALE — "clearing maker"
	// means resubmitting everything else without it, which is exactly how a
	// client clears a reference and exactly what issue #550 is about.
	createdData map[string]map[string]any

	// bootErr is the LAST boot's start error, kept rather than propagated so a
	// scenario can assert a boot was refused. Whether a refusal fails startup or
	// only reports is the implementer's call (issue #515); the assertions accept
	// either, so this is set on every boot and read only by those steps.
	bootErr error

	// adoptErr and adoptAttempted record the outcome of an adopt-term command a
	// scenario expects to be REFUSED, so the refusal itself can be asserted.
	adoptErr       error
	adoptAttempted bool

	// heldAccounted marks the (slug, term) holds a scenario has already
	// asserted, so "records no failure" can mean "nothing beyond the hold you
	// were just told about" rather than "nothing held at all". Holding a term
	// an operator mapped by hand IS the reconcile working — overwriting would
	// repoint a predicate their own data is keyed by — so a scenario is allowed
	// to name one hold and then still demand a clean boot (issue #537).
	heldAccounted map[string]bool

	// contextBeforeSecondAdoption is the stored "widget" context as it was
	// immediately before a repeated adoption, so idempotence is asserted against
	// what was actually there rather than against a rebuilt expectation.
	contextBeforeSecondAdoption json.RawMessage
}

// contextProperty is one schema property. references is empty for a literal and
// names the target type slug for a reference. noContextEntry reproduces a
// preset packaging bug: a reference the preset's OWN context never declares, so
// there is nothing for an additive merge to add. list marks a reference the
// schema declares as an ARRAY of URNs — the shape the mealplanning preset uses
// for suitableForDiet and recipeIngredient (issue #513).
type contextProperty struct {
	name           string
	jsonTyp        string
	references     string
	noContextEntry bool
	list           bool
}

func initPresetContextScenario(sc *godog.ScenarioContext) {
	w := &contextWorld{
		createdIDs:          map[string]string{},
		createdData:         map[string]map[string]any{},
		widgetContextExtras: map[string]string{},
	}

	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a built-in preset "catalog" declaring a "vendor" type with the properties:$`, w.aVendorTypeDeclaring)
	sc.Step(`^the "catalog" preset also declares a "widget" type with the properties:$`, w.aWidgetTypeDeclaring)
	sc.Step(`^a clean WeOS database provisioned by that build$`, w.aCleanDatabase)
	sc.Step(`^a "vendor" named "([^"]*)" exists$`, w.aVendorExists)
	sc.Step(`^a "widget" named "([^"]*)" exists$`, w.aWidgetExists)

	sc.Step(`^the "catalog" preset adds a "([^"]*)" reference property to "widget" targeting "([^"]*)"$`,
		w.thePresetAddsReference)
	sc.Step(`^the "catalog" preset adds a "([^"]*)" reference list property to "widget" targeting "([^"]*)"$`,
		w.thePresetAddsReferenceList)
	sc.Step(`^the "catalog" preset declares "([^"]*)" as "([^"]*)" in the "widget" context$`,
		w.thePresetDeclaresContextTerm)
	sc.Step(`^the "catalog" preset declares a context entry for "([^"]*)" on "widget"$`,
		w.thePresetDeclaresContextEntryFor)
	sc.Step(`^the "catalog" preset adds a "([^"]*)" reference property to "widget" targeting "([^"]*)" `+
		`without a context entry$`, w.thePresetAddsReferenceWithoutContextEntry)
	sc.Step(`^the "catalog" preset adds a "([^"]*)" (\w+) property to "(widget|vendor)"$`, w.thePresetAddsLiteral)

	sc.Step(`^the operator maps "([^"]*)" to "([^"]*)" in the stored "widget" context$`, w.theOperatorMapsTerm)
	sc.Step(`^the operator clears the stored "widget" context$`, w.theOperatorClearsContext)
	sc.Step(`^the operator deletes "([^"]*)" from the stored "widget" context$`, w.theOperatorDeletesTerm)
	sc.Step(`^the operator stores the raw context (.+) for "widget"$`, w.theOperatorStoresRawContext)

	sc.Step(`^the twin restarts against the same database$`, w.theTwinRestarts)
	sc.Step(`^the twin restarts against the same database again$`, w.theTwinRestarts)
	sc.Step(`^the operator reprojects the event feed$`, w.theOperatorReprojects)

	sc.Step(`^I create a "widget" named "([^"]*)" with "([^"]*)" referring to the "vendor" "([^"]*)"$`,
		w.iCreateWidgetWithReference)
	sc.Step(`^I create a "widget" named "([^"]*)" with these references:$`, w.iCreateWidgetWithReferences)
	sc.Step(`^I create a "widget" named "([^"]*)" with "([^"]*)" set to "([^"]*)"$`, w.iCreateWidgetWithLiteral)

	sc.Step(`^I update the "widget" "([^"]*)" clearing "([^"]*)"$`, w.iUpdateWidgetClearing)
	sc.Step(`^I update the "widget" "([^"]*)" setting "([^"]*)" to an empty value$`,
		w.iUpdateWidgetSettingEmpty)
	sc.Step(`^I update the "widget" "([^"]*)" setting "([^"]*)" to the "vendor" "([^"]*)"$`,
		w.iUpdateWidgetSettingReference)

	sc.Step(`^reading the "widget" "([^"]*)" back through the projection returns "([^"]*)" as the "vendor" "([^"]*)"$`,
		w.readingReturnsReference)
	sc.Step(`^reading the "widget" "([^"]*)" back through the projection returns "([^"]*)" as the vendors "([^"]*)"$`,
		w.readingReturnsReferenceList)
	sc.Step(`^reading the "widget" "([^"]*)" back through the projection returns "([^"]*)" as "([^"]*)"$`, w.readingReturnsLiteral)
	sc.Step(`^reading the "widget" "([^"]*)" back through the projection returns no value for "([^"]*)"$`, w.readingReturnsNoValue)
	sc.Step(`^the JSON-LD representation of the "widget" "([^"]*)" still carries a "([^"]*)" edge `+
		`to the "vendor" "([^"]*)"$`, w.canonicalStillCarriesEdge)
	sc.Step(`^the JSON-LD representation of the "widget" "([^"]*)" carries "([^"]*)" edges `+
		`to the vendors "([^"]*)"$`, w.canonicalCarriesEdgeList)
	sc.Step(`^the "widget" resources "([^"]*)" and "([^"]*)" carry the same RDF type$`, w.widgetsShareAnRDFType)

	sc.Step(`^the "widget" projection table has a "([^"]*)" column$`, w.theProjectionTableHasColumn)
	sc.Step(`^the stored "widget" context has an entry for "([^"]*)"$`, w.theStoredContextHasEntry)
	sc.Step(`^the stored "widget" context has no entry for "([^"]*)"$`, w.theStoredContextHasNoEntry)
	sc.Step(`^the stored "widget" context still maps "([^"]*)" to "([^"]*)"$`, w.theStoredContextStillMaps)
	sc.Step(`^every reference property the "catalog" preset declares for "widget" has a stored context entry$`,
		w.everyReferenceHasAStoredEntry)
	sc.Step(`^the stored "widget" schema is byte-identical to the one stored before the restart$`,
		w.theStoredSchemaSurvivedTheRestart)

	sc.Step(`^no resource type update is recorded for "([^"]*)"$`, w.noUpdateRecorded)
	sc.Step(`^exactly one resource type update is recorded for "([^"]*)"$`, w.exactlyOneUpdateRecorded)

	sc.Step(`^the boot reconcile reports "([^"]*)" held at its stored definition for "([^"]*)"$`, w.bootReportsHeld)
	sc.Step(`^the boot reconcile reports the "([^"]*)" context term as held for "([^"]*)"$`,
		w.bootReportsContextTermHeld)
	sc.Step(`^the boot reconcile reports "([^"]*)" as updated$`, w.bootReportsUpdated)
	sc.Step(`^the boot reconcile does not report "([^"]*)" as updated$`, w.bootDoesNotReportUpdated)
	sc.Step(`^the boot reconcile names "([^"]*)" as a property whose writes are still dropped$`, w.bootNamesDropped)

	// The adopt-term migration extends this world rather than forking it: the
	// situation it starts from is the one the guard scenarios leave behind.
	w.registerAdoptionSteps(sc)

	// Issue #515's compact-edge contract extends the same world: the shapes it
	// reads are the ones every scenario above writes.
	w.registerCompactEdgeSteps(sc)
	w.registerEdgeKeyNormalizationSteps(sc)
	w.registerIRIEdgeKeyCountSteps(sc)
	w.registerControlKeywordSteps(sc)
	w.registerConflictAdoptionSteps(sc)
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
//
// The `type` column carries the table's vocabulary rather than a raw JSON type:
// "reference" is a single URN and "reference list" is an ARRAY of URNs. Issue
// #513 needed the second word — a schema can mark an array property
// `x-resource-type`, and until this table could say so no scenario could reach
// the shape that never reads back.
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
		// JSON type of a reference is a string holding the target's URN, and of
		// a reference list an array of them.
		if p.references != "" {
			switch p.jsonTyp {
			case "reference list":
				p.list = true
				p.jsonTyp = "array"
			default:
				p.jsonTyp = "string"
			}
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

// thePresetAddsReferenceList adds a reference the schema declares as an array of
// URNs — the mealplanning preset's shape for suitableForDiet (issue #513).
func (w *contextWorld) thePresetAddsReferenceList(name, target string) error {
	return w.addWidgetProperty(contextProperty{name: name, jsonTyp: "array", references: target, list: true})
}

// thePresetDeclaresContextTerm declares a raw `@context` entry on the next
// build's "widget" type: a prefix definition, an `@type`, or a term named at an
// explicit IRI. It overrides whatever presetType would generate for that key,
// which is how a scenario says "the next build names a DIFFERENT IRI".
func (w *contextWorld) thePresetDeclaresContextTerm(term, value string) error {
	w.widgetContextExtras[term] = value
	return nil
}

// thePresetAddsReferenceWithoutContextEntry reproduces a preset packaging bug:
// the schema marks the property as a reference but the preset's own context
// never declares a term for it, so an additive merge has nothing to add and the
// writes stay dropped.
func (w *contextWorld) thePresetAddsReferenceWithoutContextEntry(name, target string) error {
	return w.addWidgetProperty(
		contextProperty{name: name, jsonTyp: "string", references: target, noContextEntry: true})
}

// thePresetDeclaresContextEntryFor is the later build that fixes a packaging
// bug: the reference property was already declared, and now the preset's own
// context declares a term for it too.
func (w *contextWorld) thePresetDeclaresContextEntryFor(name string) error {
	for i := range w.widgetProps {
		if w.widgetProps[i].name != name {
			continue
		}
		if w.widgetProps[i].references == "" {
			return fmt.Errorf("%q is not a reference property, so no context entry applies", name)
		}
		w.widgetProps[i].noContextEntry = false
		return nil
	}
	return fmt.Errorf("widget declares no property named %q", name)
}

// thePresetAddsLiteral names its type, so a scenario can change the OTHER type
// in the same boot — which is how a negative assertion about one type gets a
// positive control from a healthy one beside it.
func (w *contextWorld) thePresetAddsLiteral(name, jsonTyp, slug string) error {
	p := contextProperty{name: name, jsonTyp: jsonTyp}
	if slug == "vendor" {
		for _, existing := range w.vendorProps {
			if existing.name == p.name {
				return fmt.Errorf("property %q is already declared on vendor", p.name)
			}
		}
		w.vendorProps = append(w.vendorProps, p)
		return nil
	}
	return w.addWidgetProperty(p)
}

// presetType renders one type's schema and context from its property list. A
// reference contributes an x-resource-type marker to the schema AND a term to
// the context — declaring one without the other is exactly the defect, so the
// two are built side by side here where the asymmetry is visible.
//
// extras are raw `@context` entries no property implies — a prefix, an `@type`,
// or a term the scenario wants named at a specific IRI. They are applied LAST
// so a scenario can override the term this generator would derive, which is how
// "the next build names a different IRI" is expressed.
func presetType(
	name, slug string, props []contextProperty, extras map[string]string,
) application.PresetResourceType {
	schemaProps := make([]string, 0, len(props))
	terms := map[string]json.RawMessage{"@vocab": json.RawMessage(`"https://schema.org/"`)}
	for _, p := range props {
		if p.references == "" {
			schemaProps = append(schemaProps, fmt.Sprintf("%q:{\"type\":%q}", p.name, p.jsonTyp))
			continue
		}
		// An array reference declares x-resource-type on the array itself, as
		// the mealplanning preset does; a single reference declares it on the
		// string. Both are the same reference to every path downstream.
		if p.list {
			schemaProps = append(schemaProps, fmt.Sprintf(
				"%q:{\"type\":\"array\",\"items\":{\"type\":\"string\"},"+
					"\"x-resource-type\":%q,\"x-display-property\":\"name\"}", p.name, p.references))
		} else {
			schemaProps = append(schemaProps,
				fmt.Sprintf("%q:{\"type\":\"string\",\"x-resource-type\":%q,\"x-display-property\":\"name\"}",
					p.name, p.references))
		}
		if p.noContextEntry {
			continue
		}
		terms[p.name] = json.RawMessage(
			// Vocab-consistent on purpose: a well-formed preset names the IRI
			// its own @vocab already implies, which is what the real presets do
			// (schema.org/suitableForDiet under @vocab schema.org/). The old
			// harness used an unrelated catalog# IRI, which silently made every
			// added term a predicate MOVE — the very case issue #513 guards,
			// hidden inside scenarios meant to test the plain repair.
			fmt.Sprintf("{\"@id\":\"https://schema.org/%s\",\"@type\":\"@id\"}", p.name))
	}
	for term, value := range extras {
		encoded, err := json.Marshal(value)
		if err != nil {
			continue
		}
		terms[term] = encoded
	}
	encodedContext, err := json.Marshal(terms)
	if err != nil {
		encodedContext = json.RawMessage(`{"@vocab":"https://schema.org/"}`)
	}
	return application.PresetResourceType{
		Name:        name,
		Slug:        slug,
		Description: "issue #510 acceptance type",
		Context:     encodedContext,
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
			presetType("Vendor", "vendor", w.vendorProps, nil),
			presetType("Widget", "widget", w.widgetProps, w.widgetContextExtras),
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
	// updated, heldSchema, heldContext and dropped are keyed by slug.
	updated map[string]bool
	// heldSchema and heldContext are kept apart on purpose: both are reported
	// on a line reading "held at their stored definition", and a merged capture
	// would let a term reported as a SCHEMA conflict satisfy an assertion about
	// a CONTEXT term (issue #513, P2-4).
	heldSchema  map[string][]string
	heldContext map[string][]string
	dropped     map[string]string
	// lines is every warn/error line the boot emitted, rendered whole. Issue
	// #515's refusal has no bucket of its own here on purpose: the suite must
	// not dictate WHICH report channel carries it, only that an operator sees
	// it, so the assertions search the raw text.
	lines []string
}

func newReconcileLog() *reconcileLog {
	return &reconcileLog{
		updated:     map[string]bool{},
		heldSchema:  map[string][]string{},
		heldContext: map[string][]string{},
		dropped:     map[string]string{},
	}
}

// record appends one rendered log line for the raw-text assertions.
func (l *reconcileLog) record(msg string, fields []interface{}) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf("%s %v", msg, fields))
	l.mu.Unlock()
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
	// Matched on a stable prefix rather than the whole message: the wording
	// names which halves were reconciled and has already changed once, and a
	// test that silently stops matching would report every type as un-updated.
	if strings.Contains(msg, "reconciled resource type") {
		if slug, ok := fieldValue(fields, "slug"); ok {
			c.log.mu.Lock()
			c.log.updated[slug] = true
			c.log.mu.Unlock()
		}
	}
	c.inner.Info(ctx, msg, fields...)
}

func (c *capturingLogger) Warn(ctx context.Context, msg string, fields ...interface{}) {
	c.log.record(msg, fields)
	if strings.Contains(msg, "held at their stored definition") {
		if slug, ok := fieldValue(fields, "slug"); ok {
			c.log.mu.Lock()
			if terms, has := fieldValue(fields, "heldContextTerms"); has {
				c.log.heldContext[slug] = append(c.log.heldContext[slug], terms)
			}
			if props, has := fieldValue(fields, "heldProperties"); has {
				c.log.heldSchema[slug] = append(c.log.heldSchema[slug], props)
			}
			c.log.mu.Unlock()
		}
	}
	c.inner.Warn(ctx, msg, fields...)
}

func (c *capturingLogger) Error(ctx context.Context, msg string, fields ...interface{}) {
	c.log.record(msg, fields)
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
	var resRepo repositories.ResourceRepository
	var tripleRepo repositories.TripleRepository

	registry := w.catalogRegistry
	if w.registry != nil {
		registry = w.registry
	}
	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, registry()),
		// Decorating the Logger is what makes the boot reconcile observable
		// without a production seam existing only for the tests.
		fx.Decorate(func(inner entities.Logger) entities.Logger {
			return &capturingLogger{inner: inner, log: captured}
		}),
		fx.Populate(&rts, &rs, &db, &resRepo, &tripleRepo),
	)
	startCtx, startCancel := context.WithTimeout(ctx, fx.DefaultTimeout)
	defer startCancel()
	// The report is attached BEFORE the start so a boot that refuses to come up
	// still leaves its operator-facing lines readable (issue #515).
	w.bootReport = captured
	w.bootErr = nil
	if err := app.Start(startCtx); err != nil {
		w.bootErr = fmt.Errorf("failed to start app: %w", err)
		return w.bootErr
	}
	w.app, w.rts, w.rs, w.db = app, rts, rs, db
	w.resRepo, w.tripleRepo = resRepo, tripleRepo
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
	// Snapshot the stored schema before the process goes away, so a scenario
	// about a CONTEXT-only reconcile can prove the schema half was left alone.
	if w.rts != nil {
		if rt, err := w.rts.GetBySlug(context.Background(), "widget"); err == nil {
			w.schemaBeforeRestart = rt.Schema()
		}
	}
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

func (w *contextWorld) theOperatorDeletesTerm(term string) error {
	terms, err := w.storedContextOf("widget")
	if err != nil {
		return err
	}
	if _, ok := terms[term]; !ok {
		return fmt.Errorf("stored widget context has no %q entry to delete (terms: %s)", term, sortedTermNames(terms))
	}
	delete(terms, term)
	return w.writeStoredContext("widget", terms)
}

// theOperatorStoresRawContext writes a stored `@context` verbatim, including
// the forms an object-shaped merge cannot handle: JSON-LD permits an array of
// contexts and a bare remote-IRI string, and the API write path validates
// neither, so an operator can and does store them (issue #513).
func (w *contextWorld) theOperatorStoresRawContext(raw string) error {
	raw = strings.TrimSpace(raw)
	if !json.Valid([]byte(raw)) {
		return fmt.Errorf("the scenario's raw context %q is not valid JSON", raw)
	}
	rt, err := w.rts.GetBySlug(context.Background(), "widget")
	if err != nil {
		return fmt.Errorf("failed to load the widget type: %w", err)
	}
	_, err = w.rts.Update(context.Background(), application.UpdateResourceTypeCommand{
		ID:          rt.GetID(),
		Name:        rt.Name(),
		Slug:        rt.Slug(),
		Description: rt.Description(),
		Status:      rt.Status(),
		Context:     json.RawMessage(raw),
		Schema:      rt.Schema(),
	})
	if err != nil {
		return fmt.Errorf("failed to store the raw context for widget: %w", err)
	}
	return nil
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
	w.createdData[slug+"/"+name] = cloneData(data)
	return nil
}

// cloneData copies one resource's flat data so a later update mutates its own
// document rather than the record of what was created.
func cloneData(data map[string]any) map[string]any {
	out := make(map[string]any, len(data))
	for k, v := range data {
		out[k] = v
	}
	return out
}

// updateResource rewrites a widget through the normal update path with the data
// the world last wrote, after applying one change. Update REPLACES a resource's
// data, so a property this document omits is a property the client removed.
func (w *contextWorld) updateResource(name string, mutate func(map[string]any)) error {
	key := "widget/" + name
	id, ok := w.createdIDs[key]
	if !ok {
		return fmt.Errorf("no widget named %q was created in this scenario", name)
	}
	data := cloneData(w.createdData[key])
	mutate(data)
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := w.rs.Update(context.Background(),
		application.UpdateResourceCommand{ID: id, Data: raw}); err != nil {
		return fmt.Errorf("failed to update widget %q: %w", name, err)
	}
	w.createdData[key] = data
	return nil
}

// iUpdateWidgetClearing removes a property from the widget's document and
// rewrites it — the client-side shape of "this widget no longer has a maker".
func (w *contextWorld) iUpdateWidgetClearing(name, property string) error {
	return w.updateResource(name, func(data map[string]any) {
		delete(data, property)
	})
}

// iUpdateWidgetSettingEmpty is the other shape a client clears a reference in:
// the property is still named, with an empty value. The write path drops an
// empty reference, so it must reach the projection as the same clear.
func (w *contextWorld) iUpdateWidgetSettingEmpty(name, property string) error {
	return w.updateResource(name, func(data map[string]any) {
		if w.widgetPropertyIsList(property) {
			data[property] = []string{}
			return
		}
		data[property] = ""
	})
}

// iUpdateWidgetSettingReference rebinds a reference, so a scenario can prove a
// cleared column is not stuck at NULL.
func (w *contextWorld) iUpdateWidgetSettingReference(name, property, vendorName string) error {
	value, err := w.referenceValue(property, vendorName)
	if err != nil {
		return err
	}
	return w.updateResource(name, func(data map[string]any) {
		data[property] = value
	})
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

func (w *contextWorld) aWidgetExists(name string) error {
	return w.createResource("widget", name, map[string]any{})
}

// widgetPropertyIsList reports whether the next build declares this widget
// property as an ARRAY of references, which decides the JSON shape a scenario's
// value has to be written in.
func (w *contextWorld) widgetPropertyIsList(property string) bool {
	for _, p := range w.widgetProps {
		if p.name == property {
			return p.list
		}
	}
	return false
}

// referenceValue turns a scenario's vendor cell — one name, or several
// separated by commas — into the JSON value for that property: a bare URN for a
// single reference, an array of URNs for a reference list.
func (w *contextWorld) referenceValue(property, cell string) (any, error) {
	var ids []string
	for _, vendorName := range strings.Split(cell, ",") {
		id, err := w.vendorID(strings.TrimSpace(vendorName))
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if w.widgetPropertyIsList(property) {
		return ids, nil
	}
	if len(ids) != 1 {
		return nil, fmt.Errorf("%q is a single reference but the scenario names %d vendors", property, len(ids))
	}
	return ids[0], nil
}

func (w *contextWorld) iCreateWidgetWithReference(name, property, vendorName string) error {
	value, err := w.referenceValue(property, vendorName)
	if err != nil {
		return err
	}
	return w.createResource("widget", name, map[string]any{property: value})
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
		value, err := w.referenceValue(property, row.Cells[1].Value)
		if err != nil {
			return err
		}
		data[property] = value
	}
	return w.createResource("widget", name, data)
}

func (w *contextWorld) iCreateWidgetWithLiteral(name, property, value string) error {
	return w.createResource("widget", name, map[string]any{property: value})
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

// readingReturnsReferenceList asserts an ARRAY reference reads back with every
// URN it was written with, in order. The stored encoding is the reader's
// business — a JSON array in a TEXT column and a decoded slice both count — so
// the assertion is on the values, not the shape they arrive in.
func (w *contextWorld) readingReturnsReferenceList(name, property, vendorNames string) error {
	want, err := w.vendorIDs(vendorNames)
	if err != nil {
		return err
	}
	row, err := w.flatRead(name)
	if err != nil {
		return err
	}
	got, ok := row[property]
	if !ok || got == nil {
		return fmt.Errorf("read of %q carries no %q value — the array reference was dropped (keys: %s)",
			name, property, mapKeys(row))
	}
	ids, err := asIDList(got)
	if err != nil {
		return fmt.Errorf("read of %q returned %s = %v, which is not a list of references: %w",
			name, property, got, err)
	}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		return fmt.Errorf("read of %q returned %s = %v, want the vendors %q (%v)",
			name, property, ids, vendorNames, want)
	}
	return nil
}

// vendorIDs resolves a comma-separated list of vendor names to their URNs.
func (w *contextWorld) vendorIDs(names string) ([]string, error) {
	var ids []string
	for _, vendorName := range strings.Split(names, ",") {
		id, err := w.vendorID(strings.TrimSpace(vendorName))
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// asIDList normalizes the shapes a list-valued reference can come back as: a
// JSON array in a TEXT column, a decoded slice, or a lone URN.
func asIDList(value any) ([]string, error) {
	switch v := value.(type) {
	case []string:
		return v, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, fmt.Sprintf("%v", item))
		}
		return out, nil
	case string:
		// Deliberately NOT decoding a JSON-array string here. The projection
		// stores a list reference that way, but the read path decodes it before
		// the value reaches a client; accepting the encoded form would let the
		// suite pass while the API served a string where the schema promises a
		// list (issue #513).
		if strings.HasPrefix(strings.TrimSpace(v), "[") {
			return nil, fmt.Errorf(
				"value is still the encoded column %q — the read path did not decode it to a list", v)
		}
		return []string{v}, nil
	default:
		return nil, fmt.Errorf("unsupported value type %T", value)
	}
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

// canonicalStillCarriesEdge reads the canonical JSON-LD record rather than the
// projection, proving the reference survives as an EDGE that still resolves
// through the type's current context.
//
// Resolution is the whole point of the assertion, so it goes through
// EdgeValues and nothing else: the edge is keyed by the predicate IRI that was
// current when it was written, and a merge that repoints that property's term
// leaves the bytes in place while making them unreadable. A lookup by property
// name would pass on exactly that loss (issue #513, P2-1).
func (w *contextWorld) canonicalStillCarriesEdge(name, property, vendorName string) error {
	return w.canonicalCarriesEdgeList(name, property, vendorName)
}

// canonicalCarriesEdgeList is the list-valued form: every named vendor, in
// order. A single reference is the one-element case, so both steps share it.
func (w *contextWorld) canonicalCarriesEdgeList(name, property, vendorNames string) error {
	want, err := w.vendorIDs(vendorNames)
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
	got := application.EdgeValues(res.Data(), rt.Context(), property)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		return fmt.Errorf(
			"canonical record for %q resolves %q to %v, want the vendors %q (%v) — payload: %s",
			name, property, got, vendorNames, want, res.Data())
	}
	return nil
}

// widgetsShareAnRDFType compares the rdf:type two resources carry in their
// canonical records. A boot that moves the type's class IRI — by merging an
// `@type` or a prefix an `@type` compacts through — splits a type's resources
// across two classes, and every class-scoped query then answers with half the
// data (issue #513, P0-5).
func (w *contextWorld) widgetsShareAnRDFType(first, second string) error {
	firstType, err := w.widgetRDFType(first)
	if err != nil {
		return err
	}
	secondType, err := w.widgetRDFType(second)
	if err != nil {
		return err
	}
	if firstType != secondType {
		return fmt.Errorf(
			"widget %q carries rdf:type %q and widget %q carries %q — the boot moved the type's class, "+
				"so class-scoped queries can no longer see both", first, firstType, second, secondType)
	}
	return nil
}

func (w *contextWorld) widgetRDFType(name string) (string, error) {
	id, ok := w.createdIDs["widget/"+name]
	if !ok {
		return "", fmt.Errorf("no widget named %q was created in this scenario", name)
	}
	res, err := w.rs.GetByID(context.Background(), id)
	if err != nil {
		return "", fmt.Errorf("failed to read widget %q by id: %w", name, err)
	}
	var data map[string]any
	if err := json.Unmarshal(res.Data(), &data); err != nil {
		return "", fmt.Errorf("canonical data for %q is not a JSON object: %w", name, err)
	}
	got, ok := jsonLDField(data, "@type")
	if !ok {
		return "", fmt.Errorf("canonical record for %q carries no @type (payload: %s)", name, res.Data())
	}
	return fmt.Sprintf("%v", got), nil
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
	// A term is spelled as a bare IRI string or as {"@id": …}; both name the
	// same mapping, and an adopted preset definition arrives in the latter.
	if obj, isObj := got.(map[string]any); isObj {
		if id, has := obj["@id"]; has {
			got = id
		}
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

// theStoredSchemaSurvivedTheRestart asserts a boot that only had a context to
// merge left the stored schema byte-for-byte alone. The two halves fall back to
// the stored value independently, and nothing else in the suite would notice
// the two being crossed.
func (w *contextWorld) theStoredSchemaSurvivedTheRestart() error {
	if len(w.schemaBeforeRestart) == 0 {
		return fmt.Errorf("no stored schema was captured before a restart in this scenario")
	}
	rt, err := w.rts.GetBySlug(context.Background(), "widget")
	if err != nil {
		return fmt.Errorf("failed to load the widget type: %w", err)
	}
	if string(rt.Schema()) != string(w.schemaBeforeRestart) {
		return fmt.Errorf("the stored widget schema changed across a context-only reconcile:\nbefore: %s\nafter:  %s",
			w.schemaBeforeRestart, rt.Schema())
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

// bootReportsHeld accepts either category: the scenarios using it only care
// that the operator was told this definition stayed at its stored form.
func (w *contextWorld) bootReportsHeld(term, slug string) error {
	r, err := w.report()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, line := range append(append([]string{}, r.heldContext[slug]...), r.heldSchema[slug]...) {
		if strings.Contains(line, term) {
			return nil
		}
	}
	return fmt.Errorf("boot reconcile did not report %q held for %q (context: %v, schema: %v)",
		term, slug, r.heldContext[slug], r.heldSchema[slug])
}

// bootReportsContextTermHeld is the narrower assertion: the term was held as a
// CONTEXT term. A schema conflict reported for the same name does not satisfy
// it — the two need different operator fixes.
func (w *contextWorld) bootReportsContextTermHeld(term, slug string) error {
	r, err := w.report()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, line := range r.heldContext[slug] {
		if strings.Contains(line, term) {
			if w.heldAccounted == nil {
				w.heldAccounted = map[string]bool{}
			}
			w.heldAccounted[slug+"|"+term] = true
			return nil
		}
	}
	return fmt.Errorf(
		"boot reconcile did not report the %q context term as held for %q, so the preset's IRI was adopted "+
			"over the one existing edges use (held context terms: %v)", term, slug, r.heldContext[slug])
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
