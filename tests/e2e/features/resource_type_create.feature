Feature: Manage resource types via the MCP resource_type tools
  As an LLM client connected to the WeOS MCP server
  I want to define and update a resource type by passing its JSON-LD context and JSON Schema as JSON
  So that I can manage content types over MCP without falling back to the CLI

  Background:
    Given a clean WeOS knowledge graph

  Scenario: Creating a resource type with an object JSON Schema succeeds and the type is usable
    Given the JSON-LD context:
      """
      { "@vocab": "https://schema.org/" }
      """
    And the JSON Schema:
      """
      {
        "type": "object",
        "properties": { "name": { "type": "string" } },
        "required": ["name"]
      }
      """
    When I create the resource type "capability" over MCP
    Then the resource type "capability" is created
    And a "capability" resource can be created with:
      """
      { "name": "Summarize Inbox" }
      """

  Scenario: Creating a resource type with a string JSON-LD context succeeds
    Given the JSON-LD context:
      """
      "https://schema.org/"
      """
    And the JSON Schema:
      """
      { "type": "object", "properties": { "headline": { "type": "string" } } }
      """
    When I create the resource type "article" over MCP
    Then the resource type "article" is created

  Scenario: Updating one field over MCP preserves the type's JSON Schema
    Given the JSON-LD context:
      """
      { "@vocab": "https://schema.org/" }
      """
    And the JSON Schema:
      """
      {
        "type": "object",
        "properties": { "name": { "type": "string" } },
        "required": ["name"]
      }
      """
    And I create the resource type "capability" over MCP
    When I update the resource type "capability" with description "Refined capability"
    Then the resource type "capability" still requires a "name" property
