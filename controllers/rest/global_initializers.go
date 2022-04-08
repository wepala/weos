package rest

import (
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/gorilla/sessions"
	weosContext "github.com/wepala/weos/context"
	"golang.org/x/net/context"
	"os"
)

//Security adds authorization middleware to the initialize context
func Security(ctxt context.Context, api *RESTAPI, swagger *openapi3.Swagger) (context.Context, error) {
	middlewares := GetOperationMiddlewares(ctxt)

	for _, security := range swagger.Security {
		for key, _ := range security {
			if swagger.Components.SecuritySchemes != nil && swagger.Components.SecuritySchemes[key] != nil {
				switch swagger.Components.SecuritySchemes[key].Value.Type {
				case "openIdConnect":
					if middleware, _ := api.GetMiddleware("OpenIDMiddleware"); middleware != nil {
						middlewares = append(middlewares, middleware)
					}
					ctxt = context.WithValue(ctxt, weosContext.MIDDLEWARES, middlewares)
				}
			} else {
				api.EchoInstance().Logger.Errorf("unexpected error: security defined does not match any security schemes")
				return ctxt, fmt.Errorf("unexpected error: security defined does not match any security schemes")
			}
		}

	}

	//checking for security scheme for session, if found then instantiate a session and add to api
	for _, security := range swagger.Components.SecuritySchemes {
		if security.Value.In == "cookie" && security.Value.Name != "" {
			// initialize cookie store and set session name in env
			store := sessions.NewCookieStore([]byte(security.Value.Name))
			api.sessionStore = store
			os.Setenv("session_name", security.Value.Name)
		}
	}
	return ctxt, nil
}
