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
	"reflect"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/entities"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"go.uber.org/fx"
)

// schemaProps decodes a schema's `properties` map so assertions can talk about
// property names without depending on key order in the marshaled output.
func schemaProps(t *testing.T, raw json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("schema is not a JSON object: %v", err)
	}
	props := map[string]json.RawMessage{}
	if p, ok := top["properties"]; ok {
		if err := json.Unmarshal(p, &props); err != nil {
			t.Fatalf("`properties` is not a JSON object: %v", err)
		}
	}
	return props
}

// sameStrings compares two string slices, treating nil and empty as equal so a
// table case can leave an expectation unset.
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// assertRequired checks the merged schema's `required` array exactly.
func assertRequired(t *testing.T, raw json.RawMessage, want []string) {
	t.Helper()
	var doc struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("merged schema is not a JSON object: %v", err)
	}
	if !sameStrings(doc.Required, want) {
		t.Errorf("required = %v, want %v", doc.Required, want)
	}
}

func TestReconcileAdditiveSchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		stored        string
		preset        string
		wantChanged   bool
		wantAdded     []string
		wantConflicts []string
		// wantProps, when set, lists every property name the merged schema must
		// carry — the guard that preserved and added properties coexist.
		wantProps []string
		// wantRequired, when set, is the exact reconciled `required` array.
		wantRequired []string
	}{{
		name:        "identical schemas are a no-op",
		stored:      `{"type":"object","properties":{"name":{"type":"string"}}}`,
		preset:      `{"type":"object","properties":{"name":{"type":"string"}}}`,
		wantChanged: false,
	}, {
		name:        "formatting-only differences are a no-op",
		stored:      `{"type":"object","properties":{"name":{"type":"string"}}}`,
		preset:      "{\n \"properties\" : {\"name\":{ \"type\":\"string\" }},\n \"type\":\"object\"\n}",
		wantChanged: false,
	}, {
		name:        "a new property is merged in",
		stored:      `{"type":"object","properties":{"name":{"type":"string"}}}`,
		preset:      `{"type":"object","properties":{"name":{"type":"string"},"sku":{"type":"string"}}}`,
		wantChanged: true,
		wantAdded:   []string{"sku"},
		wantProps:   []string{"name", "sku"},
	}, {
		name:        "an empty stored schema adopts the preset wholesale",
		stored:      ``,
		preset:      `{"type":"object","properties":{"name":{"type":"string"},"sku":{"type":"string"}}}`,
		wantChanged: true,
		wantAdded:   []string{"name", "sku"},
		wantProps:   []string{"name", "sku"},
	}, {
		name:        "an empty preset schema never clears the stored one",
		stored:      `{"type":"object","properties":{"name":{"type":"string"}}}`,
		preset:      ``,
		wantChanged: false,
	}, {
		// The operator-customization guard: a property only the database knows
		// about survives, and does not block the preset's own addition.
		name:        "a stored-only property is preserved alongside a new one",
		stored:      `{"type":"object","properties":{"name":{"type":"string"},"nickname":{"type":"string"}}}`,
		preset:      `{"type":"object","properties":{"name":{"type":"string"},"sku":{"type":"string"}}}`,
		wantChanged: true,
		wantAdded:   []string{"sku"},
		wantProps:   []string{"name", "nickname", "sku"},
	}, {
		name:        "a property the preset drops is preserved, not removed",
		stored:      `{"type":"object","properties":{"name":{"type":"string"},"legacy":{"type":"string"}}}`,
		preset:      `{"type":"object","properties":{"name":{"type":"string"}}}`,
		wantChanged: false,
	}, {
		name:          "a retyped property is refused",
		stored:        `{"type":"object","properties":{"sku":{"type":"string"}}}`,
		preset:        `{"type":"object","properties":{"sku":{"type":"integer"}}}`,
		wantChanged:   false,
		wantConflicts: []string{"sku"},
	}, {
		// Per-property refusal: the conflicting property is held at its stored
		// definition, but the additive sibling still lands — otherwise one
		// cosmetic edit would block the whole type and #379 would persist for it.
		name:          "a held property does not block an additive sibling",
		stored:        `{"type":"object","properties":{"sku":{"type":"string"}}}`,
		preset:        `{"type":"object","properties":{"sku":{"type":"string","maxLength":8},"tag":{"type":"string"}}}`,
		wantChanged:   true,
		wantAdded:     []string{"tag"},
		wantConflicts: []string{"sku"},
		wantProps:     []string{"sku", "tag"},
	}, {
		// Akeem's call on issue #379: the preset is authoritative for `required`,
		// so tightening it applies even though existing rows may then fail
		// validation on their next write.
		name:        "a tightened required array follows the preset",
		stored:      `{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`,
		preset:      `{"type":"object","required":["name","sku"],"properties":{"name":{"type":"string"},"sku":{"type":"string"}}}`,
		wantChanged: true,
		wantAdded:   []string{"sku"},
		wantProps:   []string{"name", "sku"},
	}, {
		// The preset has no opinion about a property it doesn't declare, so its
		// `required` list must not silently drop the operator's own requirement.
		name:         "a stored-only property keeps its requirement",
		stored:       `{"type":"object","required":["name","nickname"],"properties":{"name":{"type":"string"},"nickname":{"type":"string"}}}`,
		preset:       `{"type":"object","required":["name"],"properties":{"name":{"type":"string"},"sku":{"type":"string"}}}`,
		wantChanged:  true,
		wantAdded:    []string{"sku"},
		wantProps:    []string{"name", "nickname", "sku"},
		wantRequired: []string{"name", "nickname"},
	}, {
		// ...but a requirement the preset DOES have an opinion about follows the
		// preset, including when the preset drops it.
		name:         "a requirement on a preset-declared property follows the preset",
		stored:       `{"type":"object","required":["name","sku"],"properties":{"name":{"type":"string"},"sku":{"type":"string"}}}`,
		preset:       `{"type":"object","required":["name"],"properties":{"name":{"type":"string"},"sku":{"type":"string"}}}`,
		wantChanged:  true,
		wantRequired: []string{"name"},
	}, {
		name:        "a changed top-level keyword alone still counts as a change",
		stored:      `{"type":"object","additionalProperties":true,"properties":{"name":{"type":"string"}}}`,
		preset:      `{"type":"object","additionalProperties":false,"properties":{"name":{"type":"string"}}}`,
		wantChanged: true,
	}, {
		name:   "nested object properties compare by value",
		stored: `{"type":"object","properties":{"addr":{"type":"object","properties":{"city":{"type":"string"}}}}}`,
		preset: `{"type":"object","properties":{"addr":{"type":"object","properties":{"city":{"type":"string"}}}}}`,
		//nolint:lll // the schemas above are clearer on one line each
		wantChanged: false,
	}, {
		name:          "a nested object property that changes shape is refused",
		stored:        `{"type":"object","properties":{"addr":{"type":"object","properties":{"city":{"type":"string"}}}}}`,
		preset:        `{"type":"object","properties":{"addr":{"type":"object","properties":{"zip":{"type":"string"}}}}}`,
		wantChanged:   false,
		wantConflicts: []string{"addr"},
	}, {
		name:        "a ref-shaped property is added like any other",
		stored:      `{"type":"object","properties":{"name":{"type":"string"}}}`,
		preset:      `{"type":"object","properties":{"name":{"type":"string"},"owner":{"$ref":"#/defs/Person"}}}`,
		wantChanged: true,
		wantAdded:   []string{"owner"},
		wantProps:   []string{"name", "owner"},
	}, {
		// A rename is NOT detected: it reads as one property added and one
		// preserved. Both columns land, the old keeps its data, the new is NULL.
		// Additively safe, but not a migration — pinned so the ADR can't drift.
		name:        "a renamed property is added, not migrated",
		stored:      `{"type":"object","properties":{"name":{"type":"string"},"sku":{"type":"string"}}}`,
		preset:      `{"type":"object","properties":{"name":{"type":"string"},"stockKeepingUnit":{"type":"string"}}}`,
		wantChanged: true,
		wantAdded:   []string{"stockKeepingUnit"},
		wantProps:   []string{"name", "sku", "stockKeepingUnit"},
	}, {
		// Indirection routes around the conflict check: the $ref is identical on
		// both sides, so the retype hides in $defs, which the preset simply wins.
		// Documented as a scope limit; pinned so it fails loudly if it changes.
		name:        "a retype behind $ref is NOT caught as a conflict",
		stored:      `{"type":"object","$defs":{"P":{"type":"string"}},"properties":{"o":{"$ref":"#/$defs/P"}}}`,
		preset:      `{"type":"object","$defs":{"P":{"type":"integer"}},"properties":{"o":{"$ref":"#/$defs/P"}}}`,
		wantChanged: true,
		wantProps:   []string{"o"},
	}, {
		name:        "a schema with no properties key is tolerated",
		stored:      `{"type":"object"}`,
		preset:      `{"type":"object","properties":{"sku":{"type":"string"}}}`,
		wantChanged: true,
		wantAdded:   []string{"sku"},
		wantProps:   []string{"sku"},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := reconcileAdditiveSchema(json.RawMessage(tc.stored), json.RawMessage(tc.preset))
			if err != nil {
				t.Fatalf("reconcileAdditiveSchema: %v", err)
			}
			if got.Changed != tc.wantChanged {
				t.Fatalf("Changed = %v, want %v (result %+v)", got.Changed, tc.wantChanged, got)
			}
			if tc.wantAdded != nil && !reflect.DeepEqual(got.Added, tc.wantAdded) {
				t.Errorf("Added = %v, want %v", got.Added, tc.wantAdded)
			}
			if !sameStrings(got.Conflicts, tc.wantConflicts) {
				t.Errorf("Conflicts = %v, want %v", got.Conflicts, tc.wantConflicts)
			}
			// A held property must keep its STORED definition in whatever schema
			// comes back — that is what keeps the merge additive.
			if len(tc.wantConflicts) > 0 && got.Schema != nil {
				storedProps := schemaProps(t, json.RawMessage(tc.stored))
				mergedProps := schemaProps(t, got.Schema)
				for _, name := range tc.wantConflicts {
					if !jsonEquivalent(storedProps[name], mergedProps[name]) {
						t.Errorf("held property %q was rewritten: stored %s, merged %s",
							name, storedProps[name], mergedProps[name])
					}
				}
			}
			if !tc.wantChanged {
				if got.Schema != nil {
					t.Errorf("unchanged reconcile returned a schema: %s", got.Schema)
				}
				return
			}
			if tc.wantProps == nil {
				return
			}
			if tc.wantRequired != nil {
				assertRequired(t, got.Schema, tc.wantRequired)
			}
			props := schemaProps(t, got.Schema)
			if len(props) != len(tc.wantProps) {
				t.Fatalf("merged properties = %v, want exactly %v", sortedKeys(props), tc.wantProps)
			}
			for _, name := range tc.wantProps {
				if _, ok := props[name]; !ok {
					t.Errorf("merged schema is missing property %q (have %v)", name, sortedKeys(props))
				}
			}
		})
	}
}

