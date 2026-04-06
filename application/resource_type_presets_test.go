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

package application_test

import (
	"encoding/json"
	"io/fs"
	"testing"
	"testing/fstest"

	"weos/application"
	"weos/application/presets"
	"weos/domain/entities"
)

func testRegistry() *application.PresetRegistry {
	return presets.NewDefaultRegistry()
}

func TestPresets_AllPresetsExist(t *testing.T) {
	t.Parallel()
	expected := []string{"auth", "core", "ecommerce", "events", "knowledge", "tasks", "website"}
	defs := testRegistry().List()
	if len(defs) != len(expected) {
		t.Fatalf("expected %d presets, got %d", len(expected), len(defs))
	}
	for i, d := range defs {
		if d.Name != expected[i] {
			t.Fatalf("preset[%d] = %q, want %q", i, d.Name, expected[i])
		}
		if len(d.Types) == 0 {
			t.Fatalf("preset %q has no types", d.Name)
		}
	}
}

func TestPresets_NoDuplicateSlugsAcrossPresets(t *testing.T) {
	t.Parallel()
	seen := make(map[string]string)
	for _, d := range testRegistry().List() {
		for _, pt := range d.Types {
			if prev, ok := seen[pt.Slug]; ok {
				t.Fatalf("duplicate slug %q in presets %q and %q", pt.Slug, prev, d.Name)
			}
			seen[pt.Slug] = d.Name
		}
	}
}

func TestPresets_AllContextsAreValidJSON(t *testing.T) {
	t.Parallel()
	for _, d := range testRegistry().List() {
		for _, pt := range d.Types {
			if len(pt.Context) == 0 {
				continue
			}
			var v any
			if err := json.Unmarshal(pt.Context, &v); err != nil {
				t.Fatalf("preset %q type %q has invalid JSON context: %v", d.Name, pt.Slug, err)
			}
		}
	}
}

func TestPresets_AllSchemasAreValidJSON(t *testing.T) {
	t.Parallel()
	for _, d := range testRegistry().List() {
		for _, pt := range d.Types {
			if len(pt.Schema) == 0 {
				continue
			}
			var v any
			if err := json.Unmarshal(pt.Schema, &v); err != nil {
				t.Fatalf("preset %q type %q has invalid JSON schema: %v", d.Name, pt.Slug, err)
			}
		}
	}
}

func TestPresets_GetFound(t *testing.T) {
	t.Parallel()
	d, ok := testRegistry().Get("website")
	if !ok {
		t.Fatal("expected to find 'website' preset")
	}
	if d.Name != "website" {
		t.Fatalf("got name %q, want %q", d.Name, "website")
	}
}

func TestPresets_GetNotFound(t *testing.T) {
	t.Parallel()
	_, ok := testRegistry().Get("nonexistent")
	if ok {
		t.Fatal("expected not to find 'nonexistent' preset")
	}
}

func TestPresets_AllTypesHaveRequiredFields(t *testing.T) {
	t.Parallel()
	for _, d := range testRegistry().List() {
		for _, pt := range d.Types {
			if pt.Name == "" {
				t.Fatalf("preset %q has a type with empty name", d.Name)
			}
			if pt.Slug == "" {
				t.Fatalf("preset %q type %q has empty slug", d.Name, pt.Name)
			}
			if pt.Description == "" {
				t.Fatalf("preset %q type %q has empty description", d.Name, pt.Slug)
			}
		}
	}
}

func TestPresets_CoreHasBehaviors(t *testing.T) {
	t.Parallel()
	d, ok := testRegistry().Get("core")
	if !ok {
		t.Fatal("expected to find 'core' preset")
	}
	if d.Behaviors == nil {
		t.Fatal("core preset should have behaviors")
	}
	if _, ok := d.Behaviors["person"]; !ok {
		t.Fatal("core preset should have 'person' behavior")
	}
	if _, ok := d.Behaviors["organization"]; !ok {
		t.Fatal("core preset should have 'organization' behavior")
	}
}

func TestPresets_BehaviorsRegistry(t *testing.T) {
	t.Parallel()
	behaviors := testRegistry().Behaviors()
	if _, ok := behaviors["person"]; !ok {
		t.Fatal("merged behaviors should include 'person'")
	}
	if _, ok := behaviors["organization"]; !ok {
		t.Fatal("merged behaviors should include 'organization'")
	}
}

func TestPresets_AutoInstallFlag(t *testing.T) {
	t.Parallel()
	for _, d := range testRegistry().List() {
		switch d.Name {
		case "core", "auth":
			if !d.AutoInstall {
				t.Fatalf("preset %q should be marked as AutoInstall", d.Name)
			}
		default:
			if d.AutoInstall {
				t.Fatalf("preset %q should NOT be marked as AutoInstall", d.Name)
			}
		}
	}
}

