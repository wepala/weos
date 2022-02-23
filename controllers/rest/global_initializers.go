package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	weosContext "github.com/wepala/weos/context"
	"golang.org/x/net/context"
)

//Security adds authorization middleware to the initialize context
func Security(ctxt context.Context, api *RESTAPI, swagger *openapi3.Swagger) (context.Context, error) {
	middlewares := GetOperationMiddlewares(ctxt)
	found := false
	for _, scheme := range swagger.Components.SecuritySchemes {
		//checks if the security scheme has type openIdConnect
		if scheme.Value.Type == "openIdConnect" {
			found = true
		}
	}
	if found {
		if middleware, _ := api.GetMiddleware("OpenIDMiddleware"); middleware != nil {
			middlewares = append(middlewares, middleware)
		}
		ctxt = context.WithValue(ctxt, weosContext.MIDDLEWARES, middlewares)
	}
	return ctxt, nil
}
