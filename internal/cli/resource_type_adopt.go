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
	"io"
	"os"
	"strings"

	"github.com/wepala/weos/v3/application"

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
		currentClass := ""
		if rt, rtErr := deps.ResourceTypeService.GetBySlug(cmd.Context(), args[1]); rtErr == nil {
			currentClass = application.ResourceTypeClassIRI(rt.Name(), rt.Slug(), rt.Context())
		}
		printHeldTerms(os.Stdout, args[0], args[1], currentClass, held)
		return nil
	},
}

// printHeldTerms renders the held terms of a type and the command that
// adopts them. A held `@type` is a class, not a predicate: it keys no edge,
// so it names no stored IRI, and adopting it changes only what NEW writes
// carry — existing records need a re-stamp.
func printHeldTerms(out io.Writer, preset, slug, currentClass string, held []application.HeldTerm) {
	if len(held) == 0 {
		_, _ = fmt.Fprintf(out, "No held terms for %q.\n", slug)
		return
	}
	_, _ = fmt.Fprintf(out, "%d held term(s) for %q:\n\n", len(held), slug)
	terms := make([]string, 0, len(held))
	var classMovers []string
	for _, h := range held {
		terms = append(terms, h.Term)
		kind := "the preset ADDS this term"
		if h.Kind == application.HeldTermRedefined {
			kind = "the preset REDEFINES this term"
		}
		_, _ = fmt.Fprintf(out, "  %s  (%s)\n", h.Term, kind)
		if h.MovesClass() && h.Term != "@type" {
			classMovers = append(classMovers, h.Term)
		}
		if h.Term == "@type" {
			stored := h.StoredIRI
			if stored == "" && currentClass != "" {
				stored = currentClass + " (no class declared; the type name through @vocab)"
			}
			_, _ = fmt.Fprintf(out, "    class today     %s\n", stored)
			_, _ = fmt.Fprintf(out, "    preset declares %s\n", h.PresetIRI)
			_, _ = fmt.Fprintln(out, "    adopting it declares the class for NEW writes; existing "+
				"records need `weos worker normalize-edge-keys --restamp`, then `weos worker reproject` and "+
				"`weos worker checkpoint reset oxigraph --truncate`")
			_, _ = fmt.Fprintln(out)
			continue
		}
		if h.Kind == application.HeldTermRedefined && (len(h.Moves) > 1 || len(h.Moves) == 0 || h.Moves[0].Property != h.Term) {
			_, _ = fmt.Fprintf(out, "    stored as       %s\n", h.StoredIRI)
			_, _ = fmt.Fprintf(out, "    preset wants    %s\n", h.PresetIRI)
		}
		for _, m := range h.Moves {
			if m.Property == "@type" {
				_, _ = fmt.Fprintf(out, "    moves the class %s -> %s (existing records need a re-stamp)\n",
					m.StoredIRI, m.PresetIRI)
				continue
			}
			_, _ = fmt.Fprintf(out, "    property        %s\n", m.Property)
			_, _ = fmt.Fprintf(out, "    data written as %s\n", m.StoredIRI)
			_, _ = fmt.Fprintf(out, "    preset wants    %s\n", m.PresetIRI)
			_, _ = fmt.Fprintf(out, "    adopting keeps edges under %s readable (recorded as an alias)\n", m.StoredIRI)
		}
		_, _ = fmt.Fprintln(out)
	}
	_, _ = fmt.Fprintf(out, "Adopt with:  %s\n", application.AdoptRemedy(preset, slug, terms, classMovers))
	if note := application.AdoptRemedyNote(terms, classMovers); note != "" {
		_, _ = fmt.Fprintf(out, "Note:        %s\n", strings.ReplaceAll(note, "<slug>", slug))
	}
}

