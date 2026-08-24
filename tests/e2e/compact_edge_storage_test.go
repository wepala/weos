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

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/pkg/jsonld"
	"github.com/wepala/weos/v3/pkg/utils"
)

// TestCompactEdgeStorage is the acceptance suite for issue #515: a resource's
// edges node moves from EXPANDED JSON-LD (keyed by predicate IRI) to COMPACT
// (keyed by property name), with the full `@context` retained.
//
// The scenarios assert three things that have to hold together: new writes are
// compact, documents already stored expanded keep reading with no migration,
// and the graph is unchanged — the same predicates and objects reach the triple
// store, and a stored compact document expands to the graph the expanded form
// stored.
func TestCompactEdgeStorage(t *testing.T) {
	runContextFeature(t, "compact-edge-storage", "features/compact_edge_storage.feature")
}

// TestReferenceShapeAmbiguity is the acceptance suite for the one shape compact
// storage does NOT rescue: two reference properties on a type sharing both the
// predicate IRI and the target type slug, which no reader can tell apart.
func TestReferenceShapeAmbiguity(t *testing.T) {
	runContextFeature(t, "reference-shape-ambiguity", "features/reference_shape_ambiguity.feature")
}

// registerCompactEdgeSteps adds issue #515's steps to the shared context world.
// Everything about booting, shaping the preset and creating resources is reused
// from preset_context_reconcile_test.go; what is new here is the ability to see
// the SHAPE a record is stored in, to plant a record in the old shape, and to
// read the same record back through the API path and the triple store.
func (w *contextWorld) registerCompactEdgeSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the twin starts against a clean database$`, w.theTwinStartsAgainstACleanDatabase)

	sc.Step(`^a "widget" named "([^"]*)" stored in the old expanded edges form with "([^"]*)" `+
		`referring to the vendors "([^"]*)"$`, w.aWidgetStoredExpanded)

	sc.Step(`^the stored canonical record for the "widget" "([^"]*)" keys its "([^"]*)" edge by the property name$`,
		w.recordKeysEdgeByPropertyName)
	sc.Step(`^the stored canonical record for the "widget" "([^"]*)" keys its "([^"]*)" edge by a predicate IRI$`,
		w.recordKeysEdgeByPredicateIRI)
	sc.Step(`^the stored canonical record for the "widget" "([^"]*)" keys its "([^"]*)" edge by "([^"]*)"$`,
		w.recordKeysEdgeBy)
	sc.Step(`^the stored canonical record for the "widget" "([^"]*)" keys no edge by an absolute IRI$`,
		w.recordKeysNoEdgeByAbsoluteIRI)
	sc.Step(`^the stored canonical record for the "widget" "([^"]*)" maps "([^"]*)" to "([^"]*)" in its own context$`,
		w.recordContextMaps)
	sc.Step(`^expanding the stored record for the "widget" "([^"]*)" through its own context yields the edges `+
		`the stored record for the "widget" "([^"]*)" already holds$`, w.expandingYieldsTheSameEdges)

	sc.Step(`^the API read of the "widget" "([^"]*)" returns "([^"]*)" as the "vendor" "([^"]*)"$`,
		w.apiReadReturnsReference)
	sc.Step(`^the API read of the "widget" "([^"]*)" returns "([^"]*)" as the vendors "([^"]*)"$`,
		w.apiReadReturnsReferenceList)

	sc.Step(`^I create a "widget" named "([^"]*)" with these targets:$`, w.iCreateWidgetWithTargets)
	sc.Step(`^reading the "widget" "([^"]*)" back through the projection returns "([^"]*)" as the "widget" "([^"]*)"$`,
		w.readingReturnsWidgetReference)
	sc.Step(`^the JSON-LD representation of the "widget" "([^"]*)" still carries a "([^"]*)" edge `+
		`to the "widget" "([^"]*)"$`, w.canonicalStillCarriesWidgetEdge)

	sc.Step(`^the triple store holds "([^"]*)" from the "widget" "([^"]*)" to the "vendor" "([^"]*)"$`,
		w.tripleStoreHolds)
	sc.Step(`^the triple store holds no "([^"]*)" edge from the "widget" "([^"]*)"$`, w.tripleStoreHoldsNo)

	sc.Step(`^the boot refuses "([^"]*)", naming "([^"]*)" and "([^"]*)" on "([^"]*)"$`, w.bootRefusesShape)
	sc.Step(`^the boot refuses nothing$`, w.bootRefusesNothing)
	sc.Step(`^the refusal tells the operator to (.+)$`, w.refusalNamesRemedy)
}

