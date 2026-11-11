@postgres @story-wm-irqzji
Feature: Full-text lexical search on PostgreSQL
  As an operator running WeOS on PostgreSQL
  I want keyword search to read a real full-text index
  So that my twin answers from the text of its memories and not from their labels alone

  On SQLite the lexical index is an FTS5 table, and memory_search matches every text literal a
  resource carries. On PostgreSQL there was no index at all: the search degraded to the knowledge
  graph's label search, which only ever sees a resource's label. A word that appears in a
  description and nowhere else was therefore unfindable on PostgreSQL, and the operator had no
  signal that the answer was built from less than the stored text.

  PostgreSQL gets its own full-text index here. The search reports the full-text index as its
  source, matches the same text SQLite matches, and ranks its hits. The two engines index the same
  resources with the same text for the same events, which is what makes a rebuild a rebuild rather
  than a migration; the order the hits come back in is each engine's own and is not compared.

  A query is user text, never query language. Full-text operator syntax — ampersand, pipe, bang,
  prefix stars, quotes, and the word NEAR — is matched as the words it is made of, so a question
  typed by a person can neither error nor quietly mean something else.

  The index is reached without the knowledge graph. Lexical search on PostgreSQL answers from the
  index alone, so an instance with no graph store configured still searches its memories.

  These scenarios need a real PostgreSQL database. They are skipped where none is configured.

  Background:
    Given a clean WeOS instance backed by a PostgreSQL database
    And the "tasks" preset is installed

  Scenario: A PostgreSQL instance answers from the full-text index rather than from graph labels
    Given the following "task" resources exist:
      | name                   | description                         |
      | Chase overdue invoices | Harbor Legal invoice chase for June |
    When I call memory_search for "invoice"
    Then the search reports the full-text index as its source
    And the search returns exactly these resources:
      | name                   |
      | Chase overdue invoices |

  Scenario: A word that appears only in a description is found
    Given the following "task" resources exist:
      | name                   | description                                     |
      | Chase overdue invoices | Dana at Harbor Legal confirmed the June balance |
      | Renew the office lease | Lease renewal for the Wepala studio             |
    When I call memory_search for "Harbor"
    Then the search returns exactly these resources:
      | name                   |
      | Chase overdue invoices |

  Scenario: The resource that mentions the query words more often is returned first
    Given the following "task" resources exist:
      | name                     | description                                                   |
      | Chase overdue invoices   | Harbor Legal invoice chase; the Harbor Legal invoice is late  |
      | Draft Q3 invoice summary | One line for Harbor Legal, with the invoice total for Q3      |
    When I call memory_search for "Harbor invoice"
    Then the search returns these resources in order:
      | name                     |
      | Chase overdue invoices   |
      | Draft Q3 invoice summary |

  Scenario: A resource that matches only one of the query words is not returned
    Given the following "task" resources exist:
      | name                     | description                         |
      | Chase overdue invoices   | Harbor Legal invoice chase for June |
      | Draft Q3 invoice summary | Finance review summary for Q3       |
    When I call memory_search for "Harbor invoice"
    Then the search returns exactly these resources:
      | name                   |
      | Chase overdue invoices |

  Scenario: Lexical search answers on an instance with no knowledge graph configured
    Given the instance runs with no knowledge graph configured
    And the following "task" resources exist:
      | name                   | description                         |
      | Chase overdue invoices | Harbor Legal invoice chase for June |
    When I call memory_search for "Harbor"
    Then the search reports the full-text index as its source
    And the search returns exactly these resources:
      | name                   |
      | Chase overdue invoices |

  Scenario Outline: A query carrying full-text operator syntax is matched as plain words
    Given the following "task" resources exist:
      | name                   | description                         |
      | Chase overdue invoices | Harbor Legal invoice chase for June |
    When I call memory_search for "<query>"
    Then the call succeeds
    And the search returns exactly these resources:
      | name                   |
      | Chase overdue invoices |

    Examples:
      | query             |
      | Harbor & invoice  |
      | Harbor \| invoice |
      | Harbor:*          |
      | !Harbor           |

  Scenario: A query wrapped in double quotes is matched as the words inside them
    Given the following "task" resources exist:
      | name                   | description                         |
      | Chase overdue invoices | Harbor Legal invoice chase for June |
    When I call memory_search for the query:
      """
      "Harbor invoice"
      """
    Then the call succeeds
    And the search returns exactly these resources:
      | name                   |
      | Chase overdue invoices |

  Scenario: An operator name in a query is matched as a word rather than obeyed
    Given the following "task" resources exist:
      | name                   | description                         |
      | Chase overdue invoices | Harbor Legal invoice chase for June |
    When I call memory_search for "Harbor NEAR invoice"
    Then the call succeeds
    And the search returns no resources

  Scenario: A query of operator characters alone matches nothing rather than everything
    Given the following "task" resources exist:
      | name                   | description                         |
      | Chase overdue invoices | Harbor Legal invoice chase for June |
    When I call memory_search for "& | ! :*"
    Then the call succeeds
    And the search returns no resources

  Scenario: A search before anything has been indexed returns no resources
    When I call memory_search for "invoice"
    Then the call succeeds
    And the search returns no resources

  Scenario: Text removed from a resource stops matching a search
    Given the following "task" resources exist:
      | name                   | description                         |
      | Chase overdue invoices | Harbor Legal invoice chase for June |
    And the "task" resource "Chase overdue invoices" is updated with the description "Harbor Legal invoice settled"
    When I call memory_search for "June"
    Then the call succeeds
    And the search returns no resources

  Scenario: Rebuilding the index from the event history restores the same matches
    Given the following "task" resources exist:
      | name                   | description                         |
      | Chase overdue invoices | Harbor Legal invoice chase for June |
      | Renew the office lease | Lease renewal for the Harbor studio |
    And the lexical index is rebuilt from the event history
    When I call memory_search for "Harbor"
    Then the search returns exactly these resources:
      | name                   |
      | Chase overdue invoices |
      | Renew the office lease |

  Scenario: A rebuilt PostgreSQL index holds the entries a SQLite index builds from the same events
    Given the following "task" resources exist:
      | name                   | description                         |
      | Chase overdue invoices | Harbor Legal invoice chase for June |
      | Renew the office lease | Lease renewal for the Harbor studio |
    When the lexical index is rebuilt from the event history
    Then the index holds an entry for each resource a SQLite instance indexes from the same events
    And each entry holds the same indexed text as the SQLite instance holds for that resource

  Scenario: A resource deleted before the rebuild is absent from the rebuilt index
    Given the following "task" resources exist:
      | name                   | description                         |
      | Chase overdue invoices | Harbor Legal invoice chase for June |
      | Renew the office lease | Lease renewal for the Harbor studio |
    And the "task" resource "Renew the office lease" is deleted
    And the lexical index is rebuilt from the event history
    When I call memory_search for "Harbor"
    Then the search returns exactly these resources:
      | name                   |
      | Chase overdue invoices |

  Scenario: A superseded fact is absent from the rebuilt index
    Given the "memory" preset is installed
    And a recorded fact "lease" stating "The Harbor studio lease renews in June" about "urn:org:wepala"
    And I record a fact superseding "lease" stating "The Harbor studio lease renews in September"
    And the lexical index is rebuilt from the event history
    When I call memory_search for "June"
    Then the call succeeds
    And the search returns no resources

  Scenario: A search narrowed to a type returns only resources of that type on PostgreSQL
    Given the following "project" resources exist:
      | name                    | description                        |
      | Harbor Legal onboarding | Invoice templates for Harbor Legal |
    And the following "task" resources exist:
      | name                   | description                         |
      | Chase overdue invoices | Harbor Legal invoice chase for June |
    When I call memory_search for "invoice" narrowed to type "task"
    Then the search returns exactly these resources:
      | name                   |
      | Chase overdue invoices |

  Scenario: A crowded PostgreSQL index still returns the narrowed type's matches
    Given 25 "project" resources whose description mentions "invoice" exist
    And the following "task" resources exist:
      | name                   | description                         |
      | Chase overdue invoices | Harbor Legal invoice chase for June |
    When I call memory_search for "invoice" narrowed to type "task"
    Then the search returns exactly these resources:
      | name                   |
      | Chase overdue invoices |
