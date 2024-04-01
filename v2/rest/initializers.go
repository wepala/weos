package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
	"net/http"
)

type ControllerConfig struct {
	Name       string
	Controller Controller
}

type MiddlewareConfig struct {
	Name       string
	Middleware Middleware
}

type RouteParams struct {
	fx.In
	Config             *openapi3.T
	APIConfig          *APIConfig
	Echo               *echo.Echo
	Logger             Log
	CommandDispatcher  CommandDispatcher
	ResourceRepository *ResourceRepository
	Controllers        []map[string]Controller `group:"controllers"`
	Middlewares        []map[string]Middleware `group:"middlewares"`
}

func RouteInitializer(p RouteParams) (err error) {
	//merge the controller configs into one map
	var controllers map[string]Controller
	for _, config := range p.Controllers {
		for name, controller := range config {
			controllers[name] = controller
		}
	}
	//merge the middleware configs into one map
	var middlewares map[string]Middleware
	for _, config := range p.Middlewares {
		for name, tmiddleware := range config {
			middlewares[name] = tmiddleware
		}
	}

	p.Echo.Add(http.MethodGet, "/health", func(c echo.Context) error {
		return c.String(200, "OK")
	})
	//TODO read all the routes and configurations to determine which controller and
	for path, pathItem := range p.Config.Paths.Map() {
		var pathMiddleware []echo.MiddlewareFunc
		//get the middleware set on the path
		if tmiddlewares, ok := pathItem.Extensions["x-middleware"].([]string); ok {
			for _, middlewareName := range tmiddlewares {
				if middleware, ok := middlewares[middlewareName]; ok {
					pathMiddleware = append(pathMiddleware, middleware(&MiddlewareParams{
						Logger:             p.Logger,
						CommandDispatcher:  p.CommandDispatcher,
						ResourceRepository: p.ResourceRepository,
						Schema:             p.Config,
						APIConfig:          p.APIConfig,
						PathMap: map[string]*openapi3.PathItem{
							path: pathItem,
						},
						Operation: nil,
					}))
				}
			}
		}

		var handler echo.HandlerFunc
		var methodsFound []string
		for method, operation := range pathItem.Operations() {
			methodsFound = append(methodsFound, method)
			if controller, ok := operation.Extensions["x-controller"].(string); ok {
				if c, ok := controllers[controller]; ok {
					handler = c(&ControllerParams{
						Logger:             p.Logger,
						CommandDispatcher:  p.CommandDispatcher,
						ResourceRepository: p.ResourceRepository,
						Schema:             p.Config,
						PathMap: map[string]*openapi3.PathItem{
							path: pathItem,
						},
						Operation: map[string]*openapi3.Operation{
							method: operation,
						},
					})
				}
			} else {
				//TODO set default controllers based on the operation
			}

			if handler == nil {
				//set default controller based on the method
				switch method {
				case http.MethodGet:
					handler = DefaultReadController(&ControllerParams{
						Logger:             p.Logger,
						CommandDispatcher:  p.CommandDispatcher,
						ResourceRepository: p.ResourceRepository,
						Schema:             p.Config,
						PathMap: map[string]*openapi3.PathItem{
							path: pathItem,
						},
						Operation: map[string]*openapi3.Operation{
							method: operation,
						},
					})
				default:
					handler = DefaultWriteController(&ControllerParams{
						Logger:             p.Logger,
						CommandDispatcher:  p.CommandDispatcher,
						ResourceRepository: p.ResourceRepository,
						Schema:             p.Config,
						PathMap: map[string]*openapi3.PathItem{
							path: pathItem,
						},
						Operation: map[string]*openapi3.Operation{
							method: operation,
						},
					})
				}
			}

			var operationMiddleware []echo.MiddlewareFunc
			if tmiddlewares, ok := operation.Extensions["x-middleware"].([]string); ok {
				for _, middlewareName := range tmiddlewares {
					if middleware, ok := middlewares[middlewareName]; ok {
						operationMiddleware = append(operationMiddleware, middleware(&MiddlewareParams{
							Logger:             p.Logger,
							CommandDispatcher:  p.CommandDispatcher,
							ResourceRepository: p.ResourceRepository,
							Schema:             p.Config,
							APIConfig:          p.APIConfig,
							PathMap: map[string]*openapi3.PathItem{
								path: pathItem,
							},
							Operation: map[string]*openapi3.Operation{
								method: operation,
							},
						}))
					}
				}
			}

			allMiddleware := make([]echo.MiddlewareFunc, 0)
			allMiddleware = append(allMiddleware, pathMiddleware...)
			allMiddleware = append(allMiddleware, operationMiddleware...)
			// Add the middleware to the routes
			p.Echo.Add(method, p.APIConfig.BasePath+path, handler, allMiddleware...)
		}
	}

	p.Echo.Add(http.MethodPost, p.APIConfig.BasePath+"/*", DefaultWriteController(&ControllerParams{
		Logger:             p.Logger,
		CommandDispatcher:  p.CommandDispatcher,
		ResourceRepository: p.ResourceRepository,
		Schema:             p.Config,
	}))
	p.Echo.Add(http.MethodPut, p.APIConfig.BasePath+"/*", DefaultWriteController(&ControllerParams{
		Logger:             p.Logger,
		CommandDispatcher:  p.CommandDispatcher,
		ResourceRepository: p.ResourceRepository,
		Schema:             p.Config,
	}))
	p.Echo.Add(http.MethodPatch, p.APIConfig.BasePath+"/*", DefaultWriteController(&ControllerParams{
		Logger:             p.Logger,
		CommandDispatcher:  p.CommandDispatcher,
		ResourceRepository: p.ResourceRepository,
		Schema:             p.Config,
	}))

	return err
}
