package rest

import (
	"context"
	"encoding/csv"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/segmentio/ksuid"
	context2 "github.com/wepala/weos/context"
	logs "github.com/wepala/weos/log"
	"github.com/wepala/weos/model"
	"go.uber.org/zap"
	"net/http"
	"time"
)

//RequestID generate request id
func RequestID(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cc := c.Request().Context()
			rid := c.Request().Header.Get(echo.HeaderXRequestID)
			if rid == "" {
				rid = ksuid.New().String()
			}
			c.Response().Header().Set(echo.HeaderXRequestID, rid)
			request := c.Request().WithContext(context.WithValue(cc, context2.REQUEST_ID, rid))
			c.SetRequest(request)
			return next(c)
		}
	}
}

func Recover(api Container, commandDispatcher model.CommandDispatcher, repository model.EntityRepository, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return middleware.Recover()(next)
	}
}

func Logger(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return middleware.Logger()(next)
	}
}

//CSVUpload parse csv and add items as "_items" to context
func CSVUpload(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			var csvFile *csv.Reader
			//if it's a csv file upload then use the body
			if c.Request().Header.Get("content-type") == "text/csv" {
				csvFile = csv.NewReader(c.Request().Body)
			} else if c.Request().Header.Get("content-type") == "multipart/form-data" {
				file, err := c.FormFile("_csv_upload")
				if err != nil {
					c.Logger().Debugf("Error retrieving upload file %s", err)
					return NewControllerError("error with file upload", err, http.StatusBadRequest)
				}
				src, err := file.Open()
				if err != nil {
					c.Logger().Debugf("Error retrieving upload file %s", err)
					return NewControllerError("error with file upload", err, http.StatusBadRequest)
				}
				defer src.Close()
				csvFile = csv.NewReader(src)
			}

			records, err := csvFile.ReadAll()
			if err != nil {
				c.Logger().Debugf("Error reading csv file %s", err)
				return NewControllerError("invalid csv", err, http.StatusBadRequest)
			}

			cc := context.WithValue(c.Request().Context(), "_items", records)
			request := c.Request().WithContext(cc)
			c.SetRequest(request)
			return next(c)
		}
	}
}

//ZapLogger switches the echo context logger to be ZapLogger
func ZapLogger(api Container, commandDispatcher model.CommandDispatcher, repository model.EntityRepository, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		var configuredLevel string
		var serviceName string
		if api.GetWeOSConfig().Log != nil {
			configuredLevel = api.GetWeOSConfig().Log.Level
			serviceName = api.GetWeOSConfig().Log.Name
		}
		return func(c echo.Context) error {
			//setting the default logger in the context as zap with the default mode being error
			req := c.Request()
			id := req.Header.Get(echo.HeaderXRequestID)
			if id == "" {
				id = ksuid.New().String()
				req.Header.Set(echo.HeaderXRequestID, id)
			}
			level := req.Header.Get(context2.HeaderXLogLevel)
			if level == "" {
				if configuredLevel != "" {
					level = configuredLevel
				} else { //by default only show errors
					level = "info"
				}
			} else {
				//only allow setting the level to debug from this header for security reasons
				level = "debug"
			}
			if serviceName == "" {
				serviceName = "weos"
			}
			zapLogger, err := new(logs.Zap).WithRequestID(serviceName, level, id)
			if err != nil {
				c.Logger().Errorf("Unexpected error setting the context logger : %s", err)
			}
			c.SetLogger(zapLogger)
			start := time.Now()
			cc := c.Request().Context()
			cc = context.WithValue(cc, echo.HeaderXRequestID, id)
			request := c.Request().WithContext(cc)
			c.SetRequest(request)
			next(c)
			response := c.Response()
			if req.URL.Path != "/health" {
				zapLogger.With(
					zap.String("remote_ip", c.RealIP()),
					zap.String("uri", req.RequestURI),
					zap.Int("status", response.Status),
					zap.String("method", c.Request().Method),
					zap.Duration("latency", time.Since(start)),
					zap.Int64("response_size", response.Size),
					zap.String("referer", req.Referer()),
					zap.String("user", context2.GetUser(req.Context())),
					zap.String("user_agent", req.UserAgent()),
				).Info("request")
			} else {
				zapLogger.With(
					zap.String("remote_ip", c.RealIP()),
					zap.String("uri", req.RequestURI),
					zap.Int("status", response.Status),
					zap.String("method", c.Request().Method),
					zap.Duration("latency", time.Since(start)),
					zap.Int64("response_size", response.Size),
					zap.String("referer", req.Referer()),
					zap.String("user_agent", req.UserAgent()),
				).Debug("request")
			}
			return nil
		}
	}
}
