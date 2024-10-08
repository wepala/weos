@WEOS-1130
Feature: Create content

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
               nullable: true
             posts:
               type: array
               nullable: true
               items:
                 $ref: "#/components/schemas/Post"
           required:
             - title
           x-identifier:
             - id
        Post:
          type: object
          properties:
            id:
              type: string
              format: ksuid
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
          x-identifier:
            - id
        Category:
          type: object
          properties:
            title:
              type: string
            description:
              type: string
              nullable: true
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
      /post:
        post:
          operationId: Add Post
          requestBody:
            description: Blog info that is submitted
            required: true
            content:
              application/json:
                schema:
                  $ref: "#/components/schemas/Post"
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
      And blogs in the api
        | id    | weos_id                     | sequence_no | title        | description    |
        | 1     | 24Kj3zfpocMlmFNV2KwkFfP2bgf | 1           | Blog 1       | Some Blog      |
        | 2     | 24Kj7ExtIFvuGgTOTLBgpZgCl0n | 1           | Blog 2       | Some Blog 2    |
      And the service is running
    
    Scenario: Create a basic item

      This is creating a basic item
      
      Given "Sojourner" is on the "Blog" create screen
      And "Sojourner" enters "3" in the "id" field
      And "Sojourner" enters "Some Blog" in the "title" field
      And "Sojourner" enters "Some Description" in the "description" field
      When the "Blog" is submitted
      Then the "Blog" is created
        | id    | title          | description                       |
        | 3     | Some Blog      | Some Description                  |
      And the "Blog" should have an id
      And the "ETag" header should be "<Generated ID>.1"

    Scenario: Create an item that has an invalid type

      The string format in the spec should be used to validate the field

      Given "Sojourner" is on the "Blog" create screen
      And "Sojourner" enters "Some Description" in the "publishedDate" field
      When the "Blog" is submitted
      Then an error should be returned

    Scenario: Create an item that is missing a required field

      Fields marked as required should be passed through

      Given "Sojourner" is on the "Blog" create screen
      And "Sojourner" enters "Some Description" in the "description" field
      When the "Blog" is submitted
      Then an error should be returned

    @WEOS-1289
    Scenario: Create an item using post data

      If form data is sent to the request body it is converted to json so the same commands could be used

      Given "Sojourner" is on the "Post" create screen
      And "Sojourner" enters "Some Post" in the "title" field
      And "Sojourner" enters "Some Description" in the "description" field
      And "Sojourner" enters "1" in the "blog_id" field
      When the "Post" form is submitted with content type "application/x-www-form-urlencoded"
      Then the "Post" is created
        | title          | description                       |
        | Some Post      | Some Description                  |
      And the "Post" should have an id
      And the "ETag" header should be present

    @WEOS-1289
    Scenario: Create an item using post data using the multipart content type

      Given "Sojourner" is on the "Category" create screen
      And "Sojourner" enters "Some Category" in the "title" field
      When the "Category" form is submitted with content type "multipart/form-data"
      Then the "Category" is created
        | title          |
        | Some Category  |
      And the "Category" should have an id
      And the "ETag" header should be present

    @WEOS-1289
    Scenario: Try to create item with content type that is not defined

      If the content type is not explicity defined then an error is returned (e.g. if json is not specified on the request then a json body should not be allowed)

      Given "Sojourner" is on the "Post" create screen
      And "Sojourner" enters "Some Post" in the "title" field
      And "Sojourner" enters "Some Description" in the "description" field
      And "Sojourner" enters "1" in the "blog_id" field
      When the "Post" is submitted without content type
      Then an error should be returned

    @WEOS-1294
    Scenario: Create item and related items

      If an item has one to many relationships or many to many relationships those connections can be established by
      passing in the info for the item so the relationship can be established

      Given "Sojourner" is on the "Blog" create screen
      And "Sojourner" enters "Some Blog" in the "title" field
      And "Sojourner" enters "Some Description" in the "description" field
      And "Sojourner" adds an item "Post" to "posts"
      And "Sojourner" enters "Some Post" in the "title" field of "Post"
      And "Sojourner" enters "Some Description" in the "description" field of "Post"
      When the "Blog" is submitted
      Then the "Blog" is created
        | title          | description                       |
        | Some Blog      | Some Description                  |
      And the "Blog" should have an id
      And the "Blog" should have a property "posts" with 1 items
      And the "ETag" header should be present

    @WEOS-1294
    Scenario: Create item and associate with an existing item

      If an item has one to many relationships or many to many relationships those connections can be established by
      passing in the identity of the related item

      Given "Sojourner" is on the "Post" create screen
      And "Sojourner" enters "Some Post" in the "title" field
      And "Sojourner" enters "Some Description" in the "description" field
      And "Sojourner" sets item "Blog" to "blog"
      And "Sojourner" enters "1" in the "id" field of "Blog"
      And "Sojourner" enters "Blog 1" in the "title" field of "Blog"
      When the "Post" is submitted
      Then the "Post" is created
        | title          | description                       |
        | Some Post      | Some Description                  |
      And the "Post" should have an id
      And the "ETag" header should be present

    @WEOS-1294 @skipped
    Scenario: Create item with related item and the item is invalid

      If the related item is invalid then an error should be returned and the parent and related items should NOT be created

      Given "Sojourner" is on the "Blog" create screen
      And "Sojourner" enters "Some Blog" in the "title" field
      And "Sojourner" enters "Some Description" in the "description" field
      And "Sojourner" adds an item "Post" to "posts"
      And "Sojourner" enters "Some Description" in the "description" field of "Post"
      When the "Blog" is submitted
      Then an error should be returned


    @WEOS-1382
    Scenario: Automatically generate ksuid on create

      The id for an schema is automatically generated when the identifier is a single field and there is no value for that
      field in the schema. The generation of the id should be based on the type and format of the identifier.

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
                 format: ksuid
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
      And the "OpenAPI 3.0" specification is parsed
      And "Sojourner" is on the "Blog" create screen
      And "Sojourner" enters "Some Blog" in the "title" field
      And "Sojourner" enters "Some Description" in the "description" field
      When the "Blog" is submitted
      Then the "Blog" is created
        | id               | title          | description                       |
        | <Generated>      | Some Blog      | Some Description                  |
      And the "Blog" should have an id
      And the "Blog" id should be a "ksuid"


  @WEOS-1382
  Scenario: Automatically generate uuid on create

    If the id of a schema is a string and the format uuid is specified then generate a uuid


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
               format: uuid
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
    And the "OpenAPI 3.0" specification is parsed
    And "Sojourner" is on the "Blog" create screen
    And "Sojourner" enters "Some Blog" in the "title" field
    And "Sojourner" enters "Some Description" in the "description" field
    When the "Blog" is submitted
    Then the "Blog" is created
      | id           | title          | description                       |
      | <Generated>  | Some Blog      | Some Description                  |
    And the "Blog" should have an id
    And the "Blog" id should be a "uuid"

  @WEOS-1382
  Scenario: Automatically generate id on create

    If the id of a schema is an integer then use the auto increment functionality of the supporting database to increment
    the integer.


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
             custom_id:
               type: string
               format: ksuid
             title:
               type: string
               description: blog title
             description:
               type: string
               nullable: true
           required:
             - title
           x-identifier:
             - custom_id
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
    And the "OpenAPI 3.0" specification is parsed
    And "Sojourner" is on the "Blog" create screen
    And "Sojourner" enters "Some Blog" in the "title" field
    And "Sojourner" enters "Some Description" in the "description" field
    When the "Blog" is submitted
    Then the "Blog" is created
      | custom_id             | title          | description                       |
      | <Generated>    | Some Blog      | Some Description                  |
    And the "Blog" should have an id
    And the "Blog" id should be a "integer"

