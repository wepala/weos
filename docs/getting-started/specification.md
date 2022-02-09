---
layout: default
title: Specification
parent: Getting Started
nav_order: 1
has_children: false
---
# Specification
WeOS takes a design-first approach using [OpenAPI specifications](https://www.openapis.org/). WeOS uses the OpenAPI specification to set up routes
and automatically associate controllers. WeOS attempts to allow developers to get a lot done with "vanilla" OpenAPI,
although you can use OpenAPI extensions to provide additional customizations and configurations.

## Configuring basic api info 
The [OpenAPI Info Object](https://github.com/OAI/OpenAPI-Specification/blob/main/versions/3.0.3.md#infoObject) is used to
provide basic information about the api. This information can be displayed to users by using the health check standard controller
```yaml
openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
```


## Setting Up Schemas
Schemas are data models used to organize information in the API. For example, a blog API will have a "Blog" and a
"Post" concept. You define these models using [OpenAPI schemas](https://swagger.io/docs/specification/data-models/).
```yaml
 Blog:
      type: object
      properties:
        url:
          type: string
          format: uri
        title:
          type: string
        description:
          type: string
        status:
          type: string
          nullable: true
          enum:
            - null
            - unpublished
            - published
        image:
          type: string
          format: byte
        categories:
          type: array
          items:
            $ref: "#/components/schemas/Category"
        posts:
          type: array
          items:
            $ref: "#/components/schemas/Post"
        lastUpdated:
          type: string
          format: date-time
        created:
          type: string
          format: date-time
      required:
        - title
        - url
```

### Defining Properties
You can define properties using the [standard OpenAPI property syntax](https://swagger.io/docs/specification/data-models/data-types/) and types. You can also use the "string" property
type along with the "format" attribute to specify additional types (e.g., date-time). See the property specification for
a complete list of property types and formats. 

| Data Type | Description                                       | OpenAPI Data Type | Format    | Default Value | 
|:----------|:--------------------------------------------------|:------------------|:----------|:--------------|
| Integer   |                                                   | integer           |           | nil           |
| Number    | Floating point Number                             | number            |           |               |
| Boolean   | true or false only. Truthy values are not allowed | boolean           |           |               |
| String    | string                                            | string            |           |               |
| Date Time |                                                   | string            | date-time |               |
| Array     |                                                   | array             |           |               |
| Object    |                                                   |                   |           |               |

### Setting Identifiers
You can use one (or more) of the properties you defined as an identifier for the schema by using the "x-identifier"
attribute. The x-identifier attribute is a list of properties that you want to use to identify an instance of the schema
uniquely. 

```yaml
Blog:
      type: object
      properties:
        id:
          type: string
        title:
          type: string
        description:
          type: string
      x-identifier:
        - id
        - title
```
Each schema must have an identifier, so if one is not explicitly defined, WeOS will automatically
add a property "id" to the schema.
### Validation
To specify basic business rules, you can use the standard OpenAPI "required" attribute on a Content Type to indicate
which properties are required. You can also use the "pattern" attribute on a specific property to specify a RegEx to use
for validation.

## Configuring Routes
API routes are what applications use to access data and execute functionality. The paths you specify in the [OpenAPI
specification](https://swagger.io/docs/specification/paths-and-operations/) will become endpoints to which your
application can send requests. Each path can have multiple operations that you can configure separately or you can
configure a group of operations

### Parameters
You can define route parameters using OpenAPI's parameters specification. Each parameter defined is used to validate
incoming data from the request. WeOS supports header, path, and query parameters (we don't support cookie parameters at
the time of writing). You can specify a parameter on a path or a specific operation within a path. Parameters that are
defined are accessible to middleware and controllers via the request context.
#### Route Level Parameters
```yaml
paths:
  /blogs:
    parameters:
        - in: query
          name: header
          schema:
            type: integer
        - in: query
          name: page
          schema:
            type: integer
```
#### Operation Level Parameters
```yaml
paths:
  /blogs:
    get:
        parameters:
            - in: query
              name: header
              schema:
                type: integer
            - in: query
              name: page
              schema:
                type: integer
```
### Middleware 
Middleware is reusable code that can be associated with a route. Middleware can add information to the request context
that can be used by the controller. To setup middleware use the `x-middleware` extension which allows for an array of 
middleware by name. The middlware needs to be regisered with the API before it can be used. WeOS provides standard middleware that you can use. 
### Controllers
Controllers associated with the paths receive requests and execute commands or query data. To associate a Controller
with an endpoint, use the "x-controller" extension with the Controller name as a string.

#### Standard Controllers
To make it easier for you to get started, WeOS provides standard controllers for common data functionality. Controllers
are available for:

| Controller Name | Description                        | Conditions              | Required Parameters                            | Optional Parameters                                                                                                           |
|:----------------|:-----------------------------------|:------------------------|------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------|
| Create          | Create An Item                     | Schema associated with  | none                                           |                                                                                                                               |
| View            | Get the details of a specific item |                         | identifier (note this could be multiple parts) | use_entity_id (for getting item by entity id instead of by user defined id), sequence_no (get a specific version of the item) |
| List            | Get a collection of items          |                         | none                                           | page, limit, query, filters,                                                                                                  |
| CreateBatch     | Bulk create items                  |                         | none                                           |                                                                                                                               |
| Update          | Edit an item                       |                         | identifier (note this could be multiple parts) |                                                                                                                               |


Standard Controllers are automatically associated with an endpoint if a controller is not explicitly specified and the
path specification meets the conditions for one of the Standard Controllers. [Learn More About Controllers](./controllers.md)



### Route Extensions
