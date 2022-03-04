package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/coreos/go-oidc/v3/oidc"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wepala/weos/projections"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/segmentio/ksuid"
	weoscontext "github.com/wepala/weos/context"
	"github.com/wepala/weos/model"
	_ "github.com/wepala/weos/swaggerui"
	"golang.org/x/net/context"
)

//CreateMiddleware is used for a single payload. It dispatches this to the model which then validates and creates it.
func CreateMiddleware(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctxt echo.Context) error {
			//look up the schema for the content type so that we could identify the rules
			newContext := ctxt.Request().Context()
			if entityFactory != nil {
				newContext = context.WithValue(newContext, weoscontext.ENTITY_FACTORY, entityFactory)
			} else {
				err := errors.New("entity factory must be set")
				api.EchoInstance().Logger.Errorf("no entity factory detected for '%s'", ctxt.Request().RequestURI)
				return err
			}
			payload := weoscontext.GetPayload(newContext)
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

			err := commandDispatcher.Dispatch(newContext, model.Create(newContext, payload, entityFactory.Name(), weosID), eventSource, projection, api.EchoInstance().Logger)
			if err != nil {
				if errr, ok := err.(*model.DomainError); ok {
					return NewControllerError(errr.Error(), err, http.StatusBadRequest)
				} else {
					return NewControllerError("unexpected error creating content type", err, http.StatusBadRequest)
				}
			}
			//add id to context for use by controller
			newContext = context.WithValue(newContext, weoscontext.ENTITY_ID, weosID)
			request := ctxt.Request().WithContext(newContext)
			ctxt.SetRequest(request)
			return next(ctxt)
		}
	}
}

//CreateController is used for a single payload. It dispatches this to the model which then validates and creates it.
func CreateController(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory) echo.HandlerFunc {
	return func(ctxt echo.Context) error {
		if weosID := weoscontext.GetEntityID(ctxt.Request().Context()); weosID != "" {
			var result *model.ContentEntity
			var Etag string
			var err error
			if projection != nil {
				result, err = projection.GetContentEntity(ctxt.Request().Context(), entityFactory, weosID)
				if err != nil {
					return err
				}
				Etag = NewEtag(result)
			}
			if result == nil {
				return NewControllerError("No entity found", err, http.StatusNotFound)
			}
			ctxt.Response().Header().Set("Etag", Etag)
			return ctxt.JSON(http.StatusCreated, result.ToMap())
		}
		return ctxt.String(http.StatusCreated, "OK")
	}
}

//CreateBatchMiddleware is used for an array of payloads. It dispatches this to the model which then validates and creates it.
func CreateBatchMiddleware(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctxt echo.Context) error {
			//look up the schema for the content type so that we could identify the rules
			newContext := ctxt.Request().Context()
			if entityFactory != nil {
				newContext = context.WithValue(newContext, weoscontext.ENTITY_FACTORY, entityFactory)
			} else {
				err := errors.New("entity factory must be set")
				api.EchoInstance().Logger.Errorf("no entity factory detected for '%s'", ctxt.Request().RequestURI)
				return err
			}
			payload := weoscontext.GetPayload(newContext)

			err := commandDispatcher.Dispatch(newContext, model.CreateBatch(newContext, payload, entityFactory.Name()), eventSource, projection, api.EchoInstance().Logger)
			if err != nil {
				if errr, ok := err.(*model.DomainError); ok {
					return NewControllerError(errr.Error(), err, http.StatusBadRequest)
				} else {
					return NewControllerError("unexpected error updating content type batch", err, http.StatusBadRequest)
				}
			}
			return next(ctxt)
		}
	}
}

//CreateBatchController is used for an array of payloads. It dispatches this to the model which then validates and creates it.
func CreateBatchController(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory) echo.HandlerFunc {
	return func(ctxt echo.Context) error {

		return ctxt.JSON(http.StatusCreated, "CreatedBatch")
	}
}

