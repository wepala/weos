package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/net/context"
	"regexp"
)

//Path initializers are run per path and can be used to configure routes that are not defined in the open api spec

//CORsInitializer sets up CORs for a specific path
func CORsInitializer(ctxt context.Context, api *RESTAPI, path string, swagger *openapi3.Swagger, pathItem *openapi3.PathItem) {
	//update path so that the open api way of specifying url parameters is change to the echo style of url parameters
	re := regexp.MustCompile(`\{([a-zA-Z0-9\-_]+?)\}`)
	echoPath := re.ReplaceAllString(path, `:$1`)
	//prep the middleware by setting up defaults
	allowedOrigins := []string{"*"}
	allowedHeaders := []string{"*"}
	//setup CORS check on options method
	corsMiddleware := middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: allowedOrigins,
		AllowHeaders: allowedHeaders,
		//AllowMethods: methodsFound,
	})

	api.EchoInstance().OPTIONS(api.config.BasePath+echoPath, func(context echo.Context) error {
		return nil
	}, corsMiddleware)
}
