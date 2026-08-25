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

package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"

	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

var workerCountIRIEdgeKeysCmd = &cobra.Command{
	Use:   "count-iri-edge-keys",
	Short: "Count the resources whose stored edges are still keyed by predicate IRI (read-only)",
	Long: `Counts, per resource type and for the whole instance, how many resources still
key an edge by predicate IRI — the form written before compact edge storage
(issue #515) and rewritten by "weos worker normalize-edge-keys" (#523).

Two surfaces are counted separately. EVENTS are what the normalization
rewrites; CANONICAL RECORDS are what every reader serves and change only when
"weos worker reproject" replays the events. Between the two commands the
events are clean and the records are not — this report shows that window.

Each IRI-keyed edge is classified the way the normalization will treat it:
resolvable (one property claims it; it will be rewritten), ambiguous (several
names claim it; it will be reported, never rewritten) or unmapped (nothing
names it; likewise). Resources holding an ambiguous or unmapped key are the
residue the normalization leaves behind.

The check PASSES — exit code 0 — only when both surfaces count zero; it exits
2 on FAIL and 1 when it could not run (unreachable database, empty store), so
a script can tell the two apart. Events of a resource type the store no
longer holds are reported as orphaned and do not fail the check: nothing can
rewrite them and nothing serves them.

It writes no row and never boots the full application — like every weos
command it applies pending schema migrations on open, and nothing else — so
it is safe on a live instance. It refuses to run against a store with no
resource type and no event, since a mistyped DSN would otherwise open an
empty database and pass.

Cost: the events surface reads every Resource.Created/Updated payload in the
history. Run the full check before the normalization and after "worker
reproject"; for a scheduled gate afterwards pass --records-only, which reads
only the canonical records.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runWorkerCountIRIEdgeKeys,
}

func init() {
	workerCountIRIEdgeKeysCmd.Flags().Int("batch-size", 500, "rows read per batch")
	workerCountIRIEdgeKeysCmd.Flags().Bool("records-only", false,
		"skip the events surface; check only the canonical records (the cheap steady-state gate)")
	workerCmd.AddCommand(workerCountIRIEdgeKeysCmd)
}

func runWorkerCountIRIEdgeKeys(cmd *cobra.Command, _ []string) error {
	batch, _ := cmd.Flags().GetInt("batch-size")
	recordsOnly, _ := cmd.Flags().GetBool("records-only")
	appCfg := GetConfig().Config

	var rt application.IRIEdgeKeyCountRuntime
	app := fx.New(
		fx.NopLogger,
		application.IRIEdgeKeyCountModule(appCfg, presets.NewDefaultRegistry()),
		fx.Populate(&rt),
	)
	startCtx, startCancel := context.WithTimeout(cmd.Context(), fx.DefaultTimeout)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start count-iri-edge-keys runtime: %w", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer stopCancel()
		_ = app.Stop(stopCtx)
	}()

	report, err := application.CountIRIEdgeKeys(cmd.Context(), rt,
		application.IRIEdgeKeyCountOptions{BatchSize: batch, RecordsOnly: recordsOnly})
	if err != nil {
		_, _ = fmt.Fprintln(os.Stdout, "check: ERROR")
		return err
	}
	printIRIEdgeKeyCountReport(os.Stdout, report)
	if !report.Passes() {
		// Exit 2, distinct from the 1 cobra uses for an error: a scheduled
		// gate must tell "the data is not clean" from "the check did not run".
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		_ = app.Stop(stopCtx)
		stopCancel()
		os.Exit(2)
	}
	return nil
}

// printIRIEdgeKeyCountReport renders a run for the operator. "check: PASS" /
// "check: FAIL" and the "ambiguous edge key" / "unmapped edge key" markers are
// the command's contract for scripts; the printer test pins them.
func printIRIEdgeKeyCountReport(out io.Writer, r application.IRIEdgeKeyCountReport) {
	for _, reason := range sortedKeys(r.Skipped) {
		_, _ = fmt.Fprintf(out, "skipped %d row(s): %s\n", r.Skipped[reason], reason)
	}
	if len(r.Types) == 0 {
		_, _ = fmt.Fprintln(out, "no resource keys an edge by predicate IRI.")
		_, _ = fmt.Fprintln(out, "check: PASS")
		return
	}
	if r.RecordsOnly {
		_, _ = fmt.Fprintln(out, "events surface not scanned (--records-only).")
	}
	width := 13
	for _, slug := range r.TypeSlugs() {
		if len(slug) > width {
			width = len(slug)
		}
	}
	_, _ = fmt.Fprintf(out, "%-*s %8s %8s   %s\n", width, "resource type", "events", "records",
		"IRI edge keys: resolvable / ambiguous / unmapped")
	for _, slug := range r.TypeSlugs() {
		t := r.Types[slug]
		_, _ = fmt.Fprintf(out, "%-*s %8d %8d   %d / %d / %d", width, slug, t.Events, t.Records,
			t.Resolvable, t.Ambiguous, t.Unmapped)
		if t.Orphaned > 0 {
			_, _ = fmt.Fprintf(out, "   (%d orphaned: type not stored)", t.Orphaned)
		}
		_, _ = fmt.Fprintln(out)
	}
	_, _ = fmt.Fprintf(out, "%-*s %8d %8d\n", width, "total", r.EventsTotal, r.RecordsTotal)
	if r.OrphanedTotal > 0 {
		_, _ = fmt.Fprintf(out, "\n%d resource(s) of a type the store no longer holds still key an edge by IRI in "+
			"their events; nothing can rewrite or read them, and they do not fail the check.\n", r.OrphanedTotal)
	}
	_, _ = fmt.Fprintf(out, "\n%d resource(s) hold an ambiguous or unmapped key — normalization will leave them "+
		"IRI-keyed until the type's @context is fixed.\n", r.ResidueTotal)
	for _, c := range r.Classified {
		line := fmt.Sprintf("%s edge key %s on %s (%s)", c.Class, c.Key, c.ResourceID, c.TypeSlug)
		if len(c.Candidates) > 0 {
			line += ": candidates " + strings.Join(c.Candidates, ", ")
		}
		_, _ = fmt.Fprintln(out, line)
	}
	if extra := r.ClassifiedTotal - len(r.Classified); extra > 0 {
		_, _ = fmt.Fprintf(out, "… and %d more not listed (the report keeps the first %d).\n",
			extra, application.MaxReportedProblems)
	}
	if r.EventsTotal == 0 && r.RecordsTotal > 0 {
		_, _ = fmt.Fprintln(out, "\nevents are clean but canonical records are not: run `weos worker reproject`.")
	}
	if r.Passes() {
		_, _ = fmt.Fprintln(out, "\ncheck: PASS")
	} else {
		_, _ = fmt.Fprintln(out, "\ncheck: FAIL")
	}
}
