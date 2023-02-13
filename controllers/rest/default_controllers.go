package rest

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	context2 "github.com/wepala/weos/context"
	"github.com/wepala/weos/model"
	"html/template"
	"net/http"
	"os"
	path1 "path"
	"strconv"
	"strings"
)

//DefaultWriteController handles the write operations (create, update, delete)
func DefaultWriteController(api Container, commandDispatcher model.CommandDispatcher, entityRepository model.EntityRepository, pathMap map[string]*openapi3.PathItem, operation map[string]*openapi3.Operation) echo.HandlerFunc {
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
				commandName = model.CREATE_COMMAND
			case http.MethodPut:
				commandName = model.UPDATE_COMMAND
			case http.MethodDelete:
				commandName = model.DELETE_COMMAND
			}
		}
	}

	return func(ctxt echo.Context) error {
		var weosID string
		var sequenceNo string
		var seq int
		var commandResponse interface{}

		//getting etag from context
		etag := ctxt.Request().Header.Get("If-Match")
		if etag != "" {
			weosID, sequenceNo = SplitEtag(etag)
			seq, err = strconv.Atoi(sequenceNo)
			if err != nil {
				return NewControllerError("unexpected error updating content type.  invalid sequence number", err, http.StatusBadRequest)
			}
		}

		var entityType string
		if entityRepository != nil {
			entityType = entityRepository.Name()
		}

		command := &model.Command{
			Type:    commandName,
			Payload: context2.GetPayload(ctxt.Request().Context()),
			Metadata: model.CommandMetadata{
				EntityID:   weosID,
				EntityType: entityType,
				SequenceNo: seq,
				Version:    1,
				UserID:     context2.GetUser(ctxt.Request().Context()),
				AccountID:  context2.GetAccount(ctxt.Request().Context()),
			},
		}
		commandResponse, err = commandDispatcher.Dispatch(ctxt.Request().Context(), command, api, entityRepository, ctxt.Logger())
		if err != nil {
			return err
		}

		//TODO the type of command and/or the api configuration should determine the status code
		switch commandName {
		case "create":
			//set etag in response header
			if entity, ok := commandResponse.(*model.ContentEntity); ok {
				ctxt.Response().Header().Set("ETag", fmt.Sprintf("%s.%d", entity.ID, entity.SequenceNo))
				return ctxt.JSON(http.StatusCreated, entity)
			}
			return ctxt.JSON(http.StatusCreated, commandResponse)
		default:
			return ctxt.JSON(http.StatusOK, commandResponse)
		}
	}
}

