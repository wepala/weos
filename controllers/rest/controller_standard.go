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
	context2 "github.com/wepala/weos/context"
	"golang.org/x/net/context"
	"gorm.io/gorm"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/wepala/weos/model"
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
		var result *model.ContentEntity
		var Etag string
		for _, projection := range app.Projections() {
			if projection != nil {
				result, err = projection.GetContentEntity(newContext, weosID)
				if err != nil {
					return err
				}
				Etag = NewEtag(result)
			}
		}
		if result == nil || result.ID == "" {
			return NewControllerError("No entity found", err, http.StatusNotFound)
		}
		entity := map[string]interface{}{}
		result.ID = ""
		result.SequenceNo = 0
		bytes, err := json.Marshal(result.Property)
		if err != nil {
			return err
		}
		err = json.Unmarshal(bytes, &entity)
		if err != nil {
			return err
		}
		ctxt.Response().Header().Set("Etag", Etag)
		return ctxt.JSON(http.StatusCreated, entity)
	}
}

//CreateBatch is used for an array of payloads. It dispatches this to the model which then validates and creates it.
func (c *StandardControllers) CreateBatch(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.HandlerFunc {
	var contentType string
	var contentTypeSchema *openapi3.SchemaRef
	//get the entity information based on the Content Type associated with this operation
	for _, requestContent := range operation.RequestBody.Value.Content {
		//use the first schema ref to determine the entity type
		if requestContent.Schema.Value.Items != nil && strings.Contains(requestContent.Schema.Value.Items.Ref, "#/components/schemas/") {
			contentType = strings.Replace(requestContent.Schema.Value.Items.Ref, "#/components/schemas/", "", -1)
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
		var sequenceNo string
		//reads the request body
		payload, _ := ioutil.ReadAll(ctxt.Request().Body)
		//getting etag from context
		etagInterface := newContext.Value("If-Match")
		if etagInterface != nil {
			if etag, ok := etagInterface.(string); ok {
				if etag != "" {
					weosID, sequenceNo = SplitEtag(etag)
					seq, err := strconv.Atoi(sequenceNo)
					if err != nil {
						return NewControllerError("unexpected error updating content type.  invalid sequence number", err, http.StatusBadRequest)
					}
					newContext = context.WithValue(newContext, context2.WEOS_ID, weosID)
					newContext = context.WithValue(newContext, context2.SEQUENCE_NO, seq)
				}
			}
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

				ctxtIdentifier := newContext.Value(p)

				primaryKeys[p] = ctxtIdentifier

			}

			for _, projection := range app.Projections() {
				if projection != nil {
					result1, err = projection.GetByKey(newContext, *cType, primaryKeys)
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
			delete(result1, "sequence_no")
			delete(result1, "weos_id")
			delete(result1, "table_alias")

			ctxt.Response().Header().Set("Etag", Etag)

			return ctxt.JSON(http.StatusOK, result1)
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
			entity := map[string]interface{}{}
			result.ID = ""
			result.SequenceNo = 0
			bytes, err := json.Marshal(result.Property)
			if err != nil {
				return err
			}
			err = json.Unmarshal(bytes, &entity)
			if err != nil {
				return err
			}

			delete(entity, "sequence_no")
			delete(entity, "weos_id")
			delete(entity, "table_alias")

			ctxt.Response().Header().Set("Etag", Etag)
			return ctxt.JSON(http.StatusOK, entity)
		}

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
		useEntity, _ := newContext.Value("use_entity_id").(string)

		var seqInt int
		if sequenceString != "" {
			seqInt, _ = strconv.Atoi(sequenceString)
		}
		var result map[string]interface{}
		var err error
		var entityID string
		var seq string
		var ok bool
		//if use_entity_id is not set then let's get the item by key
		if useEntity != "true" {
			for _, projection := range app.Projections() {
				if projection != nil {
					result, err = projection.GetByKey(ctxt.Request().Context(), *cType, identifiers)
				}
			}
		}
		//if etag is set then let's use that info
		if etag != "" {
			entityID, seq = SplitEtag(etag)
			seqInt, err = strconv.Atoi(seq)
			if err != nil {
				return NewControllerError("Invalid sequence number", err, http.StatusBadRequest)
			}
		}
		//if sequence no. was sent in the request but we don't have the entity let's get it from projection
		if entityID == "" && sequenceString != "" {
			entityID, ok = result["weos_id"].(string)
			if !ok {
				ctxt.Logger().Debugf("the item '%v' does not have an entity id stored", identifiers)
			}
		}

		if useEntity == "true" && entityID == "" {
			//get first identifier for the entity id
			for _, i := range identifiers {
				entityID = i.(string)
				if entityID != "" {
					break
				}
			}
		}

		//use the entity id and sequence no. to get the entity if they were passed in
		if entityID != "" {
			//get the entity using the sequence no.
			if seqInt != 0 {
				r, er := GetContentBySequenceNumber(app.EventRepository(), entityID, int64(seqInt))
				err = er
				if r.SequenceNo == 0 {
					return NewControllerError("No entity found", err, http.StatusNotFound)
				}
				if r != nil && r.ID != "" {
					result = r.Property.(map[string]interface{})
				}
				result["weos_id"] = r.ID
				result["sequence_no"] = r.SequenceNo
				err = er
				if err == nil && r.SequenceNo < int64(seqInt) && etag != "" { //if the etag is set then let's return the header
					return ctxt.JSON(http.StatusNotModified, r.Property)
				}
			} else {
				//get entity by entity_id
				for _, projection := range app.Projections() {
					if projection != nil {
						result, err = projection.GetByEntityID(ctxt.Request().Context(), *cType, entityID)
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
		delete(result, "table_alias")

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
