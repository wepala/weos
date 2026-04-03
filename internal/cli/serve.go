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
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"weos/api/handlers"
	apimw "weos/api/middleware"
	"weos/application"
	"weos/domain/entities"
	gormdb "weos/infrastructure/database/gorm"
	"weos/internal/config"
	"weos/web"

	authapp "github.com/akeemphilbert/pericarp/pkg/auth/application"
	authrepos "github.com/akeemphilbert/pericarp/pkg/auth/domain/repositories"
	authcasbin "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/casbin"
	authhttp "github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/http"
	"github.com/akeemphilbert/pericarp/pkg/auth/infrastructure/session"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the API server",
	Long:  `Start the WeOS HTTP API server with static file serving.`,
	RunE:  runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	appCfg := loadServeConfig()

	var personService application.PersonService
	var organizationService application.OrganizationService
	var resourceTypeService application.ResourceTypeService
	var resourceService application.ResourceService
	var authService authapp.AuthenticationService
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

	app := fx.New(
		fx.NopLogger,
		application.Module(appCfg),
		fx.Populate(&personService),
		fx.Populate(&organizationService),
		fx.Populate(&resourceTypeService),
		fx.Populate(&resourceService),
		fx.Populate(&authService),
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
	)

	startCtx, startCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer startCancel()

	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start application: %w", err)
	}

	// Sync role-access policies from the config table into Casbin.
	if accessMap, err := roleAccessRepo.GetAccessMap(context.Background()); err == nil {
		handlers.SyncAccessMapToCasbin(authzChecker, accessMap, nil)
	}
	handlers.SeedAdminPolicies(authzChecker)

	e := echo.New()
	e.HideBanner = true

	e.Use(apimw.Static(apimw.StaticConfig{
		Filesystem: web.StaticFS,
		Root:       "dist",
	}))

	api := e.Group("/api")
	api.GET("/health", handlers.HealthHandler)

	// Auth routes (pericarp built-in handlers wrapped for Echo)
	authHandlers := authhttp.NewAuthHandlers(authhttp.HandlerConfig{
		AuthService:    authService,
		SessionManager: sessionManager,
		Credentials:    credentialRepo,
		RedirectURI: authhttp.RedirectURIConfig{
			CallbackPath: "/api/auth/callback",
		},
		DefaultProvider: "google",
		FrontendURL:     appCfg.OAuth.FrontendURL,
		Logger:          logger,
	})
	impersonationHandler := handlers.NewImpersonationHandler(handlers.ImpersonationHandlerConfig{
		Store:       sessionStore,
		AccountRepo: accountRepo,
		AgentRepo:   agentRepo,
		CredRepo:    credentialRepo,
		Logger:      logger,
	})

	api.GET("/auth/login", echo.WrapHandler(http.HandlerFunc(authHandlers.Login)))
	api.GET("/auth/callback", echo.WrapHandler(http.HandlerFunc(authHandlers.Callback)))
	api.GET("/auth/me", impersonationHandler.Me(authHandlers))
	api.POST("/auth/logout", echo.WrapHandler(http.HandlerFunc(authHandlers.Logout)))

	// Protected API group — apply auth middleware when OAuth is configured
	protected := api.Group("")
	if appCfg.OAuthEnabled() {
		protected.Use(echo.WrapMiddleware(authhttp.RequireAuth(sessionManager, authService)))
		protected.Use(apimw.Impersonation(sessionStore, accountRepo))
		protected.Use(apimw.AuthorizeResource(authzChecker, accountRepo))
	}

	personHandler := handlers.NewPersonHandler(personService)
	protected.POST("/persons", personHandler.Create)
	protected.GET("/persons", personHandler.List)
	protected.GET("/persons/:id", personHandler.Get)
	protected.PUT("/persons/:id", personHandler.Update)
	protected.DELETE("/persons/:id", personHandler.Delete)

	orgHandler := handlers.NewOrganizationHandler(organizationService, personService)
	protected.POST("/organizations", orgHandler.Create)
	protected.GET("/organizations", orgHandler.List)
	protected.GET("/organizations/:id", orgHandler.Get)
	protected.PUT("/organizations/:id", orgHandler.Update)
	protected.DELETE("/organizations/:id", orgHandler.Delete)
	protected.GET("/organizations/:id/members", orgHandler.Members)

	rtHandler := handlers.NewResourceTypeHandler(resourceTypeService, authzChecker, accountRepo)
	protected.POST("/resource-types", rtHandler.Create)
	protected.GET("/resource-types", rtHandler.List)
	protected.GET("/resource-types/:id", rtHandler.Get)
	protected.PUT("/resource-types/:id", rtHandler.Update)
	protected.DELETE("/resource-types/:id", rtHandler.Delete)

	presetHandler := handlers.NewResourceTypePresetHandler(resourceTypeService)
	protected.GET("/resource-types/presets", presetHandler.List)
	protected.POST("/resource-types/presets/:name", presetHandler.Install)

	sidebarSettingsHandler := handlers.NewSidebarSettingsHandler(sidebarSettingsRepo, accountRepo, logger)
	protected.GET("/settings/sidebar", sidebarSettingsHandler.Get)
	protected.PUT("/settings/sidebar", sidebarSettingsHandler.Save)

	roleSettingsHandler := handlers.NewRoleSettingsHandler(roleSettingsRepo, accountRepo, logger)
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
		Logger:         logger,
	})
	protected.GET("/users", userHandler.List)
	protected.GET("/users/:id", userHandler.Get)
	protected.PUT("/users/:id", userHandler.Update)

	protected.POST("/admin/impersonate", impersonationHandler.Start)
	protected.POST("/admin/stop-impersonation", impersonationHandler.Stop)
	protected.GET("/admin/impersonation-status", impersonationHandler.Status)

	// Dynamic resource routes — MUST be registered after ALL static routes
	resourceHandler := handlers.NewResourceHandler(resourceService, resourceTypeService)
	protected.POST("/:typeSlug", resourceHandler.Create)
	protected.GET("/:typeSlug", resourceHandler.List)
	protected.GET("/:typeSlug/:id", resourceHandler.Get)
	protected.PUT("/:typeSlug/:id", resourceHandler.Update)
	protected.DELETE("/:typeSlug/:id", resourceHandler.Delete)

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
	if portStr := os.Getenv("PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil && port > 0 {
			appCfg.Server.Port = port
		}
	}
	return appCfg
}
