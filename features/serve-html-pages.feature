Feature: Serve HTML Content

  The API can return responses of all content types including HTML.

  Background:

    Given a developer "Sojourner"
    And "Sojourner" has an account with id "1234"
    And "OpenAPI 3.0" is used to model the service
    And there is a file "./static/some.css"
    """
    #id {
      color: black;
    }
    """
    And there is a file "./static/index.html"
    """
    <html><head><title>Test Page</title></head><body>Test Page</body></html>
    """
  @WEOS-1383
  Scenario: Folder configured to return static content at a specific endpoint

  Developers can configure an endpoint to serve content from a folder defined using the "x-folder" extension. The
  extension should automatically add the "Static" middleware

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /asset:
        get:
          operationId: getAsset
          responses:
            200:
              description: File Found
              x-folder: "./static"
            404:
              description: File not found
            400:
              description:
    """
    And the service is running
    When the "GET" endpoint "/asset/some.css" is hit
    Then a 200 response should be returned
    And the content type should be "text/css; charset=utf-8"
    And the response body should be
    """
    #id {
      color: black;
    }
    """

  @WEOS-1383
  Scenario: Serve static content from root folder

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /:
        get:
          operationId: getAsset
          responses:
            200:
              description: File Found
              x-folder: "./static"
            404:
              description: File not found
            400:
              description:
    """
    And the service is running
    When the "GET" endpoint "/some.css" is hit
    Then a 200 response should be returned
    And the content type should be "text/css; charset=utf-8"
    And the response body should be
    """
    #id {
      color: black;
    }
    """

  @WEOS-1383
  Scenario: Specifying an invalid folder to serve content from

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /:
        get:
          operationId: getAsset
          responses:
            200:
              description: File Found
              x-folder: "./foobar"
            404:
              description: File not found
            400:
              description:
    """
    When the service is running
    Then a warning should be shown informing the developer that the folder doesn't exist

  @WEOS-1295
  Scenario: Specify specific file to be served by an endpoint

  A developer can also specify that a specific file should be served from an endpoint using the x-file extension
  (e.g. serving index.html for a specific endpoint). The "File" middleware is automatically applied when the x-file
  extension is used

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /:
        get:
          operationId: getAsset
          responses:
            200:
              description: File Found
              x-file: "./static/index.html"
            404:
              description: File not found
            402:
              description: User not authenticated
    """
    And the service is running
    When the "GET" endpoint "/" is hit
    Then a 200 response should be returned
    And the content type should be "text/html; charset=utf-8"
    And the response body should be
    """
    <html><head><title>Test Page</title></head><body>Test Page</body></html>
    """

  @skipped
  @WEOS-1384
  Scenario: Specify HTML response with Go template

    Go provides a template system that can be used so that the data that would be returned could be populated in the
    template.

    Given there is a file "./templates/base.html"
    """
    <html>
      <body>
        <h1>{{ template "title" }}</h1>


        {{ template "content" }}
      </body>
    </html>
    """
    And there is a file "./templates/index.html"
    """
    {{ define "title" }}About{{ end }}


    {{ define "content" }}
      <p>About us page now</p>
    {{ end }}
    """
    And "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /:
        get:
          operationId: getAsset
          responses:
            200:
              description: Homepage
              x-templates:
                - base.html
                - index.html
            404:
              description: File not found
            402:
              description: User not authenticated
    """
    And the service is running
    When the "GET" endpoint "/" is hit
    Then a 200 response should be returned
    And the content type should be "text/html"
    And the response body should be
    """
    <html>
      <body>
        <h1>About</h1>


        <p>About us page now</p>
      </body>
    </html>
    """

  @skipped
  @WEOS-1384
  Scenario: Render Go template with data from context

    Some standard middleware add data to the context. The Entire context data should be available to the template for reference

    Given there is a file "./templates/base.html"
    """
    <html>
      <body>
        <h1>{{.Title}}</h1>


        {{.Content}}
      </body>
    </html>
    """
    And "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /:
        get:
          operationId: getAsset
          parameters:
            - in: query
              name: title
              schema:
                type: string
            - in: query
              name: content
              schema:
                type: string
          responses:
            200:
              description: Homepage
              x-templates:
                - base.html
            404:
              description: File not found
            402:
              description: User not authenticated
    """
    And the service is running
    When the "GET" endpoint "/?title=Test&content=LoremIpsum" is hit
    Then a 200 response should be returned
    And the content type should be "text/html"
    And the response body should be
    """
    <html>
      <body>
        <h1>Test</h1>


        <p>LoremIpsum</p>
      </body>
    </html>
    """

  Scenario: Invalid Go template

    if the template returns an error while rendering then return a 500 error

    Given there is a file "./templates/base.html"
    """
    <html>
      <body>
        <h1>{{.Title}}</h1>


        {{.Content}}
      </body>
    </html>
    """
    And "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /:
        get:
          operationId: getAsset
          responses:
            200:
              description: Homepage
              x-templates:
                - base.html
            404:
              description: File not found
            402:
              description: User not authenticated
    """
    And the service is running
    When the "GET" endpoint "/" is hit
    Then a 500 response should be returned

  Scenario: Embed static content in binary