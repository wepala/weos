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
       securitySchemes:
         CookieAuth:
           type: apiKey
           in: cookie
           name: JSESSIONID
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
       /blogs/{id}:
         get:
           security:
             - CookieAuth: []
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
           summary: Get Blog by id
           x-middleware:
            - Handler
           x-session:
             properties:
               oauth:
                 type: string
               ids:
                 type: integer
           operationId: Get Blog
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
           summary: Update blog details
           operationId: Update Blog
           requestBody:
             required: true
             content:
               application/json:
                 schema:
                   $ref: "#/components/schemas/Blog"
           responses:
             200:
               description: Update Blog
               content:
                 application/json:
                   schema:
                     $ref: "#/components/schemas/Blog"
         delete:
           parameters:
             - in: path
               name: id
               schema:
                 type: string
               required: true
               description: blog id
           summary: Delete blog
           operationId: Delete Blog
           responses:
             200:
               description: Blog Deleted
     """
    And blogs in the api
      | id    | weos_id                     | sequence_no | title        | description    |
      | 1     | 24Kj7ExtIFvuGgTOTLBgpZgCl0n | 1           | Blog 1       | Some Blog      |
      | 2     | 24KjDkwfmp8PCslCQ6Detx6yr1N | 1           | Blog 2       | Some Blog 2    |
    And the service is running

  @WEOS-1472
  Scenario: The session should be successfully created

    The session should be generated when the api is run

    Given "JSESSIONID" is the session name
    And the session should exist on the api

  @WEOS-1472
  Scenario: A request is made with a cookie that contains x-session details

    The name specified in the yaml file for the cookie should be used to name the session. The x-session fields should be on the session

    Given "Sojourner" is making a "GET" request on "/blogs/" with id "1"
    And "JSESSIONID" is the session name
    And "JSESSIONID" is the cookie name
    And the session should exist on the api
    And the "integer" value "12345" is entered in the session field "ids"
    And the "string" value "oath|qwerty" is entered in the session field "oauth"
    When the request with a cookie is sent
    Then a "200" response should be returned
    And the session should contain x-session data

  @WEOS-1472
  Scenario: A request is made with a cookie that is empty (no x-session details)

  An error should be returned if the context middleware retrieves no data from the datastore

    Given "Sojourner" is making a "GET" request on "/blogs/" with id "1"
    And "JSESSIONID" is the session name
    And "JSESSIONID" is the cookie name
    And the session should exist on the api
    When the request with a cookie is sent
    Then an error should be returned

  @WEOS-1472
  Scenario: A request is made with a cookie with a non-existing session name

  The name specified in the yaml file for the cookie should be used to name the session. However if a cookie is sent with the wrong name is used, an error should be returned.

    Given "Sojourner" is making a "GET" request on "/blogs/" with id "1"
    And "JSESSIONID" is the session name
    And "JSESSIONID123" is the cookie name
    And the session should exist on the api
    And the "integer" value "12345" is entered in the session field "ids"
    And the "string" value "oath|qwerty" is entered in the session field "oauth"
    When the request with a cookie is sent
    Then an error should be returned

  @WEOS-1472
  Scenario: The Session is set globally

    The cookie can be added to the global security declaration which applies it to all paths

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
       securitySchemes:
         CookieAuth:
           type: apiKey
           in: cookie
           name: JSESSIONID
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
     security:
       - CookieAuth: []
     paths:
       /blog:
         post:
           security: []
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
           summary: Get Blog by id
           x-middleware:
             - Handler
           x-session:
             properties:
               oauth:
                 type: string
               ids:
                 type: integer
           operationId: Get Blog
           responses:
             200:
               description: Blog details without any supporting collections
               content:
                 application/json:
                   schema:
                     $ref: "#/components/schemas/Blog"
     """
    And blogs in the api
      | id     | weos_id                      | sequence_no | title        | description    |
      | 11     | 24Kj7ExtIFvuGgTOTLBgpZgCl0n1 | 1           | Blog 1       | Some Blog      |
      | 21     | 24KjDkwfmp8PCslCQ6Detx6yr1N1 | 1           | Blog 2       | Some Blog 2    |
    And the service is running
    And "Sojourner" is making a "GET" request on "/blogs/" with id "1"
    And "JSESSIONID" is the session name
    And "JSESSIONID" is the cookie name
    And the session should exist on the api
    And the "integer" value "12345" is entered in the session field "ids"
    And the "string" value "oath|qwerty" is entered in the session field "oauth"
    When the request with a cookie is sent
    Then a "200" response should be returned
    And the session should contain x-session data

  @WEOS-1472
  Scenario: The Session is set globally but x-session is not provided on all paths

  The cookie can be added to the global security declaration which applies it to all paths however, x-session is needed on all paths or it should error out

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
       securitySchemes:
         CookieAuth:
           type: apiKey
           in: cookie
           name: JSESSIONID
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
     security:
       - CookieAuth: []
     paths:
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
           summary: Get Blog by id
           x-middleware:
             - Handler
           x-session:
             properties:
               oauth:
                 type: string
               ids:
                 type: integer
           operationId: Get Blog
           responses:
             200:
               description: Blog details without any supporting collections
               content:
                 application/json:
                   schema:
                     $ref: "#/components/schemas/Blog"
     """
    And blogs in the api
      | id     | weos_id                      | sequence_no | title        | description    |
      | 12     | 24Kj7ExtIFvuGgTOTLBgpZgCl0n2 | 1           | Blog 1       | Some Blog      |
      | 22     | 24KjDkwfmp8PCslCQ6Detx6yr1N2 | 1           | Blog 2       | Some Blog 2    |
    And the service is running
    Then a warning should be shown


  @WEOS-1472
  Scenario: The Session is set on an endpoint using the security tag

  The cookie can be added to the specific endpoint

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
       securitySchemes:
         CookieAuth:
           type: apiKey
           in: cookie
           name: JSESSIONID
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
           x-middleware:
             - Handler
           x-session:
             properties:
               oauth:
                 type: string
               ids:
                 type: integer
           responses:
             201:
               description: Add Blog to Aggregator
               content:
                 application/json:
                   schema:
                     $ref: "#/components/schemas/Blog"
             400:
               description: Invalid blog submitted
       /blogs/{id}:
         get:
           security:
             - CookieAuth: []
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
           summary: Get Blog by id
           x-middleware:
            - Handler
           x-session:
             properties:
               oauth:
                 type: string
               ids:
                 type: integer
           operationId: Get Blog
           responses:
             200:
               description: Blog details without any supporting collections
               content:
                 application/json:
                   schema:
                     $ref: "#/components/schemas/Blog"
     """
    And blogs in the api
      | id     | weos_id                      | sequence_no | title        | description    |
      | 13     | 24Kj7ExtIFvuGgTOTLBgpZgCl0n3 | 1           | Blog 1       | Some Blog      |
      | 23     | 24KjDkwfmp8PCslCQ6Detx6yr1N3 | 1           | Blog 2       | Some Blog 2    |
    And the service is running
    And "Sojourner" is making a "GET" request on "/blogs/" with id "1"
    And "JSESSIONID" is the session name
    And "JSESSIONID" is the cookie name
    And the session should exist on the api
    And the "integer" value "12345" is entered in the session field "ids"
    And the "string" value "oath|qwerty" is entered in the session field "oauth"
    When the request with a cookie is sent
    Then a "200" response should be returned
    And the session should contain x-session data

