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

package mealplanning

import (
	"sync"

	"weos/application"
	"weos/domain/entities"
)

// Note: the application layer owns the generic InjectBehaviorDependencies
// function (in application/behavior_injection.go). This package provides
// only the concrete behavior implementations.

// baseBehavior holds the dependencies injected after DI construction.
// All meal-planning behaviors embed this (and entities.DefaultBehavior)
// so they share the same dependency wiring.
type baseBehavior struct {
	entities.DefaultBehavior
	mu          sync.RWMutex
	resourceSvc application.ResourceService
	logger      entities.Logger
}

// SetDependencies stores the injected services. Called by InjectDependencies
// after the DI container has built ResourceService.
func (b *baseBehavior) SetDependencies(deps entities.BehaviorDependencies) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if svc, ok := deps.ResourceSvc.(application.ResourceService); ok {
		b.resourceSvc = svc
	}
	b.logger = deps.Logger
}

// svc returns the injected ResourceService (nil-safe).
func (b *baseBehavior) svc() application.ResourceService {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.resourceSvc
}

// log returns the injected logger (nil-safe).
func (b *baseBehavior) log() entities.Logger {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.logger
}
