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
		//payload, _ := ioutil.ReadAll(ctxt.Request().Body)

		var payload []byte
		var err error

		ct := ctxt.Request().Header.Get("Content-Type")

		switch ct {
		case "application/json":
			payload, err = ioutil.ReadAll(ctxt.Request().Body)
			if err != nil {
				return err
			}
		case "application/x-www-form-urlencoded":
			payload, err = ConvertFormUrlEncodedToJson(ctxt.Request())
			if err != nil {
				return err
			}
		default:
			payload, err = ioutil.ReadAll(ctxt.Request().Body) //REMOVE THIS
			if err != nil {
				return err
			}
		}

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

		err = app.Dispatcher().Dispatch(newContext, model.Create(newContext, payload, contentType, weosID))
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
		var newPayload map[string]interface{}
		//reads the request body
		payload, _ := ioutil.ReadAll(ctxt.Request().Body)
		//getting etag from context
		etagInterface := newContext.Value("If-Match")
		if etagInterface != nil {
			if etag, ok := etagInterface.(string); ok {
				if etag != "" {
					weosID, sequenceNo = SplitEtag(etag)
					json.Unmarshal(payload, &newPayload)
					newPayload["weos_id"] = weosID
					newPayload["sequence_no"] = sequenceNo
					payload, _ = json.Marshal(newPayload)
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
		entityID, _ := newContext.Value("use_entity_id").(bool)

		var sequence int
		if sequenceString != "" {
			sequence, _ = strconv.Atoi(sequenceString)
		}
		var result map[string]interface{}
		var err error

		//get by keys
		if sequence == 0 && etag == "" && !entityID {
			for _, projection := range app.Projections() {
				if projection != nil {
					result, err = projection.GetByKey(ctxt.Request().Context(), *cType, identifiers)
					break
				}
			}
		} else {
			id := ""

			//if etag given, get entity id and sequence number
			if etag != "" {
				tag, seq := SplitEtag(etag)
				id = tag
				sequence, err = strconv.Atoi(seq)
				if err != nil {
					return NewControllerError("Invalid sequence number", err, http.StatusBadRequest)
				}
			}

			//get entity_id from list of identifiers
			if id == "" {
				for _, i := range identifiers {
					id = i.(string)
					if id != "" {
						break
					}
				}
			}
			//if sequence number given, get entity by sequence number
			if sequence != 0 {
				r, er := GetContentBySequenceNumber(app.EventRepository(), id, int64(sequence))
				if r != nil && r.SequenceNo != 0 {
					if r != nil && r.ID != "" {
						result = r.Property.(map[string]interface{})
					}
					if err == nil && r.SequenceNo < int64(sequence) && r.ID != "" {
						return ctxt.JSON(http.StatusNotModified, result)
					}
				}
				result["weos_id"] = r.ID
				result["sequence_no"] = r.SequenceNo
				err = er
			} else {
				//get entity by entity_id
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
