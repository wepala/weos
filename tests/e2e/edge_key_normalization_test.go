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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"github.com/jinzhu/inflection"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/pkg/jsonld"
	"github.com/wepala/weos/v3/pkg/utils"

	pericarpinfra "github.com/akeemphilbert/pericarp/pkg/eventsourcing/infrastructure"
	"go.uber.org/fx"
)

// Issue #523: one migration rewrites stored events so every edge is keyed by
// its property name. These steps plant events in the PRE-#515 shape — the
// canonical record alone is not enough, because a reprojection reproduces
// whatever the event holds — run the normalization, and read the event store
// back.

func TestEdgeKeyNormalization(t *testing.T) {
	runContextFeature(t, "edge-key-normalization", "features/edge_key_normalization.feature")
}

func (w *contextWorld) registerEdgeKeyNormalizationSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a "(widget|vendor)" named "([^"]*)" written by the pre-#515 binary with "([^"]*)" `+
		`referring to the "(vendor|widget)" "([^"]*)"$`, w.legacyWrittenWithReference)
	sc.Step(`^a "widget" named "([^"]*)" written by the pre-#515 binary with "([^"]*)" `+
		`referring to the vendors "([^"]*)"$`, w.legacyWrittenWithReferenceList)
	sc.Step(`^a "widget" named "([^"]*)" written by the pre-#515 binary with these references:$`,
		w.legacyWrittenWithReferences)
	sc.Step(`^a "widget" named "([^"]*)" written by the pre-#515 binary with "([^"]*)" `+
		`referring to the "vendor" "([^"]*)" and "([^"]*)" set to "([^"]*)"$`, w.legacyWrittenWithReferenceAndLiteral)
	sc.Step(`^the "widget" "([^"]*)" was updated by the pre-#515 binary with "([^"]*)" `+
		`referring to the "vendor" "([^"]*)"$`, w.legacyUpdatedWithReference)
	sc.Step(`^the "catalog" preset adds a "([^"]*)" reference property to "vendor" targeting "([^"]*)"$`,
		w.thePresetAddsVendorReference)

	sc.Step(`^the stored event for the "(widget|vendor)" "([^"]*)" keys its "([^"]*)" edge by "([^"]*)"$`,
		w.storedEventKeysEdgeBy)
	sc.Step(`^the stored event for the "(widget|vendor)" "([^"]*)" keys its "([^"]*)" edge by the property name$`,
		w.storedEventKeysEdgeByPropertyName)
	sc.Step(`^the stored event for the "(widget|vendor)" "([^"]*)" keys no edge by an absolute IRI$`,
		w.storedEventKeysNoEdgeByIRI)
	sc.Step(`^every stored event for the "(widget|vendor)" "([^"]*)" keys no edge by an absolute IRI$`,
		w.everyStoredEventKeysNoEdgeByIRI)
	sc.Step(`^the stored event for the "(widget|vendor)" "([^"]*)" maps "([^"]*)" to "([^"]*)" in its own context$`,
		w.storedEventContextMaps)
	sc.Step(`^the entity node of the stored event for the "(widget|vendor)" "([^"]*)" is byte-identical `+
		`to the one stored before the run$`, w.storedEventEntityNodeUnchanged)

	sc.Step(`^the operator normalizes the stored edge keys as a dry run$`, w.theOperatorNormalizesDryRun)
	sc.Step(`^the operator normalizes the stored edge keys and writes$`, w.theOperatorNormalizesAndWrites)

	sc.Step(`^the normalization rewrote (\d+) events? for "([^"]*)"$`, w.normalizationRewroteForType)
	sc.Step(`^the normalization rewrote 0 events$`, w.normalizationRewroteNothing)
	sc.Step(`^the normalization reports (\d+) events? to rewrite for "([^"]*)"$`, w.normalizationReportsForType)
	sc.Step(`^the normalization reports itself as a dry run$`, w.normalizationReportsDryRun)
	sc.Step(`^the normalization reports nothing to rewrite$`, w.normalizationReportsNothing)
	sc.Step(`^the normalization reports no unresolved edge key$`, w.normalizationReportsNoUnresolved)
	sc.Step(`^the normalization reports the "(widget|vendor)" "([^"]*)" as ambiguous on "([^"]*)", `+
		`naming "([^"]*)" and "([^"]*)"$`, w.normalizationReportsAmbiguous)
	sc.Step(`^the normalization reports the "(widget|vendor)" "([^"]*)" as unresolved on "([^"]*)"$`,
		w.normalizationReportsUnresolved)

	sc.Step(`^the stored events are byte-identical to the ones stored before the (?:second )?run$`,
		w.storedEventsUnchanged)
	sc.Step(`^the event feed holds the same events in the same order as before the run$`, w.eventFeedSameOrder)
	sc.Step(`^every event keeps its aggregate id, sequence number and event type$`, w.eventsKeepIdentity)
	sc.Step(`^no event of a type other than "([^"]*)" or "([^"]*)" was rewritten$`, w.onlyTheseEventTypesRewritten)

	sc.Step(`^the operator removes every historical IRI from the stored "widget" context$`,
		w.theOperatorRemovesAliases)
	sc.Step(`^the read of every resource with a reference property is captured$`, w.captureEveryRead)
	sc.Step(`^the read of every resource with a reference property matches what was captured$`, w.everyReadMatches)
}

