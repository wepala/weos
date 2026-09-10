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
	"sort"
	"strings"
	"testing"

	"github.com/cucumber/godog"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
)

// Story wm-kb6sg.1: mini-me's ten food types move into core's meal-planning
// preset. The world is #521's real-preset world. "The build whose food preset
// carries mini-me's food definitions" is the default registry with those ten
// slugs taken out of meal-planning and put back under their own preset, built
// from the golden copy the unit test pins. The boot reconcile is additive, so a
// restart here fails only when core adds a term or property the golden lacks,
// or maps a term to another IRI. A term core drops, a re-ordered key, or a
// changed name or description passes; the unit byte test catches everything
// else.

func TestFoodTypes(t *testing.T) {
	runFeatureWith(t, "food-types", "features/food_types.feature", initFoodTypesScenario)
}

const miniMeFoodGoldenPath = "../../application/presets/mealplanning/testdata/mini_me_food_types.golden.json"

type foodTypesWorld struct {
	*classWorld
	listed []application.PresetDefinition
	// offered is the slug set the last "offers exactly" step named, and
	// offeredBy the preset it named them for.
	offered   map[string]bool
	offeredBy string
}

func initFoodTypesScenario(sc *godog.ScenarioContext) {
	w := &foodTypesWorld{classWorld: &classWorld{vocabWorld: newVocabWorld()}}
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})
	w.registerVocabSteps(sc)
	w.registerVocabGuardSteps(sc)

	sc.Step(`^the "([^"]*)" type advertises the RDF class "([^"]*)"$`, w.typeAdvertisesClass)
	sc.Step(`^no resource type update is recorded for "([^"]*)"$`, w.noUpdateRecorded)
	sc.Step(`^the boot reconcile does not report "([^"]*)" as updated$`, w.bootDoesNotReportUpdated)

	sc.Step(`^the operator lists the built-in presets$`, w.listBuiltInPresets)
	sc.Step(`^the "([^"]*)" preset offers exactly these types:$`, w.presetOffersExactly)
	sc.Step(`^no other built-in preset offers any of those types$`, w.noOtherPresetOffers)
	sc.Step(`^a WeOS database provisioned by the build whose "([^"]*)" preset carries mini-me's food definitions `+
		`at commit "([^"]*)"$`, w.aDatabaseFromTheMiniMeFoodBuild)
	sc.Step(`^the twin restarts on the build that moves the food definitions into meal-planning$`, w.restartOnThisBuild)
}

// --- the listing ---

// listBuiltInPresets reads the registry `weos resource-type preset list` and
// GET /api/resource-types/presets both serve: ListPresets returns the
// registry's List(), and the binary's registry is the default one.
func (w *foodTypesWorld) listBuiltInPresets() error {
	w.listed = presets.NewDefaultRegistry().List()
	if len(w.listed) == 0 {
		return fmt.Errorf("the built-in registry lists no presets")
	}
	return nil
}

