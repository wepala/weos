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
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// The boot reconcile refuses to adopt a `@context` term whose adoption would
// repoint a predicate that already has data (issue #513). That refusal is
// correct — adopting would orphan every existing edge — but on its own it
// leaves the property unreadable and the boot reporting it forever, with the
// ADR forbidding `preset install --update` at boot. These two commands are the
// way out: one shows the decision, the other makes it.

var heldTermsCmd = &cobra.Command{
	Use:   "held-terms [preset] [type-slug]",
	Short: "Show the @context terms the boot refuses to adopt for a resource type",
	Long: "Lists each term the boot is holding, the IRI existing edges are keyed by, " +
		"and the IRI the preset wants. Read this before adopting anything.",
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer func() { _ = deps.Shutdown() }()

		held, err := deps.ResourceTypeService.HeldContextTerms(cmd.Context(), args[0], args[1])
		if err != nil {
			return err
		}
		if len(held) == 0 {
			_, _ = fmt.Fprintf(os.Stdout, "No held terms for %q.\n", args[1])
			return nil
		}
		_, _ = fmt.Fprintf(os.Stdout, "%d held term(s) for %q:\n\n", len(held), args[1])
		for _, h := range held {
			_, _ = fmt.Fprintf(os.Stdout, "  %s\n", h.Term)
			_, _ = fmt.Fprintf(os.Stdout, "    property        %s\n", h.Property)
			_, _ = fmt.Fprintf(os.Stdout, "    data written as %s\n", h.StoredIRI)
			_, _ = fmt.Fprintf(os.Stdout, "    preset wants    %s\n\n", h.PresetIRI)
		}
		_, _ = fmt.Fprintf(os.Stdout,
			"Adopt with:  weos resource-type adopt-term %s %s --all\n", args[0], args[1])
		return nil
	},
}

var adoptTermCmd = &cobra.Command{
	Use:   "adopt-term [preset] [type-slug]",
	Short: "Adopt held @context terms, keeping existing edges readable",
	Long: "Takes the preset's definition for held terms and records the IRI each affected " +
		"property resolves to today, so edges written under the old IRI still resolve. " +
		"Run `weos worker reproject` afterwards to refill the projection columns.",
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		terms, _ := cmd.Flags().GetStringSlice("term")
		if !all && len(terms) == 0 {
			return fmt.Errorf("name at least one --term, or pass --all to adopt every held term")
		}
		if all {
			// --all means "whatever is held right now", so an explicit list
			// would only narrow it; refuse rather than silently pick one.
			if len(terms) > 0 {
				return fmt.Errorf("pass either --all or --term, not both")
			}
			terms = nil
		}

		deps, err := StartContainer(GetConfig())
		if err != nil {
			return err
		}
		defer func() { _ = deps.Shutdown() }()

		// An empty list means "every held term", which is exactly what --all is.
		adopted, err := deps.ResourceTypeService.AdoptContextTerms(
			cmd.Context(), args[0], args[1], terms)
		if err != nil {
			return err
		}
		if len(adopted) == 0 {
			_, _ = fmt.Fprintf(os.Stdout, "Nothing to adopt for %q — already up to date.\n", args[1])
			return nil
		}
		_, _ = fmt.Fprintf(os.Stdout, "Adopted for %q: %s\n", args[1], strings.Join(adopted, ", "))
		_, _ = fmt.Fprintln(os.Stdout,
			"Existing edges stay readable through their recorded IRIs.")
		_, _ = fmt.Fprintln(os.Stdout,
			"Run `weos worker reproject` to refill the projection columns for existing rows.")
		return nil
	},
}

func init() {
	adoptTermCmd.Flags().StringSlice("term", nil, "Term to adopt; repeat for several")
	adoptTermCmd.Flags().Bool("all", false, "Adopt every term the boot is holding for this type")
	resourceTypeCmd.AddCommand(heldTermsCmd, adoptTermCmd)
}
