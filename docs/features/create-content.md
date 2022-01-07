---
layout: default
title: Create content
parent: Features
---
# Create content

## Background

**Given** a developer "Sojourner"  
**And** "Sojourner" has an account with id "1234"  
**And** "Open API 3.0" is used to model the service  
**And** the specification is  

```
openapi: 3.0.3
info:
  title: Blog Aggregator Rest API
  version: 0.1.0
  description: REST API for interacting with the Blog Aggregator
components:
  schemas:
    Blog:
      type: object
      properties:
        title:
          type: string
          description: blog title
        description:
          type: string
      required:
        - title
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
          format: date-time
        views:
          type: integer
        categories:
          type: array
          items:
            $ref: "#/components/schemas/Post"
      required:
        - title
    Category:
      type: object
      properties:
        title:
          type: string
        description:
          type: string
      required:
        - title
paths:
  /:
    get:
      operationId: Homepage
      responses:
        200:
          description: Application Homepage
  /blog:
    post:
      operationId: Add Blog
      requestBody:
        description: Blog info that is submitted
        required: true
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/Blog"
          application/x-www-form-urlencoded:
            schema:
              $ref: "#/components/schemas/Blog"
          application/xml:
            schema:
              $ref: "#/components/schemas/Blog"
      responses:
        201:
          description: Add Blog to Aggregator
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Blog"
        400:
          description: Invalid blog submitted
```

## Scenarios

### Create a basic item

**Given** "Sojourner" is on the "Blog" create screen  
**And** "Sojourner" enters "Some Blog" in the "title" field  
**And** "Sojourner" enters "Some Description" in the "description" field  
**When** the "Blog" is submitted  
**Then** the "Blog" is created  

| title     | description      |
|:----------|:-----------------|
| Some Blog | Some Description |

**And** the "Blog" should have an id.  

### Create an item that has an invalid type

**Given** "Sojourner" is on the "Blog" create screen  
**And** "Sojourner" enters "Some Description" in the "publishedDate" field  
**When** the "Blog" is submitted  
**Then** an error should be returned.  

### Create an item that violates uniqueness requirement


### Create an item using an endpoint that does not definne the response

