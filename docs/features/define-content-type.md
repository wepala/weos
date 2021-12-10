---
layout: default
title: Model API content types
parent: Features
---
# Model API content types

As a developer you can define content types for your API. Content Types have properties that can be formatted and
  validated on submission. Relationships between content types can also be setup

## Background

**Given** a developer "Sojourner"  
**And** "Sojourner" has an account with id "1234"  
**And** "OpenAPI 3.0" is used to model the service  
**And** a content type "Category" modeled in the "OpenAPI 3.0" specification  

```
  Category:
    type: object
    properties:
      title:
        type: string
      description:
        type: string
```

## Scenarios

### Declare basic content type


A simple content type is one where the properties are primitive types and it has an "id" property that is the default
    identifier

**Given** "Sojourner" adds a schema "Blog" to the "OpenAPI 3.0" specification  

```
Blog:
  type: object
  properties:
    id:
      type: string
    title:
      type: string
    description:
      type: string
```
**When** the "OpenAPI 3.0" specification is parsed  
**Then** a model "Blog" should be added to the projection  

| field name | field type | nullable | indexed |
|:-----------|:-----------|:---------|:--------|
| title      | string     | true     |         |

**And** a "Blog" entity configuration should be setup.  

### Declare a content type with the identifier explicitly declared


Identifiers are used to configure primary keys in the projection. Multiple fields can be part of the identifiers

**Given** "Sojourner" adds a schema "Blog" to the "OpenAPI 3.0" specification  

```
Blog:
  type: object
  properties:
    guid:
      type: string
    title:
      type: string
    description:
      type: string
  x-identifier:
    - guid
```
**When** the "OpenAPI 3.0" specification is parsed  
**Then** a model "Blog" should be added to the projection  

| field name | field type | nullable | indexed |
|:-----------|:-----------|:---------|:--------|
| title      | string     | true     |         |

**And** a "Blog" entity configuration should be setup.  

### Declare content type that has a relationship to another content type

**Given** "Sojourner" adds a schema "Blog" to the "OpenAPI 3.0" specification  

```
Blog:
  type: object
  properties:
    title:
      type: string
    description:
      type: string
```
**And** "Sojourner" adds a schema "Post" to the "OpenAPI 3.0" specification  

```
Post:
  type: object
  properties:
    title:
      type: string
    description:
      type: string
    blog:
      $ref: "#/components/schemas/Blog"
    publishedDate:
      type: string
    views:
      type: integer
    categories:
      type: array
      items:
        $ref: "#/components/schemas/Category"
```
**When** the "OpenAPI 3.0" specification is parsed  
**Then** a model "Blog" should be added to the projection  
**And** a model "Post" should be added to the projection  
**And** a "Blog" entity configuration should be setup.  

### Create a content type that already exists

