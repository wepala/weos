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
	"sort"
	"strings"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"

	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

var workerNormalizeEdgeKeysCmd = &cobra.Command{
	Use:   "normalize-edge-keys",
	Short: "Rewrite stored events so every edge is keyed by its property name, not a predicate IRI",
	Long: `Walks every Resource.Created and Resource.Updated event and rewrites each
edges-node key that is a predicate IRI (the form written before compact edge
storage, issue #515) to the property name it resolves to under the type's
current @context. After that, a namespace change is a preset edit plus a
reprojection — no term aliases are needed.

DRY RUN BY DEFAULT: the command reports what it would change, per resource
type, and writes nothing until --write is passed.

An edge whose IRI more than one name claims is never rewritten; it is
reported with the candidates for you to decide. An edge that no term, alias
or @vocab prefix names is reported and left as it was, as is one whose
property name the document already keys another edge by. None of these stops
the other resource types from being rewritten. The command exits non-zero
when it declined any edge, so a script can gate on it.

Procedure: stop the server, back up the database, run with --write, then run
"weos worker reproject" to rebuild the canonical records, projection columns
and the triples table, and "weos worker checkpoint reset oxigraph --truncate"
to rebuild the knowledge graph (reproject does not reach it). Rollback is
restoring the backup — the command appends, deletes and renumbers nothing.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runWorkerNormalizeEdgeKeys,
}

func init() {
	workerNormalizeEdgeKeysCmd.Flags().Bool("write", false, "apply the rewrite (default is a dry run)")
	workerNormalizeEdgeKeysCmd.Flags().Int("batch-size", 500, "events read per batch")
	workerCmd.AddCommand(workerNormalizeEdgeKeysCmd)
}

func runWorkerNormalizeEdgeKeys(cmd *cobra.Command, _ []string) error {
	write, _ := cmd.Flags().GetBool("write")
	batch, _ := cmd.Flags().GetInt("batch-size")
	appCfg := GetConfig().Config

	var rt application.NormalizeEdgeKeysRuntime
	app := fx.New(
		fx.NopLogger,
		application.NormalizeEdgeKeysModule(appCfg, presets.NewDefaultRegistry()),
		fx.Populate(&rt),
	)
	startCtx, startCancel := context.WithTimeout(cmd.Context(), fx.DefaultTimeout)
	defer startCancel()
	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start normalize-edge-keys runtime: %w", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer stopCancel()
		_ = app.Stop(stopCtx)
	}()

	report, err := application.NormalizeEdgeKeys(cmd.Context(), rt, application.NormalizeEdgeKeysOptions{
		Write:     write,
		BatchSize: batch,
	})
	// The report is printed even when the run stopped early: each batch
	// commits on its own, so the operator needs to know what already landed.
	printNormalizeEdgeKeysReport(os.Stdout, report)
	if err != nil {
		return err
	}
	if n := report.Problems(); n > 0 {
		return fmt.Errorf("%d edge(s) were not rewritten; see the report above", n)
	}
	return nil
}

// printNormalizeEdgeKeysReport renders a run for the operator. The line
// markers ("ambiguous edge key", "unresolved edge key", "colliding edge key")
// are the command's contract for scripts; the printer test pins them.
func printNormalizeEdgeKeysReport(out io.Writer, r application.NormalizeEdgeKeysReport) {
	if r.DryRun {
		_, _ = fmt.Fprintln(out, "normalize-edge-keys: DRY RUN — no event was written. Re-run with --write to apply.")
	} else {
		_, _ = fmt.Fprintln(out, "normalize-edge-keys: events rewritten in place.")
	}
	_, _ = fmt.Fprintf(out, "scanned %d Resource.Created/Updated event(s)\n", r.Scanned)
	for _, reason := range sortedKeys(r.Skipped) {
		_, _ = fmt.Fprintf(out, "skipped %d event(s): %s\n", r.Skipped[reason], reason)
	}
	if len(r.Types) == 0 {
		_, _ = fmt.Fprintln(out, "nothing to rewrite: no resource events found.")
		return
	}
	verb := "rewrote"
	if r.DryRun {
		verb = "would rewrite"
	}
	_, _ = fmt.Fprintln(out)
	for _, slug := range r.TypeSlugs() {
		t := r.Types[slug]
		_, _ = fmt.Fprintf(out, "  %-24s %s %d of %d event(s)", slug, verb, t.Rewritten, t.Scanned)
		if n := countProblems(r.Ambiguous, slug); n > 0 {
			_, _ = fmt.Fprintf(out, "; %d ambiguous edge(s)", n)
		}
		if n := countProblems(r.Unresolved, slug); n > 0 {
			_, _ = fmt.Fprintf(out, "; %d unresolved edge(s)", n)
		}
		if n := countProblems(r.Collisions, slug); n > 0 {
			_, _ = fmt.Fprintf(out, "; %d colliding edge(s)", n)
		}
		_, _ = fmt.Fprintln(out)
	}
	if r.Rewritten == 0 && r.Problems() == 0 {
		_, _ = fmt.Fprintln(out, "\nnothing to rewrite: every edge is already keyed by its property name.")
	}
	printProblems(out, "ambiguous edge key", r.Ambiguous, r.AmbiguousTotal)
	printProblems(out, "unresolved edge key", r.Unresolved, r.UnresolvedTotal)
	printProblems(out, "colliding edge key", r.Collisions, r.CollisionTotal)
	if r.Problems() > 0 {
		_, _ = fmt.Fprintf(out, "\n%d edge(s) were not rewritten. Fix the type's @context (or the record) and re-run; "+
			"an edge left keyed by its IRI keeps reading through the existing paths.\n", r.Problems())
	}
	if !r.DryRun && r.Rewritten > 0 {
		_, _ = fmt.Fprintln(out,
			"\nNext (server stopped): `weos worker reproject` rebuilds the canonical records, projection "+
				"columns and triples; `weos worker checkpoint reset oxigraph --truncate` rebuilds the knowledge graph.")
	}
}

func countProblems(list []application.EdgeKeyProblem, slug string) int {
	n := 0
	for _, p := range list {
		if p.TypeSlug == slug {
			n++
		}
	}
	return n
}

// printProblems prints one line per declined edge under its marker, and says
// how many more the report holds beyond the bounded list.
func printProblems(out io.Writer, marker string, list []application.EdgeKeyProblem, total int) {
	for _, p := range list {
		line := fmt.Sprintf("\n%s %s on %s (%s) in event %s at position %d", marker, p.Key, p.ResourceID,
			p.TypeSlug, p.EventID, p.Position)
		if len(p.Candidates) > 0 {
			line += ": candidates " + strings.Join(p.Candidates, ", ")
		}
		_, _ = fmt.Fprintf(out, "%s — not rewritten; %s\n", line, p.Reason)
	}
	if extra := total - len(list); extra > 0 {
		_, _ = fmt.Fprintf(out, "\n… and %d more %s line(s) not listed (the report keeps the first %d).\n",
			extra, marker, application.MaxReportedProblems)
	}
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