func (w *foodTypesWorld) presetOffersExactly(name string, table *godog.Table) error {
	if w.listed == nil {
		return fmt.Errorf("the built-in presets have not been listed in this scenario")
	}
	want := map[string]bool{}
	for _, row := range table.Rows {
		if len(row.Cells) != 1 {
			return fmt.Errorf("expected one slug per row, got %d cells", len(row.Cells))
		}
		want[strings.TrimSpace(row.Cells[0].Value)] = true
	}
	var def *application.PresetDefinition
	for i := range w.listed {
		if w.listed[i].Name == name {
			def = &w.listed[i]
			break
		}
	}
	if def == nil {
		return fmt.Errorf("the listing has no %q preset", name)
	}
	got := map[string]bool{}
	for _, pt := range def.Types {
		got[pt.Slug] = true
	}
	var missing, extra []string
	for slug := range want {
		if !got[slug] {
			missing = append(missing, slug)
		}
	}
	for slug := range got {
		if !want[slug] {
			extra = append(extra, slug)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		return fmt.Errorf("the %q preset does not offer exactly the listed types: missing %v, extra %v", name, missing, extra)
	}
	w.offered, w.offeredBy = want, name
	return nil
}

func (w *foodTypesWorld) noOtherPresetOffers() error {
	if len(w.offered) == 0 {
		return fmt.Errorf("no preset's types have been named in this scenario")
	}
	var clashes []string
	for _, def := range w.listed {
		if def.Name == w.offeredBy {
			continue
		}
		for _, pt := range def.Types {
			if w.offered[pt.Slug] {
				clashes = append(clashes, def.Name+" offers "+pt.Slug)
			}
		}
	}
	if len(clashes) > 0 {
		sort.Strings(clashes)
		return fmt.Errorf("other built-in presets offer %q types: %v", w.offeredBy, clashes)
	}
	return nil
}

// --- the build before the move ---

type miniMeFoodGolden struct {
	Source struct {
		Commit string `json:"commit"`
	} `json:"source"`
	Types []struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
		Context     string `json:"context"`
		Schema      string `json:"schema"`
	} `json:"types"`
}

// miniMeFoodBuild refuses to build when it would be the current build in
// disguise: a golden copy from another commit, one that does not hold the ten
// types, or a meal-planning preset that does not offer all ten to take out.
func miniMeFoodBuild(preset, commit string) (*application.PresetRegistry, error) {
	raw, err := os.ReadFile(miniMeFoodGoldenPath)
	if err != nil {
		return nil, fmt.Errorf("read the golden copy of mini-me's food types: %w", err)
	}
	var golden miniMeFoodGolden
	if err := json.Unmarshal(raw, &golden); err != nil {
		return nil, fmt.Errorf("decode the golden copy of mini-me's food types: %w", err)
	}
	if golden.Source.Commit != commit {
		return nil, fmt.Errorf("the golden copy was taken at mini-me commit %q, not %q", golden.Source.Commit, commit)
	}
	if len(golden.Types) != 10 {
		return nil, fmt.Errorf("the golden copy holds %d food types, want 10", len(golden.Types))
	}
	moved := map[string]bool{}
	types := make([]application.PresetResourceType, 0, len(golden.Types))
	for _, g := range golden.Types {
		moved[g.Slug] = true
		types = append(types, application.PresetResourceType{
			Name:        g.Name,
			Slug:        g.Slug,
			Description: g.Description,
			Context:     json.RawMessage(g.Context),
			Schema:      json.RawMessage(g.Schema),
		})
	}

	out := application.NewPresetRegistry()
	taken := map[string]bool{}
	for _, def := range presets.NewDefaultRegistry().List() {
		if def.Name == preset {
			return nil, fmt.Errorf("the current build already registers a %q preset", preset)
		}
		if def.Name == "meal-planning" {
			kept := make([]application.PresetResourceType, 0, len(def.Types))
			for _, pt := range def.Types {
				if moved[pt.Slug] {
					taken[pt.Slug] = true
					continue
				}
				kept = append(kept, pt)
			}
			def.Types = kept
		}
		if err := out.Add(def); err != nil {
			return nil, err
		}
	}
	if len(taken) != len(moved) {
		var missing []string
		for slug := range moved {
			if !taken[slug] {
				missing = append(missing, slug)
			}
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("meal-planning does not offer the golden food types %v", missing)
	}
	if err := out.Add(application.PresetDefinition{
		Name:        preset,
		Description: "mini-me's food types as its build at commit " + commit + " declares them",
		Types:       types,
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func (w *foodTypesWorld) aDatabaseFromTheMiniMeFoodBuild(preset, commit string) error {
	if _, err := miniMeFoodBuild(preset, commit); err != nil {
		return err
	}
	w.registry = func() *application.PresetRegistry {
		reg, err := miniMeFoodBuild(preset, commit)
		if err != nil {
			panic(err)
		}
		return reg
	}
	return w.provision()
}
