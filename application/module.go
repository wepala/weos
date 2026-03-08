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

		// Event store provider
		fx.Provide(gorm.ProvideEventStore),

		// Session store provider (for pericarp auth integration)
		fx.Provide(func(cfg config.Config) sessions.Store {
			return sessions.NewCookieStore([]byte(cfg.SessionSecret))
		}),

		// Repository providers
		fx.Provide(gorm.ProvidePersonRepository),
		fx.Provide(gorm.ProvideOrganizationRepository),
		fx.Provide(gorm.ProvideResourceTypeRepository),
		fx.Provide(gorm.ProvideProjectionManager),
		fx.Provide(gorm.ProvideResourceRepository),

		// Service providers
		fx.Provide(ProvidePersonService),
		fx.Provide(ProvideOrganizationService),
		fx.Provide(ProvideResourceTypeService),
		fx.Provide(ProvideResourceService),

		// Subscribe event handlers (projections)
		fx.Invoke(subscribeEventHandlers),

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
