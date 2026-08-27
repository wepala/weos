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
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/fx"
	"gorm.io/gorm"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/internal/config"
	"github.com/wepala/weos/v3/pkg/jsonld"
)

// The acceptance suite in preset_context_reconcile_test.go proves the mechanism
// on a synthetic two-type preset. These two tests prove it on the ACTUAL
// shipped preset tree, which is what every real deployment boots — the issue
// reported 36 reference fields across 24 types stuck in the broken state on a
// real instance, and a synthetic preset cannot speak to that.

// bootRealPresets starts an app against dsn with the real preset registry,
// capturing the reconcile's operator-facing report.
func bootRealPresets(t *testing.T, dsn string, report *reconcileLog) (
	*gorm.DB, application.ResourceTypeService, func(),
) {
	t.Helper()

	cfg := config.Default()
	cfg.DatabaseDSN = dsn
	cfg.LogLevel = "error"

	registry := application.NewPresetRegistry()
	presets.RegisterAll(registry)

	var rts application.ResourceTypeService
	var db *gorm.DB
	app := fx.New(
		fx.NopLogger,
		application.Module(cfg, registry),
		fx.Decorate(func(inner entities.Logger) entities.Logger {
			return &capturingLogger{inner: inner, log: report}
		}),
		fx.Populate(&rts, &db),
	)
	startCtx, startCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		t.Fatalf("boot against the real preset tree failed: %v", err)
	}
	return db, rts, func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		_ = app.Stop(stopCtx)
		stopCancel()
	}
}

// uncoveredReferences names every installed reference property whose predicate
// IRI the stored context cannot map back — exactly the set FlattenGraph drops.
func uncoveredReferences(t *testing.T, rts application.ResourceTypeService) []string {
	t.Helper()

	page, err := rts.List(context.Background(), "", 500)
	if err != nil {
		t.Fatalf("failed to list resource types: %v", err)
	}
	if len(page.Data) == 0 {
		t.Fatal("no resource types were enumerated, so this audit would be vacuous")
	}
	var uncovered []string
	for _, rt := range page.Data {
		reverse := jsonld.BuildReverseMap(rt.Context())
		for _, ref := range application.ExtractReferenceProperties(rt.Schema(), rt.Context()) {
			if reverse[ref.PredicateIRI] != ref.PropertyName {
				uncovered = append(uncovered, rt.Slug()+"."+ref.PropertyName)
			}
		}
	}
	return uncovered
}

func countTypeUpdateEvents(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Table("events").
		Where("event_type = ?", "ResourceType.Updated").Count(&n).Error; err != nil {
		t.Fatalf("failed to count ResourceType.Updated events: %v", err)
	}
	return n
}

// installEveryPreset installs the presets that are not AutoInstall too. A bare
// boot installs only the handful marked for it, which would leave this audit
// covering three types instead of the whole tree.
func installEveryPreset(t *testing.T, rts application.ResourceTypeService) {
	t.Helper()
	registry := application.NewPresetRegistry()
	presets.RegisterAll(registry)
	for _, preset := range registry.List() {
		if _, err := rts.InstallPreset(context.Background(), preset.Name, false); err != nil {
			t.Fatalf("failed to install preset %q: %v", preset.Name, err)
		}
	}
}

// TestRealPresetsInstallWithEveryReferenceResolvable covers the FRESH install,
// which has no stored context to reconcile against and so cannot be repaired by
// a merge. A preset shipped with a missing term drops writes here permanently.
func TestRealPresetsInstallWithEveryReferenceResolvable(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "fresh.db")
	report := newReconcileLog()

	db, rts, stop := bootRealPresets(t, dsn, report)
	installEveryPreset(t, rts)
	if uncovered := uncoveredReferences(t, rts); len(uncovered) > 0 {
		t.Errorf("a fresh install leaves %d reference properties unresolvable, so their writes are dropped: %v",
			len(uncovered), uncovered)
	}
	baseline := countTypeUpdateEvents(t, db)
	stop()

	// A second boot over an already-correct database must be silent, or every
	// restart of every deployment appends an event per type.
	db2, _, stop2 := bootRealPresets(t, dsn, report)
	after := countTypeUpdateEvents(t, db2)
	stop2()
	if after != baseline {
		t.Errorf("a no-op boot emitted %d ResourceType.Updated events; the reconcile is not idempotent",
			after-baseline)
	}
}

