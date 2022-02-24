Feature: Hardcode the response for an endpoint 
  
  There are times when we want to be able to return a fixed response on a specific endpoint. 
  To do this you can create an example response and if there is not controller specified for that endpoint
  the example would be returned 
  
  Background:
    Given a developer "Sojourner"
    And "Sojourner" has an account with id "1234"
    And "Open API 3.0" is used to model the service



  @WEOS-1365
  Scenario:  Basic html response

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /:
        get:
          operationId: example
          responses:
            200:
              content:
                text/html:
                  example: |
                    <html> <head><title>Test</title></head><body>This is a test page</body></html>
    """
    When the "GET" endpoint "/" is hit
    Then a 200 response should be returned
    And the content type should be "text/html"
    And the response body should be
    """
    <html> <head><title>Test</title></head><body>This is a test page</body></html>
    """
  @WEOS-1365
  Scenario: Multiple responses

    If there are multiple response available use the first one

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /:
        get:
          operationId: example
          responses:
            200:
              content:
                text/html:
                  example: |
                    <html> <head><title>Test</title></head><body>This is a test page</body></html>
            404:
              content:
                text/html:
                  example: |
                    <html> <head><title>Page Not Found</title></head><body>Some not found page</body></html>
    """
    When the "GET" endpoint "/" is hit
    Then a 200 response should be returned
    And the content type should be "text/html"
    And the response body should be
    """
    <html> <head><title>Test</title></head><body>This is a test page</body></html>
    """
  @WEOS-1365
  Scenario: Send Accept header to hit at content of expected response

    If there are multiple content types you can send an [Accept header](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Accept)
    to indicate which content type you want returned. Wild cards could be used to specify the header (note if there are
    multiple response codes the first response code would be used).

    Given "Sojourner" adds an endpoint to the "OpenAPI 3.0" specification
    """
      /:
        get:
          operationId: example
          responses:
            200:
              content:
                text/html:
                  example: |
                    <html> <head><title>Test</title></head><body>This is a test page</body></html>
                application/xml:
                  example: |
                    <page><title>Test</title></page>
    """
    And the header "Accept" is set with value "application/*"
    When the "GET" endpoint "/" is hit
    Then a 200 response should be returned
    And the content type should be "application/xml"
    And the response body should be
    """
    <page><title>Test</title></page>
    """