// --- booting tolerantly ---

// theTwinStartsAgainstACleanDatabase is aCleanDatabase's tolerant twin: a boot
// that REFUSES a preset shape may or may not fail startup, and which it is
// belongs to the implementer, not to this contract. The error is recorded on
// the world instead of failing the step, and the refusal assertions accept a
// startup error and a reported line alike.
func (w *contextWorld) theTwinStartsAgainstACleanDatabase() error {
	dir, err := os.MkdirTemp("", "weos-compact-edges-e2e-")
	if err != nil {
		return err
	}
	w.tmpDir = dir
	w.dsn = filepath.Join(dir, "test.db")
	_ = w.boot() // recorded on w.bootErr; the refusal steps read it.
	return nil
}

// --- planting a record in the pre-#515 shape ---

// aWidgetStoredExpanded plants a widget whose canonical record is in the shape
// the pre-#515 binary wrote: an edges node keyed by the property's PREDICATE
// IRI, under a stored `@context` with the property→predicate mappings stripped.
//
// The bytes are built here rather than obtained from BuildResourceGraph on
// purpose. After the change that function emits the compact form, so a fixture
// derived from it would silently stop being a legacy fixture, and every
// compatibility scenario would pass while proving nothing. The two rules the
// old writer used — the predicate IRI from the type's context, and
// buildStorableContext's "keep @vocab and prefix strings, drop the rest" — are
// reproduced below so the fixture stays pinned to the old shape forever.
//
// The widget is created through the ordinary write path FIRST, so its triples
// and its event are the real ones an upgraded instance would already hold, and
// only the stored canonical bytes are then downgraded. Its projection column is
// blanked before the downgrade, so the value a scenario reads back can only have
// come from the legacy document — otherwise a reader that had stopped
// understanding the old shape would still pass on a leftover column.
//
// UpdateData is the rewrite rail because it re-runs the projection extraction
// over the document it is given, so all three read paths (projection, API read,
// canonical EdgeValues) see the legacy shape, not just the canonical one.
func (w *contextWorld) aWidgetStoredExpanded(name, property, vendorNames string) error {
	ids, err := w.vendorIDs(vendorNames)
	if err != nil {
		return err
	}
	written, err := w.referenceValue(property, vendorNames)
	if err != nil {
		return err
	}
	if err := w.createResource("widget", name, map[string]any{property: written}); err != nil {
		return err
	}
	id := w.createdIDs["widget/"+name]
	if err := w.db.Table("widgets").Where("id = ?", id).
		Update(utils.CamelToSnake(property), nil).Error; err != nil {
		return fmt.Errorf("failed to blank the %q column before planting the legacy record: %w", property, err)
	}

	rt, err := w.rts.GetBySlug(context.Background(), "widget")
	if err != nil {
		return fmt.Errorf("failed to load the widget type: %w", err)
	}
	vocab, contextMap := jsonld.ParseContext(rt.Context())
	predicate := jsonld.ResolvePredicateIRI(property, vocab, contextMap)

	var edgeValue any
	if w.widgetPropertyIsList(property) {
		refs := make([]any, 0, len(ids))
		for _, target := range ids {
			refs = append(refs, map[string]any{"@id": target})
		}
		edgeValue = refs
	} else {
		if len(ids) != 1 {
			return fmt.Errorf("%q is a single reference but the scenario names %d vendors", property, len(ids))
		}
		edgeValue = map[string]any{"@id": ids[0]}
	}

	doc := map[string]any{
		"@graph": []any{
			map[string]any{"@id": id, "@type": legacySchemaType(rt.Name(), rt.Context()), "name": name},
			map[string]any{"@id": id, predicate: edgeValue},
		},
	}
	if ctx := legacyStorableContext(rt.Context()); ctx != nil {
		doc["@context"] = ctx
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if err := w.resRepo.UpdateData(context.Background(), id, raw, 0); err != nil {
		return fmt.Errorf("failed to plant the legacy record for %q: %w", name, err)
	}
	return nil
}

// legacySchemaType reproduces the old writer's rule for the entity node's
// @type: the context's "@type" when it names one, otherwise the type's name.
func legacySchemaType(typeName string, ldContext json.RawMessage) string {
	var raw map[string]any
	if json.Unmarshal(ldContext, &raw) == nil {
		if ct, ok := raw["@type"].(string); ok && ct != "" {
			return ct
		}
	}
	return typeName
}

// legacyStorableContext reproduces buildStorableContext as it stands before
// issue #515: keep JSON-LD keywords other than @type and namespace-prefix
// strings, drop every property→predicate mapping, and collapse a lone @vocab to
// the bare IRI string. Stripping those mappings is what forced the read path to
// invert the context in the first place.
func legacyStorableContext(ldContext json.RawMessage) any {
	var ctx map[string]any
	if json.Unmarshal(ldContext, &ctx) != nil {
		return nil
	}
	clean := map[string]any{}
	for key, val := range ctx {
		if strings.HasPrefix(key, "@") && key != "@type" {
			clean[key] = val
			continue
		}
		if s, ok := val.(string); ok && (strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")) {
			clean[key] = val
		}
	}
	if len(clean) == 1 {
		if v, ok := clean["@vocab"]; ok {
			return v
		}
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}

// --- reading the stored SHAPE ---

// storedRecord returns a widget's canonical document, its edges node and its
// own embedded `@context`. The edges node is the second @graph member, which is
// where both shapes live; a record with no edges node is an error rather than
// an empty map, because every scenario using these steps wrote a reference.
func (w *contextWorld) storedRecord(name string) (map[string]any, map[string]any, json.RawMessage, error) {
	id, ok := w.createdIDs["widget/"+name]
	if !ok {
		return nil, nil, nil, fmt.Errorf("no widget named %q was created in this scenario", name)
	}
	res, err := w.rs.GetByID(context.Background(), id)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to read widget %q by id: %w", name, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(res.Data(), &doc); err != nil {
		return nil, nil, nil, fmt.Errorf("canonical record for %q is not a JSON object: %w", name, err)
	}
	graph, ok := doc["@graph"].([]any)
	if !ok || len(graph) < 2 {
		return nil, nil, nil, fmt.Errorf(
			"canonical record for %q carries no edges node, so the reference was never stored: %s", name, res.Data())
	}
	edges, ok := graph[1].(map[string]any)
	if !ok {
		return nil, nil, nil, fmt.Errorf("edges node of %q is not a JSON object: %s", name, res.Data())
	}
	var embedded json.RawMessage
	if raw, has := doc["@context"]; has {
		encoded, mErr := json.Marshal(raw)
		if mErr != nil {
			return nil, nil, nil, fmt.Errorf("the @context of %q could not be re-encoded: %w", name, mErr)
		}
		embedded = encoded
	}
	return doc, edges, embedded, nil
}

func edgeKeys(edges map[string]any) string {
	keys := make([]string, 0, len(edges))
	for k := range edges {
		if k == "@id" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

func (w *contextWorld) recordKeysEdgeByPropertyName(name, property string) error {
	_, edges, _, err := w.storedRecord(name)
	if err != nil {
		return err
	}
	if _, ok := edges[property]; !ok {
		return fmt.Errorf(
			"the stored record for %q keys no edge by the property name %q — it is still stored expanded, "+
				"so every read has to invert the context to find it (edge keys: %s)",
			name, property, edgeKeys(edges))
	}
	return nil
}

// recordKeysEdgeByPredicateIRI is the legacy-shape assertion. It is deliberately
// exact rather than "some absolute IRI": a fixture that drifted onto a different
// predicate would still look expanded while no longer standing for the data a
// real upgrade finds in the database.
func (w *contextWorld) recordKeysEdgeByPredicateIRI(name, property string) error {
	rt, err := w.rts.GetBySlug(context.Background(), "widget")
	if err != nil {
		return fmt.Errorf("failed to load the widget type: %w", err)
	}
	vocab, contextMap := jsonld.ParseContext(rt.Context())
	return w.recordKeysEdgeBy(name, property, jsonld.ResolvePredicateIRI(property, vocab, contextMap))
}

func (w *contextWorld) recordKeysEdgeBy(name, property, key string) error {
	_, edges, _, err := w.storedRecord(name)
	if err != nil {
		return err
	}
	if _, ok := edges[key]; !ok {
		return fmt.Errorf("the stored record for %q keys no %q edge by %q (edge keys: %s)",
			name, property, key, edgeKeys(edges))
	}
	return nil
}

func (w *contextWorld) recordKeysNoEdgeByAbsoluteIRI(name string) error {
	_, edges, _, err := w.storedRecord(name)
	if err != nil {
		return err
	}
	for key := range edges {
		if key == "@id" {
			continue
		}
		if strings.Contains(key, "://") {
			return fmt.Errorf(
				"the stored record for %q still keys an edge by the absolute IRI %q, so a reader must "+
					"invert the context to name it", name, key)
		}
	}
	return nil
}

// recordContextMaps asserts the stored document keeps the property→predicate
// mapping the compact form depends on. Without it the document no longer says
// what its own keys mean, and expanding it falls back to @vocab — which is
// right by accident for a vocab-consistent term and wrong for every other one.
func (w *contextWorld) recordContextMaps(name, term, iri string) error {
	_, _, embedded, err := w.storedRecord(name)
	if err != nil {
		return err
	}
	if len(embedded) == 0 {
		return fmt.Errorf("the stored record for %q carries no @context at all", name)
	}
	_, contextMap := jsonld.ParseContext(embedded)
	got, ok := contextMap[term]
	if !ok {
		return fmt.Errorf(
			"the stored @context of %q declares no mapping for %q, so the document does not say what its "+
				"own edge keys mean (stored @context: %s)", name, term, embedded)
	}
	if got != iri {
		return fmt.Errorf("the stored @context of %q maps %q to %q, want %q", name, term, got, iri)
	}
	return nil
}

// expandEdges renders an edges node as the graph it denotes: predicate IRI →
// sorted object IDs. A key that is already an absolute IRI is taken as the
// predicate; anything else is a term resolved through the document's OWN
// context. That is the same rule that lets the two stored shapes coexist.
func expandEdges(edges map[string]any, embedded json.RawMessage) map[string][]string {
	vocab, contextMap := jsonld.ParseContext(embedded)
	out := map[string][]string{}
	for key, val := range edges {
		if key == "@id" {
			continue
		}
		iri := key
		if !strings.Contains(key, "://") {
			iri = jsonld.ResolvePredicateIRI(key, vocab, contextMap)
		}
		ids, _ := jsonld.EdgeIDs(val)
		out[iri] = append(out[iri], ids...)
	}
	for iri := range out {
		sort.Strings(out[iri])
	}
	return out
}

func (w *contextWorld) expandingYieldsTheSameEdges(compactName, expandedName string) error {
	_, compactEdges, compactContext, err := w.storedRecord(compactName)
	if err != nil {
		return err
	}
	_, expandedEdges, expandedContext, err := w.storedRecord(expandedName)
	if err != nil {
		return err
	}
	got := expandEdges(compactEdges, compactContext)
	want := expandEdges(expandedEdges, expandedContext)
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		return fmt.Errorf(
			"the stored record for %q expands to %v but the record for %q holds %v — the two serializations "+
				"are not the same graph (compact @context: %s)",
			compactName, got, expandedName, want, compactContext)
	}
	return nil
}

// --- the API read path ---

// apiRead runs the record through entities.SimplifyJSONLD with the type's
// context, which is exactly what the resource handler serves to a JSON client.
func (w *contextWorld) apiRead(name string) (map[string]any, error) {
	id, ok := w.createdIDs["widget/"+name]
	if !ok {
		return nil, fmt.Errorf("no widget named %q was created in this scenario", name)
	}
	res, err := w.rs.GetByID(context.Background(), id)
	if err != nil {
		return nil, fmt.Errorf("failed to read widget %q by id: %w", name, err)
	}
	rt, err := w.rts.GetBySlug(context.Background(), "widget")
	if err != nil {
		return nil, fmt.Errorf("failed to load the widget type: %w", err)
	}
	simplified, err := entities.SimplifyJSONLD(res.Data(), rt.Context())
	if err != nil {
		return nil, fmt.Errorf("the API read of %q could not simplify the record: %w", name, err)
	}
	var body map[string]any
	if err := json.Unmarshal(simplified, &body); err != nil {
		return nil, fmt.Errorf("the API read of %q is not a JSON object: %w", name, err)
	}
	return body, nil
}

func (w *contextWorld) apiReadReturnsReference(name, property, vendorName string) error {
	want, err := w.vendorID(vendorName)
	if err != nil {
		return err
	}
	body, err := w.apiRead(name)
	if err != nil {
		return err
	}
	got, ok := body[property]
	if !ok {
		return fmt.Errorf("the API read of %q carries no %q key — the reference was dropped (keys: %s)",
			name, property, mapKeys(body))
	}
	if fmt.Sprintf("%v", got) != want {
		return fmt.Errorf("the API read of %q returned %s = %v, want the %q vendor %s",
			name, property, got, vendorName, want)
	}
	return nil
}

func (w *contextWorld) apiReadReturnsReferenceList(name, property, vendorNames string) error {
	want, err := w.vendorIDs(vendorNames)
	if err != nil {
		return err
	}
	body, err := w.apiRead(name)
	if err != nil {
		return err
	}
	got, ok := body[property]
	if !ok || got == nil {
		return fmt.Errorf("the API read of %q carries no %q value — the array reference was dropped (keys: %s)",
			name, property, mapKeys(body))
	}
	ids, err := asIDList(got)
	if err != nil {
		return fmt.Errorf("the API read of %q returned %s = %v, which is not a list of references: %w",
			name, property, got, err)
	}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		return fmt.Errorf("the API read of %q returned %s = %v, want the vendors %q (%v)",
			name, property, ids, vendorNames, want)
	}
	return nil
}

// --- references whose target is another widget ---

// iCreateWidgetWithTargets is the reference table generalized over the target
// TYPE. Two properties can only share a predicate legally when they target
// different types, so a table that can only name vendors cannot reach the shape
// issue #515 makes safe.
func (w *contextWorld) iCreateWidgetWithTargets(name string, table *godog.Table) error {
	if len(table.Rows) == 0 {
		return fmt.Errorf("target table has no header row")
	}
	data := map[string]any{}
	for _, row := range table.Rows[1:] {
		if len(row.Cells) != 3 {
			return fmt.Errorf("expected 3 columns per target row, got %d", len(row.Cells))
		}
		property := strings.TrimSpace(row.Cells[0].Value)
		targetType := strings.TrimSpace(row.Cells[1].Value)
		var ids []string
		for _, targetName := range strings.Split(row.Cells[2].Value, ",") {
			id, ok := w.createdIDs[targetType+"/"+strings.TrimSpace(targetName)]
			if !ok {
				return fmt.Errorf("no %s named %q was created in this scenario", targetType, targetName)
			}
			ids = append(ids, id)
		}
		if w.widgetPropertyIsList(property) {
			data[property] = ids
			continue
		}
		if len(ids) != 1 {
			return fmt.Errorf("%q is a single reference but the row names %d targets", property, len(ids))
		}
		data[property] = ids[0]
	}
	return w.createResource("widget", name, data)
}

func (w *contextWorld) widgetTargetID(name string) (string, error) {
	id, ok := w.createdIDs["widget/"+name]
	if !ok {
		return "", fmt.Errorf("no widget named %q was created in this scenario", name)
	}
	return id, nil
}

func (w *contextWorld) readingReturnsWidgetReference(name, property, targetName string) error {
	want, err := w.widgetTargetID(targetName)
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
		return fmt.Errorf("read of %q returned %s = %v, want the %q widget %s", name, property, got, targetName, want)
	}
	return nil
}

