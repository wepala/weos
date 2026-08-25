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

	"github.com/wepala/weos/v3/pkg/jsonld"
)

// Issue #522: a control keyword in a @context never claims a predicate a
// property owns.

func TestControlKeywordTerms(t *testing.T) {
	runContextFeature(t, "control-keyword-terms", "features/control_keyword_terms.feature")
}

func (w *contextWorld) registerControlKeywordSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the stored "widget" context declares these control entries:$`, w.storedContextDeclaresControlEntries)
	sc.Step(`^no predicate of "([^"]*)" is claimed by a control keyword$`, w.noPredicateClaimedByControlKeyword)
	sc.Step(`^the twin still reads the control entries of "([^"]*)" as:$`, w.controlEntriesReadAs)
	sc.Step(`^the boot reconcile does not name "([^"]*)" as a property whose writes are dropped$`,
		w.bootNoLongerNamesDropped)
}

// storedContextDeclaresControlEntries writes control entries into the stored
// widget context verbatim; the value column is JSON, so a string is quoted.
func (w *contextWorld) storedContextDeclaresControlEntries(table *godog.Table) error {
	terms, err := w.storedContextOf("widget")
	if err != nil {
		return err
	}
	for _, row := range table.Rows[1:] {
		if len(row.Cells) != 2 {
			return fmt.Errorf("expected entry | value rows")
		}
		var value any
		if err := json.Unmarshal([]byte(strings.TrimSpace(row.Cells[1].Value)), &value); err != nil {
			return fmt.Errorf("control entry %q has a value that is not JSON: %w", row.Cells[0].Value, err)
		}
		terms[strings.TrimSpace(row.Cells[0].Value)] = value
	}
	return w.writeStoredContext("widget", terms)
}

func (w *contextWorld) noPredicateClaimedByControlKeyword(slug string) error {
	rt, err := w.rts.GetBySlug(context.Background(), slug)
	if err != nil {
		return fmt.Errorf("failed to load the %q type: %w", slug, err)
	}
	_, forward := jsonld.ParseContext(rt.Context())
	for key := range jsonld.ControlKeywords {
		if iri, present := forward[key]; present {
			return fmt.Errorf("the control keyword %s entered the term map of %q as %s", key, slug, iri)
		}
	}
	for iri, name := range jsonld.BuildReverseMap(rt.Context()) {
		if jsonld.ControlKeywords[name] {
			return fmt.Errorf("%s reverse-maps to the control keyword %s on %q", iri, name, slug)
		}
	}
	return nil
}

func (w *contextWorld) controlEntriesReadAs(slug string, table *godog.Table) error {
	rt, err := w.rts.GetBySlug(context.Background(), slug)
	if err != nil {
		return fmt.Errorf("failed to load the %q type: %w", slug, err)
	}
	raw := rt.Context()
	for _, row := range table.Rows[1:] {
		reader, want := strings.TrimSpace(row.Cells[0].Value), strings.TrimSpace(row.Cells[1].Value)
		var got string
		switch {
		case reader == "subclass of":
			got = jsonld.SubClassOf(raw)
		case reader == "abstract":
			got = fmt.Sprintf("%v", jsonld.IsAbstract(raw))
		case reader == "value object":
			got = fmt.Sprintf("%v", jsonld.IsValueObject(raw))
		case reader == "adopted terms":
			got = strings.Join(jsonld.AdoptedTerms(raw), ", ")
		case strings.HasPrefix(reader, "alias of "):
			got = strings.Join(jsonld.TermAliases(raw)[strings.TrimPrefix(reader, "alias of ")], ", ")
		default:
			return fmt.Errorf("unknown control reader %q", reader)
		}
		if got != want {
			return fmt.Errorf("the twin reads %q of %s as %q, want %q", reader, slug, got, want)
		}
	}
	return nil
}
