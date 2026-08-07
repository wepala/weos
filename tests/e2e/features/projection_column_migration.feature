@issue-379
Feature: A built-in preset's schema change reaches an already-provisioned database
  As an operator upgrading WeOS across an additive resource type schema change
  I want the projection table of a built-in type to gain the property the new build declares
  So that writes to the new field are stored and readable instead of silently dropped

  Background:
    Given a built-in preset "catalog" declaring a "widget" type with the properties:
      | property | type   |
      | name     | string |
    And the "catalog" preset seeds 3 "widget" fixtures
    And a clean WeOS database provisioned by that build

  Scenario: A property added to a built-in preset becomes a projection column after restart
    When the "catalog" preset adds a "sku" string property to "widget"
    And the twin restarts against the same database
    Then the "widget" projection table has a "sku" column

  Scenario: A field added to a built-in preset round-trips after restart
    Given the "catalog" preset adds a "sku" string property to "widget"
    And the twin restarts against the same database
    When I create a "widget" named "Bolt cutter" with "sku" set to "BC-100"
    Then reading the "widget" "Bolt cutter" back over the API returns "sku" as "BC-100"

  Scenario: Restarting with an unchanged preset records no resource type update
    When the twin restarts against the same database
    Then no resource type update is recorded for "widget"
    And the "widget" projection table has a "name" column

  Scenario: The schema refresh happens once and then settles
    Given the "catalog" preset adds a "sku" string property to "widget"
    When the twin restarts against the same database
    And the twin restarts against the same database again
    Then exactly one resource type update is recorded for "widget"

  Scenario: Rows written before the column existed still read back, with the new field empty
    Given a "widget" named "Hex key" exists
    When the "catalog" preset adds a "sku" string property to "widget"
    And the twin restarts against the same database
    Then reading the "widget" "Hex key" back over the API succeeds
    And it returns no value for "sku"

  Scenario: Fixture data is not re-seeded when a preset's schema changes
    When the "catalog" preset adds a "sku" string property to "widget"
    And the twin restarts against the same database
    Then there are still exactly 3 "widget" resources

  Scenario: An archived resource type stays archived across the schema refresh
    Given the "widget" resource type is archived
    When the "catalog" preset adds a "sku" string property to "widget"
    And the twin restarts against the same database
    Then the "widget" resource type is still archived
    And the "widget" projection table has a "sku" column

  Scenario: A field written while the column was missing survives in the canonical record
    Given a "widget" named "Bolt cutter" is created with an undeclared "sku" of "BC-100"
    When the "catalog" preset adds a "sku" string property to "widget"
    And the twin restarts against the same database
    Then the JSON-LD representation of the "widget" "Bolt cutter" still carries "sku" as "BC-100"

  Scenario: Reprojecting populates the new column on rows written before it existed
    Given a "widget" named "Bolt cutter" is created with an undeclared "sku" of "BC-100"
    And the "catalog" preset adds a "sku" string property to "widget"
    And the twin restarts against the same database
    And reading the "widget" "Bolt cutter" back over the API returns no value for "sku"
    When the operator reprojects the event feed
    Then reading the "widget" "Bolt cutter" back over the API returns "sku" as "BC-100"
