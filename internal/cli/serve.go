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
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"weos/api/handlers"
	apimw "weos/api/middleware"
	"weos/application"
	"weos/internal/config"
	"weos/web"

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

	app := fx.New(
		fx.NopLogger,
		application.Module(appCfg),
		fx.Populate(&personService),
		fx.Populate(&organizationService),
		fx.Populate(&resourceTypeService),
		fx.Populate(&resourceService),
	)

	startCtx, startCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer startCancel()

	if err := app.Start(startCtx); err != nil {
		return fmt.Errorf("failed to start application: %w", err)
	}

	e := echo.New()
	e.HideBanner = true

	e.Use(apimw.Static(apimw.StaticConfig{
		Filesystem: web.StaticFS,
		Root:       "dist",
	}))

	api := e.Group("/api")
	api.GET("/health", handlers.HealthHandler)

	personHandler := handlers.NewPersonHandler(personService)
	api.POST("/persons", personHandler.Create)
	api.GET("/persons", personHandler.List)
	api.GET("/persons/:id", personHandler.Get)
	api.PUT("/persons/:id", personHandler.Update)
	api.DELETE("/persons/:id", personHandler.Delete)

	orgHandler := handlers.NewOrganizationHandler(organizationService)
	api.POST("/organizations", orgHandler.Create)
	api.GET("/organizations", orgHandler.List)
	api.GET("/organizations/:id", orgHandler.Get)
	api.PUT("/organizations/:id", orgHandler.Update)
	api.DELETE("/organizations/:id", orgHandler.Delete)

	rtHandler := handlers.NewResourceTypeHandler(resourceTypeService)
	api.POST("/resource-types", rtHandler.Create)
	api.GET("/resource-types", rtHandler.List)
	api.GET("/resource-types/:id", rtHandler.Get)
	api.PUT("/resource-types/:id", rtHandler.Update)
	api.DELETE("/resource-types/:id", rtHandler.Delete)

	presetHandler := handlers.NewResourceTypePresetHandler(resourceTypeService)
	api.GET("/resource-types/presets", presetHandler.List)
	api.POST("/resource-types/presets/:name", presetHandler.Install)

	// Dynamic resource routes — MUST be registered after ALL static routes
	resourceHandler := handlers.NewResourceHandler(resourceService, resourceTypeService)
	api.POST("/:typeSlug", resourceHandler.Create)
	api.GET("/:typeSlug", resourceHandler.List)
	api.GET("/:typeSlug/:id", resourceHandler.Get)
	api.PUT("/:typeSlug/:id", resourceHandler.Update)
	api.DELETE("/:typeSlug/:id", resourceHandler.Delete)

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
