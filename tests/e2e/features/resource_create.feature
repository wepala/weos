Feature: Create a resource via the MCP resource_create tool
  As an LLM client connected to the WeOS MCP server
  I want to create a resource by passing its data as a JSON object
  So that the resource is persisted in the knowledge graph and returned to me

  Background:
    Given a clean WeOS knowledge graph
    And a resource type "capability" exists with a JSON Schema requiring an object with a "name" property

  Scenario: Creating a capability with a valid object payload persists and returns it
    When I call resource_create for type "capability" with the data:
      """
      {
        "name": "Summarize Inbox",
        "description": "Summarize unread email into a digest",
        "kind": "skill"
      }
      """
    Then the call succeeds
    And the returned resource has a "capability" URN identifier
    And the returned resource has status "active"
    And the returned resource data has name "Summarize Inbox"
    And fetching the returned resource by its identifier returns the same capability

  Scenario: Creating a capability with a string payload returns a clear validation error
    When I call resource_create for type "capability" with the data:
      """
      "this is not an object"
      """
    Then the call fails with a validation error
    And the error states that the "data" argument must be a JSON object
    And the error does not contain the raw text "got string, want object"
    And the error does not leak a local file path

  Scenario: Creating a capability that omits a required property reports the missing property
    When I call resource_create for type "capability" with the data:
      """
      { "description": "a capability with no name" }
      """
    Then the call fails with a validation error
    And the error names the missing property "name"

  Scenario: Creating a resource of an unknown type reports the type is not found
    When I call resource_create for type "widget" with the data:
      """
      { "name": "Desk Widget" }
      """
    Then the call fails
    And the error states that the resource type "widget" is not found
