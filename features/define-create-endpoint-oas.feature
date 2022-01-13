
@WEOS-1164
Feature: Create content endpoints

  As developer you can create an endpoint that should be used to create content of a specific type. The HTTP method and the
  content type specified is used to infer the middleware that should be used.

  Background:

    Given a developer "Sojourner"
    And "Sojourner" has an account with id "1234"
    And "OpenAPI 3.0" is used to model the service
    And a content type "Category" modeled in the "OpenAPI 3.0" specification
    """
        Category:
          type: object
          properties:
            title:
              type: string
            description:
              type: string
    """
    And a content type "Blog" modeled in the "OpenAPI 3.0" specification
    """
        Blog:
          type: object
          properties:
            title:
              type: string
            description:
              type: string
    """
    And a content type "Post" modeled in the "OpenAPI 3.0" specification
    """
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
    """


  Scenario: Create a basic create endpoint

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
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
              headers:
                ETag:
                  schema:
                    type: string
                  description: specific version of item
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
              400:
                description: Invalid blog submitted
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a "POST" route "/blog" should be added to the api
    And a "Create" middleware should be added to the route

  Scenario: Create a batch of items

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /blog:
        post:
          operationId: Add Blogs
          requestBody:
            description: List of blogs to add
            required: true
            content:
              application/json:
                schema:
                  type: array
                  items:
                    type: "#/components/schemas/Blog"
              application/x-www-form-urlencoded:
                schema:
                  type: array
                  items:
                    type: "#/components/schemas/Blog"
              application/xml:
                schema:
                  type: array
                  items:
                    type: "#/components/schemas/Blog"
          responses:
            201:
              description: Added Blogs to Aggregator
              headers:
                ETag:
                  schema:
                    type: string
                  description: specific version of item
              content:
                application/json:
                  schema:
                    type: array
                    items:
                      type: "#/components/schemas/Blog"
                application/x-www-form-urlencoded:
                  schema:
                    type: array
                    items:
                      type: "#/components/schemas/Blog"
                application/xml:
                  schema:
                    type: array
                    items:
                      type: "#/components/schemas/Blog"
              400:
                description: Invalid blog submitted
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a "POST" route "/blog" should be added to the api
    And a "CreateBatch" middleware should be added to the route


  Scenario: Setup path without content type

    Specifying a content type in the path definition is necessary. If no content type is associated with the endpoint
    then the middleware will not be associated

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /blog:
        post:
          operationId: Add Blog
          responses:
            201:
              description: Add Blog to Aggregator
              headers:
                ETag:
                  schema:
                    type: string
                  description: specific version of item
              content:
                application/json:
                  schema:
                    $ref: "#/components/schemas/Blog"
            400:
              description: Invalid blog submitted
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a warning should be output to logs letting the developer know that a handler needs to be set

  Scenario: Setup path where the request body does not reference schema

    In order to use the create handler a schema is needed so that validation etc could be setup.

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /blog:
        post:
          operationId: Add Blog
          requestBody:
            description: Blog to add
            required: true
            content:
              application/json:
                schema:
                  type: object
                  properties:
                    id:
                      type: integer
                      description: blog id
                    title:
                      type: string
                      description: blog description
          responses:
            201:
              description: Add Blog to Aggregator
              headers:
                ETag:
                  schema:
                    type: string
                  description: specific version of item
              content:
                application/json:
                  schema:
                    type: object
                    properties:
                      id:
                        type: integer
                        description: blog id
                      title:
                        type: string
                        description: blog description
              400:
                description: Invalid blog submitted
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a warning should be output to logs letting the developer know that a handler needs to be set
    