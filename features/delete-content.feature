@WEOS-1131
Feature: Delete content

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
               nullable: true
             lastUpdated:
               type: string
               format: date-time
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
                 $ref: "#/components/schemas/Post"
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
             - in: header
               name: If-Match
               schema:
                 type: string
           requestBody:
             description: Blog info that is submitted
             required: false
             content:
               application/json:
                 schema:
                   $ref: "#/components/schemas/Blog"
           summary: Delete blog
           operationId: Delete Blog
           responses:
             200:
               description: Blog Deleted
     """
    And blogs in the api
      | id    | weos_id                     | sequence_no | title        | description    |
      | 1     | 29qbA6UcNUCbJm9qio8A17XBezK | 2           | Blog 1       | Some Blog      |
      | 2     | 29qbA2kSPfGdcSj0aPghsEZxFA4 | 1           | Blog 2       | Some Blog 2    |
      | 164   | 29qbA1jn7ANgvSEElCAkWi9V4hT | 1           | Blog 6       | Some Blog 6    |
      | 3     | 29qbA2Z4cOoJgri7AGI3IUWhaPp | 1           | Blog 3       | Some Blog 3    |
      | 4     | 29qbA5Xj3HvhaKwtIcxW5SnUFc1 | 1           | Blog 4       | Some Blog 4    |
      | 5     | 29qbA5ZnaFcWT77GcSpAAlPPNOJ | 1           | Blog 5       | Some Blog 5    |
      | 890   | 29qbA2i7BdjjmuRtk2fd7iPxQEq | 1           | Blog 7       | Some Blog 7    |
      | 1237  | 29qbA2hp0RS7mVb0YmAkj9HXiPS | 1           | Blog 8       | Some Blog 8    |
    And the service is running

   Scenario: Delete item based on id

     Delete an item

     Given "Sojourner" is on the "Blog" delete screen with id "1"
     When the "Blog" is submitted
     Then a 200 response should be returned
     And the "ETag" header should be "<Generated ID>.3"
     And the "Blog" "1" should be deleted

   Scenario: Delete item using entity id

     Given "Sojourner" is on the "Blog" delete screen with entity id "<Generated ID>" for blog with id "1"
     When the "Blog" is submitted
     Then a 200 response should be returned
     And the "ETag" header should be "<Generated ID>.3"
     And the "Blog" "1" should be deleted

   Scenario: Delete stale item

     If you try to delete an item and it has already been updated since since the last time the client got an updated
     version then an error is returned. This requires using the "If-Match" header

     Given "Sojourner" is on the "Blog" delete screen with id "1"
     And a header "If-Match" with value "29qbA6UcNUCbJm9qio8A17XBezK.1"
     When the "Blog" is submitted
     Then a 412 response should be returned