// TestReconcileAdditiveSchema_Idempotent pins the property that keeps a restart
// quiet: re-running the merge against its own output reports no change, so no
// ResourceTypeUpdated event is emitted on the second boot.
func TestReconcileAdditiveSchema_Idempotent(t *testing.T) {
	t.Parallel()
	stored := json.RawMessage(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`)
	preset := json.RawMessage(
		`{"type":"object","required":["name","sku"],"properties":{"name":{"type":"string"},"sku":{"type":"string"}}}`)

	first, err := reconcileAdditiveSchema(stored, preset)
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if !first.Changed {
		t.Fatal("first reconcile should have changed the schema")
	}

	second, err := reconcileAdditiveSchema(first.Schema, preset)
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if second.Changed {
		t.Fatalf("second reconcile should be a no-op, got %+v", second)
	}
}

func TestReconcileAdditiveSchema_MalformedSchemaErrors(t *testing.T) {
	t.Parallel()
	valid := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)

	if _, err := reconcileAdditiveSchema(valid, json.RawMessage(`not json`)); err == nil {
		t.Error("expected an error for a malformed preset schema")
	}
	if _, err := reconcileAdditiveSchema(json.RawMessage(`not json`), valid); err == nil {
		t.Error("expected an error for a malformed stored schema")
	}
	if _, err := reconcileAdditiveSchema(valid, json.RawMessage(`{"properties":"nope"}`)); err == nil {
		t.Error("expected an error when `properties` is not an object")
	}
}

// TestReconcilePresetSchemas_AddsPropertyToInstalledType is the service-level
// case behind issue #379: a database provisioned by an earlier build, booted
// against a preset that has since gained a property.
func TestReconcilePresetSchemas_AddsPropertyToInstalledType(t *testing.T) {
	t.Parallel()
	v1 := NewPresetRegistry()
	v1.MustAdd(testPresetSingle(
		"Product", "product", "A product",
		`{"@vocab":"https://schema.org/"}`,
		`{"type":"object","properties":{"name":{"type":"string"}}}`,
	))

	repo := newInstallTestTypeRepo()
	rSvc := newFakeResourceSvc()
	svc := makeInstallTestService(repo, rSvc, v1)
	if _, err := svc.InstallPreset(context.Background(), "single", false); err != nil {
		t.Fatalf("initial install: %v", err)
	}

	// The next build declares an extra property.
	v2 := NewPresetRegistry()
	v2.MustAdd(testPresetSingle(
		"Product", "product", "A product",
		`{"@vocab":"https://schema.org/"}`,
		`{"type":"object","properties":{"name":{"type":"string"},"sku":{"type":"string"}}}`,
	))
	svc2 := reconcileTestService(t, repo, rSvc, v2)

	res, err := svc2.ReconcilePresetSchemas(context.Background(), "single")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(res.Updated) != 1 || res.Updated[0] != "product" {
		t.Fatalf("expected Updated=[product], got %+v", res)
	}

	stored, err := repo.FindBySlug(context.Background(), "product")
	if err != nil {
		t.Fatalf("FindBySlug: %v", err)
	}
	props := schemaProps(t, stored.Schema())
	if _, ok := props["sku"]; !ok {
		t.Fatalf("stored schema is missing the added property (have %v)", sortedKeys(props))
	}
}

// TestReconcilePresetSchemas_UnchangedEmitsNoEvent covers scenarios 3 and 4 of
// the acceptance contract: restarting against an unchanged preset must not
// append a ResourceTypeUpdated per type per boot.
func TestReconcilePresetSchemas_UnchangedEmitsNoEvent(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	registry.MustAdd(testPresetSingle(
		"Product", "product", "A product",
		`{"@vocab":"https://schema.org/"}`,
		`{"type":"object","properties":{"name":{"type":"string"}}}`,
	))

	repo := newInstallTestTypeRepo()
	rSvc := newFakeResourceSvc()
	svc := makeInstallTestService(repo, rSvc, registry)
	if _, err := svc.InstallPreset(context.Background(), "single", false); err != nil {
		t.Fatalf("initial install: %v", err)
	}

	d := domain.NewEventDispatcher()
	pm := &stubProjMgr{}
	if err := SubscribeResourceTypeHandlers(d, repo, pm, noopLogger{}); err != nil {
		t.Fatalf("SubscribeResourceTypeHandlers: %v", err)
	}
	updateCount := countingUpdateSub(t, d)
	svc2 := &resourceTypeService{
		repo: repo, projMgr: pm, eventStore: &stubEventStore{}, dispatcher: d,
		registry: registry, logger: noopLogger{}, resourceSvc: rSvc,
	}

	for i := 0; i < 2; i++ {
		res, err := svc2.ReconcilePresetSchemas(context.Background(), "single")
		if err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
		if len(res.Unchanged) != 1 || len(res.Updated) != 0 {
			t.Fatalf("reconcile %d should be a no-op, got %+v", i, res)
		}
	}
	if *updateCount != 0 {
		t.Fatalf("expected zero ResourceTypeUpdated events, got %d", *updateCount)
	}
}

// TestReconcilePresetSchemas_SettlesAfterOneUpdate is scenario 4 proper: the
// first boot after a schema change updates once, the next boot is quiet.
func TestReconcilePresetSchemas_SettlesAfterOneUpdate(t *testing.T) {
	t.Parallel()
	v1 := NewPresetRegistry()
	v1.MustAdd(testPresetSingle(
		"Product", "product", "A product",
		`{"@vocab":"https://schema.org/"}`,
		`{"type":"object","properties":{"name":{"type":"string"}}}`,
	))
	repo := newInstallTestTypeRepo()
	rSvc := newFakeResourceSvc()
	if _, err := makeInstallTestService(repo, rSvc, v1).
		InstallPreset(context.Background(), "single", false); err != nil {
		t.Fatalf("initial install: %v", err)
	}

	v2 := NewPresetRegistry()
	v2.MustAdd(testPresetSingle(
		"Product", "product", "A product",
		`{"@vocab":"https://schema.org/"}`,
		`{"type":"object","properties":{"name":{"type":"string"},"sku":{"type":"string"}}}`,
	))

	d := domain.NewEventDispatcher()
	pm := &stubProjMgr{}
	if err := SubscribeResourceTypeHandlers(d, repo, pm, noopLogger{}); err != nil {
		t.Fatalf("SubscribeResourceTypeHandlers: %v", err)
	}
	updateCount := countingUpdateSub(t, d)
	svc := &resourceTypeService{
		repo: repo, projMgr: pm, eventStore: &stubEventStore{}, dispatcher: d,
		registry: v2, logger: noopLogger{}, resourceSvc: rSvc,
	}

	if _, err := svc.ReconcilePresetSchemas(context.Background(), "single"); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	second, err := svc.ReconcilePresetSchemas(context.Background(), "single")
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if len(second.Updated) != 0 || len(second.Unchanged) != 1 {
		t.Fatalf("second reconcile should be a no-op, got %+v", second)
	}
	if *updateCount != 1 {
		t.Fatalf("expected exactly one ResourceTypeUpdated event, got %d", *updateCount)
	}
}

// TestReconcilePresetSchemas_PreservesStoredIdentity is the operator-
// customization guard: only the schema is reconciled, so a renamed or
// re-described built-in type keeps its edits across a restart.
func TestReconcilePresetSchemas_PreservesStoredIdentity(t *testing.T) {
	t.Parallel()
	v1 := NewPresetRegistry()
	v1.MustAdd(testPresetSingle(
		"Product", "product", "A product",
		`{"@vocab":"https://schema.org/"}`,
		`{"type":"object","properties":{"name":{"type":"string"}}}`,
	))
	repo := newInstallTestTypeRepo()
	rSvc := newFakeResourceSvc()
	svc := makeInstallTestService(repo, rSvc, v1)
	if _, err := svc.InstallPreset(context.Background(), "single", false); err != nil {
		t.Fatalf("initial install: %v", err)
	}

	// The operator renames and re-describes the built-in type.
	stored, err := repo.FindBySlug(context.Background(), "product")
	if err != nil {
		t.Fatalf("FindBySlug: %v", err)
	}
	if _, err := svc.Update(context.Background(), UpdateResourceTypeCommand{
		ID:          stored.GetID(),
		Name:        "Catalog Item",
		Slug:        "product",
		Description: "Our own wording",
		Status:      stored.Status(),
		Context:     stored.Context(),
		Schema:      stored.Schema(),
	}); err != nil {
		t.Fatalf("operator update: %v", err)
	}

	// A later build adds a property.
	v2 := NewPresetRegistry()
	v2.MustAdd(testPresetSingle(
		"Product", "product", "A product",
		`{"@vocab":"https://schema.org/"}`,
		`{"type":"object","properties":{"name":{"type":"string"},"sku":{"type":"string"}}}`,
	))
	if _, err := reconcileTestService(t, repo, rSvc, v2).
		ReconcilePresetSchemas(context.Background(), "single"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	after, err := repo.FindBySlug(context.Background(), "product")
	if err != nil {
		t.Fatalf("FindBySlug after reconcile: %v", err)
	}
	if after.Name() != "Catalog Item" {
		t.Errorf("operator's name was clobbered: got %q", after.Name())
	}
	if after.Description() != "Our own wording" {
		t.Errorf("operator's description was clobbered: got %q", after.Description())
	}
	if _, ok := schemaProps(t, after.Schema())["sku"]; !ok {
		t.Error("the preset's added property did not land")
	}
}

// TestReconcilePresetSchemas_HoldsNonAdditiveChange pins the deferred case: a
// retyped property keeps its stored definition and is reported, while the rest
// of the merge proceeds.
func TestReconcilePresetSchemas_HoldsNonAdditiveChange(t *testing.T) {
	t.Parallel()
	v1 := NewPresetRegistry()
	v1.MustAdd(testPresetSingle(
		"Product", "product", "A product",
		`{"@vocab":"https://schema.org/"}`,
		`{"type":"object","properties":{"sku":{"type":"string"}}}`,
	))
	repo := newInstallTestTypeRepo()
	rSvc := newFakeResourceSvc()
	if _, err := makeInstallTestService(repo, rSvc, v1).
		InstallPreset(context.Background(), "single", false); err != nil {
		t.Fatalf("initial install: %v", err)
	}

	v2 := NewPresetRegistry()
	v2.MustAdd(testPresetSingle(
		"Product", "product", "A product",
		`{"@vocab":"https://schema.org/"}`,
		`{"type":"object","properties":{"sku":{"type":"integer"},"tag":{"type":"string"}}}`,
	))

	d := domain.NewEventDispatcher()
	pm := &stubProjMgr{}
	if err := SubscribeResourceTypeHandlers(d, repo, pm, noopLogger{}); err != nil {
		t.Fatalf("SubscribeResourceTypeHandlers: %v", err)
	}
	updateCount := countingUpdateSub(t, d)
	svc := &resourceTypeService{
		repo: repo, projMgr: pm, eventStore: &stubEventStore{}, dispatcher: d,
		registry: v2, logger: noopLogger{}, resourceSvc: rSvc,
	}

	res, err := svc.ReconcilePresetSchemas(context.Background(), "single")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := res.Refused["product"]; !reflect.DeepEqual(got, []string{"sku"}) {
		t.Fatalf("expected Refused[product]=[sku], got %+v", res)
	}
	if !sameStrings(res.Updated, []string{"product"}) {
		t.Errorf("the additive sibling should still be applied, got Updated=%v", res.Updated)
	}
	if *updateCount != 1 {
		t.Errorf("expected exactly one ResourceTypeUpdated event, got %d", *updateCount)
	}

	// The conflicting property keeps its stored definition even though the
	// additive sibling landed.
	after, err := repo.FindBySlug(context.Background(), "product")
	if err != nil {
		t.Fatalf("FindBySlug: %v", err)
	}
	props := schemaProps(t, after.Schema())
	if _, ok := props["tag"]; !ok {
		t.Error("the additive sibling should have landed alongside the held property")
	}
	var sku struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(props["sku"], &sku); err != nil {
		t.Fatalf("sku is not an object: %v", err)
	}
	if sku.Type != "string" {
		t.Errorf("held property was rewritten: sku.type = %q, want the stored %q", sku.Type, "string")
	}
}

// TestReconcilePresetSchemas_IgnoresUninstalledTypes: creating types is
// InstallPreset's job, so a preset type absent from this database is skipped
// rather than conjured into existence by the reconcile pass.
func TestReconcilePresetSchemas_IgnoresUninstalledTypes(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	registry.MustAdd(testPresetSingle(
		"Product", "product", "A product",
		`{"@vocab":"https://schema.org/"}`,
		`{"type":"object","properties":{"name":{"type":"string"}}}`,
	))
	repo := newInstallTestTypeRepo()
	rSvc := newFakeResourceSvc()
	svc := reconcileTestService(t, repo, rSvc, registry)

	res, err := svc.ReconcilePresetSchemas(context.Background(), "single")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(res.Updated) != 0 || len(res.Unchanged) != 0 || len(res.Refused) != 0 {
		t.Fatalf("expected an empty result for an uninstalled type, got %+v", res)
	}
	if _, err := repo.FindBySlug(context.Background(), "product"); err == nil {
		t.Error("reconcile must not create the type")
	}
}

func TestReconcilePresetSchemas_UnknownPreset(t *testing.T) {
	t.Parallel()
	svc := reconcileTestService(t, newInstallTestTypeRepo(), newFakeResourceSvc(), NewPresetRegistry())
	if _, err := svc.ReconcilePresetSchemas(context.Background(), "nope"); err == nil {
		t.Error("expected an error for an unknown preset")
	}
}

// reconcileTestService builds a service over an existing repo, standing in for
// a restart: the database persists, the process (and its dispatcher) is new.
func reconcileTestService(
	t *testing.T, repo *installTestTypeRepo, rSvc *fakeResourceSvc, registry *PresetRegistry,
) ResourceTypeService {
	t.Helper()
	d := domain.NewEventDispatcher()
	pm := &stubProjMgr{}
	if err := SubscribeResourceTypeHandlers(d, repo, pm, noopLogger{}); err != nil {
		t.Fatalf("SubscribeResourceTypeHandlers: %v", err)
	}
	return &resourceTypeService{
		repo: repo, projMgr: pm, eventStore: &stubEventStore{}, dispatcher: d,
		registry: registry, logger: noopLogger{}, resourceSvc: rSvc,
	}
}

// TestReconcilePresetSchemas_FailsWhenColumnDoesNotLand is the guard against
// announcing success over a silent failure. SimpleUnitOfWork.Commit discards
// dispatch errors, so Update returns nil even when EnsureTable failed and the
// column was never added — reporting that type as reconciled would claim the
// #379 bug is fixed for a type still dropping writes.
func TestReconcilePresetSchemas_FailsWhenColumnDoesNotLand(t *testing.T) {
	t.Parallel()
	v1 := NewPresetRegistry()
	v1.MustAdd(testPresetSingle(
		"Product", "product", "A product",
		`{"@vocab":"https://schema.org/"}`,
		`{"type":"object","properties":{"name":{"type":"string"}}}`,
	))
	repo := newInstallTestTypeRepo()
	rSvc := newFakeResourceSvc()
	if _, err := makeInstallTestService(repo, rSvc, v1).
		InstallPreset(context.Background(), "single", false); err != nil {
		t.Fatalf("initial install: %v", err)
	}

	v2 := NewPresetRegistry()
	v2.MustAdd(testPresetSingle(
		"Product", "product", "A product",
		`{"@vocab":"https://schema.org/"}`,
		`{"type":"object","properties":{"name":{"type":"string"},"sku":{"type":"string"}}}`,
	))

	// A projection manager that refuses to add columns — the ALTER failing, or
	// any other reason EnsureTable errors inside the event handler.
	d := domain.NewEventDispatcher()
	pm := &stubProjMgr{ensureTableErr: errors.New("ALTER TABLE failed")}
	if err := SubscribeResourceTypeHandlers(d, repo, pm, noopLogger{}); err != nil {
		t.Fatalf("SubscribeResourceTypeHandlers: %v", err)
	}
	svc := &resourceTypeService{
		repo: repo, projMgr: pm, eventStore: &stubEventStore{}, dispatcher: d,
		registry: v2, logger: noopLogger{}, resourceSvc: rSvc,
	}

	res, err := svc.ReconcilePresetSchemas(context.Background(), "single")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(res.Updated) != 0 {
		t.Errorf("must not report Updated when the column never landed, got %+v", res.Updated)
	}
	if _, ok := res.Failed["product"]; !ok {
		t.Fatalf("expected product in Failed, got %+v", res)
	}
	if !strings.Contains(res.Failed["product"], "sku") {
		t.Errorf("failure reason should name the missing column, got %q", res.Failed["product"])
	}
}

// TestReconcilePresetSchemas_ContinuesAfterPerTypeFailure: one bad type must
// not leave every later type in the preset unreconciled and silently dropping
// writes.
func TestReconcilePresetSchemas_ContinuesAfterPerTypeFailure(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	registry.MustAdd(PresetDefinition{
		Name: "single",
		Types: []PresetResourceType{
			{
				Name: "Broken", Slug: "broken", Description: "d",
				Context: json.RawMessage(`{"@vocab":"https://schema.org/"}`),
				Schema:  json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}}}`),
			},
			{
				Name: "Healthy", Slug: "healthy", Description: "d",
				Context: json.RawMessage(`{"@vocab":"https://schema.org/"}`),
				Schema:  json.RawMessage(`{"type":"object","properties":{"b":{"type":"string"}}}`),
			},
		},
	})
	repo := newInstallTestTypeRepo()
	rSvc := newFakeResourceSvc()
	svc := makeInstallTestService(repo, rSvc, registry)
	if _, err := svc.InstallPreset(context.Background(), "single", false); err != nil {
		t.Fatalf("initial install: %v", err)
	}

	// Corrupt the first type's stored schema so its reconcile errors.
	broken, err := repo.FindBySlug(context.Background(), "broken")
	if err != nil {
		t.Fatalf("FindBySlug: %v", err)
	}
	if err := broken.Restore(broken.GetID(), broken.Name(), "broken", broken.Description(),
		broken.Status(), broken.Context(), json.RawMessage(`"not an object"`),
		broken.CreatedAt(), 2); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if err := repo.Update(context.Background(), broken); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// A later build adds a property to BOTH types.
	v2 := NewPresetRegistry()
	v2.MustAdd(PresetDefinition{
		Name: "single",
		Types: []PresetResourceType{
			{
				Name: "Broken", Slug: "broken", Description: "d",
				Context: json.RawMessage(`{"@vocab":"https://schema.org/"}`),
				Schema:  json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"},"a2":{"type":"string"}}}`),
			},
			{
				Name: "Healthy", Slug: "healthy", Description: "d",
				Context: json.RawMessage(`{"@vocab":"https://schema.org/"}`),
				Schema:  json.RawMessage(`{"type":"object","properties":{"b":{"type":"string"},"b2":{"type":"string"}}}`),
			},
		},
	})

	res, err := reconcileTestService(t, repo, rSvc, v2).
		ReconcilePresetSchemas(context.Background(), "single")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if _, ok := res.Failed["broken"]; !ok {
		t.Errorf("expected broken in Failed, got %+v", res)
	}
	if !sameStrings(res.Updated, []string{"healthy"}) {
		t.Fatalf("the type after the failing one must still be reconciled, got Updated=%v", res.Updated)
	}
}

