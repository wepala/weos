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
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/cucumber/godog"
)

// TestContextTermAdoption is the acceptance suite for the migration that closes
// issue #513's remaining gap: the boot HOLDS a `@context` term whose adoption
// would repoint a predicate that already has data, which stops the orphaning
// but leaves the operator with no way forward — the property stays unreadable
// and the boot reports the same failure every start.
//
// It shares its step world with TestPresetContextGuards, because the situation
// it starts from is exactly the one those scenarios leave behind.
func TestContextTermAdoption(t *testing.T) {
	runContextFeature(t, "context-term-adoption", "features/context_term_adoption.feature")
}

// termAliasesKeyword is the `@context` key adoption records historical IRIs
// under. It is spelled out here rather than read from the production package on
// purpose: the contract is about what an operator finds in the stored context,
// so the suite must break if that key is renamed, not follow it silently.
const termAliasesKeyword = "weos:termAliases"

// contextTermAdopter is the application surface this contract requires. It is
// declared here, in test terms, so the suite compiles against a tree where
// nothing implements it yet and fails with a sentence rather than a build
// error. The ResourceTypeService satisfying it is what makes these scenarios
// pass.
//
// Two methods rather than one flag: adopting a NAMED term is the operator
// answering a specific held line from the boot log, and must be refused when
// that term was never held (a typo silently succeeding is worse than an error);
// adopting every held term is a sweep whose result is the list it took, which
// the CLI prints.
// adoptionPreset is the only preset this world declares, so every scenario
// adopts against it. It stays out of the Gherkin because naming it in every
// step would add a word no reader of these scenarios needs.
const adoptionPreset = "catalog"

type contextTermAdopter interface {
	AdoptContextTerms(ctx context.Context, presetName, typeSlug string, terms []string) ([]string, error)
}

// errNoAdopter marks "the command does not exist yet" so a scenario asserting a
// REFUSAL cannot be satisfied by the feature simply being absent.
var errNoAdopter = errors.New(
	"the ResourceTypeService exposes no way to adopt a held @context term: it must implement " +
		"AdoptContextTerms(ctx, presetName, typeSlug string, terms []string) ([]string, error) (issue #513)")

// registerAdoptionSteps adds the adopt-term steps to the shared context world.
func (w *contextWorld) registerAdoptionSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the operator adopts the held "([^"]*)" context term for "([^"]*)"$`, w.theOperatorAdoptsHeldTerm)
	sc.Step(`^the operator adopts the "([^"]*)" context term for "([^"]*)" again$`, w.theOperatorAdoptsTermAgain)
	sc.Step(`^the operator adopts the "([^"]*)" context term for "([^"]*)"$`, w.theOperatorTriesToAdoptTerm)
	sc.Step(`^the operator adopts every held context term for "([^"]*)"$`, w.theOperatorAdoptsEveryHeldTerm)

	sc.Step(`^the adoption is refused because "([^"]*)" was not held$`, w.theAdoptionWasRefused)
	sc.Step(`^the stored "widget" context records "([^"]*)" as a historical IRI for "([^"]*)"$`,
		w.theStoredContextRecordsAlias)
	sc.Step(`^the stored "widget" context records exactly one historical IRI for "([^"]*)"$`,
		w.theStoredContextRecordsExactlyOneAlias)
	sc.Step(`^the stored "widget" context records no historical IRI for "([^"]*)"$`, w.theStoredContextRecordsNoAlias)
	sc.Step(`^the stored "widget" context is byte-identical to the one stored before the second adoption$`,
		w.theStoredContextSurvivedTheSecondAdoption)

	sc.Step(`^the boot reconcile does not report the "([^"]*)" context term as held for "([^"]*)"$`,
		w.bootDoesNotReportContextTermHeld)
	sc.Step(`^the boot reconcile no longer names "([^"]*)" as a property whose writes are dropped$`,
		w.bootNoLongerNamesDropped)
	sc.Step(`^the boot reconcile records no failure for "([^"]*)"$`, w.bootRecordsNoFailure)
}

// --- adopting ---

func (w *contextWorld) adopter() (contextTermAdopter, error) {
	if w.rts == nil {
		return nil, fmt.Errorf("no twin is running in this scenario")
	}
	adopter, ok := w.rts.(contextTermAdopter)
	if !ok {
		return nil, errNoAdopter
	}
	return adopter, nil
}

