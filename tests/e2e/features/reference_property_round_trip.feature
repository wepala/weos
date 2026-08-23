@issue-513
Feature: Every reference shape a preset declares reads back
  As an operator whose preset declares references both as a single value and as a list
  I want a list-valued reference to survive the round-trip like a single-valued one
  So that a type reported as reconciled is one whose references can actually be read

  Background:
    Given a built-in preset "catalog" declaring a "vendor" type with the properties:
      | property | type   |
      | name     | string |
    And the "catalog" preset also declares a "widget" type with the properties:
      | property  | type           | references |
      | name      | string         |            |
      | maker     | reference      | vendor     |
      | suppliers | reference list | vendor     |
    And a clean WeOS database provisioned by that build
    And a "vendor" named "Acme" exists

  @wip
  Scenario: A single-valued and a list-valued reference declared side by side both read back
    When I create a "widget" named "Bolt cutter" with these references:
      | property  | vendor |
      | maker     | Acme   |
      | suppliers | Acme   |
    Then reading the "widget" "Bolt cutter" back over the API returns "maker" as the "vendor" "Acme"
    And reading the "widget" "Bolt cutter" back over the API returns "suppliers" as the vendors "Acme"

  @wip
  Scenario: A list-valued reference reads back every target it was written with
    Given a "vendor" named "Globex" exists
    When I create a "widget" named "Bolt cutter" with these references:
      | property  | vendor       |
      | suppliers | Acme, Globex |
    Then reading the "widget" "Bolt cutter" back over the API returns "suppliers" as the vendors "Acme, Globex"

  @wip
  Scenario: A list-valued reference reaches the projection read and the canonical record alike
    Given a "vendor" named "Globex" exists
    When I create a "widget" named "Bolt cutter" with these references:
      | property  | vendor       |
      | suppliers | Acme, Globex |
    Then the JSON-LD representation of the "widget" "Bolt cutter" carries "suppliers" edges to the vendors "Acme, Globex"
    And reading the "widget" "Bolt cutter" back over the API returns "suppliers" as the vendors "Acme, Globex"

  @wip
  Scenario: A list-valued reference added by a later build round-trips after restart
    Given the "catalog" preset adds a "distributors" reference list property to "widget" targeting "vendor"
    And the twin restarts against the same database
    When I create a "widget" named "Bolt cutter" with "distributors" referring to the "vendor" "Acme"
    Then the "widget" projection table has a "distributors" column
    And reading the "widget" "Bolt cutter" back over the API returns "distributors" as the vendors "Acme"
