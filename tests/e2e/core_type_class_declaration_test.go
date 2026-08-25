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
	"strings"
	"testing"

	"github.com/cucumber/godog"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/pkg/jsonld"
)

// Issue #521: Person and Organization declare a class. The world is #520's
// real-preset world; "the build before Person and Organization declared a
// class" is the default registry with the two "@type" entries deleted from
// the core contexts — the real preset, edited, so it cannot drift.

func TestCoreTypeClassDeclaration(t *testing.T) {
	runFeatureWith(t, "core-type-class-declaration", "features/core_type_class_declaration.feature",
		initCoreTypeClassScenario)
}

type classWorld struct {
	*vocabWorld
	heldTerms    []application.HeldTerm
	heldPreset   string
	heldSlug     string
	sweepAdopted []string
}

func initCoreTypeClassScenario(sc *godog.ScenarioContext) {
	w := &classWorld{vocabWorld: newVocabWorld()}
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.teardown()
		return ctx, nil
	})
	w.registerVocabSteps(sc)

	sc.Step(`^a WeOS database provisioned by the build before Person and Organization declared a class$`,
		w.aDatabaseFromTheBuildWithoutTheClass)
	sc.Step(`^the twin restarts on the build that declares the class(?: again)?$`, w.restartOnThisBuild)
	sc.Step(`^the build that declares the class also adds a "([^"]*)" string property to "([^"]*)"$`,
		w.thisBuildAlsoAddsLiteral)
	sc.Step(`^I create an? "([^"]*)" named "([^"]*)" with these properties:$`, w.createWithProperties)

	sc.Step(`^the stored "([^"]*)" context maps "([^"]*)" to "([^"]*)"$`, w.storedContextMaps)
	sc.Step(`^the stored "([^"]*)" context declares no "([^"]*)"$`, w.storedContextDeclaresNo)
	sc.Step(`^the "([^"]*)" type advertises the RDF class "([^"]*)"$`, w.typeAdvertisesClass)
	sc.Step(`^the embedded "@context" of that resource defines the "([^"]*)" prefix as "([^"]*)"$`,
		w.lastResourceContextDefinesPrefix)
	sc.Step(`^the "@type" of that resource resolves to "([^"]*)" through its own embedded context alone$`,
		w.lastResourceCarriesType)
	sc.Step(`^that resolution does not depend on the built-in "schema" prefix fallback$`, w.lastResourceTypeNeedsNoFallback)
	sc.Step(`^every installed resource type declares an "@type" in its stored context$`, w.everyTypeDeclaresType)
	sc.Step(`^every installed resource type advertises an RDF class that is an absolute IRI$`, w.everyTypeAdvertisesIRI)

	sc.Step(`^the operator lists the held terms for "([^"]*)" "([^"]*)"$`, w.listHeldTerms)
	sc.Step(`^"([^"]*)" is reported as held and offered at "([^"]*)"$`, w.heldTermOfferedAt)
	sc.Step(`^the held "([^"]*)" names no IRI that existing data is keyed by$`, w.heldTermHasNoStoredIRI)
	sc.Step(`^the operator adopts every held context term for "([^"]*)" "([^"]*)"$`, w.adoptEveryHeldTerm)
	sc.Step(`^the operator adopts the held "([^"]*)" context term for "([^"]*)" "([^"]*)"$`, w.adoptHeldTerm)
	sc.Step(`^the operator is told the class was not adopted and how to adopt it$`, w.sweepLeftTheClassAndSaidSo)
	sc.Step(`^the boot reconcile reports the "([^"]*)" context term as held for "([^"]*)" on the next restart$`,
		w.heldOnNextRestart)
	sc.Step(`^the boot's held report for "([^"]*)" names a command that adopts "([^"]*)"$`, w.bootRemedyAdopts)
	sc.Step(`^the command it prints adopts "([^"]*)" for "([^"]*)"$`, w.listedRemedyAdopts)
	sc.Step(`^running that command declares "([^"]*)" as the "@type" of the stored "([^"]*)" context$`,
		w.runningTheRemedyDeclares)
	sc.Step(`^the boot reconcile does not report the "([^"]*)" context term as held for "([^"]*)"$`,
		w.bootDoesNotReportContextTermHeld)
	sc.Step(`^the boot reconcile records no failure for "([^"]*)"$`, w.bootRecordsNoFailure)
	sc.Step(`^the stored "([^"]*)" context records no historical IRI for "([^"]*)"$`, w.storedContextRecordsNoAliasFor)
	sc.Step(`^the stored "([^"]*)" context records no empty historical IRI for any property$`, w.storedContextHasNoEmptyAlias)
}

