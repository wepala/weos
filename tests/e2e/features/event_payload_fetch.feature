Feature: Fetch a full event payload by URN
  As an LLM client connected to the WeOS MCP server
  I want to fetch one event's full payload by its URN
  So that I can drill into a moment recall surfaced without paying for full payloads by default

  Background:
    Given a clean WeOS knowledge graph
    And the "tasks" preset is installed

  Scenario: Fetching an event by URN returns its full payload
    Given the following activity is recorded in the event log:
      | resource-type | name               | description                  | event-type       | occurred-at          |
      | project       | May reconciliation | Match receipts to the ledger | Resource.Created | 2026-06-20T09:00:00Z |
    When I call episodic_event_get with the URN of the "Resource.Created" event for "May reconciliation"
    Then the call succeeds
    And the fetched event carries:
      | field        |
      | event URN    |
      | event type   |
      | timestamp    |
      | aggregate ID |
    And the fetched payload contains the text "Match receipts to the ledger"

  Scenario: An event URN taken from a recall result fetches that same event
    Given the following activity is recorded in the event log:
      | resource-type | name                   | event-type       | occurred-at          |
      | task          | Chase overdue invoices | Resource.Created | 2026-06-20T09:00:00Z |
    When I call episodic_recall for events between "2026-06-01T00:00:00Z" and "2026-06-30T00:00:00Z"
    And I call episodic_event_get with the event URN from the first recalled event
    Then the call succeeds
    And the fetched event is the "Resource.Created" event for "Chase overdue invoices"

  Scenario: Fetching an event that does not exist is rejected with a clear error
    When I call episodic_event_get with the event "urn:event:2NqxYzWvUtSrQpOnMlKjIhGfEdC"
    Then the call fails
    And the error explains the event is unknown

  Scenario Outline: A fetch identifier that is not an event URN is rejected with a clear error
    When I call episodic_event_get with the event "<urn>"
    Then the call fails with a validation error
    And the error explains the identifier must be an event URN

    Examples:
      | urn                                  |
      | urn:task:2NqxYzWvUtSrQpOnMlKjIhGfEdC |
      | not-an-event-urn                     |
