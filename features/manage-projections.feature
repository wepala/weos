@WEOS-1315
Feature: Manage Projections

  In WeOS a projection is a data representation of an event stream. A developer can create a projection that takes events
  as an input and stores it in a way that is easier for the application to use.

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
          x-projections:
            - Custom
            - CSV
            - Default
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
    And "Sojourner" defines a projection "Custom"
    And "Sojourner" defines a projection "CSV"
    And "Sojourner" defines an event store "Custom"

  Scenario: Set custom projection as the default projection

    Developers can set a default projection of their choosing. The default projection WeOS provides is used to setup
    a default event store. If a custom default projection is set, a default event store will also need to be configured

    Given "Sojourner" set the default projection as "Custom"
    And "Sojourner" set the default event store as "Custom"
    And the service is running
    When the "GET" endpoint "/blogs/1234" is hit
    Then the projection "Custom" is called

  Scenario: Set projections to use on a specific operation

    Developers can set multiple projections to be used on an endpoint. The projections are wrapped in a
    "MetaProjection" that has logic for co-ordinating multiple projections. On "post" all the projections are called

    Given the service is running
    And "Sojourner" is on the "Blog" create screen
    And "Sojourner" enters "3" in the "id" field
    And "Sojourner" enters "Some Blog" in the "title" field
    And "Sojourner" enters "Some Description" in the "description" field
    When the "Blog" is submitted
    Then the projection "Custom" is called
    And the projection "CSV" is called

