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
	if params.Config.OAuthEnabled() {
		google := providers.NewGoogle(providers.GoogleConfig{
			ClientID:     params.Config.OAuth.GoogleClientID,
			ClientSecret: params.Config.OAuth.GoogleClientSecret,
		})
		registry["google"] = google
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
	Registry     authapp.OAuthProviderRegistry
	Agents       authrepos.AgentRepository
	Credentials  authrepos.CredentialRepository
	Sessions     authrepos.AuthSessionRepository
	Accounts     authrepos.AccountRepository
	AuthzChecker *authcasbin.CasbinAuthorizationChecker
}) authapp.AuthenticationService {
	return authapp.NewDefaultAuthenticationService(
		params.Registry,
		params.Agents,
		params.Credentials,
		params.Sessions,
		params.Accounts,
		authapp.WithAuthorizationChecker(params.AuthzChecker),
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
