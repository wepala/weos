package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"github.com/segmentio/ksuid"
	context2 "github.com/wepala/weos-service/context"
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
		var weosID string

		json.Unmarshal(payload, &payMap)
		if v, ok := payMap["weos_id"]; ok {
			if val, ok := v.(string); ok {
				weosID = val
			}
		}
		if weosID == "" {
			weosID = ksuid.New().String()
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

		sequenceString, _ := newContext.Value("sequence_no").(string)
		etag, _ := newContext.Value("If-None-Match").(string)
		entityID, _ := newContext.Value("use_entity_id").(string)

		var sequence int
		if sequenceString != "" {
			sequence, _ = strconv.Atoi(sequenceString)
		}
		var result map[string]interface{}
		var err error
		if sequence == 0 && etag == "" && entityID != "true" {
			for _, projection := range app.Projections() {
				if projection != nil {
					result, err = projection.GetByKey(ctxt.Request().Context(), *cType, identifiers)
				}
			}
		} else if etag != "" {
			tag, seq := SplitEtag(etag)
			seqInt, er := strconv.Atoi(seq)
			if er != nil {
				return NewControllerError("Invalid sequence number", err, http.StatusBadRequest)
			}
			r, er := GetContentBySequenceNumber(app.EventRepository(), tag, int64(seqInt))
			err = er
			if r.SequenceNo == 0 {
				return NewControllerError("No entity found", err, http.StatusNotFound)
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
				r, er := GetContentBySequenceNumber(app.EventRepository(), id, int64(sequence))
				if r != nil && r.ID != "" {
					result = r.Property.(map[string]interface{})
				}
				err = er
			} else {
				for _, projection := range app.Projections() {
					if projection != nil {
						result, err = projection.GetByEntityID(ctxt.Request().Context(), *cType, id)
					}
				}
			}
		}
		weos_id, ok := result["weos_id"].(string)
		if errors.Is(err, gorm.ErrRecordNotFound) || (len(result) == 0) || !ok || weos_id == "" {
			return NewControllerError("No entity found", err, http.StatusNotFound)
		} else if err != nil {
			return NewControllerError(err.Error(), err, http.StatusBadRequest)
		}

		sequenceString = fmt.Sprint(result["sequence_no"])
		sequenceNo, _ := strconv.Atoi(sequenceString)

		etag = NewEtag(&model.ContentEntity{
			AggregateRoot: model.AggregateRoot{
				SequenceNo:  int64(sequenceNo),
				BasicEntity: model.BasicEntity{ID: weos_id},
			},
		})

		//remove sequence number and weos_id from response
		delete(result, "weos_id")
		delete(result, "sequence_no")

		//set etag
		ctxt.Response().Header().Set("Etag", etag)
		return ctxt.JSON(http.StatusOK, result)
	}
}

func (c *StandardControllers) List(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.HandlerFunc {
	return func(ctxt echo.Context) error {

		return ctxt.JSON(http.StatusOK, "List Items")
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
