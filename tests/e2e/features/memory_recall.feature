Feature: Agent memory via the MCP memory tools
  As an LLM client connected to the WeOS MCP server
  I want to record facts and recall them with supersession filtering
  So that my answers stay grounded in the twin's current beliefs

  Background:
    Given a clean WeOS knowledge graph
    And the "memory" preset is installed

  Scenario: A just-recorded fact is recallable in the same turn
    When I call resource_create for type "fact" with the data:
      """
      {
        "statement": "Akeem bases PRs on the v3 branch",
        "about": "urn:person:akeem",
        "confidence": 0.9
      }
      """
    Then the call succeeds
    When I call memory_recall for facts about "urn:person:akeem"
    Then the recall includes a fact stating "Akeem bases PRs on the v3 branch"

  Scenario: A superseded fact disappears from recall but its successor remains
    Given a recorded fact "old" stating "Akeem works from home on Mondays" about "urn:person:akeem"
    When I record a fact superseding "old" stating "Akeem works from the office on Mondays"
    And I call memory_recall for facts about "urn:person:akeem"
    Then the recall includes a fact stating "Akeem works from the office on Mondays"
    And the recall does not include a fact stating "Akeem works from home on Mondays"

  Scenario: Recall scoped to another entity excludes unrelated facts
    Given a recorded fact "akeem" stating "Akeem bases PRs on the v3 branch" about "urn:person:akeem"
    And a recorded fact "wepala" stating "Wepala builds WeOS" about "urn:org:wepala"
    When I call memory_recall for facts about "urn:org:wepala"
    Then the recall includes a fact stating "Wepala builds WeOS"
    And the recall does not include a fact stating "Akeem bases PRs on the v3 branch"

  Scenario: Confirming a playbook outcome increments its success counter
    Given a recorded playbook named "sync upstream"
    When I call playbook_record_outcome for that playbook with outcome "confirmed"
    Then the call succeeds
    And the playbook outcome reports success count 1 and failure count 0
