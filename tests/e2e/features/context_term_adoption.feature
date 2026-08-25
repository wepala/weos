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

  # === ISSUE #518: the second kind of held term ===============================
  #
  # Everything above adopts a term the preset ADDS — a `Move`, held because the
  # addition would repoint a predicate that already has data. There is a second
  # kind the boot has always classified and adoption has never been able to
  # take: a `Conflict`, a term present in BOTH the stored and the preset context
  # with different definitions. That is what a namespace rename or a corrected
  # prefix looks like on an upgrade, and it is held at its stored definition for
  # exactly the same reason a Move is — taking the preset's IRI would orphan
  # every edge already keyed by the old one.
  #
  # The two are kept apart in the operator's view because the DECISION differs.
  # A Move asks "does this property's data belong at the new IRI?". A Conflict
  # asks "which of these two IRIs is right — mine or the preset's?", and the
  # operator who edited the stored term may well answer "mine" and adopt
  # nothing. The listing must therefore say which kind each held term is, and
  # what adopting it will do, rather than presenting one undifferentiated list.
  #
  # Adoption itself is the same mechanism: record the IRI each affected property
  # resolves to TODAY under "weos:termAliases", then take the preset's
  # definition. Old edges keep reading through the alias, new writes land on the
  # new IRI, and the two coexist. Since #515 a compact edge is keyed by the
  # property name and never inverts the context at all, so the alias only earns
  # its keep on documents an older binary wrote — which is what this story is
  # for after the rescope of 2026-08-24: it is the fallback for instances that
  # cannot take #523's `normalize-edge-keys` migration, or whose reverse map is
  # ambiguous, or for any term change made before normalization. The scenarios
  # below therefore plant pre-#515 records deliberately rather than relying on a
  # new write, which would prove nothing.
  #
  # Three entries are not ordinary terms:
  #
  #   - `@vocab` is the fallback every untermed property resolves through, so
  #     adopting it repoints all of them at once. An operator may want that,
  #     never as a side effect of a sweep.
  #   - `@type` is a class, not a predicate, so no alias can move it: adopting
  #     it changes what NEW writes carry and leaves existing records where they
  #     are. A sweep never takes it (#521); naming it takes it and hands over
  #     the re-stamp route (#523).
  #   - A PREFIX the stored `@type` expands through moves the class without
  #     being named by it. #521 settled that rule for a Move through
  #     ClassMovers; these scenarios settle the same rule for a Conflict, which
  #     is the shape the real presets are actually in — `"@type":"mp:FoodItem"`
  #     is textually identical on both sides of the weos.org → weos.io move, so
  #     only the `mp` prefix diverges.

  @wip @issue-518
  Scenario: The held-terms listing tells a term the preset adds apart from one it renames
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    And I create a "widget" named "Bolt cutter" with "supplier" referring to the "vendor" "Acme"
    And the "catalog" preset declares "supplier" as "https://example.org/catalog#supplier" in the "widget" context
    And the "catalog" preset declares "maker" as "https://example.org/catalog#madeBy" in the "widget" context
    And the twin restarts against the same database
    Then the held-terms listing for "widget" names "supplier" as a term the preset adds
    And the held-terms listing for "widget" names "maker" as a term the preset redefines
    And the held-terms listing for "widget" reports "maker" as stored at "https://schema.org/maker"
    And the held-terms listing for "widget" reports "maker" as offered at "https://example.org/catalog#madeBy"
    And the held-terms listing for "widget" says adopting "maker" keeps its edges under "https://schema.org/maker" readable

  # The boot already logs a divergence, but only the Move line carries a remedy
  # (`AdoptRemedy`). While a Conflict could not be adopted that was honest;
  # once it can, a warn line with no way out strands the operator exactly as
  # before and the type is reported every start forever.
  @wip @issue-518
  Scenario: The boot names the command that adopts a term the preset renamed
    Given I create a "widget" named "Bolt cutter" with "maker" referring to the "vendor" "Acme"
    And the "catalog" preset declares "maker" as "https://example.org/catalog#madeBy" in the "widget" context
    When the twin restarts against the same database
    Then the boot reconcile reports the "maker" context term as held for "widget"
    And the boot's held report for "widget" names a command that adopts "maker"

  @wip @issue-518
  Scenario: An edge written under the old IRI still reads once the rename is adopted
    Given a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And the "catalog" preset declares "maker" as "https://example.org/catalog#madeBy" in the "widget" context
    And the twin restarts against the same database
    When the operator adopts the held "maker" context term for "widget"
    Then the stored "widget" context still maps "maker" to "https://example.org/catalog#madeBy"
    And the stored "widget" context records "https://schema.org/maker" as a historical IRI for "maker"
    And the API read of the "widget" "Bolt cutter" returns "maker" as the "vendor" "Acme"
    And the JSON-LD representation of the "widget" "Bolt cutter" still carries a "maker" edge to the "vendor" "Acme"
    When the operator reprojects the event feed
    Then reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Acme"

  @wip @issue-518
  Scenario: A write made after the rename is adopted lands on the new IRI beside the old edge
    Given a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And the "catalog" preset declares "maker" as "https://example.org/catalog#madeBy" in the "widget" context
    And the twin restarts against the same database
    When the operator adopts the held "maker" context term for "widget"
    And I create a "widget" named "Hex key" with "maker" referring to the "vendor" "Acme"
    Then the stored canonical record for the "widget" "Hex key" maps "maker" to "https://example.org/catalog#madeBy" in its own context
    And the triple store holds "https://example.org/catalog#madeBy" from the "widget" "Hex key" to the "vendor" "Acme"
    And the API read of the "widget" "Hex key" returns "maker" as the "vendor" "Acme"
    And the API read of the "widget" "Bolt cutter" returns "maker" as the "vendor" "Acme"

  @wip @issue-518
  Scenario: Adopting the same rename twice records nothing a second time
    Given a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And the "catalog" preset declares "maker" as "https://example.org/catalog#madeBy" in the "widget" context
    And the twin restarts against the same database
    When the operator adopts the held "maker" context term for "widget"
    And the operator adopts the "maker" context term for "widget" again
    Then the stored "widget" context is byte-identical to the one stored before the second adoption
    And the stored "widget" context records exactly one historical IRI for "maker"
    And the API read of the "widget" "Bolt cutter" returns "maker" as the "vendor" "Acme"

  @wip @issue-518
  Scenario: The boot settles once the rename is adopted
    Given a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And the "catalog" preset declares "maker" as "https://example.org/catalog#madeBy" in the "widget" context
    And the twin restarts against the same database
    When the operator adopts the held "maker" context term for "widget"
    And the twin restarts against the same database again
    Then the boot reconcile does not report the "maker" context term as held for "widget"
    And the boot reconcile records no failure for "widget"
    And the stored "widget" context still maps "maker" to "https://example.org/catalog#madeBy"
    And the held-terms listing for "widget" names no held term

  @wip @issue-518
  Scenario: A sweep takes a renamed term alongside an added one
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And I create a "widget" named "Hex key" with "supplier" referring to the "vendor" "Acme"
    And the "catalog" preset declares "supplier" as "https://example.org/catalog#supplier" in the "widget" context
    And the "catalog" preset declares "maker" as "https://example.org/catalog#madeBy" in the "widget" context
    And the twin restarts against the same database
    When the operator adopts every held context term for "widget"
    Then the stored "widget" context still maps "maker" to "https://example.org/catalog#madeBy"
    And the stored "widget" context still maps "supplier" to "https://example.org/catalog#supplier"
    And the stored "widget" context records "https://schema.org/maker" as a historical IRI for "maker"
    And the stored "widget" context records "https://schema.org/supplier" as a historical IRI for "supplier"
    And the API read of the "widget" "Bolt cutter" returns "maker" as the "vendor" "Acme"
    When the operator reprojects the event feed
    Then reading the "widget" "Hex key" back through the projection returns "supplier" as the "vendor" "Acme"

  # Two upgrades, two renames. The second adoption must ADD an alias rather than
  # replace the first: edges exist under both retired IRIs, and dropping either
  # orphans the records written between the two upgrades.
  @wip @issue-518
  Scenario: A term renamed twice keeps the edges written under each retired IRI readable
    Given a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And the "catalog" preset declares "maker" as "https://example.org/catalog#madeBy" in the "widget" context
    And the twin restarts against the same database
    And the operator adopts the held "maker" context term for "widget"
    And a "widget" named "Hex key" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    When the "catalog" preset declares "maker" as "https://example.org/catalog#builtBy" in the "widget" context
    And the twin restarts against the same database again
    And the operator adopts the held "maker" context term for "widget"
    Then the stored "widget" context still maps "maker" to "https://example.org/catalog#builtBy"
    And the stored "widget" context records "https://schema.org/maker" as a historical IRI for "maker"
    And the stored "widget" context records "https://example.org/catalog#madeBy" as a historical IRI for "maker"
    And the API read of the "widget" "Bolt cutter" returns "maker" as the "vendor" "Acme"
    And the API read of the "widget" "Hex key" returns "maker" as the "vendor" "Acme"

  # A prefix is not itself a predicate: nothing is keyed by it, so it takes no
  # alias of its own. What moves is every stored term that expands through it,
  # and EVERY one of them needs its old IRI recorded — recording only the first
  # would leave the rest orphaned, which is the whole failure this prevents.
  # This is the shape the real presets are in: one prefix, several properties.
  @wip @issue-518
  Scenario: A renamed prefix records an alias for every property that expands through it
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor"
    And the twin restarts against the same database
    And the operator maps "cat" to "https://schema.org/" in the stored "widget" context
    And the operator maps "maker" to "cat:madeBy" in the stored "widget" context
    And the operator maps "supplier" to "cat:suppliedBy" in the stored "widget" context
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And a "widget" named "Hex key" written by the pre-#515 binary with "supplier" referring to the "vendor" "Acme"
    And the "catalog" preset declares "cat" as "https://example.org/catalog#" in the "widget" context
    And the twin restarts against the same database
    Then the held-terms listing for "widget" names "cat" as a term the preset redefines
    And the held-terms listing for "widget" reports "cat" once
    And the held-terms listing for "widget" says adopting "cat" moves "maker" off "https://schema.org/madeBy"
    And the held-terms listing for "widget" says adopting "cat" moves "supplier" off "https://schema.org/suppliedBy"
    When the operator adopts the held "cat" context term for "widget"
    Then the stored "widget" context still maps "cat" to "https://example.org/catalog#"
    And the stored "widget" context still maps "maker" to "cat:madeBy"
    And the stored "widget" context records "https://schema.org/madeBy" as a historical IRI for "maker"
    And the stored "widget" context records "https://schema.org/suppliedBy" as a historical IRI for "supplier"
    And the stored "widget" context records no historical IRI for "cat"
    And the API read of the "widget" "Bolt cutter" returns "maker" as the "vendor" "Acme"
    And the API read of the "widget" "Hex key" returns "supplier" as the "vendor" "Acme"

  # Holding @vocab back is not a reason to refuse the type: the other held terms
  # in the same sweep are still taken.
  @wip @issue-518
  Scenario: A sweep never repoints @vocab
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "supplier" referring to the "vendor" "Acme"
    And the "catalog" preset declares "@vocab" as "https://example.org/catalog#" in the "widget" context
    And the "catalog" preset declares "maker" as "https://example.org/catalog#madeBy" in the "widget" context
    And the twin restarts against the same database
    And the held-terms listing for "widget" names "@vocab" as a term the preset redefines
    When the operator adopts every held context term for "widget"
    Then the stored "widget" context still maps "@vocab" to "https://schema.org/"
    And the stored "widget" context records no historical IRI for "supplier"
    And the stored "widget" context still maps "maker" to "https://example.org/catalog#madeBy"
    And the held-terms listing for "widget" still names "@vocab" as held

  # Adopting @vocab moves every property that had no term of its own, so each of
  # them — not the keyword — carries the alias. The edges only become readable
  # because of it: an untermed property has no forward entry in the reverse map
  # at all, so the recorded IRI is the single thing that resolves them.
  @wip @issue-518
  Scenario: An operator who names @vocab adopts it, and every property it moved stays readable
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the "catalog" preset adds a "distributor" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "supplier" referring to the "vendor" "Acme"
    And a "widget" named "Hex key" written by the pre-#515 binary with "distributor" referring to the "vendor" "Acme"
    And the "catalog" preset declares "@vocab" as "https://example.org/catalog#" in the "widget" context
    And the twin restarts against the same database
    When the operator adopts the held "@vocab" context term for "widget"
    Then the stored "widget" context still maps "@vocab" to "https://example.org/catalog#"
    And the stored "widget" context records "https://schema.org/supplier" as a historical IRI for "supplier"
    And the stored "widget" context records "https://schema.org/distributor" as a historical IRI for "distributor"
    And the stored "widget" context records no historical IRI for "@vocab"
    And the API read of the "widget" "Bolt cutter" returns "supplier" as the "vendor" "Acme"
    And the API read of the "widget" "Hex key" returns "distributor" as the "vendor" "Acme"

  @wip @issue-518
  Scenario: A sweep leaves a redefined @type where it is, and says how to take it
    Given the operator maps "@type" to "Thing" in the stored "widget" context
    And a "widget" named "Bolt cutter" exists
    And the "catalog" preset declares "@type" as "Product" in the "widget" context
    And the "catalog" preset declares "maker" as "https://example.org/catalog#madeBy" in the "widget" context
    And the twin restarts against the same database
    When the operator adopts every held context term for "widget"
    And a "widget" named "Hex key" exists
    Then the stored "widget" context still maps "@type" to "Thing"
    And the stored "widget" context still maps "maker" to "https://example.org/catalog#madeBy"
    And the "widget" resources "Bolt cutter" and "Hex key" carry the same RDF type
    And the operator is told the class was not adopted and how to adopt it

  # Adopting a class changes what NEW writes carry and nothing else — no alias
  # can reach back for a class, because a class keys no edge. Saying so, and
  # naming the re-stamp that does reach back, is the difference between an
  # operator who migrates and one who discovers two classes in the graph later.
  @wip @issue-518
  Scenario: Naming a redefined @type adopts it and hands over the re-stamp route
    Given the operator maps "@type" to "Thing" in the stored "widget" context
    And a "widget" named "Bolt cutter" exists
    And the "catalog" preset declares "@type" as "Product" in the "widget" context
    And the twin restarts against the same database
    When the operator adopts the held "@type" context term for "widget"
    And a "widget" named "Hex key" exists
    Then the stored "widget" context still maps "@type" to "Product"
    And the stored "widget" context records no historical IRI for "@type"
    And the adoption tells the operator to re-stamp the existing records and reproject
    And the "widget" resources "Bolt cutter" and "Hex key" carry different RDF types

  # The prefix case #521 could only settle for a Move. A sweep must treat a
  # Conflict prefix the class expands through exactly as it treats @type: leave
  # it, and say so. The term is held whole — the properties it also moves are
  # NOT adopted behind the class's back.
  @wip @issue-518
  Scenario: A sweep never takes a renamed prefix the class expands through
    Given the operator maps "cat" to "https://schema.org/" in the stored "widget" context
    And the operator maps "@type" to "cat:Widget" in the stored "widget" context
    And the operator maps "maker" to "cat:madeBy" in the stored "widget" context
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And the "catalog" preset declares "cat" as "https://example.org/catalog#" in the "widget" context
    And the twin restarts against the same database
    When the operator adopts every held context term for "widget"
    Then the stored "widget" context still maps "cat" to "https://schema.org/"
    And the stored "widget" context still maps "@type" to "cat:Widget"
    And the stored "widget" context records no historical IRI for "maker"
    And the held-terms listing for "widget" still names "cat" as held
    And the operator is told the class was not adopted and how to adopt it

  @wip @issue-518
  Scenario: Naming that prefix adopts it, says the class moves, and leaves the edges readable
    Given the operator maps "cat" to "https://schema.org/" in the stored "widget" context
    And the operator maps "@type" to "cat:Widget" in the stored "widget" context
    And the operator maps "maker" to "cat:madeBy" in the stored "widget" context
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And the "catalog" preset declares "cat" as "https://example.org/catalog#" in the "widget" context
    And the twin restarts against the same database
    When the operator adopts the held "cat" context term for "widget"
    Then the stored "widget" context still maps "cat" to "https://example.org/catalog#"
    And the stored "widget" context records "https://schema.org/madeBy" as a historical IRI for "maker"
    And the adoption reports the "widget" class moving from "https://schema.org/Widget" to "https://example.org/catalog#Widget"
    And the adoption tells the operator to re-stamp the existing records and reproject
    And the API read of the "widget" "Bolt cutter" returns "maker" as the "vendor" "Acme"
