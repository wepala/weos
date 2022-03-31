Feature: Upload file

  Configure file upload by setting up an endpoint to receive files

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
             banner:
               type: string
               format: binary
               x-upload:
                 folder: ./files
                 limit: 10000
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
           required:
             - title
     paths:
       /:
         get:
           operationId: Homepage
           responses:
             200:
               description: Application Homepage
       /files:
         get:
           operationId: getFiles
           responses:
             200:
               description: Files
               x-folder: "./files"
         post:
           operationId: uploadFile
           requestBody:
             content:
               multipart/form-data:
                 schema:
                   type: string
                   format: binary
                   x-upload:
                      folder: ./files
                      limit: 10000
           responses:
             201:
               description: File successfully uploaded


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
               multipart/form-data:
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

  @WEOS-1378
    Scenario: Upload file using an upload path

      You can configure an endpoint to receive a file and move it to a folder on the same machine that the service is
      running on

      Given "Sojourner" is on page that has a file input
      And the folder "./files" exists
      And "Sojourner" selects the file
      | title            | path                                       |
      | test             | ./controllers/rest/fixtures/files/test.csv |
      When the file is uploaded to "/files"
      Then the file should be available at "/files/test.csv"

  @WEOS-1378
    Scenario: Upload file as a property on a schema

      You can configure a property on a schema to be a file upload.

      Given "Sojourner" is on the "Blog" create screen
      And "Sojourner" enters "3" in the "id" field
      And "Sojourner" enters "Some Blog" in the "title" field
      And "Sojourner" enters "Some Description" in the "description" field
      And "Sojourner" selects a file for the "banner" field
        | title            | path                      |
        | test             | ./controllers/rest/fixtures/files/test2.csv |
      When the "Blog" form is submitted with content type "multipart/form-data"
      Then the "Blog" is created
        | id    | title          | description                       | banner        |
        | 3     | Some Blog      | Some Description                  | test2.csv      |


  @WEOS-1378
    Scenario: Upload file that is above the default file size limit

      The default file size limit is 1MB

      Given "Sojourner" is on page that has a file input
      And the folder "./files" exists
      And "Sojourner" selects the file
        | title            | path                      |
        | test             | ./controllers/rest/fixtures/files/test20.csv |
      And the file is "1"mb
      When the file is uploaded to "/files"
      Then an error should be returned

  @WEOS-1378
    Scenario: Upload file that is above the user defined limit

      The developer could set the file limit in bytes

      Given "Sojourner" is on page that has a file input
      And the folder "./files" exists
      And "Sojourner" selects the file
        | title            | path                      |
        | test             | ./controllers/rest/fixtures/files/test20.csv |
      And the file is "1"mb
      When the file is uploaded to "/files"
      Then an error should be returned