//DefaultReadController handles the read operations viewing a specific item
func DefaultReadController(api Container, commandDispatcher model.CommandDispatcher, entityRepository model.EntityRepository, pathMap map[string]*openapi3.PathItem, operationMap map[string]*openapi3.Operation) echo.HandlerFunc {
	logger, err := api.GetLog("Default")
	if err != nil {
		log.Fatal("no logger defined")
	}
	var templates []string
	fileName := ""
	folderFound := true
	folderErr := ""
	for currentPath, _ := range pathMap {
		for _, operation := range operationMap {
			for _, resp := range operation.Responses {
				//for 200 responses look at the accept header and determine what to render
				//TODO make this compatible with all status codes
				if templateExtension, ok := resp.Value.ExtensionProps.Extensions[TemplateExtension]; ok {
					err := json.Unmarshal(templateExtension.(json.RawMessage), &templates)
					if err != nil {
						logger.Error(err)
					}
				}
				if folderExtension, ok := resp.Value.ExtensionProps.Extensions[FolderExtension]; ok {
					folderPath := ""
					err = json.Unmarshal(folderExtension.(json.RawMessage), &folderPath)
					if err != nil {
						logger.Error(err)
					} else {
						_, err = os.Stat(folderPath)
						if os.IsNotExist(err) {
							folderFound = false
							folderErr = "error finding folder: " + folderPath + " specified on path: " + currentPath
							logger.Errorf(folderErr)
						} else if err != nil {
							logger.Error(err)
						} else {
							api.(*RESTAPI).e.Static(api.GetWeOSConfig().BasePath+currentPath, folderPath)
						}
					}
				}
				if fileExtension, ok := resp.Value.ExtensionProps.Extensions[FileExtension]; ok {
					filePath := ""
					err = json.Unmarshal(fileExtension.(json.RawMessage), &filePath)
					if err != nil {
						logger.Error(err)
					} else {
						_, err = os.Stat(filePath)
						if os.IsNotExist(err) {
							logger.Debugf("error finding file: '%s' specified on path: '%s'", filePath, currentPath)
						} else if err != nil {
							logger.Error(err)
						} else {
							fileName = filePath
						}
					}
				}

				if !folderFound {
					logger.Errorf(folderErr)
				}
			}
		}
	}

	return func(ctxt echo.Context) error {
		var entity *model.ContentEntity
		var err error
		//get identifier from context if there is an entity repository (some endpoints may not have a schema associated)
		if entityRepository != nil {
			entity, err = entityRepository.CreateEntityWithValues(ctxt.Request().Context(), []byte("{}"))
			if err != nil {
				return NewControllerError("unexpected error creating entity", err, http.StatusBadRequest)
			}
			identifier, err := entity.Identifier()
			for k, _ := range identifier {
				//get from context since the middleware restricts param to what wsa configured in the spec
				identifier[k] = ctxt.Request().Context().Value(k)
			}
			if err != nil {
				return NewControllerError("unexpected error getting identifier", err, http.StatusBadRequest)
			}
			entity, err = entityRepository.GetByKey(ctxt.Request().Context(), entityRepository, identifier)
			if err != nil {
				return NewControllerError("unexpected error getting entity", err, http.StatusBadRequest)
			}

			if entity == nil && len(templates) == 0 {
				return ctxt.JSON(http.StatusNotFound, nil)
			}
		}
		//render html if that is configured

		//check header to determine response check the accepts header
		acceptHeader := ctxt.Request().Header.Get("Accept")

		// if no accept header is found it defaults to application/json
		if acceptHeader == "" {
			acceptHeader = "application/json"
		}

		contentType := ResolveResponseType(acceptHeader, operationMap[http.MethodGet].Responses["200"].Value.Content)
		switch contentType {
		case "application/json":
			if entity != nil {
				return ctxt.JSON(http.StatusOK, entity)
			}
		default:
			if fileName != "" {
				return ctxt.File(fileName)
			} else if len(templates) > 0 {
				contextValues := ReturnContextValues(ctxt.Request().Context())
				t := template.New(path1.Base(templates[0]))
				t, err := t.ParseFiles(templates...)
				if err != nil {
					ctxt.Logger().Debugf("unexpected error %s ", err)
					return NewControllerError(fmt.Sprintf("unexpected error %s ", err), err, http.StatusInternalServerError)

				}
				err = t.Execute(ctxt.Response().Writer, contextValues)
				if err != nil {
					ctxt.Logger().Debugf("unexpected error %s ", err)
					return NewControllerError(fmt.Sprintf("unexpected error %s ", err), err, http.StatusInternalServerError)

				}
			}
		}

		return nil
	}
}

//DefaultListController handles the read operations viewing a list of items
func DefaultListController(api Container, commandDispatcher model.CommandDispatcher, entityRepository model.EntityRepository, pathMap map[string]*openapi3.PathItem, operationMap map[string]*openapi3.Operation) echo.HandlerFunc {
	return func(ctxt echo.Context) error {
		var filterOptions map[string]interface{}
		newContext := ctxt.Request().Context()
		//gets the filter, limit and page from context
		limit, _ := newContext.Value("limit").(int)
		page, _ := newContext.Value("page").(int)
		filters := newContext.Value("_filters")
		format := newContext.Value("_format")
		headers := newContext.Value("_headers")

		responseType := "application/json"

		if format != nil {
			responseType = ResolveResponseType(format.(string), operationMap[http.MethodGet].Responses["200"].Value.Content)
		}

		if entityRepository != nil {
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

			if responseType == "application/json" {
				return ctxt.JSON(http.StatusOK, resp)
			} else if responseType == "text/csv" {

				// generate csv
				w := ctxt.Response().Writer
				w.Header().Set("Content-Type", responseType)

				writer := csv.NewWriter(w)

				var csvKeys []string
				var dbFields []string

				if headerProperties, ok := headers.([]*HeaderProperties); ok && len(headerProperties) != 0 {
					for _, headerProperty := range headerProperties {
						csvKeys = append(csvKeys, headerProperty.Header)
						dbFields = append(dbFields, headerProperty.Field)
					}
				} else {
					entity := contentEntities[0].ToMap()
					for key := range entity {
						csvKeys = append(csvKeys, key)
					}
					dbFields = csvKeys
				}

				err = writer.Write(csvKeys)

				for i := 0; i < int(count); i++ {
					entityMap := contentEntities[i].ToMap()
					row := make([]string, len(dbFields))
					for j, field := range dbFields {
						row[j] = fmt.Sprintf("%v", entityMap[field])
					}

					err := writer.Write(row)
					if err != nil {
						return ctxt.NoContent(http.StatusInternalServerError)
					}
				}

				writer.Flush()

				if writer.Error() != nil {
					return ctxt.NoContent(http.StatusInternalServerError)
				}

				return ctxt.NoContent(http.StatusOK)
			}
		}
		return ctxt.NoContent(http.StatusOK)
	}
}
