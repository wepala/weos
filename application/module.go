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

	"weos/domain/repositories"
	"weos/infrastructure/database/gorm"
	"weos/infrastructure/events"
	"weos/infrastructure/logging"
	"weos/internal/config"

	"github.com/gorilla/sessions"
	"go.uber.org/fx"
)

// Module provides all application dependencies.
// It accepts a Config parameter that must be provided by the calling application.
func Module(cfg config.Config) fx.Option {
	return fx.Module("application",
		// Provide the config to all providers that need it
		fx.Provide(func() config.Config {
			return cfg
		}),

		// Logging providers
		fx.Provide(logging.ProvideZapLogger),
		fx.Provide(logging.ProvideLogger),

		// Event dispatcher provider
		fx.Provide(events.ProvideEventDispatcher),

		// Database providers
		fx.Provide(gorm.ProvideGormDB),

		// Session store provider (for pericarp auth integration)
		fx.Provide(func(cfg config.Config) sessions.Store {
			return sessions.NewCookieStore([]byte(cfg.SessionSecret))
		}),

		// Repository providers
		fx.Provide(gorm.ProvidePersonRepository),
		fx.Provide(gorm.ProvideOrganizationRepository),

		// Service providers
		fx.Provide(ProvidePersonService),
		fx.Provide(ProvideOrganizationService),
		fx.Provide(gorm.ProvideResourceTypeRepository),
		fx.Provide(gorm.ProvideProjectionManager),
		fx.Provide(gorm.ProvideResourceRepository),
		fx.Provide(ProvideResourceTypeService),
		fx.Provide(ProvideResourceService),

		// Ensure projection tables for existing resource types at startup
		fx.Invoke(ensureProjectionTables),
	)
}

func ensureProjectionTables(params struct {
	fx.In
	ProjMgr repositories.ProjectionManager
}) error {
	return params.ProjMgr.EnsureExistingTables(context.Background())
}
