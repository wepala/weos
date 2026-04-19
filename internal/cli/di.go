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
	"syscall"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/application/presets"
	"github.com/wepala/weos/v3/internal/config"

	"go.uber.org/fx"
)

// Dependencies holds all the dependencies needed by CLI commands.
type Dependencies struct {
	ResourceTypeService application.ResourceTypeService
	ResourceService     application.ResourceService
	App                 *fx.App
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
	var resourceTypeService application.ResourceTypeService
	var resourceService application.ResourceService

	app := fx.New(
		application.Module(appCfg, presets.NewDefaultRegistry()),
		fx.Invoke(func(
			rts application.ResourceTypeService,
			rs application.ResourceService,
		) {
			resourceTypeService = rts
			resourceService = rs
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
		ResourceTypeService: resourceTypeService,
		ResourceService:     resourceService,
		App:                 app,
	}, nil
}
