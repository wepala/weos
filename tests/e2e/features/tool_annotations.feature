Feature: Tool annotations advertise what mutates
  As an MCP client or in-app agent runtime
  I want every WeOS tool to declare whether it modifies the instance
  So that mutating actions can be gated behind user confirmation

  Background:
    Given a clean WeOS knowledge graph

  Scenario: Read-only tools advertise that they do not modify the instance
    When I list the server's tools
    Then the tool "memory_recall" is advertised as read-only
    And the tool "resource_get" is advertised as read-only
    And the tool "person_list" is advertised as read-only

  Scenario: Mutating tools advertise that they modify the instance
    When I list the server's tools
    Then the tool "resource_create" is advertised as mutating
    And the tool "resource_delete" is advertised as destructive
    And the tool "person_update" is advertised as destructive

  Scenario: Every advertised tool carries an explicit annotation
    When I list the server's tools
    Then every tool declares whether it is read-only or mutating

  Scenario: The episodic_recall tool is advertised as read-only
    When I list the server's tools
    Then the tool "episodic_recall" is advertised as read-only
