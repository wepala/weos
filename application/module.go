package application

import (
	"context"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	"github.com/wepala/weos/v3/infrastructure/auth"
	"github.com/wepala/weos/v3/infrastructure/database/gorm"
	"github.com/wepala/weos/v3/infrastructure/email"
	"github.com/wepala/weos/v3/infrastructure/events"
	"github.com/wepala/weos/v3/infrastructure/logging"
	storageprovider "github.com/wepala/weos/v3/infrastructure/storage/provider"
	"github.com/wepala/weos/v3/internal/config"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
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
	if registry == nil {
		panic("application.Module: PresetRegistry must not be nil — use presets.NewDefaultRegistry()")
	}
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
			return events.NewDualWriteEventStore(primary, bqStore, logger)
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
		fx.Provide(func(db *gormdb.DB) authrepos.InviteRepository { return authgorm.NewInviteRepository(db) }),
		fx.Provide(func(db *gormdb.DB) authrepos.PasswordCredentialRepository {
			return authgorm.NewPasswordCredentialRepository(db)
		}),

		// Auth infrastructure
		fx.Provide(ProvideOAuthProviderRegistry),
		fx.Provide(ProvideAuthorizationChecker),
		fx.Provide(ProvideAuthenticationService),
		fx.Provide(ProvideSessionManager),
		fx.Provide(auth.ProvideInviteTokenService),
		fx.Provide(func(
			invites authrepos.InviteRepository,
			agents authrepos.AgentRepository,
			accounts authrepos.AccountRepository,
			creds authrepos.CredentialRepository,
			tokenSvc authapp.InviteTokenService,
			logger entities.Logger,
		) *authapp.InviteService {
			return authapp.NewInviteService(invites, agents, accounts, creds, tokenSvc,
				authapp.WithInviteLogger(logger),
			)
		}),

		// Resource behavior registries (must come before ProvideResourceService).
		// The lazy resource writer breaks the construction cycle between
		// ResourceService and ResourceBehaviorRegistry; WireResourceWriter
		// (fx.Invoke below) installs the real service into it at startup.
		fx.Provide(newLazyResourceWriter),
		fx.Provide(ProvideResourceBehaviorRegistry),
		fx.Provide(ProvideBehaviorMetaRegistry),
		fx.Provide(ProvidePresetHTTPHandlers),
		fx.Provide(gorm.ProvideBehaviorSettingsRepository),

		// Email sender
		fx.Provide(email.ProvideEmailSender),

		// LinkRegistry seeded from preset.Links + package-init RegisterLink calls.
		// Both entry points converge here so the service wiring sees one
		// authoritative registry. Also exposed as a repositories.LinkSource so
		// the projection manager can replay link refs after schema re-parse.
		fx.Provide(func(r *PresetRegistry, logger entities.Logger) *LinkRegistry {
			return buildLinkRegistry(r, logger)
		}),
		fx.Provide(func(r *LinkRegistry) repositories.LinkSource { return r }),
		fx.Provide(ProvideLinkActivator),

		// Service providers
		fx.Provide(ProvideResourceTypeService),
		fx.Provide(ProvideResourceService),
		fx.Provide(ProvideResourcePermissionService),
		fx.Provide(storageprovider.ProvideFileService),

		// Install the real ResourceService into the lazy writer proxy now that
		// both exist. Behaviors close over the proxy at factory time; this
		// invoke must run before any hook can be called.
		fx.Invoke(WireResourceWriter),

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

// buildLinkRegistry seeds a process-local LinkRegistry from two sources:
// every link declared in a registered preset's PresetDefinition.Links slice,
// and every link added to DefaultLinkRegistry() via application.RegisterLink
// before the Fx container starts up. The same (SourceType, PropertyName)
// key dedups within the registry — last write wins — so a preset-declared
// link is effectively replaced if a package-init RegisterLink for the same
// key runs later.
func buildLinkRegistry(registry *PresetRegistry, logger entities.Logger) *LinkRegistry {
	out := NewLinkRegistry()
	if registry != nil {
		for _, def := range registry.List() {
			for _, link := range def.Links {
				if err := out.Add(link); err != nil {
					if logger != nil {
						logger.Error(context.Background(),
							"invalid preset link definition ignored",
							"preset", def.Name, "error", err)
					}
				}
			}
		}
	}
	for _, link := range DefaultLinkRegistry().All() {
		if err := out.Add(link); err != nil {
			if logger != nil {
				logger.Error(context.Background(),
					"invalid RegisterLink definition ignored",
					"error", err)
			}
		}
	}
	return out
}

// ProvideLinkActivator constructs the activator used to reconcile link
// definitions after resource types are installed. It depends on the
// ProjectionManager and the ResourceTypeRepository because activation needs
// to know which types are installed (repo) and where to add FK columns
// (projection manager). Returns an error (surfaced by Fx) if any dependency
// is missing — preferable to a runtime panic in Reconcile.
func ProvideLinkActivator(params struct {
	fx.In
	Registry *LinkRegistry
	ProjMgr  repositories.ProjectionManager
	TypeRepo repositories.ResourceTypeRepository
	Logger   entities.Logger
}) (*LinkActivator, error) {
	return NewLinkActivator(params.Registry, params.ProjMgr, params.TypeRepo, params.Logger)
}
