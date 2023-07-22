package rest

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/gommon/log"
	weosContext "github.com/wepala/weos/context"
	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"
	"golang.org/x/net/context"
	"gorm.io/gorm"
	"reflect"
)

//Security adds authorization middleware to the initialize context
func Security(ctxt context.Context, tapi Container, swagger *openapi3.Swagger) (context.Context, error) {
	if swagger.Components.SecuritySchemes != nil {
		middlewares := GetOperationMiddlewares(ctxt)
		logger, err := tapi.GetLog("Default")
		if err != nil {
			logger = log.New("weos")
		}
		config, err := new(SecurityConfiguration).FromSchema(swagger.Components.SecuritySchemes)
		if err != nil {
			logger.Debugf("error loading security schemes '%s'", err)
			return ctxt, err
		}
		//set config to container
		tapi.RegisterSecurityConfiguration(config)
		//check that all the security references are valid
		for _, security := range swagger.Security {
			for k, _ := range security {
				if _, ok := config.Validators[k]; !ok {
					return ctxt, fmt.Errorf("unable to find security configuration '%s'", k)
				}
			}
		}
		middlewares = append(middlewares, config.Middleware)
		ctxt = context.WithValue(ctxt, weosContext.MIDDLEWARES, middlewares)
	}

	return ctxt, nil
}

//SQLDatabase initial sql databases based on configs
func SQLDatabase(ctxt context.Context, tapi Container, swagger *openapi3.Swagger) (context.Context, error) {
	api := tapi.(*RESTAPI)
	var err error
	if api.GetConfig() != nil {
		if weosConfigData, ok := swagger.Extensions[WeOSConfigExtension]; ok {
			var config *APIConfig
			if err = json.Unmarshal(weosConfigData.(json.RawMessage), &config); err == nil {
				//if the legacy way of instantiating a connection is present then use that as the default
				if config.ServiceConfig != nil && config.ServiceConfig.Database != nil {
					var connection *sql.DB
					var gormDB *gorm.DB
					if connection, gormDB, _, err = api.SQLConnectionFromConfig(config.Database); err == nil {
						api.RegisterDBConnection("Default", connection)
						api.RegisterGORMDB("Default", gormDB)
					}
				}
				//if the databases are configured in the databases array add each one
				if config.ServiceConfig != nil && len(config.ServiceConfig.Databases) > 0 {
					for _, dbconfig := range config.ServiceConfig.Databases {
						var connection *sql.DB
						var gormDB *gorm.DB
						if connection, gormDB, _, err = api.SQLConnectionFromConfig(dbconfig); err == nil {
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
func DefaultProjection(ctxt context.Context, tapi Container, swagger *openapi3.Swagger) (context.Context, error) {
	api := tapi.(*RESTAPI)
	var err error
	if api.gormConnection != nil {
		//setup default projection if gormDB is configured
		defaultProjection, _ := api.GetProjection("Default")
		if defaultProjection == nil {
			defaultProjection, err = projections.NewProjection(ctxt, tapi, api.gormConnection, api.EchoInstance().Logger)
			api.RegisterProjection("Default", defaultProjection)

			//---- TODO clean up setting up schemas here

			//This will check the enum types on run and output an error
			for _, scheme := range api.GetConfig().Components.Schemas {
				for pName, prop := range scheme.Value.Properties {
					if prop.Value.Enum != nil {
						t := prop.Value.Type
						for _, v := range prop.Value.Enum {
							switch t {
							case "string":
								if reflect.TypeOf(v).String() != "string" {
									err = fmt.Errorf("expected field: %s, of type %s, to have enum options of the same type", pName, t)
									return ctxt, err
								}
							case "integer":
								if reflect.TypeOf(v).String() != "float64" {
									if v.(string) == "null" {
										continue
									} else {
										err = fmt.Errorf("expected field: %s, of type %s, to have enum options of the same type", pName, t)
										return ctxt, err
									}
								}
							case "number":
								if reflect.TypeOf(v).String() != "float64" {
									if v.(string) == "null" {
										continue
									} else {
										err = fmt.Errorf("expected field: %s, of type %s, to have enum options of the same type", pName, t)
										return ctxt, err
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
			for _, pathData := range api.GetConfig().Paths {
				for _, op := range pathData.Operations() {
					if op.OperationID != "" {
						opIDs = append(opIDs, op.OperationID)
					}
				}
			}

			//this ranges over the properties, pulls the x-update and them compares it against the valid operation ids in the yaml
			for _, scheme := range api.GetConfig().Components.Schemas {
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
							return ctxt, err
						}
					}
				}
			}

			//get fields to be removed during migration step
			deletedFields := map[string][]string{}
			for name, sch := range api.GetConfig().Components.Schemas {
				dfs, _ := json.Marshal(sch.Value.Extensions[RemoveExtension])
				var df []string
				json.Unmarshal(dfs, &df)
				deletedFields[name] = df
			}

			//run migrations
			err = defaultProjection.Migrate(ctxt, api.GetConfig())
			if err != nil {
				api.EchoInstance().Logger.Error(err)
				return ctxt, err
			}
		}
	}
	if err != nil {
		api.EchoInstance().Logger.Warnf("Default projection not created '%s'", err)
	}
	return ctxt, nil
}

//DefaultEventStore setup default gorm projection
func DefaultEventStore(ctxt context.Context, tapi Container, swagger *openapi3.Swagger) (context.Context, error) {
	api := tapi.(*RESTAPI)
	var err error
	var gormDB *gorm.DB
	gormDB = api.gormConnection
	//if there is a projection then add the event handler as a subscriber to the event store
	if gormDB != nil {
		var defaultEventStore model.EventRepository
		defaultEventStore, err = model.NewBasicEventRepository(gormDB, api.EchoInstance().Logger, false, "", "")
		err = defaultEventStore.Migrate(ctxt)
		api.RegisterEventStore("Default", defaultEventStore)
	}
	if err != nil {
		api.EchoInstance().Logger.Warnf("Default projection not created '%s'", err)
	}
	return ctxt, nil
}

//RegisterEntityRepositories registers the entity repositories based on the schema definitions
func RegisterEntityRepositories(ctxt context.Context, api Container, swagger *openapi3.Swagger) (context.Context, error) {
	for schemaName, schema := range swagger.Components.Schemas {
		if _, ok := schema.Value.Extensions["x-inline"]; !ok {
			//get the schema details from the swagger file
			repository, err := projections.NewGORMRepository(ctxt, api, schemaName, schema.Value)
			if err != nil {
				return ctxt, err
			}
			api.RegisterEntityRepository(repository.Name(), repository)
		}
	}
	return ctxt, nil
}

//ZapLoggerInitializer add middleware to all paths to log the request and setup zap logger
func ZapLoggerInitializer(ctxt context.Context, tapi Container, swagger *openapi3.Swagger) (context.Context, error) {
	middlewares := GetOperationMiddlewares(ctxt)
	middlewares = append(middlewares, ZapLogger)
	ctxt = context.WithValue(ctxt, weosContext.MIDDLEWARES, middlewares)
	return ctxt, nil
}
