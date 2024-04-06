package rest

import (
	"encoding/json"
	"github.com/casbin/casbin/v2"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
	"go.uber.org/fx"
	"gorm.io/gorm"
	"net/http"
	"regexp"
	"runtime/debug"
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
	Config                *openapi3.T
	APIConfig             *APIConfig
	Echo                  *echo.Echo
	Logger                Log
	CommandDispatcher     CommandDispatcher
	ResourceRepository    *ResourceRepository
	Controllers           []map[string]Controller `group:"controllers"`
	Middlewares           []map[string]Middleware `group:"middlewares"`
	GORMDB                *gorm.DB
	AuthorizationEnforcer *casbin.Enforcer
	SecuritySchemes       map[string]Validator
}

// RouteInitializer initializes the routes for the application using the open api config
func RouteInitializer(p RouteParams) (err error) {
	// merge the controller configs into one map
	controllers := make(map[string]Controller)

	for _, config := range p.Controllers {
		for name, controller := range config {
			controllers[name] = controller
		}
	}
	// merge the middleware configs into one map
	middlewares := make(map[string]Middleware)
	for _, config := range p.Middlewares {
		for name, tmiddleware := range config {
			middlewares[name] = tmiddleware
		}
	}

	p.Echo.Add(http.MethodGet, p.APIConfig.BasePath+"/health", func(c echo.Context) error {
		return c.String(200, "OK")
	})
	// get the available methods and headers for each path. If no headers are specified then default to all
	pathMethods := make(map[string][]string)
	pathHeaders := make(map[string][]string)
	for path, pathItem := range p.Config.Paths.Map() {
		methods := make([]string, 0)
		headers := make([]string, 0)
		for method, operation := range pathItem.Operations() {
			methods = append(methods, method)
			for _, parameter := range operation.Parameters {
				if parameter.Value.In == "header" {
					headers = append(headers, parameter.Value.Name)
				}
			}
		}
		pathMethods[path] = methods
		pathHeaders[path] = headers

	}

	// read all the routes and configurations to determine which controller and
	for path, pathItem := range p.Config.Paths.Map() {
		var pathMiddleware []echo.MiddlewareFunc
		//set zap logger as a default middleware
		pathMiddleware = append(pathMiddleware, ZapLogger(&MiddlewareParams{
			Logger:             p.Logger,
			CommandDispatcher:  p.CommandDispatcher,
			ResourceRepository: p.ResourceRepository,
			Schema:             p.Config,
			APIConfig:          p.APIConfig,
		}))
		//set up the security middleware if there is a config setup
		if len(p.Config.Security) > 0 {
			pathMiddleware = append(pathMiddleware, SecurityMiddleware(&MiddlewareParams{
				Logger:          p.Logger,
				SecuritySchemes: p.SecuritySchemes,
				Schema:          p.Config,
				APIConfig:       p.APIConfig,
			}))

		}
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

		var methodsFound []string
		for method, operation := range pathItem.Operations() {
			var handler echo.HandlerFunc
			methodsFound = append(methodsFound, method)
			if controller, ok := operation.Extensions["x-controller"].(string); ok {
				if c, ok := controllers[controller]; ok {
					handler = c(&ControllerParams{
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
					})
				}
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
						APIConfig:          p.APIConfig,
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
						APIConfig:          p.APIConfig,
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
			re := regexp.MustCompile(`\{([a-zA-Z0-9\-_]+?)\}`)
			echoPath := re.ReplaceAllString(path, `:$1`)
			p.Echo.Add(method, p.APIConfig.BasePath+echoPath, handler, allMiddleware...)

			//setup security enforcer
			if authRaw, ok := operation.Extensions[AuthorizationConfigExtension]; ok {

				var err error

				defer func() {
					if err1 := recover(); err1 != nil {
						log.Error("panic occurred ", string(debug.Stack()))
					}
				}()

				//update path so that the open api way of specifying url parameters is change to wildcards. This is to support the casbin policy
				//note ideal we would use the open api way of specifying url parameters but this is not supported by casbin
				re := regexp.MustCompile(`\{([a-zA-Z0-9\-_]+?)\}`)
				path = re.ReplaceAllString(path, `*`)

				//add rule to the enforcer based on the operation
				var authConfig map[string]interface{}
				if err = json.Unmarshal(authRaw.(json.RawMessage), &authConfig); err == nil {
					if allowRules, ok := authConfig["allow"]; ok {
						//setup users
						if u, ok := allowRules.(map[string]interface{})["users"]; ok {
							for _, user := range u.([]interface{}) {
								if user == nil {
									log.Warnf("user is nil on path '%s' for method '%s'", path, method)
									continue
								}
								var success bool
								success, err = p.AuthorizationEnforcer.AddPolicy(user.(string), path, method)
								if !success {
									//TODO show warning to developer or something
								}
							}
						}
						//setup roles
						if u, ok := allowRules.(map[string]interface{})["roles"]; ok {
							for _, user := range u.([]interface{}) {
								var success bool
								if user == nil {
									log.Warnf("user is nil on path '%s' for method '%s'", path, method)
									continue
								}
								success, err = p.AuthorizationEnforcer.AddPolicy(user.(string), path, method)
								if !success {
									//TODO show warning to developer or something
								}
							}
						}
					}
				}
				return err
			}

		}
		//set up endpoint for options
		//setup CORS middleware
		var allowedMethods, allowedHeaders []string
		var ok bool
		if allowedMethods, ok = pathMethods[path]; !ok {
			allowedMethods = []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
		}
		if allowedHeaders, ok = pathHeaders[path]; !ok {
			allowedHeaders = []string{"*"}
		}
		//add the methods required by solid protocol
		allowedMethods = append(allowedMethods, http.MethodOptions, http.MethodHead)
		corsMiddleware := middleware.CORSWithConfig(middleware.CORSConfig{
			AllowOrigins: []string{"*"},
			AllowMethods: allowedMethods,
			AllowHeaders: allowedHeaders,
		})
		p.Echo.Add(http.MethodOptions, p.APIConfig.BasePath+path, func(c echo.Context) error {
			return c.NoContent(http.StatusOK)
		}, corsMiddleware)
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
	p.Echo.Add(http.MethodGet, p.APIConfig.BasePath+"/*", DefaultReadController(&ControllerParams{
		Logger:             p.Logger,
		CommandDispatcher:  p.CommandDispatcher,
		ResourceRepository: p.ResourceRepository,
		Schema:             p.Config,
	}))

	return err
}