// --- the build before the class ---

func (w *classWorld) buildWithoutTheClass() *application.PresetRegistry {
	rewrote := 0
	reg := rewriteRegistry(presets.NewDefaultRegistry(), func(pt *application.PresetResourceType) {
		if pt.Slug != "person" && pt.Slug != "organization" {
			return
		}
		var ctx map[string]any
		if json.Unmarshal(pt.Context, &ctx) != nil {
			return
		}
		if _, has := ctx["@type"]; !has {
			return
		}
		delete(ctx, "@type")
		encoded, err := json.Marshal(ctx)
		if err != nil {
			return
		}
		pt.Context = encoded
		rewrote++
	})
	if rewrote != 2 {
		panic(fmt.Sprintf("the old-build shim removed @type from %d core types, want 2", rewrote))
	}
	return reg
}

func (w *classWorld) aDatabaseFromTheBuildWithoutTheClass() error {
	w.registry = w.buildWithoutTheClass
	return w.provision()
}

func (w *classWorld) createWithProperties(slug, name string, table *godog.Table) error {
	given := map[string]any{}
	for _, row := range table.Rows {
		if len(row.Cells) != 2 {
			return fmt.Errorf("expected property | value rows")
		}
		v, err := w.literalValue(slug, strings.TrimSpace(row.Cells[0].Value), strings.TrimSpace(row.Cells[1].Value))
		if err != nil {
			return err
		}
		given[strings.TrimSpace(row.Cells[0].Value)] = v
	}
	return w.createResourceOf(slug, name, given)
}

// --- what a type declares and advertises ---

func (w *classWorld) storedContextDeclaresNo(slug, term string) error {
	terms, err := w.storedContextOf(slug)
	if err != nil {
		return err
	}
	if v, has := terms[term]; has {
		return fmt.Errorf("the stored %q context declares %q as %v", slug, term, v)
	}
	return nil
}

// advertisedClass re-derives the class IRI the ontology projection advertises
// (resourceTypeClassIRI is unexported; a unit guard in package application
// pins the two against each other).
func advertisedClass(name, slug string, raw json.RawMessage) string {
	vocab, _ := jsonld.ParseContext(raw)
	typeName := name
	var ctx map[string]any
	if json.Unmarshal(raw, &ctx) == nil {
		if ct, ok := ctx["@type"].(string); ok && ct != "" {
			typeName = ct
		}
	}
	if typeName == "" {
		typeName = slug
	}
	iri := jsonld.ExpandIRI(typeName, vocab, ctx)
	if !strings.Contains(iri, "://") && !strings.HasPrefix(iri, "urn:") {
		return "urn:type:" + slug
	}
	return iri
}

func (w *classWorld) typeAdvertisesClass(slug, class string) error {
	rt, err := w.rts.GetBySlug(context.Background(), slug)
	if err != nil {
		return fmt.Errorf("failed to load the %q type: %w", slug, err)
	}
	if got := advertisedClass(rt.Name(), rt.Slug(), rt.Context()); got != class {
		return fmt.Errorf("the %q type advertises the RDF class %s, want %s", slug, got, class)
	}
	return nil
}