// TestRealPresetsRepairAnAgedDatabase reproduces the state the issue reports
// from production: a database whose stored contexts predate the reference
// properties their schemas declare. Every such term is stripped, then the real
// tree boots — which is the upgrade every deployment performs on this release.
func TestRealPresetsRepairAnAgedDatabase(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "aged.db")
	report := newReconcileLog()

	db, rts, stop := bootRealPresets(t, dsn, report)
	installEveryPreset(t, rts)
	stripped := ageStoredContexts(t, rts)
	if stripped == 0 {
		t.Fatal("no context terms were stripped, so this test would prove nothing")
	}
	t.Logf("aged the database by stripping %d reference context terms", stripped)
	baseline := countTypeUpdateEvents(t, db)
	stop()

	// The upgrade boot.
	upgrade := newReconcileLog()
	db2, rts2, stop2 := bootRealPresets(t, dsn, upgrade)
	repaired := countTypeUpdateEvents(t, db2) - baseline
	uncovered := uncoveredReferences(t, rts2)
	stop2()

	upgrade.mu.Lock()
	failed := fmt.Sprintf("%v", upgrade.dropped)
	failedCount := len(upgrade.dropped)
	upgrade.mu.Unlock()

	// The invariant is NOT "everything is repaired". A term whose IRI differs
	// from the one the data already resolves through cannot be adopted without
	// orphaning those edges, so the boot holds it (issue #513) — and most
	// meal-planning terms are prefix-form (`mp:occurrenceOf`), so most of the
	// stripped terms land there. What must never happen is a reference left
	// uncovered with nothing said about it.
	reported := map[string]bool{}
	upgrade.mu.Lock()
	for slug := range upgrade.dropped {
		reported[slug] = true
	}
	for slug := range upgrade.heldContext {
		reported[slug] = true
	}
	for slug := range upgrade.heldSchema {
		reported[slug] = true
	}
	upgrade.mu.Unlock()

	var silent []string
	for _, ref := range uncovered {
		slug := strings.SplitN(ref, ".", 2)[0]
		if !reported[slug] {
			silent = append(silent, ref)
		}
	}
	if len(silent) > 0 {
		t.Errorf("the upgrade boot left %d reference properties uncovered AND unreported: %v",
			len(silent), silent)
	}
	t.Logf("upgrade boot: %d types rewritten, %d still uncovered (all reported), failures: %s",
		repaired, len(uncovered), failed)
	_ = failedCount

	// The repair must settle: a deployment that restarts hourly must not append
	// an event per type per boot forever.
	settled := newReconcileLog()
	db3, _, stop3 := bootRealPresets(t, dsn, settled)
	extra := countTypeUpdateEvents(t, db3) - (baseline + repaired)
	stop3()
	if extra != 0 {
		t.Errorf("the repair did not settle: the next boot emitted %d more ResourceType.Updated events", extra)
	}
}

// ageStoredContexts deletes every stored context term that a reference property
// depends on, leaving the schema and the projection column in place — the exact
// shape issue #510 reports, where the type looks migrated and silently is not.
func ageStoredContexts(t *testing.T, rts application.ResourceTypeService) int {
	t.Helper()

	page, err := rts.List(context.Background(), "", 500)
	if err != nil {
		t.Fatalf("failed to list resource types: %v", err)
	}
	stripped := 0
	for _, rt := range page.Data {
		refs := application.ExtractReferenceProperties(rt.Schema(), rt.Context())
		if len(refs) == 0 || len(rt.Context()) == 0 {
			continue
		}
		terms := map[string]any{}
		// Skipping a type whose context will not decode would under-reproduce
		// the broken state, and the test would then pass on a database that was
		// never fully aged — the silent-skip failure this whole change exists to
		// end. Fail instead, so the setup is provably complete.
		if err := json.Unmarshal(rt.Context(), &terms); err != nil {
			t.Fatalf("stored context for %q is not a JSON object, so it cannot be aged: %v", rt.Slug(), err)
		}
		removed := false
		for _, ref := range refs {
			if _, ok := terms[ref.PropertyName]; ok {
				delete(terms, ref.PropertyName)
				removed = true
				stripped++
			}
		}
		if !removed {
			continue
		}
		raw, mErr := json.Marshal(terms)
		if mErr != nil {
			t.Fatalf("failed to encode the aged context for %q: %v", rt.Slug(), mErr)
		}
		if _, err := rts.Update(context.Background(), application.UpdateResourceTypeCommand{
			ID:          rt.GetID(),
			Name:        rt.Name(),
			Slug:        rt.Slug(),
			Description: rt.Description(),
			Status:      rt.Status(),
			Context:     raw,
			Schema:      rt.Schema(),
		}); err != nil {
			t.Fatalf("failed to age the stored context for %q: %v", rt.Slug(), err)
		}
	}
	return stripped
}
