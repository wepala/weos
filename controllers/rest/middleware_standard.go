package rest

import (
	"context"
	"encoding/csv"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/segmentio/ksuid"
	context2 "github.com/wepala/weos/context"
	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"
	"net/http"
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

func Recover(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
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