// TestReconcilePresetSchemas_ReportsSchemaLessType: a preset type with no schema
// projects only base columns, so every field is dropped on write. It must not
// be filed as a healthy no-op.
func TestReconcilePresetSchemas_ReportsSchemaLessType(t *testing.T) {
	t.Parallel()
	registry := NewPresetRegistry()
	registry.MustAdd(testPresetSingle(
		"Product", "product", "A product", `{"@vocab":"https://schema.org/"}`, ``,
	))
	repo := newInstallTestTypeRepo()
	rSvc := newFakeResourceSvc()
	svc := makeInstallTestService(repo, rSvc, registry)
	if _, err := svc.InstallPreset(context.Background(), "single", false); err != nil {
		t.Fatalf("initial install: %v", err)
	}

	res, err := reconcileTestService(t, repo, rSvc, registry).
		ReconcilePresetSchemas(context.Background(), "single")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !sameStrings(res.NoSchema, []string{"product"}) {
		t.Fatalf("expected NoSchema=[product], got %+v", res)
	}
	if len(res.Unchanged) != 0 {
		t.Errorf("a schema-less type must not be filed as Unchanged, got %v", res.Unchanged)
	}
}

// TestReconcilePresetSchemas_HeldPropertyDoesNotBlockAdditions: refusal is
// per-property, so a conflicting definition is held at its stored form while
// every additive property in the same schema still lands. Under the old
// all-or-nothing rule these additions were blocked and kept losing writes.
func TestReconcilePresetSchemas_HeldPropertyDoesNotBlockAdditions(t *testing.T) {
	t.Parallel()
	v1 := NewPresetRegistry()
	v1.MustAdd(testPresetSingle(
		"Product", "product", "A product",
		`{"@vocab":"https://schema.org/"}`,
		`{"type":"object","properties":{"sku":{"type":"string"}}}`,
	))
	repo := newInstallTestTypeRepo()
	rSvc := newFakeResourceSvc()
	if _, err := makeInstallTestService(repo, rSvc, v1).
		InstallPreset(context.Background(), "single", false); err != nil {
		t.Fatalf("initial install: %v", err)
	}

	v2 := NewPresetRegistry()
	v2.MustAdd(testPresetSingle(
		"Product", "product", "A product",
		`{"@vocab":"https://schema.org/"}`,
		`{"type":"object","properties":{"sku":{"type":"integer"},"gtin":{"type":"string"},"brand":{"type":"string"}}}`,
	))

	res, err := reconcileTestService(t, repo, rSvc, v2).
		ReconcilePresetSchemas(context.Background(), "single")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !sameStrings(res.Refused["product"], []string{"sku"}) {
		t.Fatalf("expected Refused[product]=[sku], got %+v", res.Refused)
	}
	if !sameStrings(res.Updated, []string{"product"}) {
		t.Fatalf("the additive properties should still have landed, got Updated=%v", res.Updated)
	}

	after, err := repo.FindBySlug(context.Background(), "product")
	if err != nil {
		t.Fatalf("FindBySlug: %v", err)
	}
	props := schemaProps(t, after.Schema())
	for _, name := range []string{"gtin", "brand"} {
		if _, ok := props[name]; !ok {
			t.Errorf("additive property %q did not land (have %v)", name, sortedKeys(props))
		}
	}
	// ...and the conflicting one is still the STORED definition, not the preset's.
	var sku struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(props["sku"], &sku); err != nil {
		t.Fatalf("sku is not an object: %v", err)
	}
	if sku.Type != "string" {
		t.Errorf("held property was rewritten: sku.type = %q, want the stored %q", sku.Type, "string")
	}
}

