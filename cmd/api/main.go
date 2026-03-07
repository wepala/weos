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

package main

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

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
)

func main() {
	_ = godotenv.Load()

	cfg := loadConfig()

	var websiteService application.WebsiteService
	var pageService application.PageService
	var sectionService application.SectionService
	var themeService application.ThemeService
	var templateService application.TemplateService
	var personService application.PersonService
	var organizationService application.OrganizationService

	// Start DI container
	app := fx.New(
		fx.NopLogger,
		application.Module(cfg),
		fx.Populate(&websiteService),
		fx.Populate(&pageService),
		fx.Populate(&sectionService),
		fx.Populate(&themeService),
		fx.Populate(&templateService),
		fx.Populate(&personService),
		fx.Populate(&organizationService),
	)

	startCtx, startCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer startCancel()

	if err := app.Start(startCtx); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start application: %v\n", err)
		os.Exit(1)
	}

	// Create Echo server
	e := echo.New()
	e.HideBanner = true

	// Serve embedded static assets with SPA fallback
	e.Use(apimw.Static(apimw.StaticConfig{
		Filesystem: web.StaticFS,
		Root:       "dist",
	}))

	websiteHandler := handlers.NewWebsiteHandler(websiteService)
	pageHandler := handlers.NewPageHandler(pageService)
	sectionHandler := handlers.NewSectionHandler(sectionService)

	// Register API routes under /api prefix
	api := e.Group("/api")
	api.GET("/health", handlers.HealthHandler)

	api.POST("/websites", websiteHandler.Create)
	api.GET("/websites", websiteHandler.List)
	api.GET("/websites/:id", websiteHandler.Get)
	api.PUT("/websites/:id", websiteHandler.Update)
	api.DELETE("/websites/:id", websiteHandler.Delete)

	api.POST("/pages", pageHandler.Create)
	api.GET("/pages", pageHandler.List)
	api.GET("/pages/:id", pageHandler.Get)
	api.PUT("/pages/:id", pageHandler.Update)
	api.DELETE("/pages/:id", pageHandler.Delete)

	api.POST("/sections", sectionHandler.Create)
	api.GET("/sections", sectionHandler.List)
	api.GET("/sections/:id", sectionHandler.Get)
	api.PUT("/sections/:id", sectionHandler.Update)
	api.DELETE("/sections/:id", sectionHandler.Delete)

	themeHandler := handlers.NewThemeHandler(themeService)
	api.POST("/themes", themeHandler.Create)
	api.GET("/themes", themeHandler.List)
	api.GET("/themes/:id", themeHandler.Get)
	api.PUT("/themes/:id", themeHandler.Update)
	api.DELETE("/themes/:id", themeHandler.Delete)
	api.POST("/themes/upload", themeHandler.Upload)

	templateHandler := handlers.NewTemplateHandler(templateService)
	api.POST("/templates", templateHandler.Create)
	api.GET("/templates", templateHandler.List)
	api.GET("/templates/:id", templateHandler.Get)
	api.PUT("/templates/:id", templateHandler.Update)
	api.DELETE("/templates/:id", templateHandler.Delete)

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

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)

	// Start server in a goroutine
	go func() {
		fmt.Printf("Starting server on %s\n", addr)
		if err := e.Start(addr); err != nil {
			if err.Error() != "http: Server closed" {
				fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
				os.Exit(1)
			}
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	fmt.Println("\nShutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Server forced to shutdown: %v\n", err)
	}

	// Stop DI container
	stopCtx, stopCancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer stopCancel()

	if err := app.Stop(stopCtx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to shutdown dependencies: %v\n", err)
	}

	fmt.Println("Server stopped")
}

func loadConfig() config.Config {
	cfg := config.Default()
	cfg.LoadFromEnvironment()

	// Allow PORT env var as a common convention
	if portStr := os.Getenv("PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil && port > 0 {
			cfg.Server.Port = port
		}
	}

	return cfg
}