func UpdateMiddleware(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctxt echo.Context) error {
			//look up the schema for the content type so that we could identify the rules
			newContext := ctxt.Request().Context()
			if entityFactory != nil {
				newContext = context.WithValue(newContext, weoscontext.ENTITY_FACTORY, entityFactory)
			} else {
				err := errors.New("entity factory must be set")
				api.EchoInstance().Logger.Errorf("no entity factory detected for '%s'", ctxt.Request().RequestURI)
				return err
			}
			var weosID string
			var sequenceNo string

			var err error

			payload := weoscontext.GetPayload(newContext)
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
						newContext = context.WithValue(newContext, weoscontext.WEOS_ID, weosID)
						newContext = context.WithValue(newContext, weoscontext.SEQUENCE_NO, seq)
					}
				}
			}

			err = commandDispatcher.Dispatch(newContext, model.Update(newContext, payload, entityFactory.Name()), eventSource, projection, api.EchoInstance().Logger)
			if err != nil {
				api.e.Logger.Errorf("error persisting entity '%s'", err)
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
			//add id to context for use by controller
			newContext = context.WithValue(newContext, weoscontext.ENTITY_ID, weosID)
			request := ctxt.Request().WithContext(newContext)
			ctxt.SetRequest(request)
			return next(ctxt)
		}

	}
}

