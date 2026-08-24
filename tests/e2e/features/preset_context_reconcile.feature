@issue-510
Feature: A built-in preset's new reference property reaches an already-provisioned database
  As an operator upgrading WeOS across a preset that gained a reference property
  I want the stored @context of a built-in type to gain the entry the new build declares
  So that writes to the new reference are stored and readable instead of silently dropped

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

  Scenario: A reference property added to a built-in preset round-trips after restart
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor"
    And the twin restarts against the same database
    When I create a "widget" named "Bolt cutter" with "supplier" referring to the "vendor" "Acme"
    Then reading the "widget" "Bolt cutter" back through the projection returns "supplier" as the "vendor" "Acme"

  Scenario: A new reference property gains both a projection column and a context entry
    When the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor"
    And the twin restarts against the same database
    Then the "widget" projection table has a "supplier" column
    And the stored "widget" context has an entry for "supplier"

  Scenario: A reference whose context entry was already stored keeps round-tripping on the same write
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor"
    And the twin restarts against the same database
    When I create a "widget" named "Bolt cutter" with these references:
      | property | vendor |
      | maker    | Acme   |
      | supplier | Acme   |
    Then reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Acme"
    And reading the "widget" "Bolt cutter" back through the projection returns "supplier" as the "vendor" "Acme"

  Scenario: A literal property added to a preset round-trips and gains no context entry
    Given the "catalog" preset adds a "sku" string property to "widget"
    And the twin restarts against the same database
    When I create a "widget" named "Bolt cutter" with "sku" set to "BC-100"
    Then reading the "widget" "Bolt cutter" back through the projection returns "sku" as "BC-100"
    And the stored "widget" context has no entry for "sku"

  Scenario: A context entry the operator changed is held at its stored definition
    Given the operator maps "maker" to "https://example.org/vocab/madeBy" in the stored "widget" context
    And the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor"
    When the twin restarts against the same database
    Then the stored "widget" context still maps "maker" to "https://example.org/vocab/madeBy"
    And the boot reconcile reports "maker" held at its stored definition for "widget"
    And the stored "widget" context has an entry for "supplier"

  Scenario: A held context entry still round-trips at the operator's definition
    Given the operator maps "maker" to "https://example.org/vocab/madeBy" in the stored "widget" context
    And the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor"
    And the twin restarts against the same database
    When I create a "widget" named "Bolt cutter" with these references:
      | property | vendor |
      | maker    | Acme   |
      | supplier | Acme   |
    Then reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Acme"
    And reading the "widget" "Bolt cutter" back through the projection returns "supplier" as the "vendor" "Acme"

  Scenario: An operator's own context entry the preset does not declare survives the merge
    Given the operator maps "warranty" to "https://example.org/vocab/warranty" in the stored "widget" context
    And the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor"
    When the twin restarts against the same database
    Then the stored "widget" context still maps "warranty" to "https://example.org/vocab/warranty"
    And the stored "widget" context has an entry for "supplier"

  # Changed by #513. A cleared context still resolves every reference — to its
  # bare property name, since there is no @vocab to prefix it — so edges exist
  # under those names and re-adopting the preset's terms would orphan them. The
  # boot reports instead of silently papering over the operator's act. A
  # property the preset adds in the SAME boot is exempt: it has no data under
  # any IRI yet, so its term merges freely.
  Scenario: A cleared context is held and reported rather than silently re-adopted
    Given the operator clears the stored "widget" context
    And the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor"
    When the twin restarts against the same database
    Then the stored "widget" context has no entry for "maker"
    And the boot reconcile reports the "maker" context term as held for "widget"

  Scenario: The context merge happens once and then settles
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor"
    When the twin restarts against the same database
    And the twin restarts against the same database again
    Then exactly one resource type update is recorded for "widget"

  Scenario: Restarting with an unchanged preset leaves an already-complete context alone
    When the twin restarts against the same database
    Then no resource type update is recorded for "widget"
    And the stored "widget" context has an entry for "maker"

  Scenario: Reporting a type as updated means its reference properties have a stored context entry
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor"
    When the twin restarts against the same database
    Then the boot reconcile reports "widget" as updated
    And every reference property the "catalog" preset declares for "widget" has a stored context entry

  # "vendor" changes in the same boot purely as a positive control: it proves the
  # boot really does report an updated type on this run, so the negative
  # assertion above it cannot rot into a permanent pass if the log line is
  # renamed (issue #513, P2-4).
  Scenario: A reference property the preset's own context never declares is not reported as updated
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the "catalog" preset adds a "code" string property to "vendor"
    When the twin restarts against the same database
    Then the boot reconcile does not report "widget" as updated
    And the boot reconcile names "supplier" as a property whose writes are still dropped
    And the boot reconcile reports "vendor" as updated

  @issue-513
  Scenario: A reference the preset never maps is still named as dropped on a later boot with nothing to merge
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    And the boot reconcile names "supplier" as a property whose writes are still dropped
    When the twin restarts against the same database again
    Then the boot reconcile names "supplier" as a property whose writes are still dropped

  # The two scenarios below write their pre-fix row through a schema that ALREADY
  # marks "supplier" a reference, so the value lands as a real EDGE keyed by the
  # @vocab-derived IRI. Written against a schema that does not declare it yet, it
  # lands in the entity node as a literal and the column refills through
  # extractNodeColumns — which is how both scenarios used to pass with the whole
  # context merge deleted (issue #513, P2-1).
  @issue-513
  Scenario: A reference written before the context entry existed reads back empty but survives in the canonical record
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    And I create a "widget" named "Bolt cutter" with "supplier" referring to the "vendor" "Acme"
    When the "catalog" preset declares a context entry for "supplier" on "widget"
    And the twin restarts against the same database
    Then reading the "widget" "Bolt cutter" back through the projection returns no value for "supplier"
    And the JSON-LD representation of the "widget" "Bolt cutter" still carries a "supplier" edge to the "vendor" "Acme"

  @issue-513
  Scenario: Reprojecting after the context entry lands populates the reference column
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    And I create a "widget" named "Bolt cutter" with "supplier" referring to the "vendor" "Acme"
    And the "catalog" preset declares a context entry for "supplier" on "widget"
    And the twin restarts against the same database
    And reading the "widget" "Bolt cutter" back through the projection returns no value for "supplier"
    When the operator reprojects the event feed
    Then reading the "widget" "Bolt cutter" back through the projection returns "supplier" as the "vendor" "Acme"

  @issue-513
  Scenario: A context entry the operator deleted is restored without rewriting the stored schema
    Given the operator deletes "maker" from the stored "widget" context
    When the twin restarts against the same database
    Then the stored "widget" context has an entry for "maker"
    And the stored "widget" schema is byte-identical to the one stored before the restart
