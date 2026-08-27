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
	"sort"
	"strings"
	"testing"

	"github.com/cucumber/godog"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
)

// Issue #535: every meal-planning house property must state a predicate the
// vocabulary it names actually defines. The world is #520's real-preset world
// (vocabWorld); "the build before the house properties were termed" is the
// default registry with the terms this story added reverted — the real preset,
// edited, so it cannot drift.

func TestMealPlanningHouseTerms(t *testing.T) {
	runFeatureWith(t, "meal-planning-house-terms",
		"features/meal_planning_house_terms.feature", initMealPlanningHouseTermsScenario)
}

// mealTermsWorld is the real-preset world plus the violations a scenario has
// already accounted for, so "the guard names no other property" can be an
// exact-set assertion rather than a count.
type mealTermsWorld struct {
	*vocabWorld
	guardNamed map[string]bool
}

func initMealPlanningHouseTermsScenario(sc *godog.ScenarioContext) {
	w := &mealTermsWorld{vocabWorld: newVocabWorld(), guardNamed: map[string]bool{}}
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})
	w.registerVocabSteps(sc)
	w.registerMealTermSteps(sc)
}

func (w *mealTermsWorld) registerMealTermSteps(sc *godog.ScenarioContext) {
	// --- what an installed type resolves ---
	sc.Step(`^the "([^"]*)" type resolves the property "([^"]*)" to "([^"]*)"$`, w.typeResolvesProperty)
	sc.Step(`^the "([^"]*)" type resolves nothing to "([^"]*)"$`, w.typeResolvesNothingTo)

	// --- the guard, swept over what is actually installed ---
	sc.Step(`^no property of an installed meal-planning type resolves to a term its vocabulary does not define$`,
		w.noUndefinedPublishedTerm)
	sc.Step(`^no property of an installed meal-planning type resolves to a term a published vocabulary `+
		`defines for another subject$`, w.noTermForAnotherSubject)
	sc.Step(`^the "([^"]*)" preset adds an untermed "([^"]*)" string property to "([^"]*)"$`,
		w.presetAddsUntermedProperty)
	sc.Step(`^the vocabulary guard names "([^"]*)" "([^"]*)" resolving to "([^"]*)"$`, w.guardNamesResolving)
	sc.Step(`^the vocabulary guard names "([^"]*)" "([^"]*)" as a term "([^"]*)" defines for another subject$`,
		w.guardNamesOtherSubject)
	sc.Step(`^the vocabulary guard names no other property of an installed meal-planning type$`,
		w.guardNamesNoOtherProperty)

	// --- the builds either side of the story ---
	sc.Step(`^a WeOS database provisioned by the build before the house properties were termed$`,
		w.aDatabaseFromBeforeTheHouseTerms)
	sc.Step(`^the twin restarts on the build that terms the house properties(?: again)?$`, w.restartOnThisBuild)
}

// --- resolving against the INSTALLED types --------------------------------

// installedPresetType reads a type back out of the database and presents it in
// the shape the guard takes. The scenarios say "an installed meal-planning
// type", so every resolution below is judged against what the install actually
// stored rather than against the registry — a broken install would otherwise
// pass on the registry's good intentions.
func (w *mealTermsWorld) installedPresetType(slug string) (application.PresetResourceType, error) {
	rt, err := w.rts.GetBySlug(context.Background(), slug)
	if err != nil {
		return application.PresetResourceType{}, fmt.Errorf("failed to load the %q type: %w", slug, err)
	}
	return application.PresetResourceType{
		Name:    rt.Name(),
		Slug:    rt.Slug(),
		Context: rt.Context(),
		Schema:  rt.Schema(),
	}, nil
}

func (w *mealTermsWorld) installedMealPlanningTypes() ([]application.PresetResourceType, error) {
	slugs, err := presetTypeSlugs("meal-planning")
	if err != nil {
		return nil, err
	}
	out := make([]application.PresetResourceType, 0, len(slugs))
	for _, slug := range slugs {
		pt, err := w.installedPresetType(slug)
		if err != nil {
			return nil, err
		}
		out = append(out, pt)
	}
	return out, nil
}