func TestPresets_NoReservedSlugCollisions(t *testing.T) {
	t.Parallel()
	reserved := application.ReservedResourceTypeSlugs()
	for _, d := range testRegistry().List() {
		for _, pt := range d.Types {
			if reserved[pt.Slug] {
				t.Fatalf("preset %q type slug %q collides with reserved API route", d.Name, pt.Slug)
			}
		}
	}
}

func TestPresets_AllFixturesAreValidJSON(t *testing.T) {
	t.Parallel()
	for _, d := range testRegistry().List() {
		for _, pt := range d.Types {
			for i, fixture := range pt.Fixtures {
				var v any
				if err := json.Unmarshal(fixture, &v); err != nil {
					t.Fatalf("preset %q type %q fixture[%d] is invalid JSON: %v",
						d.Name, pt.Slug, i, err)
				}
				// Fixtures must be JSON objects, not arrays or scalars.
				if _, ok := v.(map[string]any); !ok {
					t.Fatalf("preset %q type %q fixture[%d] must be a JSON object, got %T",
						d.Name, pt.Slug, i, v)
				}
			}
		}
	}
}

func TestPresets_FixturesOnlyOnTypesWithSchema(t *testing.T) {
	t.Parallel()
	for _, d := range testRegistry().List() {
		for _, pt := range d.Types {
			if len(pt.Fixtures) > 0 && len(pt.Schema) == 0 {
				t.Fatalf("preset %q type %q has fixtures but no schema — "+
					"fixtures require a schema for validation", d.Name, pt.Slug)
			}
		}
	}
}

func TestScreenManifest_NilScreens(t *testing.T) {
	t.Parallel()
	def := application.PresetDefinition{Name: "test"}
	manifest := def.ScreenManifest()
	if manifest != nil {
		t.Fatalf("expected nil manifest for nil Screens, got %v", manifest)
	}
}

func TestScreenManifest_EmptyFS(t *testing.T) {
	t.Parallel()
	emptyFS := fstest.MapFS{}
	def := application.PresetDefinition{Name: "test", Screens: emptyFS}
	manifest := def.ScreenManifest()
	if manifest != nil {
		t.Fatalf("expected nil manifest for empty FS, got %v", manifest)
	}
}

func TestScreenManifest_MultipleTypeSlugs(t *testing.T) {
	t.Parallel()
	// FS is expected to be rooted at the type-slug level (preset uses fs.Sub).
	testFS := fstest.MapFS{
		"task/Checklist.mjs":   {Data: []byte("export default {}")},
		"task/KanbanBoard.mjs": {Data: []byte("export default {}")},
		"project/Timeline.mjs": {Data: []byte("export default {}")},
		"project/readme.txt":   {Data: []byte("not a screen")},
	}
	def := application.PresetDefinition{Name: "test", Screens: testFS}
	manifest := def.ScreenManifest()

	if len(manifest) != 2 {
		t.Fatalf("expected 2 type slugs, got %d: %v", len(manifest), manifest)
	}
	taskFiles := manifest["task"]
	if len(taskFiles) != 2 {
		t.Fatalf("expected 2 task screens, got %d", len(taskFiles))
	}
	projectFiles := manifest["project"]
	if len(projectFiles) != 1 {
		t.Fatalf("expected 1 project screen, got %d", len(projectFiles))
	}
	if projectFiles[0] != "Timeline.mjs" {
		t.Fatalf("expected Timeline.mjs, got %s", projectFiles[0])
	}
}

func TestScreenManifest_RootLevelFilesIgnored(t *testing.T) {
	t.Parallel()
	testFS := fstest.MapFS{
		"orphan.mjs": {Data: []byte("export default {}")},
	}
	def := application.PresetDefinition{Name: "test", Screens: testFS}
	manifest := def.ScreenManifest()
	if manifest != nil {
		t.Fatalf("expected nil manifest for root-level files only, got %v", manifest)
	}
}

func TestPresets_TasksHasScreens(t *testing.T) {
	t.Parallel()
	d, ok := testRegistry().Get("tasks")
	if !ok {
		t.Fatal("expected to find 'tasks' preset")
	}
	if d.Screens == nil {
		t.Fatal("tasks preset should have Screens FS")
	}
	manifest := d.ScreenManifest()
	if manifest == nil {
		t.Fatal("tasks preset should have non-nil screen manifest")
	}
	taskScreens, ok := manifest["task"]
	if !ok {
		t.Fatal("expected task screens in manifest")
	}
	found := false
	for _, f := range taskScreens {
		if f == "Checklist.mjs" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Checklist.mjs in task screens, got %v", taskScreens)
	}

	// Verify the file is actually readable from the FS (rooted at screens/dist/).
	f, err := d.Screens.Open("task/Checklist.mjs")
	if err != nil {
		t.Fatalf("failed to open Checklist.mjs from Screens FS: %v", err)
	}
	_ = f.Close()
}

