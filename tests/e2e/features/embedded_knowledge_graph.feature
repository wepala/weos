@epic-136 @story-422
Feature: Knowledge graph runs on an embedded store
  As an operator running WeOS from a single binary
  I want the knowledge-graph projection and kg_* tools to run on an in-process store
  So that graph features work with no separate graph server to deploy

  Background:
    Given a WeOS twin with an embedded knowledge-graph store
    And the "tasks" preset is installed

  @wip
  Scenario: A projected resource is searchable through the embedded graph
    Given a "project" named "Client onboarding" exists
    When I search the knowledge graph for "Client onboarding"
    Then the search results include the "project" resource "Client onboarding"

  @wip
  Scenario: A linked resource appears in an entity's neighborhood
    Given a "project" named "Client onboarding" exists
    And a "task" named "Chase overdue invoices" exists linked to the project "Client onboarding"
    When I expand the "task" resource "Chase overdue invoices" in the knowledge graph
    Then the neighborhood includes the project "Client onboarding"

  @wip
  Scenario: A raw SPARQL query returns resources projected into the embedded store
    Given a "project" named "Website refresh" exists
    When I run the SPARQL query:
      """
      SELECT DISTINCT ?s WHERE { ?s ?p ?o }
      """
    Then the query results include the "project" resource "Website refresh"

  @wip
  Scenario: Deleting a resource drops its subject from the embedded graph
    Given a "project" named "Quarterly board pack" exists
    When I delete the "project" resource "Quarterly board pack"
    Then the knowledge graph no longer returns the "project" resource "Quarterly board pack"

  @wip
  Scenario: The embedded graph survives a restart
    Given a "project" named "Annual audit" exists
    When the twin restarts against the same embedded store
    Then the knowledge graph still returns the "project" resource "Annual audit"
