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
	"reflect"
	"testing"

	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
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
		name:          "a redefined property is refused even when a sibling is additive",
		stored:        `{"type":"object","properties":{"sku":{"type":"string"}}}`,
		preset:        `{"type":"object","properties":{"sku":{"type":"string","maxLength":8},"tag":{"type":"string"}}}`,
		wantChanged:   false,
		wantConflicts: []string{"sku"},
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
			// A refused reconcile must never hand back a schema a caller could
			// persist by mistake.
			if len(tc.wantConflicts) > 0 && got.Schema != nil {
				t.Errorf("refused reconcile returned a schema: %s", got.Schema)
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

// TestReconcilePresetSchemas_RefusesNonAdditiveChange pins the deferred case:
// a retyped property leaves the stored type entirely alone and is reported.
func TestReconcilePresetSchemas_RefusesNonAdditiveChange(t *testing.T) {
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
	if len(res.Updated) != 0 {
		t.Errorf("a refused type must not be updated, got %+v", res.Updated)
	}
	if *updateCount != 0 {
		t.Errorf("a refused type must emit no event, got %d", *updateCount)
	}

	// The additive sibling must NOT have been smuggled in.
	after, err := repo.FindBySlug(context.Background(), "product")
	if err != nil {
		t.Fatalf("FindBySlug: %v", err)
	}
	if _, ok := schemaProps(t, after.Schema())["tag"]; ok {
		t.Error("a refused reconcile partially applied the preset")
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
