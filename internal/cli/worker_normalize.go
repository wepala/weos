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

An edge whose IRI more than one property claims is never rewritten; it is
reported with both candidate properties for you to decide. An edge that no
term, alias or @vocab prefix names is reported and left as it was. Neither
stops the other resource types from being rewritten.

Procedure: stop the server, back up the database, run with --write, then run
"weos worker reproject" to rebuild the canonical records, projection columns
and the knowledge graph. Rollback is restoring the backup — the command
appends, deletes and renumbers nothing.`,
	Args: cobra.NoArgs,
	RunE: runWorkerNormalizeEdgeKeys,
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
	if err != nil {
		return err
	}
	printNormalizeEdgeKeysReport(os.Stdout, report)
	return nil
}

// printNormalizeEdgeKeysReport renders a run for the operator. The line
// markers ("ambiguous edge key", "unresolved edge key") are part of the
// command's contract — scripts and the acceptance suite grep for them.
func printNormalizeEdgeKeysReport(out io.Writer, r application.NormalizeEdgeKeysReport) {
	if r.DryRun {
		_, _ = fmt.Fprintln(out, "normalize-edge-keys: DRY RUN — no event was written. Re-run with --write to apply.")
	} else {
		_, _ = fmt.Fprintln(out, "normalize-edge-keys: events rewritten in place.")
	}
	_, _ = fmt.Fprintf(out, "scanned %d Resource.Created/Updated event(s)\n", r.Scanned)
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
		_, _ = fmt.Fprintf(out, "  %-24s %s %d of %d event(s)\n", slug, verb, t.Rewritten, t.Scanned)
	}
	if r.Rewritten == 0 {
		_, _ = fmt.Fprintln(out, "\nnothing to rewrite: every edge is already keyed by its property name.")
	}
	for _, p := range r.Ambiguous {
		_, _ = fmt.Fprintf(out, "\nambiguous edge key %s on %s (%s): candidates %s — not rewritten; %s\n",
			p.Key, p.ResourceID, p.TypeSlug, strings.Join(p.Candidates, ", "), p.Reason)
	}
	for _, p := range r.Unresolved {
		_, _ = fmt.Fprintf(out, "\nunresolved edge key %s on %s (%s) — not rewritten; %s\n",
			p.Key, p.ResourceID, p.TypeSlug, p.Reason)
	}
	if !r.DryRun && r.Rewritten > 0 {
		_, _ = fmt.Fprintln(out,
			"\nNext: run `weos worker reproject` (server stopped) to rebuild the canonical records, "+
				"projection columns and the knowledge graph from the rewritten events.")
	}
}
