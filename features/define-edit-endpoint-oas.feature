@WEOS-1177
Feature: Edit content endpoints

  As developer you can create an endpoint that should be used to edit content of a specific type. The HTTP method and the
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
    And blogs in the api
    | id    | title        | description    |
    | 1234  | Blog 1       | Some Blog      |
    | 4567  | Blog 2       | Some Blog 2    |


  Scenario: Create a basic edit endpoint

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
    /blog/{id}:
      put:
        operationId: Edit Blog
        parameters:
          - in: path
            name: id
            schema:
              type: string
            required: true
            description: blog id
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
            description: Update blog
            content:
              application/json:
                schema:
                  $ref: "#/components/schemas/Blog"
          400:
            description: Invalid blog submitted
            content:
              application/json:
                schema:
                  $ref: "#/components/schemas/ErrorResponse"
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a "PUT" route should be added to the api
    And a "edit" middleware should be added to the route

  Scenario: Setup path without content type

    Specifying a content type in the path definition is necessary. If no content type is associated with the endpoint
    then the middleware will not be associated

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
    /blog/{id}:
      put:
        operationId: Edit Blog
        parameters:
          - in: path
            name: id
            schema:
              type: string
            required: true
            description: blog id
        responses:
          201:
            description: Add Blog to Aggregator
            content:
              application/json:
                schema:
                  $ref: "#/components/schemas/Blog"
          400:
            description: Invalid blog submitted
            content:
              application/json:
                schema:
                  $ref: "#/components/schemas/ErrorResponse"
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a warning should be output to logs letting the developer know that a handler needs to be set

  Scenario: Setup path where the request body does not reference schema

  In order to use the create handler a schema is needed so that validation etc could be setup.

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
    /blog/{id}:
      put:
        operationId: Add Blog
        parameters:
          - in: path
            name: id
            schema:
              type: string
            required: true
            description: blog id
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
            content:
              application/json:
                schema:
                  $ref: "#/components/schemas/Blog"
          400:
            description: Invalid blog submitted
            content:
              application/json:
                schema:
                  $ref: "#/components/schemas/ErrorResponse"
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a warning should be output to logs letting the developer know that a handler needs to be set