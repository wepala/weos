Feature: Setup List endpoint

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
    """
    And blogs in the api
      | id    | title        | description    |
      | 1234  | Blog 1       | Some Blog      |
      | 4567  | Blog 2       | Some Blog 2    |


  Scenario: Setup list endpoint by specifying it returns an array of content type

    For "GET" requests the response schema is used to infer the content type to use. If there is an array schema anywhere
    in the response then it can be used as a list

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
    /blogs:
      get:
        operationId: Get Blogs
        responses:
          200:
            description: List of blogs
            content:
              application/json:
                schema:
                  type: object
                  properties:
                    total:
                      type: integer
                    page:
                      type: integer
                    items:
                      type: array
                      items:
                        $ref: "#/components/schemas/Blog"
          400:
            description: Invalid blog submitted
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a "GET" route should be added to the api
    And a "List" middleware should be added to the route

  Scenario: Setup list endpoint the uses a custom response fields

    By default the total results are turned with the result array as total and page. The developer can also specify the
    field in the response to use for that data

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
    /blogs:
      get:
        operationId: Get Blogs
        responses:
          200:
            description: List of blogs
            content:
              application/json:
                schema:
                  type: object
                  properties:
                    t:
                      type: integer
                      x-alias: total
                    p:
                      type: integer
                      x-alias: page
                    items:
                      type: array
                      items:
                        $ref: "#/components/schemas/Blog"
          400:
            description: Invalid blog submitted
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a "GET" route should be added to the api
    And a "List" middleware should be added to the route

  Scenario: Setup list endpoint by specifying the controller

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
    /blogs:
      get:
        operationId: Get Blogs
        x-controller: List
        responses:
          200:
            description: List of blogs
            content:
              application/json:
                schema:
                  type: object
                  properties:
                    total:
                      type: integer
                    page:
                      type: integer
                    items:
                      type: array
                      items:
                        $ref: "#/components/schemas/Blog"
          400:
            description: Invalid blog submitted
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a "GET" route should be added to the api
    And a "List" middleware should be added to the route

  Scenario: Setup list endpoint by specifying the controller but there is no array in the response

    The response must have an array of items of a content type returned

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
    /blogs:
      get:
        operationId: Get Blogs
        x-controller: List
        responses:
          200:
            description: List of blogs
            content:
              application/json:
                schema:
                  $ref: "#/components/schemas/Blog"
          400:
            description: Invalid blog submitted
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a warning should be output because the endpoint is invalid


  Scenario: Setup filter endpoint

  The DSL for lists filters have three parts:
  1. Field - The field to be filtered on
  2. Operator - the operator to use for the filter (eq, ne, gt, lt, in, like)
  3. Value (an array or possible values. Most operators will be for scalar values

  """
    /blogs:
      get:
        operationId: Get Blogs
        summary: Get List of Blogs
        parameters:
          - in: query
            name: filters
            schema:
              type: array
              items:
                type: object
                properties:
                  field:
                    type: string
                  operator:
                    type: string
                  value:
                    type: array
                    items:
                      type: string
        responses:
          200:
            description: List of blogs
            content:
              application/json:
                schema:
                  type: object
                  properties:
                    total:
                      type: integer
                    page:
                      type: integer
                    items:
                      type: array
                      items:
                        $ref: "#/components/schemas/Blog"
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a "GET" route should be added to the api
    And a "List" middleware should be added to the route

  Scenario: Setup list endpoint with some filter capability

  Developers can specify which fields can be filtered and what filter options are available by setting the fields as enumerations

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
    /blogs:
      get:
        operationId: Get Blogs
        parameters:
          - in: post
            name: filters
            schema:
              type: array
              items:
                type: object
                properties:
                  field:
                    type: string
                    enum:
                      - title
                      - description
                  operator:
                    type: string
                    enum:
                      - eq
                      - ne
                  value:
                    type: array
                    items:
                      type: string
        responses:
          200:
            description: Update blog
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
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a "GET" route should be added to the api
    And a "list" middleware should be added to the route


  Scenario: Specify list item with an invalid filters definition

    If filters are specified then it should be in the expected for the controller to be associated. If it's invalid it
    should show a warning (otherwise a controller that knows how to parse the filters should be explicitly set).

  """
    /blogs:
      get:
        operationId: Get Blogs
        summary: Get List of Blogs
        x-context:
          filters:
            - adadsfad
            - adfadsf
          page: 1
          limit: 10
        responses:
          200:
            description: List of blogs
            content:
              application/json:
                schema:
                  type: object
                  properties:
                    total:
                      type: integer
                    page:
                      type: integer
                    items:
                      type: array
                      items:
                        $ref: "#/components/schemas/Blog"
    """
    When the "OpenAPI 3.0" specification is parsed
    Then a "GET" route should be added to the api
    And a "List" middleware should be added to the route

  Scenario: Sort a list of items



