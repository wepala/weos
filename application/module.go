package application

import (
	"weos/infrastructure/database/gorm"
	"weos/infrastructure/events"
	"weos/infrastructure/logging"
	"weos/internal/config"
	"weos/pkg/identity"

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

		// Set the identity base path from config
		fx.Invoke(func(cfg config.Config) {
			identity.SetBasePath(cfg.IdentityBasePath)
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

		// TODO: Add repository providers here, e.g.:
		// fx.Provide(gorm.ProvideUserRepository),

		// TODO: Add EventStore provider for event sourcing, e.g.:
		// fx.Provide(gorm.ProvideEventStore),

		// TODO: Add service providers here, e.g.:
		// fx.Provide(ProvideUserService),

		// TODO: Subscribe event handlers, e.g.:
		// fx.Invoke(SubscribeEventHandlers),

		// TODO: Add lifecycle hooks for startup tasks, e.g.:
		// fx.Invoke(func(lc fx.Lifecycle, db *gormlib.DB) {
		// 	lc.Append(fx.Hook{
		// 		OnStart: func(ctx context.Context) error { return nil },
		// 		OnStop:  func(ctx context.Context) error { return nil },
		// 	})
		// }),
	)
}
