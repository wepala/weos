@WEOS-1343
Feature: Use OpenAPI Security Scheme to protect endpoints

  OpenID provides (security schemes)[https://swagger.io/docs/specification/authentication] that you can use to protect
  endpoints. There are a few security schemes available - http, apiKey, openIdConnect and oauth2.

  Background:

    Given a developer "Sojourner"
    And "Sojourner" has a valid user account
    And "Open API 3.0" is used to model the service
    And the specification is
     """
      openapi: 3.0.3
      info:
        title: Tasks API
        description: Tasks API
        version: 1.0.0
      servers:
        - url: 'http://localhost:8681'
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
      components:
        securitySchemes:
          Auth0:
            type: openIdConnect
            openIdConnectUrl: https://dev-bhjqt6zc.us.auth0.com/.well-known/openid-configuration
            x-skip-expiry-check: true
            x-jwt-map:
              user: sub
              role: azp
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
                 nullable: true
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
                nullable: true
              blog:
                $ref: "#/components/schemas/Blog"
              publishedDate:
                type: string
                format: date-time
                nullable: true
              views:
                type: integer
                nullable: true
              categories:
                type: array
                items:
                  $ref: "#/components/schemas/Category"
                nullable: true
            required:
              - title
          Category:
            type: object
            properties:
              title:
                type: string
              description:
                type: string
                nullable: true
            required:
              - title
      security:
        - Auth0: ["email"]
      paths:
        /:
          get:
            operationId: Homepage
            security: []
            responses:
              200:
                description: Application Homepage
        /blog:
          post:
            operationId: Add Blog
            parameters:
              - in: header
                name: Authorization
                schema:
                  type: string
            security: []
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
           security: []
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
        /blogs/{id}:
           get:
             parameters:
               - in: header
                 name: Authorization
                 schema:
                   type: string
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
               - in: query
                 name: use_entity_id
                 schema:
                   type: boolean
               - in: header
                 name: If-None-Match
                 schema:
                   type: string
             summary: Get Blog by id
             operationId: Get Blog
             x-auth:
               allow:
                 roles:
                   - Y9IvGucEhViFd58GL0bBoNrgEk3ohW88
             responses:
               200:
                 description: Blog details without any supporting collections
                 content:
                   application/json:
                     schema:
                       $ref: "#/components/schemas/Blog"
           put:
             parameters:
               - in: path
                 name: id
                 schema:
                   type: string
                 required: true
                 description: blog id
               - in: header
                 name: Authorization
                 schema:
                   type: string
             summary: Update blog details
             operationId: Update Blog
             requestBody:
               required: true
               content:
                 application/json:
                   schema:
                     $ref: "#/components/schemas/Blog"
             x-auth:
               allow:
                 users:
                   - auth0|1234
                   - auth0|60d0c84316f69600691c1614
               deny:
                 roles:
                   - Y9IvGucEhViFd58GL0bBoNrgEk3ohW88
             responses:
               200:
                 description: Update Blog
                 content:
                   application/json:
                     schema:
                       $ref: "#/components/schemas/Blog"
        /post:
          post:
            operationId: Add Post
            x-auth:
               deny:
                 roles:
                   - Y9IvGucEhViFd58GL0bBoNrgEk3ohW88
            requestBody:
              description: Blog info that is submitted
              required: true
              content:
                application/x-www-form-urlencoded:
                  schema:
                    $ref: "#/components/schemas/Post"
            responses:
              201:
                description: Add Blog to Aggregator
              400:
                description: Invalid blog submitted
        /category:
          post:
            operationId: Add Category
            requestBody:
              description: Category info that is submitted
              required: true
              content:
                multipart/form-data:
                  schema:
                    $ref: "#/components/schemas/Category"
            responses:
              201:
                description: Add Category
              400:
                description: Invalid Category submitted
     """
    And "Sojourner" authenticated and received a JWT
    And blogs in the api
      | id    | weos_id                     | title        | description    |
      | 1234  | 22xu1Xa5CS3DK1Om2tB7OBDfWAF | Blog 1       | Some Blog      |
    And the service is running

  Scenario: Set security globally

    If the security is set globally then that security scheme should be applied to each path

    Given "Sojourner" is on the "Blog" edit screen with id "1234"
    And "Sojourner" enters "Some Blog" in the "title" field
    And "Sojourner" enters "Some Description" in the "description" field
    When the "Blog" is submitted
    Then an 401 error should be returned

  Scenario: Turn off security on specific path when security set globally

    If security is set globally, it could be turned off on a specific path by setting the security parameter but with an
    empty array as the value

    Given "Sojourner" is on the "Blog" list screen
    And "Sojourner" authenticated and received a JWT
    And blogs in the api
      | id    | weos_id                     | title        | description    |
      | 1     | 22xu1Xa5CS3DK1Om2tB7OJDHDSF | Blog 4       | Some Blog 4    |
    And the service is running
    And the items per page are 5
    When the search button is hit
    Then a 200 response should be returned

  Scenario: Valid JWT with request on path protected by OpenID

    If the request is made with a valid JWT then the JWT is validated, the expiration checked and if all is well then the
    JWT is considered valid

    Given "Sojourner" authenticated and received a JWT
    When the "GET" endpoint "/blogs/1234" is hit
    Then a 200 response should be returned
    And a blog should be returned
      | id    | title        | description    |
      | 1234  | Blog 1       | Some Blog      |

  Scenario: Valid JWT subject stored with command events

    If a user logs in with a valid JWT then the header X-USER-ID should be set with the value in the "sub" field of the token

    Given "Sojourner" is on the "Blog" edit screen with id "1234"
    And "Sojourner" authenticated and received a JWT
    And "Sojourner" enters "Some Blog" in the "title" field
    And "Sojourner" enters "Some Description" in the "description" field
    When the "Blog" is submitted
    And the user id on the entity events should be "auth0|60d0c84316f69600691c1614"

  Scenario: Expired JWT with request on path protected by OpenID

    If the JWT is expired

  Scenario: Invalid OpenID connect url set in security scheme

    If the openIdConnectUrl set is not a valid openid connect url then a warning should be shown to the developer

    Given the specification is
     """
      openapi: 3.0.3
      info:
        title: Tasks API
        description: Tasks API
        version: 1.0.0
      servers:
        - url: 'http://localhost:8681'
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
      components:
        securitySchemes:
          Auth0:
            type: openIdConnect
            openIdConnectUrl: https://google.com
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
                 nullable: true
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
                nullable: true
              blog:
                $ref: "#/components/schemas/Blog"
              publishedDate:
                type: string
                format: date-time
                nullable: true
              views:
                type: integer
                nullable: true
              categories:
                type: array
                items:
                  $ref: "#/components/schemas/Category"
                nullable: true
            required:
              - title
          Category:
            type: object
            properties:
              title:
                type: string
              description:
                type: string
                nullable: true
            required:
              - title
      security:
        - Auth0: ["email","name"]
      paths:
        /:
          get:
            operationId: Homepage
            security: []
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
        /post:
          post:
            operationId: Add Post
            requestBody:
              description: Blog info that is submitted
              required: true
              content:
                application/x-www-form-urlencoded:
                  schema:
                    $ref: "#/components/schemas/Post"
            responses:
              201:
                description: Add Blog to Aggregator
              400:
                description: Invalid blog submitted
        /category:
          post:
            operationId: Add Category
            requestBody:
              description: Category info that is submitted
              required: true
              content:
                multipart/form-data:
                  schema:
                    $ref: "#/components/schemas/Category"
            responses:
              201:
                description: Add Category
              400:
                description: Invalid Category submitted
     """
    When the "OpenAPI 3.0" specification is parsed
    Then a warning should be shown

  Scenario: Invalid security scheme set

    If the developer references a security scheme that is not defined then an error should be shown so that the developer
    knows that security was not correctly configured.

    Given the specification is
     """
      openapi: 3.0.3
      info:
        title: Tasks API
        description: Tasks API
        version: 1.0.0
      servers:
        - url: 'http://localhost:8681'
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
      components:
        securitySchemes:
          Auth0:
            type: openIdConnect
            openIdConnectUrl: https://dev-bhjqt6zc.us.auth0.com/.well-known/openid-configuration
            x-skip-expiry-check: true
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
                 nullable: true
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
                nullable: true
              views:
                type: integer
                nullable: true
              categories:
                type: array
                items:
                  $ref: "#/components/schemas/Category"
                nullable: true
            required:
              - title
          Category:
            type: object
            properties:
              title:
                type: string
              description:
                type: string
                nullable: true
            required:
              - title
      security:
        - Foo: ["email","name"]
      paths:
        /:
          get:
            operationId: Homepage
            security: []
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
        /post:
          post:
            operationId: Add Post
            requestBody:
              description: Blog info that is submitted
              required: true
              content:
                application/x-www-form-urlencoded:
                  schema:
                    $ref: "#/components/schemas/Post"
            responses:
              201:
                description: Add Blog to Aggregator
              400:
                description: Invalid blog submitted
        /category:
          post:
            operationId: Add Category
            requestBody:
              description: Category info that is submitted
              required: true
              content:
                multipart/form-data:
                  schema:
                    $ref: "#/components/schemas/Category"
            responses:
              201:
                description: Add Category
              400:
                description: Invalid Category submitted
     """
    When the "OpenAPI 3.0" specification is parsed
    Then an error should be returned

  @WEOS-1519
  Scenario: User Denied based on id not being in the allow list

    In order to support JWT from different authentication services, the developer should be able to specify which part of
    the JWT should be used for the user id, role, organization

    Given "Sojourner" is on the "Category" create screen
    And "Sojourner" authenticated and received a JWT
    And "Sojourner" enters "3" in the "id" field
    And "Sojourner" enters "Some Blog" in the "title" field
    And "Sojourner" enters "Some Description" in the "description" field
    When the "Category" is submitted
    Then a 403 response should be returned


  @WEOS-1519
  Scenario: User Allowed based on the role being on the allow list

  In order to support JWT from different authentication services, the developer should be able to specify which part of
  the JWT should be used for the user id, role, organization

    Given "Sojourner" authenticated and received a JWT
    When the "GET" endpoint "/blogs/1234" is hit
    Then a 200 response should be returned


  @WEOS-1519
  Scenario: User denied based on the role being on the deny list

    Given "Sojourner" is on the "Post" create screen
    And "Sojourner" authenticated and received a JWT
    And "Sojourner" enters "Some New Title" in the "title" field
    And "Sojourner" enters "Some Description" in the "description" field
    When the "Post" is submitted
    Then a 403 response should be returned

  Scenario: Request with missing required scope

  Scenario: Set security on a specific path