// catalogPreset builds a preset whose single type carries the given schema.
func catalogPreset(autoInstall bool, schema string) PresetDefinition {
	return PresetDefinition{
		Name:        "single",
		AutoInstall: autoInstall,
		Types: []PresetResourceType{{
			Name: "Product", Slug: "product", Description: "A product",
			Context: json.RawMessage(`{"@vocab":"https://schema.org/"}`),
			Schema:  json.RawMessage(schema),
		}},
	}
}

// TestEnsureBuiltInResourceTypes_ReconcilesNonAutoInstallPresets is the
// regression guard for the scope of the fix. Only two presets in the tree set
// AutoInstall, so gating the reconcile on that flag left issue #379 unfixed for
// every preset an operator installs on demand — including any from a private
// registrar. Moving the reconcile back under the AutoInstall guard must fail
// this test.
func TestEnsureBuiltInResourceTypes_ReconcilesNonAutoInstallPresets(t *testing.T) {
	t.Parallel()
	v1 := NewPresetRegistry()
	v1.MustAdd(catalogPreset(false, `{"type":"object","properties":{"name":{"type":"string"}}}`))

	repo := newInstallTestTypeRepo()
	rSvc := newFakeResourceSvc()
	// The operator installed it explicitly at some earlier point.
	if _, err := makeInstallTestService(repo, rSvc, v1).
		InstallPreset(context.Background(), "single", false); err != nil {
		t.Fatalf("initial install: %v", err)
	}

	// A later build adds a property. The preset is still NOT auto-install.
	v2 := NewPresetRegistry()
	v2.MustAdd(catalogPreset(false,
		`{"type":"object","properties":{"name":{"type":"string"},"sku":{"type":"string"}}}`))

	err := ensureBuiltInResourceTypes(struct {
		fx.In
		Registry      *PresetRegistry
		TypeSvc       ResourceTypeService
		Logger        entities.Logger
		LinkActivator *LinkActivator `optional:"true"`
		Links         *LinkRegistry  `optional:"true"`
	}{
		Registry: v2,
		TypeSvc:  reconcileTestService(t, repo, rSvc, v2),
		Logger:   noopLogger{},
	})
	if err != nil {
		t.Fatalf("ensureBuiltInResourceTypes: %v", err)
	}

	after, aErr := repo.FindBySlug(context.Background(), "product")
	if aErr != nil {
		t.Fatalf("FindBySlug: %v", aErr)
	}
	if _, ok := schemaProps(t, after.Schema())["sku"]; !ok {
		t.Fatal("a non-AutoInstall preset's added property never reached the stored schema")
	}
}