// --- planting pre-#515 events ---

func (w *contextWorld) thePresetAddsVendorReference(name, target string) error {
	for _, existing := range w.vendorProps {
		if existing.name == name {
			return fmt.Errorf("property %q is already declared on vendor", name)
		}
	}
	w.vendorProps = append(w.vendorProps, contextProperty{name: name, jsonTyp: "string", references: target})
	return nil
}

func (w *contextWorld) targetID(slug, name string) (string, error) {
	id, ok := w.createdIDs[slug+"/"+name]
	if !ok {
		return "", fmt.Errorf("no %s named %q was created in this scenario", slug, name)
	}
	return id, nil
}

func (w *contextWorld) propertyIsList(slug, property string) bool {
	props := w.widgetProps
	if slug == "vendor" {
		props = w.vendorProps
	}
	for _, p := range props {
		if p.name == property {
			return p.list
		}
	}
	return false
}

func (w *contextWorld) legacyWrittenWithReference(slug, name, property, targetSlug, targetName string) error {
	id, err := w.targetID(targetSlug, targetName)
	if err != nil {
		return err
	}
	return w.plantLegacy(slug, name, map[string][]string{property: {id}}, nil, false)
}

func (w *contextWorld) legacyWrittenWithReferenceList(name, property, vendorNames string) error {
	ids, err := w.vendorIDs(vendorNames)
	if err != nil {
		return err
	}
	return w.plantLegacy("widget", name, map[string][]string{property: ids}, nil, false)
}

func (w *contextWorld) legacyWrittenWithReferences(name string, table *godog.Table) error {
	if len(table.Rows) == 0 {
		return fmt.Errorf("reference table has no header row")
	}
	refs := map[string][]string{}
	for _, row := range table.Rows[1:] {
		if len(row.Cells) != 2 {
			return fmt.Errorf("expected 2 columns per reference row, got %d", len(row.Cells))
		}
		ids, err := w.vendorIDs(row.Cells[1].Value)
		if err != nil {
			return err
		}
		refs[strings.TrimSpace(row.Cells[0].Value)] = ids
	}
	return w.plantLegacy("widget", name, refs, nil, false)
}

func (w *contextWorld) legacyWrittenWithReferenceAndLiteral(name, property, vendorName, literal, value string) error {
	id, err := w.targetID("vendor", vendorName)
	if err != nil {
		return err
	}
	return w.plantLegacy("widget", name, map[string][]string{property: {id}}, map[string]any{literal: value}, false)
}

func (w *contextWorld) legacyUpdatedWithReference(name, property, vendorName string) error {
	id, err := w.targetID("vendor", vendorName)
	if err != nil {
		return err
	}
	return w.plantLegacy("widget", name, map[string][]string{property: {id}}, nil, true)
}

