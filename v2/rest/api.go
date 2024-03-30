package rest

import (
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
	"golang.org/x/net/context"
)

// registerHooks registers the hooks for the application
func registerHooks(lifecycle fx.Lifecycle, e *echo.Echo) {
	lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() {
				if err := e.Start(":8681"); err != nil {
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

var API = fx.Module("rest",
	fx.Provide(
		WeOSConfig,
		Config,
		NewEcho,
		NewZap,
		NewGORM,
		NewGORMResourceRepository,
		NewCommandDispatcher,
		NewEventDispatcher,
	),
	fx.Invoke(RouteInitializer, registerHooks),
)
