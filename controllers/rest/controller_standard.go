package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	weoscontext "github.com/wepala/weos/context"
	"github.com/wepala/weos/model"
	_ "github.com/wepala/weos/swaggerui"
	"golang.org/x/net/context"
	"net/http"
	"strconv"
)

func APIDiscovery(api Container, commandDispatcher model.CommandDispatcher, repository model.EntityRepository, pathMap map[string]*openapi3.PathItem, operation map[string]*openapi3.Operation) echo.HandlerFunc {
	return func(ctxt echo.Context) error {
		newContext := ctxt.Request().Context()

		//get content type expected for 200 response
		responseType := newContext.Value(weoscontext.RESPONSE_PREFIX + strconv.Itoa(http.StatusOK))
		if responseType == "application/json" {
			return ctxt.JSON(http.StatusOK, api.GetConfig())
		} else if responseType == "application/html" {
			return ctxt.Redirect(http.StatusPermanentRedirect, SWAGGERUIENDPOINT)
		}

		return NewControllerError("No response format chosen for a valid response", nil, http.StatusBadRequest)
	}
}

func HealthCheck(api Container, commandDispatcher model.CommandDispatcher, repository model.EntityRepository, pathMap map[string]*openapi3.PathItem, operation map[string]*openapi3.Operation) echo.HandlerFunc {
	return func(context echo.Context) error {
		response := &HealthCheckResponse{
			Version: api.GetConfig().Info.Version,
		}
		return context.JSON(http.StatusOK, response)
	}

}

func LogLevel(tapi Container, commandDispatcher model.CommandDispatcher, repository model.EntityRepository, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	api := tapi.(*RESTAPI)
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			newContext := c.Request().Context()
			req := c.Request()
			res := c.Response()
			level := req.Header.Get(weoscontext.HeaderXLogLevel)
			if level == "" {
				level = "error"
			}

			res.Header().Set(weoscontext.HeaderXLogLevel, level)

			//Set the log.level in context based on what is passed into the header
			switch level {
			case "debug":
				c.Logger().SetLevel(log.DEBUG)
			case "info":
				c.Logger().SetLevel(log.INFO)
			case "warn":
				c.Logger().SetLevel(log.WARN)
			case "error":
				c.Logger().SetLevel(log.ERROR)
			}

			//Sets the logger on the application object
			if api.GetWeOSConfig() == nil {
				api.Config = &APIConfig{}
			}

			if api.GetWeOSConfig().Log == nil {
				api.GetWeOSConfig().Log = &model.LogConfig{}
			}

			api.GetWeOSConfig().Log.Level = level

			//Assigns the log level to context
			newContext = context.WithValue(newContext, weoscontext.HeaderXLogLevel, level)
			request := c.Request().WithContext(newContext)
			c.SetRequest(request)
			return next(c)
		}
	}
}
