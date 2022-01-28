
Feature: List content

  The list controller provides pagination functionality if it's configured. The list endpoints also allow for filtering
  and sorting the collection

  The DSL for lists filters have three parts:
  1. Field - The field to be filtered on
  2. Operator - the operator to use for the filter (eq, ne, gt, lt, in, like)
  3. Value - if it's a single value
  4. Values - if it's an array of values

  The filters can be applied directly to the context (the controller pulls the values from the context). They can also be defined in query and aliased to the necessary values in the context

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
        driver: sqlite3
        database: e2e.db
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
         get:
           operationId: Get Blogs
           summary: Get List of Blogs
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

               required: false
               description: query string
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
    And the service is running
    And blogs in the api
      | id    | entity id                   | sequence no | title        | description    |
      | 1     | 24Kj7ExtIFvuGgTOTLBgpZgCl0n | 2           | Blog 1       | Some Blog      |
      | 2     | 24KjDkwfmp8PCslCQ6Detx6yr1N | 1           | Blog 2       | Some Blog 2    |
      | 164   | 24KjFbp82wGq4qb5LAxLdA5GbR2 | 1           | Blog 6       | Some Blog 6    |
      | 3     | 24KjHaQbjEv0ZxfKxFup1dI6iKP | 4           | Blog 3       | Some Blog 3    |
      | 4     | 24KjIq8KJhIhWa7d8sNJhRilGpA | 1           | Blog 4       | Some Blog 4    |
      | 5     | 24KjLAP17p3KvTy5YCMWUIRlOSS | 1           | Blog 5       | Some Blog 5    |
      | 890   | 24KjMP9uTPxW5Xuhziv1balYskX | 1           | Blog 7       | Some Blog 7    |
      | 1237  | 24KjNifBFHrIQcfEe2QCaiHXd22 | 1           | Blog 8       | Some Blog 8    |

  @WEOS-1133
  Scenario: Get list of items

    By default the list of items returned would be paged (for performance reasons). It will be sorted on the id by default

    Given "Sojourner" is on the "Blog" list screen
    And the items per page are 5
    When the search button is hit
    Then a 200 response should be returned
    And the list results should be
      | id    | title        | description    |
      | 1     | Blog 1       | Some Blog      |
      | 1237  | Blog 8       | Some Blog 8    |
      | 164   | Blog 6       | Some Blog 6    |
      | 2     | Blog 2       | Some Blog 2    |
      | 3     | Blog 3       | Some Blog 3    |

    And the total results should be 8
    And the page in the result should be 1

  @WEOS-1133
  Scenario: Get second page of items

    If the page and limit parameters are defined then the endpoint can be used to get paged results

    Given "Sojourner" is on the "Blog" list screen
    And the items per page are 2
    And the page no. is 2
    When the search button is hit
    Then a 200 response should be returned
    And the list results should be
      | id    | title        | description    |
      | 164   | Blog 6       | Some Blog 6    |
      | 2     | Blog 2       | Some Blog 2    |
    And the total results should be 8
    And the page in the result should be 2

  @WEOS-1134
  Scenario: Filter list using equal operator

    The equal operator "eq" can only be used with a single value.

    Given "Sojourner" is on the "Blog" list screen
    And the items per page are 5
    And a filter on the field "id" "eq" with value "3"
    When the search button is hit
    Then a 200 response should be returned
    And the list results should be
      | id    | entity id                   | sequence no | title        | description    |
      | 3     | 24KjHaQbjEv0ZxfKxFup1dI6iKP | 4           | Blog 3       | Some Blog 3    |
    And the total results should be 1
    And the page in the result should be 1

  @WEOS-1134
  Scenario: Filter list using equal operator but passing in multiple values

    The equal operator can only work with a single value

    Given "Sojourner" is on the "Blog" list screen
    And the items per page are 5
    And a filter on the field "id" "eq" with values
      | values       |
      | 3            |
      | 4            |
    When the search button is hit
    Then a 400 response should be returned
    And an error should be returned

  @WEOS-1134
  Scenario: Filter list using not equal operator

    The not equal operator "ne" can only be used with a single value.

    Given "Sojourner" is on the "Blog" list screen
    And the items per page are 5
    And a filter on the field "id" "ne" with value "3"
    When the search button is hit
    Then a 200 response should be returned
    And the list results should be
      | id    | entity id                   | sequence no | title        | description    |
      | 1     | 24Kj7ExtIFvuGgTOTLBgpZgCl0n | 2           | Blog 1       | Some Blog      |
      | 2     | 24KjDkwfmp8PCslCQ6Detx6yr1N | 1           | Blog 2       | Some Blog 2    |
      | 4     | 24KjIq8KJhIhWa7d8sNJhRilGpA | 1           | Blog 4       | Some Blog 4    |
      | 5     | 24KjLAP17p3KvTy5YCMWUIRlOSS | 1           | Blog 5       | Some Blog 5    |
      | 164   | 24KjFbp82wGq4qb5LAxLdA5GbR2 | 1           | Blog 6       | Some Blog 6    |
    And the total results should be 7
    And the page in the result should be 1

  @WEOS-1134
  Scenario: Filter list using greater than operator

    The greater than operator "gt" can only be used with a single value.

    Given "Sojourner" is on the "Blog" list screen
    And the items per page are 5
    And a filter on the field "id" "gt" with value "3"
    When the search button is hit
    Then a 200 response should be returned
    And the list results should be
      | id    | entity id                   | sequence no | title        | description    |
      | 4     | 24KjIq8KJhIhWa7d8sNJhRilGpA | 1           | Blog 4       | Some Blog 4    |
      | 5     | 24KjLAP17p3KvTy5YCMWUIRlOSS | 1           | Blog 5       | Some Blog 5    |
      | 164   | 24KjFbp82wGq4qb5LAxLdA5GbR2 | 1           | Blog 6       | Some Blog 6    |
      | 890   | 24KjMP9uTPxW5Xuhziv1balYskX | 1           | Blog 7       | Some Blog 7    |
      | 1237  | 24KjNifBFHrIQcfEe2QCaiHXd22 | 1           | Blog 8       | Some Blog 8    |
    And the total results should be 5
    And the page in the result should be 1

  @WEOS-1134
  Scenario: Filter list using less than operator

    The less than operator "lt" can only be used with a single value.

    Given "Sojourner" is on the "Blog" list screen
    And the items per page are 5
    And a filter on the field "id" "lt" with value "3"
    When the search button is hit
    Then a 200 response should be returned
    And the list results should be
      | id    | entity id                   | sequence no | title        | description    |
      | 1     | 24Kj7ExtIFvuGgTOTLBgpZgCl0n | 2           | Blog 1       | Some Blog      |
      | 2     | 24KjDkwfmp8PCslCQ6Detx6yr1N | 1           | Blog 2       | Some Blog 2    |
    And the total results should be 2
    And the page in the result should be 1

  @WEOS-1134
  Scenario: Filter list using like  operator

    The like operator "like" can only be used with a single value.

    Given "Sojourner" is on the "Blog" list screen
    And the items per page are 5
    And a filter on the field "id" "like" with value "1"
    When the search button is hit
    Then a 200 response should be returned
    And the list results should be
      | id    | entity id                   | sequence no | title        | description    |
      | 1     | 24Kj7ExtIFvuGgTOTLBgpZgCl0n | 2           | Blog 1       | Some Blog      |
      | 164   | 24KjFbp82wGq4qb5LAxLdA5GbR2 | 1           | Blog 6       | Some Blog 6    |
      | 1237  | 24KjNifBFHrIQcfEe2QCaiHXd22 | 1           | Blog 8       | Some Blog 8    |
    And the total results should be 3
    And the page in the result should be 1

  @WEOS-1134
  Scenario: Filter list using in than operator

    The in operator "in" is used to check if there items where the field's value is in the provided list of possible field values

    Given "Sojourner" is on the "Blog" list screen
    And the items per page are 5
    And a filter on the field "id" "in" with values
      | values       |
      | 3            |
      | 4            |
    When the search button is hit
    Then a 200 response should be returned
    And the list results should be
      | id    | entity id                   | sequence no | title        | description    |
      | 3     | 24KjHaQbjEv0ZxfKxFup1dI6iKP | 4           | Blog 3       | Some Blog 3    |
      | 4     | 24KjIq8KJhIhWa7d8sNJhRilGpA | 1           | Blog 4       | Some Blog 4    |
    And the total results should be 2
    And the page in the result should be 1