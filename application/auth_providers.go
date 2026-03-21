package application

import (
	"net/http"

	"weos/internal/config"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/providers"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"github.com/gorilla/sessions"
	"go.uber.org/fx"
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

func ProvideAuthenticationService(params struct {
	fx.In
	Registry    authapp.OAuthProviderRegistry
	Agents      authrepos.AgentRepository
	Credentials authrepos.CredentialRepository
	Sessions    authrepos.AuthSessionRepository
	Accounts    authrepos.AccountRepository
}) authapp.AuthenticationService {
	return authapp.NewDefaultAuthenticationService(
		params.Registry,
		params.Agents,
		params.Credentials,
		params.Sessions,
		params.Accounts,
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