func TestPresets_PresetsWithoutScreensHaveNilManifest(t *testing.T) {
	t.Parallel()
	for _, d := range testRegistry().List() {
		if d.Name == "tasks" {
			continue // tasks has screens
		}
		manifest := d.ScreenManifest()
		if manifest != nil {
			t.Fatalf("preset %q should have nil screen manifest, got %v", d.Name, manifest)
		}
	}
}

func TestScreenManifest_NestedPathsIgnored(t *testing.T) {
	t.Parallel()
	testFS := fstest.MapFS{
		"task/List.mjs":       {Data: []byte("export default {}")},
		"task/sub/Nested.mjs": {Data: []byte("export default {}")},
		"task/a/b/c/Deep.mjs": {Data: []byte("export default {}")},
	}
	def := application.PresetDefinition{Name: "test", Screens: testFS}
	manifest := def.ScreenManifest()
	if len(manifest["task"]) != 1 {
		t.Fatalf("expected 1 screen (nested ignored), got %d: %v", len(manifest["task"]), manifest["task"])
	}
	if manifest["task"][0] != "List.mjs" {
		t.Fatalf("expected List.mjs, got %s", manifest["task"][0])
	}
}

func TestScreenManifest_NonMjsFilesIgnored(t *testing.T) {
	t.Parallel()
	testFS := fstest.MapFS{
		"task/List.mjs":   {Data: []byte("export default {}")},
		"task/styles.css": {Data: []byte(".foo{}")},
		"task/README.md":  {Data: []byte("readme")},
	}
	def := application.PresetDefinition{Name: "test", Screens: testFS}
	manifest := def.ScreenManifest()
	if len(manifest["task"]) != 1 {
		t.Fatalf("expected 1 .mjs file, got %d: %v", len(manifest["task"]), manifest["task"])
	}
}

// Ensure the ScreenManifest method works with fs.Sub (simulating how presets
// strip the screens/dist/ prefix before storing the FS).
func TestScreenManifest_WithSubFS(t *testing.T) {
	t.Parallel()
	base := fstest.MapFS{
		"screens/dist/widget/Dashboard.mjs": {Data: []byte("export default {}")},
	}
	sub, err := fs.Sub(base, "screens/dist")
	if err != nil {
		t.Fatal(err)
	}
	def := application.PresetDefinition{Name: "test", Screens: sub}
	manifest := def.ScreenManifest()
	if manifest == nil {
		t.Fatal("expected non-nil manifest")
	}
	if len(manifest["widget"]) != 1 {
		t.Fatalf("expected 1 widget screen, got %v", manifest)
	}
}

func TestPresets_CoreHasBehaviorMeta(t *testing.T) {
	t.Parallel()
	d, ok := testRegistry().Get("core")
	if !ok {
		t.Fatal("expected to find 'core' preset")
	}
	if d.BehaviorMeta == nil {
		t.Fatal("core preset should have BehaviorMeta")
	}
	pm, ok := d.BehaviorMeta["person"]
	if !ok {
		t.Fatal("core preset should have 'person' behavior metadata")
	}
	if pm.Slug != "person" {
		t.Fatalf("expected slug 'person', got %q", pm.Slug)
	}
	if pm.DisplayName == "" {
		t.Fatal("person behavior meta should have a display name")
	}
	if !pm.Default {
		t.Fatal("person behavior should be default-enabled")
	}
	if pm.Manageable {
		t.Fatal("person behavior should not be user-manageable")
	}
}

func TestPresets_BehaviorsMetaRegistry(t *testing.T) {
	t.Parallel()
	meta := testRegistry().BehaviorsMeta()
	if _, ok := meta["person"]; !ok {
		t.Fatal("merged behavior meta should include 'person'")
	}
	if _, ok := meta["organization"]; !ok {
		t.Fatal("merged behavior meta should include 'organization'")
	}
}

func TestPresets_BehaviorMetaClone(t *testing.T) {
	t.Parallel()
	d, ok := testRegistry().Get("core")
	if !ok {
		t.Fatal("expected to find 'core' preset")
	}
	// Mutating the clone should not affect the registry.
	d.BehaviorMeta["person"] = entities.BehaviorMeta{Slug: "mutated"}
	d2, _ := testRegistry().Get("core")
	if d2.BehaviorMeta["person"].Slug == "mutated" {
		t.Fatal("clone mutation leaked into registry")
	}
}

func TestPresets_BehaviorMetaKeysMatchBehaviors(t *testing.T) {
	t.Parallel()
	for _, d := range testRegistry().List() {
		for slug := range d.BehaviorMeta {
			if _, ok := d.Behaviors[slug]; !ok {
				t.Fatalf("preset %q has BehaviorMeta for %q but no matching Behaviors entry",
					d.Name, slug)
			}
		}
	}
}
