@story-wm-irqzji
Feature: Narrowing a lexical memory search to one resource type
  As an LLM client connected to the WeOS MCP server
  I want to restrict a keyword search to a single resource type
  So that a crowded index cannot bury the kind of memory I asked for

  A memory_search call matches keywords against the text of every indexed resource, whatever its
  type. A client that wants only the matching tasks has to read the whole result and narrow it
  itself, and when the index is crowded the matches it wanted may not be in the result at all:
  the result limit is spent on higher-ranked resources of other types first.

  Naming a type on the call narrows the search instead. The search returns only resources of that
  type, and the limit is spent on that type alone, so what comes back is that type's best matches
  rather than whatever survived the unnarrowed ranking.

  The narrowing holds wherever the search runs. An instance with no full-text index answers from
  graph labels instead, and that degraded answer is narrowed the same way: a filter the instance
  quietly drops turns a smaller answer into a confidently wrong one.

  Naming a type the instance has nothing indexed under is not an error. The index is a derived
  view, so "no entries of that type" and "no such type" are the same observation from the search's
  side, and both are honestly reported as no matches.

  Background:
    Given a clean WeOS knowledge graph
    And the "tasks" preset is installed

  Scenario: A search narrowed to a type returns only resources of that type
    Given the following "project" resources exist:
      | name                    | description                        |
      | Harbor Legal onboarding | Invoice templates for Harbor Legal |
    And the following "task" resources exist:
      | name                   | description                          |
      | Chase overdue invoices | Harbor Legal invoice chase for June  |
      | Renew the office lease | Lease renewal, unrelated to billing  |
    When I call memory_search for "invoice" narrowed to type "task"
    Then the search returns exactly these resources:
      | name                   |
      | Chase overdue invoices |

  Scenario: A crowded index still returns the narrowed type's matches
    Given 25 "project" resources whose description mentions "invoice" exist
    And the following "task" resources exist:
      | name                   | description                         |
      | Chase overdue invoices | Harbor Legal invoice chase for June |
    When I call memory_search for "invoice" narrowed to type "task"
    Then the search returns exactly these resources:
      | name                   |
      | Chase overdue invoices |

  Scenario: A search that names no type returns matches of every type
    Given the following "project" resources exist:
      | name                    | description                        |
      | Harbor Legal onboarding | Invoice templates for Harbor Legal |
    And the following "task" resources exist:
      | name                   | description                         |
      | Chase overdue invoices | Harbor Legal invoice chase for June |
    When I call memory_search for "invoice"
    Then the search returns exactly these resources:
      | name                    |
      | Harbor Legal onboarding |
      | Chase overdue invoices  |

  Scenario: A type whose resources do not match returns no resources rather than the unnarrowed matches
    Given the following "project" resources exist:
      | name                    | description                        |
      | Harbor Legal onboarding | Invoice templates for Harbor Legal |
    And the following "task" resources exist:
      | name                   | description                       |
      | Renew the office lease | Lease renewal for the Harbor site |
    When I call memory_search for "invoice" narrowed to type "task"
    Then the call succeeds
    And the search returns no resources

  Scenario: A search narrowed to a type the instance has nothing indexed under returns no resources
    Given the following "task" resources exist:
      | name                   | description                         |
      | Chase overdue invoices | Harbor Legal invoice chase for June |
    When I call memory_search for "invoice" narrowed to type "recipe"
    Then the call succeeds
    And the search returns no resources

  Scenario: A narrowed search on an instance with no full-text index still returns only that type
    Given the instance has no full-text index available
    And the following "project" resources exist:
      | name                      |
      | Invoice templates rollout |
    And the following "task" resources exist:
      | name                   |
      | Chase overdue invoices |
    When I call memory_search for "invoice" narrowed to type "task"
    Then the search reports graph labels as its source
    And the search returns exactly these resources:
      | name                   |
      | Chase overdue invoices |
