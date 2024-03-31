package rest

import (
	"encoding/json"
	"github.com/getkin/kin-openapi/openapi3"
	"golang.org/x/net/context"
)

type ResourceCreateParams struct {
	Resource Resource
	Schema   *openapi3.Schema
}

// CreateHandler is used to add entities to the repository.
func CreateHandler(ctx context.Context, command *Command, repository Repository, logger Log) (interface{}, error) {
	var err error
	var params *ResourceCreateParams
	err = json.Unmarshal(command.Payload, &params)
	if err != nil {
		logger.Errorf("error creating entity: %s", err)
		return nil, err
	}
	entity := params.Resource
	//TODO as per the solid framework all intermediate paths in the URI should be created
	if entity.IsValid() {
		errs := repository.Persist(ctx, logger, []Resource{entity})
		if len(errs) > 0 {
			logger.Errorf("error persisting entity: %s", err)
			return nil, err
		}
	} else {
		return nil, entity.GetErrors()[0]
	}
	return entity, nil
}
