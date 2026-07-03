Feature: Query the event log via the MCP episodic_recall tool
  As an LLM client connected to the WeOS MCP server
  I want to query the event log by time window and combinable filters
  So that I can answer "what happened between X and Y" from the twin's history

  Background:
    Given a clean WeOS knowledge graph
    And the "tasks" preset is installed

  Scenario: A time window returns matching events in chronological order
    Given the following activity is recorded in the event log:
      | resource-type | name                     | event-type       | occurred-at          |
      | project       | Client onboarding        | Resource.Created | 2026-06-10T09:00:00Z |
      | task          | Draft Q3 invoice summary | Resource.Created | 2026-06-20T09:00:00Z |
      | task          | Chase overdue invoices   | Resource.Created | 2026-06-28T14:30:00Z |
    When I call episodic_recall for events between "2026-06-15T00:00:00Z" and "2026-06-30T00:00:00Z"
    Then the call succeeds
    And the recall returns exactly these events in order:
      | resource                 | event-type       |
      | Draft Q3 invoice summary | Resource.Created |
      | Chase overdue invoices   | Resource.Created |

  Scenario: Recalled events use the compact shape instead of the full payload
    Given the following activity is recorded in the event log:
      | resource-type | name               | description                   | event-type       | occurred-at          |
      | project       | May reconciliation | Match receipts to the ledger  | Resource.Created | 2026-06-20T09:00:00Z |
    When I call episodic_recall for events between "2026-06-01T00:00:00Z" and "2026-06-30T00:00:00Z"
    Then the call succeeds
    And every recalled event carries:
      | field           |
      | event URN       |
      | event type      |
      | timestamp       |
      | aggregate ID    |
      | payload summary |
    And the recall does not include the full payload text "Match receipts to the ledger"

  Scenario: Filtering by aggregate ID returns only that resource's events
    Given the following activity is recorded in the event log:
      | resource-type | name                     | event-type       | occurred-at          |
      | task          | Draft Q3 invoice summary | Resource.Created | 2026-06-20T09:00:00Z |
      | task          | Chase overdue invoices   | Resource.Created | 2026-06-21T10:00:00Z |
      | task          | Chase overdue invoices   | Resource.Updated | 2026-06-25T11:00:00Z |
    When I call episodic_recall for events about the resource named "Chase overdue invoices"
    Then the call succeeds
    And the recall returns exactly these events in order:
      | resource               | event-type       |
      | Chase overdue invoices | Resource.Created |
      | Chase overdue invoices | Resource.Updated |

  Scenario: Filtering by event type returns only events of that type
    Given the following activity is recorded in the event log:
      | resource-type | name                     | event-type       | occurred-at          |
      | task          | Draft Q3 invoice summary | Resource.Created | 2026-06-20T09:00:00Z |
      | task          | Draft Q3 invoice summary | Resource.Updated | 2026-06-22T15:00:00Z |
      | task          | Chase overdue invoices   | Resource.Created | 2026-06-23T10:00:00Z |
    When I call episodic_recall for events of type "Resource.Updated"
    Then the call succeeds
    And every recalled event has type "Resource.Updated"
    And the recall includes the "Resource.Updated" event for "Draft Q3 invoice summary"

  Scenario: Filtering by resource type returns only that type's events
    Given the following activity is recorded in the event log:
      | resource-type | name                   | event-type       | occurred-at          |
      | project       | Client onboarding      | Resource.Created | 2026-06-10T09:00:00Z |
      | task          | Chase overdue invoices | Resource.Created | 2026-06-20T09:00:00Z |
    When I call episodic_recall for events of resource type "task"
    Then the call succeeds
    And every recalled event is for a "task" resource
    And the recall includes the "Resource.Created" event for "Chase overdue invoices"
    And the recall does not include an event for "Client onboarding"

  Scenario: Combined filters narrow the results together
    Given the following activity is recorded in the event log:
      | resource-type | name                     | event-type       | occurred-at          |
      | project       | Client onboarding        | Resource.Created | 2026-06-16T09:00:00Z |
      | task          | Draft Q3 invoice summary | Resource.Created | 2026-06-10T09:00:00Z |
      | task          | Draft Q3 invoice summary | Resource.Updated | 2026-06-20T09:00:00Z |
      | task          | Chase overdue invoices   | Resource.Created | 2026-06-21T10:00:00Z |
    When I call episodic_recall with the filters:
      | filter        | value                |
      | from          | 2026-06-15T00:00:00Z |
      | until         | 2026-06-30T00:00:00Z |
      | event-type    | Resource.Created     |
      | resource-type | task                 |
    Then the call succeeds
    And the recall returns exactly these events in order:
      | resource               | event-type       |
      | Chase overdue invoices | Resource.Created |

  Scenario: A relative time range returns only events inside the window
    Given the following activity is recorded in the event log:
      | resource-type | name                   | event-type       | occurred-at |
      | task          | Archive 2025 contracts | Resource.Created | 10 days ago |
      | task          | Chase overdue invoices | Resource.Created | 2 days ago  |
    When I call episodic_recall for events from the last 7 days
    Then the call succeeds
    And the recall includes the "Resource.Created" event for "Chase overdue invoices"
    And the recall does not include an event for "Archive 2025 contracts"

  Scenario: A recall without an explicit limit returns the default page of 20 events
    Given 25 "task" resources were created on consecutive days starting "2026-06-01T09:00:00Z"
    When I call episodic_recall for events between "2026-06-01T00:00:00Z" and "2026-07-01T00:00:00Z"
    Then the call succeeds
    And the recall returns 20 events
    And the recall reports more events are available
    And the recall provides a cursor for the next page

  Scenario: The cursor returns the next page without repeating events
    Given 25 "task" resources were created on consecutive days starting "2026-06-01T09:00:00Z"
    And I have recalled the first page of events between "2026-06-01T00:00:00Z" and "2026-07-01T00:00:00Z"
    When I call episodic_recall with the cursor from the first page
    Then the call succeeds
    And the recall returns 5 events
    And no event from the first page is repeated
    And the recall reports no more events are available

  Scenario: A limit above the hard maximum returns at most 100 events
    Given 105 "task" resources were created on consecutive days starting "2026-03-01T09:00:00Z"
    When I call episodic_recall requesting up to 500 events
    Then the call succeeds
    And the recall returns 100 events
    And the recall reports more events are available

  Scenario: A window with no recorded activity returns an empty result
    Given the following activity is recorded in the event log:
      | resource-type | name                   | event-type       | occurred-at          |
      | task          | Chase overdue invoices | Resource.Created | 2026-06-20T09:00:00Z |
    When I call episodic_recall for events between "2026-01-01T00:00:00Z" and "2026-01-31T23:59:59Z"
    Then the call succeeds
    And the recall returns no events
    And the recall reports no more events are available

  Scenario Outline: An invalid time range is rejected with a clear error
    When I call episodic_recall for events between "<from>" and "<until>"
    Then the call fails with a validation error
    And the error explains the time range is invalid

    Examples:
      | from                 | until                |
      | 2026-06-30T00:00:00Z | 2026-06-01T00:00:00Z |
      | not-a-timestamp      | 2026-06-30T00:00:00Z |

  Scenario: The same query returns identical results every time
    Given the following activity is recorded in the event log:
      | resource-type | name                     | event-type       | occurred-at          |
      | task          | Draft Q3 invoice summary | Resource.Created | 2026-06-20T09:00:00Z |
      | task          | Chase overdue invoices   | Resource.Created | 2026-06-20T09:00:00Z |
    When I call episodic_recall for events between "2026-06-01T00:00:00Z" and "2026-06-30T00:00:00Z" twice
    Then both recalls return identical events in identical order

  Scenario: An invalid pagination cursor is rejected with a clear error
    When I call episodic_recall with the cursor "not-a-cursor"
    Then the call fails with a validation error
    And the error explains the cursor is invalid

  Scenario: A resource created through the live resource_create tool appears in recall
    When I call resource_create for type "task" with the data:
      """
      {
        "name": "Chase overdue invoices",
        "status": "open",
        "priority": "high"
      }
      """
    And I call episodic_recall for events from the last 1 days
    Then the call succeeds
    And the recall includes the "Resource.Created" event for "Chase overdue invoices"
    And the payload summary for that event carries "Chase overdue invoices"
