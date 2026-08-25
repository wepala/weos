@wip @issue-519
Feature: One check counts the resources whose edges still key by predicate IRI
  As an operator running the edge-key normalization on a live instance
  I want one read-only command that counts the resources still keyed by predicate IRI
  So that I can prove the migration is complete, and keep proving it afterwards

  # WHY THIS EXISTS. #515 changed the WRITE path: an edges node is keyed by the
  # property name and the document's own `@context` carries the mapping. #523
  # rewrites the stored EVENTS to that shape. Neither one forces the old shape
  # out on its own: `jsonld.EdgeProperty` reads both key forms indefinitely, so
  # an instance can sit half-normalized — a type the migration declined, a
  # restored backup, an import from an old instance — and nothing says so.
  #
  # This story is that check. It was originally written to SIZE the migration;
  # #515 shipped 2026-08-24, so every record written before then is IRI-keyed
  # and the size is "all of it". What is left, and what matters more, is the
  # VERIFICATION: run it before #523 to see a number, run it after to see zero,
  # and keep running it so the number stays zero.
  #
  # ---------------------------------------------------------------------------
  # CONTRACT — the parts these scenarios pin, and the design questions settled
  # here so an implementer is not left to infer them from step wording.
  #
  # 1. TWO SURFACES, COUNTED SEPARATELY. The story says "stored resources".
  #    There are two things that phrase can mean and they disagree for a real
  #    window of the migration, so the check reports BOTH, labelled:
  #
  #      - CANONICAL RECORDS — the `data` column of every projection table plus
  #        the generic `resources` table. This is what every reader serves, so
  #        it is the number that answers "is my instance still on the old
  #        shape". A count that walked only the projection tables would report
  #        zero while pre-projection-table records sat unmigrated, so it must
  #        walk both.
  #      - EVENTS — the Resource.Created and Resource.Updated payloads, which
  #        are what #523 rewrites and what a reprojection replays from. A
  #        canonical record that reads clean is meaningless if the event behind
  #        it still holds the IRI: the next reproject puts it back.
  #
  #    They diverge on purpose between `normalize --write` and `worker
  #    reproject`: the events are clean and the canonical records are not. That
  #    window is the operator's most likely mistake — declaring victory after
  #    the migration and before the reprojection — so the check must show it
  #    rather than average it away. The first scenario is that window.
  #
  # 2. THE UNIT OF THE HEADLINE NUMBER IS A RESOURCE, NOT AN EVENT OR AN EDGE.
  #    The story asks how many RESOURCES still key by IRI. A resource with a
  #    Created and an Updated event counts once; a resource with three IRI-keyed
  #    edges counts once. (#523's own report counts events, which is the right
  #    unit for "how much work is left to do"; this is the right unit for "how
  #    much of my data is on the old shape". They will legitimately differ.)
  #    The classification counts in CONTRACT 4 are per EDGE KEY, because a
  #    classification is a property of a key and one resource can hold keys of
  #    two different classes.
  #
  # 3. AN `@id` IS NOT AN EDGE KEY. A compact, post-#515 edges node still looks
  #    like `{"@id":"urn:widget:…","maker":{…}}`. `jsonld.IsIRIKey` is a
  #    colon test — it is TRUE for `@id`, and the URN value contains colons too.
  #    A check that did not exclude the `@id` control key would report every
  #    resource on every instance, forever, and the number would never reach
  #    zero. `normalizeEdgeKeys` already excludes it; the count must agree, or
  #    the two disagree about what "done" means.
  #
  # 4. THE CLASSIFICATION USES #523's RESOLVER, NOT A SECOND OPINION. Each
  #    IRI-shaped key is classified with the same `edgeKeyResolver` #523 uses:
  #
  #      - RESOLVABLE — exactly one property name claims the predicate.
  #        Includes the #510 population, whose key is `@vocab` + property name
  #        with no reverse-map entry at all: `jsonld.EdgeProperty` resolves it,
  #        every reader returns it, and #523 rewrites it. Calling those
  #        "unmapped" would tell the operator to expect a residue that never
  #        appears.
  #      - AMBIGUOUS — more than one name claims the predicate, so #523 will
  #        decline it and name the candidates.
  #      - UNMAPPED — no term, no alias and no `@vocab` prefix names it,
  #        including every edge of a resource type the store no longer holds.
  #
  #    A count that classified differently from the migration would be worse
  #    than no count: the operator would chase a number that cannot move. So
  #    the check also reports how many resources hold at least one
  #    ambiguous-or-unmapped key — the residue #523 will leave behind, which is
  #    the number to reconcile against after a run rather than "zero".
  #
  # 5. IT WRITES NOTHING. Not the events, not the canonical records, and not a
  #    single appended event — which means it must NOT boot `application.Module`,
  #    whose startup reconcile appends ResourceType events (the same reason
  #    NormalizeEdgeKeysModule exists). An operator has to be able to run this
  #    on a live instance during an incident without changing it.
  #
  # 6. IT IS A GATE, NOT A REPORT. "A permanent check keeps that number at zero"
  #    means the check has a pass/fail, so a runbook step, a cron or a CI job can
  #    depend on it: it PASSES when both surface totals are zero and FAILS
  #    otherwise, and the command exits non-zero when it fails — the same
  #    convention `normalize-edge-keys` already uses for a declined edge. The
  #    scenarios below say "the check passes / fails"; the CLI exit path and the
  #    printed report markers are pinned by a printer unit test, as
  #    `worker_normalize_test.go` does for #523.
  #
  # 7. NAMING. `weos worker count-iri-edge-keys` is the recommended sibling to
  #    `weos worker normalize-edge-keys`. The scenarios are phrased in behavior,
  #    not flags, so the name may change; what may not change is that the
  #    documented before-and-after procedure for #523 names this command.
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

  # --- the before-and-after check for #523 ---

  # The flagship scenario, and the reason the check reports two surfaces. After
  # `normalize --write` the events are clean and the canonical records — what
  # every reader serves — are not, because nothing has replayed them yet. An
  # operator who stopped there would believe the migration was done while every
  # read still went through the inversion #515 exists to remove.
  Scenario: The count is non-zero before normalization, and only reaches zero after the reprojection
    Given a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    When the operator counts the resources with IRI-keyed edges
    Then the count reports 1 "widget" resource with an IRI-keyed edge in its events
    And the count reports 1 "widget" resource with an IRI-keyed edge in its canonical record
    And the check fails
    When the operator normalizes the stored edge keys and writes
    And the operator counts the resources with IRI-keyed edges
    Then the count reports no resource with an IRI-keyed edge in its events
    And the count reports 1 "widget" resource with an IRI-keyed edge in its canonical record
    And the check fails
    When the operator reprojects the event feed
    And the operator counts the resources with IRI-keyed edges
    Then the count reports no resource with an IRI-keyed edge in its events
    And the count reports no resource with an IRI-keyed edge in its canonical record
    And the check passes

  # The zero case has to be genuinely reachable or the check is noise. Two traps
  # sit in it, and this scenario is where both are caught: the "vendor" from the
  # Background has no reference property at all, and the "widget" is a compact
  # post-#515 write whose edges node still carries an `@id` key whose value is a
  # URN. Both contain colons; neither is an edge keyed by an IRI — see CONTRACT 3.
  Scenario: An instance written entirely after #515 counts zero and the check passes
    Given I create a "widget" named "Bolt cutter" with "maker" referring to the "vendor" "Acme"
    And the stored canonical record for the "widget" "Bolt cutter" keys its "maker" edge by the property name
    When the operator counts the resources with IRI-keyed edges
    Then the count reports no resource with an IRI-keyed edge in its events
    And the count reports no resource with an IRI-keyed edge in its canonical record
    And the check passes

  # --- what the number counts ---

  # A resource that was edited carries a Resource.Updated with the same graph
  # shape, so the events surface meets it twice. The operator is being told how
  # much DATA is on the old shape, not how many rows a migration will touch —
  # see CONTRACT 2.
  Scenario: A resource with two IRI-keyed events counts once
    Given a "vendor" named "Globex" exists
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And the "widget" "Bolt cutter" was updated by the pre-#515 binary with "maker" referring to the "vendor" "Globex"
    When the operator counts the resources with IRI-keyed edges
    Then the count reports 1 "widget" resource with an IRI-keyed edge in its events
    And the count reports 1 "widget" resource with an IRI-keyed edge in its canonical record

  Scenario: The count breaks down per resource type and totals the instance
    Given a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And a "widget" named "Hex key" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And the "catalog" preset adds a "flagship" reference property to "vendor" targeting "widget"
    And the twin restarts against the same database
    And a "vendor" named "Globex" written by the pre-#515 binary with "flagship" referring to the "widget" "Bolt cutter"
    When the operator counts the resources with IRI-keyed edges
    Then the count reports 2 "widget" resources with an IRI-keyed edge in their events
    And the count reports 1 "vendor" resource with an IRI-keyed edge in its events
    And the count reports 3 resources with an IRI-keyed edge in their events across the instance
    And the count reports 3 resources with an IRI-keyed edge in their canonical records across the instance
    And the check fails

  # --- resolvable, ambiguous, unmapped ---

  Scenario: An IRI a single property claims is counted as resolvable
    Given a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    When the operator counts the resources with IRI-keyed edges
    Then the count classifies the "widget" "Bolt cutter" edge on "https://schema.org/maker" as resolvable
    And the count classifies the IRI edge keys for "widget" as:
      | resolvable | ambiguous | unmapped |
      | 1          | 0         | 0        |
    And the count reports no resource that normalization would leave IRI-keyed

  # The #510 population: a reference property the preset never gave a `@context`
  # term. Its key is `@vocab` + the property name, which `BuildReverseMap` has no
  # entry for — but `EdgeProperty` resolves it, every reader returns it, and #523
  # rewrites it. Reporting it as unmapped would promise the operator a residue
  # that never materializes — see CONTRACT 4.
  Scenario: An edge only @vocab resolves is counted as resolvable
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor" without a context entry
    And the twin restarts against the same database
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "supplier" referring to the "vendor" "Acme"
    When the operator counts the resources with IRI-keyed edges
    Then the count classifies the "widget" "Bolt cutter" edge on "https://schema.org/supplier" as resolvable
    And the count reports no resource that normalization would leave IRI-keyed
    When the operator normalizes the stored edge keys and writes
    And the operator reprojects the event feed
    And the operator counts the resources with IRI-keyed edges
    Then the count reports no resource with an IRI-keyed edge in its events
    And the check passes

  # Two properties on one predicate. #523 declines this edge and names both
  # candidates, so the count must predict exactly that: the operator is told the
  # number will NOT reach zero on this instance until the context is fixed, and
  # the second half of the scenario proves the two commands agree about it.
  Scenario: An edge on a shared predicate is counted as ambiguous, and stays counted after normalization
    Given the "catalog" preset adds a "partner" reference property to "widget" targeting "widget"
    And the "catalog" preset declares "partner" as "https://schema.org/associated" in the "widget" context
    And the twin restarts against the same database
    And the operator maps "maker" to "https://schema.org/associated" in the stored "widget" context
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    When the operator counts the resources with IRI-keyed edges
    Then the count classifies the "widget" "Bolt cutter" edge on "https://schema.org/associated" as ambiguous, naming "maker" and "partner"
    And the count classifies the IRI edge keys for "widget" as:
      | resolvable | ambiguous | unmapped |
      | 0          | 1         | 0        |
    And the count reports 1 resource that normalization would leave IRI-keyed
    When the operator normalizes the stored edge keys and writes
    And the operator reprojects the event feed
    And the operator counts the resources with IRI-keyed edges
    Then the count reports 1 "widget" resource with an IRI-keyed edge in its events
    And the check fails

  # An IRI no term, no alias and no @vocab prefix accounts for. The record is
  # already unreadable; the point of counting it is that the operator learns it
  # exists before the migration silently leaves it behind.
  Scenario: An edge nothing resolves is counted as unmapped
    Given the operator maps "maker" to "https://example.org/legacy#madeBy" in the stored "widget" context
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And the operator maps "maker" to "https://schema.org/maker" in the stored "widget" context
    When the operator counts the resources with IRI-keyed edges
    Then the count classifies the "widget" "Bolt cutter" edge on "https://example.org/legacy#madeBy" as unmapped
    And the count classifies the IRI edge keys for "widget" as:
      | resolvable | ambiguous | unmapped |
      | 0          | 0         | 1        |
    And the count reports 1 resource that normalization would leave IRI-keyed
    And the check fails

  # The two units in one resource: it is counted ONCE, and its keys are
  # classified separately. After the migration the resolvable key is gone and the
  # unmapped one is not — which is why the residue number, not zero, is what the
  # operator reconciles a post-migration run against.
  Scenario: A resource holding one resolvable and one unmapped edge is counted once and classified per edge
    Given the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor"
    And the twin restarts against the same database
    And the operator maps "maker" to "https://example.org/legacy#madeBy" in the stored "widget" context
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with these references:
      | property | vendor |
      | maker    | Acme   |
      | supplier | Acme   |
    And the operator maps "maker" to "https://schema.org/maker" in the stored "widget" context
    When the operator counts the resources with IRI-keyed edges
    Then the count reports 1 "widget" resource with an IRI-keyed edge in its events
    And the count classifies the IRI edge keys for "widget" as:
      | resolvable | ambiguous | unmapped |
      | 1          | 0         | 1        |
    And the count reports 1 resource that normalization would leave IRI-keyed
    When the operator normalizes the stored edge keys and writes
    And the operator reprojects the event feed
    And the operator counts the resources with IRI-keyed edges
    Then the count reports 1 "widget" resource with an IRI-keyed edge in its events
    And the count classifies the IRI edge keys for "widget" as:
      | resolvable | ambiguous | unmapped |
      | 0          | 0         | 1        |

  # --- read-only ---

  # An operator has to be able to run this on a live instance without changing
  # it. "The same events in the same order" also catches the failure that would
  # not show up as a rewritten payload: booting the full module, whose startup
  # reconcile APPENDS ResourceType events — see CONTRACT 5.
  Scenario: The check writes nothing, and a second run reports the same numbers
    Given a "vendor" named "Globex" exists
    And a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And a "widget" named "Hex key" written by the pre-#515 binary with "suppliers" referring to the vendors "Acme, Globex"
    When the operator counts the resources with IRI-keyed edges
    Then the count reports 2 resources with an IRI-keyed edge in their events across the instance
    When the operator counts the resources with IRI-keyed edges
    Then the count reports 2 resources with an IRI-keyed edge in their events across the instance
    And the count reports 2 resources with an IRI-keyed edge in their canonical records across the instance
    And the stored events are byte-identical to the ones stored before the run
    And the event feed holds the same events in the same order as before the run
    And the stored canonical records are byte-identical to the ones stored before the run

  # --- the permanent check ---

  Scenario: A fresh write after normalization keeps the count at zero
    Given a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And the operator normalizes the stored edge keys and writes
    And the operator reprojects the event feed
    When I create a "widget" named "Hex key" with "maker" referring to the "vendor" "Acme"
    And the operator counts the resources with IRI-keyed edges
    Then the count reports no resource with an IRI-keyed edge in its events
    And the count reports no resource with an IRI-keyed edge in its canonical record
    And the check passes

  # What makes it a PERMANENT check rather than a one-off report: it fails again
  # the moment the old shape comes back — a restored backup, an import from an
  # instance that never migrated, a write path that regressed. Nothing else on
  # the instance would say so, because both key forms keep reading.
  Scenario: The check fails again the moment an IRI-keyed record reappears
    Given I create a "widget" named "Bolt cutter" with "maker" referring to the "vendor" "Acme"
    And the operator counts the resources with IRI-keyed edges
    And the check passes
    When a "widget" named "Hex key" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And the operator counts the resources with IRI-keyed edges
    Then the count reports 1 "widget" resource with an IRI-keyed edge in its events
    And the count reports 1 "widget" resource with an IRI-keyed edge in its canonical record
    And the check fails

  # The check measures the SHAPE of the key, not whether it agrees with the
  # current context. After normalization a rename is a preset edit plus a
  # reprojection (#523), and the number must stay at zero through one — otherwise
  # every future rename would light up the permanent check for no reason.
  Scenario: A rename after normalization leaves the count at zero
    Given a "widget" named "Bolt cutter" written by the pre-#515 binary with "maker" referring to the "vendor" "Acme"
    And the operator normalizes the stored edge keys and writes
    And the operator reprojects the event feed
    When the operator maps "maker" to "https://example.org/catalog#madeBy" in the stored "widget" context
    And the operator reprojects the event feed
    And the operator counts the resources with IRI-keyed edges
    Then the count reports no resource with an IRI-keyed edge in its events
    And the count reports no resource with an IRI-keyed edge in its canonical record
    And the check passes
