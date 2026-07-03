Feature: Similar-event search from a seed event
  As an LLM client connected to the WeOS MCP server
  I want events ranked by structural similarity to a seed event
  So that I can surface comparable past activity from the twin's history

  Background:
    Given a clean WeOS knowledge graph
    And the "tasks" preset is installed

  Scenario: Events sharing a referenced resource outrank events that are merely the same kind
    Given the following activity is recorded in the event log:
      | resource-type | name                     | event-type       | occurred-at          | project           |
      | project       | Client onboarding        | Resource.Created | 2026-06-01T09:00:00Z |                   |
      | project       | Website refresh          | Resource.Created | 2026-06-11T09:00:00Z |                   |
      | task          | Update contract terms    | Resource.Created | 2026-06-02T09:00:00Z | Client onboarding |
      | task          | Draft Q3 invoice summary | Resource.Created | 2026-06-09T09:00:00Z |                   |
      | task          | Chase overdue invoices   | Resource.Created | 2026-06-10T09:00:00Z | Client onboarding |
    When I call episodic_similar seeded with the "Resource.Created" event for "Chase overdue invoices"
    Then the call succeeds
    And the results do not include the seed event
    And the results list exactly these events in order:
      | resource                 | event-type       |
      | Update contract terms    | Resource.Created |
      | Client onboarding        | Resource.Created |
      | Draft Q3 invoice summary | Resource.Created |
      | Website refresh          | Resource.Created |

  Scenario: Temporal proximity breaks the tie between otherwise equal events
    Given the following activity is recorded in the event log:
      | resource-type | name                     | event-type       | occurred-at          | project           |
      | project       | Client onboarding        | Resource.Created | 2026-06-01T08:00:00Z |                   |
      | task          | Update contract terms    | Resource.Created | 2026-06-01T09:00:00Z | Client onboarding |
      | task          | Schedule onboarding call | Resource.Created | 2026-06-14T09:00:00Z | Client onboarding |
      | task          | Chase overdue invoices   | Resource.Created | 2026-06-15T09:00:00Z | Client onboarding |
    When I call episodic_similar seeded with the "Resource.Created" event for "Chase overdue invoices"
    Then the call succeeds
    And the results list exactly these events in order:
      | resource                 | event-type       |
      | Schedule onboarding call | Resource.Created |
      | Update contract terms    | Resource.Created |
      | Client onboarding        | Resource.Created |

  Scenario: A seed that shares no references still returns ranked results
    Given the following activity is recorded in the event log:
      | resource-type | name                     | event-type       | occurred-at          |
      | task          | Update contract terms    | Resource.Created | 2026-06-09T09:00:00Z |
      | task          | Draft Q3 invoice summary | Resource.Created | 2026-06-10T09:00:00Z |
      | project       | Website refresh          | Resource.Created | 2026-06-11T09:00:00Z |
    When I call episodic_similar seeded with the "Resource.Created" event for "Draft Q3 invoice summary"
    Then the call succeeds
    And the results list exactly these events in order:
      | resource              | event-type       |
      | Update contract terms | Resource.Created |
      | Website refresh       | Resource.Created |

  Scenario: The same seed returns the identical ranking every time, even for tied events
    Given the following activity is recorded in the event log:
      | resource-type | name                     | event-type       | occurred-at          | project           |
      | project       | Client onboarding        | Resource.Created | 2026-06-01T09:00:00Z |                   |
      | task          | Chase overdue invoices   | Resource.Created | 2026-06-10T09:00:00Z | Client onboarding |
      | task          | Update contract terms    | Resource.Created | 2026-06-12T09:00:00Z | Client onboarding |
      | task          | Schedule onboarding call | Resource.Created | 2026-06-12T09:00:00Z | Client onboarding |
    When I call episodic_similar seeded with the "Resource.Created" event for "Chase overdue invoices" twice
    Then both searches return identical events in identical order

  Scenario: Results use the compact event shape ordered from most to least similar
    Given the following activity is recorded in the event log:
      | resource-type | name                   | event-type       | occurred-at          | project           |
      | project       | Client onboarding      | Resource.Created | 2026-06-01T09:00:00Z |                   |
      | task          | Chase overdue invoices | Resource.Created | 2026-06-10T09:00:00Z | Client onboarding |
      | task          | Update contract terms  | Resource.Created | 2026-06-12T09:00:00Z | Client onboarding |
    When I call episodic_similar seeded with the "Resource.Created" event for "Chase overdue invoices"
    Then the call succeeds
    And every result carries:
      | field                |
      | event URN            |
      | event type           |
      | timestamp            |
      | aggregate ID         |
      | payload summary      |
      | referenced resources |
      | similarity score     |
    And the results are ordered from most to least similar

  Scenario: A similarity search without an explicit limit returns at most 20 events
    Given the following activity is recorded in the event log:
      | resource-type | name       | event-type       | occurred-at          |
      | project       | Acme audit | Resource.Created | 2026-05-30T09:00:00Z |
    And 25 "task" resources linked to "Acme audit" were created on consecutive days starting "2026-06-01T09:00:00Z"
    When I call episodic_similar seeded with the "Resource.Created" event for "Acme audit"
    Then the call succeeds
    And the results contain 20 events

  Scenario: A limit above the hard maximum returns at most 100 events
    Given the following activity is recorded in the event log:
      | resource-type | name       | event-type       | occurred-at          |
      | project       | Acme audit | Resource.Created | 2026-02-28T09:00:00Z |
    And 105 "task" resources linked to "Acme audit" were created on consecutive days starting "2026-03-01T09:00:00Z"
    When I call episodic_similar seeded with the "Resource.Created" event for "Acme audit" requesting up to 500 events
    Then the call succeeds
    And the results contain 100 events

  Scenario: A seed event that does not exist is rejected with a clear error
    When I call episodic_similar seeded with the event "urn:event:2NqxYzWvUtSrQpOnMlKjIhGfEdC"
    Then the call fails
    And the error explains the seed event is unknown

  Scenario Outline: A seed that is not an event URN is rejected with a clear error
    When I call episodic_similar seeded with the event "<seed>"
    Then the call fails with a validation error
    And the error explains the seed must be an event URN

    Examples:
      | seed                                 |
      | urn:task:2NqxYzWvUtSrQpOnMlKjIhGfEdC |
      | not-an-event-urn                     |
