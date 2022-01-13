@WEOS-1170 @long
Feature: Run API

   The API should be able to run across many platforms

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
     """


   Scenario: Run on mac intel

     Given that the "mac" binary is generated
     And the binary is run with the specification
     And request body
     """
     {
       "title: "Test1",
       "description": "Lorem Ipsum"
      }
     """
     When the "POST" endpoint "/blog" is hit
     Then a 201 response should be returned

   @linux32
   Scenario: Run on linux 32 bit

     Given that the "linux32" binary is generated
     And the binary is run with the specification
     And request body
     """
     {
       "title: "Test1",
       "description": "Lorem Ipsum"
      }
     """
     When the "POST" endpoint "/blog" is hit
     Then a 201 response should be returned


   Scenario: Run on linux 64 bit

     Given that the "linux64" binary is generated
     And the binary is run with the specification
     And request body
     """
     {
       "title: "Test1",
       "description": "Lorem Ipsum"
      }
     """
     When the "POST" endpoint "/blog" is hit
     Then a 201 response should be returned

   Scenario: Run on Windows 32 bit

     Given that the "windows32" binary is generated
     And the binary is run with the specification
     And request body
     """
     {
       "title: "Test1",
       "description": "Lorem Ipsum"
      }
     """
     When the "POST" endpoint "/blog" is hit
     Then a 201 response should be returned

   Scenario: Run on Windows 64 bit

     Given that the "widnows64" binary is generated
     And the binary is run with the specification
     And request body
     """
     {
       "title: "Test1",
       "description": "Lorem Ipsum"
      }
     """
     When the "POST" endpoint "/blog" is hit
     Then a 201 response should be returned