func (w *contextWorld) theOperatorAdoptsHeldTerm(term, slug string) error {
	adopter, err := w.adopter()
	if err != nil {
		return err
	}
	if _, err := adopter.AdoptContextTerms(context.Background(), adoptionPreset, slug, []string{term}); err != nil {
		return fmt.Errorf("adopting the held %q term for %q failed: %w", term, slug, err)
	}
	return nil
}

// theOperatorTriesToAdoptTerm records the outcome instead of failing on it, so
// a scenario can assert the command REFUSED. The refusal assertion rejects
// errNoAdopter explicitly, so an unimplemented command never counts as one.
func (w *contextWorld) theOperatorTriesToAdoptTerm(term, slug string) error {
	adopter, err := w.adopter()
	if err != nil {
		w.adoptErr = err
		w.adoptAttempted = true
		return nil
	}
	_, w.adoptErr = adopter.AdoptContextTerms(context.Background(), adoptionPreset, slug, []string{term})
	w.adoptAttempted = true
	return nil
}

// theOperatorAdoptsTermAgain is the idempotence probe: it snapshots the stored
// context first so a second adoption that quietly appends another alias, or
// rewrites the term, is visible.
func (w *contextWorld) theOperatorAdoptsTermAgain(term, slug string) error {
	adopter, err := w.adopter()
	if err != nil {
		return err
	}
	rt, err := w.rts.GetBySlug(context.Background(), slug)
	if err != nil {
		return fmt.Errorf("failed to load the %q type before the second adoption: %w", slug, err)
	}
	w.contextBeforeSecondAdoption = append(json.RawMessage(nil), rt.Context()...)
	if _, err := adopter.AdoptContextTerms(context.Background(), adoptionPreset, slug, []string{term}); err != nil {
		return fmt.Errorf(
			"adopting %q for %q a second time failed, so the command is not idempotent: %w", term, slug, err)
	}
	return nil
}

func (w *contextWorld) theOperatorAdoptsEveryHeldTerm(slug string) error {
	adopter, err := w.adopter()
	if err != nil {
		return err
	}
	if _, err := adopter.AdoptContextTerms(context.Background(), adoptionPreset, slug, nil); err != nil {
		return fmt.Errorf("adopting every held context term for %q failed: %w", slug, err)
	}
	return nil
}

func (w *contextWorld) theAdoptionWasRefused(term string) error {
	if !w.adoptAttempted {
		return fmt.Errorf("no adoption was attempted in this scenario")
	}
	if errors.Is(w.adoptErr, errNoAdopter) {
		return w.adoptErr
	}
	if w.adoptErr == nil {
		return fmt.Errorf(
			"adopting %q succeeded, but that term was never held — a term with nothing to adopt must be refused, "+
				"because recording an alias no data was ever written under widens the reverse map for nothing "+
				"and a mistyped term name would otherwise look like success", term)
	}
	if !strings.Contains(w.adoptErr.Error(), term) {
		return fmt.Errorf("adoption was refused with %q, which never names the %q term the operator asked for",
			w.adoptErr, term)
	}
	return nil
}

// --- alias assertions ---

// storedAliases reads the historical IRIs recorded in the stored context,
// accepting both a lone string and an array for each property, since JSON-LD
// authors write single values either way.
func (w *contextWorld) storedAliases(slug string) (map[string][]string, error) {
	terms, err := w.storedContextOf(slug)
	if err != nil {
		return nil, err
	}
	raw, ok := terms[termAliasesKeyword]
	if !ok {
		return map[string][]string{}, nil
	}
	entries, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("stored %q entry is %T, want an object keyed by property name", termAliasesKeyword, raw)
	}
	out := make(map[string][]string, len(entries))
	for property, val := range entries {
		switch v := val.(type) {
		case string:
			out[property] = []string{v}
		case []any:
			iris := make([]string, 0, len(v))
			for _, item := range v {
				iris = append(iris, fmt.Sprintf("%v", item))
			}
			out[property] = iris
		default:
			return nil, fmt.Errorf("recorded aliases for %q are %T, want a string or an array of strings",
				property, val)
		}
	}
	return out, nil
}

