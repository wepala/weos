# API Startup Flow Chart

## Main entrypoint 

The OpenAPI spec can be passed to the CLI via environment variable `WEOS_SCHEMA` or as a command line argument `schema`. 
The value for the OpenAPI spec could be the contents of a valid spec or the filepath to the specification

```mermaid
flowchart TD
  Start --> ev{config\nenvironment\nvariable}
  ev -->|Yes| evConfig(Set config to environment variable)
  ev -->|No| paramConfig(Set config to cli parameter `schema`)
  evConfig --> instantiateAPI[[Instantiate API]]
  paramConfig --> instantiateAPI[[Instantiate API]] --> run([Api Running])
```

### Instantiate API

```mermaid
flowchart TD
    Start --> newEcho[Instantiate Echo] --> initialize[[Initialize API]] --> initResponse{Successfully\nInitialized}
    initResponse -->|Yes| run([API Running])
    initResponse -->|No| logError[Log Error] --> fail([API Not Running])
```

#### Initialize API

```mermaid
flowchart TD
    Start --> setEcho[Set Echo Instance] --> registerContextMiddleware[Register Context Middleware] --> checkConfigContents{Config is File}
    checkConfigContents -->|Yes| checkFile[Load File] --> loadFileSuccess{Is Successful}
    loadFileSuccess -->|No| logError[Log Error] --> fail([API Not Running])
    loadFileSuccess -->|Yes| interpolateEV[Interpolate Environment Variables] --> parseSchemas[[Parse Schemas]] --> schemaMap[/Dynamic Struct Builder Map/] --> saveSchema[Save Schema] --> processWeOSConfig[[Process WeOS Config]] --> weosConfigParseSucces{Succesful}
    weosConfigParseSucces -->|No| logError
    weosConfigParseSucces -->|Yes| processPreMiddleware{Has middleware} 
    processPreMiddleware -->|Yes| addEchoPreMiddleware[Add to Echo as Pre Middleware] --> processPaths[[Process Paths]] --> run([Api Running])
    processPreMiddleware -->|No| processPaths
```

##### Parse Schemas
This is where the OpenAPI schemas are converted to GORM models 

```mermaid
flowchart TD
    Start --> hasUnprocessedSchema{Unprocessed\nScehma} 
    hasUnprocessedSchema -->|Yes| instantiateSchema[[New Schema]] --> buildersRelationsKeys[/Builders, Relations, Keys/] --> schemaMap[/Dynamic Struct Builder Map/] --> hasUnprocessedBuilders{Unprocessed Builders}
    hasUnprocessedSchema -->|No| hasUnprocessedBuilders{Unprocessed\nBuilders}
    hasUnprocessedBuilders -->|Yes| hasUnprocessedRelationships{Unprocssed\nSchema\nRelationships}
    hasUnprocessedBuilders -->|No| returnMap([Return Dynamic Struct Map])
    hasUnprocessedRelationships -->|Yes| addSchemaRelationship[Add Schema Relationship] --> setGormTags[Set GORM Metadata] --> instantiateGormModel[Instantiate Gorm Model] --> checkTableName{Has Table Name}
    hasUnprocessedRelationships -->|No| setGormTags
    checkTableName -->|Yes| returnMap
    checkTableName -->|No| logError[Log Error] --> returnMap
```

##### Process WeOS Config
The `x-weos-config` contains the database configuration that is used to instantiate a database connection. It also contains
REST middleware configuration 

```mermaid
flowchart TD
    Start --> initializeService[[Initialize Service]] --> hasPreMiddleware{Has\nUnprocessed\nMiddleware}
    hasPreMiddleware -->|Yes| checkMiddlewareIsValid[Check Middleware Is Valid] --> isValidMiddlware{Is Valid}
    hasPreMiddleware -->|No| End
    isValidMiddlware --> |Yes| addToEcho[Add to echo pre middleware] --> End
    isValidMiddlware --> |No| End
```

###### Initialize Service
Each API extends a base `Service` that WeOS provides. When a service is instantiated the db connections are setup using 
`database` configuration in the `x-weos-config`

```mermaid
flowchart LR
    Start --> intantiateGorm[Intantiate Gorm] --> setupHTTPClient[Setup HTTPClient] --> setupEventStream[Setup Event Repository] --> BaseService[/Base Service/] --> End([End]) 
```

##### Setup Paths 
Each path in the OpenAPI spec is processed and the relevant middleware, controllers are associated

```mermaid
flowchart TD
    Start --> paths[/Paths/] --> unprocessedPaths{Has\nUnprocessed\nPaths}
    unprocessedPaths -->|Yes|unprocessedOperations{Has\nUnprocessed\nOperations}
    unprocessedOperations -->|Yes|unprocessedMiddleware{Has\nUnprocessed\nMiddleware}
    unprocessedMiddleware -->|Yes|getMiddleware[Get Middleare]-->middlewareExists{Middleware Exists}
    middlewareExists-->|No|controllerSpecified{Controller Specified}
    middlewareExists-->|Yes|addToPath[Add to Path Middlware List]-->controllerSpecified
    unprocessedMiddleware -->|No|controllerSpecified
    controllerSpecified-->|Yes|getController{Controller\nValid}
    getController-->|Yes|setController
    controllerSpecified-->|No|canUseStandardController{Has\nStandard\nController}
    canUseStandardController-->|Yes|standardController[/Standard Controller/]-->setController
    canUseStandardController-->|No|setController
    setController-->hasController{Has\nController}
    hasController-->|Yes|configureEchoRoute[Configure Echo Route]-->configureCORS[Configure CORS]-->echoInstance[/echo instance/]-->End([End])
    hasController-->|No|logError[Log Error]-->configureCORS
    
    
```