func UpdateController(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory) echo.HandlerFunc {
	return func(ctxt echo.Context) error {
		var err error
		var Etag string
		var identifiers []string
		var result *model.ContentEntity
		var result1 map[string]interface{}
		newContext := ctxt.Request().Context()
		weosID := newContext.Value(weoscontext.ENTITY_ID)
		if weosID == nil || weosID == "" {
			//find entity based on identifiers specified
			pks, _ := json.Marshal(entityFactory.Schema().Extensions[IdentifierExtension])
			json.Unmarshal(pks, &identifiers)

			if len(identifiers) == 0 {
				identifiers = append(identifiers, "id")
			}

			primaryKeys := map[string]interface{}{}
			for _, p := range identifiers {

				ctxtIdentifier := newContext.Value(p)

				primaryKeys[p] = ctxtIdentifier

			}

			result1, err = projection.GetByKey(newContext, entityFactory, primaryKeys)
			if err != nil {
				return err
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
			if projection != nil {

				result, err = projection.GetContentEntity(newContext, entityFactory, weosID.(string))
				if err != nil {
					return err
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

func BulkUpdate(app model.Service, spec *openapi3.Swagger, path *openapi3.PathItem, operation *openapi3.Operation) echo.HandlerFunc {
	return func(ctxt echo.Context) error {
		return nil
	}
}

func APIDiscovery(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory) echo.HandlerFunc {
	return func(ctxt echo.Context) error {
		newContext := ctxt.Request().Context()

		//get content type expected for 200 response
		responseType := newContext.Value(weoscontext.RESPONSE_PREFIX + strconv.Itoa(http.StatusOK))
		if responseType == "application/json" {
			return ctxt.JSON(http.StatusOK, api.Swagger)
		} else if responseType == "application/html" {
			return ctxt.Redirect(http.StatusPermanentRedirect, SWAGGERUIENDPOINT)
		}

		return NewControllerError("No response format chosen for a valid response", nil, http.StatusBadRequest)
	}
}

func ViewMiddleware(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctxt echo.Context) error {
			if entityFactory == nil {
				err := errors.New("entity factory must be set")
				api.EchoInstance().Logger.Errorf("no entity factory detected for '%s'", ctxt.Request().RequestURI)
				return err
			}
			pks, _ := json.Marshal(entityFactory.Schema().Extensions[IdentifierExtension])
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

			var result map[string]interface{}
			var err error
			var entityID string
			var seq string
			var ok bool
			var seqInt int

			etag, _ := newContext.Value("If-None-Match").(string)
			useEntity, _ := newContext.Value("use_entity_id").(bool)
			seqInt, ok = newContext.Value("sequence_no").(int)
			if !ok {
				if seq, ok = newContext.Value("sequence_no").(string); ok {
					ctxt.Logger().Debugf("invalid sequence no")
				}
			} else {
				//if we sucessfully pulled a sequence number and it is zero, the entity does not exist
				if seqInt == 0 {
					return NewControllerError("Entity does not exist", nil, http.StatusNotFound)
				}
			}

			//if use_entity_id is not set then let's get the item by key
			if !useEntity {
				if projection != nil {
					result, err = projection.GetByKey(ctxt.Request().Context(), entityFactory, identifiers)
				}
			}
			//if etag is set then let's use that info
			if etag != "" {
				entityID, seq = SplitEtag(etag)
				seqInt, err = strconv.Atoi(seq)
			}

			//if a sequence no. was sent BUT it could not be converted to an integer then return an error
			if seq != "" && seqInt == 0 {
				return NewControllerError("Invalid sequence number", err, http.StatusBadRequest)
			}
			//if sequence no. was sent in the request but we don't have the entity let's get it from projection
			if entityID == "" && seqInt != 0 {
				entityID, ok = result["weos_id"].(string)
				if !ok {
					ctxt.Logger().Debugf("the item '%v' does not have an entity id stored", identifiers)
				}
			}

			if useEntity && entityID == "" {
				//get first identifier for the entity id
				for _, i := range identifiers {
					if entityID, ok = i.(string); ok && entityID != "" {
						break
					}
				}
			}

			//use the entity id and sequence no. to get the entity if they were passed in
			if entityID != "" {
				//get the entity using the sequence no.
				if seqInt != 0 {
					//get the events up to the sequence
					events, err := eventSource.GetByAggregateAndSequenceRange(entityID, 0, int64(seqInt))
					//create content entity
					r, er := entityFactory.NewEntity(ctxt.Request().Context())
					if er != nil {
						return NewControllerError("unable to create entity", er, http.StatusInternalServerError)
					}
					er = r.ApplyEvents(events)
					if er != nil {
						return NewControllerError("unable to changes", er, http.StatusInternalServerError)
					}
					if r.SequenceNo == 0 {
						return NewControllerError("No entity found", err, http.StatusNotFound)
					}
					if r != nil && r.ID != "" { //get the map from the entity
						result = r.ToMap()
					}
					result["weos_id"] = r.ID
					result["sequence_no"] = r.SequenceNo
					err = er
					if err == nil && r.SequenceNo < int64(seqInt) && etag != "" { //if the etag is set then let's return the header
						return ctxt.JSON(http.StatusNotModified, r.Property)
					}
				} else {
					//get entity by entity_id

					if projection != nil {
						result, err = projection.GetByEntityID(ctxt.Request().Context(), entityFactory, entityID)
					}

				}
			}

			//add result to context
			newContext = context.WithValue(newContext, weoscontext.ENTITY, result) //TODO store the entity instead (this requires the different projection calls to return entities)
			request := ctxt.Request().WithContext(newContext)
			ctxt.SetRequest(request)
			return next(ctxt)
		}
	}

}

func ViewController(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory) echo.HandlerFunc {
	return func(ctxt echo.Context) error {
		newContext := ctxt.Request().Context()

		var err error
		var weosID string
		var ok bool

		if err = weoscontext.GetError(newContext); err != nil {
			return NewControllerError("Error occurred", err, http.StatusBadRequest)
		}
		if entityFactory == nil {
			err = errors.New("entity factory must be set")
			api.EchoInstance().Logger.Errorf("no entity factory detected for '%s'", ctxt.Request().RequestURI)
			return err
		}

		entity := model.GetEntity(newContext)
		if entity == nil {
			return NewControllerError("No entity found", err, http.StatusNotFound)
		}
		if weosID, ok = entity["weos_id"].(string); !ok {
			return NewControllerError("No entity found", err, http.StatusNotFound)
		}
		sequenceString := fmt.Sprint(entity["sequence_no"])
		sequenceNo, _ := strconv.Atoi(sequenceString)

		etag := NewEtag(&model.ContentEntity{
			AggregateRoot: model.AggregateRoot{
				SequenceNo:  int64(sequenceNo),
				BasicEntity: model.BasicEntity{ID: weosID},
			},
		})

		//remove sequence number and weos_id from response
		delete(entity, "weos_id")
		delete(entity, "sequence_no")
		delete(entity, "table_alias")

		//set etag
		ctxt.Response().Header().Set("Etag", etag)
		return ctxt.JSON(http.StatusOK, entity)
	}
}

func ListMiddleware(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctxt echo.Context) error {
			var filterOptions map[string]interface{}
			newContext := ctxt.Request().Context()
			if entityFactory == nil {
				err := errors.New("entity factory must be set")
				api.EchoInstance().Logger.Errorf("no entity factory detected for '%s'", ctxt.Request().RequestURI)
				return NewControllerError(err.Error(), nil, http.StatusBadRequest)
			}
			//gets the filter, limit and page from context
			limit, _ := newContext.Value("limit").(int)
			page, _ := newContext.Value("page").(int)
			filters := newContext.Value("_filters")
			schema := entityFactory.Schema()
			if filters != nil {
				filterOptions = map[string]interface{}{}
				filterOptions = filters.(map[string]interface{})
				for key, values := range filterOptions {
					if len(values.(*FilterProperties).Values) != 0 && values.(*FilterProperties).Operator != "in" {
						msg := "this operator " + values.(*FilterProperties).Operator + " does not support multiple values "
						return NewControllerError(msg, nil, http.StatusBadRequest)
					}
					// checking if the field is valid based on schema provided
					if schema.Properties[key] == nil {
						if key == "id" && schema.ExtensionProps.Extensions[IdentifierExtension] == nil {
							continue
						}
						msg := "invalid property found in filter: " + key
						return NewControllerError(msg, nil, http.StatusBadRequest)
					}

				}
			}
			if page == 0 {
				page = 1
			}
			var count int64
			var err error
			var contentEntities []map[string]interface{}
			// sort by default is by id
			sorts := map[string]string{"id": "asc"}

			if projection != nil {
				contentEntities, count, err = projection.GetContentEntities(newContext, entityFactory, page, limit, "", sorts, filterOptions)
			}

			if err != nil {
				return NewControllerError(err.Error(), err, http.StatusBadRequest)
			}
			resp := ListApiResponse{
				Total: count,
				Page:  page,
				Items: contentEntities,
			}
			//Add response to context for controller
			newContext = context.WithValue(newContext, weoscontext.ENTITY_COLLECTION, resp)
			request := ctxt.Request().WithContext(newContext)
			ctxt.SetRequest(request)
			return next(ctxt)
		}
	}
}

func ListController(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory) echo.HandlerFunc {

	return func(ctxt echo.Context) error {
		newContext := ctxt.Request().Context()
		resp := newContext.Value(weoscontext.ENTITY_COLLECTION)
		if resp == nil {
			return NewControllerError("unexpected error creating content type", nil, http.StatusBadRequest)
		}
		return ctxt.JSON(http.StatusOK, resp.(ListApiResponse))
	}
}

//DeleteMiddleware delete entity
func DeleteMiddleware(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctxt echo.Context) error {
			//look up the schema for the content type so that we could identify the rules
			newContext := ctxt.Request().Context()
			var weosID string
			var sequenceNo string

			if entityFactory != nil {
				newContext = context.WithValue(newContext, weoscontext.ENTITY_FACTORY, entityFactory)
			} else {
				err := errors.New("entity factory must be set")
				api.EchoInstance().Logger.Errorf("no entity factory detected for '%s'", ctxt.Request().RequestURI)
				return err
			}
			//getting etag from context
			etagInterface := newContext.Value("If-Match")
			if etagInterface != nil {
				if etag, ok := etagInterface.(string); ok {
					if etag != "" {
						weosID, sequenceNo = SplitEtag(etag)
						seq, err := strconv.Atoi(sequenceNo)
						if err != nil {
							return NewControllerError("unexpected error deleting content type.  invalid sequence number", err, http.StatusBadRequest)
						}
						newContext = context.WithValue(newContext, weoscontext.WEOS_ID, weosID)
						newContext = context.WithValue(newContext, weoscontext.SEQUENCE_NO, seq)
					}
				}
			}

			var err error
			var identifiers []string
			var result1 map[string]interface{}
			var ok bool

			//Uses the identifiers to pull the weosID, to be later used to get Seq NO
			if etagInterface == nil {
				//find entity based on identifiers specified
				pks, _ := json.Marshal(entityFactory.Schema().Extensions[IdentifierExtension])
				json.Unmarshal(pks, &identifiers)

				if len(identifiers) == 0 {
					identifiers = append(identifiers, "id")
				}

				primaryKeys := map[string]interface{}{}
				for _, p := range identifiers {

					ctxtIdentifier := newContext.Value(p)

					primaryKeys[p] = ctxtIdentifier

				}

				if projection != nil {
					result1, err = projection.GetByKey(newContext, entityFactory, primaryKeys)
					if err != nil {
						return err
					}

				}
				weosID, ok = result1["weos_id"].(string)

				if (len(result1) == 0) || !ok || weosID == "" {
					return NewControllerError("No entity found", err, http.StatusNotFound)
				} else if err != nil {
					return NewControllerError(err.Error(), err, http.StatusBadRequest)
				}
			}

			//Dispatch the actual delete to projecitons
			err = commandDispatcher.Dispatch(newContext, model.Delete(newContext, entityFactory.Name(), weosID), eventSource, projection, api.EchoInstance().Logger)
			if err != nil {
				if errr, ok := err.(*model.DomainError); ok {
					if strings.Contains(errr.Error(), "error deleting entity. This is a stale item") {
						return NewControllerError(errr.Error(), err, http.StatusPreconditionFailed)
					}
					if strings.Contains(errr.Error(), "invalid:") {
						return NewControllerError(errr.Error(), err, http.StatusUnprocessableEntity)
					}
					return NewControllerError(errr.Error(), err, http.StatusBadRequest)
				} else {
					return NewControllerError("unexpected error deleting content type", err, http.StatusBadRequest)
				}
			}
			//Add response to context for controller
			newContext = context.WithValue(newContext, weoscontext.WEOS_ID, weosID)
			request := ctxt.Request().WithContext(newContext)
			ctxt.SetRequest(request)
			return next(ctxt)
		}
	}
}

