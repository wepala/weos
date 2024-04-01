package rest

import (
	"encoding/json"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"io"
	"net/http"
	"strconv"
)

type ControllerParams struct {
	Logger             Log
	CommandDispatcher  CommandDispatcher
	ResourceRepository *ResourceRepository
	DefaultProjection  *Projection
	Projections        map[string]Projection
	Schema             *openapi3.T
	PathMap            map[string]*openapi3.PathItem
	Operation          map[string]*openapi3.Operation
	Echo               *echo.Echo
	APIConfig          *APIConfig
}

// DefaultWriteController handles the write operations (create, update, delete)
func DefaultWriteController(p *ControllerParams) echo.HandlerFunc {

	var err error
	var commandName string
	var resourceType string
	for method, toperation := range p.Operation {
		if toperation.RequestBody == nil || toperation.RequestBody.Value == nil {
			continue
		}
		//get the schema for the operation
		for _, requestContent := range toperation.RequestBody.Value.Content {
			if requestContent.Schema != nil {
				//use the first schema ref to determine the entity type
				if requestContent.Schema.Ref != "" {
					//get the entity type from the ref
					resourceType = requestContent.Schema.Ref
				}
			}
		}
		//If there is a x-command extension then dispatch that command by default
		if rawCommand, ok := toperation.Extensions["x-command"].(json.RawMessage); ok {
			err := json.Unmarshal(rawCommand, &commandName)
			if err != nil {
				p.Logger.Fatalf("error unmarshalling command: %s", err)
			}
		}
		//If there is a x-command-name extension then dispatch that command by default otherwise use the default command based on the operation type
		if commandName == "" {
			switch method {
			case http.MethodPost:
				commandName = UPDATE_COMMAND
			case http.MethodPut:
				commandName = UPDATE_COMMAND
			case http.MethodPatch:
				commandName = UPDATE_COMMAND
			case http.MethodDelete:
				commandName = DELETE_COMMAND
			}
		}
	}

	return func(ctxt echo.Context) error {
		var sequenceNo string
		var seq int

		//getting etag from context
		etag := ctxt.Request().Header.Get("If-Match")
		if etag != "" {
			_, sequenceNo = SplitEtag(etag)
			seq, err = strconv.Atoi(sequenceNo)
			if err != nil {
				return NewControllerError("unexpected error updating content type.  invalid sequence number", err, http.StatusBadRequest)
			}
		}

		body, err := io.ReadAll(ctxt.Request().Body)
		if err != nil {
			ctxt.Logger().Debugf("unexpected error reading request body: %s", err)
			return NewControllerError("unexpected error reading request body", err, http.StatusBadRequest)
		}

		contentType := ctxt.Request().Header.Get(echo.HeaderContentType)
		//for certain content types treat with it differently
		switch contentType {
		case "application/ld+json":
			resource, err := p.ResourceRepository.Initialize(ctxt.Request().Context(), p.Logger, body)
			if err != nil {
				ctxt.Logger().Errorf("unexpected error creating entity: %s", err)
				return NewControllerError("unexpected error creating entity", err, http.StatusBadRequest)
			}
			//if the sequence number is not one more than the current sequence number then return an error
			if seq != 0 && resource.GetSequenceNo() != seq+1 {
				return NewControllerError("unexpected error updating content type.  invalid sequence number", err, http.StatusPreconditionFailed)
			}
			errs := p.ResourceRepository.Persist(ctxt.Request().Context(), p.Logger, []Resource{resource})
			if len(errs) > 0 {
				ctxt.Logger().Errorf("unexpected error persisting entity: %s", errs)
				return NewControllerError("unexpected error persisting entity", errs[0], http.StatusBadRequest)
			}
			//set etag in response header
			ctxt.Response().Header().Set("ETag", fmt.Sprintf("%s.%d", resource.GetID(), resource.GetSequenceNo()))
			if resource.GetSequenceNo() == 1 {
				return ctxt.JSON(http.StatusCreated, resource)
			} else {
				return ctxt.JSON(http.StatusOK, resource)
			}
		default:
			//At the time of this writing only application/ld+json resources can be written. Everything else is
			var defaultProjection Projection
			if projection, ok := p.Projections[resourceType]; ok {
				defaultProjection = projection
			}
			response, err := p.CommandDispatcher.Dispatch(ctxt.Request().Context(), &Command{
				Type: commandName,
			}, ctxt.Logger(), &CommandOptions{
				ResourceRepository: p.ResourceRepository,
				DefaultProjection:  defaultProjection,
			})

			if response.Code != 0 {
				return ctxt.JSON(response.Code, response.Body)
			} else {
				if err != nil {
					return ctxt.NoContent(http.StatusInternalServerError)
				} else {
					return ctxt.NoContent(http.StatusOK)
				}
			}
		}
	}
}

