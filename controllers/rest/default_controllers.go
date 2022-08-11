package rest

import (
	"encoding/json"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	context2 "github.com/wepala/weos/context"
	"github.com/wepala/weos/model"
	"net/http"
	"strconv"
	"strings"
)

//DefaultWriteController handles the write operations (create, update, delete)
func DefaultWriteController(api Container, commandDispatcher model.CommandDispatcher, entityRepository model.EntityRepository, operation map[string]*openapi3.Operation) echo.HandlerFunc {
	var commandName string
	var err error

	logger, err := api.GetLog("Default")
	if err != nil {
		log.Fatal("no logger defined")
	}

	for method, operation := range operation {
		//If there is a x-command extension then dispatch that command by default
		if rawCommand, ok := operation.Extensions["x-command"].(json.RawMessage); ok {
			err := json.Unmarshal(rawCommand, &commandName)
			if err != nil {
				logger.Fatalf("error unmarshalling command: %s", err)
			}
		}
		//If there is a x-command-name extension then dispatch that command by default otherwise use the default command based on the operation type
		if commandName == "" {
			switch method {
			case http.MethodPost:
				commandName = "create"
			case http.MethodPut:
				commandName = "update"
			case http.MethodDelete:
				commandName = "delete"
			}
		}
	}

	return func(ctxt echo.Context) error {
		var weosID string
		var sequenceNo string
		var seq int

		//getting etag from context
		etag := ctxt.Request().Header.Get("If-Match")
		if etag != "" {
			weosID, sequenceNo = SplitEtag(etag)
			seq, err = strconv.Atoi(sequenceNo)
			if err != nil {
				return NewControllerError("unexpected error updating content type.  invalid sequence number", err, http.StatusBadRequest)
			}
		}

		command := &model.Command{
			Type:    commandName,
			Payload: context2.GetPayload(ctxt.Request().Context()),
			Metadata: model.CommandMetadata{
				EntityID:   weosID,
				EntityType: entityRepository.Name(),
				SequenceNo: seq,
				Version:    1,
				UserID:     context2.GetUser(ctxt.Request().Context()),
				AccountID:  context2.GetAccount(ctxt.Request().Context()),
			},
		}
		_, err = commandDispatcher.Dispatch(ctxt.Request().Context(), command, api, entityRepository, ctxt.Logger())
		if err != nil {
			//TODO the type of error return should determine the status code
			ctxt.Logger().Debugf("error dispatching command: %s", err)
			return ctxt.JSON(http.StatusBadRequest, err)
		}

		//TODO the type of command and/or the api configuration should determine the status code
		switch commandName {
		case "create":
			return ctxt.JSON(http.StatusCreated, make(map[string]string))
		case "update":
			return ctxt.JSON(http.StatusOK, make(map[string]string))
		case "delete":
			return ctxt.JSON(http.StatusOK, make(map[string]string))
		default:
			return ctxt.NoContent(http.StatusOK)
		}
	}
}

//DefaultReadController handles the read operations viewing a specific item
func DefaultReadController(api Container, commandDispatcher model.CommandDispatcher, entityRepository model.EntityRepository, operationMap map[string]*openapi3.Operation) echo.HandlerFunc {
	return func(ctxt echo.Context) error {
		var entity *model.ContentEntity
		var err error
		//get identifier from context
		entity, err = entityRepository.CreateEntityWithValues(ctxt.Request().Context(), []byte("{}"))
		if err != nil {
			return NewControllerError("unexpected error creating entity", err, http.StatusBadRequest)
		}
		identifier, err := entity.Identifier()
		if err != nil {
			return NewControllerError("unexpected error getting identifier", err, http.StatusBadRequest)
		}
		entity, err = entityRepository.GetByKey(ctxt.Request().Context(), entityRepository, identifier)
		if err != nil {
			return NewControllerError("unexpected error getting entity", err, http.StatusBadRequest)
		}

		//check the accepts header

		return ctxt.JSON(http.StatusOK, entity)
	}
}

//DefaultListController handles the read operations viewing a list of items
func DefaultListController(api Container, commandDispatcher model.CommandDispatcher, entityRepository model.EntityRepository, operationMap map[string]*openapi3.Operation) echo.HandlerFunc {
	return func(ctxt echo.Context) error {
		var filterOptions map[string]interface{}
		newContext := ctxt.Request().Context()
		//gets the filter, limit and page from context
		limit, _ := newContext.Value("limit").(int)
		page, _ := newContext.Value("page").(int)
		filters := newContext.Value("_filters")

		schema := entityRepository.Schema()
		if filters != nil {
			filterOptions = map[string]interface{}{}
			filterOptions = filters.(map[string]interface{})
			for key, values := range filterOptions {
				if len(values.(*FilterProperties).Values) != 0 && values.(*FilterProperties).Operator != "in" {
					msg := "this operator " + values.(*FilterProperties).Operator + " does not support multiple values "
					return NewControllerError(msg, nil, http.StatusBadRequest)
				}
				// checking if the field is valid based on schema provided, split on "."
				parts := strings.Split(key, ".")
				if schema.Properties[parts[0]] == nil {
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
		var contentEntities []*model.ContentEntity
		// sort by default is by id
		sorts := map[string]string{"id": "asc"}

		contentEntities, count, err = entityRepository.GetList(newContext, entityRepository, page, limit, "", sorts, filterOptions)
		if err != nil {
			return NewControllerError(err.Error(), err, http.StatusBadRequest)
		}
		resp := ListApiResponse{
			Total: count,
			Page:  page,
			Items: contentEntities,
		}
		return ctxt.JSON(http.StatusOK, resp)
	}
}
