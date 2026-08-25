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
	"strconv"
	"strings"
	"testing"

	"github.com/cucumber/godog"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/pkg/jsonld"
	"github.com/wepala/weos/v3/pkg/utils"
)

// Issue #520: the house vocabulary moves from weos.org to weos.io. These
// scenarios run against the REAL built-in presets, so they get their own
// world: the shared contextWorld with its registry pointed at
// presets.NewDefaultRegistry() — or at a copy of it rewritten to look like
// the build before the move.

const (
	oldHouseDomain = "https://weos.org/vocab/"
	newHouseDomain = "https://weos.io/vocab/"
)

func TestHouseVocabularyDomain(t *testing.T) {
	runFeatureWith(t, "house-vocabulary-domain", "features/house_vocabulary_domain.feature", initHouseVocabularyScenario)
}

// vocabWorld is the shared world plus the build the next boot represents.
type vocabWorld struct {
	*contextWorld
	// extraLiterals are string properties the "build that moved the
	// vocabulary" also adds, keyed by type slug.
	extraLiterals map[string][]string
	// fixtureSeq numbers the auto-created prerequisite resources.
	fixtureSeq int
	// lastID / lastSlug are the resource "a <slug> resource is created" made.
	lastID, lastSlug string
	// restampReport is the last --restamp run's report.
	restampReport *application.NormalizeEdgeKeysReport
}

