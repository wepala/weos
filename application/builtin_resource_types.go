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

	"weos/domain/entities"

	"go.uber.org/fx"
)

// ensureBuiltInResourceTypes installs all presets marked as AutoInstall at
// startup, creating resource types and seeding fixture data if they don't
// already exist.
func ensureBuiltInResourceTypes(params struct {
	fx.In
	Registry *PresetRegistry
	TypeSvc  ResourceTypeService
	Logger   entities.Logger
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
			params.Logger.Info(ctx, "seeded built-in fixture data",
				"slug", slug, "count", count)
		}
	}
	return nil
}
