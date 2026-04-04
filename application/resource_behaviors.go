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
	"weos/domain/entities"
)

// ResourceBehaviorRegistry maps resource type slugs to their custom behaviors.
// Types without a registered behavior use DefaultBehavior (no-op).
type ResourceBehaviorRegistry map[string]entities.ResourceBehavior

// ProvideResourceBehaviorRegistry builds the behavior registry from all
// registered presets. Each preset can declare behaviors for its resource types.
func ProvideResourceBehaviorRegistry(registry *PresetRegistry) ResourceBehaviorRegistry {
	return registry.Behaviors()
}
