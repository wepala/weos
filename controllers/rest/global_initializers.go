package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	weosContext "github.com/wepala/weos/context"
	"golang.org/x/net/context"
)

//GlobalMiddlewareInitializer adds user defined middleware to the initialize context
func GlobalMiddlewareInitializer(ctxt context.Context, api *RESTAPI, swagger *openapi3.Swagger) (context.Context, error) {
	if len(api.middlewares) != 0 {
		ctxt = context.WithValue(ctxt, weosContext.MIDDLEWARES, api.middlewares)
	}
	//OR do i check the security schemes and attach if the len is not 0
	return ctxt, nil
}