func initHouseVocabularyScenario(sc *godog.ScenarioContext) {
	w := &vocabWorld{
		contextWorld:  &contextWorld{createdIDs: map[string]string{}, widgetContextExtras: map[string]string{}},
		extraLiterals: map[string][]string{},
	}
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})

	sc.Step(`^a clean WeOS database$`, w.aCleanDatabaseOnThisBuild)
	sc.Step(`^a WeOS database provisioned by the build before the vocabulary moved$`, w.aDatabaseFromTheOldBuild)
	sc.Step(`^the twin restarts on the build that moved the vocabulary(?: again)?$`, w.restartOnThisBuild)
	sc.Step(`^the build that moved the vocabulary also adds a "([^"]*)" string property to "([^"]*)"$`,
		w.thisBuildAlsoAddsLiteral)

	sc.Step(`^the operator installs every built-in preset$`, w.installEveryPreset)
	sc.Step(`^the operator installs the "([^"]*)" preset$`, w.installPreset)

	sc.Step(`^no installed resource type resolves any term, prefix or "@type" under "([^"]*)"$`, w.noTypeResolvesUnder)
	sc.Step(`^every installed type of "([^"]*)" that declares "([^"]*)" resolves it to "([^"]*)"$`,
		w.everyTypeResolvesPrefix)
	sc.Step(`^every house IRI the installed types of "([^"]*)" resolve is under "([^"]*)"$`, w.everyHouseIRIUnder)
	sc.Step(`^every reference property of every installed type reverse-maps to its own name$`, w.everyReferenceReverseMaps)
	sc.Step(`^no two properties of one installed type resolve to the same predicate IRI$`, w.noPredicateShared)
	sc.Step(`^the boot reconcile reports the "([^"]*)" context term as held for "([^"]*)"$`, w.bootReportsContextTermHeld)
	sc.Step(`^the boot reconcile reports the "([^"]*)" context term as held for every installed meal-planning type `+
		`whose stored context declares it$`, w.heldForEveryMealPlanningType)
	sc.Step(`^no installed meal-planning type has "([^"]*)" resolving to "([^"]*)"$`, w.noMealPlanningTypeResolves)
	sc.Step(`^the stored "([^"]*)" context still maps "([^"]*)" to "([^"]*)"$`, w.storedContextMaps)
	sc.Step(`^the "([^"]*)" projection table has a "([^"]*)" column$`, w.projectionTableHasColumn)

	sc.Step(`^a "([^"]*)" resource is created$`, w.aResourceIsCreated)
	sc.Step(`^an? "([^"]*)" named "([^"]*)" exists$`, w.aNamedResourceExists)
	sc.Step(`^I create an? "([^"]*)" named "([^"]*)" with "([^"]*)" referring to the "([^"]*)" "([^"]*)"$`,
		w.createWithReference)
	sc.Step(`^I create an? "([^"]*)" named "([^"]*)" with "([^"]*)" set to "([^"]*)"$`, w.createWithLiteral)
	sc.Step(`^I create an? "([^"]*)" named "([^"]*)" with these references:$`, w.createWithReferences)

	sc.Step(`^that resource carries the RDF type "([^"]*)"$`, w.lastResourceCarriesType)
	sc.Step(`^no resource carries an RDF type under "([^"]*)"$`, w.noResourceCarriesTypeUnder)
	sc.Step(`^the triple store holds "([^"]*)" from the "([^"]*)" "([^"]*)" to the "([^"]*)" "([^"]*)"$`,
		w.tripleStoreHoldsEdge)
	sc.Step(`^the triple store holds no edge under "([^"]*)" from the "([^"]*)" "([^"]*)"$`, w.tripleStoreHoldsNoEdgeUnder)
	sc.Step(`^the triple store holds "([^"]*)" from the "([^"]*)" "([^"]*)" with the value "([^"]*)"$`,
		w.documentStatesLiteral)
	sc.Step(`^the triple store holds no statement under "([^"]*)" about the "([^"]*)" "([^"]*)"$`,
		w.documentStatesNothingUnder)
	sc.Step(`^reading the "([^"]*)" "([^"]*)" back through the projection returns "([^"]*)" as the "([^"]*)" "([^"]*)"$`,
		w.projectionReturnsReference)
	sc.Step(`^reading the "([^"]*)" "([^"]*)" back through the projection returns "([^"]*)" as "([^"]*)"$`,
		w.projectionReturnsLiteral)
	sc.Step(`^the API read of the "([^"]*)" "([^"]*)" returns "([^"]*)" as the "([^"]*)" "([^"]*)"$`,
		w.apiReturnsReference)
	sc.Step(`^the JSON-LD representation of the "([^"]*)" "([^"]*)" still carries an? "([^"]*)" edge `+
		`to the "([^"]*)" "([^"]*)"$`, w.documentCarriesEdge)

	// --- the re-stamp (issue #520, decided 2026-08-25) ---
	sc.Step(`^the operator maps "([^"]*)" to "([^"]*)" in the stored "([^"]*)" context$`, w.operatorMapsTermOf)
	sc.Step(`^an? "([^"]*)" named "([^"]*)" with "([^"]*)" referring to the "([^"]*)" "([^"]*)" exists$`,
		w.createWithReference)
	sc.Step(`^I create an? "([^"]*)" named "([^"]*)" with "([^"]*)" referring to the "([^"]*)" "([^"]*)" `+
		`and "([^"]*)" set to "([^"]*)"$`, w.createWithReferenceAndLiteral)
	sc.Step(`^the operator reprojects the event feed$`, w.theOperatorReprojects)
	sc.Step(`^the operator re-stamps the stored documents as a dry run$`, w.restampDryRun)
	sc.Step(`^the operator re-stamps the stored documents and writes$`, w.restampAndWrite)
	sc.Step(`^the re-stamp reports (\d+) events? to re-stamp for "([^"]*)"$`, w.restampReportsForType)
	sc.Step(`^the re-stamp reports nothing to re-stamp for "([^"]*)"$`, w.restampReportsNothingForType)
	sc.Step(`^the re-stamp reports nothing to re-stamp$`, w.restampReportsNothing)
	sc.Step(`^the re-stamp reports itself as a dry run$`, w.restampReportsDryRun)
	sc.Step(`^the re-stamp re-stamped (\d+) events? for "([^"]*)"$`, w.restampedForType)
	sc.Step(`^the re-stamp re-stamped 0 events$`, w.restampedNothing)
	sc.Step(`^the stored events are byte-identical to the ones stored before the (?:second )?run$`,
		w.storedEventsUnchanged)
	sc.Step(`^the event feed holds the same events in the same order as before the run$`, w.eventFeedSameOrder)
	sc.Step(`^every event keeps its aggregate id, sequence number and event type$`, w.eventsKeepIdentity)
	sc.Step(`^no event of a type other than "([^"]*)", "([^"]*)", "([^"]*)" or "([^"]*)" was re-stamped$`,
		w.onlyTheseFourEventTypesRewritten)
	sc.Step(`^the entity node of the stored event for the "([^"]*)" "([^"]*)" is byte-identical to the one stored `+
		`before the run apart from its "@type"$`, w.entityNodeUnchangedApartFromType)
	sc.Step(`^the stored event for the "([^"]*)" "([^"]*)" maps "([^"]*)" to "([^"]*)" in its own context$`,
		w.storedEventContextMaps)
	sc.Step(`^the stored event for the "([^"]*)" "([^"]*)" keys its "([^"]*)" edge by the property name$`,
		w.storedEventKeysEdgeByPropertyName)
	sc.Step(`^the read of every resource with a reference property is captured$`, w.captureEveryRead)
	sc.Step(`^the read of every resource with a reference property matches what was captured$`, w.everyReadMatches)
	sc.Step(`^the "([^"]*)" "([^"]*)" carries the RDF type "([^"]*)" in the stored document$`, w.resourceCarriesType)
	sc.Step(`^every "([^"]*)" resource carries the same RDF type in the stored document$`, w.everyResourceSameType)
	sc.Step(`^no resource carries an RDF type under "([^"]*)" in the stored document$`, w.noResourceCarriesTypeUnder)
	sc.Step(`^no "([^"]*)" resource carries an RDF type under "([^"]*)" in the stored document$`,
		w.noResourceOfTypeCarriesTypeUnder)
	sc.Step(`^the stored document states "([^"]*)" from the "([^"]*)" "([^"]*)" to the "([^"]*)" "([^"]*)"$`,
		w.documentStatesEdge)
	sc.Step(`^the stored document states no edge under "([^"]*)" from the "([^"]*)" "([^"]*)"$`,
		w.documentStatesNoEdgeUnder)
	sc.Step(`^the stored document states "([^"]*)" from the "([^"]*)" "([^"]*)" with the value "([^"]*)"$`,
		w.documentStatesLiteral)
	sc.Step(`^the stored document states no statement under "([^"]*)" about the "([^"]*)" "([^"]*)"$`,
		w.documentStatesNothingUnder)
}

// --- the re-stamp ---

func (w *vocabWorld) operatorMapsTermOf(term, iri, slug string) error {
	terms, err := w.storedContextOf(slug)
	if err != nil {
		return err
	}
	terms[term] = iri
	return w.writeStoredContext(slug, terms)
}

