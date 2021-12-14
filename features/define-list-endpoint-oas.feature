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
              content:
                application/json:
                  schema:
                    $ref: "#/components/schemas/ErrorResponse"
      /posts:
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
            name: sort
            schema:
              type: array
              items:
                type: string
          - in: query
            name: blog_id
            schema:
              type: string
          - in: query
            name: category
            schema:
              type: string
        get:
          operationId: List Posts
          responses:
            200:
              description: List of Posts
              content:
                application/json:
                  schema:
                    type: object
                    properties:
                      total:
                        type: integer
                      page:
                        type: integer
                      limit:
                        type: integer
                      items:
                        type: array
                        items:
                          $ref: "#/components/schemas/Post"
    """


  Scenario: Get a list of items

  Scenario: Filter a list of items

  Scenario: Sort a list of items



