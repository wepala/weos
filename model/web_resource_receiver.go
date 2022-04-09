package model

import (
	"encoding/json"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"golang.org/x/net/context"
)

func CreateWebResourceHandler(ctx context.Context, command *Command, eventStore EventRepository, projection Projection, logger Log) error {
	if logger == nil {
		return fmt.Errorf("no logger set")
	}

	//initialize any services
	domainService := NewWebResourceService(command.Metadata.ServerURL, projection)

	var schema *openapi3.Schema

	entityFactory := GetEntityFactory(ctx)
	//if there is no entity factory then let's look up the schema in the system
	if entityFactory == nil {
		schemaURL := GetSchemaURL(command.Metadata.ServerURL, command.Metadata.Path)
		schemaResource, err := projection.GetByID(ctx, schemaURL)
		if err != nil {
			return NewDomainError(fmt.Sprintf("schema '%s' not found", schemaURL), "WebResource", "", err)
		}
		//if a schema resource is found let's use that
		if schemaResource != nil {
			//convert schema web resource to schema
			schemaBytes, err := json.Marshal(schemaResource)
			if err != nil {
				return &WeOSError{
					message: "error converting schemaResource to OpenAPI resource",
				}
			}
			err = json.Unmarshal(schemaBytes, schema)
			if err != nil {
				return &WeOSError{
					message: fmt.Sprintf("error unmarshalling schema '%s'", err),
				}
			}
		} else {
			//let's create a schema based on the payload
			schema = SchemaFromPayload(command.Payload)
			_, err := domainService.CreateSchema(ctx, command.Metadata.Path, schema)
			if err != nil {
				return &WeOSError{
					message: fmt.Sprintf("error creating schemaResource '%s'", err),
				}
			}
		}
	} else {
		schema = entityFactory.Schema()
	}

	//check to see if there is a schema
	updatedEntity, err := domainService.Create(ctx, command.Metadata.ServerURL, command.Metadata.Path)
	if err != nil {
		return err
	}
	err = eventStore.Persist(ctx, updatedEntity)
	if err != nil {
		return err
	}
	return nil
}
