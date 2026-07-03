Feature: Recalled events report the resources they reference
  As an LLM client connected to the WeOS MCP server
  I want every recalled event to carry the IDs of the resources it references
  So that I can trace which events touched a resource across the twin's history

  Background:
    Given a clean WeOS knowledge graph
    And the "tasks" preset is installed

  Scenario: An event with no payload references reports exactly its own resource
    Given the following activity is recorded in the event log:
      | resource-type | name              | event-type       | occurred-at          |
      | project       | Client onboarding | Resource.Created | 2026-06-18T08:00:00Z |
    When I call episodic_recall for events between "2026-06-01T00:00:00Z" and "2026-06-30T00:00:00Z"
    Then the call succeeds
    And the recall includes the "Resource.Created" event for "Client onboarding"
    And the referenced resources for that event are exactly:
      | resource          |
      | Client onboarding |

  Scenario: An event whose payload references another resource reports both resources
    Given the following activity is recorded in the event log:
      | resource-type | name                   | event-type       | occurred-at          | project           |
      | project       | Client onboarding      | Resource.Created | 2026-06-18T08:00:00Z |                   |
      | task          | Chase overdue invoices | Resource.Created | 2026-06-20T09:00:00Z | Client onboarding |
    When I call episodic_recall for events between "2026-06-01T00:00:00Z" and "2026-06-30T00:00:00Z"
    Then the call succeeds
    And the recall includes the "Resource.Created" event for "Chase overdue invoices"
    And the referenced resources for that event are exactly:
      | resource               |
      | Chase overdue invoices |
      | Client onboarding      |

  Scenario: A relationship event reports both resources it links
    Given a "project" named "Client onboarding" exists
    When I create a "task" named "Chase overdue invoices" linked to the project "Client onboarding"
    And I call episodic_recall for events of type "Triple.Created"
    Then the call succeeds
    And the recall includes the "Triple.Created" event for "Chase overdue invoices"
    And the referenced resources for that event are exactly:
      | resource               |
      | Chase overdue invoices |
      | Client onboarding      |

  Scenario: References to a deleted resource remain part of recalled history
    Given a "project" named "Client onboarding" exists
    And a "task" named "Chase overdue invoices" exists linked to the project "Client onboarding"
    And the project "Client onboarding" has since been deleted
    When I call episodic_recall for events of type "Triple.Created"
    Then the call succeeds
    And the recall includes the "Triple.Created" event for "Chase overdue invoices"
    And the referenced resources for that event include "Client onboarding"

  Scenario: A rebuilt reference projection reports the same references from the event log
    Given a "project" named "Client onboarding" exists
    And a "task" named "Chase overdue invoices" exists linked to the project "Client onboarding"
    When the event reference projection is rebuilt from the event log
    And I call episodic_recall for events of type "Triple.Created"
    Then the call succeeds
    And the recall includes the "Triple.Created" event for "Chase overdue invoices"
    And the referenced resources for that event are exactly:
      | resource               |
      | Chase overdue invoices |
      | Client onboarding      |