func (w *vocabWorld) createWithReferenceAndLiteral(
	slug, name, property, targetSlug, target, literal, value string,
) error {
	id, err := w.targetID(targetSlug, target)
	if err != nil {
		return err
	}
	v, err := w.literalValue(slug, literal, value)
	if err != nil {
		return err
	}
	return w.createResourceOf(slug, name, map[string]any{property: id, literal: v})
}

// restamp runs normalize-edge-keys --restamp the way the CLI does: the live
// app stopped, its own narrow runtime built on the CURRENT build's registry.
func (w *vocabWorld) restamp(write bool) error {
	w.registry = w.thisBuild
	if err := w.normalizeWith(application.NormalizeEdgeKeysOptions{Write: write, Restamp: true}); err != nil {
		return err
	}
	w.restampReport = w.normalizeReport
	return nil
}

func (w *vocabWorld) restampDryRun() error   { return w.restamp(false) }
func (w *vocabWorld) restampAndWrite() error { return w.restamp(true) }

func (w *vocabWorld) restampCount(slug string) (int, bool, error) {
	if w.restampReport == nil {
		return 0, false, fmt.Errorf("the re-stamp has not run in this scenario")
	}
	if slug == "" {
		return w.restampReport.Restamped, w.restampReport.DryRun, nil
	}
	if t := w.restampReport.Types[slug]; t != nil {
		return t.Restamped, w.restampReport.DryRun, nil
	}
	return 0, w.restampReport.DryRun, nil
}

func (w *vocabWorld) restampExpect(slug string, want int, dryRun bool) error {
	got, isDry, err := w.restampCount(slug)
	if err != nil {
		return err
	}
	if isDry != dryRun {
		return fmt.Errorf("the re-stamp reported DryRun=%v, want %v", isDry, dryRun)
	}
	if got != want {
		return fmt.Errorf("the re-stamp reported %d event(s) for %q, want %d (report: %s)",
			got, slug, want, marshalForMessage(w.restampReport))
	}
	return nil
}

func (w *vocabWorld) restampReportsForType(n int, slug string) error {
	return w.restampExpect(slug, n, true)
}
func (w *vocabWorld) restampReportsNothingForType(slug string) error {
	return w.restampExpect(slug, 0, true)
}
func (w *vocabWorld) restampReportsNothing() error { return w.restampExpect("", 0, true) }
func (w *vocabWorld) restampedForType(n int, slug string) error {
	return w.restampExpect(slug, n, false)
}
func (w *vocabWorld) restampedNothing() error { return w.restampExpect("", 0, false) }

func (w *vocabWorld) restampReportsDryRun() error {
	if w.restampReport == nil || !w.restampReport.DryRun {
		return fmt.Errorf("the re-stamp did not report itself as a dry run")
	}
	return nil
}

// entityNodeUnchangedApartFromType compares the Created event's entity node
// before and after the run with @type removed from both.
func (w *vocabWorld) entityNodeUnchangedApartFromType(slug, name string) error {
	id, err := w.targetID(slug, name)
	if err != nil {
		return err
	}
	strip := func(events []storedEvent) string {
		node := entityNodesByAggregate(events)[id]
		var m map[string]any
		if json.Unmarshal([]byte(node), &m) != nil {
			return node
		}
		delete(m, "@type")
		return marshalForMessage(m)
	}
	after, err := w.snapshotEvents()
	if err != nil {
		return err
	}
	before, now := strip(w.eventsBeforeNormalize), strip(after)
	if before == "" || before != now {
		return fmt.Errorf("the entity node of %q changed beyond @type:\n before %s\n after  %s", name, before, now)
	}
	return nil
}

func (w *vocabWorld) resourceCarriesType(slug, name, class string) error {
	id, err := w.targetID(slug, name)
	if err != nil {
		return err
	}
	doc, embedded, err := w.document(id)
	if err != nil {
		return err
	}
	if got := classOf(doc, embedded); got != class {
		return fmt.Errorf("%s %q carries the RDF type %s, want %s", slug, name, got, class)
	}
	return nil
}

func (w *vocabWorld) everyResourceSameType(slug string) error {
	classes := map[string]string{}
	for key, id := range w.createdIDs {
		if !strings.HasPrefix(key, slug+"/") {
			continue
		}
		doc, embedded, err := w.document(id)
		if err != nil {
			return err
		}
		classes[key] = classOf(doc, embedded)
	}
	seen := ""
	for key, class := range classes {
		if seen == "" {
			seen = class
		}
		if class != seen {
			return fmt.Errorf("%s resources carry different RDF types: %v", slug, classes)
		}
		_ = key
	}
	return nil
}

// documentStatesEdge derives the statement the graph store would: the edges
// node's key resolved through the document's OWN context.
func (w *vocabWorld) documentEdges(slug, name string) (map[string][]string, error) {
	id, err := w.targetID(slug, name)
	if err != nil {
		return nil, err
	}
	doc, embedded, err := w.document(id)
	if err != nil {
		return nil, err
	}
	graph, _ := doc["@graph"].([]any)
	out := map[string][]string{}
	if len(graph) < 2 {
		return out, nil
	}
	edges, _ := graph[1].(map[string]any)
	vocab, forward := jsonld.ParseContext(embedded)
	for key, val := range edges {
		if key == "@id" {
			continue
		}
		predicate := key
		if !jsonld.IsIRIKey(key) {
			predicate = jsonld.ResolvePredicateIRI(key, vocab, forward)
		}
		ids, _ := jsonld.EdgeIDs(val)
		out[predicate] = append(out[predicate], ids...)
	}
	return out, nil
}