func (w *classWorld) lastResourceContextDefinesPrefix(prefix, ns string) error {
	_, embedded, err := w.document(w.lastID)
	if err != nil {
		return err
	}
	var ctx map[string]any
	if json.Unmarshal(embedded, &ctx) != nil {
		return fmt.Errorf("the embedded @context of the %s resource is not an object: %s", w.lastSlug, embedded)
	}
	if got := fmt.Sprintf("%v", ctx[prefix]); got != ns {
		return fmt.Errorf("the embedded @context defines %q as %v, want %s", prefix, ctx[prefix], ns)
	}
	return nil
}

// lastResourceTypeNeedsNoFallback checks the entity @type's prefix is one
// the document itself declares — jsonld.ExpandIRI has a WeOS-only fallback
// for an undeclared `schema:` prefix that no conformant processor shares.
func (w *classWorld) lastResourceTypeNeedsNoFallback() error {
	doc, embedded, err := w.document(w.lastID)
	if err != nil {
		return err
	}
	graph, _ := doc["@graph"].([]any)
	if len(graph) == 0 {
		return fmt.Errorf("the %s resource has no @graph", w.lastSlug)
	}
	entity, _ := graph[0].(map[string]any)
	typ, _ := entity["@type"].(string)
	prefix, _, hasPrefix := strings.Cut(typ, ":")
	if !hasPrefix || strings.HasPrefix(typ, "http") {
		return nil
	}
	var ctx map[string]any
	_ = json.Unmarshal(embedded, &ctx)
	if _, declared := ctx[prefix].(string); !declared {
		return fmt.Errorf("the entity @type %q uses the %q prefix, which the document's own @context does not declare", typ, prefix)
	}
	return nil
}

func (w *classWorld) everyTypeDeclaresType() error {
	types, err := w.installedTypes()
	if err != nil {
		return err
	}
	for _, rt := range types {
		var ctx map[string]any
		if json.Unmarshal(rt.Context(), &ctx) != nil {
			return fmt.Errorf("the %q context is not an object", rt.Slug())
		}
		if typ, _ := ctx["@type"].(string); typ == "" {
			return fmt.Errorf("the installed type %q declares no @type", rt.Slug())
		}
	}
	return nil
}

func (w *classWorld) everyTypeAdvertisesIRI() error {
	types, err := w.installedTypes()
	if err != nil {
		return err
	}
	for _, rt := range types {
		got := advertisedClass(rt.Name(), rt.Slug(), rt.Context())
		if !strings.Contains(got, "://") && !strings.HasPrefix(got, "urn:") {
			return fmt.Errorf("the installed type %q advertises %q, which is not an absolute IRI", rt.Slug(), got)
		}
	}
	return nil
}

// --- held terms and adoption on the real core preset ---

func (w *classWorld) listHeldTerms(preset, slug string) error {
	held, err := w.rts.HeldContextTerms(context.Background(), preset, slug)
	if err != nil {
		return fmt.Errorf("listing the held terms for %s %s failed: %w", preset, slug, err)
	}
	w.heldTerms, w.heldPreset, w.heldSlug = held, preset, slug
	return nil
}

func (w *classWorld) heldTerm(term string) (*application.HeldTerm, error) {
	if w.heldSlug == "" {
		return nil, fmt.Errorf("the held terms have not been listed in this scenario")
	}
	for i := range w.heldTerms {
		if w.heldTerms[i].Term == term {
			return &w.heldTerms[i], nil
		}
	}
	return nil, fmt.Errorf("%q is not reported as held for %q (held: %+v)", term, w.heldSlug, w.heldTerms)
}

func (w *classWorld) heldTermOfferedAt(term, iri string) error {
	h, err := w.heldTerm(term)
	if err != nil {
		return err
	}
	if h.PresetIRI != iri {
		return fmt.Errorf("%q is offered at %s, want %s", term, h.PresetIRI, iri)
	}
	return nil
}

func (w *classWorld) heldTermHasNoStoredIRI(term string) error {
	h, err := w.heldTerm(term)
	if err != nil {
		return err
	}
	if h.StoredIRI != "" {
		return fmt.Errorf("the held %q names %s as the IRI existing data is keyed by; a class keys no edge", term, h.StoredIRI)
	}
	return nil
}

