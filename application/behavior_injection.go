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

// InjectBehaviorDependencies walks the resource behavior registry and calls
// SetDependencies on every behavior that implements DependencyInjectable.
// This lets behaviors registered at preset init time receive runtime services
// (such as ResourceService) after the DI container has built them.
func InjectBehaviorDependencies(
	registry ResourceBehaviorRegistry,
	resourceSvc ResourceService,
	logger entities.Logger,
) {
	deps := entities.BehaviorDependencies{
		ResourceSvc: resourceSvc,
		Logger:      logger,
	}
	for _, b := range registry {
		if injectable, ok := b.(entities.DependencyInjectable); ok {
			injectable.SetDependencies(deps)
		}
	}
}
