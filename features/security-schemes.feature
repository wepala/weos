Feature: Use OpenAPI Security Scheme to protect endpoints

  OpenID provides (security schemes)[https://swagger.io/docs/specification/authentication] that you can use to protect
  endpoints. There are a few security schemes available - http, apiKey, openIdConnect and oauth2.

  Background:

    Given a developer "Sojourner"
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
        database:
          driver: sqlite3
          database: e2e.db
      components:
        securitySchemes:
          Auth0:
            type: openIdConnect
            openIdConnectUrl: https://samples.auth0.com/.well-known/openid-configuration
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
    And the service is running


  Scenario: Set security globally

    If the security is set globally then that security scheme should be applied to each path

    Given "Sojourner" is on the "Blog" create screen
    And "Sojourner" enters "3" in the "id" field
    And "Sojourner" enters "Some Blog" in the "title" field
    And "Sojourner" enters "Some Description" in the "description" field
    When the "Blog" is submitted
    Then an 401 error should be returned

  Scenario: Turn off security on specific path when security set globally

  Scenario: Set security on a specific path

  Scenario: No JWT with request on path protected by OpenID

  Scenario: Valid JWT with request on path protected by OpenID

  Scenario: Invalid OpenID connect url set in security scheme

  Scenario: Expired JWT with request on path protected by OpenID

  Scenario: Request with missing required scope