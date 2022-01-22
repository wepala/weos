@WEOS-1308 @skipped
Feature: Add data to request context via the spec

  A developer can hardcode data that should be in the request context of an api

  Background:

    Given a developer "Sojourner"
    And "Sojourner" has an account with id "1234"
    And "OpenAPI 3.0" is used to model the service

  Scenario: Add basic key value to context on endpoint

    The x-content extension should be used to add data to the request context

    Given the specification is
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
        driver: sqlite3
        database: e2e.db
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
    paths:
     /:
       get:
         operationId: Homepage
         responses:
           200:
             description: Application Homepage
     /blog:
       get:
         operationId: Get Blogs
         summary: Get List of Blogs
         x-context:
           page: 1
           limit: 10
         responses:
           200:
             description: List of blogs
             content:
               application/json:
                 schema:
                   type: object
                   properties:
                     total:
                       type: integer
                     page:
                       type: integer
                     items:
                       type: array
                       items:
                         $ref: "#/components/schemas/Blog"
    """
    And the service is running
    And "Sojourner" is on the "Blog" list screen
    When the search button is hit
    Then there should be a key "page" in the request context with value "1"
    And there should be a key "limit" in the request context with value "10"


  Scenario: Add value to context that is also declared as a parameter

    If there is a parameter with the same name as a hardcoded value in the context then the incoming parameter value
    takes precedence

    Given the specification is
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
        driver: sqlite3
        database: e2e.db
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
    paths:
     /blogs/{id}:
       get:
         parameters:
           - in: path
             name: id
             schema:
               type: string
             required: true
             description: blog id
           - in: query
             name: sequence_no
             schema:
               type: string
         x-content:
           id: 2
         summary: Get Blog by id
         operationId: Get Blog
         responses:
           200:
             description: Blog details without any supporting collections
             content:
               application/json:
                 schema:
                   $ref: "#/components/schemas/Blog"
    """
    And the service is running
    And "Sojourner" is on the "Blog" edit screen with id "1234"
    When the "Blog" is submitted
    Then there should be a key "id" in the request context with value "1234"