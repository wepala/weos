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
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"github.com/wepala/weos/v3/application"
)

// Issue #518: a REDEFINED term (a Conflict) is reported beside an ADDED one
// and can be adopted, recording the IRI its edges are keyed by.

func (w *contextWorld) registerConflictAdoptionSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the held-terms listing for "widget" names "([^"]*)" as a term the preset (adds|redefines)$`,
		w.heldListingNamesKind)
	sc.Step(`^the held-terms listing for "widget" reports "([^"]*)" as stored at "([^"]*)"$`, w.heldListingStoredAt)
	sc.Step(`^the held-terms listing for "widget" reports "([^"]*)" as offered at "([^"]*)"$`, w.heldListingOfferedAt)
	sc.Step(`^the held-terms listing for "widget" says adopting "([^"]*)" keeps its edges under "([^"]*)" readable$`,
		w.heldListingKeepsEdges)
	sc.Step(`^the held-terms listing for "widget" says adopting "([^"]*)" moves "([^"]*)" off "([^"]*)"$`,
		w.heldListingMoves)
	sc.Step(`^the held-terms listing for "widget" reports "([^"]*)" once$`, w.heldListingReportsOnce)
	sc.Step(`^the held-terms listing for "widget" still names "([^"]*)" as held$`, w.heldListingStillNames)
	sc.Step(`^the held-terms listing for "widget" names no held term$`, w.heldListingEmpty)
	sc.Step(`^the boot's held report for "widget" names a command that adopts "([^"]*)"$`, w.bootHeldReportAdopts)
	sc.Step(`^the operator is told the class was not adopted and how to adopt it$`, w.sweepTellsAboutTheClass)
	sc.Step(`^the adoption tells the operator to re-stamp the existing records and reproject$`, w.adoptionNeedsRestamp)
	sc.Step(`^the adoption reports the "widget" class moving from "([^"]*)" to "([^"]*)"$`, w.adoptionReportsClassMove)
	sc.Step(`^the "widget" resources "([^"]*)" and "([^"]*)" carry different RDF types$`, w.widgetsCarryDifferentRDFTypes)
	sc.Step(`^the stored "widget" context maps "([^"]*)" to "([^"]*)"$`, w.theStoredContextStillMaps)
}

func (w *contextWorld) heldListing() ([]application.HeldTerm, error) {
	adopter, err := w.adopter()
	if err != nil {
		return nil, err
	}
	held, err := adopter.HeldContextTerms(context.Background(), adoptionPreset, "widget")
	if err != nil {
		return nil, fmt.Errorf("listing the held terms for widget failed: %w", err)
	}
	return held, nil
}

func (w *contextWorld) heldEntry(term string) (*application.HeldTerm, error) {
	held, err := w.heldListing()
	if err != nil {
		return nil, err
	}
	for i := range held {
		if held[i].Term == term {
			return &held[i], nil
		}
	}
	return nil, fmt.Errorf("the held-terms listing for widget does not name %q (held: %+v)", term, held)
}

func (w *contextWorld) heldListingNamesKind(term, kind string) error {
	h, err := w.heldEntry(term)
	if err != nil {
		return err
	}
	want := application.HeldTermAdded
	if kind == "redefines" {
		want = application.HeldTermRedefined
	}
	if h.Kind != want {
		return fmt.Errorf("the listing names %q as a term the preset %s, want %s", term, h.Kind, want)
	}
	return nil
}

func (w *contextWorld) heldListingStoredAt(term, iri string) error {
	h, err := w.heldEntry(term)
	if err != nil {
		return err
	}
	if h.StoredIRI != iri {
		return fmt.Errorf("the listing reports %q stored at %q, want %s", term, h.StoredIRI, iri)
	}
	return nil
}

func (w *contextWorld) heldListingOfferedAt(term, iri string) error {
	h, err := w.heldEntry(term)
	if err != nil {
		return err
	}
	if h.PresetIRI != iri {
		return fmt.Errorf("the listing reports %q offered at %q, want %s", term, h.PresetIRI, iri)
	}
	return nil
}

