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

// reconcilePresetSchemas merges a preset's additive schema changes into the
// types already stored in this database and reports every outcome that means
// data is still being dropped (issue #379).
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
		logger.Error(ctx, "failed to reconcile preset schema",
			"preset", presetName, "error", err)
	}
	if reconciled == nil {
		return
	}
	for _, slug := range reconciled.Updated {
		logger.Info(ctx, "reconciled resource type schema from preset",
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
	for slug, reason := range reconciled.Failed {
		logger.Error(ctx,
			"resource type NOT reconciled: writes to its new properties will be dropped",
			"preset", presetName, "slug", slug, "reason", reason)
	}
	for _, slug := range reconciled.NoSchema {
		logger.Warn(ctx,
			"preset type declares no JSON Schema: its projection has only base columns",
			"preset", presetName, "slug", slug)
	}
}
