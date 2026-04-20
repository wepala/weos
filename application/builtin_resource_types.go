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