//DeleteController handle delete
func DeleteController(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory) echo.HandlerFunc {
	return func(ctxt echo.Context) error {
		newContext := ctxt.Request().Context()
		if weosIDRaw := newContext.Value(weoscontext.WEOS_ID); weosIDRaw != nil {
			if weosID, ok := weosIDRaw.(string); ok {
				deleteEventSeq, err := eventSource.GetAggregateSequenceNumber(weosID)
				if err != nil {
					return NewControllerError("No delete event found", err, http.StatusNotFound)
				}

				etag := NewEtag(&model.ContentEntity{
					AggregateRoot: model.AggregateRoot{
						SequenceNo:  deleteEventSeq,
						BasicEntity: model.BasicEntity{ID: weosID},
					},
				})

				ctxt.Response().Header().Set("Etag", etag)

				return ctxt.JSON(http.StatusOK, "Deleted")
			}
		}

		return ctxt.String(http.StatusBadRequest, "Item not deleted")
	}
}

//DefaultResponseMiddleware returns content type based on content type in example
func DefaultResponseMiddleware(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctxt echo.Context) error {
			ctx := ctxt.Request().Context()
			//take media type from the request since the context wouldnt add it because there is no entity factory to use
			mediaType := ctxt.Request().Header.Get(weoscontext.ACCEPT)
			var bytesArray []byte
			var err error
			contentType := ""
			var respCode int
			found := false
			if mediaType != "" && strings.Replace(mediaType, "*", "", -1) != "/" && mediaType != "/" {
				for code, resp := range operation.Responses {
					respCode, _ = strconv.Atoi(code)
					if resp.Value.Content[mediaType] == nil {
						//check for wild card
						if strings.Contains(mediaType, "*") {
							mediaT := strings.Replace(mediaType, "*", "", -1)
							for key, content := range resp.Value.Content {
								if strings.Contains(key, mediaT) {
									if content.Example != nil {
										bytesArray, err = JSONMarshal(content.Example)
										if err != nil {
											api.e.Logger.Debugf("unexpected error %s ", err)
											return NewControllerError(fmt.Sprintf("unexpected error %s ", err), err, http.StatusBadRequest)
										}
										contentType = key + "; " + "charset=UTF-8"
										found = true
										break
									}
								}

							}
						}
						if found {
							break
						}
					} else {
						if resp.Value.Content[mediaType].Example != nil {
							bytesArray, err = json.Marshal(resp.Value.Content[mediaType].Example)
							if err != nil {
								api.e.Logger.Debugf("unexpected error %s ", err)
								return NewControllerError(fmt.Sprintf("unexpected error %s ", err), err, http.StatusBadRequest)
							}
							contentType = mediaType + "; " + "charset=UTF-8"
							found = true
							break
						}
					}
				}
			}
			if !found { //if using the accept header nothing is found, use the first content type
				for code, resp := range operation.Responses {
					respCode, _ = strconv.Atoi(code)
					for key, content := range resp.Value.Content {
						if content.Example != nil {
							bytesArray, err = JSONMarshal(content.Example)
							if err != nil {
								api.e.Logger.Debugf("unexpected error %s ", err)
								return NewControllerError(fmt.Sprintf("unexpected error %s ", err), err, http.StatusBadRequest)

							}
							contentType = key + "; " + "charset=UTF-8"
							found = true
							break
						}
					}
					if found {
						break
					}
				}
			}

			//Add response to context for controller
			ctx = context.WithValue(ctx, "resp", ctxt.Blob(respCode, contentType, bytesArray))
			request := ctxt.Request().WithContext(ctx)
			ctxt.SetRequest(request)
			return next(ctxt)

		}
	}
}

