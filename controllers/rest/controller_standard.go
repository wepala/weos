package rest

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"github.com/segmentio/ksuid"
	context2 "github.com/wepala/weos-service/context"
	"golang.org/x/net/context"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos-service/model"
)

type StandardControllers struct {
}

//Create is used for a single payload. It dispatches this to the model which then validates and creates it.
func (c *StandardControllers) Create(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.HandlerFunc {
	var contentType string
	var contentTypeSchema *openapi3.SchemaRef
	//get the entity information based on the Content Type associated with this operation
	for _, requestContent := range operation.RequestBody.Value.Content {
		//use the first schema ref to determine the entity type
		if requestContent.Schema.Ref != "" {
			contentType = strings.Replace(requestContent.Schema.Ref, "#/components/schemas/", "", -1)
			//get the schema details from the swagger file
			contentTypeSchema = spec.Components.Schemas[contentType]
			break
		}
	}
	return func(ctxt echo.Context) error {
		//look up the schema for the content type so that we could identify the rules
		newContext := ctxt.Request().Context()
		if contentType != "" && contentTypeSchema.Value != nil {
			newContext = context.WithValue(newContext, context2.CONTENT_TYPE, &context2.ContentType{
				Name:   contentType,
				Schema: contentTypeSchema.Value,
			})
		}
		//reads the request body
		payload, _ := ioutil.ReadAll(ctxt.Request().Body)
		weosID := ksuid.New().String()

		err := app.Dispatcher().Dispatch(newContext, model.Create(newContext, payload, contentType, weosID))
		if err != nil {
			if errr, ok := err.(*model.DomainError); ok {
				return NewControllerError(errr.Error(), err, http.StatusBadRequest)
			} else {
				return NewControllerError("unexpected error creating content type", err, http.StatusBadRequest)
			}
		}
		var result *model.ContentEntity
		var Etag string
		for _, projection := range app.Projections() {
			if projection != nil {
				result, err := projection.GetContentEntity(newContext, weosID)
				if err != nil {
					return err
				}
				Etag = NewEtag(result)
			}
		}
		result.ID = ""
		result.SequenceNo = 0
		ctxt.Response().Header().Set("Etag", Etag)
		return ctxt.JSON(http.StatusCreated, result)
	}
}

//CreateBatch is used for an array of payloads. It dispatches this to the model which then validates and creates it.
func (c *StandardControllers) CreateBatch(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.HandlerFunc {
	var contentType string
	var contentTypeSchema *openapi3.SchemaRef
	//get the entity information based on the Content Type associated with this operation
	for _, requestContent := range operation.RequestBody.Value.Content {
		//use the first schema ref to determine the entity type
		if requestContent.Schema.Value.Items != nil && strings.Contains(requestContent.Schema.Value.Items.Value.Type, "#/components/schemas/") {
			contentType = strings.Replace(requestContent.Schema.Value.Items.Value.Type, "#/components/schemas/", "", -1)
			//get the schema details from the swagger file
			contentTypeSchema = spec.Components.Schemas[contentType]
			break
		}
	}
	return func(ctxt echo.Context) error {
		//look up the schema for the content type so that we could identify the rules
		newContext := ctxt.Request().Context()
		if contentType != "" && contentTypeSchema.Value != nil {
			newContext = context.WithValue(newContext, context2.CONTENT_TYPE, &context2.ContentType{
				Name:   contentType,
				Schema: contentTypeSchema.Value,
			})
		}
		//reads the request body
		payload, _ := ioutil.ReadAll(ctxt.Request().Body)

		err := app.Dispatcher().Dispatch(newContext, model.CreateBatch(newContext, payload, contentType))
		if err != nil {
			if errr, ok := err.(*model.DomainError); ok {
				return NewControllerError(errr.Error(), err, http.StatusBadRequest)
			} else {
				return NewControllerError("unexpected error updating content type batch", err, http.StatusBadRequest)
			}
		}
		return ctxt.JSON(http.StatusCreated, "CreatedBatch")
	}
}

func (c *StandardControllers) Update(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.HandlerFunc {
	var contentType string
	var contentTypeSchema *openapi3.SchemaRef
	//get the entity information based on the Content Type associated with this operation
	for _, requestContent := range operation.RequestBody.Value.Content {
		//use the first schema ref to determine the entity type
		if requestContent.Schema.Ref != "" {
			contentType = strings.Replace(requestContent.Schema.Ref, "#/components/schemas/", "", -1)
			//get the schema details from the swagger file
			contentTypeSchema = spec.Components.Schemas[contentType]
			break
		}
	}
	return func(ctxt echo.Context) error {
		//look up the schema for the content type so that we could identify the rules
		newContext := ctxt.Request().Context()
		cType := &context2.ContentType{}
		if contentType != "" && contentTypeSchema.Value != nil {
			cType = &context2.ContentType{
				Name:   contentType,
				Schema: contentTypeSchema.Value,
			}
			newContext = context.WithValue(newContext, context2.CONTENT_TYPE, cType)
		}
		var weosID string
		var newPayload map[string]interface{}
		//reads the request body
		payload, _ := ioutil.ReadAll(ctxt.Request().Body)
		//getting etag from context
		etagInterface := newContext.Value("If-Match")
		if etagInterface != nil {
			etag := etagInterface.(string)
			weosID, sequenceNo := SplitEtag(etag)
			json.Unmarshal(payload, &newPayload)
			newPayload["weos_id"] = weosID
			newPayload["sequence_no"] = sequenceNo
			payload, _ = json.Marshal(newPayload)

		}

		err := app.Dispatcher().Dispatch(newContext, model.Update(newContext, payload, contentType))
		if err != nil {
			if errr, ok := err.(*model.DomainError); ok {
				if strings.Contains(errr.Error(), "error updating entity. This is a stale item") {
					return NewControllerError(errr.Error(), err, http.StatusPreconditionFailed)
				}
				if strings.Contains(errr.Error(), "invalid:") {
					return NewControllerError(errr.Error(), err, http.StatusUnprocessableEntity)
				}
				return NewControllerError(errr.Error(), err, http.StatusBadRequest)
			} else {
				return NewControllerError("unexpected error updating content type", err, http.StatusBadRequest)
			}
		}
		var Etag string
		var identifiers []string
		var result *model.ContentEntity
		var result1 map[string]interface{}
		if etagInterface == nil {
			//find entity based on identifiers specified
			pks, _ := json.Marshal(contentTypeSchema.Value.Extensions["x-identifier"])
			json.Unmarshal(pks, &identifiers)
			if len(identifiers) == 0 {
				identifiers = append(identifiers, "id")
			}
			primaryKeys := map[string]interface{}{}
			for _, p := range identifiers {
				primaryKeys[p] = newContext.Value(p)
			}

			for _, projection := range app.Projections() {
				if projection != nil {
					result1, err = projection.GetByKey(newContext, cType, primaryKeys)
					if err != nil {
						return err
					}

				}
			}
			weos_id, ok := result1["weos_id"].(string)
			sequenceString := fmt.Sprint(result1["sequence_no"])
			sequenceNo, _ := strconv.Atoi(sequenceString)
			Etag = NewEtag(&model.ContentEntity{
				AggregateRoot: model.AggregateRoot{
					SequenceNo:  int64(sequenceNo),
					BasicEntity: model.BasicEntity{ID: weos_id},
				},
			})
			if (len(result1) == 0) || !ok || weos_id == "" {
				return NewControllerError("No entity found", err, http.StatusNotFound)
			} else if err != nil {
				return NewControllerError(err.Error(), err, http.StatusBadRequest)
			}
			result1["weos_id"] = ""
			result1["SequenceNo"] = 0

			ctxt.Response().Header().Set("Etag", Etag)

			return ctxt.JSON(http.StatusOK, result)
		} else {
			//find contentEntity based on weosid
			for _, projection := range app.Projections() {
				if projection != nil {
					result, err = projection.GetContentEntity(newContext, weosID)
					if err != nil {
						return err
					}

				}
			}
			if result == nil || result.ID == "" {
				return NewControllerError("No entity found", err, http.StatusNotFound)
			} else if err != nil {
				return NewControllerError(err.Error(), err, http.StatusBadRequest)
			}
			Etag = NewEtag(result)
			result.ID = ""
			result.SequenceNo = 0

			ctxt.Response().Header().Set("Etag", Etag)

			return ctxt.JSON(http.StatusOK, result)
		}

	}
}

func (c *StandardControllers) BulkUpdate(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.HandlerFunc {
	return func(ctxt echo.Context) error {
		return nil
	}
}

func (c *StandardControllers) View(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.HandlerFunc {
	return func(ctxt echo.Context) error {

		return ctxt.JSON(http.StatusOK, "View Item")
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

func (c *StandardControllers) Get(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.HandlerFunc {
	return func(ctxt echo.Context) error {
		//TODO call GetByID

		return ctxt.JSON(200, nil)
	}
}
func (c *StandardControllers) HealthCheck(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.HandlerFunc {
	return func(context echo.Context) error {
		response := &HealthCheckResponse{
			Version: spec.Info.Version,
		}
		return context.JSON(http.StatusOK, response)
	}

}