func (w *contextWorld) canonicalStillCarriesWidgetEdge(name, property, targetName string) error {
	want, err := w.widgetTargetID(targetName)
	if err != nil {
		return err
	}
	id, err := w.widgetTargetID(name)
	if err != nil {
		return err
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
	if strings.Join(got, ",") != want {
		return fmt.Errorf(
			"canonical record for %q resolves %q to %v, want the widget %q (%s) — payload: %s",
			name, property, got, targetName, want, res.Data())
	}
	return nil
}

// --- the triple store ---

// tripleStoreHolds proves the ontology did not move. ExtractReferenceTriples
// runs on the ORIGINAL input data and is untouched by issue #515, so these are
// regression guards: they pass before the change and must keep passing after
// it. They are here because the change edits the same file, and routing triples
// through the new edges node would silently repoint every predicate.
func (w *contextWorld) subjectTriples(name string) ([]string, string, error) {
	id, err := w.widgetTargetID(name)
	if err != nil {
		return nil, "", err
	}
	if w.tripleRepo == nil {
		return nil, "", fmt.Errorf("no triple repository is available in this scenario")
	}
	triples, err := w.tripleRepo.FindBySubject(context.Background(), id)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read the triples of %q: %w", name, err)
	}
	rendered := make([]string, 0, len(triples))
	for _, t := range triples {
		rendered = append(rendered, t.Predicate+" -> "+t.Object)
	}
	sort.Strings(rendered)
	return rendered, id, nil
}

