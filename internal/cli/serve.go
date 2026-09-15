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

package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/open-feature/go-sdk/openfeature"
	"github.com/wepala/weos/v3/api/handlers"
	apimw "github.com/wepala/weos/v3/api/middleware"

	"github.com/wepala/weos/v3/application"
	appagents "github.com/wepala/weos/v3/application/agents"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
	gormdb "github.com/wepala/weos/v3/infrastructure/database/gorm"
	"github.com/wepala/weos/v3/internal/config"
	mcpserver "github.com/wepala/weos/v3/internal/mcp"
	weosoauth "github.com/wepala/weos/v3/internal/oauth"
	"github.com/wepala/weos/v3/web"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authcasbin "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/casbin"
	authhttp "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/http"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/fx"
	gormlib "gorm.io/gorm"
)

var serveViper = viper.New()

// customFxOptions are extra fx options merged into the serve command's fx graph.
// Downstream binaries (e.g. weos-kulr) call RegisterFxOptions before Execute()
// to plug in app-specific providers, invokes, or modules without forking
// serve.go. Mirrors the process-global registration pattern used for presets.
var customFxOptions []fx.Option

// RegisterFxOptions appends fx options to be merged into the serve command's
// fx graph. Must be called before Execute(). Reachable from downstream binaries
// via the public re-export in pkg/cli.
func RegisterFxOptions(opts ...fx.Option) {
	customFxOptions = append(customFxOptions, opts...)
}

// EchoConfigurer customizes the serve command's *echo.Echo after the core and
// preset routes are wired but before the dynamic /:typeSlug catch-all.
type EchoConfigurer func(e *echo.Echo)

// customEchoConfigurers are invoked in registration order during serve, after
// the fx graph has started (so they can rely on any process-global a registered
// fx.Invoke captured) and before the dynamic resource catch-all. Mirrors the
// process-global registration pattern used for presets and fx options, and is
// the HTTP-route equivalent of RegisterFxOptions for downstream binaries that
// need to add plain Echo routes (e.g. crossdeck's OAuth and connections APIs).
var customEchoConfigurers []EchoConfigurer

// RegisterEchoConfigurer appends an Echo configurer. Must be called before
// Execute(). Reachable from downstream binaries via the public re-export in
// pkg/cli.
func RegisterEchoConfigurer(c EchoConfigurer) {
	customEchoConfigurers = append(customEchoConfigurers, c)
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the API server",
	Long:  `Start the WeOS HTTP API server with static file serving.`,
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().Bool("mcp", true, "enable MCP server over HTTP at /api/mcp")
	serveViper.SetEnvPrefix("MCP")
	serveViper.AutomaticEnv()
	if err := serveViper.BindPFlag("enabled", serveCmd.Flags().Lookup("mcp")); err != nil {
		panic(fmt.Sprintf("failed to bind MCP flag: %v", err))
	}
	rootCmd.AddCommand(serveCmd)
}