func (w *vocabWorld) documentStatesEdge(predicate, slug, name, targetSlug, target string) error {
	want, err := w.targetID(targetSlug, target)
	if err != nil {
		return err
	}
	edges, err := w.documentEdges(slug, name)
	if err != nil {
		return err
	}
	for _, id := range edges[predicate] {
		if id == want {
			return nil
		}
	}
	return fmt.Errorf("the stored document of %s %q states no %s -> %s (edges: %v)", slug, name, predicate, want, edges)
}

func (w *vocabWorld) documentStatesNoEdgeUnder(ns, slug, name string) error {
	edges, err := w.documentEdges(slug, name)
	if err != nil {
		return err
	}
	for predicate := range edges {
		if strings.HasPrefix(predicate, ns) {
			return fmt.Errorf("the stored document of %s %q states an edge under %s: %s", slug, name, ns, predicate)
		}
	}
	return nil
}

// --- builds ---

// thisBuild is the registry as authored now: the vocabulary on weos.io, plus
// whatever the scenario said the new build also adds.
func (w *vocabWorld) thisBuild() *application.PresetRegistry {
	reg := presets.NewDefaultRegistry()
	if len(w.extraLiterals) == 0 {
		return reg
	}
	return rewriteRegistry(reg, func(pt *application.PresetResourceType) {
		for _, prop := range w.extraLiterals[pt.Slug] {
			pt.Schema = addStringProperty(pt.Schema, prop)
		}
	})
}

// oldBuild is the same registry with the house vocabulary rewritten back to
// weos.org — a string substitution over each type's context rather than a
// second copy of the presets, so it cannot drift from what shipped.
func (w *vocabWorld) oldBuild() *application.PresetRegistry {
	rewrote := 0
	reg := rewriteRegistry(presets.NewDefaultRegistry(), func(pt *application.PresetResourceType) {
		before := string(pt.Context)
		pt.Context = json.RawMessage(strings.ReplaceAll(before, newHouseDomain, oldHouseDomain))
		if string(pt.Context) != before {
			rewrote++
		}
	})
	if rewrote == 0 {
		// The shim would otherwise be the current build in disguise, and every
		// upgrade scenario would pass without an upgrade having happened.
		panic("the old-build shim rewrote no type context: no built-in preset names the house domain")
	}
	return reg
}

func rewriteRegistry(
	src *application.PresetRegistry, edit func(*application.PresetResourceType),
) *application.PresetRegistry {
	out := application.NewPresetRegistry()
	for _, preset := range src.List() {
		def := preset
		types := make([]application.PresetResourceType, len(preset.Types))
		copy(types, preset.Types)
		for i := range types {
			edit(&types[i])
		}
		def.Types = types
		out.MustAdd(def)
	}
	return out
}

func addStringProperty(schema json.RawMessage, name string) json.RawMessage {
	var s map[string]any
	if json.Unmarshal(schema, &s) != nil || s == nil {
		s = map[string]any{"type": "object"}
	}
	props, _ := s["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
	}
	props[name] = map[string]any{"type": "string"}
	s["properties"] = props
	out, _ := json.Marshal(s)
	return out
}

func (w *vocabWorld) provision() error {
	dir, err := os.MkdirTemp("", "weos-house-vocab-e2e-")
	if err != nil {
		return err
	}
	w.tmpDir = dir
	w.dsn = filepath.Join(dir, "test.db")
	return w.boot()
}

func (w *vocabWorld) aCleanDatabaseOnThisBuild() error {
	w.registry = w.thisBuild
	return w.provision()
}

func (w *vocabWorld) aDatabaseFromTheOldBuild() error {
	w.registry = w.oldBuild
	return w.provision()
}

func (w *vocabWorld) restartOnThisBuild() error {
	w.registry = w.thisBuild
	w.stop()
	return w.boot()
}

func (w *vocabWorld) thisBuildAlsoAddsLiteral(prop, slug string) error {
	w.extraLiterals[slug] = append(w.extraLiterals[slug], prop)
	return nil
}

// --- installing ---

func (w *vocabWorld) installPreset(name string) error {
	if _, err := w.rts.InstallPreset(context.Background(), name, false); err != nil {
		return fmt.Errorf("failed to install the %q preset: %w", name, err)
	}
	return nil
}

func (w *vocabWorld) installEveryPreset() error {
	for _, preset := range presets.NewDefaultRegistry().List() {
		if err := w.installPreset(preset.Name); err != nil {
			return err
		}
	}
	return nil
}

// --- reading the installed types ---

func (w *vocabWorld) installedTypes() ([]*entities.ResourceType, error) {
	var out []*entities.ResourceType
	cursor := ""
	for {
		page, err := w.rts.List(context.Background(), cursor, 200)
		if err != nil {
			return nil, fmt.Errorf("failed to list resource types: %w", err)
		}
		out = append(out, page.Data...)
		if !page.HasMore {
			return out, nil
		}
		cursor = page.Cursor
	}
}