func (w *contextWorld) tripleStoreHolds(predicate, name, vendorName string) error {
	object, err := w.vendorID(vendorName)
	if err != nil {
		return err
	}
	rendered, _, err := w.subjectTriples(name)
	if err != nil {
		return err
	}
	for _, line := range rendered {
		if line == predicate+" -> "+object {
			return nil
		}
	}
	return fmt.Errorf(
		"the triple store holds no %s -> %s for widget %q, so the predicate this reference contributes "+
			"to the graph moved (held: %v)", predicate, object, name, rendered)
}

func (w *contextWorld) tripleStoreHoldsNo(predicate, name string) error {
	rendered, _, err := w.subjectTriples(name)
	if err != nil {
		return err
	}
	for _, line := range rendered {
		if strings.HasPrefix(line, predicate+" -> ") {
			return fmt.Errorf("the triple store holds %q for widget %q, so the predicate moved (held: %v)",
				line, name, rendered)
		}
	}
	return nil
}

// --- the ambiguous-shape refusal ---

// ambiguityRefusalMarker is the one token this contract fixes about the refusal's
// wording, so the assertions can find it without dictating which channel
// reports it — a startup error and an operator-facing log line both count.
const ambiguityRefusalMarker = "ambiguous reference shape"

// refusalText gathers every place a refusal could surface for this boot.
func (w *contextWorld) refusalText() (string, error) {
	r, err := w.report()
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	lines := append([]string{}, r.lines...)
	r.mu.Unlock()

	var found []string
	if w.bootErr != nil && strings.Contains(w.bootErr.Error(), ambiguityRefusalMarker) {
		found = append(found, w.bootErr.Error())
	}
	for _, line := range lines {
		if strings.Contains(line, ambiguityRefusalMarker) {
			found = append(found, line)
		}
	}
	return strings.Join(found, "\n"), nil
}

