Feature: Customize the path schema dynamically

  There are times when a developer wants to allow a user to customize how the information passed to the endpoint is mapped
  to the schema defined by the developer for that endpoint (example importing a csv, the columns on the csv need to be
  mapped to the schema). The "_schema" parameter is used to map a schema

  Background:

    Given a developer "Sojourner"
    And "Sojourner" has an account with id "1234"
    And "OpenAPI 3.0" is used to model the service

  Scenario: Set schema map via the context

    The mapping could be set via the context using the x-context extension and the _schema parameter

  Scenario: Set schema map via query string

    The schema could be set using the _schema

  Scenario: Set schema map via request body

  Scenario:
