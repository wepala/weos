package rest

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/segmentio/ksuid"
	"go.uber.org/zap"
	"golang.org/x/net/context"
	"os"
	"regexp"
	"time"
)

type MiddlewareParams struct {
	Logger             Log
	CommandDispatcher  CommandDispatcher
	ResourceRepository *ResourceRepository
	Schema             *openapi3.T
	APIConfig          *APIConfig
	PathMap            map[string]*openapi3.PathItem
	Operation          map[string]*openapi3.Operation
}

// ZapLogger switches the echo context logger to be ZapLogger
func ZapLogger(p *MiddlewareParams) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		var configuredLevel string
		var serviceName string
		if p.APIConfig.Log != nil {
			configuredLevel = p.APIConfig.Log.Level
			serviceName = p.APIConfig.Log.Name
		}
		return func(c echo.Context) error {
			//setting the default logger in the context as zap with the default mode being error
			req := c.Request()
			id := req.Header.Get(echo.HeaderXRequestID)
			if id == "" {
				id = ksuid.New().String()
				req.Header.Set(echo.HeaderXRequestID, id)
			}
			level := req.Header.Get("X-Log-Level")
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
			zapLogger, err := new(Zap).WithRequestID(serviceName, level, id)
			if err != nil {
				c.Logger().Errorf("Unexpected error setting the context logger : %s", err)
			}
			c.SetLogger(zapLogger)
			start := time.Now()
			cc := c.Request().Context()
			cc = context.WithValue(cc, echo.HeaderXRequestID, id)
			request := c.Request().WithContext(cc)
			c.SetRequest(request)
			err = next(c)
			response := c.Response()
			re := regexp.MustCompile(`^` + os.Getenv("BASE_PATH") + `/health`)
			if !re.MatchString(request.URL.Path) {
				zapLogger.With(
					zap.String("remote_ip", c.RealIP()),
					zap.String("uri", req.RequestURI),
					zap.Int("status", response.Status),
					zap.String("method", c.Request().Method),
					zap.Duration("latency", time.Since(start)),
					zap.Int64("response_size", response.Size),
					zap.String("referer", req.Referer()),
					zap.String("user_agent", req.UserAgent()),
				).Info("request")
			}
			return err
		}
	}
}
