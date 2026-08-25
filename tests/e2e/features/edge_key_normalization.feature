@wip @issue-523
Feature: One migration rewrites stored edges to property-name keys
  As an operator whose instance predates the compact edge storage change
  I want one command that rewrites every stored event's edges to property-name keys
  So that a later namespace change is a preset edit plus a reprojection, with no aliases

  # WHY THIS EXISTS. #515 changed the WRITE path: an edges node is now keyed by
  # the property name, and the document's own `@context` carries the mapping.
  # Everything written before that ships as a predicate IRI resolved at write
  # time:
  #
  #   {"@id":"urn:transaction:…","https://schema.org/isPartOf":{"@id":"urn:statement:…"}}
  #
  # `ResourceCreated` carries that graph and events are immutable, so a
  # reprojection reproduces the old key no matter what the current context says.
  # That is what makes a rename expensive today: every moved term needs an alias,
  # forever, on every instance (#518). Normalizing the stored events once removes
  # the problem class instead of managing it per rename.
  #
  # The migration rewrites EVENTS. The canonical record, the projection columns
  # and the knowledge graph are all downstream of the event feed, so the
  # documented procedure is: stop the server, back up the database, run the
  # migration, run `weos worker reproject`. The rollback is restoring the backup
  # — the command appends nothing and deletes nothing, so the event feed after a
  # run holds exactly the same aggregates, sequence numbers and event types it
  # held before.
  #
  # ---------------------------------------------------------------------------
  # CONTRACT — the parts these scenarios pin, so an implementer knows what is
  # being asserted rather than inferring it from step wording.
  #
  # 1. RESOLUTION AGREES WITH THE READ PATH. The ticket says "resolve each IRI
  #    through BuildReverseMap". Taken literally that would DECLINE the largest
  #    legacy population: a reference property the preset never gave a `@context`
  #    term (#510). Its edge key is `@vocab` + property name, which has no
  #    reverse-map entry, yet `jsonld.EdgeProperty` resolves it today and every
  #    reader returns the value. A migration that reported those as unresolvable
  #    would leave behind exactly the records it exists to fix. So the rule is:
  #    an edge key is resolvable when `jsonld.EdgeProperty` resolves it — term,
  #    then `weos:termAliases`, then the `@vocab` prefix — and "no reverse entry
  #    at all" means none of the three resolve it.
  #
  # 2. AMBIGUITY IS DETECTED FORWARD, NOT FROM THE REVERSE MAP.
  #    `BuildReverseMap` is a `map[string]string`: two properties on one
  #    predicate collapse into one entry and which one survives is map-iteration
  #    order. It cannot report its own ambiguity. Group the type's properties by
  #    resolved predicate IRI (the `ParseContext` direction) and treat any IRI
  #    claimed by more than one property as ambiguous FOR EVERY EDGE keyed by it.
  #
  # 3. AMBIGUOUS MEANS SHARED PREDICATE — the target type does not rescue it.
  #    #521 refuses at boot only when two reference properties share a predicate
  #    AND a target type slug, so that shape cannot reach this command: the
  #    instance does not start. The shape that DOES reach it is two properties
  #    sharing a predicate with different targets, which boots happily. A reader
  #    could in principle guess from the target's `urn:<typeSlug>:…`, and this
  #    contract says do not: guessing is the failure the epic exists to stop.
  #
  # 4. THE REWRITTEN EVENT IS INDISTINGUISHABLE FROM A FRESH WRITE. The edges
  #    node is keyed by property name AND the document's embedded `@context`
  #    gains the same term mappings `buildStorableContext` writes today. Without
  #    that second half the document no longer expands to a graph and the
  #    knowledge graph — which loads this payload verbatim — silently loses every
  #    predicate. Consequence, and it is intended: for an edge that resolved
  #    through an ALIAS, the predicate in the graph moves to the term's CURRENT
  #    IRI. That is the rename the operator already adopted, and it is why no
  #    alias is needed after normalization.
  #
  # 5. REPORT MARKERS, so these scenarios can find the lines. The report is a
  #    dry run unless the operator asks for a write.
  #      - per resource type: a line naming the type slug and the number of
  #        events it would rewrite (or did rewrite)
  #      - ambiguous: a line containing "ambiguous edge key", the resource id,
  #        the predicate IRI, and both candidate property names
  #      - unresolvable: a line containing "unresolved edge key", the resource id
  #        and the IRI
  #    A problem on one resource type never stops another from being rewritten.
  # ---------------------------------------------------------------------------

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

  # --- the round trip the ticket asks a fixture to prove ---

  Scenario: A pre-#515 event is rewritten to key its edge by the property name
    Given a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And the stored event for the "widget" "Bolt cutter" keys its "maker" edge by "https://schema.org/maker"
    When the operator normalizes the stored edge keys and writes
    Then the normalization rewrote 1 event for "widget"
    And the stored event for the "widget" "Bolt cutter" keys its "maker" edge by the property name
    And the stored event for the "widget" "Bolt cutter" keys no edge by an absolute IRI
    And the stored event for the "widget" "Bolt cutter" maps "maker" to "https://schema.org/maker" in its own context
    When the operator reprojects the event feed
    Then reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Acme"
    And the API read of the "widget" "Bolt cutter" returns "maker" as the "vendor" "Acme"
    And the JSON-LD representation of the "widget" "Bolt cutter" still carries a "maker" edge to the "vendor" "Acme"
    And the triple store holds "https://schema.org/maker" from the "widget" "Bolt cutter" to the "vendor" "Acme"

  Scenario: A pre-#515 list edge is rewritten and still reads back as a list
    Given a "vendor" named "Globex" exists
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "suppliers" referring to the vendors "Acme, Globex"
    When the operator normalizes the stored edge keys and writes
    And the operator reprojects the event feed
    Then the stored event for the "widget" "Bolt cutter" keys its "suppliers" edge by the property name
    And reading the "widget" "Bolt cutter" back through the projection returns "suppliers" as the vendors "Acme, Globex"
    And the API read of the "widget" "Bolt cutter" returns "suppliers" as the vendors "Acme, Globex"
    And the JSON-LD representation of the "widget" "Bolt cutter" carries "suppliers" edges to the vendors "Acme, Globex"

  # A resource that was edited carries a Resource.Updated whose payload is the
  # same graph shape. "Every stored event" has to mean both, or the migration
  # normalizes the creation and leaves the edit keyed by an IRI — and the edit is
  # what a reprojection lands on last.
  Scenario: An update event carries the same graph and is rewritten too
    Given a "vendor" named "Globex" exists
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And the "widget" "Bolt cutter" was updated by the pre-#515 binary with "maker" referring to the "vendor" "Globex"
    When the operator normalizes the stored edge keys and writes
    Then the normalization rewrote 2 events for "widget"
    And every stored event for the "widget" "Bolt cutter" keys no edge by an absolute IRI
    When the operator reprojects the event feed
    Then reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Globex"

  # --- dry run by default ---

  Scenario: The command reports per resource type and writes nothing until told to
    Given a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    When the operator normalizes the stored edge keys as a dry run
    Then the normalization reports 1 event to rewrite for "widget"
    And the normalization reports itself as a dry run
    And the stored event for the "widget" "Bolt cutter" keys its "maker" edge by "https://schema.org/maker"
    When the operator normalizes the stored edge keys and writes
    Then the stored event for the "widget" "Bolt cutter" keys its "maker" edge by the property name

  Scenario: The dry run counts each resource type separately
    Given a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And a "widget" named "Hex key" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And the "catalog" preset adds a "flagship" reference property to "vendor" targeting "widget"
    And the twin restarts against the same database
    And a "vendor" named "Globex" written by the pre-#515 binary with "flagship" referring to the "widget" "Bolt cutter"
    When the operator normalizes the stored edge keys as a dry run
    Then the normalization reports 2 events to rewrite for "widget"
    And the normalization reports 1 event to rewrite for "vendor"

  # --- idempotence ---

  Scenario: A second run rewrites nothing
    Given a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And the operator normalizes the stored edge keys and writes
    When the operator normalizes the stored edge keys as a dry run
    Then the normalization reports nothing to rewrite
    When the operator normalizes the stored edge keys and writes
    Then the normalization rewrote 0 events
    And the stored events are byte-identical to the ones stored before the second run

  Scenario: An instance written entirely after #515 is left alone
    Given I create a "widget" named "Bolt cutter" with "maker" referring to the "vendor" "Acme"
    When the operator normalizes the stored edge keys as a dry run
    Then the normalization reports nothing to rewrite
    When the operator normalizes the stored edge keys and writes
    Then the stored events are byte-identical to the ones stored before the run
    And reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Acme"

  # --- the populations the reverse map alone does not cover ---

  # The #510 population: a reference property the preset never gave a context
  # term. Its edge key is `@vocab` + the property name, which BuildReverseMap has
  # no entry for. It reads back today through EdgeProperty's @vocab fallback, so
  # the migration must rewrite it — see CONTRACT 1.
  Scenario: An edge that only @vocab resolves is rewritten, not reported
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "supplier" referring to the "vendor" "Acme"
    And the stored event for the "widget" "Bolt cutter" keys its "supplier" edge by "https://schema.org/supplier"
    When the operator normalizes the stored edge keys and writes
    Then the normalization reports no unresolved edge key
    And the stored event for the "widget" "Bolt cutter" keys its "supplier" edge by the property name
    When the operator reprojects the event feed
    Then reading the "widget" "Bolt cutter" back through the projection returns "supplier" as the "vendor" "Acme"

  # The payoff scenario. An adopted alias exists precisely so an edge written
  # under an old IRI keeps resolving. Normalization consumes the alias: the key
  # becomes the property name, so the alias has no work left to do and can be
  # dropped — which is the whole "later renames need no aliases" claim.
  Scenario: An edge resolved through a recorded alias is rewritten, and the alias is then dead weight
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "supplier" referring to the "vendor" "Acme"
    And the "catalog" preset declares "supplier" as "https://example.org/catalog#supplier" in the "widget" context
    And the twin restarts against the same database
    And the operator adopts the held "supplier" context term for "widget"
    And the stored "widget" context records "https://schema.org/supplier" as a historical IRI for "supplier"
    When the operator normalizes the stored edge keys and writes
    Then the stored event for the "widget" "Bolt cutter" keys its "supplier" edge by the property name
    And the stored event for the "widget" "Bolt cutter" maps "supplier" to "https://example.org/catalog#supplier" in its own context
    When the operator reprojects the event feed
    Then reading the "widget" "Bolt cutter" back through the projection returns "supplier" as the "vendor" "Acme"
    When the operator removes every historical IRI from the stored "widget" context
    And the operator reprojects the event feed
    Then reading the "widget" "Bolt cutter" back through the projection returns "supplier" as the "vendor" "Acme"
    And the JSON-LD representation of the "widget" "Bolt cutter" still carries a "supplier" edge to the "vendor" "Acme"

  # After normalization a rename is a preset edit plus a reprojection. Nothing
  # records the old IRI and nothing needs to.
  Scenario: A rename after normalization needs no alias
    Given a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And the operator normalizes the stored edge keys and writes
    When the operator maps "maker" to "https://example.org/catalog#madeBy" in the stored "widget" context
    And the operator reprojects the event feed
    Then reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Acme"
    And the API read of the "widget" "Bolt cutter" returns "maker" as the "vendor" "Acme"
    And the stored "widget" context records no historical IRI for "maker"

  # --- the branches that report rather than rewrite ---

  # Two reference properties on one predicate with DIFFERENT target types. #521
  # accepts this shape at boot — compact storage makes it safe for new writes,
  # each property keeps its own key. It is exactly the shape a legacy edge cannot
  # be attributed to: the key names the predicate, and the predicate names two
  # properties. The target's `urn:<typeSlug>:…` would let a reader guess; this
  # contract says report instead.
  Scenario: An edge on a shared predicate is reported with both candidates, never rewritten
    Given the "catalog" preset adds a "partner" reference property to "widget" targeting "widget"
    And the "catalog" preset declares "maker" as "https://schema.org/associated" in the "widget" context
    And the "catalog" preset declares "partner" as "https://schema.org/associated" in the "widget" context
    And the twin restarts against the same database
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    When the operator normalizes the stored edge keys and writes
    Then the normalization reports the "widget" "Bolt cutter" as ambiguous on "https://schema.org/associated", naming "maker" and "partner"
    And the stored event for the "widget" "Bolt cutter" keys its "maker" edge by "https://schema.org/associated"
    And the normalization rewrote 0 events for "widget"

  # An ambiguous type must not quarantine the whole instance. The operator needs
  # the clean types migrated and the faulty one named, in one pass.
  Scenario: An ambiguous type does not stop a clean one being rewritten
    Given the "catalog" preset adds a "partner" reference property to "widget" targeting "widget"
    And the "catalog" preset declares "maker" as "https://schema.org/associated" in the "widget" context
    And the "catalog" preset declares "partner" as "https://schema.org/associated" in the "widget" context
    And the "catalog" preset adds a "flagship" reference property to "vendor" targeting "widget"
    And the twin restarts against the same database
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And a "vendor" named "Globex" written by the pre-#515 binary with "flagship" referring to the "widget" "Bolt cutter"
    When the operator normalizes the stored edge keys and writes
    Then the normalization reports the "widget" "Bolt cutter" as ambiguous on "https://schema.org/associated", naming "maker" and "partner"
    And the normalization rewrote 1 event for "vendor"
    And the stored event for the "vendor" "Globex" keys its "flagship" edge by the property name

  # An IRI no term, no alias and no @vocab prefix accounts for. It is already
  # unreadable — the point of reporting it is that the operator learns it exists
  # instead of the migration quietly dropping the only record that it did.
  Scenario: An edge nothing resolves is reported and left exactly as it was
    Given the operator maps "maker" to "https://example.org/legacy#madeBy" in the stored "widget" context
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And the operator maps "maker" to "https://schema.org/maker" in the stored "widget" context
    When the operator normalizes the stored edge keys and writes
    Then the normalization reports the "widget" "Bolt cutter" as unresolved on "https://example.org/legacy#madeBy"
    And the stored event for the "widget" "Bolt cutter" keys its "maker" edge by "https://example.org/legacy#madeBy"
    And the normalization rewrote 0 events for "widget"

  Scenario: A resource with one unresolvable edge keeps its resolvable ones
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor"
    And the twin restarts against the same database
    And the operator maps "maker" to "https://example.org/legacy#madeBy" in the stored "widget" context
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with these references:
      | property | vendor |
      | maker    | Acme   |
      | supplier | Acme   |
    And the operator maps "maker" to "https://schema.org/maker" in the stored "widget" context
    When the operator normalizes the stored edge keys and writes
    Then the normalization reports the "widget" "Bolt cutter" as unresolved on "https://example.org/legacy#madeBy"
    And the stored event for the "widget" "Bolt cutter" keys its "supplier" edge by the property name
    And the stored event for the "widget" "Bolt cutter" keys its "maker" edge by "https://example.org/legacy#madeBy"
    When the operator reprojects the event feed
    Then reading the "widget" "Bolt cutter" back through the projection returns "supplier" as the "vendor" "Acme"

  # --- the guarantee that makes rewriting events acceptable ---

  Scenario: The run reproduces every read it found, across every type with a reference
    Given a "vendor" named "Globex" exists
    And the "catalog" preset adds a "flagship" reference property to "vendor" targeting "widget"
    And the twin restarts against the same database
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with these references:
      | property  | vendor       |
      | maker     | Acme         |
      | suppliers | Acme, Globex |
    And a "vendor" named "Initech" written by the pre-#515 binary with "flagship" referring to the "widget" "Bolt cutter"
    And the read of every resource with a reference property is captured
    When the operator normalizes the stored edge keys and writes
    And the operator reprojects the event feed
    Then the read of every resource with a reference property matches what was captured

  # Pericarp events carry aggregateID, seqNo and payload — no hash chain, no
  # signature — so re-encoding a payload is safe. What is NOT safe is the
  # migration appending, deleting or renumbering anything, because the rollback
  # story is "restore the database" and a reprojection replays by position.
  Scenario: The run rewrites payloads only — no event is added, removed or renumbered
    Given a "vendor" named "Globex" exists
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And a "widget" named "Hex key" written by the pre-#515 binary with "suppliers" referring to the vendors "Acme, Globex"
    When the operator normalizes the stored edge keys and writes
    Then the event feed holds the same events in the same order as before the run
    And every event keeps its aggregate id, sequence number and event type
    And no event of a type other than "Resource.Created" or "Resource.Updated" was rewritten

  Scenario: The entity node and the intrinsic properties beside an edge are untouched
    Given the "catalog" preset adds a "sku" string property to "widget"
    And the twin restarts against the same database
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme" and "sku" set to "BC-100"
    When the operator normalizes the stored edge keys and writes
    Then the entity node of the stored event for the "widget" "Bolt cutter" is byte-identical to the one stored before the run
    When the operator reprojects the event feed
    Then reading the "widget" "Bolt cutter" back through the projection returns "sku" as "BC-100"
    And reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Acme"

  # The knowledge graph loads the event payload verbatim as JSON-LD. A compact
  # key with no term mapping in the document's own @context expands to nothing,
  # so the predicate would vanish from the graph while the projection and the API
  # read both stayed green — see CONTRACT 4.
  Scenario: The knowledge graph keeps the predicate the legacy document carried
    Given a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    When the operator normalizes the stored edge keys and writes
    And the operator reprojects the event feed
    Then the triple store holds "https://schema.org/maker" from the "widget" "Bolt cutter" to the "vendor" "Acme"

  Scenario: A term declared away from @vocab keeps its predicate through the rewrite
    Given the "catalog" preset adds a "courier" reference property to "widget" targeting "vendor"
    And the "catalog" preset declares "courier" as "https://example.org/catalog#courier" in the "widget" context
    And the twin restarts against the same database
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "courier" referring to the "vendor" "Acme"
    When the operator normalizes the stored edge keys and writes
    Then the stored event for the "widget" "Bolt cutter" keys its "courier" edge by the property name
    And the stored event for the "widget" "Bolt cutter" maps "courier" to "https://example.org/catalog#courier" in its own context
    When the operator reprojects the event feed
    Then the triple store holds "https://example.org/catalog#courier" from the "widget" "Bolt cutter" to the "vendor" "Acme"
    And the triple store holds no "https://schema.org/courier" edge from the "widget" "Bolt cutter"
