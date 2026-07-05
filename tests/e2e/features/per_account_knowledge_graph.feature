@epic-136 @story-431 @wip
Feature: Per-account knowledge graphs in a shared-process deployment
  As an operator running one WeOS process for many accounts
  I want each account's knowledge graph isolated to its own store
  So that every kg_* query only ever answers from the caller's own graph

  Scenario: Search returns only the searching account's entities
    Given a per-account WeOS twin with embedded knowledge-graph stores
    And the "tasks" preset is installed
    And account "Harbor Legal" has a "project" named "Q3 Roadmap"
    And account "Cedar Realty" has a "project" named "Q3 Roadmap"
    When account "Harbor Legal" searches the knowledge graph for "Q3 Roadmap"
    Then the search results include the "project" resource "Q3 Roadmap" owned by account "Harbor Legal"
    And the search results exclude the "project" resource "Q3 Roadmap" owned by account "Cedar Realty"

  Scenario: Expanding an entity shows only the caller account's neighborhood
    Given a per-account WeOS twin with embedded knowledge-graph stores
    And the "tasks" preset is installed
    And account "Harbor Legal" has a "task" named "Chase overdue invoices" linked to the project "Client onboarding"
    And account "Cedar Realty" has a "project" named "Client onboarding"
    When account "Harbor Legal" expands its task "Chase overdue invoices" in the knowledge graph
    Then the neighborhood includes the project "Client onboarding" owned by account "Harbor Legal"
    And the neighborhood excludes the project "Client onboarding" owned by account "Cedar Realty"

  Scenario: A SPARQL query answers only from the caller account's graph
    Given a per-account WeOS twin with embedded knowledge-graph stores
    And the "tasks" preset is installed
    And account "Harbor Legal" has a "project" named "Website refresh"
    And account "Cedar Realty" has a "project" named "Website refresh"
    When account "Harbor Legal" runs the SPARQL query:
      """
      SELECT DISTINCT ?s WHERE { ?s ?p ?o }
      """
    Then the query results include the "project" resource "Website refresh" owned by account "Harbor Legal"
    And the query results exclude the "project" resource "Website refresh" owned by account "Cedar Realty"

  Scenario: Class listing reflects only the caller account's own graph
    Given a per-account WeOS twin with embedded knowledge-graph stores
    And the "tasks" preset is installed
    And account "Harbor Legal" has a "project" named "Annual audit"
    And account "Cedar Realty" has no resources in the knowledge graph
    When account "Cedar Realty" lists the knowledge graph classes
    Then no classes are listed

  Scenario: Describing a class returns only the caller account's example instances
    Given a per-account WeOS twin with embedded knowledge-graph stores
    And the "tasks" preset is installed
    And account "Harbor Legal" has a "project" named "Client onboarding"
    And account "Cedar Realty" has a "project" named "Client onboarding"
    When account "Harbor Legal" describes the "project" class with example instances
    Then the example instances include the "project" resource "Client onboarding" owned by account "Harbor Legal"
    And the example instances exclude the "project" resource "Client onboarding" owned by account "Cedar Realty"

  Scenario: Path lookup does not traverse another account's relationships
    Given a per-account WeOS twin with embedded knowledge-graph stores
    And the "tasks" preset is installed
    And account "Harbor Legal" has a "task" named "Inspect cables" linked to the project "Bridge upgrade"
    And account "Cedar Realty" has a "task" named "Inspect cables"
    And account "Cedar Realty" has a "project" named "Bridge upgrade"
    When account "Cedar Realty" looks for a path from its task "Inspect cables" to its project "Bridge upgrade"
    Then no path is found

  Scenario: An account cannot read another account's entity by its exact identifier
    Given a per-account WeOS twin with embedded knowledge-graph stores
    And the "tasks" preset is installed
    And account "Harbor Legal" has a "project" named "Q3 Roadmap"
    When account "Cedar Realty" expands the project "Q3 Roadmap" owned by account "Harbor Legal"
    Then the neighborhood is empty

  Scenario: Deleting a resource drops it only from the owning account's graph
    Given a per-account WeOS twin with embedded knowledge-graph stores
    And the "tasks" preset is installed
    And account "Harbor Legal" has a "project" named "Board pack"
    And account "Cedar Realty" has a "project" named "Board pack"
    When account "Harbor Legal" deletes its project "Board pack"
    Then the knowledge graph no longer returns the "project" resource "Board pack" owned by account "Harbor Legal"
    And the knowledge graph still returns the "project" resource "Board pack" owned by account "Cedar Realty"

  Scenario: Per-account isolation is off by default in single-tenant mode
    Given a WeOS twin with an embedded knowledge-graph store
    And the "tasks" preset is installed
    And a "project" named "Spring campaign" exists
    And a "project" named "Vendor review" exists
    When I run the SPARQL query:
      """
      SELECT DISTINCT ?s WHERE { ?s ?p ?o }
      """
    Then the query results include the "project" resource "Spring campaign"
    And the query results include the "project" resource "Vendor review"

  Scenario: A remote request with no resolvable account is refused rather than served a default graph
    Given a per-account WeOS twin serving the remote HTTP MCP transport
    And the "tasks" preset is installed
    And account "Harbor Legal" has a "project" named "Q3 Roadmap"
    When a remote request with no resolvable account searches the knowledge graph for "Q3 Roadmap"
    Then the knowledge graph reports it is not configured
    And no entities are returned

  Scenario: A local stdio request with no resolvable account is served from the local graph
    Given a per-account WeOS twin serving the local stdio MCP transport
    And the "tasks" preset is installed
    And account "Harbor Legal" has a "project" named "Q3 Roadmap"
    And a local request with no resolvable account has a "project" named "Weekly review" in the local graph
    When the local request searches the knowledge graph for "Weekly review"
    Then the search results include the "project" resource "Weekly review" owned by the local graph
    And the search results exclude the "project" resource "Q3 Roadmap" owned by account "Harbor Legal"
    And account "Harbor Legal" does not see the "project" resource "Weekly review" owned by the local graph

  Scenario: A checkpoint reset replays each account's history into its own graph
    Given a per-account WeOS twin with embedded knowledge-graph stores
    And the "tasks" preset is installed
    And account "Harbor Legal" has a "project" named "Annual audit"
    And account "Cedar Realty" has a "project" named "Budget review"
    When the knowledge-graph projection is rebuilt from event history
    Then the knowledge graph still returns the "project" resource "Annual audit" owned by account "Harbor Legal"
    And the knowledge graph still returns the "project" resource "Budget review" owned by account "Cedar Realty"
    And the knowledge graph does not return the "project" resource "Budget review" owned by account "Harbor Legal"

  Scenario: The process serves many accounts and restarts cleanly without stale locks
    Given a per-account WeOS twin with embedded knowledge-graph stores
    And the "tasks" preset is installed
    And 25 accounts each have their own "project" in the knowledge graph
    When the twin restarts against the same embedded stores
    Then the knowledge graph returns each account's own project
    And no account sees another account's project
    And the twin reports no knowledge-graph store lock or file-handle errors
