
Feature: Hydrate database using events

  The events generated in the API could be used to re-create tables in the base data store or to create new datastores.
  The events could be used to do repairs as well (if the handlers on the projection are done in an idempotent way)

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
      | id    | weos_id                     | sequence_no | title        | description    |
      | 1     | 24Kj7ExtIFvuGgTOTLBgpZgCl0n | 2           | Blog 1       | Some Blog      |
      | 2     | 24KjDkwfmp8PCslCQ6Detx6yr1N | 1           | Blog 2       | Some Blog 2    |
      | 164   | 24KjFbp82wGq4qb5LAxLdA5GbR2 | 1           | Blog 6       | Some Blog 6    |
      | 3     | 24KjHaQbjEv0ZxfKxFup1dI6iKP | 4           | Blog 3       | Some Blog 3    |
      | 4     | 24KjIq8KJhIhWa7d8sNJhRilGpA | 1           | Blog 4       | Some Blog 4    |
      | 5     | 24KjLAP17p3KvTy5YCMWUIRlOSS | 1           | Blog 5       | Some Blog 5    |
      | 890   | 24KjMP9uTPxW5Xuhziv1balYskX | 1           | Blog 7       | Some Blog 7    |
      | 1237  | 24KjNifBFHrIQcfEe2QCaiHXd22 | 1           | Blog 8       | Some Blog 8    |

  @WEOS-1327
  Scenario: Hydrate tables based on events

    A developer should be able to configure an event repository to replay all it's events on startup. This should trigger
    the associated projections

    Given Sojourner" deletes the "Blogs" table
    When "Sojourner" calls the replay method on the event repository
    Then the "Blogs" table should be populated with
      | id    | weos_id                     | sequence_no | title        | description    |
      | 1     | 24Kj7ExtIFvuGgTOTLBgpZgCl0n | 2           | Blog 1       | Some Blog      |
      | 2     | 24KjDkwfmp8PCslCQ6Detx6yr1N | 1           | Blog 2       | Some Blog 2    |
      | 164   | 24KjFbp82wGq4qb5LAxLdA5GbR2 | 1           | Blog 6       | Some Blog 6    |
      | 3     | 24KjHaQbjEv0ZxfKxFup1dI6iKP | 4           | Blog 3       | Some Blog 3    |
      | 4     | 24KjIq8KJhIhWa7d8sNJhRilGpA | 1           | Blog 4       | Some Blog 4    |
      | 5     | 24KjLAP17p3KvTy5YCMWUIRlOSS | 1           | Blog 5       | Some Blog 5    |
      | 890   | 24KjMP9uTPxW5Xuhziv1balYskX | 1           | Blog 7       | Some Blog 7    |
      | 1237  | 24KjNifBFHrIQcfEe2QCaiHXd22 | 1           | Blog 8       | Some Blog 8    |
    And the total no. events and processed and failures should be returned

  @WEOS-1327
  Scenario: Repair data tables after some was deleted

  @WEOS-1327
  Scenario: Repair tables after some content has been deleted
    Given a "Blog" with id "1237" was deleted
    And a "Blog" with id "164" was deleted
    When "Sojourner" calls the replay method on the event repository
    Then the "Blogs" table should be populated with
      | id    | weos_id                     | sequence_no | title        | description    |
      | 1     | 24Kj7ExtIFvuGgTOTLBgpZgCl0n | 2           | Blog 1       | Some Blog      |
      | 2     | 24KjDkwfmp8PCslCQ6Detx6yr1N | 1           | Blog 2       | Some Blog 2    |
      | 164   | 24KjFbp82wGq4qb5LAxLdA5GbR2 | 1           | Blog 6       | Some Blog 6    |
      | 3     | 24KjHaQbjEv0ZxfKxFup1dI6iKP | 4           | Blog 3       | Some Blog 3    |
      | 4     | 24KjIq8KJhIhWa7d8sNJhRilGpA | 1           | Blog 4       | Some Blog 4    |
      | 5     | 24KjLAP17p3KvTy5YCMWUIRlOSS | 1           | Blog 5       | Some Blog 5    |
      | 890   | 24KjMP9uTPxW5Xuhziv1balYskX | 1           | Blog 7       | Some Blog 7    |
      | 1237  | 24KjNifBFHrIQcfEe2QCaiHXd22 | 1           | Blog 8       | Some Blog 8    |
    And the total no. events and processed and failures should be returned

  Scenario: Continue loading events if error occurs

    The event that failed should be logged out WITHOUT the payload



  Scenario: Repair specific schemas

  Scenario: Set database to a specific state on a given date