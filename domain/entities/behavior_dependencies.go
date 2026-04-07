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

package entities

// BehaviorDependencies bundles runtime services that behaviors may need.
// ResourceSvc is typed as `any` to avoid a domain → application import cycle;
// concrete behaviors type-assert it to application.ResourceService.
type BehaviorDependencies struct {
	ResourceSvc any // application.ResourceService
	Logger      Logger
}

// DependencyInjectable is implemented by behaviors that need services
// injected after the DI container is built. Behaviors are registered at
// preset init time (before services exist), so the application layer walks
// the merged registry and calls SetDependencies on any behavior that
// implements this interface.
type DependencyInjectable interface {
	SetDependencies(deps BehaviorDependencies)
}
