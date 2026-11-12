@story-wm-u1xp4w
Feature: Matching any word of a lexical memory search
  As an LLM client connected to the WeOS MCP server
  I want a keyword search that answers from any of the words of the question it was given
  So that a sentence a person typed finds the memories it is about rather than nothing at all

  memory_search matches every content word of a query: a resource is a hit only when it holds all
  of them. That is the right rule for two or three keywords and the wrong one for a sentence. A
  question a person types carries words no memory holds, so the search that should have found the
  one memory about it finds nothing, and the client has no way to ask the looser question.

  Naming the "any" match mode asks that looser question. The query's stop words — the small words
  a question is built from, by one list for both engines — are dropped, and a resource that holds
  any of the remaining content words is a hit. Hits are ranked by how many distinct content words
  each one holds, so the resource the question is most about comes first, and a resource that
  repeats one of those words many times does not outrank a resource that holds more of them. Where
  even that is too loose, a caller names how many of the words a hit has to hold.

  The looser mode changes nothing else. The query is still user text and never query language, the
  search still answers only from resources the caller may read, and a type filter still narrows it.
  Both are applied where the match is made rather than to the answer afterwards, so a crowded index
  cannot push the best-covering resource out of the result. The "all" mode is still what a call
  that names no mode gets, and it is unchanged.

  These scenarios run on SQLite. The PostgreSQL half of the same behaviour is in
  postgres_lexical_index.feature.

  Background:
    Given a clean WeOS knowledge graph
    And the "tasks" preset is installed

  Scenario: A sentence query returns the resource that holds the most of its words first
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

  Scenario: A resource that repeats one query word ranks below one that holds more of them
    Given the following "task" resources exist:
      | name                    | description                                            |
      | Dunmore invoice backlog | Invoice chase, invoice reminder and invoice escalation |
      | Harbor Legal onboarding | One invoice template for the new client                |
    When I call memory_search for "Harbor invoice" matching any word
    Then the search returns these resources in order:
      | name                    |
      | Harbor Legal onboarding |
      | Dunmore invoice backlog |

  Scenario: Two resources that hold the same query words are ranked by how often they hold them
    Given the following "task" resources exist:
      | name                     | description                                            |
      | Chase the Harbor invoice | Harbor Legal invoice chase; the Harbor invoice is late |
      | Draft the Q3 summary     | One line for Harbor, with the invoice total for Q3     |
    When I call memory_search for "Harbor invoice" matching any word
    Then the search returns these resources in order:
      | name                     |
      | Chase the Harbor invoice |
      | Draft the Q3 summary     |

  Scenario: A resource that shares only stop words with the query is not returned
    Given the following "task" resources exist:
      | name                     | description                                   |
      | Chase the Harbor invoice | Harbor Legal invoice chase for the June lease |
      | Order the office chairs  | Two more chairs for the studio                |
    When I call memory_search for "the invoice for the Harbor Legal lease" matching any word
    Then the search returns exactly these resources:
      | name                     |
      | Chase the Harbor invoice |

  Scenario: A query of stop words alone returns no resources
    Given the following "task" resources exist:
      | name                     | description                                   |
      | Chase the Harbor invoice | Harbor Legal invoice chase for the June lease |
    When I call memory_search for "what is it about" matching any word
    Then the call succeeds
    And the search returns no resources

  Scenario: A question holding a contraction ranks the resource about its content words first
    Given the following "task" resources exist:
      | name                          | description                                        |
      | Help Dana log in              | Reset the child account password so she can log in |
      | Renew the child care contract | Contract renewal for the Wepala studio             |
      | Order the office chairs       | You can collect them from the studio               |
    When I call memory_search for "my child can't log in" matching any word
    Then the search returns these resources in order:
      | name                          |
      | Help Dana log in              |
      | Renew the child care contract |

  Scenario: A query of a contraction alone returns no resources
    Given the following "task" resources exist:
      | name                    | description                                        |
      | Help Dana log in        | Reset the child account password so she can log in |
      | Order the office chairs | You can collect them from the studio               |
    When I call memory_search for "can't" matching any word
    Then the call succeeds
    And the search returns no resources

  Scenario Outline: A query carrying full-text operator syntax is matched as words in any mode
    Given the following "task" resources exist:
      | name                     | description                                   |
      | Chase the Harbor invoice | Harbor Legal invoice chase for the June lease |
      | Renew the office lease   | Lease renewal for the Wepala studio           |
    When I call memory_search for "<query>" matching any word
    Then the search returns exactly these resources:
      | name                     |
      | Chase the Harbor invoice |

    Examples:
      | query             |
      | Harbor & invoice  |
      | Harbor \| invoice |
      | Harbor:*          |
      | !Harbor           |

  Scenario: A query of operator characters alone returns no resources in any mode
    Given the following "task" resources exist:
      | name                     | description                                   |
      | Chase the Harbor invoice | Harbor Legal invoice chase for the June lease |
    When I call memory_search for "& | ! :*" matching any word
    Then the call succeeds
    And the search returns no resources

  Scenario Outline: A word with punctuation inside it is matched as the word a person wrote
    Given the following "task" resources exist:
      | name                   | description                                      |
      | Chase the Dunmore fee  | Follow-up with Dana's office at dana@example.com |
      | Renew the office lease | Lease renewal for the Wepala studio              |
    When I call memory_search for "<query>" matching any word
    Then the search returns exactly these resources:
      | name                  |
      | Chase the Dunmore fee |

    Examples:
      | query            |
      | follow-up        |
      | Dana's           |
      | dana@example.com |

  Scenario: Requiring two matching words leaves out a resource that holds only one
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

  Scenario: Requiring more matching words than the query holds asks for all of its words
    Given the following "task" resources exist:
      | name                     | description                                   |
      | Chase the Harbor invoice | Harbor Legal invoice chase for the June lease |
      | Renew the office lease   | Invoice the landlord before the June renewal  |
    When I call memory_search for "Harbor invoice" matching any 5 words
    Then the search returns exactly these resources:
      | name                     |
      | Chase the Harbor invoice |

  Scenario: A crowded index still returns the resource that holds the most query words
    Given 25 "task" resources whose description mentions "invoice" exist
    And the following "task" resources exist:
      | name                     | description                                   |
      | Chase the Harbor invoice | Harbor Legal invoice chase for the June lease |
    When I call memory_search for "Harbor Legal invoice" matching any word
    Then the search returns the "task" resource "Chase the Harbor invoice"

  Scenario: A search narrowed to a type returns only that type's matches in any mode
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

  Scenario: Another account's better-matching resource is not returned in any mode
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

  Scenario: A search that names no match mode still returns only resources holding every word
    Given the following "task" resources exist:
      | name                     | description                                   |
      | Chase the Harbor invoice | Harbor Legal invoice chase for the June lease |
      | Harbor Legal onboarding  | Welcome pack for the new Legal client         |
    When I call memory_search for "the invoice for the Harbor Legal lease"
    Then the search returns exactly these resources:
      | name                     |
      | Chase the Harbor invoice |