// plantLegacy writes a resource through the real write path — so its
// aggregate, transaction, Triple.Created siblings and Resource.Published all
// exist exactly as they would — and then downgrades what the pre-#515 binary
// would have stored: the event's Data graph keyed by predicate IRI, with the
// old storable @context, and the canonical record to match. Sequence numbers,
// positions and transaction ids are untouched, which is what makes this the
// shape the migration must undo.
func (w *contextWorld) plantLegacy(
	slug, name string, refs map[string][]string, literals map[string]any, update bool,
) error {
	data := map[string]any{}
	for property, ids := range refs {
		if w.propertyIsList(slug, property) {
			data[property] = ids
		} else {
			if len(ids) != 1 {
				return fmt.Errorf("%q is a single reference but the scenario names %d targets", property, len(ids))
			}
			data[property] = ids[0]
		}
	}
	for k, v := range literals {
		data[k] = v
	}
	eventType := "Resource.Created"
	if update {
		eventType = "Resource.Updated"
		id, err := w.targetID(slug, name)
		if err != nil {
			return err
		}
		data["name"] = name
		raw, err := json.Marshal(data)
		if err != nil {
			return err
		}
		if _, err := w.rs.Update(context.Background(), application.UpdateResourceCommand{ID: id, Data: raw}); err != nil {
			return fmt.Errorf("failed to update %s %q: %w", slug, name, err)
		}
	} else if err := w.createResource(slug, name, data); err != nil {
		return err
	}
	return w.downgradeToLegacy(slug, name, refs, literals, eventType)
}

