@epic-wm-xtpky
Feature: Read an MCP resource_list call as flat projection rows
  As an LLM client connected to the WeOS MCP server
  I want a listing answered as projection rows instead of JSON-LD documents
  So that a large filtered result set costs a fraction of the tokens

  A resource_list call answers with each resource's JSON-LD document, which carries its @context,
  its entity node and its edges. A client that wants to count the open tasks or add up an amount
  pays for all of that and then reads one property per resource. Asking for flat rows answers with
  the projection row instead: the columns the projection keeps, named the way the projection read
  names them, and nothing else.

  Flat rows are a different shape for the same listing, not a different listing. The conditions
  from the filter story apply unchanged, a condition the projection cannot apply is refused the
  same way, and the cursor pages a flat listing exactly as it pages a JSON-LD one.

  A type whose resources have no projection table has no rows to return. Such a call answers from
  the entity path rather than failing — the same fallback the REST listing makes — and it keeps the
  conditions it was given, so a listing that cannot be answered from a projection narrows the same
  way rather than quietly returning everything.

  Asking for nothing is unchanged: a call that does not ask for flat rows, or asks for them and
  turns them off, answers with JSON-LD documents the way it did before this story.

  Background:
    Given a clean WeOS knowledge graph
    And the "tasks" preset is installed

  @story-wm-xtpky.2
  Scenario: A flat listing answers with projection rows instead of JSON-LD documents
    Given the following "task" resources exist:
      | name                     | status | priority |
      | Chase overdue invoices   | open   | high     |
      | Draft Q3 invoice summary | open   | normal   |
    When I call resource_list for type "task" asking for flat rows
    Then the call succeeds
    And the listing reports its results as rows
    And the listing reports no JSON-LD documents
    And the row for "Chase overdue invoices" carries these column values:
      | column   | value                  |
      | name     | Chase overdue invoices |
      | status   | open                   |
      | priority | high                   |

  @story-wm-xtpky.2
  Scenario: Every flat row carries the standard projection columns
    Given the following "task" resources exist:
      | name                   | status | priority |
      | Chase overdue invoices | open   | high     |
    When I call resource_list for type "task" asking for flat rows
    Then the call succeeds
    And every returned row carries these columns:
      | column     |
      | id         |
      | status     |
      | createdAt  |
      | createdBy  |
      | accountId  |
      | sequenceNo |
      | updatedAt  |

  @story-wm-xtpky.2
  Scenario: A flat listing of a type with no resources returns no rows
    When I call resource_list for type "task" asking for flat rows
    Then the call succeeds
    And the listing returns no rows

  @story-wm-xtpky.2
  Scenario: A flat listing reports a cursor while more rows remain
    Given 25 "task" resources with status "open" exist
    When I call resource_list for type "task" asking for flat rows
    Then the call succeeds
    And the listing returns 20 rows
    And the listing provides a cursor for the next page

  @story-wm-xtpky.2
  Scenario: The cursor continues a flat listing without repeating a row
    Given 25 "task" resources with status "open" exist
    And I have listed the first page of "task" flat rows
    When I call resource_list with the cursor from the first page asking for flat rows
    Then the call succeeds
    And the listing returns 5 rows
    And no row from the first page is repeated

  @story-wm-xtpky.2
  Scenario: A condition narrows a flat listing to the rows it matches
    Given the following "task" resources exist:
      | name                     | status | priority |
      | Chase overdue invoices   | open   | high     |
      | Draft Q3 invoice summary | open   | normal   |
      | Archive 2025 contracts   | done   | low      |
    When I call resource_list for type "task" asking for flat rows with the filters:
      | field  | operator | value |
      | status | eq       | open  |
    Then the call succeeds
    And the listing contains exactly these rows:
      | name                     |
      | Chase overdue invoices   |
      | Draft Q3 invoice summary |

  @story-wm-xtpky.2
  Scenario: The cursor continues a filtered flat listing with the condition still applied
    Given 25 "task" resources with status "open" exist
    And 5 "task" resources with status "done" exist
    And I have listed the first page of "task" flat rows with "status" equal to "open"
    When I call resource_list with the cursor from the first page and the same filters asking for flat rows
    Then the call succeeds
    And the listing returns 5 rows
    And every returned row has status "open"
    And no row from the first page is repeated

  @story-wm-xtpky.2
  Scenario: A condition on an unfilterable field is refused under flat rows as well
    Given the following "task" resources exist:
      | name                   | status | priority |
      | Chase overdue invoices | open   | high     |
    When I call resource_list for type "task" asking for flat rows with the filters:
      | field    | operator | value               |
      | assignee | eq       | dana@harborlegal.ex |
    Then the call fails with a validation error
    And the error names the field "assignee" as not filterable
    And no rows are returned

  @story-wm-xtpky.2
  Scenario: A type with no projection table answers from the entity path instead of failing
    Given a resource type "briefing" exists with no projection table
    And the following "briefing" resources exist:
      | name                      |
      | Monday partner briefing   |
      | Quarterly lender briefing |
    When I call resource_list for type "briefing" asking for flat rows
    Then the call succeeds
    And the listing reports JSON-LD documents rather than rows
    And the listing contains exactly these resources:
      | name                      |
      | Monday partner briefing   |
      | Quarterly lender briefing |

  @story-wm-xtpky.2
  Scenario: The fallback for a type with no projection table keeps the condition it was given
    Given a resource type "briefing" exists with no projection table
    And the "briefing" type declares a "status" property
    And the following "briefing" resources exist:
      | name                      | status |
      | Monday partner briefing   | open   |
      | Quarterly lender briefing | done   |
    When I call resource_list for type "briefing" asking for flat rows with the filters:
      | field  | operator | value |
      | status | eq       | open  |
    Then the call succeeds
    And the listing contains exactly these resources:
      | name                    |
      | Monday partner briefing |

  @story-wm-xtpky.2
  Scenario: A call that does not ask for flat rows answers with JSON-LD documents as before
    Given the following "task" resources exist:
      | name                     | status | priority |
      | Chase overdue invoices   | open   | high     |
      | Draft Q3 invoice summary | open   | normal   |
    When I call resource_list for type "task"
    Then the call succeeds
    And the listing reports JSON-LD documents rather than rows
    And the listing contains exactly these resources:
      | name                     |
      | Chase overdue invoices   |
      | Draft Q3 invoice summary |

  @story-wm-xtpky.2
  Scenario: A call that turns flat rows off answers with JSON-LD documents
    Given the following "task" resources exist:
      | name                   | status | priority |
      | Chase overdue invoices | open   | high     |
    When I call resource_list for type "task" with flat rows turned off
    Then the call succeeds
    And the listing reports JSON-LD documents rather than rows
    And the listing contains exactly these resources:
      | name                   |
      | Chase overdue invoices |