// resolvedIRIs lists every IRI a stored context resolves: @vocab, each term
// and prefix, and the expanded @type.
func resolvedIRIs(ldContext json.RawMessage) map[string]string {
	vocab, forward := jsonld.ParseContext(ldContext)
	out := map[string]string{}
	if vocab != "" {
		out["@vocab"] = vocab
	}
	for term, iri := range forward {
		out[term] = iri
	}
	var raw map[string]any
	if json.Unmarshal(ldContext, &raw) == nil {
		if typ, ok := raw["@type"].(string); ok && typ != "" {
			out["@type"] = jsonld.ExpandIRI(typ, vocab, raw)
		}
	}
	return out
}

func (w *vocabWorld) noTypeResolvesUnder(ns string) error {
	types, err := w.installedTypes()
	if err != nil {
		return err
	}
	for _, rt := range types {
		for term, iri := range resolvedIRIs(rt.Context()) {
			if strings.HasPrefix(iri, ns) {
				return fmt.Errorf("installed type %q resolves %q to %s, under %s", rt.Slug(), term, iri, ns)
			}
		}
	}
	return nil
}

func presetTypeSlugs(preset string) ([]string, error) {
	def, ok := presets.NewDefaultRegistry().Get(preset)
	if !ok {
		return nil, fmt.Errorf("no built-in preset named %q", preset)
	}
	slugs := make([]string, 0, len(def.Types))
	for _, pt := range def.Types {
		slugs = append(slugs, pt.Slug)
	}
	return slugs, nil
}

func (w *vocabWorld) everyTypeResolvesPrefix(preset, prefix, ns string) error {
	slugs, err := presetTypeSlugs(preset)
	if err != nil {
		return err
	}
	declared := 0
	for _, slug := range slugs {
		terms, err := w.storedContextOf(slug)
		if err != nil {
			return err
		}
		got, ok := terms[prefix]
		if !ok {
			continue
		}
		declared++
		if fmt.Sprintf("%v", got) != ns {
			return fmt.Errorf("installed type %q maps %q to %v, want %s", slug, prefix, got, ns)
		}
	}
	if declared == 0 {
		return fmt.Errorf("no installed type of %q declares the %q prefix", preset, prefix)
	}
	return nil
}

func isHouseIRI(iri string) bool {
	return strings.Contains(iri, "weos.io") || strings.Contains(iri, "weos.org")
}

func (w *vocabWorld) everyHouseIRIUnder(preset, ns string) error {
	slugs, err := presetTypeSlugs(preset)
	if err != nil {
		return err
	}
	for _, slug := range slugs {
		rt, err := w.rts.GetBySlug(context.Background(), slug)
		if err != nil {
			return fmt.Errorf("failed to load the %q type: %w", slug, err)
		}
		for term, iri := range resolvedIRIs(rt.Context()) {
			if isHouseIRI(iri) && !strings.HasPrefix(iri, ns) {
				return fmt.Errorf("installed type %q resolves %q to %s, not under %s", slug, term, iri, ns)
			}
		}
	}
	return nil
}

func (w *vocabWorld) everyReferenceReverseMaps() error {
	types, err := w.installedTypes()
	if err != nil {
		return err
	}
	for _, rt := range types {
		reverse := jsonld.BuildReverseMap(rt.Context())
		for _, ref := range application.ExtractReferenceProperties(rt.Schema(), rt.Context()) {
			vocab, _ := jsonld.ParseContext(rt.Context())
			if got, ok := reverse[ref.PredicateIRI]; ok && got != ref.PropertyName {
				return fmt.Errorf("type %q: %s reverse-maps to %q, not %q", rt.Slug(), ref.PredicateIRI, got, ref.PropertyName)
			} else if !ok && (vocab == "" || ref.PredicateIRI != vocab+ref.PropertyName) {
				return fmt.Errorf("type %q: %s (property %q) has no reverse entry", rt.Slug(), ref.PredicateIRI, ref.PropertyName)
			}
		}
	}
	return nil
}

func (w *vocabWorld) noPredicateShared() error {
	types, err := w.installedTypes()
	if err != nil {
		return err
	}
	for _, rt := range types {
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(rt.Schema(), &schema) != nil {
			continue
		}
		vocab, forward := jsonld.ParseContext(rt.Context())
		byIRI := map[string]string{}
		for prop := range schema.Properties {
			iri := jsonld.ResolvePredicateIRI(prop, vocab, forward)
			if other, taken := byIRI[iri]; taken {
				return fmt.Errorf("type %q: properties %q and %q both resolve to %s", rt.Slug(), other, prop, iri)
			}
			byIRI[iri] = prop
		}
	}
	return nil
}

func (w *vocabWorld) heldForEveryMealPlanningType(term string) error {
	slugs, err := presetTypeSlugs("meal-planning")
	if err != nil {
		return err
	}
	checked := 0
	for _, slug := range slugs {
		terms, err := w.storedContextOf(slug)
		if err != nil {
			return err
		}
		if _, declares := terms[term]; !declares {
			continue
		}
		checked++
		if err := w.bootReportsContextTermHeld(term, slug); err != nil {
			return err
		}
	}
	if checked == 0 {
		return fmt.Errorf("no installed meal-planning type declares %q", term)
	}
	return nil
}