func (w *contextWorld) heldListingKeepsEdges(term, iri string) error {
	return w.heldListingMoves(term, term, iri)
}

func (w *contextWorld) heldListingMoves(term, property, iri string) error {
	h, err := w.heldEntry(term)
	if err != nil {
		return err
	}
	for _, m := range h.Moves {
		if m.Property == property && m.StoredIRI == iri {
			return nil
		}
	}
	return fmt.Errorf("the listing does not say adopting %q moves %q off %s (moves: %+v)", term, property, iri, h.Moves)
}

func (w *contextWorld) heldListingReportsOnce(term string) error {
	held, err := w.heldListing()
	if err != nil {
		return err
	}
	n := 0
	for _, h := range held {
		if h.Term == term {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("the listing reports %q %d times, want once (held: %+v)", term, n, held)
	}
	return nil
}

func (w *contextWorld) heldListingStillNames(term string) error {
	_, err := w.heldEntry(term)
	return err
}

func (w *contextWorld) heldListingEmpty() error {
	held, err := w.heldListing()
	if err != nil {
		return err
	}
	if len(held) != 0 {
		return fmt.Errorf("the listing still names held terms: %+v", held)
	}
	return nil
}

// bootHeldReportAdopts: the boot line that holds the term carries a remedy
// that adopts it — a sweep for an ordinary term, the term by name when it
// moves the class.
func (w *contextWorld) bootHeldReportAdopts(term string) error {
	r, err := w.report()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, line := range r.lines {
		if !strings.Contains(line, "adopt-term "+adoptionPreset+" widget") || !strings.Contains(line, term) {
			continue
		}
		if strings.Contains(line, "--all") || strings.Contains(line, "--term "+term) {
			return nil
		}
	}
	return fmt.Errorf("no boot line for widget names a command that adopts %q (lines: %v)", term, r.lines)
}

func (w *contextWorld) sweepTellsAboutTheClass() error {
	if w.lastAdoption == nil {
		return fmt.Errorf("no adoption has run in this scenario")
	}
	if len(w.lastAdoption.StillHeld) == 0 {
		return fmt.Errorf("the sweep reports nothing left held; the class was taken or not reported: %+v", w.lastAdoption)
	}
	held, err := w.heldListing()
	if err != nil {
		return err
	}
	var names, movers []string
	for _, h := range held {
		names = append(names, h.Term)
		if h.MovesClass() && h.Term != "@type" {
			movers = append(movers, h.Term)
		}
	}
	remedy := application.AdoptRemedy(adoptionPreset, "widget", names, movers)
	for _, left := range w.lastAdoption.StillHeld {
		if !strings.Contains(remedy, "--term "+left) {
			return fmt.Errorf("the operator is handed %q, which does not adopt %q", remedy, left)
		}
	}
	return nil
}

func (w *contextWorld) adoptionNeedsRestamp() error {
	if w.lastAdoption == nil || !w.lastAdoption.NeedsRestamp() {
		return fmt.Errorf("the adoption did not tell the operator to re-stamp: %+v", w.lastAdoption)
	}
	return nil
}

func (w *contextWorld) adoptionReportsClassMove(from, to string) error {
	if w.lastAdoption == nil || w.lastAdoption.ClassMove == nil {
		return fmt.Errorf("the adoption reported no class move: %+v", w.lastAdoption)
	}
	m := w.lastAdoption.ClassMove
	if m.StoredIRI != from || m.PresetIRI != to {
		return fmt.Errorf("the adoption reports the class moving %s -> %s, want %s -> %s", m.StoredIRI, m.PresetIRI, from, to)
	}
	return nil
}

func (w *contextWorld) widgetsCarryDifferentRDFTypes(a, b string) error {
	aType, err := w.widgetRDFType(a)
	if err != nil {
		return err
	}
	bType, err := w.widgetRDFType(b)
	if err != nil {
		return err
	}
	if aType == bType {
		return fmt.Errorf("widgets %q and %q both carry %q; the adopted class did not reach the new write", a, b, aType)
	}
	return nil
}