// DefaultExecuteController handles the write operations that have a command associated with them
func DefaultExecuteController(p *ControllerParams) echo.HandlerFunc {
	var commandName string
	var err error

	for method, toperation := range p.Operation {
		//get the schema for the operation
		for _, requestContent := range toperation.RequestBody.Value.Content {
			if requestContent.Schema != nil {
				//use the first schema ref to determine the entity type
				if requestContent.Schema.Ref != "" {
					//get the entity type from the ref
					//schema = p.Schema.Components.Schemas[requestContent.Schema.Ref].Value
				}
			}
		}
		//If there is a x-command extension then dispatch that command by default
		if rawCommand, ok := toperation.Extensions["x-command"].(json.RawMessage); ok {
			err := json.Unmarshal(rawCommand, &commandName)
			if err != nil {
				p.Logger.Fatalf("error unmarshalling command: %s", err)
			}
		}
		//If there is a x-command-name extension then dispatch that command by default otherwise use the default command based on the operation type
		if commandName == "" {
			switch method {
			case http.MethodPost:
				commandName = UPDATE_COMMAND
			case http.MethodPut:
				commandName = UPDATE_COMMAND
			case http.MethodPatch:
				commandName = UPDATE_COMMAND
			case http.MethodDelete:
				commandName = DELETE_COMMAND
			}
		}
	}

	return func(ctxt echo.Context) error {
		var sequenceNo string
		var seq int

		//getting etag from context
		etag := ctxt.Request().Header.Get("If-Match")
		if etag != "" {
			_, sequenceNo = SplitEtag(etag)
			seq, err = strconv.Atoi(sequenceNo)
			if err != nil {
				return NewControllerError("unexpected error updating content type.  invalid sequence number", err, http.StatusBadRequest)
			}
		}

		body, err := io.ReadAll(ctxt.Request().Body)
		if err != nil {
			ctxt.Logger().Debugf("unexpected error reading request body: %s", err)
			return NewControllerError("unexpected error reading request body", err, http.StatusBadRequest)
		}

		command := &Command{
			Type:    commandName,
			Payload: body,
			Metadata: CommandMetadata{
				SequenceNo: seq,
				Version:    1,
				UserID:     GetUser(ctxt.Request().Context()),
				AccountID:  GetAccount(ctxt.Request().Context()),
			},
		}
		response, err := p.CommandDispatcher.Dispatch(ctxt.Request().Context(), command, ctxt.Logger(), &CommandOptions{
			ResourceRepository: p.ResourceRepository,
			DefaultProjection:  nil,
			Projections:        nil,
		})
		//an error handler `HTTPErrorHandler` can be defined on the echo instance to handle error responses
		if err != nil {
			return NewControllerError("unexpected error executing command", err, http.StatusBadRequest)
		}
		return ctxt.JSON(response.Code, response.Body)
	}
}

// DefaultReadController handles the read operations viewing a specific item
func DefaultReadController(p *ControllerParams) echo.HandlerFunc {
	return func(ctxt echo.Context) error {
		contentType := ctxt.Request().Header.Get(echo.HeaderContentType)
		//for certain content types treat with it differently
		switch contentType {
		case "application/ld+json":
			resource, err := p.ResourceRepository.Initialize(ctxt.Request().Context(), p.Logger, []byte("{}"))
			if err != nil {
				return NewControllerError("unexpected error creating entity", err, http.StatusBadRequest)
			}
			var payload []byte
			//if the sequence no is one that means it's a new resource and the resource doesn't exist
			if resource.GetSequenceNo() == 1 {
				return ctxt.NoContent(http.StatusNotFound)
			}
			payload, err = json.Marshal(resource)
			return ctxt.Blob(http.StatusOK, "application/ld+json", payload)
		default:
			//if there a path map then use that to get the resource
			for path, _ := range p.PathMap {
				if path != p.APIConfig.BasePath+"/*" {
					//TODO check to see if there is a type in the path and try to get the projection
					//TODO check to see the params and try to get the item by that
				} else {
					return ctxt.NoContent(http.StatusNotFound)
				}
			}
		}

		return ctxt.NoContent(http.StatusNotFound)
	}
}
