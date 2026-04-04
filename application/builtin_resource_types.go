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

// ensureBuiltInResourceTypes creates resource types from all presets marked as
// BuiltIn at startup, if they don't already exist.
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
		for _, bt := range preset.Types {
			if _, err := params.TypeSvc.GetBySlug(ctx, bt.Slug); err == nil {
				continue // already exists
			}
			cmd := CreateResourceTypeCommand(bt)
			if _, err := params.TypeSvc.Create(ctx, cmd); err != nil {
				params.Logger.Error(ctx, "failed to create built-in resource type",
					"slug", bt.Slug, "error", err)
			} else {
				params.Logger.Info(ctx, "created built-in resource type", "slug", bt.Slug)
			}
		}
	}
	return nil
}
