Feature: Get API Details

  The OpenAPI details can be made available via the API

  Background:

    Given a developer "Sojourner"
    And "Sojourner" has an account with id "1234"
    And "Open API 3.0" is used to model the service
    And the specification is
    """
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
              content:
                application/json:
                  schema:
                    $ref: "#/components/schemas/ErrorResponse"
    """


  Scenario: View the api via the Swagger UI

    Developers can see and test the api using the [Swagger UI](https://swagger.io/tools/swagger-ui/) by setting the
    APIDiscovery handler

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
    /api:
      get:
        operationId: Get API Details
        x-controller: APIDiscovery
        responses:
          200:
            description: API Details
            content:
              application/html:
                schema:
                  type: string
    """
    And the "OpenAPI 3.0" specification is parsed
    And a "GET" route should be added to the api