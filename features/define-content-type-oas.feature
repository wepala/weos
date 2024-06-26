@WEOS-1110
Feature: Create Content Types

  As a developer you can define content types for your API. Content Types have properties that can be formatted and
  validated on submission. Relationships between content types can also be setup

  Background:

    Given a developer "Sojourner"
    And "Sojourner" has an account with id "1234"
    And "OpenAPI 3.0" is used to model the service
    And all fields are nullable by default
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

  Scenario: Declare basic content type

    A simple content type is one where the properties are primitive types. If there is no identifier specified one will
    be created by default

    Given "Sojourner" adds a schema "Blog" to the "OpenAPI 3.0" specification
    """
        Blog:
          type: object
          properties:
            title:
              type: string
              description: blog title
            description:
              type: string

    """
    When the "OpenAPI 3.0" specification is parsed
    Then a model "Blog" should be added to the projection
      | Field       | Comment      | Type           | Null     | Key      | Default     |
      | id          |              | integer        | false    | PK       | NULL        |
      | title       | blog title   | varchar(512)   | true     |          | NULL        |
      | description |              | varchar(512)   | true     |          | NULL        |
    And a "Blog" entity configuration should be setup
    """
    erDiagram
      Blog
      Blog {
        uint id
        string title
        string description
      }
    """

  Scenario: Declare a content type with the identifier explicitly declared

    Identifiers are used to configure primary keys in the projection. Multiple fields can be part of the identifiers

    Given "Sojourner" adds a schema "Blog" to the "OpenAPI 3.0" specification
    """
        Blog:
          type: object
          properties:
            guid:
              type: string
            title:
              type: string
            description:
              type: string
          x-identifier:
            - guid
            - title

    """
    When the "OpenAPI 3.0" specification is parsed
    Then a model "Blog" should be added to the projection
      | Field       | Comment      | Type           | Null     | Key      | Default     |
      | guid        |              | varchar(512)   | false    | PK       | NULL        |
      | title       | blog title   | varchar(512)   | false    | PK       | NULL        |
      | description |              | varchar(512)   | true     |          | NULL        |
    And a "Blog" entity configuration should be setup
    """
    erDiagram
      Blog
      Blog {
        string guid
        string title
        string description
      }
    """


  Scenario: Declare a content type that has required fields

  Required properies should be added to the `required` parameter as per the OpenAPI specification. Properties that are
  marked as identifiers don't need to be marked as `required`

    Given "Sojourner" adds a schema "Blog" to the "OpenAPI 3.0" specification
   """
       Blog:
         type: object
         properties:
           id:
             type: string
           title:
             type: string
           description:
             type: string
         required:
           - title
   """
    When the "OpenAPI 3.0" specification is parsed
    Then a model "Blog" should be added to the projection
      | Field       | Comment      | Type           | Null     | Key      | Default     |
      | id          |              | varchar(512)   | false    | PK       | NULL        |
      | title       | blog title   | varchar(512)   | false    |          | NULL        |
      | description |              | varchar(512)   | true     |          | NULL        |
    And a "Blog" entity configuration should be setup
  """
  erDiagram
    Blog
    Blog {
      string id
      string title
      string description
    }
  """

  Scenario: Declare content type that has a many to one relationship to another content type

    Many to one relationships is determined by what a property is referencing. If the property of a Content Type is
    referencing a single other content type then many to one relationship is inferred.

    Given "Sojourner" adds a schema "Blog" to the "OpenAPI 3.0" specification
    """
        Blog:
          type: object
          properties:
            title:
              type: string
            description:
              type: string
    """
    And "Sojourner" adds a schema "Post" to the "OpenAPI 3.0" specification
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
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a model "Blog" should be added to the projection
      | Field       | Comment      | Type           | Null     | Key      | Default     |
      | id          |              | integer        | false    | PK       | NULL        |
      | title       |              | varchar(512)   | false    |          | NULL        |
      | description |              | varchar(512)   | true     |          | NULL        |
    And a model "Post" should be added to the projection
      | Field       | Comment      | Type           | Null     | Key      | Default     |
      | id          |              | integer        | false    | PK       | NULL        |
      | title       |              | varchar(512)   | false    |          | NULL        |
      | description |              | varchar(512)   | true     |          | NULL        |
      | blog_id     |              | integer        | true     | FK       | NULL        |
    And a "Blog" entity configuration should be setup
    """
    erDiagram
      Blog
      Blog {
        uint id
        string title
        string description
      }
    """
    
  Scenario: Declare content type that has a many to one relationship to another content type with a multipart identifier

    A content type could be associated with another content type that has an identifier that has multiple parts. Though
    it's one field that is mapped, the data store would need to accommodate the parts of the identifier for the mapped
    content type

    Given "Sojourner" adds a schema "Blog" to the "OpenAPI 3.0" specification
    """
        Blog:
          type: object
          properties:
            guid:
              type: string
            title:
              type: string
            description:
              type: string
          x-identifier:
            - guid
            - title

    """
    And "Sojourner" adds a schema "Post" to the "OpenAPI 3.0" specification
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
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a model "Blog" should be added to the projection
      | Field       | Comment      | Type           | Null     | Key      | Default     |
      | guid        |              | varchar(512)   | false    | PK       | NULL        |
      | title       |              | varchar(512)   | false    |          | NULL        |
      | description |              | varchar(512)   | true     |          | NULL        |
    And a model "Post" should be added to the projection
      | Field       | Comment      | Type           | Null     | Key      | Default     |
      | id          |              | integer        | false    | PK       | NULL        |
      | title       |              | varchar(512)   | false    |          | NULL        |
      | description |              | varchar(512)   | true     |          | NULL        |
      | blog_guid   |              | varchar(512)   | true     | FK       | NULL        |
      | blog_title  |              | varchar(512)   | true     | FK       | NULL        |
    And a "Post" entity configuration should be setup
    """
    erDiagram
      Post {
        uint id
        string title
        string description
        string blogGuid
        string blogTitle
      }
    """

  Scenario: Declare content type that has a many to many relationship to another content type

    Given "Sojourner" adds a schema "Blog" to the "OpenAPI 3.0" specification
    """
        Blog:
          type: object
          properties:
            title:
              type: string
            description:
              type: string

    """
    And "Sojourner" adds a schema "Post" to the "OpenAPI 3.0" specification
    """
        Post:
          type: object
          properties:
            title:
              type: string
            description:
              type: string
            publishedDate:
              type: string
            views:
              type: integer
            categories:
              type: array
              items:
                $ref: "#/components/schemas/Category"
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a model "Post" should be added to the projection
      | Field       | Comment      | Type           | Null     | Key      | Default     |
      | id          |              | integer        | false    | PK       | NULL        |
      | title       |              | varchar(512)   | true     |          | NULL        |
      | description |              | varchar(512)   | true     |          | NULL        |
    And a model "PostCategories" should be added to the projection
      | Field       | Comment      | Type           | Null     | Key      | Default     |
      | id          |              | integer        | false    | PK       | NULL        |
      | category_id |              | integer        | false    | PK       | NULL        |
    And a "Post" entity configuration should be setup
    """
    erDiagram
      Blog ||--o{ Post : contains
      Blog {
        uint id
        string title
        string description
      }
      Category ||--o{ Post : contains
      Post {
        uint id
        string title
        string description
      }
    """

  Scenario: Use format to set granular types

    Developers can use the `format` attribute to set the format of a property. This should be used to validate content
    using common formats (e.g. email)

    Given "Sojourner" adds a schema "Post" to the "OpenAPI 3.0" specification
    """
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
            email:
              type: string
              format: email
            publishedDate:
              type: string
              format: date-time
            views:
              type: integer
            categories:
              type: array
              items:
                $ref: "#/components/schemas/Category"
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a model "Post" should be added to the projection
      | Field          | Comment      | Type           | Null     | Key      | Default     |
      | id             |              | varchar(512)   | false    | PK       | NULL        |
      | title          |              | varchar(512)   | true     |          | NULL        |
      | description    |              | varchar(512)   | true     |          | NULL        |
      | email          |              | varchar(512)   | true     |          | NULL        |
      | published_date |              | datetime       | true     |          | NULL        |
    And a model "PostCategories" should be added to the projection
      | Field       | Comment      | Type           | Null     | Key      | Default     |
      | id          |              | varchar(512)   | false    | PK       | NULL        |
      | category_id |              | integer        | false    | PK       | NULL        |
    And a "Post" entity configuration should be setup
    """
    erDiagram
      Post {
        string id
        string title
        string description
        string email
        datetime publishedDate
      }
    """

  @skipped
  Scenario: Setup validation rules for content

    Developers can also use Regex to define content validation for a field

    Given "Sojourner" adds a schema "Post" to the "OpenAPI 3.0" specification
    """
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
            email:
              type: string
              pattern: '^[a-zA-Z0-9.!#$%&*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$'
            publishedDate:
              type: string
              format: date-time
            views:
              type: integer
            categories:
              type: array
              items:
                $ref: "#/components/schemas/Category"
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a model "Post" should be added to the projection
      | Field         | Comment      | Type           | Null     | Key      | Default     |
      | id            |              | varchar(512)   | false    | PK       | NULL        |
      | title         |              | varchar(512)   | true     |          | NULL        |
      | description   |              | varchar(512)   | true     |          | NULL        |
      | email         |              | varchar(512)   | true     |          | NULL        |
      | publishedDate |              | datetime       | true     |          | NULL        |
      | views         |              | int            | true     |          | NULL        |
    And a model "PostCategory" should be added to the projection
      | Field       | Comment      | Type           | Null     | Key      | Default     |
      | post_id     |              | varchar(512)   | false    | PK       | NULL        |
      | category_id |              | varchar(512)   | false    | PK       | NULL        |

  @WEOS-1116
  Scenario: Setup a content type with an enumeration

    A property can be defined with a list of possible options. Null is added by default since in WeOS properties are nullable
    by default

    Given "Sojourner" adds a schema "Post" to the "OpenAPI 3.0" specification
    """
        Post:
          type: object
          properties:
            id:
              type: string
            title:
              type: string
            description:
              type: string
            status:
              type: string
              enum:
                - unpublished
                - published
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a model "Post" should be added to the projection
      | Field         | Comment      | Type           | Null     | Key      | Default     |
      | id            |              | varchar(512)   | false    | PK       | NULL        |
      | title         |              | varchar(512)   | true     |          | NULL        |
      | description   |              | varchar(512)   | true     |          | NULL        |
      | status        |              | varchar(512)   | false    |          | unpublished |

  @WEOS-1116
  Scenario: Setup a content type with an enumeration that is an integer

    The column should match the content type of the property. To signal that the property could be omitted use the "null" option

    Given "Sojourner" adds a schema "Post" to the "OpenAPI 3.0" specification
    """
        Post:
          type: object
          properties:
            id:
              type: string
            title:
              type: string
            description:
              type: string
            status:
              type: integer
              nullable: true
              enum:
                - "null"
                - 0
                - 1
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a model "Post" should be added to the projection
      | Field         | Comment      | Type           | Null     | Key      | Default     |
      | id            |              | varchar(512)   | false    | PK       | NULL        |
      | title         |              | varchar(512)   | true     |          | NULL        |
      | description   |              | varchar(512)   | true     |          | NULL        |
      | status        |              | integer        | false    |          | unpublished |

  @focus-1116
  Scenario: Setup a content type with an enumeration but the options don't match the type

    The options should match the type of the property i.e if the type is an integer then the options should be integers.

    Given "Sojourner" adds a schema "Post" to the "OpenAPI 3.0" specification
    """
        Post:
          type: object
          properties:
            id:
              type: string
            title:
              type: string
            description:
              type: string
            status:
              type: integer
              enum:
                - unpublished
                - published
    """
    When the "OpenAPI 3.0" specification is parsed
    Then an error should be returned on running to show that the enum values are invalid


  Scenario: Create a content type that already exists

    Given "Sojourner" adds a schema "Blog" to the "OpenAPI 3.0" specification
    """
        Category:
            type: object
            properties:
              title:
                type: string
              summary:
                type: string
    """
    When the "OpenAPI 3.0" specification is parsed
    Then an error should be returned

  @WEOS-1342
  Scenario: Create field on a schema that should only be updated on create

    Developers can indicate in the schema the list of operations that would trigger an update on a date field using the
    x-update extension. The values are the operationIds of the operations that should trigger the update in value. By
    updating

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
             created:
               type: string
               format: date-time
               x-update:
                 - Add Blog
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
             - in: header
               name: If-Match
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
      | id    | weos_id        | sequence_no | title        | description    |
      | 1234  | 986888285      | 1           | Blog 1       | Some Blog      |
      | 4567  | 5uhq85nal      | 1           | Blog 2       | Some Blog 2    |
    And the service is running
    And "Sojourner" is on the "Blog" create screen
    And "Sojourner" enters "3" in the "id" field
    And "Sojourner" enters "Some Blog" in the "title" field
    And "Sojourner" enters "Some Description" in the "description" field
    When the "Blog" is submitted
    Then the "Blog" is created
      | title          | description                       |
      | Some Blog      | Some Description                  |
    And the "created" field should have today's date

  @WEOS-1342
  Scenario: Updated field on a schema that should updated when the entity is updated

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
               nullable: true
             updated:
               type: string
               format: date-time
               nullable: true
               x-update:
                 - Update Blog
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
             - in: header
               name: If-Match
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
      | id    | weos_id        | sequence_no | title        | description    |
      | 1234  | 986888285      | 1           | Blog 1       | Some Blog      |
      | 4567  | 5uhq85nal      | 1           | Blog 2       | Some Blog 2    |
    And the service is running
    And "Sojourner" is on the "Blog" edit screen with id "1234"
    And "Sojourner" enters "Some New Title" in the "title" field
    And "Sojourner" enters "Some Description" in the "description" field
    When the "Blog" is submitted
    Then a 200 response should be returned
    And the "Blog" is updated
      | title          | description                       |
      | Some New Title | Some Description                  |
    And the "updated" field should have today's date

  @WEOS-1342
  Scenario: Reference invalid operation id

    If an invalid operation id is referenced then return an error to the developer when starting up the API

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
             updated:
               type: string
               format: date-time
               x-update:
                 - asdfasdf asdf asfd
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
             - in: header
               name: If-Match
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
    When the service is running
    Then an error is returned