package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	weoscontext "github.com/wepala/weos/context"
	"golang.org/x/net/context"
	"regexp"
)

//CORsInitializer sets up CORs for a specific path
func CORsInitializer(ctxt context.Context, api Container, path string, swagger *openapi3.Swagger, pathItem *openapi3.PathItem) (context.Context, error) {
	//update path so that the open api way of specifying url parameters is change to the echo style of url parameters
	re := regexp.MustCompile(`\{([a-zA-Z0-9\-_]+?)\}`)
	echoPath := re.ReplaceAllString(path, `:$1`)
	tmethodsFound := ctxt.Value(weoscontext.METHODS_FOUND)
	if methodsFound, ok := tmethodsFound.([]string); ok {
		//prep the middleware by setting up defaults
		allowedOrigins := []string{"*"}
		allowedHeaders := []string{"*"}
		//setup CORS check on options method
		corsMiddleware := middleware.CORSWithConfig(middleware.CORSConfig{
			AllowOrigins: allowedOrigins,
			AllowHeaders: allowedHeaders,
			AllowMethods: methodsFound,
		})

		api.(*RESTAPI).EchoInstance().OPTIONS(api.GetWeOSConfig().BasePath+echoPath, func(context echo.Context) error {
			return nil
		}, corsMiddleware)
	}

	return ctxt, nil
}
