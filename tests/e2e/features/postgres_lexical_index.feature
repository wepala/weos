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

  The index is one table for every account the instance serves, so on this engine too the search
  is scoped to what the caller may read: resources the caller created, ownerless resources, and
  resources shared with the caller by a read grant, with no wider reach for an account admin.
  Another account's matching resource is neither returned nor quoted back, and the caller's own
  match is not pushed out of a crowded result by it. The wider property, and the SQLite half of
  it, is specified in lexical_search_account_scope.feature; the scenarios here pin the PostgreSQL
  query that applies the scope.

  The "any" match mode is built on this engine's own query. The stop words come off the query by
  the same core list SQLite uses, the remaining content words are OR-ed into the tsquery rather
  than AND-ed, and the hits are ranked by how many distinct content words each one holds before
  the engine's own relevance is consulted. The type filter and the caller's scope still sit in the
  same WHERE clause, so they still narrow the match rather than the answer. The wider property,
  and the SQLite half of it, is in lexical_search_any_match.feature.

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

  Scenario: Another account's matching resource is not returned on PostgreSQL
    Given account "Harbor Legal" has the following "task" resources:
      | name                   | description                                  |
      | Chase overdue invoices | Dunmore settlement balance still outstanding |
    When account "Cedar Realty" calls memory_search for "Dunmore"
    Then the call succeeds
    And the search returns no resources
    And the search returns no text from the "task" resource "Chase overdue invoices"

  Scenario: The searching account still finds its own resource on PostgreSQL
    Given account "Harbor Legal" has the following "task" resources:
      | name                   | description                                  |
      | Chase overdue invoices | Dunmore settlement balance still outstanding |
    When account "Harbor Legal" calls memory_search for "Dunmore"
    Then the search reports the full-text index as its source
    And the search returns exactly these resources:
      | name                   |
      | Chase overdue invoices |

  Scenario: A crowded PostgreSQL index still returns the searching account's own match
    Given account "Harbor Legal" has 25 "task" resources whose description mentions "Dunmore"
    And account "Cedar Realty" has the following "task" resources:
      | name                             | description                         |
      | Prepare the Dunmore Road showing | Dunmore Road viewing for the Patels |
    When account "Cedar Realty" calls memory_search for "Dunmore"
    Then the search returns exactly these resources:
      | name                             |
      | Prepare the Dunmore Road showing |

  Scenario: A PostgreSQL search narrowed to a type still excludes another account's resource
    Given account "Harbor Legal" has the following "task" resources:
      | name                   | description                                  |
      | Chase overdue invoices | Dunmore settlement balance still outstanding |
    And account "Cedar Realty" has the following "project" resources:
      | name                    | description                           |
      | Dunmore Road onboarding | Dunmore paperwork for the new listing |
    When account "Cedar Realty" calls memory_search for "Dunmore" narrowed to type "task"
    Then the call succeeds
    And the search returns no resources

  @story-wm-u1xp4w
  Scenario: A PostgreSQL sentence query returns the resource holding the most of its words first
    Given the following "task" resources exist:
      | name                     | description                                   |
      | Chase the Harbor invoice | Harbor Legal invoice chase for the June lease |
      | Harbor Legal onboarding  | Welcome pack for the new Legal client         |
      | Renew the office lease   | Lease renewal for the Wepala studio           |
    When I call memory_search for "the invoice for the Harbor Legal lease" matching any word
    Then the search returns these resources in order:
      | name                     |
      | Chase the Harbor invoice |
      | Harbor Legal onboarding  |
      | Renew the office lease   |

  @story-wm-u1xp4w
  Scenario: A PostgreSQL query of stop words alone returns no resources
    Given the following "task" resources exist:
      | name                     | description                                   |
      | Chase the Harbor invoice | Harbor Legal invoice chase for the June lease |
    When I call memory_search for "what is it about" matching any word
    Then the call succeeds
    And the search returns no resources

  @story-wm-u1xp4w
  Scenario: A PostgreSQL query of a contraction alone returns no resources
    Given the following "task" resources exist:
      | name                    | description                                        |
      | Help Dana log in        | Reset the child account password so she can log in |
      | Order the office chairs | You can collect them from the studio               |
    When I call memory_search for "can't" matching any word
    Then the call succeeds
    And the search returns no resources

  @story-wm-u1xp4w
  Scenario: A PostgreSQL search requiring two matching words leaves out a resource holding only one
    Given the following "task" resources exist:
      | name                     | description                                   |
      | Chase the Harbor invoice | Harbor Legal invoice chase for the June lease |
      | Harbor Legal onboarding  | Welcome pack for the new Legal client         |
      | Renew the office lease   | Invoice the landlord before the June renewal  |
    When I call memory_search for "Harbor Legal invoice" matching any 2 words
    Then the search returns exactly these resources:
      | name                     |
      | Chase the Harbor invoice |
      | Harbor Legal onboarding  |

  @story-wm-u1xp4w
  Scenario: A PostgreSQL search in any mode narrowed to a type returns only that type's matches
    Given the following "project" resources exist:
      | name                    | description                        |
      | Harbor Legal onboarding | Invoice templates for Harbor Legal |
    And the following "task" resources exist:
      | name                   | description                  |
      | Renew the office lease | Invoice the landlord in June |
    When I call memory_search for "Harbor Legal invoice" matching any word narrowed to type "task"
    Then the search returns exactly these resources:
      | name                   |
      | Renew the office lease |

  @story-wm-u1xp4w
  Scenario: A PostgreSQL search in any mode still excludes another account's better match
    Given account "Harbor Legal" has the following "task" resources:
      | name                   | description                                  |
      | Chase overdue invoices | Dunmore settlement balance still outstanding |
    And account "Cedar Realty" has the following "task" resources:
      | name                             | description            |
      | Prepare the Dunmore Road showing | Viewing for the Patels |
    When account "Cedar Realty" calls memory_search for "the Dunmore settlement balance" matching any word
    Then the search returns exactly these resources:
      | name                             |
      | Prepare the Dunmore Road showing |
