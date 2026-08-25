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
	env := domain.EventEnvelope[any]{ID: id, AggregateID: aggregate, EventType: eventType,
		Payload: payload, Created: time.Now().UTC(), SequenceNo: seq}
	if err := e.store.Append(context.Background(), aggregate, seq-1, env); err != nil {
		t.Fatalf("append %s: %v", id, err)
	}
}

func (e *normalizeTestEnv) run(t testing.TB, opts NormalizeEdgeKeysOptions) NormalizeEdgeKeysReport {
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
	report, err := NormalizeEdgeKeys(context.Background(), rt, opts)
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

	dry := env.run(t, NormalizeEdgeKeysOptions{BatchSize: 2})
	if !dry.DryRun || dry.Types["widget"].Rewritten != 4 || dry.Rewritten != 4 {
		t.Fatalf("dry run: %+v", dry)
	}
	if env.rawPayload(t, "evt-c-urn:widget:a") != before {
		t.Fatal("a dry run wrote to the store")
	}
	if dry.UnresolvedTotal != 1 || dry.Unresolved[0].TypeSlug != "ghost" {
		t.Fatalf("the missing type must be reported unresolved: %+v", dry.Unresolved)
	}

	written := env.run(t, NormalizeEdgeKeysOptions{BatchSize: 2, Write: true})
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

	again := env.run(t, NormalizeEdgeKeysOptions{BatchSize: 2, Write: true})
	if again.Rewritten != 0 || env.rawPayload(t, "evt-c-urn:widget:a") != after {
		t.Fatalf("second run must change nothing: %+v", again)
	}
}
