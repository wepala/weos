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
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/cucumber/godog"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/internal/config"

	"go.uber.org/fx"
)

// Issue #519: the read-only count of resources still keyed by predicate IRI —
// the before-and-after check for the normalization (#523).

func TestIRIEdgeKeyCount(t *testing.T) {
	runContextFeature(t, "iri-edge-key-count", "features/iri_edge_key_count.feature")
}

func (w *contextWorld) registerIRIEdgeKeyCountSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the operator counts the resources with IRI-keyed edges$`, w.theOperatorCounts)

	sc.Step(`^the count reports (\d+) "(widget|vendor)" resources? with an IRI-keyed edge in (?:its|their) events$`,
		w.countReportsTypeEvents)
	sc.Step(`^the count reports (\d+) "(widget|vendor)" resources? with an IRI-keyed edge in (?:its|their) `+
		`canonical records?$`, w.countReportsTypeRecords)
	sc.Step(`^the count reports no resource with an IRI-keyed edge in its events$`, w.countReportsNoEvents)
	sc.Step(`^the count reports no resource with an IRI-keyed edge in its canonical record$`, w.countReportsNoRecords)
	sc.Step(`^the count reports (\d+) resources with an IRI-keyed edge in their events across the instance$`,
		w.countReportsTotalEvents)
	sc.Step(`^the count reports (\d+) resources with an IRI-keyed edge in their canonical records across the instance$`,
		w.countReportsTotalRecords)
	sc.Step(`^the check (passes|fails)$`, w.theCheck)

	sc.Step(`^the count classifies the "(widget|vendor)" "([^"]*)" edge on "([^"]*)" as (resolvable|unmapped)$`,
		w.countClassifies)
	sc.Step(`^the count classifies the "(widget|vendor)" "([^"]*)" edge on "([^"]*)" as ambiguous, `+
		`naming "([^"]*)" and "([^"]*)"$`, w.countClassifiesAmbiguous)
	sc.Step(`^the count classifies the IRI edge keys for "(widget|vendor)" as:$`, w.countClassifiesTable)
	sc.Step(`^the count reports (\d+) resources? that normalization would leave IRI-keyed$`, w.countReportsResidue)
	sc.Step(`^the count reports no resource that normalization would leave IRI-keyed$`, w.countReportsNoResidue)

	sc.Step(`^the stored canonical records are byte-identical to the ones stored before the run$`,
		w.canonicalRecordsUnchanged)
}

// theOperatorCounts runs the check the way the CLI does — its own narrow
// runtime, with the live app stopped, like the reproject and normalize steps
// — and keeps the feed and record snapshots so the read-only scenario can
// prove nothing moved.
func (w *contextWorld) theOperatorCounts() error {
	w.stop()
	before, err := w.snapshotEvents()
	if err != nil {
		return err
	}
	w.eventsBeforeNormalize = before
	recordsBefore, err := w.snapshotCanonicalRecords()
	if err != nil {
		return err
	}

	cfg := config.Default()
	cfg.DatabaseDSN = w.dsn
	cfg.LogLevel = "error"
	registry := w.catalogRegistry
	if w.registry != nil {
		registry = w.registry
	}
	var rt application.IRIEdgeKeyCountRuntime
	app := fx.New(fx.NopLogger, application.IRIEdgeKeyCountModule(cfg, registry()), fx.Populate(&rt))
	startCtx, startCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start the count-iri-edge-keys runtime: %w", err)
	}
	report, runErr := application.CountIRIEdgeKeys(context.Background(), rt, application.IRIEdgeKeyCountOptions{})
	stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	_ = app.Stop(stopCtx)
	stopCancel()
	if runErr != nil {
		return fmt.Errorf("count-iri-edge-keys failed: %w", runErr)
	}
	w.countReport = &report

	after, err := w.snapshotEvents()
	if err != nil {
		return err
	}
	w.eventsAfterNormalize = after
	w.recordsBeforeCount, w.recordsAfterCount = recordsBefore, nil
	if w.recordsAfterCount, err = w.snapshotCanonicalRecords(); err != nil {
		return err
	}
	return w.boot()
}

func (w *contextWorld) snapshotCanonicalRecords() (map[string]string, error) {
	var rows []struct {
		ID   string
		Data string
	}
	if err := w.db.Table("resources").Select("id", "data").Order("id ASC").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to read the canonical records: %w", err)
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.ID] = r.Data
	}
	return out, nil
}

func (w *contextWorld) count() (*application.IRIEdgeKeyCountReport, error) {
	if w.countReport == nil {
		return nil, fmt.Errorf("the count has not run in this scenario")
	}
	return w.countReport, nil
}

func (w *contextWorld) typeCounts(slug string) (application.IRIEdgeKeyTypeCount, error) {
	r, err := w.count()
	if err != nil {
		return application.IRIEdgeKeyTypeCount{}, err
	}
	if t := r.Types[slug]; t != nil {
		return *t, nil
	}
	return application.IRIEdgeKeyTypeCount{}, nil
}

func (w *contextWorld) countReportsTypeEvents(n int, slug string) error {
	t, err := w.typeCounts(slug)
	if err != nil {
		return err
	}
	if t.Events != n {
		return fmt.Errorf("the count reports %d %s resource(s) IRI-keyed in events, want %d (%+v)", t.Events, slug, n, t)
	}
	return nil
}

func (w *contextWorld) countReportsTypeRecords(n int, slug string) error {
	t, err := w.typeCounts(slug)
	if err != nil {
		return err
	}
	if t.Records != n {
		return fmt.Errorf("the count reports %d %s resource(s) IRI-keyed in canonical records, want %d (%+v)",
			t.Records, slug, n, t)
	}
	return nil
}

func (w *contextWorld) countReportsNoEvents() error  { return w.countReportsTotalEvents(0) }
func (w *contextWorld) countReportsNoRecords() error { return w.countReportsTotalRecords(0) }
func (w *contextWorld) countReportsNoResidue() error { return w.countReportsResidue(0) }

func (w *contextWorld) countReportsTotalEvents(n int) error {
	r, err := w.count()
	if err != nil {
		return err
	}
	if r.EventsTotal != n {
		return fmt.Errorf("the count reports %d resource(s) IRI-keyed in events across the instance, want %d",
			r.EventsTotal, n)
	}
	return nil
}

func (w *contextWorld) countReportsTotalRecords(n int) error {
	r, err := w.count()
	if err != nil {
		return err
	}
	if r.RecordsTotal != n {
		return fmt.Errorf("the count reports %d resource(s) IRI-keyed in canonical records across the instance, want %d",
			r.RecordsTotal, n)
	}
	return nil
}

func (w *contextWorld) countReportsResidue(n int) error {
	r, err := w.count()
	if err != nil {
		return err
	}
	if r.ResidueTotal != n {
		return fmt.Errorf("the count reports %d resource(s) normalization would leave IRI-keyed, want %d",
			r.ResidueTotal, n)
	}
	return nil
}

func (w *contextWorld) theCheck(verdict string) error {
	r, err := w.count()
	if err != nil {
		return err
	}
	if got := r.Passes(); got != (verdict == "passes") {
		return fmt.Errorf("the check %s (events=%d records=%d), want it to %s",
			map[bool]string{true: "passes", false: "fails"}[got], r.EventsTotal, r.RecordsTotal, verdict)
	}
	return nil
}

func (w *contextWorld) classificationOf(slug, name, iri string) (*application.EdgeKeyClassification, error) {
	r, err := w.count()
	if err != nil {
		return nil, err
	}
	id, err := w.targetID(slug, name)
	if err != nil {
		return nil, err
	}
	for i := range r.Classified {
		c := &r.Classified[i]
		if c.ResourceID == id && c.Key == iri {
			return c, nil
		}
	}
	return nil, nil
}

func (w *contextWorld) countClassifies(slug, name, iri, class string) error {
	c, err := w.classificationOf(slug, name, iri)
	if err != nil {
		return err
	}
	if class == "resolvable" {
		// Resolvable edges are counted, not listed: only the residue is.
		if c != nil {
			return fmt.Errorf("the count lists the %q edge on %q as %s, want resolvable", iri, name, c.Class)
		}
		t, err := w.typeCounts(slug)
		if err != nil {
			return err
		}
		if t.Resolvable == 0 {
			return fmt.Errorf("the count classifies no %s edge as resolvable (%+v)", slug, t)
		}
		return nil
	}
	if c == nil || string(c.Class) != class {
		return fmt.Errorf("the count does not classify the %q edge on %q as %s (listed: %+v)", iri, name, class, c)
	}
	return nil
}

func (w *contextWorld) countClassifiesAmbiguous(slug, name, iri, first, second string) error {
	c, err := w.classificationOf(slug, name, iri)
	if err != nil {
		return err
	}
	if c == nil || c.Class != application.EdgeKeyAmbiguous {
		return fmt.Errorf("the count does not classify the %q edge on %q as ambiguous (listed: %+v)", iri, name, c)
	}
	want := []string{first, second}
	sort.Strings(want)
	got := append([]string(nil), c.Candidates...)
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		return fmt.Errorf("the ambiguous edge %s on %q names %v, want %v", iri, name, got, want)
	}
	return nil
}

func (w *contextWorld) countClassifiesTable(slug string, table *godog.Table) error {
	if len(table.Rows) != 2 || len(table.Rows[1].Cells) != 3 {
		return fmt.Errorf("expected a header row and one row of three counts")
	}
	want := make([]int, 3)
	for i, cell := range table.Rows[1].Cells {
		n, err := strconv.Atoi(strings.TrimSpace(cell.Value))
		if err != nil {
			return fmt.Errorf("bad count %q: %w", cell.Value, err)
		}
		want[i] = n
	}
	t, err := w.typeCounts(slug)
	if err != nil {
		return err
	}
	if t.Resolvable != want[0] || t.Ambiguous != want[1] || t.Unmapped != want[2] {
		return fmt.Errorf("the count classifies %s edge keys as %d / %d / %d, want %d / %d / %d",
			slug, t.Resolvable, t.Ambiguous, t.Unmapped, want[0], want[1], want[2])
	}
	return nil
}

func (w *contextWorld) canonicalRecordsUnchanged() error {
	if w.recordsBeforeCount == nil || w.recordsAfterCount == nil {
		return fmt.Errorf("the count has not run in this scenario")
	}
	if len(w.recordsBeforeCount) != len(w.recordsAfterCount) {
		return fmt.Errorf("the canonical records changed in number across the run: %d -> %d",
			len(w.recordsBeforeCount), len(w.recordsAfterCount))
	}
	for id, before := range w.recordsBeforeCount {
		if after := w.recordsAfterCount[id]; after != before {
			return fmt.Errorf("the canonical record %s changed across the run:\n before %s\n after  %s", id, before, after)
		}
	}
	return nil
}
