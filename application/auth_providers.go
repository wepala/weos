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

// The keys the OAuth provider registry holds each provider under. They are
// also the provider recorded on a credential, which is why a trusted issuer's
// provider claim for one of these providers must name it verbatim: only then
// does a person the door verified with Google resolve to the same credential
// as the one this instance's own Google sign-in would. An assertion may also
// name OAuthProviderDoor, which is not a registry key (see OAuthProviderKeys).
const (
	OAuthProviderGoogle   = "google"
	OAuthProviderNetSuite = "netsuite"
	OAuthProviderApple    = "apple"
)

// OAuthProviderDoor is the provider a trusted issuer (the door) names for an
// identity it owns itself: a person who signed up to the door with an email
// and a password, whose address the door proved before it asserts. No registry
// entry holds this key — this instance never runs that sign-in itself — so it
// reaches an instance only through a login assertion, and only with
// email_verified true, as every other key does.
//
// A door identity and a Google or Apple identity that hold the same email are
// one person, in either order, with or without OAUTH_ALLOWED_EMAILS (decision
// wm-vvi6t). An issuer sends one provider key and one subject for each person
// it asserts, so a person who signed up to the door with a password and later
// signs in through the door with Google reaches this instance as a google
// identity it has not seen. The door writes a person only after that person
// reads a code sent to the mailbox, so the person's door credential proves who
// owns its email for that google or apple identity, which is linked to the
// person (doorCredentialProvesOwnerFor). A door identity the instance has not
// seen is linked, the same way, to a person whose google, apple or opted-in
// password credential holds its email, unless a door credential already holds
// it (below).
//
// A door credential proves nothing for another door identity. A second door
// subject for an email comes only from an operator re-creating the person at
// the door, and it is never joined to the first: while an active door
// credential of an active person holds the email, that sign-in is refused 409
// unproven-owner (ErrUnprovenOwner), whatever other credentials that person
// holds.
const OAuthProviderDoor = "door"

// OAuthProviderKeys lists every provider key a trusted issuer's assertion may
// name: every key the registry can hold, whether or not that provider is
// configured on this instance, plus OAuthProviderDoor, which the registry never
// holds. A fleet instance usually configures none — the door signs people in —
// and must still accept the door's "google", "apple" or "door".
func OAuthProviderKeys() []string {
	return []string{OAuthProviderGoogle, OAuthProviderNetSuite, OAuthProviderApple, OAuthProviderDoor}
}

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
		registry[OAuthProviderGoogle] = providers.NewGoogle(providers.GoogleConfig{
			ClientID:     cfg.GoogleClientID,
			ClientSecret: cfg.GoogleClientSecret,
		})
	}
	if cfg.NetSuiteClientID != "" && cfg.NetSuiteClientSecret != "" && cfg.NetSuiteAccountID != "" {
		registry[OAuthProviderNetSuite] = &displayNameFallback{OAuthProvider: providers.NewNetSuite(providers.NetSuiteConfig{
			ClientID:     cfg.NetSuiteClientID,
			ClientSecret: cfg.NetSuiteClientSecret,
			AccountID:    cfg.NetSuiteAccountID,
			Scopes:       cfg.NetSuiteScopes,
		})}
	}
	if cfg.AppleConfigured() {
		// Apple's ID token omits the user's name except on the very first
		// authorization (and even then only in the form_post body, which the
		// token exchange doesn't surface), so DisplayName is effectively always
		// empty. Wrap in displayNameFallback for the same reason as NetSuite —
		// otherwise FindOrCreateAgent rejects the empty name and the browser
		// sees the opaque "failed to find or create agent".
		registry[OAuthProviderApple] = &displayNameFallback{OAuthProvider: providers.NewApple(providers.AppleConfig{
			ClientID:   cfg.AppleClientID,
			TeamID:     cfg.AppleTeamID,
			KeyID:      cfg.AppleKeyID,
			PrivateKey: cfg.ApplePrivateKey,
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
	EventStore          esdomain.EventStore       `optional:"true"`
	EventDispatcher     *esdomain.EventDispatcher `optional:"true"`
	JWTService          authapp.JWTService        `optional:"true"`
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
	svc := authapp.NewDefaultAuthenticationService(
		params.Registry,
		params.Agents,
		params.Credentials,
		params.Sessions,
		params.Accounts,
		opts...,
	)
	// Decorate so the OAuth callback can tell first-time signups from returning
	// logins (see new_account_signal.go). FindOrCreateAgent is only ever called
	// from the OAuth callback path, and the decorator is additionally inert
	// unless a caller installs a flag pointer in the request context — so
	// password and MCP login flows are entirely unaffected.
	return &newAccountSignalService{AuthenticationService: svc, credentials: params.Credentials}
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
