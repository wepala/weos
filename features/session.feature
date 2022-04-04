@WEOS-1472
Feature: As a developer I should be able configure a session which can be used to store data

  A developer should be able to configure a session to use for storing data for retrieval

  Background:

    Given a developer "Sojourner"
    And "Sojourner" has an account with id "1234"
    And "OpenAPI 3.0" is used to model the service
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
      components:
        securitySchemes:
          cookieAuth:
            type: apiKey
            in: cookie
            name: JSESSIONID
      schemas:
        Blog:
           type: object
           properties:
             id:
               type: string
               format: ksuid
             title:
               type: string
               description: blog title
             description:
               type: string
             posts:
               type: array
               items:
                 $ref: "#/components/schemas/Post"
           required:
             - title
           x-identifier:
             - id
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
                $ref: "#/components/schemas/Category"
          required:
            - title
        Category:
          type: object
          properties:
            title:
              type: string
            description:
              type: string
    paths:
      /:
        get:
          operationId: Homepage
          responses:
            200:
              description: Application Homepage
      /blogs:
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
        get:
          operationId: Get Blogs
          summary: Get List of Blogs
          parameters:
            - in: query
              name: page
              schema:
                type: integer
            - in: query
              name: limit
              schema:
                type: integer
            - in: query
              name: _filters
                schema:
                  type: array
                  items:
                    type: object
                    properties:
                      field:
                      type: string
                      operator:
                      type: string
                    value:
                      type: array
                      items:
                        type: string
             required: false
             description: query string
          x-session:
            properties:
              oauth:
                type: string
              id:
                type: integer
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
    And blogs in the api
      | id    | entity id                   | sequence no | title        | description    |
      | 1234  | 22xu1Xa5CS3DK1Om2tB7OBDfWAF | 2           | Blog 1       | Some Blog      |
      | 4567  | 22xu4iw0bWMwxqbrUvjqEqu5dof | 1           | Blog 2       | Some Blog 2    |
    And the service is running

  Scenario: The session should be successfully created

    The session should be generated when the api is run

    Given "JSESSIONID" is the session name
    And the session should exist on the api


  Scenario: The name of the cookie should be the same name as the session

    The name specified in the yaml file for the cookie should be used to name the session

    Given "Sojourner" is making a new request
    And "JSESSIONID" is the session name
    And the value "12345" is entered in the session field "id"
    And the value "oath|qwerty" is entered in the session field "oauth"
    When the request with a cookie is sent
    Then a 200 response should be returned


  Scenario: The x-session data should be stored in the context

    x-session data should be stored in context using the field names as keys

    Given "Sojourner" is making a new request
    And "JSESSIONID" is the session name
    And the value "12345" is entered in the session field "id"
    And the value "oath|qwerty" is entered in the session field "oauth"
    When the request with a cookie is sent
    Then a 200 response should be returned
    And the context should contain x-session data

  Scenario: Retrieving an empty session from the api

    An error should be returned if the session is empty on the api

    Given "JSESSIONID1" is the session name
    And the session should exist on the api
    Then an error should be returned


  Scenario: Retrieving no data from the gorm store

    An error should be returned if the context middleware retrieves no data from the datastore

    Given "Sojourner" is making a new request
    And "JSESSIONID" is the session name
    When the request with a cookie is sent
    Then an error should be returned