func (w *classWorld) heldTermNames(preset, slug string) ([]string, error) {
	held, err := w.rts.HeldContextTerms(context.Background(), preset, slug)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(held))
	for _, h := range held {
		names = append(names, h.Term)
	}
	return names, nil
}

func (w *classWorld) adoptEveryHeldTerm(preset, slug string) error {
	adopted, err := w.rts.AdoptContextTerms(context.Background(), preset, slug, nil)
	if err != nil {
		return fmt.Errorf("the sweep for %s %s failed: %w", preset, slug, err)
	}
	w.sweepAdopted, w.heldPreset, w.heldSlug = adopted, preset, slug
	return nil
}

func (w *classWorld) adoptHeldTerm(term, preset, slug string) error {
	if _, err := w.rts.AdoptContextTerms(context.Background(), preset, slug, []string{term}); err != nil {
		return fmt.Errorf("adopting the held %q term for %s %s failed: %w", term, preset, slug, err)
	}
	return nil
}

// sweepLeftTheClassAndSaidSo: the sweep adopted nothing for the class, and
// the remedy the operator is handed afterwards names the command that does.
func (w *classWorld) sweepLeftTheClassAndSaidSo() error {
	for _, term := range w.sweepAdopted {
		if term == "@type" {
			return fmt.Errorf("the sweep adopted @type; a sweep must never move a class")
		}
	}
	names, err := w.heldTermNames(w.heldPreset, w.heldSlug)
	if err != nil {
		return err
	}
	remedy := application.AdoptRemedy(w.heldPreset, w.heldSlug, names, nil)
	if !strings.Contains(remedy, "--term @type") {
		return fmt.Errorf("after the sweep the operator is told %q, which does not adopt the class", remedy)
	}
	return nil
}

func (w *classWorld) heldOnNextRestart(term, slug string) error {
	if err := w.restartOnThisBuild(); err != nil {
		return err
	}
	return w.bootReportsContextTermHeld(term, slug)
}

func (w *classWorld) bootRemedyAdopts(slug, term string) error {
	r, err := w.report()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, line := range r.lines {
		if strings.Contains(line, "adopt-term core "+slug) && strings.Contains(line, "--term "+term) {
			return nil
		}
	}
	return fmt.Errorf("no boot line for %q names a command that adopts %q (lines: %v)", slug, term, r.lines)
}

func (w *classWorld) listedRemedyAdopts(term, slug string) error {
	if w.heldSlug != slug {
		return fmt.Errorf("the held terms listed were for %q, not %q", w.heldSlug, slug)
	}
	names := make([]string, 0, len(w.heldTerms))
	for _, h := range w.heldTerms {
		names = append(names, h.Term)
	}
	if remedy := application.AdoptRemedy(w.heldPreset, slug, names, nil); !strings.Contains(remedy, "--term "+term) {
		return fmt.Errorf("held-terms prints %q, which does not adopt %q", remedy, term)
	}
	return nil
}

func (w *classWorld) runningTheRemedyDeclares(declared, slug string) error {
	if err := w.adoptHeldTerm("@type", w.heldPreset, slug); err != nil {
		return err
	}
	return w.storedContextMaps(slug, "@type", declared)
}

func (w *classWorld) storedContextRecordsNoAliasFor(slug, property string) error {
	rt, err := w.rts.GetBySlug(context.Background(), slug)
	if err != nil {
		return err
	}
	if iris := jsonld.TermAliases(rt.Context())[property]; len(iris) > 0 {
		return fmt.Errorf("the stored %q context records %v as historical IRIs for %q", slug, iris, property)
	}
	return nil
}

func (w *classWorld) storedContextHasNoEmptyAlias(slug string) error {
	rt, err := w.rts.GetBySlug(context.Background(), slug)
	if err != nil {
		return err
	}
	for property, iris := range jsonld.TermAliases(rt.Context()) {
		for _, iri := range iris {
			if iri == "" {
				return fmt.Errorf("the stored %q context records an empty historical IRI for %q", slug, property)
			}
		}
	}
	return nil
}
