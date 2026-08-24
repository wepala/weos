@issue-515
Feature: A resource's edges are stored under their property names, and both shapes read
  As an operator whose presets declare references the @context does not always name
  I want the stored record to key each edge by the property it was written under
  So that no read path has to invert the context, and no write is silently dropped

  # Every silent-drop in #510, #511 and #513 has one cause: an edge is keyed by a
  # predicate IRI and the read path must map that IRI back to a property name
  # through jsonld.BuildReverseMap. That inversion fails when a property has no
  # `@context` term, when two properties share a predicate, and when a term is
  # later repointed. Compact form never performs the inversion — the key IS the
  # property name.
  #
  # The graph stays canonical. Compact and expanded are two serializations of the
  # same graph: the stored document expands to the same edges node and yields the
  # same triples. ExtractReferenceTriples runs on the ORIGINAL input data and is
  # untouched, so predicates in the triple store must not move.
  #
  # There is no migration. Old documents stay expanded and keep reading; new
  # writes are compact; the two forms coexist and are told apart by their keys —
  # an edges key is either an absolute IRI or a term name.

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

  Scenario: A new write keys its edge by the property name and keeps the mapping in its own context
    When I create a "widget" named "Bolt cutter" with "maker" referring to the "vendor" "Acme"
    Then the stored canonical record for the "widget" "Bolt cutter" keys its "maker" edge by the property name
    And the stored canonical record for the "widget" "Bolt cutter" keys no edge by an absolute IRI
    And the stored canonical record for the "widget" "Bolt cutter" maps "maker" to "https://schema.org/maker" in its own context
    And reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Acme"
    And the API read of the "widget" "Bolt cutter" returns "maker" as the "vendor" "Acme"
    And the JSON-LD representation of the "widget" "Bolt cutter" still carries a "maker" edge to the "vendor" "Acme"

  Scenario: A document stored in the old expanded form keeps reading beside a new compact write
    Given a "widget" named "Bolt cutter" stored in the old expanded edges form with "maker" referring to the vendors "Acme"
    When I create a "widget" named "Hex key" with "maker" referring to the "vendor" "Acme"
    Then the stored canonical record for the "widget" "Bolt cutter" keys its "maker" edge by a predicate IRI
    And the stored canonical record for the "widget" "Hex key" keys its "maker" edge by the property name
    And reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Acme"
    And reading the "widget" "Hex key" back through the projection returns "maker" as the "vendor" "Acme"
    And the API read of the "widget" "Bolt cutter" returns "maker" as the "vendor" "Acme"
    And the API read of the "widget" "Hex key" returns "maker" as the "vendor" "Acme"
    And the JSON-LD representation of the "widget" "Bolt cutter" still carries a "maker" edge to the "vendor" "Acme"
    And the JSON-LD representation of the "widget" "Hex key" still carries a "maker" edge to the "vendor" "Acme"

  Scenario: A list reference stored in the old expanded form still reads back as a list beside a compact one
    Given a "vendor" named "Globex" exists
    And a "widget" named "Bolt cutter" stored in the old expanded edges form with "suppliers" referring to the vendors "Acme, Globex"
    When I create a "widget" named "Hex key" with these references:
      | property  | vendor       |
      | suppliers | Acme, Globex |
    Then the stored canonical record for the "widget" "Hex key" keys its "suppliers" edge by the property name
    And reading the "widget" "Bolt cutter" back through the projection returns "suppliers" as the vendors "Acme, Globex"
    And reading the "widget" "Hex key" back through the projection returns "suppliers" as the vendors "Acme, Globex"
    And the API read of the "widget" "Bolt cutter" returns "suppliers" as the vendors "Acme, Globex"
    And the API read of the "widget" "Hex key" returns "suppliers" as the vendors "Acme, Globex"
    And the JSON-LD representation of the "widget" "Bolt cutter" carries "suppliers" edges to the vendors "Acme, Globex"
    And the JSON-LD representation of the "widget" "Hex key" carries "suppliers" edges to the vendors "Acme, Globex"

  Scenario: A reference property the preset never named in its context round-trips
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    When I create a "widget" named "Bolt cutter" with "supplier" referring to the "vendor" "Acme"
    Then reading the "widget" "Bolt cutter" back through the projection returns "supplier" as the "vendor" "Acme"
    And the API read of the "widget" "Bolt cutter" returns "supplier" as the "vendor" "Acme"
    And the JSON-LD representation of the "widget" "Bolt cutter" still carries a "supplier" edge to the "vendor" "Acme"

  Scenario: Repointing a term after the write orphans nothing
    Given I create a "widget" named "Bolt cutter" with "maker" referring to the "vendor" "Acme"
    When the operator maps "maker" to "https://example.org/catalog#madeBy" in the stored "widget" context
    Then the JSON-LD representation of the "widget" "Bolt cutter" still carries a "maker" edge to the "vendor" "Acme"
    And the API read of the "widget" "Bolt cutter" returns "maker" as the "vendor" "Acme"
    When the operator reprojects the event feed
    Then reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Acme"
    And the JSON-LD representation of the "widget" "Bolt cutter" still carries a "maker" edge to the "vendor" "Acme"

  Scenario: A compact record replays through a reproject with every edge intact
    Given a "vendor" named "Globex" exists
    When I create a "widget" named "Bolt cutter" with these references:
      | property  | vendor       |
      | maker     | Acme         |
      | suppliers | Acme, Globex |
    And the operator reprojects the event feed
    Then the stored canonical record for the "widget" "Bolt cutter" keys its "maker" edge by the property name
    And reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Acme"
    And reading the "widget" "Bolt cutter" back through the projection returns "suppliers" as the vendors "Acme, Globex"

  Scenario: A stored compact record expands to the graph the expanded form stored
    Given the "catalog" preset adds a "courier" reference property to "widget" targeting "vendor"
    And the "catalog" preset declares "courier" as "https://example.org/catalog#courier" in the "widget" context
    And the twin restarts against the same database
    And a "widget" named "Bolt cutter" stored in the old expanded edges form with "courier" referring to the vendors "Acme"
    When I create a "widget" named "Hex key" with "courier" referring to the "vendor" "Acme"
    Then the stored canonical record for the "widget" "Bolt cutter" keys its "courier" edge by "https://example.org/catalog#courier"
    And the stored canonical record for the "widget" "Hex key" keys its "courier" edge by the property name
    And expanding the stored record for the "widget" "Hex key" through its own context yields the edges the stored record for the "widget" "Bolt cutter" already holds

  Scenario: Both shapes put the same predicate and the same object in the triple store
    Given a "widget" named "Bolt cutter" stored in the old expanded edges form with "maker" referring to the vendors "Acme"
    When I create a "widget" named "Hex key" with "maker" referring to the "vendor" "Acme"
    Then the triple store holds "https://schema.org/maker" from the "widget" "Bolt cutter" to the "vendor" "Acme"
    And the triple store holds "https://schema.org/maker" from the "widget" "Hex key" to the "vendor" "Acme"

  Scenario: A property with no context term still resolves its predicate through @vocab in the triple store
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    When I create a "widget" named "Bolt cutter" with "supplier" referring to the "vendor" "Acme"
    Then the triple store holds "https://schema.org/supplier" from the "widget" "Bolt cutter" to the "vendor" "Acme"

  Scenario: A term declared away from @vocab keeps its predicate in the triple store
    Given the "catalog" preset adds a "courier" reference property to "widget" targeting "vendor"
    And the "catalog" preset declares "courier" as "https://example.org/catalog#courier" in the "widget" context
    And the twin restarts against the same database
    When I create a "widget" named "Bolt cutter" with "courier" referring to the "vendor" "Acme"
    Then the triple store holds "https://example.org/catalog#courier" from the "widget" "Bolt cutter" to the "vendor" "Acme"
    And the triple store holds no "https://schema.org/courier" edge from the "widget" "Bolt cutter"
