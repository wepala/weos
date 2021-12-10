Feature: Create content

  Background:

    Given a developer "Harriet"
    And "Harriet" has an account with id "1234"
    And "Open API 3.0" is used to model the service
    And a content type "Category" modeled in "Open API 3.0"
    """
      Category:
        type: object
        properties:
          title:
            type: string
          description:
            type: string
    """
    And a content type "Blog" modeled in "Open API 3.0"
    """
      Blog:
        type: object
        properties:
          title:
            type: string
          description:
            type: string
    """
    And a content type "Post" modeled in "Open API 3.0"
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


    Scenario: Create a basic item

    Scenario: Create an item with a property that is a collection

    Scenario: Create an item that does not meet validation requirements

    Scenario: Create an item that violates uniqueness requirement



