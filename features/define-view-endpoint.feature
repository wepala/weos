@WEOS-1222
Feature: Setup View endpoint

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


   Scenario: Setup view endpoint by setting the response body

     The response schema can be used to infer that the view handler should be used. An ETag should be returned that can
     be used to avoid update collisions (it's a concatenation of the entity id and the version e.g. abasdf123.1)

     Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
     """
       /blogs/{id}:
         get:
           parameters:
             - in: path
               name: id
               schema:
                 type: string
               required: true
               description: blog id
           summary: Get Blog by id
           operationId: Get Blog
           responses:
             200:
               description: Blog details without any supporting collections
               headers:
                 ETag:
                   schema:
                     type: string
                   description: specific version of item
               content:
                 application/json:
                   schema:
                     $ref: "#/components/schemas/Blog"
             404:
               description: Blog not found

     """
     When the "OpenAPI 3.0" specification is parsed
     Then a "GET" route should be added to the api
     And a "View" middleware should be added to the route

   Scenario: Setup view endpoint by specifying the controller

     Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
     """
       /blogs/{id}:
         get:
           parameters:
             - in: path
               name: id
               schema:
                 type: string
               required: true
               description: blog id
           summary: Get Blog by id
           operationId: Get Blog
           x-middleware:
             - ViewMiddleware
           x-controller: ViewController
           responses:
             200:
               description: Blog details without any supporting collections
               headers:
                 ETag:
                   schema:
                     type: string
                   description: specific version of item
               content:
                 application/json:
                   schema:
                     $ref: "#/components/schemas/Blog"
             404:
               description: Blog not found
     """
     When the "OpenAPI 3.0" specification is parsed
     Then a "GET" route should be added to the api
     And a "View" middleware should be added to the route

   Scenario: Setup view endpoint that allows for getting specific version

     A developer can pass in the specific sequence no (sequence_no) to get an entity at a specific state

     Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
     """
       /blogs/{id}:
         get:
           parameters:
             - in: path
               name: id
               schema:
                 type: string
               required: true
               description: blog id
             - in: header
               name: version
               x-context-name: sequence_no
               schema:
                 type: string
           summary: Get Blog by id
           operationId: Get Blog
           responses:
             200:
               description: Blog details without any supporting collections
               headers:
                 ETag:
                   schema:
                     type: string
                   description: specific version of item
               content:
                 application/json:
                   schema:
                     $ref: "#/components/schemas/Blog"
             404:
               description: Blog not found

     """
     When the "OpenAPI 3.0" specification is parsed
     Then a "GET" route should be added to the api
     And a "View" middleware should be added to the route

   Scenario: Setup view endpoint that can be used to check unchanged item

     Check if version is the latest version https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/ETag

     Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
     """
       /blogs/{id}:
         get:
           parameters:
             - in: path
               name: id
               schema:
                 type: string
               required: true
               description: blog id
             - in: header
               name: If-None-Match
               x-context-name: etag
               schema:
                 type: string
           summary: Get Blog by id
           operationId: Get Blog
           responses:
             200:
               description: Blog details without any supporting collections
               headers:
                 ETag:
                   schema:
                     type: string
                   description: specific version of item
               content:
                 application/json:
                   schema:
                     $ref: "#/components/schemas/Blog"
             304:
               description: Not modified
             404:
               description: Blog not found

     """
     When the "OpenAPI 3.0" specification is parsed
     Then a "GET" route should be added to the api
     And a "View" middleware should be added to the route