func (w *contextWorld) theStoredContextRecordsAlias(iri, property string) error {
	aliases, err := w.storedAliases("widget")
	if err != nil {
		return err
	}
	for _, got := range aliases[property] {
		if got == iri {
			return nil
		}
	}
	return fmt.Errorf(
		"stored widget context records %v as the historical IRIs for %q, not %q — without it every edge already "+
			"written under that IRI stays unreadable, and a reproject rewrites nothing because the event is immutable",
		aliases[property], property, iri)
}

func (w *contextWorld) theStoredContextRecordsExactlyOneAlias(property string) error {
	aliases, err := w.storedAliases("widget")
	if err != nil {
		return err
	}
	if len(aliases[property]) != 1 {
		return fmt.Errorf("stored widget context records %d historical IRIs for %q (%v), want exactly 1",
			len(aliases[property]), property, aliases[property])
	}
	return nil
}

func (w *contextWorld) theStoredContextRecordsNoAlias(property string) error {
	aliases, err := w.storedAliases("widget")
	if err != nil {
		return err
	}
	if len(aliases[property]) > 0 {
		return fmt.Errorf("stored widget context records %v as historical IRIs for %q, but nothing was adopted for it",
			aliases[property], property)
	}
	return nil
}

// theStoredContextSurvivedTheSecondAdoption compares the stored context term by
// term rather than byte by byte: Go's map marshaling orders keys, but a nested
// definition re-encoded on the way through would reorder nothing an operator
// cares about, and the assertion is about CONTENT not being touched twice.
func (w *contextWorld) theStoredContextSurvivedTheSecondAdoption() error {
	if len(w.contextBeforeSecondAdoption) == 0 {
		return fmt.Errorf("no stored context was captured before a second adoption in this scenario")
	}
	rt, err := w.rts.GetBySlug(context.Background(), "widget")
	if err != nil {
		return fmt.Errorf("failed to load the widget type: %w", err)
	}
	before, err := canonicalJSON(w.contextBeforeSecondAdoption)
	if err != nil {
		return fmt.Errorf("the context captured before the second adoption is not valid JSON: %w", err)
	}
	after, err := canonicalJSON(rt.Context())
	if err != nil {
		return fmt.Errorf("the stored context after the second adoption is not valid JSON: %w", err)
	}
	if before != after {
		return fmt.Errorf("the second adoption changed the stored widget context, so adopting is not idempotent:"+
			"\nbefore: %s\nafter:  %s", before, after)
	}
	return nil
}

// canonicalJSON re-encodes a JSON value with map keys sorted, so two contexts
// differing only in key order compare equal.
func canonicalJSON(raw json.RawMessage) (string, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(sortJSON(value))
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func sortJSON(value any) any {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		// encoding/json already marshals map keys in sorted order, so rebuilding
		// the map is enough to normalise nested values too.
		out := make(map[string]any, len(v))
		for _, k := range keys {
			out[k] = sortJSON(v[k])
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, sortJSON(item))
		}
		return out
	default:
		return value
	}
}

// --- boot-report assertions ---

func (w *contextWorld) bootDoesNotReportContextTermHeld(term, slug string) error {
	r, err := w.report()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, line := range r.heldContext[slug] {
		if strings.Contains(line, term) {
			return fmt.Errorf(
				"boot reconcile still reports the %q context term as held for %q (held: %v) — adoption did not "+
					"settle the type, so the operator sees the same warning every start",
				term, slug, r.heldContext[slug])
		}
	}
	return nil
}

func (w *contextWorld) bootNoLongerNamesDropped(property string) error {
	r, err := w.report()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for slug, reason := range r.dropped {
		if strings.Contains(reason, property) {
			return fmt.Errorf("boot reconcile still names %q as a property whose writes are dropped for %q: %s",
				property, slug, reason)
		}
	}
	return nil
}

func (w *contextWorld) bootRecordsNoFailure(slug string) error {
	r, err := w.report()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if reason, failed := r.dropped[slug]; failed {
		return fmt.Errorf("boot reconcile still reports %q as NOT reconciled: %s", slug, reason)
	}
	if len(r.heldContext[slug]) > 0 || len(r.heldSchema[slug]) > 0 {
		return fmt.Errorf("boot reconcile still holds definitions for %q (context: %v, schema: %v)",
			slug, r.heldContext[slug], r.heldSchema[slug])
	}
	return nil
}
