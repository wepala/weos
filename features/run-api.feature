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


   Scenario Outline: Create item on different platforms

     This runs the api using sqlite and then makes a create request on the API.

     Given that the "<build>" binary is generated
     And is run on the operating system "<os>" as "<mount>"
     And the binary is run with the specification
     And request body
     """
     {
       "title":"Test1",
       "description": "Lorem Ipsum",
       "url": "adsf"
      }
     """
     When the "POST" endpoint "/blog" is hit
     Then a 200 response should be returned

     Examples:
     | build                       | os                                                      | mount                      |
     | weos-linux-amd64            | ubuntu:latest                                           | /weos                      |
#     | weos-windows-4.0-amd64.exe  | mcr.microsoft.com/windows/nanoserver:ltsc2022           | /weos.exe      |