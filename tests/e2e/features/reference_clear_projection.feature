@issue-550
Feature: A cleared reference reaches the flat projection
  As an operator who removes a relationship from a resource
  I want the flat projection to forget the reference the update removed
  So that a read of the projection agrees with the record the event store keeps

  # The event store already records the clear correctly: the update path replaces
  # the entity's data wholesale, so the stored record carries no such property in
  # its entity node and no such edge in its edges node. The flat projection is the
  # one surface that keeps the old value, because the upsert derives its column
  # list from the keys the data produced, and a property that went away produces
  # no key. Every scenario below therefore reads through the projection.
  #
  # The projection read also carries the denormalized display column for a
  # reference, under the camelCase name the flat read returns — `makerDisplay`
  # for `maker`. That column is DERIVED, not a declared property: it is written
  # by this path and by the async display-values subscriber alike. Two scenarios
  # pin it, so a fix that clears every column absent from the write is caught
  # rather than shipped.

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
  Scenario: A cleared reference is gone from the projection read
    Given I create a "widget" named "Bolt cutter" with "maker" referring to the "vendor" "Acme"
    When I update the "widget" "Bolt cutter" clearing "maker"
    Then reading the "widget" "Bolt cutter" back through the projection returns no value for "maker"

  @wip
  Scenario: The display value of a cleared reference goes with it
    Given I create a "widget" named "Bolt cutter" with "maker" referring to the "vendor" "Acme"
    And reading the "widget" "Bolt cutter" back through the projection returns "makerDisplay" as "Acme"
    When I update the "widget" "Bolt cutter" clearing "maker"
    Then reading the "widget" "Bolt cutter" back through the projection returns no value for "makerDisplay"

  @wip
  Scenario: A cleared list reference is gone from the projection read
    Given I create a "widget" named "Bolt cutter" with these references:
      | property  | vendor |
      | suppliers | Acme   |
    When I update the "widget" "Bolt cutter" clearing "suppliers"
    Then reading the "widget" "Bolt cutter" back through the projection returns no value for "suppliers"

  @wip
  Scenario: A reference the update keeps is not cleared with the one it removes
    Given I create a "widget" named "Bolt cutter" with these references:
      | property  | vendor |
      | maker     | Acme   |
      | suppliers | Acme   |
    When I update the "widget" "Bolt cutter" clearing "suppliers"
    Then reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Acme"

  @wip
  Scenario: The display value of a reference the update keeps is not cleared
    Given I create a "widget" named "Bolt cutter" with these references:
      | property  | vendor |
      | maker     | Acme   |
      | suppliers | Acme   |
    When I update the "widget" "Bolt cutter" clearing "suppliers"
    Then reading the "widget" "Bolt cutter" back through the projection returns "makerDisplay" as "Acme"

  @wip
  Scenario: A literal property the update keeps survives a cleared reference
    Given I create a "widget" named "Bolt cutter" with "maker" referring to the "vendor" "Acme"
    When I update the "widget" "Bolt cutter" clearing "maker"
    Then reading the "widget" "Bolt cutter" back through the projection returns "name" as "Bolt cutter"

  @wip
  Scenario: Clearing a reference with an empty value has the same result as removing it
    Given I create a "widget" named "Bolt cutter" with "maker" referring to the "vendor" "Acme"
    When I update the "widget" "Bolt cutter" setting "maker" to an empty value
    Then reading the "widget" "Bolt cutter" back through the projection returns no value for "maker"

  @wip
  Scenario: A reference set again after a clear reads back
    Given I create a "widget" named "Bolt cutter" with "maker" referring to the "vendor" "Acme"
    And I update the "widget" "Bolt cutter" clearing "maker"
    When I update the "widget" "Bolt cutter" setting "maker" to the "vendor" "Acme"
    Then reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Acme"