func DefaultResponseController(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory) echo.HandlerFunc {
	return func(context echo.Context) error {
		newContext := context.Request().Context()
		value := newContext.Value("resp")
		if value == nil {
			return NewControllerError("unexpected error all responses were parsed, nothing was found", nil, http.StatusBadRequest)
		}
		return value.(error)
	}
}

func Get(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory) echo.HandlerFunc {
	return func(ctxt echo.Context) error {
		//TODO call GetByID

		return ctxt.JSON(200, nil)
	}
}
func HealthCheck(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory) echo.HandlerFunc {
	return func(context echo.Context) error {
		response := &HealthCheckResponse{
			Version: api.Swagger.Info.Version,
		}
		return context.JSON(http.StatusOK, response)
	}

}

//OpenIDMiddleware handling JWT in incoming Authorization header
func OpenIDMiddleware(api *RESTAPI, projection projections.Projection, commandDispatcher model.CommandDispatcher, eventSource model.EventRepository, entityFactory model.EntityFactory, path *openapi3.PathItem, operation *openapi3.Operation) echo.MiddlewareFunc {
	var openIdConnectUrl string
	securityCheck := true
	var verifiers []*oidc.IDTokenVerifier
	algs := []string{"RS256", "RS384", "RS512", "HS256"}
	if operation.Security != nil && len(*operation.Security) == 0 {
		securityCheck = false
	}
	for _, schemes := range api.Swagger.Components.SecuritySchemes {
		//checks if the security scheme type is openIdConnect
		if schemes.Value.Type == "openIdConnect" {
			//get the open id connect url
			if openIdUrl, ok := schemes.Value.ExtensionProps.Extensions[OPENIDCONNECTURLEXTENSION]; ok {
				err := json.Unmarshal(openIdUrl.(json.RawMessage), &openIdConnectUrl)
				if err != nil {
					api.EchoInstance().Logger.Errorf("unable to unmarshal open id connect url '%s'", err)
				} else {
					//get the Jwk url from open id connect url and validate url
					jwksUrl, err := GetJwkUrl(openIdConnectUrl)
					if err != nil {
						api.EchoInstance().Logger.Warnf("invalid open id connect url: %s", err)
					} else {
						//by default skipExpiryCheck is false meaning it will not run an expiry check
						skipExpiryCheck := false
						//get skipexpirycheck that is an extension in the openapi spec
						if expireCheck, ok := schemes.Value.ExtensionProps.Extensions[SKIPEXPIRYCHECKEXTENSION]; ok {
							err := json.Unmarshal(expireCheck.(json.RawMessage), &skipExpiryCheck)
							if err != nil {
								api.EchoInstance().Logger.Errorf("unable to unmarshal skip expiry '%s'", err)
							}
						}
						//create key set and verifier
						keySet := oidc.NewRemoteKeySet(context.Background(), jwksUrl)
						tokenVerifier := oidc.NewVerifier(openIdConnectUrl, keySet, &oidc.Config{
							ClientID:             "",
							SupportedSigningAlgs: algs,
							SkipClientIDCheck:    true,
							SkipExpiryCheck:      skipExpiryCheck,
							SkipIssuerCheck:      true,
							Now:                  time.Now,
						})
						verifiers = append(verifiers, tokenVerifier)
					}

				}
			}

		}
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctxt echo.Context) error {
			var err error
			var token string
			if !securityCheck {
				return next(ctxt)
			}
			if len(verifiers) == 0 {
				api.e.Logger.Debugf("unexpected error no verifiers were set")
				return NewControllerError("unexpected error no verifiers were set", nil, http.StatusBadRequest)
			}
			newContext := ctxt.Request().Context()
			//get the token from request header since this runs before the context middleware
			if ctxt.Request().Header[weoscontext.AUTHORIZATION] != nil {
				token = ctxt.Request().Header[weoscontext.AUTHORIZATION][0]
			}
			if token == "" {
				api.e.Logger.Debugf("no JWT token was found")
				return NewControllerError("no JWT token was found", nil, http.StatusUnauthorized)
			}
			jwtToken := strings.Replace(token, "Bearer ", "", -1)
			var idToken *oidc.IDToken
			for _, tokenVerifier := range verifiers {
				idToken, err = tokenVerifier.Verify(newContext, jwtToken)
				if err != nil || idToken == nil {
					api.e.Logger.Debugf(err.Error())
					return NewControllerError("unexpected error verifying token", err, http.StatusUnauthorized)
				}
			}

			newContext = context.WithValue(newContext, weoscontext.USER_ID, idToken.Subject)
			request := ctxt.Request().WithContext(newContext)
			ctxt.SetRequest(request)
			return next(ctxt)

		}
	}
}
