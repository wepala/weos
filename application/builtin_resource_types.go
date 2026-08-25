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

package application

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/wepala/weos/v3/domain/entities"

	"go.uber.org/fx"
)

// ensureBuiltInResourceTypes installs all presets marked as AutoInstall at
// startup, creating resource types and seeding fixture data if they don't
// already exist. After every auto-install finishes, a single LinkActivator
// reconcile runs so cross-preset links whose endpoints are now both present
// activate at startup regardless of preset install order.
func ensureBuiltInResourceTypes(params struct {
	fx.In
	Registry      *PresetRegistry
	TypeSvc       ResourceTypeService
	Logger        entities.Logger
	LinkActivator *LinkActivator `optional:"true"`
	Links         *LinkRegistry  `optional:"true"`
}) error {
	ctx := context.Background()
	for _, preset := range params.Registry.List() {
		// The schema reconcile runs for EVERY registered preset, not just the
		// auto-installed ones. Only two presets in the tree set AutoInstall, so
		// gating the reconcile on it would leave issue #379 unfixed for every
		// preset an operator installs on demand — including any shipped by a
		// private registrar. A preset type that isn't installed in this database
		// is a no-op inside the reconcile, so running it unconditionally is safe.
		reconcilePresetSchemas(ctx, params.TypeSvc, params.Logger, preset.Name)

		if !preset.AutoInstall {
			continue
		}
		result, err := params.TypeSvc.InstallPreset(ctx, preset.Name, false)
		if err != nil {
			params.Logger.Error(ctx, "failed to install built-in preset",
				"preset", preset.Name, "error", err)
		}
		if result == nil {
			continue
		}
		for _, slug := range result.Created {
			params.Logger.Info(ctx, "created built-in resource type", "slug", slug)
		}
		for slug, count := range result.Seeded {
			if count <= 0 {
				continue
			}
			params.Logger.Info(ctx, "seeded built-in fixture data",
				"slug", slug, "count", count)
		}
	}
	// Ambiguity is checked AFTER both the reconcile and the installs above, so a
	// preset that ships ambiguous is caught on the boot that installs it rather
	// than the one after. It runs every boot, not only when something changed:
	// a type becomes ambiguous when a later build adds a property onto an
	// existing predicate, and by then data already exists under the property
	// that loses (issue #515).
	reportAmbiguousReferences(ctx, params.TypeSvc, params.Logger, params.Links)

	// InstallPreset already reconciles after each install, but presets install
	// one at a time in the loop above — if preset B depends on preset A's
	// types, the activation during A's install won't know B exists yet. A
	// single terminal reconcile catches any link whose endpoints ended up
	// both installed across the whole auto-install sequence.
	//
	// A reconcile error is returned so Fx's invoke machinery fails startup.
	// Link activation is load-bearing for correct FK/projection columns;
	// booting a service with a silently-broken link graph is worse than
	// refusing to start, since clients would see missing display values and
	// other denormalized link projections even though triple extraction/linking
	// can still occur for affected references.
	if params.LinkActivator != nil {
		if err := params.LinkActivator.Reconcile(ctx); err != nil {
			params.Logger.Error(ctx, "terminal link reconcile failed", "error", err)
			return fmt.Errorf("terminal link reconcile: %w", err)
		}
	}
	return nil
}

// reconcilePresetSchemas merges a preset's additive schema AND `@context`
// changes into the types already stored in this database, and reports every
// outcome that means data is still being dropped (issues #379 and #510).
//
// A reconcile failure is logged, not fatal. That is a deliberate asymmetry with
// the terminal LinkActivator reconcile above, which does fail startup: link
// activation is all-or-nothing for a whole deployment, whereas a per-type
// schema failure is contained to that type, and refusing to boot the entire
// service over one type would take a running system down to fix a subset of
// writes. Every failing type is named in the log instead — the operator signal
// that was missing before this existed.
func reconcilePresetSchemas(
	ctx context.Context, svc ResourceTypeService, logger entities.Logger, presetName string,
) {
	reconciled, err := svc.ReconcilePresetSchemas(ctx, presetName)
	if err != nil {
		logger.Error(ctx, "failed to reconcile preset",
			"preset", presetName, "error", err)
	}
	if reconciled == nil {
		return
	}
	for _, slug := range reconciled.Updated {
		logger.Info(ctx, "reconciled resource type schema and context from preset",
			"preset", presetName, "slug", slug)
	}
	for slug, held := range reconciled.Refused {
		// Held at their stored definition, not applied. The type's additive
		// properties still landed, so this is a narrower warning than it used to
		// be: only these properties need an operator decision.
		logger.Warn(ctx,
			"resource type properties held at their stored definition: preset diverges non-additively",
			"preset", presetName, "slug", slug, "heldProperties", held)
	}
	for slug, held := range reconciled.RefusedContext {
		// A term whose stored IRI differs from the one the preset now declares —
		// an operator edit, or a preset that repointed its own term between
		// builds, which is the commoner cause. Held rather than overwritten,
		// because existing edges are already keyed by the stored IRI and
		// repointing it would orphan them (issues #510, #513).
		logger.Warn(ctx,
			"resource type context terms held at their stored definition: preset declares a different IRI",
			"preset", presetName, "slug", slug, "heldContextTerms", held)
	}
	for slug, terms := range reconciled.Repointed {
		// Phrased as held at their stored definition like the divergence case,
		// because that is what an operator sees happen — but the reason and the
		// remedy differ, so the remedy is named here.
		logger.Warn(ctx,
			"resource type context terms held at their stored definition: adopting them would "+
				"repoint a predicate that already has data",
			"preset", presetName, "slug", slug, "heldContextTerms", terms,
			"remedy", AdoptRemedy(presetName, slug, terms))
	}
	for slug, reason := range reconciled.UnparseableContext {
		logger.Error(ctx,
			"resource type @context could not be read; merged its schema only",
			"preset", presetName, "slug", slug, "reason", reason)
	}
	for slug, reason := range reconciled.Failed {
		// The reason names what actually failed: a projection column that never
		// appeared, a reference property with no `@context` term to resolve
		// through, or both. Either way writes to those properties are dropped.
		logger.Error(ctx,
			"resource type NOT reconciled: writes to some of its properties will be dropped",
			"preset", presetName, "slug", slug, "reason", reason)
	}
	for _, slug := range reconciled.NoSchema {
		logger.Warn(ctx,
			"preset type declares no JSON Schema: its projection has only base columns",
			"preset", presetName, "slug", slug)
	}
}

