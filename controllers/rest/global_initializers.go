package rest

import (
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	weosContext "github.com/wepala/weos/context"
	"golang.org/x/net/context"
)

//Security adds authorization middleware to the initialize context
func Security(ctxt context.Context, api *RESTAPI, swagger *openapi3.Swagger) (context.Context, error) {
	middlewares := GetOperationMiddlewares(ctxt)
	found := false

	for _, security := range swagger.Security {
		for key, _ := range security {
			if swagger.Components.SecuritySchemes != nil && swagger.Components.SecuritySchemes[key] != nil {
				//checks if the security scheme has type openIdConnect
				if swagger.Components.SecuritySchemes[key].Value.Type == "openIdConnect" {
					found = true
					break
				}

			}
		}

	}
	if found {
		if middleware, _ := api.GetMiddleware("OpenIDMiddleware"); middleware != nil {
			middlewares = append(middlewares, middleware)
		}
		ctxt = context.WithValue(ctxt, weosContext.MIDDLEWARES, middlewares)
	} else {
		if swagger.Components.SecuritySchemes != nil || swagger.Security != nil {
			api.EchoInstance().Logger.Errorf("unexpected error: security defined does not match any security schemes")
			return ctxt, fmt.Errorf("unexpected error: security defined does not match any security schemes")
		}

	}
	return ctxt, nil
}
