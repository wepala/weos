@issue-513
Feature: A boot reconcile never moves or blocks a predicate that already has data
  As an operator upgrading WeOS across a preset whose @context changed
  I want the boot to leave every predicate my stored edges are keyed by resolvable
  So that an upgrade never orphans data while reporting the type reconciled

  Background:
    Given a built-in preset "catalog" declaring a "vendor" type with the properties:
      | property | type   |
      | name     | string |
    And the "catalog" preset also declares a "widget" type with the properties:
      | property | type      | references |
      | name     | string    |            |
      | maker    | reference | vendor     |
    And a clean WeOS database provisioned by that build
    And a "vendor" named "Acme" exists

  Scenario: A term naming a different IRI than the data already uses is held and reported
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    And I create a "widget" named "Bolt cutter" with "supplier" referring to the "vendor" "Acme"
    When the "catalog" preset declares "supplier" as "https://example.org/catalog#supplier" in the "widget" context
    And the twin restarts against the same database
    Then the boot reconcile reports the "supplier" context term as held for "widget"

  Scenario: A reference written before its term existed still resolves after a differently-named term merges
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    And I create a "widget" named "Bolt cutter" with "supplier" referring to the "vendor" "Acme"
    When the "catalog" preset declares "supplier" as "https://example.org/catalog#supplier" in the "widget" context
    And the twin restarts against the same database
    Then the JSON-LD representation of the "widget" "Bolt cutter" still carries a "supplier" edge to the "vendor" "Acme"

  Scenario: A prefix the preset adds does not repoint a compact term the stored context already resolved
    Given the operator maps "maker" to "cat:madeBy" in the stored "widget" context
    And I create a "widget" named "Bolt cutter" with "maker" referring to the "vendor" "Acme"
    When the "catalog" preset declares "cat" as "https://example.org/catalog#" in the "widget" context
    And the twin restarts against the same database
    Then the JSON-LD representation of the "widget" "Bolt cutter" still carries a "maker" edge to the "vendor" "Acme"

  Scenario: A term that moves the type's RDF class is held so its resources stay in one class
    Given a "widget" named "Bolt cutter" exists
    When the "catalog" preset declares "@type" as "Product" in the "widget" context
    And the twin restarts against the same database
    And a "widget" named "Hex key" exists
    Then the "widget" resources "Bolt cutter" and "Hex key" carry the same RDF type
    And the boot reconcile reports the "@type" context term as held for "widget"

  Scenario Outline: A stored context that is not a JSON object still lets the schema merge through
    Given the operator stores the raw context <context> for "widget"
    And the "catalog" preset adds a "sku" string property to "widget"
    And the twin restarts against the same database
    When I create a "widget" named "Bolt cutter" with "sku" set to "BC-100"
    Then the "widget" projection table has a "sku" column
    And reading the "widget" "Bolt cutter" back through the projection returns "sku" as "BC-100"

    Examples:
      | context                 |
      | ["https://schema.org/"] |
      | "https://schema.org/"   |