func (w *vocabWorld) noMealPlanningTypeResolves(prefix, ns string) error {
	slugs, err := presetTypeSlugs("meal-planning")
	if err != nil {
		return err
	}
	for _, slug := range slugs {
		terms, err := w.storedContextOf(slug)
		if err != nil {
			return err
		}
		if fmt.Sprintf("%v", terms[prefix]) == ns {
			return fmt.Errorf("installed type %q already resolves %q to %s", slug, prefix, ns)
		}
	}
	return nil
}

func (w *vocabWorld) storedContextMaps(slug, term, iri string) error {
	terms, err := w.storedContextOf(slug)
	if err != nil {
		return err
	}
	if got := fmt.Sprintf("%v", terms[term]); got != iri {
		return fmt.Errorf("the stored %q context maps %q to %v, want %s", slug, term, terms[term], iri)
	}
	return nil
}

func (w *vocabWorld) projectionTableHasColumn(slug, column string) error {
	table := strings.ReplaceAll(slug, "-", "_") + "s"
	if !w.db.Migrator().HasColumn(table, utils.CamelToSnake(column)) {
		return fmt.Errorf("the %q projection table has no %q column", table, column)
	}
	return nil
}

// --- creating real resources ---

// fixtureData fills a type's REQUIRED properties with fixture values, creating
// any prerequisite resource a required reference points at, so a scenario
// can name only the property it is about.
func (w *vocabWorld) fixtureData(slug string, given map[string]any) (map[string]any, error) {
	rt, err := w.rts.GetBySlug(context.Background(), slug)
	if err != nil {
		return nil, fmt.Errorf("failed to load the %q type: %w", slug, err)
	}
	var schema struct {
		Properties map[string]struct {
			Type          string `json:"type"`
			XResourceType string `json:"x-resource-type"`
			Enum          []any  `json:"enum"`
			Format        string `json:"format"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(rt.Schema(), &schema); err != nil {
		return nil, fmt.Errorf("the %q schema is not an object: %w", slug, err)
	}
	data := map[string]any{}
	for k, v := range given {
		data[k] = v
	}
	for _, req := range schema.Required {
		if _, has := data[req]; has {
			continue
		}
		prop := schema.Properties[req]
		switch {
		case prop.XResourceType != "":
			w.fixtureSeq++
			name := fmt.Sprintf("%s fixture %d", prop.XResourceType, w.fixtureSeq)
			if err := w.createResourceOf(prop.XResourceType, name, nil); err != nil {
				return nil, err
			}
			data[req] = w.createdIDs[prop.XResourceType+"/"+name]
		case len(prop.Enum) > 0:
			data[req] = prop.Enum[0]
		case prop.Type == "number" || prop.Type == "integer":
			data[req] = 1
		case prop.Type == "boolean":
			data[req] = false
		case prop.Type == "array":
			data[req] = []any{}
		case prop.Format == "date":
			data[req] = "2026-01-01"
		case prop.Format == "date-time":
			data[req] = "2026-01-01T00:00:00Z"
		default:
			data[req] = "fixture " + req
		}
	}
	return data, nil
}

func (w *vocabWorld) createResourceOf(slug, name string, given map[string]any) error {
	if given == nil {
		given = map[string]any{}
	}
	if name != "" {
		given["name"] = name
	}
	data, err := w.fixtureData(slug, given)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	res, err := w.rs.Create(context.Background(), application.CreateResourceCommand{TypeSlug: slug, Data: raw})
	if err != nil {
		return fmt.Errorf("failed to create %s %q: %w", slug, name, err)
	}
	key := slug + "/" + name
	if name == "" {
		key = slug + "/" + res.GetID()
	}
	w.createdIDs[key] = res.GetID()
	w.lastID, w.lastSlug = res.GetID(), slug
	return nil
}

func (w *vocabWorld) aResourceIsCreated(slug string) error {
	w.fixtureSeq++
	return w.createResourceOf(slug, fmt.Sprintf("%s %d", slug, w.fixtureSeq), nil)
}

func (w *vocabWorld) aNamedResourceExists(slug, name string) error {
	return w.createResourceOf(slug, name, nil)
}

func (w *vocabWorld) createWithReference(slug, name, property, targetSlug, target string) error {
	id, err := w.targetID(targetSlug, target)
	if err != nil {
		return err
	}
	return w.createResourceOf(slug, name, map[string]any{property: id})
}

// literalValue types a scenario's literal by the schema: a number property
// gets a number, everything else the string as written.
func (w *vocabWorld) literalValue(slug, property, value string) (any, error) {
	rt, err := w.rts.GetBySlug(context.Background(), slug)
	if err != nil {
		return nil, err
	}
	var schema struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	_ = json.Unmarshal(rt.Schema(), &schema)
	switch schema.Properties[property].Type {
	case "number":
		return strconv.ParseFloat(value, 64)
	case "integer":
		return strconv.Atoi(value)
	default:
		return value, nil
	}
}

func (w *vocabWorld) createWithLiteral(slug, name, property, value string) error {
	v, err := w.literalValue(slug, property, value)
	if err != nil {
		return err
	}
	return w.createResourceOf(slug, name, map[string]any{property: v})
}

func (w *vocabWorld) createWithReferences(slug, name string, table *godog.Table) error {
	if len(table.Rows) == 0 || len(table.Rows[0].Cells) != 3 {
		return fmt.Errorf("expected a header row of property | target slug | target")
	}
	given := map[string]any{}
	for _, row := range table.Rows[1:] {
		id, err := w.targetID(strings.TrimSpace(row.Cells[1].Value), strings.TrimSpace(row.Cells[2].Value))
		if err != nil {
			return err
		}
		given[strings.TrimSpace(row.Cells[0].Value)] = id
	}
	return w.createResourceOf(slug, name, given)
}

// --- what a resource says about itself ---

// document returns a resource's stored JSON-LD document and its embedded
// @context — what the knowledge graph store ingests verbatim.
func (w *vocabWorld) document(id string) (map[string]any, json.RawMessage, error) {
	res, err := w.rs.GetByID(context.Background(), id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read %s: %w", id, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(res.Data(), &doc); err != nil {
		return nil, nil, fmt.Errorf("the record of %s is not a JSON object: %w", id, err)
	}
	var embedded json.RawMessage
	if raw, has := doc["@context"]; has {
		embedded, _ = json.Marshal(raw)
	}
	return doc, embedded, nil
}

// classOf expands the entity node's @type through the document's own context
// — the class the graph store derives for the resource.
func classOf(doc map[string]any, embedded json.RawMessage) string {
	graph, _ := doc["@graph"].([]any)
	if len(graph) == 0 {
		return ""
	}
	entity, _ := graph[0].(map[string]any)
	typ, _ := entity["@type"].(string)
	vocab, _ := jsonld.ParseContext(embedded)
	var raw map[string]any
	_ = json.Unmarshal(embedded, &raw)
	if raw == nil {
		if s := strings.Trim(string(embedded), `"`); strings.HasPrefix(s, "http") {
			vocab = s
		}
	}
	return jsonld.ExpandIRI(typ, vocab, raw)
}

