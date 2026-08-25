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

package application

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

// The run against a real store: the same narrow fx assembly the command
// uses, a scratch SQLite file, events appended through pericarp so the
// payload column holds exactly what a real feed holds.

type normalizeTestEnv struct {
	dsn   string
	store domain.EventStore
	db    *gorm.DB
}

func newNormalizeTestEnv(t testing.TB) *normalizeTestEnv {
	t.Helper()
	env := &normalizeTestEnv{dsn: filepath.Join(t.TempDir(), "normalize_test.db") +
		"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_txlock=immediate"}
	// Seed and project the resource type through the reproject rail, which
	// is what a real imported store would have done.
	var rt ReprojectRuntime
	app := fx.New(fx.NopLogger, ReprojectModule(config.Config{DatabaseDSN: env.dsn}),
		fx.Populate(&rt), fx.Populate(&env.db))
	ctx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("start seed runtime: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer stopCancel()
		_ = app.Stop(stopCtx)
	})
	env.store = rt.EventStore
	return env
}

func (e *normalizeTestEnv) append(t testing.TB, id, aggregate, eventType string, seq int, payload any) {
	t.Helper()
	e.appendInTx(t, id, aggregate, eventType, seq, "tx-"+id, payload)
}

// appendInTx appends with an explicit transaction id, so a Resource.Created
// and its Resource.Published can share one — which is what makes the
// projection write a canonical record on replay.
func (e *normalizeTestEnv) appendInTx(t testing.TB, id, aggregate, eventType string, seq int, tx string, payload any) {
	t.Helper()
	env := domain.EventEnvelope[any]{ID: id, AggregateID: aggregate, EventType: eventType,
		Payload: payload, Created: time.Now().UTC(), SequenceNo: seq, TransactionID: tx}
	if err := e.store.Append(context.Background(), aggregate, seq-1, env); err != nil {
		t.Fatalf("append %s: %v", id, err)
	}
}

func (e *normalizeTestEnv) run(t testing.TB, opts NormalizeEdgeKeysOptions) (NormalizeEdgeKeysReport, error) {
	t.Helper()
	var rt NormalizeEdgeKeysRuntime
	app := fx.New(fx.NopLogger, NormalizeEdgeKeysModule(config.Config{DatabaseDSN: e.dsn}, NewPresetRegistry()),
		fx.Populate(&rt))
	ctx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("start normalize runtime: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer stopCancel()
		_ = app.Stop(stopCtx)
	}()
	return NormalizeEdgeKeys(context.Background(), rt, opts)
}

func (e *normalizeTestEnv) mustRun(t testing.TB, opts NormalizeEdgeKeysOptions) NormalizeEdgeKeysReport {
	t.Helper()
	report, err := e.run(t, opts)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	return report
}

func (e *normalizeTestEnv) rawPayload(t testing.TB, id string) string {
	t.Helper()
	var raw string
	if err := e.db.Table("events").Select("payload").Where("id = ?", id).Scan(&raw).Error; err != nil {
		t.Fatalf("read payload %s: %v", id, err)
	}
	return raw
}

const legacyWidgetDoc = `{"@context":"https://schema.org/","@graph":[` +
	`{"@id":"%s","@type":"Widget","name":"%s","serial":9007199254740993,"weight":1.50},` +
	`{"@id":"%s","https://schema.org/maker":{"@id":"urn:vendor:v1"}}]}`

