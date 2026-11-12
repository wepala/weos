@story-wm-irqzji
Feature: Account scope on a lexical memory search
  As an operator running one WeOS instance for several accounts
  I want a keyword search to answer only from the caller's own account
  So that no account is shown another account's memories, or the text inside them

  The lexical index is one table for the whole instance: every resource the instance stores is
  indexed into it, whatever account it belongs to. memory_search answered from that table with no
  account check at all, so on a multi-account instance one account's search could return another
  account's resources — and, because a full-text answer carries a snippet of the matched text, it
  returned the other account's words as well as its identifiers. The identifier alone is a leak;
  the snippet is the stored sentence.

  The search is scoped to the caller's account here. A search answers from the caller's own
  resources, and another account's matching resource is neither counted, returned, nor quoted.
  The boundary is the same one the rest of the instance already draws around an account's
  resources, so it holds for a resource created in one account and searched for from another
  whether the index was written as the resource was stored or rebuilt from the event history
  afterwards.

  The scope is applied where the match is made, not to the answer afterwards. A search spends its
  limit on the caller's own resources, so another account's better-ranked matches cannot push the
  caller's own match out of a crowded result, and it composes with the type filter: narrowing to a
  type narrows within the account rather than reaching across accounts for that type.

  Background:
    Given a clean WeOS knowledge graph
    And the "tasks" preset is installed

  @wip
  Scenario: Another account's matching resource is not returned
    Given account "Harbor Legal" has the following "task" resources:
      | name                   | description                                  |
      | Chase overdue invoices | Dunmore settlement balance still outstanding |
    When account "Cedar Realty" calls memory_search for "Dunmore"
    Then the call succeeds
    And the search returns no resources
    And the search returns no text from the "task" resource "Chase overdue invoices"

  @wip
  Scenario: The searching account still finds its own resource
    Given account "Harbor Legal" has the following "task" resources:
      | name                   | description                                  |
      | Chase overdue invoices | Dunmore settlement balance still outstanding |
    When account "Harbor Legal" calls memory_search for "Dunmore"
    Then the search reports the full-text index as its source
    And the search returns exactly these resources:
      | name                   |
      | Chase overdue invoices |

  @wip
  Scenario: A crowded index still returns the searching account's own match
    Given account "Harbor Legal" has 25 "task" resources whose description mentions "Dunmore"
    And account "Cedar Realty" has the following "task" resources:
      | name                             | description                         |
      | Prepare the Dunmore Road showing | Dunmore Road viewing for the Patels |
    When account "Cedar Realty" calls memory_search for "Dunmore"
    Then the search returns exactly these resources:
      | name                             |
      | Prepare the Dunmore Road showing |

  @wip
  Scenario: A search narrowed to a type still excludes another account's resource of that type
    Given account "Harbor Legal" has the following "task" resources:
      | name                   | description                                  |
      | Chase overdue invoices | Dunmore settlement balance still outstanding |
    And account "Cedar Realty" has the following "project" resources:
      | name                    | description                           |
      | Dunmore Road onboarding | Dunmore paperwork for the new listing |
    When account "Cedar Realty" calls memory_search for "Dunmore" narrowed to type "task"
    Then the call succeeds
    And the search returns no resources

  @wip
  Scenario: Text added to one account's resource stays invisible to another account
    Given account "Harbor Legal" has the following "task" resources:
      | name           | description                         |
      | Chase invoices | Harbor Legal invoice chase for June |
    And account "Harbor Legal" updates its "task" resource "Chase invoices" with the description "Dunmore settlement"
    When account "Cedar Realty" calls memory_search for "Dunmore"
    Then the call succeeds
    And the search returns no resources

  @wip
  Scenario: A rebuilt index keeps each account's text to its own account
    Given account "Harbor Legal" has the following "task" resources:
      | name                   | description                                  |
      | Chase overdue invoices | Dunmore settlement balance still outstanding |
    And account "Cedar Realty" has the following "task" resources:
      | name                             | description                         |
      | Prepare the Dunmore Road showing | Dunmore Road viewing for the Patels |
    And the lexical index is rebuilt from the event history
    When account "Cedar Realty" calls memory_search for "Dunmore"
    Then the search returns exactly these resources:
      | name                             |
      | Prepare the Dunmore Road showing |
