package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"weos/application"
	"weos/internal/config"

	"go.uber.org/fx"
)

// Dependencies holds all the dependencies needed by CLI commands.
// Add service interfaces here as your application grows.
type Dependencies struct {
	// TODO: Add your service interfaces here, e.g.:
	// MyService application.MyServiceInterface
	App *fx.App
}

// StartContainer starts the Fx container and returns dependencies.
func StartContainer(cliCfg *CLIConfig) (*Dependencies, error) {
	return startContainerWithConfig(cliCfg.Config)
}

// Shutdown gracefully shuts down the Fx container.
func (d *Dependencies) Shutdown() error {
	if d.App == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), fx.DefaultTimeout)
	defer cancel()

	return d.App.Stop(ctx)
}

// StartContainerWithDSN starts the Fx container with the given database DSN.
// Useful for E2E in-process testing so tests run in the same process.
func StartContainerWithDSN(dsn string) (*Dependencies, error) {
	appCfg := config.Default()
	appCfg.DatabaseDSN = dsn
	appCfg.LoadFromEnvironment()
	return startContainerWithConfig(appCfg)
}

func startContainerWithConfig(appCfg config.Config) (*Dependencies, error) {
	// TODO: Add your service variables here to extract from the DI container, e.g.:
	// var myService application.MyServiceInterface

	app := fx.New(
		application.Module(appCfg),
		fx.Invoke(func(
		// TODO: Add service parameters here to extract, e.g.:
		// svc application.MyServiceInterface,
		) {
			// TODO: Assign extracted services, e.g.:
			// myService = svc
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	startCtx, startCancel := context.WithTimeout(ctx, fx.DefaultTimeout)
	defer startCancel()

	if err := app.Start(startCtx); err != nil {
		return nil, fmt.Errorf("failed to start application: %w", err)
	}

	return &Dependencies{
		// TODO: Assign extracted services, e.g.:
		// MyService: myService,
		App: app,
	}, nil
}