func (w *vocabWorld) lastResourceCarriesType(class string) error {
	doc, embedded, err := w.document(w.lastID)
	if err != nil {
		return err
	}
	if got := classOf(doc, embedded); got != class {
		return fmt.Errorf("the %s resource carries the RDF type %s, want %s", w.lastSlug, got, class)
	}
	return nil
}

func (w *vocabWorld) noResourceCarriesTypeUnder(ns string) error {
	for key, id := range w.createdIDs {
		doc, embedded, err := w.document(id)
		if err != nil {
			return err
		}
		if got := classOf(doc, embedded); strings.HasPrefix(got, ns) {
			return fmt.Errorf("%s carries the RDF type %s, under %s", key, got, ns)
		}
	}
	return nil
}

func (w *vocabWorld) triplesOf(slug, name string) ([]string, string, error) {
	id, err := w.targetID(slug, name)
	if err != nil {
		return nil, "", err
	}
	triples, err := w.tripleRepo.FindBySubject(context.Background(), id)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read the triples of %s %q: %w", slug, name, err)
	}
	rendered := make([]string, 0, len(triples))
	for _, t := range triples {
		rendered = append(rendered, t.Predicate+" -> "+t.Object)
	}
	sort.Strings(rendered)
	return rendered, id, nil
}

func (w *vocabWorld) tripleStoreHoldsEdge(predicate, slug, name, targetSlug, target string) error {
	object, err := w.targetID(targetSlug, target)
	if err != nil {
		return err
	}
	rendered, _, err := w.triplesOf(slug, name)
	if err != nil {
		return err
	}
	for _, line := range rendered {
		if line == predicate+" -> "+object {
			return nil
		}
	}
	return fmt.Errorf("the triple store holds no %s -> %s for %s %q (held: %v)", predicate, object, slug, name, rendered)
}

func (w *vocabWorld) tripleStoreHoldsNoEdgeUnder(ns, slug, name string) error {
	rendered, _, err := w.triplesOf(slug, name)
	if err != nil {
		return err
	}
	for _, line := range rendered {
		if strings.HasPrefix(line, ns) {
			return fmt.Errorf("the triple store holds %q for %s %q, under %s", line, slug, name, ns)
		}
	}
	return nil
}

// documentStatesLiteral checks the statement the graph store derives for a
// literal: the entity node holds the value, and the document's own context
// resolves the property to the predicate. Literals never reach the triples
// table, so the stored document is the honest surface.
func (w *vocabWorld) documentStatesLiteral(predicate, slug, name, value string) error {
	id, err := w.targetID(slug, name)
	if err != nil {
		return err
	}
	doc, embedded, err := w.document(id)
	if err != nil {
		return err
	}
	graph, _ := doc["@graph"].([]any)
	entity, _ := graph[0].(map[string]any)
	vocab, forward := jsonld.ParseContext(embedded)
	for prop, v := range entity {
		if strings.HasPrefix(prop, "@") || fmt.Sprintf("%v", v) != value {
			continue
		}
		if jsonld.ResolvePredicateIRI(prop, vocab, forward) == predicate {
			return nil
		}
	}
	return fmt.Errorf("the document of %s %q states no %s = %q (entity: %v, context: %s)",
		slug, name, predicate, value, entity, embedded)
}

