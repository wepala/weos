@WEOS-1125
Feature: Remove field from content type

  If a field is removed from content type it should NOT remove data stored in that field. In order to permanently remove
  a field use the x-remove extension to permanently remove a field

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
             id:
               type: string
             title:
               type: string
               description: blog title
             description:
               type: string
             url:
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
           required:
             - title
           x-identifier:
             - title
         Tag:
           type: object
           properties:
             guid:
               type: string
             title:
               type: string
           required:
             - title
           x-identifier:
             - guid
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
      | id    | entity id                   | sequence no | title        | description    |
      | 1234  | 22xu1Xa5CS3DK1Om2tB7OBDfWAF | 2           | Blog 1       | Some Blog      |
      | 4567  | 22xu4iw0bWMwxqbrUvjqEqu5dof | 1           | Blog 2       | Some Blog 2    |
    And the service is running

  Scenario: Remove a field that has no data

    Because the url field has been removed it should not be returned in the response

    Given "Sojourner" removed the "url" field from the "Blog" content type
    And the service is reset
    When the "GET" endpoint "/blogs/1234" is hit
    Then a 200 response should be returned
    And a blog should be returned
      | id    | title        | description    |
      | 1234  | Blog 1       | Some Blog      |
    And a blog should be returned without field "url"

  Scenario: Remove a field that has data

    If a field that is removed is added back it should still have the contents that was there before

    Given "Sojourner" removed the "description" field from the "Blog" content type
    And the service is reset
    And the "GET" endpoint "/blogs/1234" is hit
    And a 200 response should be returned
    And a blog should be returned
      | id    | title        |
      | 1234  | Blog 1       |
    And a blog should be returned without field "description"
    And "Sojourner" adds the field "description" type "string" to the "Blog" content type
    And the service is reset
    When the "GET" endpoint "/blogs/1234" is hit
    Then a 200 response should be returned
    And a blog should be returned
      | id    | title        | description    |
      | 1234  | Blog 1       | Some Blog      |
  
  Scenario: Permanently remove a field

    In order to permanently remove a field the "x-remove" extension should be used

    Given "Sojourner" adds the "x-remove" attribute to the "description" field on the "Blog" content type
    When the service is reset
    Then the "description" field should be removed from the "Blog" table

  Scenario: Remove a field that has already been removed

    If the field was already removed (maybe because of previous run) just show a warning

    Given "Sojourner" adds the "x-remove" attribute to the "description" field on the "Blog" content type
    And the service is reset
    And the "description" field should be removed from the "Blog" table
    And the service is reset
    Then a warning should be output to the logs telling the developer the property doesn't exist
  
@skipped
  #this behaviour is not consistent across databases and after discussion, was decided would be handled later
  Scenario: Remove a field that is an identifier

    It's fine to remove an identifier

    Given "Sojourner" adds the "x-remove" attribute to the "guid" field on the "Tag" content type
    Given "Sojourner" adds the "x-remove" attribute to the "title" field on the "Tag" content type
    When the service is reset
    Then the "title" field should be removed from the "Tag" table
    And the "guid" field should be removed from the "Tag" table

  
  Scenario: Remove a field that is part of an identifier

    It's fine to remove a part of an identifier

    Given "Sojourner" adds the "x-remove" attribute to the "guid" field on the "Tag" content type
    When the service is reset
    Then the "guid" field should be removed from the "Tag" table

  Scenario: Remove a field that is part of a foreign key reference

    Given "Sojourner" adds the "x-remove" attribute to the "title" field on the "Category" content type
    When the service is reset
    Then an error should show letting the developer know that is part of a foreign key reference