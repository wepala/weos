Feature: Specify the response details for endpoints

  Background:

    Given a developer "Sojourner"
    And "Sojourner" has an account with id "1234"
    And "OpenAPI 3.0" is used to model the service


  Scenario: Specify JSON response

    If the response content-type is application/json then json will be returned. Each standard controller has a standard
    response

  Scenario: Map the standard controller response to custom properties on response body schema

    A custom response could be created and the x-alias extension used to map the standard response properties to the
    custom response schema

  Scenario: Get item back on command endpoints

    Usually the standard controllers do NOT return the item updated. If the same schema used on the input is used on the
    response body then the a response would be returned

  Scenario: Specify HTML response

    An html response can be specified. An html template could be specified using the x-template extension

  Scenario: Specify HTML response with Go template

    Go provides a template system that can be used so that the data that would be returned could be populated in the
    template

  Scenario: Specify multiple response types

    If there are multiple response types available the first one is used by default unless a _format is specified in the
    request

  Scenario: Specify redirect response

    A redirect response type can be used to redirect to another url using the x-url extension

  Scenario: Customize error response

    Error responses can be customized similar to how response bodies are customized.