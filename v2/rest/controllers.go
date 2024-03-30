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

// DefaultWriteController handles the write operations (create, update, delete)
func DefaultWriteController(logger Log, commandDispatcher CommandDispatcher, resourceRepository Repository, api *openapi3.T, pathMap map[string]*openapi3.PathItem, operation map[string]*openapi3.Operation) echo.HandlerFunc {
	var commandName string
	var err error
	var schema *openapi3.Schema

	for method, toperation := range operation {
		//get the schema for the operation
		for _, requestContent := range toperation.RequestBody.Value.Content {
			if requestContent.Schema != nil {
				//use the first schema ref to determine the entity type
				if requestContent.Schema.Ref != "" {
					//get the entity type from the ref
					schema = api.Components.Schemas[requestContent.Schema.Ref].Value
				}
			}
		}
		//If there is a x-command extension then dispatch that command by default
		if rawCommand, ok := toperation.Extensions["x-command"].(json.RawMessage); ok {
			err := json.Unmarshal(rawCommand, &commandName)
			if err != nil {
				logger.Fatalf("error unmarshalling command: %s", err)
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
		var commandResponse interface{}

		//getting etag from context
		etag := ctxt.Request().Header.Get("If-Match")
		if etag != "" {
			_, sequenceNo = SplitEtag(etag)
			seq, err = strconv.Atoi(sequenceNo)
			if err != nil {
				return NewControllerError("unexpected error updating content type.  invalid sequence number", err, http.StatusBadRequest)
			}
		}

		var resource *BasicResource
		body, err := io.ReadAll(ctxt.Request().Body)
		if err != nil {
			ctxt.Logger().Debugf("unexpected error reading request body: %s", err)
			return NewControllerError("unexpected error reading request body", err, http.StatusBadRequest)
		}
		resource, err = new(BasicResource).FromSchema(api, body)
		//not sure this is correct
		payload, err := json.Marshal(&ResourceCreateParams{
			Resource: resource,
			Schema:   schema,
		})

		command := &Command{
			Type:    commandName,
			Payload: payload,
			Metadata: CommandMetadata{
				EntityID:   resource.GetID(),
				EntityType: resource.GetType(),
				SequenceNo: seq,
				Version:    1,
				UserID:     GetUser(ctxt.Request().Context()),
				AccountID:  GetAccount(ctxt.Request().Context()),
			},
		}
		commandResponse, err = commandDispatcher.Dispatch(ctxt.Request().Context(), command, resourceRepository, ctxt.Logger())
		//an error handler `HTTPErrorHandler` can be defined on the echo instance to handle error responses
		if err != nil {

		}

		//TODO the type of command and/or the api configuration should determine the status code
		switch commandName {
		case "create":
			//set etag in response header
			if entity, ok := commandResponse.(*BasicResource); ok {
				ctxt.Response().Header().Set("ETag", fmt.Sprintf("%s.%d", entity.GetID(), entity.GetSequenceNo()))
				return ctxt.JSON(http.StatusCreated, entity)
			}
			return ctxt.JSON(http.StatusCreated, commandResponse)
		default:
			//check to see if the response is a map or string
			if stringResponse, ok := commandResponse.(string); ok {
				return ctxt.String(http.StatusOK, stringResponse)
			} else {
				return ctxt.JSON(http.StatusOK, commandResponse)
			}
		}
	}
}

// DefaultReadController handles the read operations viewing a specific item
//func DefaultReadController(logger Log, commandDispatcher CommandDispatcher, entityRepository EntityRepository, pathMap map[string]*openapi3.PathItem, operationMap map[string]*openapi3.Operation) echo.HandlerFunc {
//	var templates []string
//	fileName := ""
//	folderFound := true
//	folderErr := ""
//	isFolder := false
//	for currentPath, _ := range pathMap {
//		for _, operation := range operationMap {
//			for _, resp := range operation.Responses {
//				//for 200 responses look at the accept header and determine what to render
//				//TODO make this compatible with all status codes
//				if templateExtension, ok := resp.Value.ExtensionProps.Extensions[TemplateExtension]; ok {
//					err := json.Unmarshal(templateExtension.(json.RawMessage), &templates)
//					if err != nil {
//						logger.Error(err)
//					}
//				}
//				if folderExtension, ok := resp.Value.ExtensionProps.Extensions[FolderExtension]; ok {
//					isFolder = true
//					folderPath := ""
//					err = json.Unmarshal(folderExtension.(json.RawMessage), &folderPath)
//					if err != nil {
//						logger.Error(err)
//					} else {
//						_, err = os.Stat(folderPath)
//						if os.IsNotExist(err) {
//							folderFound = false
//							folderErr = "error finding folder: " + folderPath + " specified on path: " + currentPath
//							logger.Errorf(folderErr)
//						} else if err != nil {
//							logger.Error(err)
//						} else {
//							api.(*RESTAPI).e.Static(api.GetWeOSConfig().BasePath+currentPath, folderPath)
//						}
//					}
//				}
//				if fileExtension, ok := resp.Value.ExtensionProps.Extensions[FileExtension]; ok {
//					filePath := ""
//					err = json.Unmarshal(fileExtension.(json.RawMessage), &filePath)
//					if err != nil {
//						logger.Error(err)
//					} else {
//						_, err = os.Stat(filePath)
//						if os.IsNotExist(err) {
//							logger.Debugf("error finding file: '%s' specified on path: '%s'", filePath, currentPath)
//						} else if err != nil {
//							logger.Error(err)
//						} else {
//							fileName = filePath
//						}
//					}
//				}
//
//				if !folderFound {
//					logger.Errorf(folderErr)
//				}
//			}
//		}
//	}
//
//	return func(ctxt echo.Context) error {
//		var entity *model.ContentEntity
//		var err error
//		//get identifier from context if there is an entity repository (some endpoints may not have a schema associated)
//		if entityRepository != nil {
//			entity, err = entityRepository.CreateEntityWithValues(ctxt.Request().Context(), []byte("{}"))
//			if err != nil {
//				return NewControllerError("unexpected error creating entity", err, http.StatusBadRequest)
//			}
//			identifier, err := entity.Identifier()
//			for k, _ := range identifier {
//				//get from context since the middleware restricts param to what wsa configured in the spec
//				identifier[k] = ctxt.Request().Context().Value(k)
//			}
//			if err != nil {
//				return NewControllerError("unexpected error getting identifier", err, http.StatusBadRequest)
//			}
//			entity, err = entityRepository.GetByKey(ctxt.Request().Context(), entityRepository, identifier)
//			if err != nil {
//				return NewControllerError("unexpected error getting entity", err, http.StatusBadRequest)
//			}
//
//			if entity == nil && len(templates) == 0 && fileName == "" {
//				return ctxt.JSON(http.StatusNotFound, nil)
//			}
//
//			if isFolder && !folderFound {
//				return ctxt.JSON(http.StatusNotFound, nil)
//			}
//		}
//		//render html if that is configured
//
//		//check header to determine response check the accepts header
//		acceptHeader := ctxt.Request().Header.Get("Accept")
//
//		// if no accept header is found it defaults to application/json
//		if acceptHeader == "" {
//			acceptHeader = "application/json"
//		}
//
//		contentType := ResolveResponseType(acceptHeader, operationMap[http.MethodGet].Responses["200"].Value.Content)
//		switch contentType {
//		case "application/json":
//			if entity != nil {
//				return ctxt.JSON(http.StatusOK, entity)
//			}
//		default:
//			if fileName != "" {
//				return ctxt.File(fileName)
//			} else if len(templates) > 0 {
//				contextValues := ReturnContextValues(ctxt.Request().Context())
//				t := template.New(path1.Base(templates[0]))
//				t, err := t.ParseFiles(templates...)
//				if err != nil {
//					ctxt.Logger().Debugf("unexpected error %s ", err)
//					return NewControllerError(fmt.Sprintf("unexpected error %s ", err), err, http.StatusInternalServerError)
//
//				}
//				err = t.Execute(ctxt.Response().Writer, contextValues)
//				if err != nil {
//					ctxt.Logger().Debugf("unexpected error %s ", err)
//					return NewControllerError(fmt.Sprintf("unexpected error %s ", err), err, http.StatusInternalServerError)
//
//				}
//			}
//		}
//
//		return nil
//	}
//}