// declaredProperties lists a stored schema's property names.
func declaredProperties(schema json.RawMessage) []string {
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if len(schema) == 0 || json.Unmarshal(schema, &s) != nil {
		return nil
	}
	names := make([]string, 0, len(s.Properties))
	for name := range s.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (w *mealTermsWorld) typeResolvesProperty(slug, property, predicate string) error {
	pt, err := w.installedPresetType(slug)
	if err != nil {
		return err
	}
	declared := declaredProperties(pt.Schema)
	found := false
	for _, name := range declared {
		if name == property {
			found = true
			break
		}
	}
	if !found {
		// Without this the assertion would still "pass" for a property the
		// type does not have: @vocab resolves any name at all.
		return fmt.Errorf("the installed %q type declares no %q property (declared: %v)", slug, property, declared)
	}
	if got := presets.ResolvedPredicateFor(pt, property); got != predicate {
		return fmt.Errorf("the %q type resolves %q to %s, want %s (context: %s)",
			slug, property, got, predicate, pt.Context)
	}
	return nil
}

// typeResolvesNothingTo covers both routes to an IRI: a schema property that
// resolves there, and a context entry that names it. A repair that removed the
// property but left the term behind would otherwise read as clean.
func (w *mealTermsWorld) typeResolvesNothingTo(slug, iri string) error {
	pt, err := w.installedPresetType(slug)
	if err != nil {
		return err
	}
	for _, property := range declaredProperties(pt.Schema) {
		if got := presets.ResolvedPredicateFor(pt, property); got == iri {
			return fmt.Errorf("the %q type resolves the property %q to %s", slug, property, iri)
		}
	}
	for term, got := range resolvedIRIs(pt.Context) {
		if got == iri {
			return fmt.Errorf("the %q type resolves the context entry %q to %s", slug, term, iri)
		}
	}
	return nil
}

// --- the guard ------------------------------------------------------------

// mealPlanningViolations sweeps the INSTALLED meal-planning types. It reads the
// raw violations — waivers not filtered — because no meal-planning waiver may
// survive this story, so a waived meal-planning entry has to fail here too.
func (w *mealTermsWorld) mealPlanningViolations() ([]presets.VocabularyViolation, error) {
	types, err := w.installedMealPlanningTypes()
	if err != nil {
		return nil, err
	}
	return presets.PublishedVocabularyViolations(types), nil
}

func (w *mealTermsWorld) violationsOfFault(fault presets.VocabularyFault) ([]presets.VocabularyViolation, error) {
	all, err := w.mealPlanningViolations()
	if err != nil {
		return nil, err
	}
	var out []presets.VocabularyViolation
	for _, v := range all {
		if v.Fault == fault {
			out = append(out, v)
		}
	}
	return out, nil
}

func renderViolations(violations []presets.VocabularyViolation) string {
	lines := make([]string, 0, len(violations))
	for _, v := range violations {
		lines = append(lines, v.String())
	}
	return strings.Join(lines, "\n  ")
}

func (w *mealTermsWorld) noUndefinedPublishedTerm() error {
	found, err := w.violationsOfFault(presets.FaultUndefinedTerm)
	if err != nil {
		return err
	}
	if len(found) > 0 {
		return fmt.Errorf("%d installed meal-planning propert(ies) state a term the vocabulary does not define:\n  %s",
			len(found), renderViolations(found))
	}
	return nil
}

func (w *mealTermsWorld) noTermForAnotherSubject() error {
	found, err := w.violationsOfFault(presets.FaultOtherSubject)
	if err != nil {
		return err
	}
	if len(found) > 0 {
		return fmt.Errorf("%d installed meal-planning propert(ies) borrow a term published for another subject:\n  %s",
			len(found), renderViolations(found))
	}
	return nil
}

// presetAddsUntermedProperty is the shape that caused this issue: a house
// property added to a preset schema with no term of its own, riding @vocab into
// somebody else's namespace. The registry the NEXT install reads is what
// changes, so the running app is restarted onto it — the database is still
// empty at this point and nothing has been installed from it yet.
func (w *mealTermsWorld) presetAddsUntermedProperty(preset, property, slug string) error {
	slugs, err := presetTypeSlugs(preset)
	if err != nil {
		return err
	}
	known := false
	for _, s := range slugs {
		if s == slug {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("the %q preset declares no %q type (declares: %v)", preset, slug, slugs)
	}
	w.extraLiterals[slug] = append(w.extraLiterals[slug], property)
	return w.restartOnThisBuild()
}

func (w *mealTermsWorld) findViolation(slug, property string) (presets.VocabularyViolation, error) {
	all, err := w.mealPlanningViolations()
	if err != nil {
		return presets.VocabularyViolation{}, err
	}
	for _, v := range all {
		if v.Slug == slug && v.PropertyName == property {
			w.guardNamed[v.Key()] = true
			return v, nil
		}
	}
	return presets.VocabularyViolation{}, fmt.Errorf(
		"the vocabulary guard does not name %s %q (it names:\n  %s)", slug, property, renderViolations(all))
}

func (w *mealTermsWorld) guardNamesResolving(slug, property, iri string) error {
	v, err := w.findViolation(slug, property)
	if err != nil {
		return err
	}
	if v.Fault != presets.FaultUndefinedTerm {
		return fmt.Errorf("the guard names %s %q as %q, not as a term the vocabulary does not define",
			slug, property, v.Fault)
	}
	if v.PredicateIRI != iri {
		return fmt.Errorf("the guard names %s %q resolving to %s, want %s", slug, property, v.PredicateIRI, iri)
	}
	return nil
}

func (w *mealTermsWorld) guardNamesOtherSubject(slug, property, namespace string) error {
	v, err := w.findViolation(slug, property)
	if err != nil {
		return err
	}
	if v.Fault != presets.FaultOtherSubject {
		return fmt.Errorf("the guard names %s %q as %q, not as a term published for another subject",
			slug, property, v.Fault)
	}
	if !strings.HasPrefix(v.PredicateIRI, namespace) {
		return fmt.Errorf("the guard names %s %q at %s, which is not under %s",
			slug, property, v.PredicateIRI, namespace)
	}
	return nil
}

func (w *mealTermsWorld) guardNamesNoOtherProperty() error {
	all, err := w.mealPlanningViolations()
	if err != nil {
		return err
	}
	var extra []presets.VocabularyViolation
	for _, v := range all {
		if !w.guardNamed[v.Key()] {
			extra = append(extra, v)
		}
	}
	if len(extra) > 0 {
		return fmt.Errorf("the guard also names %d propert(ies) the scenario did not:\n  %s",
			len(extra), renderViolations(extra))
	}
	return nil
}

// --- the build before the house properties were termed --------------------

const foodOntologyNamespace = "http://purl.org/foodontology#"

// preStoryMealTerms maps "<slug>.<property>" to the term the build BEFORE #535
// declared for it. "Reverted" is two operations, not one, and the map records
// which each property needs:
//
//   - "" — the property had NO term and rode `@vocab` into schema.org, so the
//     shim strips the term this story added;
//   - a compact `fo:` term — the property had a WRONG term, pointing into the
//     food ontology at a name that ontology never defined, so the shim restores
//     it. Only this second kind produces a stored statement under a published
//     IRI, which is what the edge upgrade scenario needs to exist at all.
//
// Twenty-one entries, one per repaired property, so a repair that missed one
// fails by name in buildBeforeTheHouseTerms rather than inside a total.
// `shopping-list-item.ingredient` is deliberately absent: it already resolved
// to `mp:ingredient` before this story.
var preStoryMealTerms = map[string]string{
	"recipe-ingredient.quantity":    "",
	"recipe-ingredient.unit":        "",
	"recipe-ingredient.optional":    "",
	"recipe-ingredient.preparation": "",
	"meal-occurrence.cookedAt":      "",
	"meal-occurrence.status":        "",
	"meal-occurrence.date":          "",
	"pantry.isDefault":              "",
	"food-item.quantity":            "",
	"food-item.unit":                "",
	"food-item.storage":             "",
	"food-item.expirationDate":      "",
	"shopping-list.createdAt":       "",
	"shopping-list.status":          "",
	"shopping-list-item.quantity":   "",
	"shopping-list-item.unit":       "",
	"shopping-list-item.checked":    "",

	"recipe.recipeIngredient":      "fo:hasIngredient",
	"recipe-ingredient.ingredient": "fo:ingredient",
	"ingredient.shoppingCategory":  "fo:ShoppingCategory",
	"ingredient.season":            "fo:at_its_best",
}

// buildBeforeTheHouseTerms is the default registry with #535's terms reverted —
// a transform over PresetResourceType.Context, not a second copy of the
// presets, so it cannot drift from what shipped.
func (w *mealTermsWorld) buildBeforeTheHouseTerms() *application.PresetRegistry {
	reverted := map[string]bool{}
	reg := rewriteRegistry(presets.NewDefaultRegistry(), func(pt *application.PresetResourceType) {
		var ctx map[string]any
		if json.Unmarshal(pt.Context, &ctx) != nil || ctx == nil {
			return
		}
		changed := false
		for key, before := range preStoryMealTerms {
			slug, property, _ := strings.Cut(key, ".")
			if slug != pt.Slug {
				continue
			}
			if _, declared := ctx[property]; !declared {
				// The shim has gone stale against the preset it shims: the
				// story's term is not there to revert, so every upgrade
				// scenario would run against the current build in disguise.
				panic(fmt.Sprintf("the pre-#535 shim expects the %q context to declare %q; it does not", slug, property))
			}
			if before == "" {
				delete(ctx, property)
			} else {
				ctx[property] = before
				ctx["fo"] = foodOntologyNamespace
			}
			reverted[key] = true
			changed = true
		}
		if !changed {
			return
		}
		encoded, err := json.Marshal(ctx)
		if err != nil {
			panic(fmt.Sprintf("the pre-#535 shim could not re-encode the %q context: %v", pt.Slug, err))
		}
		pt.Context = encoded
	})
	if len(reverted) != len(preStoryMealTerms) {
		var missing []string
		for key := range preStoryMealTerms {
			if !reverted[key] {
				missing = append(missing, key)
			}
		}
		sort.Strings(missing)
		panic(fmt.Sprintf("the pre-#535 shim reverted %d of %d terms; no type owns: %v",
			len(reverted), len(preStoryMealTerms), missing))
	}
	return reg
}

func (w *mealTermsWorld) aDatabaseFromBeforeTheHouseTerms() error {
	w.registry = w.buildBeforeTheHouseTerms
	return w.provision()
}