// listAllTypesLimit is a ceiling for the boot sweep. A deployment with more
// installed types than this has other problems; a page size stops a runaway
// query from stalling startup.
const listAllTypesLimit = 1000

// reportAmbiguousReferences names every INSTALLED type whose reference
// properties no reader can tell apart, and what to do about it.
//
// It enumerates installed types rather than preset declarations: a type
// created through the API or MCP belongs to no preset, and one whose preset is
// no longer registered would drop out of a preset-driven sweep while its data
// stays. Both are exactly the populations that carry a hand-authored shape.
//
// Reported, not fatal, matching how a per-type reconcile failure is handled: a
// bad shape on one type is contained to that type, and refusing to boot the
// whole service over it would take a running system down to fix a subset of
// writes. The message carries both remedies because which one is meant depends
// on intent — see ambiguousReferenceShape.
func reportAmbiguousReferences(
	ctx context.Context, svc ResourceTypeService, logger entities.Logger, links *LinkRegistry,
) {
	if links == nil {
		links = NewLinkRegistry()
	}
	page, err := svc.List(ctx, "", listAllTypesLimit)
	if err != nil {
		// Saying nothing would make a transient fault look like a clean boot.
		logger.Warn(ctx, "could not list resource types to check their reference shapes", "error", err)
		return
	}
	for _, existing := range page.Data {
		ReportAmbiguousReferenceShape(ctx, logger, existing.Slug(),
			existing.Schema(), existing.Context(), links.BySource(existing.Slug()))
	}
}

// ReportAmbiguousReferenceShape names every pair of reference properties on one
// type that no reader can tell apart, and what to do about it.
//
// Exported and called from the resource-type write path as well as from boot,
// because a type created through the API or MCP belongs to no preset and the
// boot sweep never sees it — which is precisely the population that hand-authors
// several ID fields onto one relation. An operator learns when they define the
// shape rather than from a boot log they may never read.
//
// Reported, not fatal, matching how a per-type reconcile failure is handled: a
// bad shape on one type is contained to that type. Warn rather than Error
// because it repeats on every boot with no way to acknowledge it, and an
// operator who cannot change a shipped preset would otherwise have an alert
// firing forever over a line they cannot act on.
func ReportAmbiguousReferenceShape(
	ctx context.Context, logger entities.Logger, slug string,
	schema, ldContext json.RawMessage, links []PresetLinkDefinition,
) {
	refs := ExtractReferencePropertiesWithLinks(schema, ldContext, links)
	groups := ambiguousReferenceShape(refs)
	if len(groups) == 0 {
		return
	}
	for _, names := range groups {
		predicate, target := "", ""
		for _, ref := range refs {
			if ref.PropertyName == names[0] {
				predicate, target = ref.PredicateIRI, ref.TargetType
				break
			}
		}
		logger.Warn(ctx,
			"ambiguous reference shape: these properties resolve to one predicate and one "+
				"target type, so no reader can tell them apart and one stands in for the other. "+
				"Give the relationships different predicates if they differ, or collapse them "+
				"into a single array property if they are one relationship with several targets",
			"slug", slug, "properties", names, "predicate", predicate, "targetType", target)
	}
}

// AdoptRemedy is the command an operator runs to adopt the held terms of a
// type. A sweep (--all) deliberately never takes `@type` — an alias cannot
// move a class — so a held class is named explicitly, and a type holding
// both a class and other terms is given both commands. Printing the sweep
// alone for a held class sent the operator to a command that adopted
// nothing and left the boot warning forever (issue #521).
func AdoptRemedy(presetName, slug string, held []string) string {
	base := "weos resource-type adopt-term " + presetName + " " + slug
	var classHeld, othersHeld bool
	for _, term := range held {
		if term == "@type" {
			classHeld = true
		} else {
			othersHeld = true
		}
	}
	switch {
	case classHeld && othersHeld:
		return base + " --all; then " + base + " --term @type (a sweep never moves the class)"
	case classHeld:
		return base + " --term @type (a sweep never moves the class; re-stamp and reproject afterwards)"
	default:
		return base + " --all"
	}
}
