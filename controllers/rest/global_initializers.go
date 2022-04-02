package rest

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	weosContext "github.com/wepala/weos/context"
	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"
	"golang.org/x/net/context"
	"gorm.io/gorm"
	"reflect"
)

//Security adds authorization middleware to the initialize context
func Security(ctxt context.Context, api *RESTAPI, swagger *openapi3.Swagger) (context.Context, error) {
	middlewares := GetOperationMiddlewares(ctxt)
	found := false

	for _, security := range swagger.Security {
		for key, _ := range security {
			if swagger.Components.SecuritySchemes != nil && swagger.Components.SecuritySchemes[key] != nil {
				//checks if the security scheme has type openIdConnect
				if swagger.Components.SecuritySchemes[key].Value.Type == "openIdConnect" {
					found = true
					break
				}

			}
		}

	}
	if found {
		if middleware, _ := api.GetMiddleware("OpenIDMiddleware"); middleware != nil {
			middlewares = append(middlewares, middleware)
		}
		ctxt = context.WithValue(ctxt, weosContext.MIDDLEWARES, middlewares)
	} else {
		if swagger.Components.SecuritySchemes != nil && swagger.Security != nil {
			api.EchoInstance().Logger.Errorf("unexpected error: security defined does not match any security schemes")
			return ctxt, fmt.Errorf("unexpected error: security defined does not match any security schemes")
		}

	}
	return ctxt, nil
}

//SQLDatabase initial sql databases based on configs
func SQLDatabase(ctxt context.Context, api *RESTAPI, swagger *openapi3.Swagger) (context.Context, error) {
	var err error
	if api.Swagger != nil {
		if weosConfigData, ok := api.Swagger.Extensions[WeOSConfigExtension]; ok {
			var config *APIConfig
			if err = json.Unmarshal(weosConfigData.(json.RawMessage), &config); err == nil {
				//if the legacy way of instantiating a connection is present then use that as the default
				if config.ServiceConfig != nil && config.ServiceConfig.Database != nil {
					var connection *sql.DB
					var gormDB *gorm.DB
					if connection, gormDB, err = api.SQLConnectionFromConfig(config.Database); err == nil {
						api.RegisterDBConnection("Default", connection)
						api.RegisterGORMDB("Default", gormDB)
					}
				}
				//if the databases are configured in the databases array add each one
				if config.ServiceConfig != nil && len(config.ServiceConfig.Databases) > 0 {
					for _, dbconfig := range config.ServiceConfig.Databases {
						var connection *sql.DB
						var gormDB *gorm.DB
						if connection, gormDB, err = api.SQLConnectionFromConfig(dbconfig); err == nil {
							api.RegisterDBConnection(dbconfig.Name, connection)
							api.RegisterGORMDB(dbconfig.Name, gormDB)
						}
					}
				}
			}
		}
	}
	return ctxt, err
}

//DefaultProjection setup default gorm projection
func DefaultProjection(ctxt context.Context, api *RESTAPI, swagger *openapi3.Swagger) (context.Context, error) {
	var gormDB *gorm.DB
	var err error
	if gormDB, err = api.GetGormDBConnection("Default"); err == nil {
		//setup default projection if gormDB is configured
		defaultProjection, _ := api.GetProjection("Default")
		if defaultProjection == nil {
			defaultProjection, err = projections.NewProjection(ctxt, gormDB, api.EchoInstance().Logger)
			api.RegisterProjection("Default", defaultProjection)

			//---- TODO clean up setting up schemas here

			//This will check the enum types on run and output an error
			for _, scheme := range api.Swagger.Components.Schemas {
				for pName, prop := range scheme.Value.Properties {
					if prop.Value.Enum != nil {
						t := prop.Value.Type
						for _, v := range prop.Value.Enum {
							switch t {
							case "string":
								if reflect.TypeOf(v).String() != "string" {
									err = fmt.Errorf("expected field: %s, of type %s, to have enum options of the same type", pName, t)
								}
							case "integer":
								if reflect.TypeOf(v).String() != "float64" {
									if v.(string) == "null" {
										continue
									} else {
										err = fmt.Errorf("expected field: %s, of type %s, to have enum options of the same type", pName, t)
									}
								}
							case "number":
								if reflect.TypeOf(v).String() != "float64" {
									if v.(string) == "null" {
										continue
									} else {
										err = fmt.Errorf("expected field: %s, of type %s, to have enum options of the same type", pName, t)
									}
								}
							}
						}
					}
				}
			}

			//this ranges over the paths and pulls out the operationIDs into an array
			opIDs := []string{}
			idFound := false
			for _, pathData := range api.Swagger.Paths {
				for _, op := range pathData.Operations() {
					if op.OperationID != "" {
						opIDs = append(opIDs, op.OperationID)
					}
				}
			}

			//this ranges over the properties, pulls the x-update and them compares it against the valid operation ids in the yaml
			for _, scheme := range api.Swagger.Components.Schemas {
				for _, prop := range scheme.Value.Properties {
					xUpdate := []string{}
					xUpdateBytes, _ := json.Marshal(prop.Value.Extensions["x-update"])
					json.Unmarshal(xUpdateBytes, &xUpdate)
					for _, r := range xUpdate {
						idFound = false
						for _, id := range opIDs {
							if r == id {
								idFound = true
							}
						}
						if !idFound {
							err = fmt.Errorf("provided x-update operation id: %s is invalid", r)
						}
					}
				}
			}

			//get the database schema
			schemas := CreateSchema(ctxt, api.EchoInstance(), api.Swagger)
			api.Schemas = schemas
			ctxt = context.WithValue(ctxt, weosContext.SCHEMA_BUILDERS, schemas)

			//get fields to be removed during migration step
			deletedFields := map[string][]string{}
			for name, sch := range api.Swagger.Components.Schemas {
				dfs, _ := json.Marshal(sch.Value.Extensions[RemoveExtension])
				var df []string
				json.Unmarshal(dfs, &df)
				deletedFields[name] = df
			}

			//run migrations
			err = defaultProjection.Migrate(ctxt, schemas, deletedFields)
			if err != nil {
				api.EchoInstance().Logger.Error(err)
			}
		}
	}
	if err != nil {
		api.EchoInstance().Logger.Warnf("Default projection not created '%s'", err)
	}
	return ctxt, nil
}

//DefaultEventStore setup default gorm projection
func DefaultEventStore(ctxt context.Context, api *RESTAPI, swagger *openapi3.Swagger) (context.Context, error) {
	var err error
	var gormDB *gorm.DB
	//if there is a projection then add the event handler as a subscriber to the event store
	if gormDB, err = api.GetGormDBConnection("Default"); err == nil {
		var defaultEventStore model.EventRepository
		defaultEventStore, err = model.NewBasicEventRepository(gormDB, api.EchoInstance().Logger, false, "", "")
		//if there is a default projection then add it as a listener to the default event store
		if defaultProjection, err := api.GetProjection("Default"); err == nil {
			defaultEventStore.AddSubscriber(defaultProjection.GetEventHandler())
		}
		err = defaultEventStore.Migrate(ctxt)
		api.RegisterEventStore("Default", defaultEventStore)
	}
	if err != nil {
		api.EchoInstance().Logger.Warnf("Default projection not created '%s'", err)
	}
	return ctxt, nil
}