// buildServer starts the application for appCfg and mounts every route serve
// answers, in serve's order, with extra merged into the fx graph. It does not
// listen: runServe does, and the tests serve the returned *echo.Echo
// themselves, so what they boot is what serve boots. On success the caller
// stops the returned app; on an error nothing is left running.
func buildServer(appCfg config.Config, extra ...fx.Option) (_ *echo.Echo, _ *fx.App, err error) {
	var resourceTypeService application.ResourceTypeService
	var resourceService application.ResourceService
	var kgService application.KnowledgeGraphService
	var lexicalSearch application.LexicalSearch
	var episodicRecall application.EpisodicRecall
	var resourcePermService application.ResourcePermissionService
	var fileService application.FileService
	var authService authapp.AuthenticationService
	var assertedSignIn *application.AssertedSignIn
	var providerRegistry authapp.OAuthProviderRegistry
	var sessionManager session.SessionManager
	var credentialRepo authrepos.CredentialRepository
	var agentRepo authrepos.AgentRepository
	var accountRepo authrepos.AccountRepository
	var sessionStore sessions.Store
	var logger entities.Logger
	var sidebarSettingsRepo *gormdb.SidebarSettingsRepository
	var roleSettingsRepo *gormdb.RoleSettingsRepository
	var roleAccessRepo *gormdb.RoleResourceAccessRepository
	var authzChecker *authcasbin.CasbinAuthorizationChecker
	var jwtService authapp.JWTService
	var inviteService *authapp.InviteService
	var inviteRepo authrepos.InviteRepository
	var db *gormlib.DB
	var presetHandlers application.PresetHTTPHandlers
	var notificationService application.NotificationService
	var orchestrator *appagents.Orchestrator
	var skillRegistry *application.SkillRegistry
	var featureInvalidator repositories.FeatureCacheInvalidator
	var featureAdmin *application.FeatureAdminService
	var featureClient *openfeature.Client
	var erasureService *application.AccountErasureService
	var erasureLocks repositories.AccountErasureLocks
	var memberQuery repositories.AccountMemberQuery
	var resourceRepo repositories.ResourceRepository

	registry := presets.NewDefaultRegistry()

	fxOpts := []fx.Option{
		fx.NopLogger,
		application.Module(appCfg, registry),
		fx.Provide(weosoauth.ProvideJWTService),
		fx.Populate(&orchestrator),
		fx.Populate(&skillRegistry),
		fx.Populate(&featureInvalidator),
		fx.Populate(&featureAdmin),
		fx.Populate(&featureClient),
		fx.Populate(&resourceTypeService),
		fx.Populate(&resourceService),
		fx.Populate(&kgService),
		fx.Populate(&lexicalSearch),
		fx.Populate(&episodicRecall),
		fx.Populate(&resourcePermService),
		fx.Populate(&fileService),
		fx.Populate(&authService),
		fx.Populate(&assertedSignIn),
		fx.Populate(&providerRegistry),
		fx.Populate(&sessionManager),
		fx.Populate(&credentialRepo),
		fx.Populate(&agentRepo),
		fx.Populate(&accountRepo),
		fx.Populate(&sessionStore),
		fx.Populate(&logger),
		fx.Populate(&sidebarSettingsRepo),
		fx.Populate(&roleSettingsRepo),
		fx.Populate(&roleAccessRepo),
		fx.Populate(&authzChecker),
		fx.Populate(&jwtService),
		fx.Populate(&inviteService),
		fx.Populate(&inviteRepo),
		fx.Populate(&db),
		fx.Populate(&presetHandlers),
		fx.Populate(&notificationService),
		fx.Populate(&erasureService, &erasureLocks, &memberQuery, &resourceRepo),
	}
	fxOpts = append(fxOpts, extra...)
	app := fx.New(fxOpts...)

	startCtx, startCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer startCancel()

	if startErr := app.Start(startCtx); startErr != nil {
		return nil, nil, fmt.Errorf("failed to start application: %w", startErr)
	}
	// A route that cannot be built must not leave the application running
	// behind the error.
	defer func() {
		if err == nil {
			return
		}
		stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
		defer stopCancel()
		if stopErr := app.Stop(stopCtx); stopErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to shutdown dependencies: %v\n", stopErr)
		}
	}()

	// Sync role-access policies from the config table into Casbin.
	accessMap, accessMapErr := roleAccessRepo.GetAccessMap(context.Background())
	if accessMapErr != nil {
		logger.Warn(context.Background(), "failed to load role access map at startup, RBAC policies may be incomplete", "error", accessMapErr)
	} else {
		if syncErr := application.SyncAccessMapToCasbin(authzChecker, accessMap, nil); syncErr != nil {
			logger.Warn(context.Background(), "casbin policy sync errors at startup", "error", syncErr)
		}
	}
	if seedErr := application.SeedAdminPolicies(authzChecker); seedErr != nil {
		logger.Warn(context.Background(), "failed to seed admin policies at startup", "error", seedErr)
	}

	e := echo.New()
	e.HideBanner = true

	e.Use(apimw.Static(apimw.StaticConfig{
		Filesystem: web.StaticFS(),
		Root:       "dist",
	}))

	api := e.Group("/api")
	api.Use(apimw.Messages())
	api.GET("/health", handlers.HealthHandler)

	// Auth routes (pericarp built-in handlers wrapped for Echo)
	authHandlers := authhttp.NewAuthHandlers(authhttp.HandlerConfig{
		AuthService:    authService,
		SessionManager: sessionManager,
		Credentials:    credentialRepo,
		RedirectURI: authhttp.RedirectURIConfig{
			CallbackPath: "/api/auth/callback",
		},
		DefaultProvider: appCfg.DefaultOAuthProvider(),
		FrontendURL:     appCfg.OAuth.FrontendURL,
		Logger:          logger,
	})
	impersonationHandler := handlers.NewImpersonationHandler(handlers.ImpersonationHandlerConfig{
		Store:          sessionStore,
		AccountRepo:    accountRepo,
		AgentRepo:      agentRepo,
		CredRepo:       credentialRepo,
		Members:        memberQuery,
		SessionManager: sessionManager,
		AuthService:    authService,
		ErasureLocks:   erasureLocks,
		Logger:         logger,
	})

	// Wrapped so a provider this instance cannot begin a sign-in with returns
	// the browser to the sign-in screen with a reason, rather than replacing
	// the page with the bare handler's JSON error. See authLoginHandler.
	api.GET("/auth/login", authLoginHandler(authHandlers.Login, authLoginConfig{
		Registry: providerRegistry,
		// The same resolution /auth/login has always used, so a link that
		// names no provider keeps working wherever it works today. The
		// wrapper checks the RESULT against the registry, which is what
		// catches the auto-pick's "google" fallback on an unconfigured
		// instance.
		DefaultProvider: appCfg.DefaultOAuthProvider(),
		FrontendURL:     appCfg.OAuth.FrontendURL,
	}))
	// Wrapped so the callback handles Apple's form_post (POST) and appends
	// ?new_account=1 for first-time signups. Registered for both GET (Google,
	// NetSuite) and POST (Apple). See authCallbackHandler.
	callback := authCallbackHandler(authHandlers.Callback)
	api.GET("/auth/callback", callback)
	api.POST("/auth/callback", callback)
	// Provider discovery for the sign-in screen. Reads the registry
	// /auth/login resolves against, and sits with /auth/login and
	// /auth/callback outside the protected group — the caller is anonymous
	// by definition. When the instance takes a trusted issuer's assertions it
	// also offers the issuer, so an expired session can get back to the door.
	handlers.MountAuthProviders(api, handlers.NewAuthProvidersHandler(providerRegistry,
		handlers.WithTrustedIssuer(appCfg)))
	// Email + password account flow. Public routes — must reach the handler
	// even when no session exists yet, so they sit outside the protected group.
	// Mirror the SessionManager's dev-default Secure flag (Secure=false when
	// SESSION_SECRET is unset) so the JWT cookie is accepted in plain-HTTP
	// local dev and stays Secure in any real deployment.
	secureCookies := appCfg.SessionSecret != config.DefaultSessionSecret
	// One refresh token store for connectors and native apps: a native sign-in's
	// refresh token is kept, rotated and revoked there too (wm-lnimb).
	refreshRepo := weosoauth.NewRefreshTokenRepository(db)
	passwordAuthHandlers := handlers.NewPasswordAuthHandler(handlers.PasswordAuthHandlerConfig{
		AuthService:    authService,
		SessionManager: sessionManager,
		SecureCookies:  secureCookies,
		Logger:         logger,
		AccountRepo:    accountRepo,
		ErasureLocks:   erasureLocks,
		RefreshTokens:  refreshRepo,
		AgentRepo:      agentRepo,
		JWTService:     jwtService,
	})
	handlers.MountPasswordAuth(api, passwordAuthHandlers, handlers.PasswordAuthRoutes{
		SignIn:       appCfg.PasswordAuthEnabled,
		Registration: appCfg.PasswordRegistrationEnabled,
	})
	// Registration used to come along with PASSWORD_AUTH_ENABLED, so an
	// existing deployment that upgrades loses its register route the moment it
	// picks up this version. Say so at startup: without a line here the change
	// is invisible until someone's signup form 404s in production.
	if appCfg.PasswordAuthEnabled && !appCfg.PasswordRegistrationEnabled {
		logger.Info(context.Background(),
			"password sign-in is on and account registration is off; POST /api/auth/register is not mounted",
			"remedy", "set PASSWORD_REGISTRATION_ENABLED=true to offer open self-service signup")
	}
	// Registration without sign-in would mint accounts that could never be
	// used, so it is ignored rather than honored. Warn, because the operator
	// asked for something they are not getting.
	if appCfg.PasswordRegistrationEnabled && !appCfg.PasswordAuthEnabled {
		logger.Warn(context.Background(),
			"PASSWORD_REGISTRATION_ENABLED is set but PASSWORD_AUTH_ENABLED is not; registration stays unmounted",
			"remedy", "set PASSWORD_AUTH_ENABLED=true as well")
	}

	// Derive a public base URL for OAuth metadata, JWT issuer, and bearer auth,
	// and for the origin a browser may post a login assertion from.
	baseURL := strings.TrimRight(appCfg.OAuth.BaseURL, "/")
	if baseURL == "" {
		host := appCfg.Server.Host
		// Wildcard bind hosts aren't valid public origins; map to localhost.
		if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
			host = "localhost"
		}
		// net.JoinHostPort handles IPv6 bracketing correctly.
		hostPort := net.JoinHostPort(host, strconv.Itoa(appCfg.Server.Port))
		baseURL = "http://" + hostPort
	}

	// The identity read sits outside the protected group: it checks the
	// session its cookie names itself (see ImpersonationHandler.Me). A request
	// that carries a bearer token is checked by BearerOrSession's bearer path
	// instead, so an app in a native shell — whose web view holds no cookie for
	// the instance, only the token its sign-in handed back — can read the
	// account it signed in to (wm-hg3xf). The protected group below takes a
	// token too (wm-aj2eb).
	if appCfg.AuthEnabled() {
		api.GET("/auth/me", impersonationHandler.Me(authHandlers),
			apimw.BearerWhenPresent(jwtService, baseURL, accountRepo, erasureLocks))
	} else {
		api.GET("/auth/me", handlers.DevMe(credentialRepo, agentRepo, accountRepo, logger))
	}

	// Login asserted by a trusted issuer — a fleet's front door that has
	// already verified the person. Public for the same reason as the password
	// routes: the caller has no session yet. Mounted only when
	// TRUSTED_ISSUER, TRUSTED_ISSUER_JWKS_URL and TRUSTED_ISSUER_AUDIENCE are
	// all set and SESSION_SECRET is not core's public default; a partial set
	// warns at boot and mounts nothing, and the default secret logs an error
	// and mounts nothing. A browser may post to it only from the trusted
	// issuer's origin or this instance's (baseURL). See
	// docs/decisions/trusted-issuer-login-assertion.md.
	assertMounted := handlers.MountTrustedIssuerAssertion(context.Background(), api, appCfg, logger,
		func() *handlers.TrustedIssuerHandler {
			return handlers.NewTrustedIssuerAssertionHandler(appCfg.TrustedIssuer, appCfg.OAuth.AllowedEmails, handlers.TrustedIssuerAssertionDeps{
				SignIn:        assertedSignIn,
				Sessions:      passwordAuthHandlers,
				Logger:        logger,
				PublicBaseURL: baseURL,
			})
		})
	// A trusted issuer's sign-in hands a native app a bearer token, and the app
	// reads its account with it (wm-hg3xf). With no JWT_SIGNING_KEY that token
	// is signed by a key made at boot, so every restart or deploy signs the app
	// out. Say so once at boot (wm-a6xb6). The key itself is never logged.
	// Only when the assert route is mounted: a partly configured issuer, or one
	// on core's public session secret, issues no such token, and the mount has
	// already logged why.
	if assertMounted && weosoauth.TokensDieOnRestart(appCfg) {
		logger.Warn(context.Background(),
			"JWT_SIGNING_KEY is empty or auto, so native bearer tokens will not survive a restart: each boot signs tokens with a new key, and an app holding one is signed out after every restart or deploy",
			"remedy", "set JWT_SIGNING_KEY to a PEM-encoded RSA private key that stays the same across restarts and on every instance")
	}

	// A native sign-in's access token lasts an hour, and an app in a native
	// shell holds nothing else, so the refresh token handed back beside it
	// renews it here (wm-lnimb). Public, like the sign-ins: the caller's access
	// token may already have expired. Mounted wherever a native sign-in is.
	if appCfg.PasswordAuthEnabled || assertMounted {
		handlers.MountSessionRenewal(api, passwordAuthHandlers)
	}

	// Logout must clear BOTH the gorilla session (pericarp Logout) AND the
	// JWT cookie issued by the password and OAuth flows. Routing through
	// the password handler so a single endpoint is correct for both flows.
	api.POST("/auth/logout", func(c echo.Context) error {
		return passwordAuthHandlers.Logout(c, authHandlers.Logout)
	})

	// OAuth 2.1 endpoints for MCP remote auth (unprotected — they handle their own auth).
	// Registered via e.Pre() so they run before the SPA static middleware,
	// which would otherwise intercept /.well-known/* and /oauth/* paths.
	//
	// Gated on AuthEnabled rather than OAuthEnabled: this server is the
	// authorization server, and it can act as one whenever it can authenticate
	// a resource owner at all — which now includes an instance offering only
	// password sign-in. Keyed to OAuthEnabled, such an instance mounted none of
	// these routes, so a connector asking how to authorize against it got the
	// SPA's static handler instead of the metadata, and could not discover it,
	// register with it, or authorize at all.
	if appCfg.AuthEnabled() {
		clientRepo := weosoauth.NewClientRepository(db)
		codeRepo := weosoauth.NewAuthCodeRepository(db)

		const mcpResourcePath = "/api/mcp"
		var defaultResource string
		knownResources := map[string]bool{}
		if serveViper.GetBool("enabled") {
			defaultResource = mcpResourcePath
			knownResources[mcpResourcePath] = true
		}
		prHandler := weosoauth.ProtectedResourceMetadata(baseURL, defaultResource, knownResources)
		asHandler := weosoauth.AuthorizationServerMetadata(baseURL, appCfg.OAuth.DynamicRegistration)
		regHandler := weosoauth.RegisterClient(clientRepo, appCfg.OAuth.DynamicRegistration)
		authzHandler := weosoauth.Authorize(authService, sessionManager, sessionStore,
			clientRepo, codeRepo, credentialRepo, logger, baseURL,
			appCfg.OAuthEnabled(), appCfg.OAuth.AllowedEmails)
		cbHandler := weosoauth.Callback(authService, sessionStore, codeRepo, accountRepo, logger, baseURL, appCfg.OAuth.AllowedEmails)
		tokHandler := weosoauth.Token(jwtService, codeRepo, refreshRepo, agentRepo, accountRepo, logger)

		e.Pre(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				p := c.Request().URL.Path
				m := c.Request().Method
				switch {
				case weosoauth.IsProtectedResourceMetadataRequest(m, p):
					return prHandler(c)
				case m == http.MethodGet && p == "/.well-known/oauth-authorization-server":
					return asHandler(c)
				case m == http.MethodPost && p == "/oauth/register":
					return regHandler(c)
				case m == http.MethodGet && p == "/oauth/authorize":
					return authzHandler(c)
				case m == http.MethodGet && p == "/oauth/callback":
					return cbHandler(c)
				case m == http.MethodPost && p == "/oauth/token":
					return tokHandler(c)
				default:
					return next(c)
				}
			}
		})
	}

	// Protected API group — apply real auth middleware when any
	// authentication mechanism (OAuth, password, or a trusted issuer) is
	// configured. Falling back to SoftAuth only in fully-unconfigured dev mode
	// keeps a password or trusted-issuer deployment from looking authenticated
	// while protected routes remain effectively open. A trusted issuer counts
	// from its first setting, mounted or not: see config.Config.AuthEnabled.
	protected := api.Group("")
	if appCfg.AuthEnabled() {
		// The group takes a bearer token as well as a session, the way the MCP
		// group does (wm-aj2eb): an app in a native shell holds no cookie for
		// the instance, only the token its sign-in handed back. A token wins
		// over a cookie beside it. Preset handlers mounted Protected inherit
		// this.
		//
		// It takes a native sign-in's token only. A token a third-party
		// connector got from /oauth/token is refused here (wm-8i8ln): a person
		// connecting a client agrees to the MCP and agent routes, not to roles,
		// invites, impersonation or the account itself.
		//
		// The erasure guard goes first, around the session auth: an account
		// whose deletion began and did not finish is refused everywhere with
		// the code that says so, where RequireAuth alone would call it merely
		// deactivated. It defers to the token path, which checks the token's
		// account itself. The one route a locked account may still use is
		// mounted on its own group below.
		sessionAuth := authhttp.RequireAuth(sessionManager, authService)
		protected.Use(apimw.ErasureGuard(sessionManager, erasureLocks, logger, apimw.DeferToBearer()))
		protected.Use(apimw.BearerOrSession(jwtService, sessionAuth, baseURL, accountRepo, erasureLocks,
			apimw.RefuseConnectorTokens()))
		protected.Use(apimw.Impersonation(sessionStore, accountRepo, erasureLocks, logger))
		protected.Use(apimw.AuthorizeResource(authzChecker, accountRepo, logger))
	} else {
		protected.Use(apimw.SoftAuth(credentialRepo, agentRepo, accountRepo, logger))
	}

	// The account routes (story wm-kb6sg.3). The export is an ordinary
	// protected read. The deletion has its own group: its auth admits an
	// erasure-locked account for this one purpose, and it takes no
	// Impersonation middleware because it refuses while one is active rather
	// than act as the impersonated person. It takes a bearer token as the
	// protected group does (wm-aj2eb), so an app in a native shell can offer
	// the in-app deletion; a token scoped to a locked account is admitted here
	// as a session scoped to one is. A connector's token is refused, as on the
	// protected group (wm-8i8ln).
	accountHandler := handlers.NewAccountHandler(handlers.AccountHandlerConfig{
		Erasure:        erasureService,
		Accounts:       accountRepo,
		Resources:      resourceRepo,
		ResourceTypes:  resourceTypeService,
		SessionManager: sessionManager,
		Store:          sessionStore,
		SecureCookies:  secureCookies,
		Logger:         logger,
	})
	protected.GET("/account/export", accountHandler.Export)
	accountGroup := api.Group("")
	if appCfg.AuthEnabled() {
		accountGroup.Use(apimw.BearerOrSessionForErasure(jwtService, baseURL, accountRepo, erasureLocks,
			apimw.SessionAuthForErasure(sessionManager, authService, accountRepo, erasureLocks, logger),
			apimw.RefuseConnectorTokens()))
	} else {
		accountGroup.Use(apimw.SoftAuth(credentialRepo, agentRepo, accountRepo, logger))
	}
	accountGroup.DELETE("/account", accountHandler.Delete)

	personHandler := handlers.NewPersonHandler(resourceService)
	protected.POST("/persons", personHandler.Create)
	protected.GET("/persons", personHandler.List)
	protected.GET("/persons/:id", personHandler.Get)
	protected.PUT("/persons/:id", personHandler.Update)
	protected.DELETE("/persons/:id", personHandler.Delete)

	orgHandler := handlers.NewOrganizationHandler(resourceService)
	protected.POST("/organizations", orgHandler.Create)
	protected.GET("/organizations", orgHandler.List)
	protected.GET("/organizations/:id", orgHandler.Get)
	protected.PUT("/organizations/:id", orgHandler.Update)
	protected.DELETE("/organizations/:id", orgHandler.Delete)
	protected.GET("/organizations/:id/members", orgHandler.Members)

	// Notification inbox (#427). Registered before the generic /:typeSlug
	// resource routes; the plural path never collides with the "notification"
	// type slug, and the static sub-paths take radix priority over /:typeSlug/:id.
	notificationHandler := handlers.NewNotificationHandler(notificationService, logger)
	protected.GET("/notifications", notificationHandler.List)
	protected.GET("/notifications/unread-count", notificationHandler.UnreadCount)
	protected.POST("/notifications/mark-all-read", notificationHandler.MarkAllRead)
	protected.POST("/notifications/:id/read", notificationHandler.MarkRead)

	rtHandler := handlers.NewResourceTypeHandler(resourceTypeService, authzChecker, accountRepo, logger)
	protected.POST("/resource-types", rtHandler.Create)
	protected.GET("/resource-types", rtHandler.List)
	protected.GET("/resource-types/:id", rtHandler.Get)
	protected.PUT("/resource-types/:id", rtHandler.Update)
	protected.DELETE("/resource-types/:id", rtHandler.Delete)

	presetHandler := handlers.NewResourceTypePresetHandler(resourceTypeService)
	protected.GET("/resource-types/presets", presetHandler.List)
	protected.POST("/resource-types/presets/:name", presetHandler.Install)
	protected.GET("/resource-types/:typeSlug/behaviors", presetHandler.ListBehaviors)
	protected.PUT("/resource-types/:typeSlug/behaviors", presetHandler.SetBehaviors)

	screenHandler := handlers.NewPresetScreenHandler(registry)
	protected.GET("/resource-types/presets/:name/screens/*", screenHandler.Serve)

	sidebarSettingsHandler := handlers.NewSidebarSettingsHandler(sidebarSettingsRepo, accountRepo, logger)
	protected.GET("/settings/sidebar", sidebarSettingsHandler.Get)
	protected.PUT("/settings/sidebar", sidebarSettingsHandler.Save)

	roleSettingsHandler := handlers.NewRoleSettingsHandler(handlers.RoleSettingsHandlerConfig{
		Repo:        roleSettingsRepo,
		AccountRepo: accountRepo,
		Logger:      logger,
	})
	protected.GET("/settings/roles", roleSettingsHandler.Get)
	protected.PUT("/settings/roles", roleSettingsHandler.Save)

	roleAccessHandler := handlers.NewRoleAccessHandler(handlers.RoleAccessHandlerConfig{
		Repo:        roleAccessRepo,
		Checker:     authzChecker,
		AccountRepo: accountRepo,
		Logger:      logger,
	})
	protected.GET("/settings/role-access", roleAccessHandler.Get)
	protected.PUT("/settings/role-access", roleAccessHandler.Save)

	userHandler := handlers.NewUserHandler(handlers.UserHandlerConfig{
		AgentRepo:      agentRepo,
		CredentialRepo: credentialRepo,
		AccountRepo:    accountRepo,
		Features:       featureInvalidator,
		Logger:         logger,
	})
	featureHandler := handlers.NewFeatureHandler(handlers.FeatureHandlerConfig{
		Admin:  featureAdmin,
		Logger: logger,
	})
	// Listing is readable by any authenticated caller — the admin UI needs it
	// to decide what to render. Changing state is gated inside the service,
	// which the CLI and the MCP tools share, so the rule lives in one place.
	// GET /features is registered below on its own group: it must answer a
	// caller with no session (#487), which the protected group cannot do.
	protected.PUT("/features/:key/instance", featureHandler.SetInstance)
	protected.DELETE("/features/:key/instance", featureHandler.ResetInstance)
	protected.PUT("/features/:key/account", featureHandler.SetAccount)
	protected.DELETE("/features/:key/account", featureHandler.ResetAccount)
	// Grants hang off the same :key. The static segment is registered before
	// the parameterised one so it cannot be swallowed by it.
	protected.GET("/features/grants", featureHandler.GrantsHeldBy)
	protected.GET("/features/:key/grants", featureHandler.ListGrants)
	protected.POST("/features/:key/grants", featureHandler.Grant)
	protected.DELETE("/features/:key/grants", featureHandler.RevokeGrant)

	protected.GET("/users", userHandler.List)
	protected.GET("/users/:id", userHandler.Get)
	protected.PUT("/users/:id", userHandler.Update)

	inviteHandler := handlers.NewInviteHandler(handlers.InviteHandlerConfig{
		InviteService:  inviteService,
		InviteRepo:     inviteRepo,
		AccountRepo:    accountRepo,
		CredentialRepo: credentialRepo,
		Logger:         logger,
	})
	protected.POST("/invites", inviteHandler.Create)
	protected.GET("/invites", inviteHandler.List)
	protected.DELETE("/invites/:id", inviteHandler.Revoke)

	// The invite-accept, feature-listing and MCP groups below take the session
	// stack under OAuth and under a trusted issuer. A trusted issuer counts from
	// its first setting, as it does for AuthEnabled: an instance whose only
	// sign-in is the door must never answer these routes as the dev user.
	// A password-only instance keeps the dev stack it has always had on these
	// three groups; that gap predates the trusted issuer and is tracked apart
	// from it (bead wm-25gh1), so this change leaves password deployments as
	// they were.
	sessionStack := appCfg.OAuthEnabled() || !appCfg.TrustedIssuer.Unset()

	// Accept uses a separate group that loads session identity when present
	// (so the handler can verify email from the session) but does not require
	// auth — the invite token itself is the authorization.
	// Without the session stack, no auth middleware is applied so the handler
	// runs anonymously and uses the request-body email. SoftAuth is NOT used
	// here because it defaults to admin@weos.dev, which would force the
	// fail-closed session-email path for every accept request.
	acceptGroup := api.Group("")
	if sessionStack {
		acceptGroup.Use(echo.WrapMiddleware(apimw.OptionalAuth(sessionManager, authService)))
	}
	acceptGroup.POST("/invites/accept", inviteHandler.Accept)

	// The feature listing (#486/#487). It answers a caller with no session —
	// the admin needs the instance view to draw a sign-in page — so it cannot
	// live on the protected group.
	//
	// Two things about this group are load-bearing.
	//
	// It keeps Impersonation. The protected group applies it, and a listing
	// that lost it would answer with the real admin's features while every
	// other call on the page answered with the impersonated user's — a sidebar
	// drawn for one person over another person's data, which is worse than
	// either alone. Impersonation passes through untouched when there is no
	// identity, so it costs the anonymous case nothing.
	//
	// It is registered BEFORE mcpGroup, deliberately. Echo's Group.Use
	// registers a not-found catch-all at the group prefix, so the LAST
	// empty-prefix group under /api owns what an unmatched /api path answers —
	// see the note at mcpGroup. Putting this group after it would silently
	// move that ownership and change the answer for every unmounted path,
	// including /api/auth/register when registration is off, whose parity
	// password_registration_flag.feature pins in a test that builds its own
	// echo instance and would therefore not notice.
	featuresGroup := api.Group("")
	if sessionStack {
		featuresGroup.Use(echo.WrapMiddleware(apimw.OptionalAuth(sessionManager, authService)))
		featuresGroup.Use(apimw.Impersonation(sessionStore, accountRepo, erasureLocks, logger))
	} else {
		featuresGroup.Use(apimw.SoftAuth(credentialRepo, agentRepo, accountRepo, logger))
	}
	featuresGroup.GET("/features", featureHandler.List)

	protected.POST("/admin/impersonate", impersonationHandler.Start)
	protected.GET("/admin/impersonation-status", impersonationHandler.Status)
	// Stopping takes the protected group's session checks but not its
	// Impersonation middleware. That middleware refuses a cookie it no longer
	// allows, and the request that ends such an impersonation must not be
	// refused with it (wm-1yjuv). The checks are per-route middleware, not a
	// new group, so no group's not-found catch-all moves (see featuresGroup).
	// EndHeldImpersonation runs first: those checks refuse a session whose
	// account is suspended, locked for deletion or no longer the caller's
	// before Stop is reached, and that refusal must still end the impersonation.
	// The checks take a native sign-in's bearer token and refuse a connector's,
	// exactly as the protected group does, so an app that started an
	// impersonation with its token can also end it (wm-ptcuk).
	var stopGuards []echo.MiddlewareFunc
	if appCfg.AuthEnabled() {
		stopGuards = []echo.MiddlewareFunc{
			apimw.EndHeldImpersonation(),
			apimw.ErasureGuard(sessionManager, erasureLocks, logger, apimw.DeferToBearer()),
			apimw.BearerOrSession(jwtService, authhttp.RequireAuth(sessionManager, authService), baseURL,
				accountRepo, erasureLocks, apimw.RefuseConnectorTokens()),
		}
	} else {
		stopGuards = []echo.MiddlewareFunc{
			apimw.EndHeldImpersonation(),
			apimw.SoftAuth(credentialRepo, agentRepo, accountRepo, logger),
		}
	}
	api.POST("/admin/stop-impersonation", impersonationHandler.Stop, stopGuards...)

	// File upload routes — registered before dynamic catch-all
	uploadHandler := handlers.NewUploadHandler(fileService, logger, appCfg.Storage.MaxUploadBytes)
	protected.POST("/uploads", uploadHandler.Upload)

	// The read route strips its mount path from the request path, so the
	// registration and the handler take it from this one constant.
	const uploadFilesPath = "/uploads/files/"
	protected.GET(uploadFilesPath+"*",
		handlers.ServeUploadedFiles("/api"+uploadFilesPath, appCfg.Storage.LocalPath, logger))

	// MCP + in-app agent routes — registered before dynamic catch-all. Both
	// share one auth stack (BearerOrSession under OAuth or a trusted issuer,
	// SoftAuth otherwise; see sessionStack). This is the group a connector's
	// token is for, so it takes one; the protected group does not.
	mcpGroup := api.Group("")
	if sessionStack {
		sessionAuth := authhttp.RequireAuth(sessionManager, authService)
		mcpGroup.Use(apimw.ErasureGuard(sessionManager, erasureLocks, logger, apimw.DeferToBearer()))
		mcpGroup.Use(apimw.BearerOrSession(jwtService, sessionAuth, baseURL, accountRepo, erasureLocks))
		mcpGroup.Use(apimw.Impersonation(sessionStore, accountRepo, erasureLocks, logger))
	} else {
		mcpGroup.Use(apimw.SoftAuth(credentialRepo, agentRepo, accountRepo, logger))
	}
	mcpGroup.Use(apimw.AuthorizeResource(authzChecker, accountRepo, logger))

	// Careful: this is the last Use() on an empty-prefix group under /api, and
	// echo's Group.Use registers the group's not-found catch-all at the group
	// prefix. So THIS group owns what an unmatched /api/... path answers —
	// a bare 404 in dev, and 401 with a Bearer challenge once OAuth or a
	// trusted issuer is configured (sessionStack), because BearerOrSession
	// sets the challenge before it checks the session.
	//
	// Adding a later empty-prefix group, or reordering these three (protected,
	// acceptGroup, mcpGroup), silently moves that ownership and changes the
	// answer for every unmounted path — including /api/auth/register when
	// PASSWORD_REGISTRATION_ENABLED is off. What must stay true is that the
	// registration path answers exactly what an invented path answers; the
	// specific status is allowed to differ per deployment. The acceptance
	// scenarios in tests/e2e/features/password_registration_flag.feature pin
	// that parity, and tests/e2e/password_registration_flag_test.go builds its
	// own echo instance, so it will NOT notice if this ordering changes.

	// The in-app agent is independent of the MCP transport flag: with MCP
	// disabled it still chats, just tool-less (the orchestrator degrades).
	agentHandler := handlers.NewAgentHandler(orchestrator, logger)
	// The in-app agent's REST surface is gated on "agent-chat" (#486). It is
	// the one sidebar entry that is a capability rather than an administrative
	// screen — Users and Settings are gated by role, and a role is not a flag.
	// Declared ON, so no instance loses a working page by upgrading.
	//
	// This composes with #485 rather than overlapping it: off means there is
	// no assistant at all, on means there is one whose skill graph #485
	// filters for the caller.
	agentGate := apimw.RequireFeature(application.ToolFeatureGate(featureClient), application.FeatureAgentChat)
	mcpGroup.POST("/agent/conversations/:conversationID/messages", agentHandler.SendMessage, agentGate)
	mcpGroup.POST("/agent/conversations/:conversationID/confirmations/:callID", agentHandler.Confirm, agentGate)
	mcpGroup.GET("/agent/conversations/:conversationID", agentHandler.History, agentGate)

	if serveViper.GetBool("enabled") {
		mcpSrv, mcpErr := mcpserver.NewConfiguredServer(
			resourceTypeService, resourceService, kgService, lexicalSearch, episodicRecall,
			featureAdmin, application.ToolFeatureGate(featureClient), slog.Default(),
		)
		if mcpErr != nil {
			return nil, nil, fmt.Errorf("failed to create MCP server: %w", mcpErr)
		}
		mcpHandler := mcpserver.HandlerForServer(mcpSrv, slog.Default())

		// The in-app agent shares this exact tool surface (epic #397): the
		// skill registry validates allowlists against it, the coordinator's
		// direct tools are its read-only subset, each conversation turn opens
		// a toolset session under the caller's identity, and every mutating
		// tool call pauses for the user's confirmation in the chat.
		skillRegistry.SetKnownTools(mcpserver.KnownTools(mcpSrv))
		readOnlyTools, roErr := mcpserver.ReadOnlyToolNames(context.Background(), mcpSrv)
		if roErr != nil {
			return nil, nil, fmt.Errorf("failed to list read-only tools for the agent: %w", roErr)
		}
		confirmMutations, cmErr := mcpserver.MutatingConfirmationProvider(context.Background(), mcpSrv)
		if cmErr != nil {
			return nil, nil, fmt.Errorf("failed to build the agent confirmation provider: %w", cmErr)
		}
		orchestrator.SetToolsetFactory(
			mcpserver.AgentToolsetFactory(mcpSrv, mcpserver.AgentToolsetConfig{
				RequireConfirmationProvider: confirmMutations,
			}), readOnlyTools,
		)
		mcpGroup.Any("/mcp", echo.WrapHandler(mcpHandler))
		mcpGroup.Any("/mcp/*", echo.WrapHandler(mcpHandler))
		logger.Info(context.Background(), "MCP server enabled", "path", "/api/mcp")
	} else {
		logger.Info(context.Background(), "MCP server disabled via configuration")
	}

	// Permission routes — registered before dynamic catch-all
	permHandler := handlers.NewResourcePermissionHandler(resourcePermService)
	protected.POST("/:typeSlug/:id/permissions", permHandler.Grant)
	protected.GET("/:typeSlug/:id/permissions", permHandler.List)
	protected.DELETE("/:typeSlug/:id/permissions/:agentId", permHandler.Revoke)

	// Preset-contributed HTTP handlers. Registered before the dynamic /:typeSlug
	// catch-all so preset routes aren't shadowed by it.
	mountPresetHandlers(api, protected, presetHandlers, logger)

	// Downstream-binary Echo extensions (e.g. crossdeck's OAuth, connections,
	// and client-metadata routes). Registered after core + preset routes but
	// before the dynamic catch-all so custom routes aren't shadowed by
	// /:typeSlug. Runs after app.Start, so configurers can rely on any
	// process-global captured by a registered fx.Invoke.
	for _, configure := range customEchoConfigurers {
		configure(e)
	}

	// Dynamic resource routes — MUST be registered after ALL static routes
	resourceHandler := handlers.NewResourceHandler(resourceService, resourceTypeService, logger)
	protected.POST("/:typeSlug", resourceHandler.Create)
	protected.GET("/:typeSlug", resourceHandler.List)
	protected.GET("/:typeSlug/:id", resourceHandler.Get)
	protected.PUT("/:typeSlug/:id", resourceHandler.Update)
	protected.DELETE("/:typeSlug/:id", resourceHandler.Delete)

	return e, app, nil
}

