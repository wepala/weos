package application

import (
	"context"
	"fmt"
	"net/http"

	"github.com/wepala/weos/v3/internal/config"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authcasbin "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/casbin"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/providers"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	esdomain "github.com/akeemphilbert/pericarp/pkg/eventsourcing/domain"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/gorilla/sessions"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

func ProvideOAuthProviderRegistry(params struct {
	fx.In
	Config config.Config
}) authapp.OAuthProviderRegistry {
	registry := make(authapp.OAuthProviderRegistry)
	if !params.Config.OAuthEnabled() {
		return registry
	}
	cfg := params.Config.OAuth
	if cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "" {
		registry["google"] = providers.NewGoogle(providers.GoogleConfig{
			ClientID:     cfg.GoogleClientID,
			ClientSecret: cfg.GoogleClientSecret,
		})
	}
	if cfg.NetSuiteClientID != "" && cfg.NetSuiteClientSecret != "" && cfg.NetSuiteAccountID != "" {
		registry["netsuite"] = &displayNameFallback{OAuthProvider: providers.NewNetSuite(providers.NetSuiteConfig{
			ClientID:     cfg.NetSuiteClientID,
			ClientSecret: cfg.NetSuiteClientSecret,
			AccountID:    cfg.NetSuiteAccountID,
			Scopes:       cfg.NetSuiteScopes,
		})}
	}
	return registry
}

// displayNameFallback wraps an OAuthProvider so that UserInfo.DisplayName is
// never empty when it reaches FindOrCreateAgent. NetSuite's userinfo response
// frequently omits both `name` and `preferred_username`, leaving DisplayName
// empty — and pericarp's agent.With rejects empty names with
// "agent name cannot be empty", which surfaces to the browser as the opaque
// "failed to find or create agent". Falling back to Email then ProviderUserID
// is enough to unblock first login; admins can rename the user afterwards.
type displayNameFallback struct {
	authapp.OAuthProvider
}

func (w *displayNameFallback) Exchange(ctx context.Context, code, codeVerifier, redirectURI string) (*authapp.AuthResult, error) {
	res, err := w.OAuthProvider.Exchange(ctx, code, codeVerifier, redirectURI)
	if res != nil {
		fillDisplayName(&res.UserInfo)
	}
	return res, err
}

func (w *displayNameFallback) RefreshToken(ctx context.Context, refreshToken string) (*authapp.AuthResult, error) {
	res, err := w.OAuthProvider.RefreshToken(ctx, refreshToken)
	if res != nil {
		fillDisplayName(&res.UserInfo)
	}
	return res, err
}

func fillDisplayName(ui *authapp.UserInfo) {
	if ui.DisplayName != "" {
		return
	}
	if ui.Email != "" {
		ui.DisplayName = ui.Email
		return
	}
	ui.DisplayName = ui.ProviderUserID
}

func ProvideAuthorizationChecker(db *gorm.DB) (*authcasbin.CasbinAuthorizationChecker, error) {
	adapter, err := gormadapter.NewAdapterByDB(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create casbin gorm adapter: %w", err)
	}
	return authcasbin.NewCasbinAuthorizationChecker(adapter)
}

func ProvideAuthenticationService(params struct {
	fx.In
	Registry            authapp.OAuthProviderRegistry
	Agents              authrepos.AgentRepository
	Credentials         authrepos.CredentialRepository
	Sessions            authrepos.AuthSessionRepository
	Accounts            authrepos.AccountRepository
	PasswordCredentials authrepos.PasswordCredentialRepository
	AuthzChecker        *authcasbin.CasbinAuthorizationChecker
	EventStore          esdomain.EventStore         `optional:"true"`
	EventDispatcher     *esdomain.EventDispatcher   `optional:"true"`
	JWTService          authapp.JWTService          `optional:"true"`
}) authapp.AuthenticationService {
	opts := []authapp.AuthServiceOption{
		authapp.WithAuthorizationChecker(params.AuthzChecker),
		authapp.WithPasswordCredentialRepository(params.PasswordCredentials),
	}
	if params.JWTService != nil {
		opts = append(opts, authapp.WithJWTService(params.JWTService))
	}
	// Wiring both lets pericarp's auth aggregates persist their events
	// AND broadcast them to subscribers (e.g. kulr's AgentCreated listener).
	// WithEventDispatcher is a no-op inside pericarp unless WithEventStore
	// is also set, so they're conditionally paired.
	if params.EventStore != nil {
		opts = append(opts, authapp.WithEventStore(params.EventStore))
		if params.EventDispatcher != nil {
			opts = append(opts, authapp.WithEventDispatcher(params.EventDispatcher))
		}
	}
	return authapp.NewDefaultAuthenticationService(
		params.Registry,
		params.Agents,
		params.Credentials,
		params.Sessions,
		params.Accounts,
		opts...,
	)
}

func ProvideSessionManager(params struct {
	fx.In
	Config config.Config
	Store  sessions.Store
}) session.SessionManager {
	opts := session.DefaultSessionOptions()
	if params.Config.SessionSecret == "change-me-in-production" {
		opts.Secure = false
		opts.SameSite = http.SameSiteLaxMode
	}
	return session.NewGorillaSessionManager("weos-session", params.Store, opts)
}
