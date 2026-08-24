@issue-513
Feature: An operator adopts a held @context term without orphaning the data behind it
  As an operator whose boot holds a preset term because adopting it would repoint live data
  I want one command that adopts the term and records the IRI my existing edges are keyed by
  So that the property becomes readable again and the boot stops reporting the type every start

  # The boot's hold (issue #513) stops the orphaning but leaves the operator stuck: the
  # property stays unreadable, the boot logs the same failure every start, and the ADR
  # forbids `preset install --update` at boot. Events are immutable and ResourceCreated
  # carries the graph keyed by the write-time IRI, so a reproject reproduces that key no
  # matter what is done to the stored data. Adoption therefore records an ALIAS — the old
  # IRI, kept in the stored context under "weos:termAliases" — rather than rewriting edges.

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

  Scenario: A reference written before the term existed reads back once the term is adopted
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    And I create a "widget" named "Bolt cutter" with "supplier" referring to the "vendor" "Acme"
    And the "catalog" preset declares "supplier" as "https://example.org/catalog#supplier" in the "widget" context
    And the twin restarts against the same database
    When the operator adopts the held "supplier" context term for "widget"
    Then the stored "widget" context still maps "supplier" to "https://example.org/catalog#supplier"
    And the stored "widget" context records "https://schema.org/supplier" as a historical IRI for "supplier"
    And the JSON-LD representation of the "widget" "Bolt cutter" still carries a "supplier" edge to the "vendor" "Acme"
    When the operator reprojects the event feed
    Then reading the "widget" "Bolt cutter" back through the projection returns "supplier" as the "vendor" "Acme"

  Scenario: A write made after adoption uses the adopted IRI and reads back beside the old one
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    And I create a "widget" named "Bolt cutter" with "supplier" referring to the "vendor" "Acme"
    And the "catalog" preset declares "supplier" as "https://example.org/catalog#supplier" in the "widget" context
    And the twin restarts against the same database
    When the operator adopts the held "supplier" context term for "widget"
    And I create a "widget" named "Hex key" with "supplier" referring to the "vendor" "Acme"
    Then reading the "widget" "Hex key" back through the projection returns "supplier" as the "vendor" "Acme"
    And the JSON-LD representation of the "widget" "Hex key" still carries a "supplier" edge to the "vendor" "Acme"
    And the JSON-LD representation of the "widget" "Bolt cutter" still carries a "supplier" edge to the "vendor" "Acme"

  Scenario: The boot settles once the term is adopted
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    And I create a "widget" named "Bolt cutter" with "supplier" referring to the "vendor" "Acme"
    And the "catalog" preset declares "supplier" as "https://example.org/catalog#supplier" in the "widget" context
    And the twin restarts against the same database
    When the operator adopts the held "supplier" context term for "widget"
    And the twin restarts against the same database again
    Then the boot reconcile does not report the "supplier" context term as held for "widget"
    And the boot reconcile no longer names "supplier" as a property whose writes are dropped
    And the boot reconcile records no failure for "widget"
    And the stored "widget" context still maps "supplier" to "https://example.org/catalog#supplier"

  Scenario: Adopting the same term twice records nothing a second time
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    And I create a "widget" named "Bolt cutter" with "supplier" referring to the "vendor" "Acme"
    And the "catalog" preset declares "supplier" as "https://example.org/catalog#supplier" in the "widget" context
    And the twin restarts against the same database
    When the operator adopts the held "supplier" context term for "widget"
    And the operator adopts the "supplier" context term for "widget" again
    Then the stored "widget" context is byte-identical to the one stored before the second adoption
    And the stored "widget" context records exactly one historical IRI for "supplier"
    And the JSON-LD representation of the "widget" "Bolt cutter" still carries a "supplier" edge to the "vendor" "Acme"

  Scenario: Adopting a term the boot never held is refused
    Given I create a "widget" named "Bolt cutter" with "maker" referring to the "vendor" "Acme"
    When the operator adopts the "maker" context term for "widget"
    Then the adoption is refused because "maker" was not held
    And the stored "widget" context records no historical IRI for "maker"
    And the JSON-LD representation of the "widget" "Bolt cutter" still carries a "maker" edge to the "vendor" "Acme"

  Scenario: An adopted alias never shadows a term another property still uses
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    And the operator maps "maker" to "https://schema.org/supplier" in the stored "widget" context
    And I create a "widget" named "Bolt cutter" with "maker" referring to the "vendor" "Acme"
    And the "catalog" preset declares "supplier" as "https://example.org/catalog#supplier" in the "widget" context
    And the twin restarts against the same database
    When the operator adopts the held "supplier" context term for "widget"
    Then the stored "widget" context still maps "supplier" to "https://example.org/catalog#supplier"
    And the stored "widget" context records "https://schema.org/supplier" as a historical IRI for "supplier"
    When the operator reprojects the event feed
    Then reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Acme"
    And reading the "widget" "Bolt cutter" back through the projection returns no value for "supplier"

  Scenario: Adopting a held prefix records the alias against the property the prefix moves
    Given the operator maps "maker" to "cat:madeBy" in the stored "widget" context
    And I create a "widget" named "Bolt cutter" with "maker" referring to the "vendor" "Acme"
    And the "catalog" preset declares "cat" as "https://example.org/catalog#" in the "widget" context
    And the twin restarts against the same database
    When the operator adopts the held "cat" context term for "widget"
    Then the stored "widget" context records "https://schema.org/cat:madeBy" as a historical IRI for "maker"
    And the JSON-LD representation of the "widget" "Bolt cutter" still carries a "maker" edge to the "vendor" "Acme"
    When the operator reprojects the event feed
    Then reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Acme"

  Scenario: Adopting every held term for a type takes all of them in one command
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the "catalog" preset adds a "distributor" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    And I create a "widget" named "Bolt cutter" with these references:
      | property    | vendor |
      | supplier    | Acme   |
      | distributor | Acme   |
    And the "catalog" preset declares "supplier" as "https://example.org/catalog#supplier" in the "widget" context
    And the "catalog" preset declares "distributor" as "https://example.org/catalog#distributor" in the "widget" context
    And the twin restarts against the same database
    When the operator adopts every held context term for "widget"
    And the operator reprojects the event feed
    Then reading the "widget" "Bolt cutter" back through the projection returns "supplier" as the "vendor" "Acme"
    And reading the "widget" "Bolt cutter" back through the projection returns "distributor" as the "vendor" "Acme"
    And the stored "widget" context records "https://schema.org/supplier" as a historical IRI for "supplier"
    And the stored "widget" context records "https://schema.org/distributor" as a historical IRI for "distributor"

  Scenario: Adopting every held term still leaves the type's RDF class where it is
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    And I create a "widget" named "Bolt cutter" with "supplier" referring to the "vendor" "Acme"
    And the "catalog" preset declares "supplier" as "https://example.org/catalog#supplier" in the "widget" context
    And the "catalog" preset declares "@type" as "Product" in the "widget" context
    And the twin restarts against the same database
    When the operator adopts every held context term for "widget"
    And a "widget" named "Hex key" exists
    Then the stored "widget" context still maps "supplier" to "https://example.org/catalog#supplier"
    And the stored "widget" context has no entry for "@type"
    And the "widget" resources "Bolt cutter" and "Hex key" carry the same RDF type
