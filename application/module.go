package application

import (
	"context"

	"weos/domain/entities"
	"weos/domain/repositories"
	"weos/infrastructure/database/gorm"
	"weos/infrastructure/events"
	"weos/infrastructure/logging"
	"weos/internal/config"

	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authgorm "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/database/gorm"
	"github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	"github.com/gorilla/sessions"
	"go.uber.org/fx"
	gormdb "gorm.io/gorm"
)

// Module provides all application dependencies.
// It accepts a Config parameter that must be provided by the calling application.
func Module(cfg config.Config, registry *PresetRegistry) fx.Option {
	return fx.Module("application",
		// Provide the config and preset registry to all providers
		fx.Provide(func() config.Config {
			return cfg
		}),
		fx.Provide(func() *PresetRegistry {
			return registry
		}),

		// Logging providers
		fx.Provide(logging.ProvideZapLogger),
		fx.Provide(logging.ProvideLogger),

		// Event dispatcher provider
		fx.Provide(events.ProvideEventDispatcher),

		// Database providers
		fx.Provide(gorm.ProvideGormDB),

		// Event store provider (with optional BigQuery dual-write)
		fx.Provide(gorm.ProvideEventStore),
		fx.Decorate(func(lc fx.Lifecycle, primary domain.EventStore, cfg config.Config, logger entities.Logger) domain.EventStore {
			if cfg.BigQueryProjectID == "" {
				return primary
			}
			bqStore, err := events.ProvideBigQueryEventStore(cfg)
			if err != nil || bqStore == nil {
				logger.Error(context.Background(), "failed to create BigQuery event store, using primary only",
					"error", err)
				return primary
			}
			lc.Append(fx.Hook{OnStop: func(_ context.Context) error { return bqStore.Close() }})
			logger.Info(context.Background(), "BigQuery dual-write enabled",
				"project", cfg.BigQueryProjectID, "dataset", cfg.BigQueryDatasetID)
			return events.NewDualWriteEventStore(primary, bqStore)
		}),

		// Session store provider (for pericarp auth integration)
		fx.Provide(func(cfg config.Config) sessions.Store {
			return sessions.NewCookieStore([]byte(cfg.SessionSecret))
		}),

		// Repository providers
		fx.Provide(gorm.ProvideResourceTypeRepository),
		fx.Provide(gorm.ProvideProjectionManager),
		fx.Provide(gorm.ProvideResourceRepository),
		fx.Provide(gorm.ProvideSidebarSettingsRepository),
		fx.Provide(gorm.ProvideRoleSettingsRepository),
		fx.Provide(gorm.ProvideRoleResourceAccessRepository),
		fx.Provide(gorm.ProvideTripleRepository),
		fx.Provide(gorm.ProvideResourcePermissionRepository),

		// Auth repositories (from pericarp)
		fx.Provide(func(db *gormdb.DB) authrepos.AgentRepository { return authgorm.NewAgentRepository(db) }),
		fx.Provide(func(db *gormdb.DB) authrepos.CredentialRepository { return authgorm.NewCredentialRepository(db) }),
		fx.Provide(func(db *gormdb.DB) authrepos.AuthSessionRepository {
			return authgorm.NewAuthSessionRepository(db)
		}),
		fx.Provide(func(db *gormdb.DB) authrepos.AccountRepository { return authgorm.NewAccountRepository(db) }),

		// Auth infrastructure
		fx.Provide(ProvideOAuthProviderRegistry),
		fx.Provide(ProvideAuthorizationChecker),
		fx.Provide(ProvideAuthenticationService),
		fx.Provide(ProvideSessionManager),

		// Resource behavior registry (must come before ProvideResourceService)
		fx.Provide(ProvideResourceBehaviorRegistry),

		// Service providers
		fx.Provide(ProvideResourceTypeService),
		fx.Provide(ProvideResourceService),
		fx.Provide(ProvideResourcePermissionService),

		// Subscribe event handlers (projections)
		fx.Invoke(subscribeEventHandlers),

		// Ensure built-in resource types and projection tables at startup
		fx.Invoke(ensureBuiltInResourceTypes),
		fx.Invoke(ensureProjectionTables),
	)
}

func ensureProjectionTables(params struct {
	fx.In
	ProjMgr repositories.ProjectionManager
}) error {
	return params.ProjMgr.EnsureExistingTables(context.Background())
}
