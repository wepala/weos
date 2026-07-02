Feature: Declarative agent skills as resources
  As a WeOS app operator
  I want agent skills to be plain resources with a validated definition
  So that I can add or fix a skill with a data change instead of a deploy

  Background:
    Given a clean WeOS knowledge graph
    And the "agents" preset is installed

  Scenario: Installing the agents preset ships the example researcher skill
    When I call resource_list for type "agent-skill"
    Then the listing includes a resource named "knowledge_graph_researcher"

  Scenario: A well-formed skill definition is accepted
    When I call resource_create for type "agent-skill" with the data:
      """
      {
        "schemaVersion": 1,
        "name": "invoice_lookup",
        "description": "Answers questions about invoices: totals, due dates, overdue lookups",
        "instructions": "Look invoices up with the resource tools before answering; cite the invoice URN.",
        "tools": ["resource_list", "resource_get"],
        "mode": "single_turn",
        "widgets": ["table", "card"]
      }
      """
    Then the call succeeds
    And the returned resource has a "agent-skill" URN identifier

  Scenario: A skill with an unknown mode is rejected
    When I call resource_create for type "agent-skill" with the data:
      """
      {
        "schemaVersion": 1,
        "name": "freeform_helper",
        "description": "Handles anything at all",
        "instructions": "Do whatever seems right.",
        "mode": "freestyle"
      }
      """
    Then the call fails with a validation error
    And the error names the invalid property "mode"

  Scenario: A skill without instructions is rejected
    When I call resource_create for type "agent-skill" with the data:
      """
      {
        "schemaVersion": 1,
        "name": "silent_skill",
        "description": "Routes but never says how to act"
      }
      """
    Then the call fails with a validation error
    And the error names the missing property "instructions"
