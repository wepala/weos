package rest

import (
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
	"golang.org/x/net/context"
	"os"
)

// registerHooks registers the hooks for the application
func registerHooks(lifecycle fx.Lifecycle, e *echo.Echo) {
	lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() {
				if err := e.Start(":" + os.Getenv("WEOS_PORT")); err != nil {
					e.Logger.Info("shutting down the server")
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return e.Shutdown(ctx)
		},
	})
}

var Core = fx.Module("weos-basic",
	fx.Provide(
		WeOSConfig,
		Config,
		NewEcho,
		NewZap,
		NewClient,
		NewGORM,
		NewCommandDispatcher,
		NewResourceRepository,
		NewGORMProjection,
		NewSecurityConfiguration,
	),
	fx.Invoke(RouteInitializer),
)

var API = fx.Module("rest",
	Core,
	fx.Invoke(registerHooks))
