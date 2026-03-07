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

	// TODO: Add service variables to populate from Fx, e.g.:
	// var myService application.MyServiceInterface

	// Start DI container
	app := fx.New(
		fx.NopLogger,
		application.Module(cfg),
		// TODO: Use fx.Populate to extract services, e.g.:
		// fx.Populate(&myService),
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

	// TODO: Create handlers and inject services, e.g.:
	// myHandler := handlers.NewMyHandler(myService)

	// Register API routes under /api prefix
	api := e.Group("/api")
	api.GET("/health", handlers.HealthHandler)
	// TODO: Register your routes here, e.g.:
	// api.GET("/items", myHandler.List)
	// api.POST("/items", myHandler.Create)

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
