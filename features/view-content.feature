# Feature: View content

#   Background:

#     Given a developer "Sojourner"
#     And "Sojourner" has an account with id "1234"
#     And "Open API 3.0" is used to model the service
#     And the specification is
#     """
#     openapi: 3.0.3
#     info:
#       title: Blog Aggregator Rest API
#       version: 0.1.0
#       description: REST API for interacting with the Blog Aggregator
#     components:
#       schemas:
#         Blog:
#           type: object
#           properties:
#             title:
#               type: string
#               description: blog title
#             description:
#               type: string
#           required:
#             - title
#         Post:
#           type: object
#           properties:
#             title:
#               type: string
#             description:
#               type: string
#             blog:
#               $ref: "#/components/schemas/Blog"
#             publishedDate:
#               type: string
#               format: date-time
#             views:
#               type: integer
#             categories:
#               type: array
#               items:
#                 $ref: "#/components/schemas/Post"
#           required:
#             - title
#         Category:
#           type: object
#           properties:
#             title:
#               type: string
#             description:
#               type: string
#           required:
#             - title
#     paths:
#       /:
#         get:
#           operationId: Homepage
#           responses:
#             200:
#               description: Application Homepage
#       /blog:
#         post:
#           operationId: Add Blog
#           requestBody:
#             description: Blog info that is submitted
#             required: true
#             content:
#               application/json:
#                 schema:
#                   $ref: "#/components/schemas/Blog"
#               application/x-www-form-urlencoded:
#                 schema:
#                   $ref: "#/components/schemas/Blog"
#               application/xml:
#                 schema:
#                   $ref: "#/components/schemas/Blog"
#           responses:
#             201:
#               description: Add Blog to Aggregator
#               content:
#                 application/json:
#                   schema:
#                     $ref: "#/components/schemas/Blog"
#             400:
#               description: Invalid blog submitted
#               content:
#                 application/json:
#                   schema:
#                     $ref: "#/components/schemas/ErrorResponse"
#       /blogs/{id}:
#         get:
#           parameters:
#             - in: path
#               name: id
#               schema:
#                 type: string
#               required: true
#               description: blog id
#             - in: query
#               name: sequence_no
#               schema:
#                 type: string
#           summary: Get Blog by id
#           operationId: Get Blog
#           responses:
#             200:
#               description: Blog details without any supporting collections
#               content:
#                 application/json:
#                   schema:
#                     $ref: "#/components/schemas/Blog"
#         put:
#           parameters:
#             - in: path
#               name: id
#               schema:
#                 type: string
#               required: true
#               description: blog id
#           summary: Update blog details
#           operationId: Update Blog
#           requestBody:
#             required: true
#             content:
#               application/json:
#                 schema:
#                   $ref: "#/components/schemas/Blog"
#           responses:
#             200:
#               description: Update Blog
#               content:
#                 application/json:
#                   schema:
#                     $ref: "#/components/schemas/Blog"
#         delete:
#           parameters:
#             - in: path
#               name: id
#               schema:
#                 type: string
#               required: true
#               description: blog id
#           summary: Delete blog
#           operationId: Delete Blog
#           responses:
#             200:
#               description: Blog Deleted
#     """
#     And blogs in the api
#       | id    | entity id                   | sequence no | title        | description    |
#       | 1234  | 22xu1Xa5CS3DK1Om2tB7OBDfWAF | 2           | Blog 1       | Some Blog      |
#       | 4567  | 22xu4iw0bWMwxqbrUvjqEqu5dof | 1           | Blog 2       | Some Blog 2    |


#   Scenario: Get blog details

#     The blog should be retrieved using the identifier in the projection. The `ETag` header returned is a combination of
#     the entity id and the sequence no.

#     When the "POST" endpoint "/blog/1234" is hit
#     Then a 200 response should be returned
#     And a blog should be returned
#       | id    | title        | description    |
#       | 1234  | Blog 1       | Some Blog      |
#     And the "ETag" header should be "22xu1Xa5CS3DK1Om2tB7OBDfWAF.2"

#   Scenario: Get blog details using the entity id

#     If the view controller gets a parameter `use_entity_id` set to true then it will use the identifier as the entity id

#     When the "POST" endpoint "/blog/22xu4iw0bWMwxqbrUvjqEqu5dof?use_entity_id=true" is hit
#     Then a 200 response should be returned
#     And a blog should be returned
#       | id    | title        | description    |
#       | 4567  | Blog 2       | Some Blog 2     |
#     And the "ETag" header should be "22xu4iw0bWMwxqbrUvjqEqu5dof.1"

#   Scenario: Get specific version of an entity

#     A developer can pass in the specific sequence no (sequence_no) to get an entity at a specific state

#     Given Sojourner is updating "Blog" with id "4567"
#     And "Sojourner" enters "Some New Blog" in the "title" field
#     And the "Blog" is submitted
#     When the "POST" endpoint "/blog/4567" is hit
#     Then a 200 response should be returned
#     And a blog should be returned
#       | id    | title         | description    |
#       | 4567  | Some New Blog | Some Blog 2    |
#     And the "ETag" header should be "22xu4iw0bWMwxqbrUvjqEqu5dof.2"

#   Scenario: Check if new version of an item is available

#     Check if version is the latest version https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/ETag

#     Given a header "If-None-Match" with value "22xu1Xa5CS3DK1Om2tB7OBDfWAF.2"
#     When the "POST" endpoint "/blog/1234" is hit
#     Then a 304 response should be returned


