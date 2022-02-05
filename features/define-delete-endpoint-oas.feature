@WEOS-1178
Feature: Delete content endpoints

  As developer you can create an endpoint that should be used to delete content of a specific type. The HTTP method and the
  content type specified is used to infer the controller that should be used.

  Background:

    Given a developer "Sojourner"
    And "Sojourner" has an account with id "1234"
    And "OpenAPI 3.0" is used to model the service
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
    And a content type "Blog" modeled in the "OpenAPI 3.0" specification
    """
        Blog:
          type: object
          properties:
            title:
              type: string
            description:
              type: string
    """
    And a content type "Post" modeled in the "OpenAPI 3.0" specification
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
            blog:
              $ref: "#/components/schemas/Blog"
            publishedDate:
              type: string
            views:
              type: integer
            categories:
              type: array
              items:
                $ref: "#/components/schemas/Category"
          x-identifier:
            - id
            - title
    """


  Scenario: Create a basic delete endpoint with the identifier in the path

    The delete endpoint would used the requestBody to associate the handler automatically BUT the request body is marked
    optional because there is no need to send the blog info (the id is enough)

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /blogs/{id}:
        delete:
          operationId: Delete Blog
          parameters:
            - in: path
              name: id
              schema:
                type: string
              required: true
              description: blog id
          requestBody:
            description: Blog info that is submitted
            required: false
            content:
              application/json:
                schema:
                  $ref: "#/components/schemas/Blog"
          responses:
            200:
              description: Update blog
              content:
                application/json:
                  schema:
                    $ref: "#/components/schemas/Blog"
            400:
              description: Invalid blog submitted
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a "DELETE" route should be added to the api
    And a "DeleteController" middleware should be added to the route


  Scenario: Create a basic delete endpoint with the entity explicitly declared

    A developer should be able to declare what the entity type is explicitly (as an alternative to using the requestBody
    to indicate what it is)

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /blogs/{id}:
        delete:
          operationId: Delete Blog
          parameters:
            - in: path
              name: id
              schema:
                type: string
              required: true
              description: blog id
          x-schema: Blog
          responses:
            200:
              description: Update blog
              content:
                application/json:
                  schema:
                    $ref: "#/components/schemas/Blog"
            400:
              description: Invalid blog submitted
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a "DELETE" route should be added to the api
    And a "DeleteController" middleware should be added to the route

  Scenario: Create a basic delete endpoint with the identifier in the query string

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /blogs:
        delete:
          operationId: Delete Blog
          parameters:
            - in: query
              name: id
              schema:
                type: string
              required: true
              description: blog id
          requestBody:
            description: Blog info that is submitted
            required: false
            content:
              application/json:
                schema:
                  $ref: "#/components/schemas/Blog"
          responses:
            200:
              description: Update blog
              content:
                application/json:
                  schema:
                    $ref: "#/components/schemas/Blog"
            400:
              description: Invalid blog submitted
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a "DELETE" route should be added to the api
    And a "DeleteController" middleware should be added to the route

  Scenario: Create a basic delete endpoint with the identifier in the header with an alias

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /blogs:
        delete:
          operationId: Delete Blog
          parameters:
            - in: header
              name: X-Item-Id
              schema:
                type: string
              required: true
              description: blog id
              x-context-name: id
          requestBody:
            description: Blog info to be deleted
            required: false
            content:
              application/json:
                schema:
                  $ref: "#/components/schemas/Blog"
          responses:
            200:
              description: Update blog
              content:
                application/json:
                  schema:
                    $ref: "#/components/schemas/Blog"
            400:
              description: Invalid blog submitted`
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a "DELETE" route should be added to the api
    And a "DeleteController" middleware should be added to the route


  Scenario: Create a basic delete endpoint with the identifier in the path and the controller manually set

  Though the controller would typically automatically be set, it should use what is set in x-controller if available

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /blogs/{id}:
        delete:
          operationId: Delete Blog
          parameters:
            - in: path
              name: id
              schema:
                type: string
              required: true
              description: blog id
          x-controller: DeleteController
          requestBody:
            description: Blog info that is submitted
            required: false
            content:
              application/json:
                schema:
                  $ref: "#/components/schemas/Blog"
          responses:
            200:
              description: Update blog
            400:
              description: Invalid blog submitted
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a "DELETE" route "/blogs/:id" should be added to the api
    And a "DeleteController" middleware should be added to the route

  Scenario: Create an endpoint that does not have parameters for all parts of identifier

  Content types can have multiple fields as part of the identifier. If there are no parameters that map to the identifier
  then the item cannot be retrieved

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /posts/{id}:
        delete:
          operationId: Delete Post
          parameters:
            - in: path
              name: id
              schema:
                type: string
              required: true
              description: post id
          x-schema: Post
          responses:
            201:
              description: Add Blog to Aggregator
              content:
                application/json:
                  schema:
                    $ref: "#/components/schemas/Post"
            400:
              description: Invalid blog submitted
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a warning should be output to logs letting the developer know that a parameter for each part of the idenfier must be set

  Scenario: Setup path without content type

    Specifying a content type in the path definition is necessary. If no content type is associated with the endpoint
    then the middleware will not be associated

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /blogs/{id}:
        delete:
          operationId: Delete Blog
          parameters:
            - in: path
              name: id
              schema:
                type: string
              required: true
              description: blog id
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
    When the "OpenAPI 3.0" specification is parsed
    Then a warning should be output to logs letting the developer know that a handler needs to be set

  Scenario: Setup path where the request body does not reference schema

    In order to use the delete handler a schema is needed so that validation etc could be setup or use the x-content-type
    extension to specify the content type 

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /blogs/{id}:
        delete:
          operationId: Add Blog
          parameters:
            - in: path
              name: id
              schema:
                type: string
              required: true
              description: blog id
          requestBody:
            description: Blog to add
            required: true
            content:
              application/json:
                schema:
                  type: object
                  properties:
                    id:
                      type: integer
                      description: blog id
                    title:
                      type: string
                      description: blog description
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
    When the "OpenAPI 3.0" specification is parsed
    Then a warning should be output to logs letting the developer know that a handler needs to be set