// TestReconcilePresetSchemas_HeldTypeSettlesAcrossRestarts: a type carrying a
// held property must still go quiet after its additive changes land. Without
// this, a schema stuck in permanent divergence would emit a ResourceTypeUpdated
// on every boot forever — the failure the clean path already guards against.
func TestReconcilePresetSchemas_HeldTypeSettlesAcrossRestarts(t *testing.T) {
	t.Parallel()
	v1 := NewPresetRegistry()
	v1.MustAdd(catalogPreset(true, `{"type":"object","properties":{"sku":{"type":"string"}}}`))
	repo := newInstallTestTypeRepo()
	rSvc := newFakeResourceSvc()
	if _, err := makeInstallTestService(repo, rSvc, v1).
		InstallPreset(context.Background(), "single", false); err != nil {
		t.Fatalf("initial install: %v", err)
	}

	v2 := NewPresetRegistry()
	v2.MustAdd(catalogPreset(true,
		`{"type":"object","properties":{"sku":{"type":"integer"},"gtin":{"type":"string"}}}`))

	d := domain.NewEventDispatcher()
	pm := &stubProjMgr{}
	if err := SubscribeResourceTypeHandlers(d, repo, pm, noopLogger{}); err != nil {
		t.Fatalf("SubscribeResourceTypeHandlers: %v", err)
	}
	updateCount := countingUpdateSub(t, d)
	svc := &resourceTypeService{
		repo: repo, projMgr: pm, eventStore: &stubEventStore{}, dispatcher: d,
		registry: v2, logger: noopLogger{}, resourceSvc: rSvc,
	}

	for i := 0; i < 3; i++ {
		res, err := svc.ReconcilePresetSchemas(context.Background(), "single")
		if err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
		// The hold is reported every boot — it stays unresolved until an
		// operator acts — but it must not keep rewriting the schema.
		if !sameStrings(res.Refused["product"], []string{"sku"}) {
			t.Fatalf("boot %d: expected sku held, got %+v", i, res.Refused)
		}
		if i > 0 && len(res.Updated) != 0 {
			t.Fatalf("boot %d rewrote the schema again: %+v", i, res.Updated)
		}
	}
	if *updateCount != 1 {
		t.Fatalf("expected exactly one ResourceTypeUpdated across three boots, got %d", *updateCount)
	}
}

