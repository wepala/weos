# API Startup Flow Chart

## Main entrypoint 

The OpenAPI spec can be passed to the CLI via environment variable `WEOS_SCHEMA` or as a command line argument `schema`. 
The value for the OpenAPI spec could be the contents of a valid spec or the filepath to the specification



### Instantiate API

The api is instantiated calling api.New(). The openapi spec is passed to the api.New() function as a file path or as a string.
The `api.New()` function instantiates the echo framework and then initializes the api. An instance of the api is returned
and the api instance is essentially a service container that can be used to access other services.


```mermaid
flowchart TD
    Start --> newEcho[Instantiate Echo] --> initialize[[Initialize API]] --> initResponse{Successfully\nInitialized}
    initResponse -->|Yes| run([API Running])
    initResponse -->|No| logError[Log Error] --> fail([API Not Running])
```

For a quick start you can use `api.Start()` instead of `api.New()`


### Initialize API

During the initialization of the api the following steps are performed:
1. Setup default logger, http client in service container
2. Register standard controllers 
3. Register standard middleware
4. Register standard global initializers
5. Register standard operation initializers
6. Setup the default command dispatcher
7. Setup global middleware
8. Run global initializers
9. Run path initializers
10. Run operation initializers

```mermaid
flowchart TD
    Start --> setupDefaultServices[Set Logger, Http Client] --> registerBasicControllers[Setup Default Controllers] --> registerMiddleawre[Register Middleware]
    registerMiddleawre --> registerGlobalInitializers[Register Global Initializers] --> registerPathInitializers[Register Path Initializers] --> registerOperationInitializers[Register Operation Initializers] --> registerCommandDispatcher[Register Command Dispatcher] --> registerGlobalMiddleware[Register Global Middleware] --> runGlobalInitializers[Run Global Initializers] --> runPathInitializers[Run Path Initializers] --> runOperationInitializers[Run Operation Initializers] --> runGlobalMiddleware[Run Global Middleware] --> runPathMiddleware[Run Path Middleware] --> runOperationMiddleware[Run Operation Middleware] --> runOperation[Run Operation] --> End
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