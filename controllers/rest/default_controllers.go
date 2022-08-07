package rest

import (
	"encoding/json"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	context2 "github.com/wepala/weos/context"
	"github.com/wepala/weos/model"
	"net/http"
	"strconv"
)

//DefaultWriteController handles the write operations (create, update, delete)
func DefaultWriteController(api Container, commandDispatcher model.CommandDispatcher, entityRepository model.EntityRepository, operation *openapi3.Operation) echo.HandlerFunc {
	return func(ctxt echo.Context) error {
		var commandName string
		var err error
		var weosID string
		var sequenceNo string
		var seq int
		//If there is a x-command extension then dispatch that command by default
		if rawCommand, ok := operation.Extensions["x-command"].(json.RawMessage); ok {
			err := json.Unmarshal(rawCommand, &commandName)
			if err != nil {
				ctxt.Logger().Errorf("error unmarshalling command: %s", err)
				return err
			}
		}
		//If there is a x-command-name extension then dispatch that command by default otherwise use the default command based on the operation type
		if commandName == "" {
			switch ctxt.Request().Method {
			case http.MethodPost:
				commandName = "create"
			case http.MethodPut:
				commandName = "update"
			case http.MethodDelete:
				commandName = "delete"
			}
		}
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
		eventRepository, err := api.GetEventStore("Default")
		if err != nil {
			ctxt.Logger().Errorf("error getting event repository: %s", err)
		}
		err = commandDispatcher.Dispatch(ctxt.Request().Context(), command, api, eventRepository, entityRepository, ctxt.Logger())
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

func DefaultReadController(api Container, commandDispatcher model.CommandDispatcher, entityRepository model.EntityRepository, operation *openapi3.Operation) echo.HandlerFunc {
	return func(ctxt echo.Context) error {
		return nil
	}
}
