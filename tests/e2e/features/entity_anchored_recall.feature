Feature: Entity-anchored recall of events
  As an LLM client connected to the WeOS MCP server
  I want to anchor episodic recall on one or more resources
  So that I can retrieve the events involving the entities a user asks about

  Background:
    Given a clean WeOS knowledge graph
    And the "tasks" preset is installed

  Scenario: Anchoring on a resource finds every event that references it
    Given a "project" named "Client onboarding" exists
    And a "task" named "Chase overdue invoices" exists linked to the project "Client onboarding"
    When I call episodic_recall for events about the resource named "Client onboarding"
    Then the call succeeds
    And the recall includes the "Resource.Created" event for "Client onboarding"
    And the recall includes the "Resource.Created" event for "Chase overdue invoices"
    And the recall includes the "Triple.Created" event for "Chase overdue invoices"
    And the referenced resources for that event include "Client onboarding"

  Scenario: Anchoring on a task does not return the linked project's own events
    Given a "project" named "Client onboarding" exists
    And a "task" named "Chase overdue invoices" exists linked to the project "Client onboarding"
    When I call episodic_recall for events about the resource named "Chase overdue invoices"
    Then the call succeeds
    And the recall includes the "Resource.Created" event for "Chase overdue invoices"
    And the recall does not include an event for "Client onboarding"

  Scenario: Multiple anchors return the union of their events in chronological order
    Given the following activity is recorded in the event log:
      | resource-type | name                     | event-type       | occurred-at          | project           |
      | project       | Client onboarding        | Resource.Created | 2026-06-10T09:00:00Z |                   |
      | project       | Website refresh          | Resource.Created | 2026-06-12T09:00:00Z |                   |
      | task          | Chase overdue invoices   | Resource.Created | 2026-06-20T09:00:00Z | Client onboarding |
      | task          | Redesign landing page    | Resource.Created | 2026-06-22T10:00:00Z | Website refresh   |
      | task          | Draft Q3 invoice summary | Resource.Created | 2026-06-24T11:00:00Z |                   |
    When I call episodic_recall for events about the resources named "Client onboarding" and "Website refresh"
    Then the call succeeds
    And the recall returns exactly these events in order:
      | resource               | event-type       |
      | Client onboarding      | Resource.Created |
      | Website refresh        | Resource.Created |
      | Chase overdue invoices | Resource.Created |
      | Redesign landing page  | Resource.Created |

  Scenario: An anchor composes with the time window and event type filters
    Given the following activity is recorded in the event log:
      | resource-type | name                     | event-type       | occurred-at          | project           |
      | project       | Client onboarding        | Resource.Created | 2026-06-10T09:00:00Z |                   |
      | task          | Chase overdue invoices   | Resource.Created | 2026-06-20T09:00:00Z | Client onboarding |
      | task          | Draft Q3 invoice summary | Resource.Created | 2026-06-22T10:00:00Z |                   |
      | task          | Chase overdue invoices   | Resource.Updated | 2026-06-25T11:00:00Z | Client onboarding |
      | task          | Update contract terms    | Resource.Created | 2026-07-05T10:00:00Z | Client onboarding |
    When I call episodic_recall with the filters:
      | filter     | value                |
      | about      | Client onboarding    |
      | from       | 2026-06-15T00:00:00Z |
      | until      | 2026-06-30T00:00:00Z |
      | event-type | Resource.Created     |
    Then the call succeeds
    And the recall returns exactly these events in order:
      | resource               | event-type       |
      | Chase overdue invoices | Resource.Created |

  Scenario: Anchoring on a deleted resource still returns its history
    Given a "project" named "Client onboarding" exists
    And a "task" named "Chase overdue invoices" exists linked to the project "Client onboarding"
    And the project "Client onboarding" has since been deleted
    When I call episodic_recall for events about the resource named "Client onboarding"
    Then the call succeeds
    And the recall includes the "Resource.Created" event for "Client onboarding"
    And the recall includes the "Triple.Created" event for "Chase overdue invoices"

  Scenario: An anchored recall without an explicit limit returns the default page of 20 events
    Given the following activity is recorded in the event log:
      | resource-type | name       | event-type       | occurred-at          |
      | project       | Acme audit | Resource.Created | 2026-05-30T09:00:00Z |
    And 25 "task" resources linked to "Acme audit" were created on consecutive days starting "2026-06-01T09:00:00Z"
    When I call episodic_recall for events about the resource named "Acme audit"
    Then the call succeeds
    And the recall returns 20 events
    And the recall reports more events are available
    And the recall provides a cursor for the next page

  Scenario: The cursor continues an anchored recall without repeating events
    Given the following activity is recorded in the event log:
      | resource-type | name       | event-type       | occurred-at          |
      | project       | Acme audit | Resource.Created | 2026-05-30T09:00:00Z |
    And 25 "task" resources linked to "Acme audit" were created on consecutive days starting "2026-06-01T09:00:00Z"
    And I have recalled the first page of events about "Acme audit"
    When I call episodic_recall with the cursor from the first page
    Then the call succeeds
    And the recall returns 6 events
    And no event from the first page is repeated
    And the recall reports no more events are available

  Scenario: An anchor with no recorded events returns an empty result
    Given the following activity is recorded in the event log:
      | resource-type | name                   | event-type       | occurred-at          |
      | task          | Chase overdue invoices | Resource.Created | 2026-06-20T09:00:00Z |
    When I call episodic_recall for events about the resource "urn:project:2NqxYzWvUtSrQpOnMlKjIhGfEdC"
    Then the call succeeds
    And the recall returns no events
    And the recall reports no more events are available

  Scenario: A malformed anchor is rejected with a clear error
    When I call episodic_recall for events about the resource "not-a-urn"
    Then the call fails with a validation error
    And the error explains the resource URN is invalid