// printAdoptOutcome renders what adopt-term did. A sweep never takes the
// class (nor @vocab), so when one is still held afterwards the operator is
// told so and handed the command that adopts it — never "already up to
// date". Every adoption is followed by the same route: the alias keeps the
// API and projection reads working, but a reprojection replays payloads
// verbatim, so the triples table and the knowledge graph keep the OLD
// predicate until existing records are re-stamped. A moved class is named
// as well.
func printAdoptOutcome(out io.Writer, preset, slug string, sweep bool, result application.AdoptResult,
	stillHeld []application.HeldTerm) {
	// What the sweep left is the service's own answer (result.StillHeld);
	// the held-terms listing only says which of them move the class.
	leftHeld := append([]string(nil), result.StillHeld...)
	var classMovers []string
	classStillHeld, dependent := false, false
	for _, h := range stillHeld {
		if h.MovesClass() {
			classStillHeld = true
			if h.Term != "@type" {
				classMovers = append(classMovers, h.Term)
			}
		}
		if h.MovesClass() || h.Term == "@vocab" {
			leftHeld = appendUnique(leftHeld, h.Term)
		}
	}
	for _, term := range leftHeld {
		if term != "@type" && term != "@vocab" && !contains(classMovers, term) {
			dependent = true
		}
	}
	if sweep && len(leftHeld) > 0 {
		what := strings.Join(leftHeld, ", ")
		if classStillHeld {
			what += " (the class)"
		}
		why := "a sweep never moves the class or @vocab"
		if dependent {
			why += ", nor a term written against a prefix it left held"
		}
		_, _ = fmt.Fprintf(out, "%s is still held for %q: %s. Adopt it with: %s\n",
			what, slug, why, application.AdoptRemedy(preset, slug, leftHeld, classMovers))
	}
	if len(result.Adopted) == 0 {
		if len(leftHeld) > 0 {
			_, _ = fmt.Fprintf(out, "Nothing else to adopt for %q.\n", slug)
		} else {
			_, _ = fmt.Fprintf(out, "Nothing to adopt for %q — already up to date.\n", slug)
		}
		return
	}
	_, _ = fmt.Fprintf(out, "Adopted for %q: %s\n", slug, strings.Join(result.Adopted, ", "))
	_, _ = fmt.Fprintln(out, "Existing edges stay readable through their recorded IRIs (API and projection reads).")
	if result.ClassMove != nil {
		_, _ = fmt.Fprintf(out, "The %q class moves from %s to %s for NEW writes.\n", slug,
			result.ClassMove.StoredIRI, result.ClassMove.PresetIRI)
	}
	_, _ = fmt.Fprintf(out, "The triples table and the knowledge graph keep the old IRIs until existing records are "+
		"re-stamped: `weos worker normalize-edge-keys --restamp --type %s --write`, then `weos worker reproject` and "+
		"`weos worker checkpoint reset oxigraph --truncate`.\n", slug)
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
		result, err := deps.ResourceTypeService.AdoptContextTerms(
			cmd.Context(), args[0], args[1], terms)
		if err != nil {
			return err
		}
		// The service already says what a sweep left; the listing is only
		// consulted to tell which of those move the class, so failing to
		// fetch it degrades the wording, not the answer — and says so.
		stillHeld, hErr := deps.ResourceTypeService.HeldContextTerms(cmd.Context(), args[0], args[1])
		if hErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: could not list the held terms after adoption: %v\n", hErr)
			stillHeld = nil
		}
		printAdoptOutcome(os.Stdout, args[0], args[1], all, result, stillHeld)
		return nil
	},
}

func init() {
	adoptTermCmd.Flags().StringSlice("term", nil, "Term to adopt; repeat for several")
	adoptTermCmd.Flags().Bool("all", false, "Adopt every term the boot is holding for this type")
	resourceTypeCmd.AddCommand(heldTermsCmd, adoptTermCmd)
}

func appendUnique(list []string, s string) []string {
	if contains(list, s) {
		return list
	}
	return append(list, s)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
