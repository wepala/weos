@WEOS-1132
Feature: Edit content

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
             title:
               type: string
               description: blog title
             description:
               type: string
             lastUpdated:
               type: string
               format: date-time
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
     And the service is running
     And blogs in the api
       | id    | weos_id        | sequence_no | title        | description    |
       | 1234  | 986888285      | 2           | Blog 1       | Some Blog      |
       | 4567  | 5uhq85nal      | 1           | Blog 2       | Some Blog 2    |

   Scenario: Edit item

     Updating an item leads to a new sequence no. being created and returned

     Given "Sojourner" is on the "Blog" edit screen with id "1234"
     And "Sojourner" enters "Some New Title" in the "title" field
     And "Sojourner" enters "Some Description" in the "description" field
     When the "Blog" is submitted
     Then a 200 response should be returned
     And the "ETag" header should be "<Generated ID>.3"
     And the "Blog" is updated
       | title          | description                       |
       | Some New Title | Some Description                  |
   
   Scenario: Update item with invalid data

     If the content type validation fails then a 422 response code should be returned (the request could have a valid
     format but the contents are invalid)

     Given "Sojourner" is on the "Blog" edit screen with id "1234"
     And "Sojourner" enters "Some New Title" in the "last_updated" field
     When the "Blog" is submitted
     Then a 422 response should be returned


   Scenario: Update stale item

     If you try to update an item and it has already been updated since since the last time the client got an updated
     version then an error is returned. This requires using the "If-Match" header

     Given "Sojourner" is on the "Blog" edit screen with id "1234"
     And "Sojourner" enters "Some New Title" in the "lastUpdated" field
     And a header "If-Match" with value "986888285.1"
     When the "Blog" is submitted
     Then a 412 response should be returned
