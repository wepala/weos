@issue-515
Feature: A preset whose reference shape no reader can disambiguate is refused at boot
  As an operator installing a preset
  I want a shape that cannot be read back refused before anything is written into it
  So that I fix the preset rather than discover the loss in the data months later

  # Compact storage makes a shared predicate safe — several properties on one
  # predicate each keep their own key. One shape stays genuinely ambiguous: two
  # reference properties on the same type sharing BOTH the predicate IRI and the
  # target type slug. No reader can tell those apart, and Akeem's rule says they
  # should not exist:
  #
  #   - Different relationships need different predicates.
  #   - One relationship with several targets needs a single array property.
  #
  # Group a type's reference properties by (predicateIRI, targetTypeSlug) and
  # refuse any group holding more than one property, naming both remedies. A
  # property that is ALREADY an array is the correct shape and passes.
  #
  # CONTRACT for the refusal's wording, so these scenarios can find it: the
  # refusal is reported on a line (or a boot error) whose text contains the
  # marker "ambiguous reference shape", and names the type slug, both property
  # names, the shared predicate IRI, and both remedies — the phrase
  # "different predicates" and the phrase "a single array property".

  Background:
    Given a built-in preset "catalog" declaring a "vendor" type with the properties:
      | property | type   |
      | name     | string |
    And the "catalog" preset also declares a "widget" type with the properties:
      | property | type      | references |
      | name     | string    |            |
      | maker    | reference | vendor     |

  Scenario: Two reference properties sharing a predicate and a target type are refused, with both remedies named
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor"
    And the "catalog" preset declares "maker" as "https://schema.org/associated" in the "widget" context
    And the "catalog" preset declares "supplier" as "https://schema.org/associated" in the "widget" context
    When the twin starts against a clean database
    Then the boot refuses "widget", naming "maker" and "supplier" on "https://schema.org/associated"
    And the refusal tells the operator to give the relationships different predicates
    And the refusal tells the operator to collapse them into a single array property

  Scenario: Two reference properties sharing a predicate but not a target type are accepted
    Given the "catalog" preset adds a "partner" reference property to "widget" targeting "widget"
    And the "catalog" preset declares "maker" as "https://schema.org/associated" in the "widget" context
    And the "catalog" preset declares "partner" as "https://schema.org/associated" in the "widget" context
    When the twin starts against a clean database
    Then the boot refuses nothing

  Scenario: Two reference properties sharing a predicate but not a target type each read back under their own name
    Given the "catalog" preset adds a "partner" reference property to "widget" targeting "widget"
    And the "catalog" preset declares "maker" as "https://schema.org/associated" in the "widget" context
    And the "catalog" preset declares "partner" as "https://schema.org/associated" in the "widget" context
    And the twin starts against a clean database
    And a "vendor" named "Acme" exists
    And a "widget" named "Hex key" exists
    When I create a "widget" named "Bolt cutter" with these targets:
      | property | type   | target  |
      | maker    | vendor | Acme    |
      | partner  | widget | Hex key |
    Then reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Acme"
    And reading the "widget" "Bolt cutter" back through the projection returns "partner" as the "widget" "Hex key"
    And the JSON-LD representation of the "widget" "Bolt cutter" still carries a "maker" edge to the "vendor" "Acme"
    And the JSON-LD representation of the "widget" "Bolt cutter" still carries a "partner" edge to the "widget" "Hex key"

  Scenario: A single array property on a predicate is the correct shape and is accepted
    Given the "catalog" preset adds a "suppliers" reference list property to "widget" targeting "vendor"
    And the "catalog" preset declares "maker" as "https://example.org/catalog#madeBy" in the "widget" context
    And the "catalog" preset declares "suppliers" as "https://schema.org/associated" in the "widget" context
    When the twin starts against a clean database
    Then the boot refuses nothing

  Scenario: Two reference properties targeting one type on different predicates are accepted
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor"
    When the twin starts against a clean database
    Then the boot refuses nothing
    And a "vendor" named "Acme" exists
    And I create a "widget" named "Bolt cutter" with these targets:
      | property | type   | target |
      | maker    | vendor | Acme   |
      | supplier | vendor | Acme   |
    And reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Acme"
    And reading the "widget" "Bolt cutter" back through the projection returns "supplier" as the "vendor" "Acme"

  Scenario: A type that becomes ambiguous across builds is refused on the next boot
    Given the twin starts against a clean database
    And a "vendor" named "Acme" exists
    And I create a "widget" named "Bolt cutter" with "maker" referring to the "vendor" "Acme"
    When the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor"
    And the "catalog" preset declares "supplier" as "https://schema.org/maker" in the "widget" context
    And the twin restarts against the same database and reports what it finds
    Then the stored "widget" context still maps "supplier" to "https://schema.org/maker"
    And the boot refuses "widget", naming "maker" and "supplier" on "https://schema.org/maker"
    And the refusal tells the operator to give the relationships different predicates
    And the refusal tells the operator to collapse them into a single array property
