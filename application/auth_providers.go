package application

import (
	"fmt"
	"net/http"

	"github.com/wepala/weos/v3/internal/config"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authcasbin "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/casbin"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/providers"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
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
		registry["netsuite"] = providers.NewNetSuite(providers.NetSuiteConfig{
			ClientID:     cfg.NetSuiteClientID,
			ClientSecret: cfg.NetSuiteClientSecret,
			AccountID:    cfg.NetSuiteAccountID,
			Scopes:       cfg.NetSuiteScopes,
		})
	}
	return registry
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
	JWTService          authapp.JWTService `optional:"true"`
}) authapp.AuthenticationService {
	opts := []authapp.AuthServiceOption{
		authapp.WithAuthorizationChecker(params.AuthzChecker),
		authapp.WithPasswordCredentialRepository(params.PasswordCredentials),
	}
	if params.JWTService != nil {
		opts = append(opts, authapp.WithJWTService(params.JWTService))
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
