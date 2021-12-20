package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/segmentio/ksuid"
	"github.com/wepala/weos-service/context"
	logs "github.com/wepala/weos-service/log"
	"github.com/wepala/weos-service/model"
)

//StandardMiddleware receiver for all the standard middleware that WeOS provides
type StandardMiddleware struct {
}

//RequestID generate request id
func (m *StandardMiddleware) RequestID(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cc := c.(*context.Context)
			req := cc.Request()
			res := cc.Response()
			rid := req.Header.Get(echo.HeaderXRequestID)
			if rid == "" {
				rid = ksuid.New().String()
			}
			res.Header().Set(echo.HeaderXRequestID, rid)

			return next(cc.WithValue(cc, context.REQUEST_ID, rid))
		}
	}
}

func (m *StandardMiddleware) Recover(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return middleware.Recover()(next)
	}
}

//ZapLogger switch to using ZapLogger
func (m *StandardMiddleware) ZapLogger(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			//setting the default logger in the context as zap with the default mode being error
			zapLogger, err := logs.NewZap("error")
			if err != nil {
				c.Logger().Errorf("Unexpected error setting the context logger : %s", err)
			}
			c.SetLogger(zapLogger)
			cc := c.(*context.Context)
			return next(cc)
		}
	}
}

func (m *StandardMiddleware) Logger(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return middleware.Logger()(next)
	}
}