func runServe(cmd *cobra.Command, args []string) error {
	appCfg := loadServeConfig()
	e, app, err := buildServer(appCfg, customFxOptions...)
	if err != nil {
		return err
	}

	addr := fmt.Sprintf("%s:%d", appCfg.Server.Host, appCfg.Server.Port)

	go func() {
		fmt.Printf("Starting server on %s\n", addr)
		if err := e.Start(addr); err != nil {
			if err.Error() != "http: Server closed" {
				fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
				os.Exit(1)
			}
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	fmt.Println("\nShutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Server forced to shutdown: %v\n", err)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer stopCancel()

	if err := app.Stop(stopCtx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to shutdown dependencies: %v\n", err)
	}

	fmt.Println("Server stopped")
	return nil
}

func loadServeConfig() config.Config {
	appCfg := cfg.Config
	if portStr := os.Getenv("PORT"); portStr != "" { //nolint:forbidigo // late PORT override for PaaS entrypoints
		if port, err := strconv.Atoi(portStr); err == nil && port > 0 {
			appCfg.Server.Port = port
		}
	}
	// The long-lived server process runs the background subscribers by default.
	// Operators running a separate dedicated worker can set
	// WORKER_RUN_IN_PROCESS=false to keep this serve process API-only. This is
	// the only place the var is honored — short-lived CLI commands never start
	// workers regardless of the environment.
	appCfg.Worker.RunInProcess = true
	if v := os.Getenv("WORKER_RUN_IN_PROCESS"); v != "" { //nolint:forbidigo // serve-only worker toggle, see comment above
		if b, err := strconv.ParseBool(v); err == nil {
			appCfg.Worker.RunInProcess = b
		}
	}
	return appCfg
}
