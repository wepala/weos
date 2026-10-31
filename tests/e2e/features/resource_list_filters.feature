@epic-wm-xtpky
Feature: Filter an MCP resource_list call on projection columns
  As an LLM client connected to the WeOS MCP server
  I want to narrow a resource_list call with field conditions and be told when one cannot be applied
  So that I never build an answer on a condition the instance quietly dropped

  A resource_list call returns every resource of the type, so a client that wants only the open
  tasks has to read the whole page and narrow it itself. Conditions passed to the listing narrow
  it instead: each condition names a field, an operator and a value, and a resource is returned
  only when it satisfies every condition.

  Six operators are supported and no others: eq, ne, gt, gte, lt and lte. A field is filterable
  when it is one of the type's schema properties or one of the standard columns every projection
  carries: id, status, createdAt, createdBy, accountId, sequenceNo and updatedAt. Anything else
  is refused, and the refusal names what would have been accepted. That refusal is the point of
  this story: a condition the projection cannot apply used to be dropped, and the unfiltered page
  came back in its place — which a person notices and an LLM reports as a confident wrong answer.

  The JSON-LD keys @id and @type are schema properties but never projection columns, so they are
  not filterable either. A condition value arrives as a JSON string, number or boolean; a value
  with structure is refused. A type that declares no schema has only standard columns, so there
  is no property list to check its fields against and they are not checked.

  An eq condition on a reference field still matches a resource that holds the referenced
  identifier among several, which is how the existing REST listing behaves and is unchanged here.

  Background:
    Given a clean WeOS knowledge graph
    And the "tasks" preset is installed

  @story-wm-xtpky.1
  Scenario: A condition on a schema property returns only the resources it matches
    Given the following "task" resources exist:
      | name                     | status | priority |
      | Chase overdue invoices   | open   | high     |
      | Draft Q3 invoice summary | open   | normal   |
      | Archive 2025 contracts   | done   | low      |
    When I call resource_list for type "task" with the filters:
      | field  | operator | value |
      | status | eq       | open  |
    Then the call succeeds
    And the listing contains exactly these resources:
      | name                     |
      | Chase overdue invoices   |
      | Draft Q3 invoice summary |

  @story-wm-xtpky.1
  Scenario: Two conditions narrow the listing together
    Given the following "task" resources exist:
      | name                     | status | priority |
      | Chase overdue invoices   | open   | high     |
      | Draft Q3 invoice summary | open   | normal   |
      | Renew the office lease   | done   | high     |
      | Archive 2025 contracts   | done   | low      |
    When I call resource_list for type "task" with the filters:
      | field    | operator | value |
      | status   | eq       | open  |
      | priority | eq       | high  |
    Then the call succeeds
    And the listing contains exactly these resources:
      | name                   |
      | Chase overdue invoices |

  @story-wm-xtpky.1
  Scenario Outline: A comparison condition on a due date returns only the tasks it matches
    Given the following "task" resources exist:
      | name                     | status | dueDate    |
      | Chase overdue invoices   | open   | 2026-06-10 |
      | Draft Q3 invoice summary | open   | 2026-06-20 |
      | Archive 2025 contracts   | open   | 2026-06-30 |
    When I call resource_list for type "task" with the filters:
      | field   | operator   | value      |
      | dueDate | <operator> | 2026-06-20 |
    Then the call succeeds
    And the listing contains exactly the resources named "<expected>"

    Examples:
      | operator | expected                                         |
      | ne       | Chase overdue invoices, Archive 2025 contracts   |
      | gt       | Archive 2025 contracts                           |
      | gte      | Draft Q3 invoice summary, Archive 2025 contracts |
      | lt       | Chase overdue invoices                           |
      | lte      | Chase overdue invoices, Draft Q3 invoice summary |

  @story-wm-xtpky.1
  Scenario: A condition that matches nothing returns an empty listing rather than every resource
    Given the following "task" resources exist:
      | name                     | status | priority |
      | Chase overdue invoices   | open   | high     |
      | Draft Q3 invoice summary | open   | normal   |
    When I call resource_list for type "task" with the filters:
      | field  | operator | value   |
      | status | eq       | blocked |
    Then the call succeeds
    And the listing returns no resources

  @story-wm-xtpky.1
  Scenario: A condition on a field the projection has no column for is refused rather than ignored
    Given the following "task" resources exist:
      | name                     | status | priority |
      | Chase overdue invoices   | open   | high     |
      | Draft Q3 invoice summary | open   | normal   |
    When I call resource_list for type "task" with the filters:
      | field    | operator | value               |
      | assignee | eq       | dana@harborlegal.ex |
    Then the call fails with a validation error
    And the error names the field "assignee" as not filterable
    And no resources are returned

  @story-wm-xtpky.1
  Scenario: The refusal for an unfilterable field lists the fields the type accepts
    When I call resource_list for type "task" with the filters:
      | field    | operator | value               |
      | assignee | eq       | dana@harborlegal.ex |
    Then the call fails with a validation error
    And the error lists these filterable fields:
      | field       |
      | name        |
      | description |
      | status      |
      | priority    |
      | dueDate     |
      | project     |
      | id          |
      | createdAt   |
      | createdBy   |
      | accountId   |
      | sequenceNo  |
      | updatedAt   |

  @story-wm-xtpky.1
  Scenario Outline: A condition on a JSON-LD key is refused because it is not a projection column
    When I call resource_list for type "task" with the filters:
      | field | operator | value   |
      | <key> | eq       | <value> |
    Then the call fails with a validation error
    And the error names the field "<key>" as not filterable

    Examples:
      | key   | value                           |
      | @id   | urn:weos:resource:task:4f1ac8d2 |
      | @type | https://schema.org/Action       |

  @story-wm-xtpky.1
  Scenario: An unsupported operator is refused and the refusal names the operators that are supported
    Given the following "task" resources exist:
      | name                   | status | priority |
      | Chase overdue invoices | open   | high     |
    When I call resource_list for type "task" with the filters:
      | field  | operator | value |
      | status | contains | ope   |
    Then the call fails with a validation error
    And the error names the supported operators:
      | operator |
      | eq       |
      | ne       |
      | gt       |
      | gte      |
      | lt       |
      | lte      |
    And no resources are returned

  @story-wm-xtpky.1
  Scenario: A condition on a standard column the type's schema does not declare narrows the listing
    Given the following "task" resources exist:
      | name                     | status | created-at           |
      | Archive 2025 contracts   | done   | 2026-03-02T09:00:00Z |
      | Chase overdue invoices   | open   | 2026-06-20T09:00:00Z |
      | Draft Q3 invoice summary | open   | 2026-06-24T11:00:00Z |
    When I call resource_list for type "task" with the filters:
      | field     | operator | value                |
      | createdAt | gte      | 2026-06-01T00:00:00Z |
    Then the call succeeds
    And the listing contains exactly these resources:
      | name                     |
      | Chase overdue invoices   |
      | Draft Q3 invoice summary |

  @story-wm-xtpky.1
  Scenario: An eq condition on a reference field matches a task that holds that reference
    Given a "project" resource named "Client onboarding" exists
    And the following "task" resources exist:
      | name                   | status | project           |
      | Chase overdue invoices | open   | Client onboarding |
      | Archive 2025 contracts | done   |                   |
    When I call resource_list for type "task" with an "eq" condition on "project" matching "Client onboarding"
    Then the call succeeds
    And the listing contains exactly these resources:
      | name                   |
      | Chase overdue invoices |

  @story-wm-xtpky.1
  Scenario: A type that declares no JSON Schema is not refused a condition on an undeclared field
    Given a resource type "scratchpad" exists with no JSON Schema
    When I call resource_list for type "scratchpad" with the filters:
      | field | operator | value   |
      | topic | eq       | pricing |
    Then the tool does not refuse the condition as an unfilterable field

  @story-wm-xtpky.1
  Scenario: A call with no conditions returns every resource of the type
    Given the following "task" resources exist:
      | name                     | status | priority |
      | Chase overdue invoices   | open   | high     |
      | Draft Q3 invoice summary | open   | normal   |
      | Archive 2025 contracts   | done   | low      |
    When I call resource_list for type "task"
    Then the call succeeds
    And the listing contains exactly these resources:
      | name                     |
      | Chase overdue invoices   |
      | Draft Q3 invoice summary |
      | Archive 2025 contracts   |

  @story-wm-xtpky.1
  Scenario: An empty list of conditions returns every resource of the type
    Given the following "task" resources exist:
      | name                     | status | priority |
      | Chase overdue invoices   | open   | high     |
      | Draft Q3 invoice summary | open   | normal   |
      | Archive 2025 contracts   | done   | low      |
    When I call resource_list for type "task" with the JSON filters:
      """
      []
      """
    Then the call succeeds
    And the listing contains exactly these resources:
      | name                     |
      | Chase overdue invoices   |
      | Draft Q3 invoice summary |
      | Archive 2025 contracts   |

  @story-wm-xtpky.1
  Scenario: A filtered listing reports a cursor while more matches remain
    Given 25 "task" resources with status "open" exist
    And 5 "task" resources with status "done" exist
    When I call resource_list for type "task" with the filters:
      | field  | operator | value |
      | status | eq       | open  |
    Then the call succeeds
    And the listing returns 20 resources
    And every returned resource has status "open"
    And the listing provides a cursor for the next page

  @story-wm-xtpky.1
  Scenario: The cursor continues a filtered listing with the condition still applied
    Given 25 "task" resources with status "open" exist
    And 5 "task" resources with status "done" exist
    And I have listed the first page of "task" resources with "status" equal to "open"
    When I call resource_list with the cursor from the first page and the same filters
    Then the call succeeds
    And the listing returns 5 resources
    And every returned resource has status "open"
    And no resource from the first page is repeated

  @story-wm-xtpky.1
  Scenario: A condition value given as a JSON number matches the stored number
    Given a resource type "invoice" exists with a numeric "amountDue" property
    And the following "invoice" resources exist:
      | name                   | amountDue |
      | Harbor Legal June fees | 1250      |
      | Harbor Legal July fees | 980       |
    When I call resource_list for type "invoice" with the JSON filters:
      """
      [{"field": "amountDue", "operator": "gte", "value": 1000}]
      """
    Then the call succeeds
    And the listing contains exactly these resources:
      | name                   |
      | Harbor Legal June fees |

  @story-wm-xtpky.1
  Scenario: A condition value given as a JSON boolean matches the stored boolean
    Given a resource type "invoice" exists with a boolean "paid" property
    And the following "invoice" resources exist:
      | name                   | paid  |
      | Harbor Legal June fees | true  |
      | Harbor Legal July fees | false |
    When I call resource_list for type "invoice" with the JSON filters:
      """
      [{"field": "paid", "operator": "eq", "value": true}]
      """
    Then the call succeeds
    And the listing contains exactly these resources:
      | name                   |
      | Harbor Legal June fees |

  @story-wm-xtpky.1
  Scenario Outline: A condition value that is not a single value is refused
    When I call resource_list for type "task" with the JSON filters:
      """
      [{"field": "status", "operator": "eq", "value": <value>}]
      """
    Then the call fails with a validation error
    And the error states that a condition value must be a string, a number or a boolean

    Examples:
      | value             |
      | {"state": "open"} |
      | ["open", "done"]  |
