@epic-136 @story-422
Feature: Selecting and degrading the knowledge-graph store
  As an operator running WeOS
  I want the graph store chosen from configuration and treated as an optional dependency
  So that the twin runs whether or not a graph backend is available

  Scenario: With no graph store configured the tools report the graph is unavailable
    Given a WeOS twin with no knowledge-graph store configured
    And the "tasks" preset is installed
    When I search the knowledge graph for "Client onboarding"
    Then the knowledge graph reports it is not configured

  Scenario: An unopenable embedded store degrades to no graph without stopping the twin
    Given a WeOS twin whose embedded knowledge-graph store path cannot be opened
    And the "tasks" preset is installed
    When I create a "project" named "Client onboarding"
    Then the "project" resource "Client onboarding" is created
    And the knowledge graph reports it is not configured
    And the twin logs a single error that the embedded knowledge-graph store could not be opened
