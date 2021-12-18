Feature: Create content

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


  Scenario: Get a list of items

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
    /blogs/:
      put:
        operationId: Get Blogs
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
    Then a "PUT" route should be added to the api
    And a "edit" middleware should be added to the route


  Scenario: Filter a list of items

  Scenario: Sort a list of items