func TestNormalizeEdgeKeys_AgainstAStore(t *testing.T) {
	env := newNormalizeTestEnv(t)
	ctx := context.Background()

	env.append(t, "evt-rt", "urn:type:widget", "ResourceType.Created", 1, entities.ResourceTypeCreated{
		Name: "Widget", Slug: "widget", Timestamp: time.Now().UTC(),
		Context: json.RawMessage(`{"@vocab":"https://schema.org/","maker":{"@id":"https://schema.org/maker","@type":"@id"}}`),
		Schema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},` +
			`"maker":{"type":"string","x-resource-type":"vendor"}}}`),
	})
	// Three legacy widgets and an update, so a batch of 2 splits the feed
	// and the update's aggregate must carry its slug across the batch edge.
	for _, id := range []string{"urn:widget:a", "urn:widget:b", "urn:widget:c"} {
		env.append(t, "evt-c-"+id, id, "Resource.Created", 1, entities.ResourceCreated{
			TypeSlug: "widget", Data: json.RawMessage(strings.ReplaceAll(legacyWidgetDoc, "%s", id)),
			Timestamp: time.Now().UTC()})
	}
	env.append(t, "evt-u-c", "urn:widget:c", "Resource.Updated", 2, entities.ResourceUpdated{
		Data: json.RawMessage(strings.ReplaceAll(legacyWidgetDoc, "%s", "urn:widget:c")), Timestamp: time.Now().UTC()})
	// A type the store never held: reported, never fatal.
	env.append(t, "evt-ghost", "urn:ghost:1", "Resource.Created", 1, entities.ResourceCreated{
		TypeSlug: "ghost", Data: json.RawMessage(strings.ReplaceAll(legacyWidgetDoc, "%s", "urn:ghost:1")),
		Timestamp: time.Now().UTC()})

	// Materialize the resource type so FindBySlug can see it.
	var rt ReprojectRuntime
	seed := fx.New(fx.NopLogger, ReprojectModule(config.Config{DatabaseDSN: env.dsn}), fx.Populate(&rt))
	if err := seed.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := Reproject(ctx, rt, ReprojectOptions{}); err != nil {
		t.Fatalf("reproject: %v", err)
	}
	_ = seed.Stop(ctx)

	before := env.rawPayload(t, "evt-c-urn:widget:a")

	dry := env.mustRun(t, NormalizeEdgeKeysOptions{BatchSize: 2})
	if !dry.DryRun || dry.Types["widget"].Rewritten != 4 || dry.Rewritten != 4 {
		t.Fatalf("dry run: %+v", dry)
	}
	if env.rawPayload(t, "evt-c-urn:widget:a") != before {
		t.Fatal("a dry run wrote to the store")
	}
	if dry.UnresolvedTotal != 1 || dry.Unresolved[0].TypeSlug != "ghost" {
		t.Fatalf("the missing type must be reported unresolved: %+v", dry.Unresolved)
	}

	written := env.mustRun(t, NormalizeEdgeKeysOptions{BatchSize: 2, Write: true})
	if written.Rewritten != 4 || written.Types["widget"].Rewritten != 4 || written.Types["widget"].Scanned != 4 {
		t.Fatalf("write: %+v", written)
	}
	after := env.rawPayload(t, "evt-c-urn:widget:a")
	// The entity node's numbers are re-encoded exactly as the store held
	// them. (pericarp's own insert already went through float64, which is
	// why the stored serial is 9007199254740992 and not the 9007199254740993
	// the fixture wrote — the migration must not move it a second time.)
	numbers := regexp.MustCompile(`"serial":[0-9.e+]+,"weight":[0-9.e+]+`)
	if got, want := numbers.FindString(after), numbers.FindString(before); got != want || want == "" {
		t.Errorf("entity numbers changed across the rewrite: before %q, after %q", want, got)
	}
	if !strings.Contains(after, `"maker":{"@id":"urn:vendor:v1"}`) {
		t.Errorf("rewritten payload lacks the property-name key:\n%s", after)
	}
	if strings.Contains(after, "https://schema.org/maker\":{") {
		t.Errorf("the IRI key survived the rewrite:\n%s", after)
	}
	if u := env.rawPayload(t, "evt-u-c"); !strings.Contains(u, `"maker":{"@id":"urn:vendor:v1"}`) {
		t.Errorf("the update event in the last batch was not rewritten:\n%s", u)
	}

	again := env.mustRun(t, NormalizeEdgeKeysOptions{BatchSize: 2, Write: true})
	if again.Rewritten != 0 || env.rawPayload(t, "evt-c-urn:widget:a") != after {
		t.Fatalf("second run must change nothing: %+v", again)
	}
}

func (e *normalizeTestEnv) count(t testing.TB, opts IRIEdgeKeyCountOptions) (IRIEdgeKeyCountReport, error) {
	t.Helper()
	var rt IRIEdgeKeyCountRuntime
	app := fx.New(fx.NopLogger, IRIEdgeKeyCountModule(config.Config{DatabaseDSN: e.dsn}, NewPresetRegistry()),
		fx.Populate(&rt))
	ctx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("start count runtime: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer stopCancel()
		_ = app.Stop(stopCtx)
	}()
	return CountIRIEdgeKeys(context.Background(), rt, opts)
}

func TestCountIRIEdgeKeys_RefusesAnEmptyStore(t *testing.T) {
	env := newNormalizeTestEnv(t)
	if _, err := env.count(t, IRIEdgeKeyCountOptions{}); !errors.Is(err, ErrNothingToCheck) {
		t.Fatalf("an empty store must be refused, got %v", err)
	}
}

func TestCountIRIEdgeKeys_AgainstAStore(t *testing.T) {
	env := newNormalizeTestEnv(t)
	ctx := context.Background()
	env.append(t, "evt-rt", "urn:type:widget", "ResourceType.Created", 1, entities.ResourceTypeCreated{
		Name: "Widget", Slug: "widget", Timestamp: time.Now().UTC(),
		Context: json.RawMessage(`{"@vocab":"https://schema.org/","maker":{"@id":"https://schema.org/maker","@type":"@id"}}`),
		Schema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},` +
			`"maker":{"type":"string","x-resource-type":"vendor"}}}`),
	})
	for _, id := range []string{"urn:widget:a", "urn:widget:b", "urn:widget:c"} {
		env.appendInTx(t, "evt-c-"+id, id, "Resource.Created", 1, "tx-"+id, entities.ResourceCreated{
			TypeSlug: "widget", Data: json.RawMessage(strings.ReplaceAll(legacyWidgetDoc, "%s", id)),
			Timestamp: time.Now().UTC()})
		env.appendInTx(t, "evt-p-"+id, id, "Resource.Published", 2, "tx-"+id, entities.ResourcePublished{
			TypeSlug: "widget", Timestamp: time.Now().UTC()})
	}
	env.appendInTx(t, "evt-u-c", "urn:widget:c", "Resource.Updated", 3, "tx-u-c", entities.ResourceUpdated{
		Data: json.RawMessage(strings.ReplaceAll(legacyWidgetDoc, "%s", "urn:widget:c")), Timestamp: time.Now().UTC()})
	env.appendInTx(t, "evt-pu-c", "urn:widget:c", "Resource.Published", 4, "tx-u-c", entities.ResourcePublished{
		TypeSlug: "widget", Timestamp: time.Now().UTC()})
	env.appendInTx(t, "evt-ghost", "urn:ghost:1", "Resource.Created", 1, "tx-ghost", entities.ResourceCreated{
		TypeSlug: "ghost", Data: json.RawMessage(strings.ReplaceAll(legacyWidgetDoc, "%s", "urn:ghost:1")),
		Timestamp: time.Now().UTC()})
	env.appendInTx(t, "evt-pghost", "urn:ghost:1", "Resource.Published", 2, "tx-ghost", entities.ResourcePublished{
		TypeSlug: "ghost", Timestamp: time.Now().UTC()})
	reproject := func() {
		var rt ReprojectRuntime
		seed := fx.New(fx.NopLogger, ReprojectModule(config.Config{DatabaseDSN: env.dsn}), fx.Populate(&rt))
		if err := seed.Start(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := Reproject(ctx, rt, ReprojectOptions{}); err != nil {
			t.Fatalf("reproject: %v", err)
		}
		_ = seed.Stop(ctx)
	}
	reproject()

	report, err := env.count(t, IRIEdgeKeyCountOptions{BatchSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	w := report.Types["widget"]
	if w == nil || w.Events != 3 || w.Records != 3 || w.Resolvable != 3 || w.Residue != 0 {
		t.Fatalf("widget: %+v", w)
	}
	g := report.Types["ghost"]
	if g == nil || g.Orphaned != 1 || g.Events != 0 || g.Records != 0 {
		t.Fatalf("the missing type must be reported orphaned on both surfaces, not counted: %+v", g)
	}
	if report.EventsTotal != 3 || report.RecordsTotal != 3 || report.OrphanedTotal != 1 || report.Passes() {
		t.Fatalf("totals: %+v", report)
	}

	if _, err := env.run(t, NormalizeEdgeKeysOptions{Write: true}); err != nil {
		t.Fatal(err)
	}
	window, err := env.count(t, IRIEdgeKeyCountOptions{BatchSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if window.EventsTotal != 0 || window.RecordsTotal != 3 || window.Verdict() != VerdictFail {
		t.Fatalf("between normalize and reproject the events are clean and the records are not: %+v", window)
	}

	reproject()
	after, err := env.count(t, IRIEdgeKeyCountOptions{BatchSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if after.EventsTotal != 0 || after.RecordsTotal != 0 || after.OrphanedTotal != 1 || !after.Passes() {
		t.Fatalf("after reproject both surfaces are clean and the orphan does not fail the check: %+v", after)
	}
	recordsOnly, err := env.count(t, IRIEdgeKeyCountOptions{RecordsOnly: true, BatchSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !recordsOnly.RecordsOnly || recordsOnly.EventsTotal != 0 || recordsOnly.RecordsTotal != 0 ||
		!recordsOnly.Passes() {
		t.Fatalf("records-only: %+v", recordsOnly)
	}
}

// The population --restamp serves on an old instance: an IRI-keyed
// (pre-#515) document whose old predicate the type's context now names only
// through an alias, and the Triple.Created written beside it. Both must move
// in one run — the triple from the resolver's answer, before the rewrite
// replaces the document's context.
func TestNormalizeEdgeKeys_RestampMovesTheTripleOfAnIRIKeyedDocument(t *testing.T) {
	env := newNormalizeTestEnv(t)
	ctx := context.Background()
	env.append(t, "evt-rt", "urn:type:food-item", "ResourceType.Created", 1, entities.ResourceTypeCreated{
		Name: "Food Item", Slug: "food-item", Timestamp: time.Now().UTC(),
		Context: json.RawMessage(`{"@vocab":"https://schema.org/","mp":"https://weos.io/vocab/meal-planning#",
		  "@type":"mp:FoodItem","ingredient":{"@id":"mp:isInstanceOf","@type":"@id"},
		  "weos:termAliases":{"ingredient":["https://weos.org/vocab/meal-planning#isInstanceOf"]}}`),
		Schema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},` +
			`"ingredient":{"type":"string","x-resource-type":"ingredient"}}}`),
	})
	legacy := `{"@context":{"@vocab":"https://schema.org/","mp":"https://weos.org/vocab/meal-planning#"},"@graph":[` +
		`{"@id":"urn:food-item:a","@type":"mp:FoodItem","name":"Garlic head"},` +
		`{"@id":"urn:food-item:a","https://weos.org/vocab/meal-planning#isInstanceOf":{"@id":"urn:ingredient:g"}}]}`
	env.appendInTx(t, "evt-c", "urn:food-item:a", "Resource.Created", 1, "tx-a", entities.ResourceCreated{
		TypeSlug: "food-item", Data: json.RawMessage(legacy), Timestamp: time.Now().UTC()})
	env.appendInTx(t, "evt-t", "urn:food-item:a", "Triple.Created", 2, "tx-a", entities.TripleCreated{}.With(
		"urn:food-item:a", "https://weos.org/vocab/meal-planning#isInstanceOf", "urn:ingredient:g", ""))
	var rt ReprojectRuntime
	seed := fx.New(fx.NopLogger, ReprojectModule(config.Config{DatabaseDSN: env.dsn}), fx.Populate(&rt))
	if err := seed.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := Reproject(ctx, rt, ReprojectOptions{}); err != nil {
		t.Fatalf("reproject: %v", err)
	}
	_ = seed.Stop(ctx)

	report := env.mustRun(t, NormalizeEdgeKeysOptions{Write: true, Restamp: true, BatchSize: 1})
	fi := report.Types["food-item"]
	if fi == nil || fi.Rewritten != 1 || fi.Restamped != 1 || fi.TriplesMoved != 1 {
		t.Fatalf("food-item: %+v (report %+v)", fi, report)
	}
	doc := env.rawPayload(t, "evt-c")
	if !strings.Contains(doc, `"ingredient":{"@id":"urn:ingredient:g"}`) ||
		!strings.Contains(doc, `"mp":"https://weos.io/vocab/meal-planning#"`) {
		t.Errorf("the document was not rewritten and re-stamped together:\n%s", doc)
	}
	triple := env.rawPayload(t, "evt-t")
	if !strings.Contains(triple, `"predicate":"https://weos.io/vocab/meal-planning#isInstanceOf"`) {
		t.Errorf("the triple did not move with its document:\n%s", triple)
	}
	again := env.mustRun(t, NormalizeEdgeKeysOptions{Write: true, Restamp: true})
	if again.Rewritten+again.Restamped+again.TriplesMoved != 0 {
		t.Errorf("a second run must move nothing: %+v", again)
	}
}