func (w *vocabWorld) documentStatesNothingUnder(ns, slug, name string) error {
	id, err := w.targetID(slug, name)
	if err != nil {
		return err
	}
	doc, embedded, err := w.document(id)
	if err != nil {
		return err
	}
	if class := classOf(doc, embedded); strings.HasPrefix(class, ns) {
		return fmt.Errorf("%s %q carries the class %s, under %s", slug, name, class, ns)
	}
	graph, _ := doc["@graph"].([]any)
	vocab, forward := jsonld.ParseContext(embedded)
	for _, node := range graph {
		m, _ := node.(map[string]any)
		for prop := range m {
			if strings.HasPrefix(prop, "@") {
				continue
			}
			if iri := jsonld.ResolvePredicateIRI(prop, vocab, forward); strings.HasPrefix(iri, ns) {
				return fmt.Errorf("%s %q states %q under %s (%s)", slug, name, prop, ns, iri)
			}
		}
	}
	return nil
}

func (w *vocabWorld) flatReadOf(slug, name string) (map[string]any, error) {
	id, err := w.targetID(slug, name)
	if err != nil {
		return nil, err
	}
	row, err := w.rs.GetFlat(context.Background(), slug, id)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s %q back: %w", slug, name, err)
	}
	return row, nil
}

func (w *vocabWorld) projectionReturnsReference(slug, name, property, targetSlug, target string) error {
	want, err := w.targetID(targetSlug, target)
	if err != nil {
		return err
	}
	row, err := w.flatReadOf(slug, name)
	if err != nil {
		return err
	}
	if got := fmt.Sprintf("%v", row[property]); got != want {
		return fmt.Errorf("read of %s %q returned %s = %v, want %s (keys: %s)", slug, name, property, row[property],
			want, mapKeys(row))
	}
	return nil
}

func (w *vocabWorld) projectionReturnsLiteral(slug, name, property, want string) error {
	row, err := w.flatReadOf(slug, name)
	if err != nil {
		return err
	}
	if got := fmt.Sprintf("%v", row[property]); got != want {
		return fmt.Errorf("read of %s %q returned %s = %v, want %q", slug, name, property, row[property], want)
	}
	return nil
}

func (w *vocabWorld) apiReturnsReference(slug, name, property, targetSlug, target string) error {
	want, err := w.targetID(targetSlug, target)
	if err != nil {
		return err
	}
	id, err := w.targetID(slug, name)
	if err != nil {
		return err
	}
	res, err := w.rs.GetByID(context.Background(), id)
	if err != nil {
		return err
	}
	rt, err := w.rts.GetBySlug(context.Background(), slug)
	if err != nil {
		return err
	}
	simplified, err := entities.SimplifyJSONLD(res.Data(), rt.Context())
	if err != nil {
		return fmt.Errorf("the API read of %s %q could not simplify the record: %w", slug, name, err)
	}
	var body map[string]any
	if err := json.Unmarshal(simplified, &body); err != nil {
		return err
	}
	if got := fmt.Sprintf("%v", body[property]); got != want {
		return fmt.Errorf("the API read of %s %q returned %s = %v, want %s", slug, name, property, body[property], want)
	}
	return nil
}

func (w *vocabWorld) documentCarriesEdge(slug, name, property, targetSlug, target string) error {
	want, err := w.targetID(targetSlug, target)
	if err != nil {
		return err
	}
	id, err := w.targetID(slug, name)
	if err != nil {
		return err
	}
	doc, embedded, err := w.document(id)
	if err != nil {
		return err
	}
	edges, _ := doc["@graph"].([]any)
	if len(edges) < 2 {
		return fmt.Errorf("the document of %s %q has no edges node", slug, name)
	}
	node, _ := edges[1].(map[string]any)
	rt, err := w.rts.GetBySlug(context.Background(), slug)
	if err != nil {
		return err
	}
	for key, val := range node {
		if key == "@id" {
			continue
		}
		prop, ok := jsonld.EdgeProperty(key, rt.Context())
		if !ok {
			prop, _ = jsonld.EdgeProperty(key, embedded)
		}
		if prop != property {
			continue
		}
		ids, _ := jsonld.EdgeIDs(val)
		for _, got := range ids {
			if got == want {
				return nil
			}
		}
	}
	return fmt.Errorf("the document of %s %q carries no %q edge to %s (edges: %v)", slug, name, property, want, node)
}

func (w *vocabWorld) noResourceOfTypeCarriesTypeUnder(slug, ns string) error {
	for key, id := range w.createdIDs {
		if !strings.HasPrefix(key, slug+"/") {
			continue
		}
		doc, embedded, err := w.document(id)
		if err != nil {
			return err
		}
		if got := classOf(doc, embedded); strings.HasPrefix(got, ns) {
			return fmt.Errorf("%s carries the RDF type %s, under %s", key, got, ns)
		}
	}
	return nil
}

func (w *vocabWorld) onlyTheseFourEventTypesRewritten(a, b, c, d string) error {
	before, after, err := w.beforeAndAfter()
	if err != nil {
		return err
	}
	allowed := map[string]bool{a: true, b: true, c: true, d: true}
	for i := range before {
		if i >= len(after) || string(before[i].Payload) == string(after[i].Payload) {
			continue
		}
		if t := after[i].EventType; !allowed[t] {
			return fmt.Errorf("event %s of type %s was re-stamped; only %s, %s, %s and %s may be", after[i].ID, t, a, b, c, d)
		}
	}
	return nil
}