func (w *contextWorld) bootRefusesShape(slug, first, second, predicate string) error {
	text, err := w.refusalText()
	if err != nil {
		return err
	}
	if text == "" {
		reported := "the boot came up clean"
		if w.bootErr != nil {
			reported = "the boot failed for an unrelated reason: " + w.bootErr.Error()
		}
		return fmt.Errorf(
			"nothing the boot reported mentions %q, so two reference properties sharing a predicate AND a "+
				"target type were accepted — no reader can tell %q from %q on %q (%s)",
			ambiguityRefusalMarker, first, second, predicate, reported)
	}
	for _, want := range []string{slug, first, second, predicate} {
		if !strings.Contains(text, want) {
			return fmt.Errorf("the refusal never names %q, so the operator cannot find the shape to fix:\n%s",
				want, text)
		}
	}
	return nil
}

func (w *contextWorld) bootRefusesNothing() error {
	if w.bootErr != nil {
		return fmt.Errorf("the boot did not come up: %w", w.bootErr)
	}
	text, err := w.refusalText()
	if err != nil {
		return err
	}
	if text != "" {
		return fmt.Errorf("the boot refused a shape that is well formed, so the guard over-reaches:\n%s", text)
	}
	return nil
}

// refusalNamesRemedy checks the refusal names a way out. The two remedies are
// the whole point of the message: a refusal that only says "no" leaves the
// operator to guess which of the two fixes their case needs.
func (w *contextWorld) refusalNamesRemedy(remedy string) error {
	text, err := w.refusalText()
	if err != nil {
		return err
	}
	want := map[string]string{
		"give the relationships different predicates": "different predicates",
		"collapse them into a single array property":  "a single array property",
	}[strings.TrimSpace(remedy)]
	if want == "" {
		return fmt.Errorf("this suite declares no remedy phrasing for %q", remedy)
	}
	if !strings.Contains(text, want) {
		return fmt.Errorf("the refusal never says %q, so it names the problem without naming the fix:\n%s",
			want, text)
	}
	return nil
}
