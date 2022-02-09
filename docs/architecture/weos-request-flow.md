# WeOS Rest Endpoint Request Flow
This shows what happens with an incoming request to an endpoint
```mermaid
flowchart LR
    Start --> runPreMiddleware[[Run Pre Middleware]] --> getRoute[[Get Route]] --> runMiddleware[[Run Middleware]] --> runController[[Run Controller]]-->End([End])
```

## Pre Middleware 
We have standard middleware that we typically use 
- RequestID - Generate request id 
- Recover - Return friendly error when there is a panic
- ZapLogger - Switches to using Zap logger instead of echo logger 

## Get Route 
Uses echo framework functionality. 

## Run Middleware
Middleware are run in the order in which they are configured on the path. 

### Context Middleware
By default a Context Middleware is set which adds values in the request based on the parameters configured on that path
in the OpenAPI spec.

```mermaid
flowchart TD
    Start --> request[/Request/] --> getAccountID[/Get Account ID/] --> hasUnprocessedParams{Has\nUnprocessed\nParams}
    hasUnprocessedParams -->|No|End([End])
    hasUnprocessedParams -->|Yes|getParamFromRequest[[Get Param From Request]]-->requestHasParam{Request\nHas\nParam}
    requestHasParam -->|Yes|addToContext[Add param and request value to context] --> End
    requestHasParam -->|No|End
```

## Run Controller
The controller that is run is either explicitly set using the `x-controller` extension or automatically configured. 
### Standard Create Controller
```mermaid
flowchart TD
    Start --> request[/Request/] --> addContentTypeToContext[Add Content Type to Context] -->|Context| contextWithContentType[/Name: Content Type\n Schema: Some Open API Schema/]
    contextWithContentType --> dispatchCommand[[Dispatch Command]] --> errorReturned{Error\nReturned}
    errorReturned-->|No|getItem[Get Item from Projection] --> createEtag[Create Etag] --> Etag[/ETag/] --> End
    errorReturned-->|Yes|returnError[Return Error]-->End
```




