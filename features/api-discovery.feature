@WEOS-1127
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
    x-weos-config:
      logger:
        level: warn
        report-caller: true
        formatter: json
      database:
        database: "%s"
        driver: "%s"
        host: "%s"
        password: "%s"
        username: "%s"
        port: %d
      event-source:
        - title: default
          driver: service
          endpoint: https://prod1.weos.sh/events/v1
        - title: event
          driver: sqlite3
          database: e2e.db
      databases:
        - title: default
          driver: sqlite3
          database: e2e.db
      rest:
        middleware:
          - RequestID
          - Recover
          - ZapLogger
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
    When the "GET" endpoint "/api" is hit
    Then a 200 response should be returned
    And the swagger ui should be shown

  Scenario: Get the api info as json

    Developers can get the api details as a json response. This make it easier to programmatically get schema information.
    To get the json response  the response type should be set to application/json

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
                application/json:
                  schema:
                    type: string
    """
    And the "OpenAPI 3.0" specification is parsed
    And a "GET" route should be added to the api
    When the "GET" endpoint "/api" is hit
    Then a 200 response should be returned
    And the api as json should be shown