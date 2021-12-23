package rest

import (
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos-service/model"
)

type StandardControllers struct {
}

func (c *StandardControllers) Create(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.HandlerFunc {
	var contentType string
	//get the entity information based on the Content Type associated with this operation
	for _, requestContent := range operation.RequestBody.Value.Content {
		//use the first schema ref to determine the entity type
		if requestContent.Schema.Ref != "" {
			contentType = strings.Replace(requestContent.Schema.Ref, "#/components/schemas/", "", -1)
			//TODO look up the schema for the content type so that we could identify the rules
			break
		}
	}
	return func(ctxt echo.Context) error {
		//reads the request body
		payload, _ := ioutil.ReadAll(ctxt.Request().Body)
		app.Dispatcher().Dispatch(ctxt.Request().Context(), model.Create(ctxt.Request().Context(), payload, contentType))
		return ctxt.JSON(http.StatusCreated, "Created")
	}
}

//TODO needs to be fleshed out
func (c *StandardControllers) CreateBatch(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.HandlerFunc {
	return func(ctxt echo.Context) error {
		var entityType string
		//get the entity information based on the Content Type associated with this operation
		for _, requestContent := range operation.RequestBody.Value.Content {
			//use the first schema ref to determine the entity type
			if requestContent.Schema.Ref != "" {
				entityType = strings.Replace(requestContent.Schema.Ref, "#/components/schemas/", "", -1)
				break
			}
		}
		//reads the request body
		payload, _ := ioutil.ReadAll(ctxt.Request().Body)

		app.Dispatcher().Dispatch(ctxt.Request().Context(), model.CreateBatch(ctxt.Request().Context(), payload, entityType))

		return ctxt.JSON(http.StatusCreated, "CreatedBatch")
	}
}

func (c *StandardControllers) Update(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.HandlerFunc {
	return func(context echo.Context) error {

		return nil
	}
}

func (c *StandardControllers) BulkUpdate(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.HandlerFunc {
	return func(context echo.Context) error {

		return nil
	}
}

func (c *StandardControllers) List(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.HandlerFunc {
	return func(context echo.Context) error {

		return nil
	}
}

func (c *StandardControllers) Delete(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.HandlerFunc {
	return func(context echo.Context) error {

		return nil
	}
}
