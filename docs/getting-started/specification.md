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

## Setting Up Content Types
Content Types are data models used to organize information in the API. For example, a blog API will have a "Blog" and a
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

### Setting Identifiers
You can use one (or more) of the properties you defined as an identifier for the Content Type by using the "x-identifier"
attribute. The x-identifier attribute is a list of properties that you want to use to identify an instance of the Content
Type uniquely. Each Content Type must have an identifier, so if one is not explicitly defined, WeOS will automatically
add a property "id" to the Content Type. 

### Validation
To specify basic business rules, you can use the standard OpenAPI "required" attribute on a Content Type to indicate
which properties are required. You can aslo use the "pattern" attribute on a specific property to specify a RegEx to use
for validation.

## Configuring Routes
API routes are what applications use to access data and execute functionality. The paths you specify in the [OpenAPI
specification](https://swagger.io/docs/specification/paths-and-operations/) will become endpoints to which your application can send requests. Controllers associated with the paths
receive requests and execute commands or query data. To associate a Controller with an endpoint, use the "x-controller"
extensions with the Controller name as a string.

### Standard Controllers
To make it easier for you to get started, WeOS provides standard controllers for common data functionality. Controllers
are available for:
1. Creating Content
2. View Content Details
3. Viewing a list of Content
4. Creating/Updating Content in a batch
5. Updating Content
6. Deleting Content 

Standard Controllers are automatically associated with an endpoint if a controller is not explicitly specified and the
path specification meets the conditions for one of the Standard Controllers. [Learn More About Controllers](./controllers.md)

## Configuring Databases
