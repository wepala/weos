package rest

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"github.com/segmentio/ksuid"
	context2 "github.com/wepala/weos-service/context"
	"github.com/wepala/weos-service/projections"
	"golang.org/x/net/context"
	"gorm.io/gorm"

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

		//for inserting weos_id during testing
		payMap := map[string]interface{}{}
		json.Unmarshal(payload, &payMap)
		weosID := ksuid.New().String()
		if v, ok := payMap["weos_id"]; ok {
			if val, ok := v.(string); ok {
				weosID = val
			}
		}

		err := app.Dispatcher().Dispatch(newContext, model.Create(newContext, payload, contentType, weosID))
		if err != nil {
			if errr, ok := err.(*model.DomainError); ok {
				return NewControllerError(errr.Error(), err, http.StatusBadRequest)
			} else {
				return NewControllerError("unexpected error creating content type", err, http.StatusBadRequest)
			}
		}

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

		ctxt.Response().Header().Set("Etag", Etag)
		return ctxt.JSON(http.StatusCreated, "Created")
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
	//var contentType string
	//var contentTypeSchema *openapi3.SchemaRef
	////get the entity information based on the Content Type associated with this operation
	//for _, requestContent := range operation.RequestBody.Value.Content {
	//	//use the first schema ref to determine the entity type
	//	if requestContent.Schema.Ref != "" {
	//		contentType = strings.Replace(requestContent.Schema.Ref, "#/components/schemas/", "", -1)
	//		//get the schema details from the swagger file
	//		contentTypeSchema = spec.Components.Schemas[contentType]
	//		break
	//	}
	//}
	return func(ctxt echo.Context) error {
		//look up the schema for the content type so that we could identify the rules
		//newContext := ctxt.Request().Context()
		//if contentType != "" && contentTypeSchema.Value != nil {
		//	newContext = context.WithValue(newContext, context2.CONTENT_TYPE, &context2.ContentType{
		//		Name:   contentType,
		//		Schema: contentTypeSchema.Value,
		//	})
		//}
		//
		////reads the request body
		//payload, _ := ioutil.ReadAll(ctxt.Request().Body)
		//
		//err := app.Dispatcher().Dispatch(newContext, model.Update(newContext, payload, contentType))
		//if err != nil {
		//	if errr, ok := err.(*model.DomainError); ok {
		//		return NewControllerError(errr.Error(), err, http.StatusBadRequest)
		//	} else {
		//		return NewControllerError("unexpected error updating content type", err, http.StatusBadRequest)
		//	}
		//}

		return ctxt.JSON(http.StatusOK, "Updated")
	}
}

func (c *StandardControllers) BulkUpdate(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.HandlerFunc {
	return func(ctxt echo.Context) error {
		return nil
	}
}

func (c *StandardControllers) View(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.HandlerFunc {
	var contentType string
	var contentTypeSchema *openapi3.SchemaRef
	//get the entity information based on the Content Type associated with this operation
	for _, requestContent := range operation.Responses.Get(http.StatusOK).Value.Content {
		//use the first schema ref to determine the entity type
		if requestContent.Schema.Ref != "" {
			contentType = strings.Replace(requestContent.Schema.Ref, "#/components/schemas/", "", -1)
			//get the schema details from the swagger file
			contentTypeSchema = spec.Components.Schemas[contentType]
			break
		}
	}

	return func(ctxt echo.Context) error {
		cType := &context2.ContentType{}
		if contentType != "" && contentTypeSchema.Value != nil {
			cType = &context2.ContentType{
				Name:   contentType,
				Schema: contentTypeSchema.Value,
			}
		}

		pks, _ := json.Marshal(cType.Schema.Extensions["x-identifier"])
		primaryKeys := []string{}
		json.Unmarshal(pks, &primaryKeys)

		if len(primaryKeys) == 0 {
			primaryKeys = append(primaryKeys, "id")
		}

		newContext := ctxt.Request().Context()

		identifiers := map[string]interface{}{}

		for _, p := range primaryKeys {
			identifiers[p] = newContext.Value(p)
		}

		sequence, _ := newContext.Value("sequence_no").(int)
		etag, _ := newContext.Value("If-None-Match").(string)
		entityID, _ := newContext.Value("use_entity_id").(string)

		var result map[string]interface{}
		var err error
		if sequence == 0 && etag == "" && entityID != "true" {
			result, err = app.Projections()[0].(*projections.GORMProjection).GetByKey(ctxt.Request().Context(), *cType, identifiers)
		} else if etag != "" {
			tag, seq := SplitEtag(etag)
			seqInt, er := strconv.Atoi(seq)
			if er != nil {
				return ctxt.JSON(http.StatusBadRequest, "Invalid sequence number")
			}
			r := &model.ContentEntity{}
			r, err = GetContentBySequenceNumber(app.EventRepository(), tag, int64(seqInt))
			if r.SequenceNo == 0 {
				return ctxt.JSON(http.StatusNotFound, "No entity found")
			}
			if err == nil && r.SequenceNo < int64(seqInt) {
				return ctxt.JSON(http.StatusNotModified, r.Property)
			}
		} else {
			//get first identifider
			id := ""
			for _, i := range identifiers {
				id = i.(string)
				if id != "" {
					break
				}
			}
			if sequence != 0 {
				r := &model.ContentEntity{}
				r, err = GetContentBySequenceNumber(app.EventRepository(), id, int64(sequence))
				if r != nil {
					result = r.Property.(map[string]interface{})
				}
			} else {
				result, err = app.Projections()[0].(*projections.GORMProjection).GetByEntityID(ctxt.Request().Context(), *cType, id)
			}
		}

		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ctxt.JSON(http.StatusNotFound, "No entity found")
		}

		return ctxt.JSON(http.StatusOK, result)
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