// downgradeToLegacy rewrites a resource's latest event of the given type and
// its canonical record to the pre-#515 shape: the edges node keyed by the
// predicate IRI each property resolved to under the type's context, and the
// old storable @context.
func (w *contextWorld) downgradeToLegacy(
	slug, name string, refs map[string][]string, literals map[string]any, eventType string,
) error {
	id := w.createdIDs[slug+"/"+name]

	rt, err := w.rts.GetBySlug(context.Background(), slug)
	if err != nil {
		return fmt.Errorf("failed to load the %s type: %w", slug, err)
	}
	vocab, contextMap := jsonld.ParseContext(rt.Context())
	entity := map[string]any{"@id": id, "@type": legacySchemaType(rt.Name(), rt.Context()), "name": name}
	for k, v := range literals {
		entity[k] = v
	}
	edges := map[string]any{"@id": id}
	for property, ids := range refs {
		predicate := jsonld.ResolvePredicateIRI(property, vocab, contextMap)
		if w.propertyIsList(slug, property) {
			list := make([]any, 0, len(ids))
			for _, target := range ids {
				list = append(list, map[string]any{"@id": target})
			}
			edges[predicate] = list
		} else {
			edges[predicate] = map[string]any{"@id": ids[0]}
		}
	}
	doc := map[string]any{"@graph": []any{entity, edges}}
	if ctx := legacyStorableContext(rt.Context()); ctx != nil {
		doc["@context"] = ctx
	}

	// Downgrade the EVENT: the latest event of the right type for this
	// aggregate is the one the write above appended.
	var rows []pericarpinfra.GormEventModel
	if err := w.db.Model(&pericarpinfra.GormEventModel{}).
		Where("aggregate_id = ? AND event_type = ?", id, eventType).
		Order("sequence_no DESC").Limit(1).Find(&rows).Error; err != nil {
		return fmt.Errorf("failed to find the %s event for %q: %w", eventType, name, err)
	}
	if len(rows) == 0 {
		return fmt.Errorf("no %s event was stored for %s %q", eventType, slug, name)
	}
	payload := rows[0].Payload
	payload["Data"] = doc
	if err := w.db.Model(&pericarpinfra.GormEventModel{}).
		Where("id = ?", rows[0].ID).Update("payload", payload).Error; err != nil {
		return fmt.Errorf("failed to downgrade the %s event for %q: %w", eventType, name, err)
	}

	// Downgrade the canonical record to match, as the old binary would have
	// left it, blanking the projection columns the edge feeds.
	table := inflection.Plural(strings.ReplaceAll(slug, "-", "_"))
	for property := range refs {
		if err := w.db.Table(table).Where("id = ?", id).
			Update(utils.CamelToSnake(property), nil).Error; err != nil {
			return fmt.Errorf("failed to blank the %q column before planting the legacy record: %w", property, err)
		}
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

// --- reading the stored events ---

type storedEvent struct {
	ID          string
	AggregateID string
	EventType   string
	SequenceNo  int
	Position    int64
	Payload     []byte
}

func (w *contextWorld) snapshotEvents() ([]storedEvent, error) {
	var rows []pericarpinfra.GormEventModel
	if err := w.db.Model(&pericarpinfra.GormEventModel{}).Order("position ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to read the event feed: %w", err)
	}
	out := make([]storedEvent, 0, len(rows))
	for _, r := range rows {
		payload, err := json.Marshal(r.Payload)
		if err != nil {
			return nil, err
		}
		out = append(out, storedEvent{ID: r.ID, AggregateID: r.AggregateID, EventType: r.EventType,
			SequenceNo: r.SequenceNo, Position: r.Position, Payload: payload})
	}
	return out, nil
}

// storedEventDocs returns the Data graph of every edge-carrying event for a
// resource, oldest first.
func (w *contextWorld) storedEventDocs(slug, name string) ([]map[string]any, error) {
	id, err := w.targetID(slug, name)
	if err != nil {
		return nil, err
	}
	var rows []pericarpinfra.GormEventModel
	if err := w.db.Model(&pericarpinfra.GormEventModel{}).
		Where("aggregate_id = ? AND event_type IN ?", id, []string{"Resource.Created", "Resource.Updated"}).
		Order("sequence_no ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to read the events for %s %q: %w", slug, name, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no resource event is stored for %s %q", slug, name)
	}
	docs := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		doc, ok := r.Payload["Data"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("event %s for %s %q carries no Data object", r.ID, slug, name)
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func (w *contextWorld) latestStoredEventDoc(slug, name string) (map[string]any, error) {
	docs, err := w.storedEventDocs(slug, name)
	if err != nil {
		return nil, err
	}
	return docs[len(docs)-1], nil
}

func edgesNodeOf(doc map[string]any) (map[string]any, error) {
	graph, ok := doc["@graph"].([]any)
	if !ok || len(graph) < 2 {
		return nil, fmt.Errorf("the stored document carries no edges node: %s", marshalForMessage(doc))
	}
	edges, ok := graph[1].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("the stored edges node is not a JSON object: %s", marshalForMessage(doc))
	}
	return edges, nil
}

func marshalForMessage(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (w *contextWorld) storedEventKeysEdgeBy(slug, name, property, key string) error {
	doc, err := w.latestStoredEventDoc(slug, name)
	if err != nil {
		return err
	}
	edges, err := edgesNodeOf(doc)
	if err != nil {
		return err
	}
	if _, ok := edges[key]; !ok {
		return fmt.Errorf("the stored event for %q keys no %q edge by %q (edge keys: %s)",
			name, property, key, edgeKeys(edges))
	}
	return nil
}

func (w *contextWorld) storedEventKeysEdgeByPropertyName(slug, name, property string) error {
	return w.storedEventKeysEdgeBy(slug, name, property, property)
}

func (w *contextWorld) storedEventKeysNoEdgeByIRI(slug, name string) error {
	doc, err := w.latestStoredEventDoc(slug, name)
	if err != nil {
		return err
	}
	return edgesHaveNoIRIKey(name, doc)
}

func (w *contextWorld) everyStoredEventKeysNoEdgeByIRI(slug, name string) error {
	docs, err := w.storedEventDocs(slug, name)
	if err != nil {
		return err
	}
	for _, doc := range docs {
		if err := edgesHaveNoIRIKey(name, doc); err != nil {
			return err
		}
	}
	return nil
}

func edgesHaveNoIRIKey(name string, doc map[string]any) error {
	edges, err := edgesNodeOf(doc)
	if err != nil {
		return err
	}
	for key := range edges {
		if key != "@id" && jsonld.IsIRIKey(key) {
			return fmt.Errorf("the stored event for %q still keys an edge by the IRI %q (edge keys: %s)",
				name, key, edgeKeys(edges))
		}
	}
	return nil
}

func (w *contextWorld) storedEventContextMaps(slug, name, term, iri string) error {
	doc, err := w.latestStoredEventDoc(slug, name)
	if err != nil {
		return err
	}
	raw, has := doc["@context"]
	if !has {
		return fmt.Errorf("the stored event for %q carries no @context at all", name)
	}
	embedded, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	_, contextMap := jsonld.ParseContext(embedded)
	got, ok := contextMap[term]
	if !ok {
		return fmt.Errorf("the stored event's @context for %q declares no mapping for %q (stored @context: %s)",
			name, term, embedded)
	}
	if got != iri {
		return fmt.Errorf("the stored event's @context for %q maps %q to %q, want %q", name, term, got, iri)
	}
	return nil
}

func (w *contextWorld) storedEventEntityNodeUnchanged(slug, name string) error {
	id, err := w.targetID(slug, name)
	if err != nil {
		return err
	}
	before := entityNodesByAggregate(w.eventsBeforeNormalize)[id]
	after, err := w.snapshotEvents()
	if err != nil {
		return err
	}
	now := entityNodesByAggregate(after)[id]
	if before == "" || now == "" {
		return fmt.Errorf("no entity node was captured for %q before (%q) or after (%q) the run", name, before, now)
	}
	if before != now {
		return fmt.Errorf("the entity node of %q changed across the run:\n before %s\n after  %s", name, before, now)
	}
	return nil
}

// entityNodesByAggregate renders each aggregate's Resource.Created entity node
// as canonical JSON, keyed by aggregate id.
func entityNodesByAggregate(events []storedEvent) map[string]string {
	out := map[string]string{}
	for _, e := range events {
		if e.EventType != "Resource.Created" {
			continue
		}
		var payload map[string]any
		if json.Unmarshal(e.Payload, &payload) != nil {
			continue
		}
		doc, _ := payload["Data"].(map[string]any)
		graph, _ := doc["@graph"].([]any)
		if len(graph) == 0 {
			continue
		}
		out[e.AggregateID] = marshalForMessage(graph[0])
	}
	return out
}

// --- running the normalization ---

func (w *contextWorld) normalize(write bool) error {
	return w.normalizeWith(application.NormalizeEdgeKeysOptions{Write: write})
}

// normalizeWith runs normalize-edge-keys with the given options, the live app
// stopped, snapshotting the feed on either side.
func (w *contextWorld) normalizeWith(opts application.NormalizeEdgeKeysOptions) error {
	w.stop()
	before, err := w.snapshotEvents()
	if err != nil {
		return err
	}
	w.eventsBeforeNormalize = before

	cfg := config.Default()
	cfg.DatabaseDSN = w.dsn
	cfg.LogLevel = "error"
	registry := w.catalogRegistry
	if w.registry != nil {
		registry = w.registry
	}
	var rt application.NormalizeEdgeKeysRuntime
	app := fx.New(fx.NopLogger, application.NormalizeEdgeKeysModule(cfg, registry()), fx.Populate(&rt))
	startCtx, startCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start the normalize-edge-keys runtime: %w", err)
	}
	report, runErr := application.NormalizeEdgeKeys(context.Background(), rt, opts)
	stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	_ = app.Stop(stopCtx)
	stopCancel()
	if runErr != nil {
		return fmt.Errorf("normalize-edge-keys failed: %w", runErr)
	}
	w.normalizeReport = &report

	after, err := w.snapshotEvents()
	if err != nil {
		return err
	}
	w.eventsAfterNormalize = after
	return w.boot()
}

func (w *contextWorld) theOperatorNormalizesDryRun() error    { return w.normalize(false) }
func (w *contextWorld) theOperatorNormalizesAndWrites() error { return w.normalize(true) }

func (w *contextWorld) normalizationReport() (*application.NormalizeEdgeKeysReport, error) {
	if w.normalizeReport == nil {
		return nil, fmt.Errorf("the normalization has not run in this scenario")
	}
	return w.normalizeReport, nil
}

func (w *contextWorld) typeCount(slug string, want int, dryRun bool) error {
	r, err := w.normalizationReport()
	if err != nil {
		return err
	}
	if r.DryRun != dryRun {
		return fmt.Errorf("the run reported DryRun=%v, want %v", r.DryRun, dryRun)
	}
	got := 0
	if t := r.Types[slug]; t != nil {
		got = t.Rewritten
	}
	if got != want {
		return fmt.Errorf("the normalization reported %d event(s) for %q, want %d (report: %s)",
			got, slug, want, marshalForMessage(r))
	}
	return nil
}

func (w *contextWorld) normalizationRewroteForType(n int, slug string) error {
	return w.typeCount(slug, n, false)
}

func (w *contextWorld) normalizationReportsForType(n int, slug string) error {
	return w.typeCount(slug, n, true)
}

func (w *contextWorld) normalizationRewroteNothing() error {
	r, err := w.normalizationReport()
	if err != nil {
		return err
	}
	if r.DryRun || r.Rewritten != 0 {
		return fmt.Errorf("expected a write that rewrote nothing, got DryRun=%v Rewritten=%d", r.DryRun, r.Rewritten)
	}
	return nil
}

func (w *contextWorld) normalizationReportsDryRun() error {
	r, err := w.normalizationReport()
	if err != nil {
		return err
	}
	if !r.DryRun {
		return fmt.Errorf("the run did not report itself as a dry run")
	}
	return nil
}

func (w *contextWorld) normalizationReportsNothing() error {
	r, err := w.normalizationReport()
	if err != nil {
		return err
	}
	if r.Rewritten != 0 {
		return fmt.Errorf("the normalization reported %d event(s) to rewrite, want none", r.Rewritten)
	}
	return nil
}

func (w *contextWorld) normalizationReportsNoUnresolved() error {
	r, err := w.normalizationReport()
	if err != nil {
		return err
	}
	if len(r.Unresolved) != 0 {
		return fmt.Errorf("the normalization reported unresolved edge keys: %s", marshalForMessage(r.Unresolved))
	}
	return nil
}

func (w *contextWorld) normalizationReportsAmbiguous(slug, name, iri, first, second string) error {
	r, err := w.normalizationReport()
	if err != nil {
		return err
	}
	id, err := w.targetID(slug, name)
	if err != nil {
		return err
	}
	want := []string{first, second}
	sort.Strings(want)
	for _, p := range r.Ambiguous {
		if p.ResourceID != id || p.Key != iri {
			continue
		}
		got := append([]string(nil), p.Candidates...)
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			return fmt.Errorf("the ambiguous edge key %s on %q names %v, want %v", iri, name, got, want)
		}
		return nil
	}
	return fmt.Errorf("the normalization did not report %q as ambiguous on %s (ambiguous: %s)",
		name, iri, marshalForMessage(r.Ambiguous))
}

func (w *contextWorld) normalizationReportsUnresolved(slug, name, iri string) error {
	r, err := w.normalizationReport()
	if err != nil {
		return err
	}
	id, err := w.targetID(slug, name)
	if err != nil {
		return err
	}
	for _, p := range r.Unresolved {
		if p.ResourceID == id && p.Key == iri {
			return nil
		}
	}
	return fmt.Errorf("the normalization did not report %q as unresolved on %s (unresolved: %s)",
		name, iri, marshalForMessage(r.Unresolved))
}

// --- the feed as a whole ---

func (w *contextWorld) beforeAndAfter() ([]storedEvent, []storedEvent, error) {
	if w.eventsBeforeNormalize == nil || w.eventsAfterNormalize == nil {
		return nil, nil, fmt.Errorf("the normalization has not run in this scenario")
	}
	return w.eventsBeforeNormalize, w.eventsAfterNormalize, nil
}

func (w *contextWorld) storedEventsUnchanged() error {
	before, after, err := w.beforeAndAfter()
	if err != nil {
		return err
	}
	if len(before) != len(after) {
		return fmt.Errorf("the feed held %d events before the run and %d after", len(before), len(after))
	}
	for i := range before {
		if before[i].ID != after[i].ID || !bytes.Equal(before[i].Payload, after[i].Payload) {
			return fmt.Errorf("event %s (%s) changed across the run:\n before %s\n after  %s",
				before[i].ID, before[i].EventType, before[i].Payload, after[i].Payload)
		}
	}
	return nil
}

func (w *contextWorld) eventFeedSameOrder() error {
	before, after, err := w.beforeAndAfter()
	if err != nil {
		return err
	}
	if len(before) != len(after) {
		return fmt.Errorf("the feed held %d events before the run and %d after", len(before), len(after))
	}
	for i := range before {
		if before[i].ID != after[i].ID || before[i].Position != after[i].Position {
			return fmt.Errorf("event order changed at index %d: %s@%d became %s@%d",
				i, before[i].ID, before[i].Position, after[i].ID, after[i].Position)
		}
	}
	return nil
}

func (w *contextWorld) eventsKeepIdentity() error {
	before, after, err := w.beforeAndAfter()
	if err != nil {
		return err
	}
	for i := range before {
		if i >= len(after) {
			break
		}
		b, a := before[i], after[i]
		if b.AggregateID != a.AggregateID || b.SequenceNo != a.SequenceNo || b.EventType != a.EventType {
			return fmt.Errorf("event %s lost its identity: %s/%d/%s became %s/%d/%s",
				b.ID, b.AggregateID, b.SequenceNo, b.EventType, a.AggregateID, a.SequenceNo, a.EventType)
		}
	}
	return nil
}

func (w *contextWorld) onlyTheseEventTypesRewritten(first, second string) error {
	before, after, err := w.beforeAndAfter()
	if err != nil {
		return err
	}
	for i := range before {
		if i >= len(after) || bytes.Equal(before[i].Payload, after[i].Payload) {
			continue
		}
		if t := after[i].EventType; t != first && t != second {
			return fmt.Errorf("event %s of type %s was rewritten; only %s and %s may be", after[i].ID, t, first, second)
		}
	}
	return nil
}

// --- the surrounding operator steps ---

func (w *contextWorld) theOperatorRemovesAliases() error {
	terms, err := w.storedContextOf("widget")
	if err != nil {
		return err
	}
	if _, ok := terms[jsonld.TermAliasesKeyword]; !ok {
		return fmt.Errorf("the stored widget context records no historical IRI to remove")
	}
	delete(terms, jsonld.TermAliasesKeyword)
	return w.writeStoredContext("widget", terms)
}

// captureEveryRead reads every resource the scenario created back through the
// projection, so a later step can prove the run reproduced each of them.
func (w *contextWorld) captureEveryRead() error {
	reads, err := w.readEverything()
	if err != nil {
		return err
	}
	w.capturedReads = reads
	return nil
}

func (w *contextWorld) everyReadMatches() error {
	if w.capturedReads == nil {
		return fmt.Errorf("no reads were captured in this scenario")
	}
	now, err := w.readEverything()
	if err != nil {
		return err
	}
	for key, want := range w.capturedReads {
		got, ok := now[key]
		if !ok {
			return fmt.Errorf("%s could not be read back after the run", key)
		}
		if got != want {
			return fmt.Errorf("the read of %s changed across the run:\n before %s\n after  %s", key, want, got)
		}
	}
	return nil
}

func (w *contextWorld) readEverything() (map[string]string, error) {
	out := map[string]string{}
	for key, id := range w.createdIDs {
		slug := strings.SplitN(key, "/", 2)[0]
		row, err := w.rs.GetFlat(context.Background(), slug, id)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s back: %w", key, err)
		}
		delete(row, "updated_at")
		delete(row, "updatedAt")
		out[key] = marshalForMessage(row)
	}
	return out, nil
}
