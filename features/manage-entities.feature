@WEOS-1400
Feature: Manage Entities

  An entity is a model that is recognized by it's identifier. Ideally business logic should be contained within an entity.

  Background:

    Given a developer "Sojourner"
    And "Sojourner" has a valid user account
    And "Open API 3.0" is used to model the service
    And the specification is
    """
    openapi: 3.0.3
    info:
      title: Blog Aggregator Rest API
      version: 0.1.0
      description: REST API for interacting with the Blog Aggregator
    servers:
      - url: https://prod1.weos.sh/blog/dev
        description: WeOS Dev
      - url: https://prod1.weos.sh/blog/v1
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
      rest:
        middleware:
          - RequestID
          - Recover
          - ZapLogger
    components:
      schemas:
        Blog:
          x-entity: Blog
          type: object
          properties:
            id:
              type: string
            title:
              type: string
              description: blog title
            description:
              type: string
            required:
              - title
            x-identifier:
              - id
    paths:
      /:
        get:
          operationId: Homepage
          responses:
            200:
              description: Application Homepage
      /blogs/{id}:
        get:
          parameters:
            - in: path
              name: id
              schema:
                type: string
              required: true
              description: blog id
          summary: Get Blog by id
          operationId: Get Blog
          responses:
            200:
              description: Blog details without any supporting collections
              content:
                application/json:
                  schema:
                    $ref: "#/components/schemas/Blog"
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


  Scenario: Create a custom entity with custom business logic

  Scenario: Create entity with field types not supported by OpenAPI

  Scenario: Create entity with customized fields that are not in the specification