// TestReconcilePresetSchemas_IgnoresColumnlessProperties: some schema property
// names deliberately yield no projection column of their own. Reporting them as
// missing would raise a false alarm announcing data loss that isn't happening.
func TestReconcilePresetSchemas_IgnoresColumnlessProperties(t *testing.T) {
	t.Parallel()
	v1 := NewPresetRegistry()
	v1.MustAdd(catalogPreset(true, `{"type":"object","properties":{"name":{"type":"string"}}}`))
	repo := newInstallTestTypeRepo()
	rSvc := newFakeResourceSvc()
	if _, err := makeInstallTestService(repo, rSvc, v1).
		InstallPreset(context.Background(), "single", false); err != nil {
		t.Fatalf("initial install: %v", err)
	}

	// A later build adds properties the projection deliberately has no column
	// for: the canonical data blob and a JSON-LD meta-key.
	v2 := NewPresetRegistry()
	v2.MustAdd(catalogPreset(true,
		`{"type":"object","properties":{"name":{"type":"string"},"data":{"type":"string"},"@id":{"type":"string"}}}`))

	res, err := reconcileTestService(t, repo, rSvc, v2).
		ReconcilePresetSchemas(context.Background(), "single")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("columnless properties were reported as a failure: %+v", res.Failed)
	}
	if !sameStrings(res.Updated, []string{"product"}) {
		t.Errorf("expected Updated=[product], got %+v", res)
	}
}
