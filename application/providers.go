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

// This file contains Fx provider functions for application services.
// Each provider function uses fx.In struct injection to declare dependencies.
//
// Example provider pattern:
//
//	func ProvideUserService(params struct {
//		fx.In
//		Config     config.Config
//		UserRepo   repositories.UserRepository
//		Logger     entities.Logger
//		EventStore domain.EventStore
//		EventDispatcher *domain.EventDispatcher
//	}) UserServiceInterface {
//		return NewUserService(
//			params.UserRepo,
//			params.Logger,
//			params.EventStore,
//			params.EventDispatcher,
//		)
//	}
//
// For named dependencies (e.g., multiple agents of the same type),
// use fx.ResultTags in module.go and named struct tags in providers:
//
//	// In module.go:
//	fx.Provide(fx.Annotate(ProvideMyAgent, fx.ResultTags(`name:"myAgent"`)))
//
//	// In provider:
//	func ProvideMyService(params struct {
//		fx.In
//		Agent agent.Agent `name:"myAgent"`
//	}) MyServiceInterface { ... }
