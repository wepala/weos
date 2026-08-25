@wip @issue-521
Feature: Person and Organization declare an RDF class
  As an operator whose graph is queried by class, and by an LLM through the kg_* tools
  I want the built-in Person and Organization types to declare their RDF class like every other type
  So that what a person resource IS in the graph is written down rather than inferred from a display name

  # WHY THIS EXISTS. `core`'s `person` and `organization` are the only two
  # built-in types whose `@context` declares no `"@type"`. Verified against the
  # real registry, not the story text: a sweep of every type in
  # `presets.NewDefaultRegistry()` reports exactly these two and nothing else.
  # Every other schema.org-backed type writes a bare term — `"@type":"Product"`,
  # `"@type":"Recipe"` — and the house-vocabulary types write a compact one whose
  # prefix the same context defines (`mem:Fact`, `ag:AgentSkill`, `fo:Food`).
  #
  # ---------------------------------------------------------------------------
  # CONTRACT — what these scenarios pin, and the parts an implementer would
  # otherwise have to infer. Every claim below was checked against the code on
  # this branch, and the load-bearing ones were run.
  #
  # 1. THE CLASS THESE RESOURCES ALREADY CARRY IS `schema:Person`. This is the
  #    answer to "which class", and it changes what the story is.
  #    `BuildResourceGraph` (application/triple_extraction.go:737) stamps the
  #    entity node's `@type` as the context's `"@type"` IF IT SETS ONE, else the
  #    resource TYPE'S NAME — which for these two is the display name "Person" /
  #    "Organization". That bare term expands vocab-relative through the
  #    context's `"@vocab":"https://schema.org/"`, so a person written today
  #    already carries `https://schema.org/Person`, and the ontology projection
  #    already advertises the same IRI (`resourceTypeClassIRI`,
  #    application/oxigraph_handler.go:118, computes it the same way).
  #
  #    So the RECOMMENDED declaration is the bare term, and it is a NO-OP for the
  #    class: `"@type":"Person"` on person, `"@type":"Organization"` on
  #    organization. Run on this branch, the document
  #    `BuildResourceGraph` produces with the declaration is BYTE-IDENTICAL to
  #    the one it produces without it. The story is therefore about DECLARING
  #    what is already true — pinning the class against a later rename of the
  #    display name, and closing the audit — not about moving anything.
  #
  #    THE REASONING THE CODE COMMENT MUST CARRY, all four parts:
  #      a. The context's `@vocab` is `https://schema.org/` and every property
  #         term on these types (`givenName`, `familyName`, `email`, `name`,
  #         `url`, `description`) resolves there. The class belongs in the same
  #         vocabulary as the predicates asserted about it.
  #      b. It is the class these resources already carry implicitly, so
  #         declaring it moves no stored document and breaks no consumer whose
  #         query already matches `https://schema.org/Person`.
  #      c. `foaf:` and `org:` are declared on these two contexts and used by
  #         nothing. Naming the class from them would put the class in one
  #         namespace and every predicate on it in another, and would move the
  #         class of every person and organization already stored. The prefixes
  #         stay declared for terms that genuinely need them.
  #      d. The BARE term, not `"schema:Person"`. `jsonld.ExpandIRI`
  #         (pkg/jsonld/context.go:20) has a WeOS-internal fallback that maps an
  #         undeclared `schema:` prefix onto `@vocab`; a conformant JSON-LD
  #         processor has no such rule and would read `schema:Person` as an
  #         absolute IRI in a `schema:` scheme. The graph store parses the
  #         document, WeOS does not — so the compact form would be wrong in the
  #         one place that matters while every WeOS-side assertion passed. The
  #         scenario "The declared class resolves through the type's own
  #         context" is the guard for exactly that.
  #
  #    The class is an Examples column throughout so the choice can flip to
  #    `foaf:Person` / `org:Organization` by editing tables. CONTRACT 6 names the
  #    two scenarios that must be amended, not just re-tabled, if it does.
  #
  # 2. ON AN EXISTING INSTALL THE DECLARATION IS HELD, WITH OR WITHOUT DATA.
  #    Run against `reconcileAdditiveContext` on this branch: a preset `"@type"`
  #    the stored context lacks starts as an Added term and is then taken back by
  #    `holdMovingTerms`, because `livePredicates` unconditionally adds `"@type"`
  #    and `resolveIn` reads the stored context's `"@type"` — absent — as `""`,
  #    which differs from the preset's expansion. The result for every spelling
  #    tried (`Person`, `schema:Person`, `foaf:Person`, the full IRI) is
  #    identical:
  #      Added=[]  Conflicts=[]  Changed=false
  #      Moves=[{Term:@type Property:@type StoredIRI:"" PresetIRI:<the class>}]
  #    Three consequences the scenarios pin:
  #      - The hold does NOT depend on any person or organization existing. The
  #        check keys off the stored context and schema, never a row count, so a
  #        brand-new empty install that merely PREDATES this build still holds.
  #      - `Changed=false` means the boot writes nothing for these types. The
  #        only signal is the warn line "context terms held at their stored
  #        definition: adopting them would repoint a predicate that already has
  #        data" carrying `heldContextTerms`, which is the same bucket the #513
  #        and #520 steps already read.
  #      - Nothing splits while the hold lasts. The stored context still has no
  #        `"@type"`, so writes made after the upgrade take the type name exactly
  #        as writes made before it did. Both land on `https://schema.org/Person`
  #        under EITHER candidate class, which is why the "nothing splits"
  #        scenario hardcodes that IRI rather than parameterising it.
  #
  # 3. THE OPERATOR'S WAY OUT IS BLOCKED BY THE INSTRUCTION THEY ARE GIVEN.
  #    This is the defect the story has to fix on the existing-install side, and
  #    it is invisible unless a scenario names it.
  #    `selectTermsToAdopt` (application/resource_type_service.go:1000)
  #    deliberately excludes `@type` from a SWEEP: "a sweep never takes it, and
  #    an operator who wants it must say so". But both places that tell the
  #    operator what to run print the sweep —
  #      - `weos resource-type held-terms core person` ends with
  #        "Adopt with: weos resource-type adopt-term core person --all"
  #        (internal/cli/resource_type_adopt.go:62), and
  #      - the boot's own warn line carries
  #        `remedy: weos resource-type adopt-term core person --all`
  #        (application/builtin_resource_types.go:154).
  #    Following either one calls `AdoptContextTerms` with an empty term list,
  #    which selects nothing, returns nothing, and prints
  #    "Nothing to adopt for "person" — already up to date." — while the boot
  #    goes on reporting the hold on every start, forever. The working command is
  #    `weos resource-type adopt-term core person --term @type`. The scenarios
  #    under "ADOPTING THE CLASS" pin both halves: that a sweep leaves it held,
  #    and that the instruction the operator is handed is one that works.
  #
  # 4. WHAT ADOPTION DOES, AND DOES NOT, RECORD. `adoptTerms` records a
  #    historical IRI only when `move.StoredIRI != ""`. A held `@type` has an
  #    empty StoredIRI, so no alias is written — correctly: a class is not a
  #    predicate and no edge is keyed by it. An implementation that recorded `""`
  #    as an alias would widen the reverse map with an empty IRI, so the absence
  #    is asserted rather than assumed. After adoption the boot settles: run on
  #    this branch, the same reconcile against the adopted context returns
  #    Moves=[] and Changed=false.
  #
  # 5. THE MIGRATION LINE IN THE STORY IS NOT TRUE AS WRITTEN, AND #520 SETTLED
  #    WHY. "Existing resources gain the class on reprojection" cannot be right:
  #    `worker reproject` REPLAYS each Resource.Created/Updated payload verbatim,
  #    and the class is derived from the document's OWN embedded `@context` and
  #    entity `@type`, stamped at write time. The mechanism that moves a stored
  #    class is `weos worker normalize-edge-keys --restamp`, added by #520. The
  #    full procedure, if the class ever needs to move, is
  #      weos resource-type adopt-term core person --term @type
  #      weos resource-type adopt-term core organization --term @type
  #      weos worker normalize-edge-keys --restamp --type person --type organization --write
  #      weos worker reproject
  #      weos worker checkpoint reset oxigraph --truncate
  #    and it must run in that order: `--restamp` works from the type's STORED
  #    context, so a re-stamp before adoption has nothing to move.
  #
  #    Under the recommended class the whole migration is a NO-OP, and that is
  #    the finding, not a gap. `restampDocument`
  #    (application/edge_key_normalization.go:558) compares the entity `@type` as
  #    the CLASS it expands to, not as text, and compares the embedded context
  #    against `buildStorableContext` of the current one — which strips `"@type"`
  #    (`isTermDefinition`: a top-level `@type` is a keyword redefinition the
  #    parser refuses, so it never reaches a document). Both sides are already
  #    equal, so the re-stamp reports nothing to re-stamp and rewrites no event.
  #
  # 6. THE TWO SCENARIOS THAT MUST BE AMENDED IF THE CLASS FLIPS TO foaf:/org:.
  #    Everything else survives a table edit.
  #      - "The declared class is the one existing resources already carry"
  #        asserts the re-stamp finds nothing. Under `foaf:Person` it finds every
  #        person event and the scenario inverts.
  #      - "Nothing splits while the class is held" asserts a pre- and
  #        post-upgrade resource share `https://schema.org/Person`. That stays
  #        true while HELD under either class, but its sibling assertion — that
  #        the same still holds after adoption without a migration — is
  #        choice-A only.
  #    "After the migration every resource carries the declared class" is written
  #    as an end-state and holds under either choice, which is why it exists
  #    separately from the no-op pin.
  #
  # 7. FIXTURES. `person` requires `givenName` and `familyName`; `organization`
  #    requires `name` and `slug`. The `person` behavior COMPUTES `name` from
  #    givenName + familyName on every create and update
  #    (application/presets/core/preset.go:93), so the generic fixture filler —
  #    which sets `name` from the scenario's quoted name and then fills the
  #    required strings with "fixture givenName" / "fixture familyName" — stores
  #    a person whose `name` is "fixture givenName fixture familyName". Steps
  #    that only need a person to exist can live with that (the world keys a
  #    resource by the name the SCENARIO used, not the stored one). Steps that
  #    assert on `name` cannot, so this contract adds ONE new fixture step:
  #      I create a "<slug>" named "<name>" with these properties:
  #        | givenName  | Ada      |
  #        | familyName | Lovelace |
  #    a literal-valued sibling of #520's existing `with these references:`
  #    table. Nothing else about the fixtures changes.
  #
  # 8. STEPS REUSED, AND THE THREE THAT ARE NEW. Reused verbatim from
  #    `house_vocabulary_domain.feature` / `context_term_adoption.feature`:
  #    `a clean WeOS database`, `a "<slug>" resource is created`,
  #    `that resource carries the RDF type "<class>"`,
  #    `the "<slug>" "<name>" carries the RDF type "<class>" in the stored
  #    document`, `every "<slug>" resource carries the same RDF type in the
  #    stored document`, `the stored "<slug>" context maps "<term>" to "<value>"`
  #    (a raw-value comparison, so it pins the SPELLING as well as the class),
  #    `the boot reconcile reports/does not report the "<term>" context term as
  #    held for "<slug>"`, `the boot reconcile records no failure for "<slug>"`,
  #    `the operator adopts the held "<term>" context term for "<preset>"
  #    "<slug>"`, `the operator adopts every held context term for "<preset>"
  #    "<slug>"`, `the operator lists the held terms for "<preset>" "<slug>"`,
  #    `the operator re-stamps the stored documents as a dry run` / `and writes`,
  #    `the re-stamp reports nothing to re-stamp for "<slug>"`,
  #    `the operator reprojects the event feed`, the projection- and API-read
  #    steps, and `the "<slug>" projection table has a "<column>" column`.
  #    New here:
  #      - `a WeOS database provisioned by the build before Person and
  #        Organization declared a class` and `the twin restarts on the build
  #        that declares the class`. The old-build shim mirrors #520's: take
  #        `presets.NewDefaultRegistry()` and DELETE the `"@type"` key from the
  #        `core` person and organization contexts, decoding and re-encoding the
  #        real `PresetResourceType.Context` rather than keeping a second copy,
  #        so it cannot drift from the preset.
  #      - `the "<slug>" type advertises the RDF class "<class>"`. `tests/e2e` is
  #        an external package and `resourceTypeClassIRI` is unexported, so the
  #        binding must re-derive the IRI the way that function does. To stop the
  #        two from drifting, this story ALSO owes a unit guard in package
  #        `application` pinning
  #        `resourceTypeClassIRI("Person","person",<core context>)` and its
  #        organization twin. Named here because a Gherkin step cannot enforce it.
  #      - `I create a "<slug>" named "<name>" with these properties:` (CONTRACT 7).
  #
  # 9. WHY THE ASSERTIONS ARE ON THE STORED DOCUMENT. Same reason #520 gives: the
  #    embedded graph store only exists under the `oxigraph_embedded` build tag
  #    and the main gate has none, so "carries the RDF type" means the entity
  #    `@type` expanded through the document's OWN `@context` — the JSON-LD the
  #    store ingests verbatim.
  #
  # ---------------------------------------------------------------------------
  # OPEN QUESTIONS — these need an answer before the story is called done.
  #
  # 1. Does adopting `@type` break the ONTOLOGY projection for these two types?
  #    `projectResourceTypeOntology` loads the type's RAW STORED context into the
  #    graph store as a JSON-LD document (application/oxigraph_handler.go:105).
  #    Adoption writes `weos:adoptedTerms` into that stored context, and
  #    `buildStorableContext` exists precisely because WeOS control keys make a
  #    document "unparseable, and the graph store rejects it outright" — but the
  #    ontology path does no such cleaning. If the store rejects it, the class
  #    triple for person disappears at the moment the operator declares it, which
  #    would defeat the story on the existing-install path. Not asserted here
  #    because the store is behind a build tag; it needs either a check under
  #    `oxigraph_embedded` or a decision that it is #522's. This is a pre-existing
  #    hazard for every adopted type, not one #521 introduces — but #521 is the
  #    first story to tell operators to adopt as routine.
  #
  # 2. Should the audit sweep in "No built-in type is left without a class" be
  #    this story's or #522's? It is asserted here because this story is the one
  #    that makes it true, and because it is the only assertion that would catch
  #    a future preset shipping a type with no class. If #522 claims the
  #    epic-wide guards, move it there rather than deleting it.
  # ---------------------------------------------------------------------------

  # ===========================================================================
  # A FRESH INSTALL. `core` is an AutoInstall preset, so `a clean WeOS database`
  # already has person and organization — no explicit install step.
  # ===========================================================================

  Scenario Outline: A core resource carries the class its type declares
    Given a clean WeOS database
    When a "<slug>" resource is created
    Then that resource carries the RDF type "<class>"
    And the stored "<slug>" context maps "@type" to "<declared>"
    And the "<slug>" type advertises the RDF class "<class>"

    Examples:
      | slug         | declared     | class                             |
      | person       | Person       | https://schema.org/Person         |
      | organization | Organization | https://schema.org/Organization   |

  # CONTRACT 1d. `"schema:Person"` passes every WeOS-side assertion above,
  # because ExpandIRI has a private fallback for an undeclared `schema:` prefix.
  # A JSON-LD parser does not, and the graph store is a JSON-LD parser. This is
  # the only scenario that can tell the two spellings apart.
  Scenario Outline: The declared class resolves through the type's own context
    Given a clean WeOS database
    Then the "@type" the stored "<slug>" context declares resolves to "<class>" through that context alone
    And that resolution does not depend on the built-in "schema" prefix fallback

    Examples:
      | slug         | class                           |
      | person       | https://schema.org/Person       |
      | organization | https://schema.org/Organization |

  # The person behaviour rewrites `name` on every write. Declaring a class must
  # not disturb it, and the entity node is where both live.
  Scenario: Declaring the class leaves the computed name and the properties alone
    Given a clean WeOS database
    When I create a "person" named "Ada Lovelace" with these properties:
      | givenName  | Ada      |
      | familyName | Lovelace |
    Then that resource carries the RDF type "https://schema.org/Person"
    And reading the "person" "Ada Lovelace" back through the projection returns "name" as "Ada Lovelace"
    And reading the "person" "Ada Lovelace" back through the projection returns "givenName" as "Ada"
    And the API read of the "person" "Ada Lovelace" returns "familyName" as "Lovelace"

  # The audit, asserted rather than quoted. Verified against
  # presets.NewDefaultRegistry() on this branch: person and organization are the
  # only two types with no "@type", so this sweep is red before the edit and
  # green after it, and it stays green only while no new preset ships a
  # class-less type.
  Scenario: No built-in resource type is left without a class
    Given a clean WeOS database
    When the operator installs every built-in preset
    Then every installed resource type declares an "@type" in its stored context
    And every installed resource type advertises an RDF class that is an absolute IRI

  # ===========================================================================
  # AN EXISTING INSTALL — the hold. CONTRACT 2.
  # ===========================================================================

  Scenario Outline: The upgrade holds the class rather than declaring it under the operator
    Given a WeOS database provisioned by the build before Person and Organization declared a class
    And a "<slug>" resource is created
    When the twin restarts on the build that declares the class
    Then the boot reconcile reports the "@type" context term as held for "<slug>"
    And the stored "<slug>" context declares no "@type"

    Examples:
      | slug         |
      | person       |
      | organization |

  # The surprising half, and the one an implementer would get wrong by assuming
  # the guard is about protecting data: the hold fires off the stored CONTEXT,
  # never a row count. An install that has never created a person still holds.
  Scenario Outline: An install with no resources of the type holds the class just the same
    Given a WeOS database provisioned by the build before Person and Organization declared a class
    When the twin restarts on the build that declares the class
    Then the boot reconcile reports the "@type" context term as held for "<slug>"

    Examples:
      | slug         |
      | person       |
      | organization |

  # Nothing splits while the hold lasts: the stored context has no "@type", so a
  # write made after the upgrade takes the type name exactly as one made before
  # it did. The IRI is hardcoded because it is the PRE-declaration class, which
  # is the same under either candidate.
  Scenario: Nothing splits while the class is held
    Given a WeOS database provisioned by the build before Person and Organization declared a class
    And I create a "person" named "Ada Lovelace" with these properties:
      | givenName  | Ada      |
      | familyName | Lovelace |
    When the twin restarts on the build that declares the class
    And I create a "person" named "Grace Hopper" with these properties:
      | givenName  | Grace  |
      | familyName | Hopper |
    Then the "person" "Ada Lovelace" carries the RDF type "https://schema.org/Person" in the stored document
    And the "person" "Grace Hopper" carries the RDF type "https://schema.org/Person" in the stored document
    And every "person" resource carries the same RDF type in the stored document

  # Per-entry refusal, on the real preset. A held class must not take an
  # additive property beside it down — that is the drop #510 closed.
  Scenario: A held class does not block an additive change on the same type
    Given a WeOS database provisioned by the build before Person and Organization declared a class
    And the build that declares the class also adds a "jobTitle" string property to "person"
    When the twin restarts on the build that declares the class
    Then the boot reconcile reports the "@type" context term as held for "person"
    And the "person" projection table has a "jobTitle" column
    And I create a "person" named "Ada Lovelace" with these properties:
      | givenName  | Ada      |
      | familyName | Lovelace |
      | jobTitle   | Analyst  |
    And reading the "person" "Ada Lovelace" back through the projection returns "jobTitle" as "Analyst"

  # The hold never resolves itself, so an operator who restarts before reading
  # the log must still find it.
  Scenario: The hold is reported on every boot, not only the first
    Given a WeOS database provisioned by the build before Person and Organization declared a class
    And the twin restarts on the build that declares the class
    And the boot reconcile reports the "@type" context term as held for "person"
    When the twin restarts on the build that declares the class again
    Then the boot reconcile reports the "@type" context term as held for "person"

  # ===========================================================================
  # ADOPTING THE CLASS. CONTRACT 3 — the sweep will not take it, and today both
  # instructions the operator is handed are the sweep.
  # ===========================================================================

  Scenario: The held class is reported so the operator can see the decision
    Given a WeOS database provisioned by the build before Person and Organization declared a class
    And the twin restarts on the build that declares the class
    When the operator lists the held terms for "core" "person"
    Then "@type" is reported as held and offered at "https://schema.org/Person"
    And the held "@type" names no IRI that existing data is keyed by

  # The gap, asserted so it cannot be closed by accident or left open in
  # silence. A sweep is documented to skip @type; what must NOT stand is a
  # sweep that skips it while reporting "already up to date".
  Scenario: Adopting every held term leaves the class held, and says so
    Given a WeOS database provisioned by the build before Person and Organization declared a class
    And a "person" resource is created
    And the twin restarts on the build that declares the class
    When the operator adopts every held context term for "core" "person"
    Then the stored "person" context declares no "@type"
    And the operator is told the class was not adopted and how to adopt it
    And the boot reconcile reports the "@type" context term as held for "person" on the next restart

  # The story's real work on this path: the command an operator is HANDED has to
  # be one that finishes the job. Both places that print a remedy are asserted,
  # because fixing one and not the other leaves the operator stuck in the other.
  Scenario: The instruction the operator is given is one that adopts the class
    Given a WeOS database provisioned by the build before Person and Organization declared a class
    And the twin restarts on the build that declares the class
    Then the boot's held report for "person" names a command that adopts "@type"
    When the operator lists the held terms for "core" "person"
    Then the command it prints adopts "@type" for "person"
    And running that command declares "Person" as the "@type" of the stored "person" context

  Scenario Outline: Adopting the class by name declares it and the boot settles
    Given a WeOS database provisioned by the build before Person and Organization declared a class
    And a "<slug>" resource is created
    And the twin restarts on the build that declares the class
    When the operator adopts the held "@type" context term for "core" "<slug>"
    Then the stored "<slug>" context maps "@type" to "<declared>"
    And the "<slug>" type advertises the RDF class "<class>"
    When the twin restarts on the build that declares the class again
    Then the boot reconcile does not report the "@type" context term as held for "<slug>"
    And the boot reconcile records no failure for "<slug>"

    Examples:
      | slug         | declared     | class                           |
      | person       | Person       | https://schema.org/Person       |
      | organization | Organization | https://schema.org/Organization |

  # CONTRACT 4. A class is not a predicate and no edge is keyed by it, so there
  # is nothing to alias — and an empty string recorded as one would widen the
  # reverse map with an IRI nothing was ever written under.
  Scenario: Adopting the class records no historical IRI
    Given a WeOS database provisioned by the build before Person and Organization declared a class
    And a "person" resource is created
    And the twin restarts on the build that declares the class
    When the operator adopts the held "@type" context term for "core" "person"
    Then the stored "person" context records no historical IRI for "@type"
    And the stored "person" context records no empty historical IRI for any property

  # ===========================================================================
  # THE MIGRATION. CONTRACT 5 — a reprojection alone never moves a stored class;
  # `--restamp` does, and it reads the type's STORED context, so it must run
  # after adoption.
  # ===========================================================================

  # THE FINDING, and the scenario to invert if the class flips (CONTRACT 6): the
  # class named by the declaration is the class every existing resource already
  # carries, so the migration the story asks for has no work to do.
  Scenario: The declared class is the one existing resources already carry
    Given a WeOS database provisioned by the build before Person and Organization declared a class
    And a "person" named "Ada Lovelace" exists
    And the twin restarts on the build that declares the class
    And the operator adopts the held "@type" context term for "core" "person"
    When the operator re-stamps the stored documents as a dry run
    Then the re-stamp reports nothing to re-stamp for "person"
    And the "person" "Ada Lovelace" carries the RDF type "https://schema.org/Person" in the stored document
    When the operator re-stamps the stored documents and writes
    Then the re-stamp re-stamped 0 events for "person"
    And the stored events are byte-identical to the ones stored before the run

  # The documented ordering, pinned: `--restamp` works from the stored context,
  # so before adoption it has nothing to move. An operator who runs the
  # migration first and the adoption second gets a silent no-op.
  Scenario: A re-stamp before the class is adopted has nothing to move
    Given a WeOS database provisioned by the build before Person and Organization declared a class
    And a "person" named "Ada Lovelace" exists
    And the twin restarts on the build that declares the class
    When the operator re-stamps the stored documents as a dry run
    Then the re-stamp reports nothing to re-stamp for "person"
    And the stored "person" context declares no "@type"

  # Written as an END STATE so it holds whichever class is chosen: after the
  # documented procedure, a resource written before the declaration and one
  # written after it carry the same declared class.
  Scenario Outline: After the migration every resource carries the declared class
    Given a WeOS database provisioned by the build before Person and Organization declared a class
    And a "<slug>" named "Older" exists
    And the twin restarts on the build that declares the class
    And the operator adopts the held "@type" context term for "core" "<slug>"
    When the operator re-stamps the stored documents and writes
    And the operator reprojects the event feed
    And a "<slug>" named "Newer" exists
    Then the "<slug>" "Older" carries the RDF type "<class>" in the stored document
    And the "<slug>" "Newer" carries the RDF type "<class>" in the stored document
    And every "<slug>" resource carries the same RDF type in the stored document

    Examples:
      | slug         | class                           |
      | person       | https://schema.org/Person       |
      | organization | https://schema.org/Organization |

  # The guarantee that makes rewriting stored events acceptable: nothing a
  # reader sees moves. The computed name is the one that would notice — it lives
  # in the entity node beside the `@type` the re-stamp is allowed to touch.
  Scenario: The migration changes nothing an API reader sees
    Given a WeOS database provisioned by the build before Person and Organization declared a class
    And I create a "person" named "Ada Lovelace" with these properties:
      | givenName  | Ada      |
      | familyName | Lovelace |
    And the twin restarts on the build that declares the class
    And the operator adopts the held "@type" context term for "core" "person"
    When the operator re-stamps the stored documents and writes
    And the operator reprojects the event feed
    Then reading the "person" "Ada Lovelace" back through the projection returns "name" as "Ada Lovelace"
    And reading the "person" "Ada Lovelace" back through the projection returns "givenName" as "Ada"
    And the API read of the "person" "Ada Lovelace" returns "familyName" as "Lovelace"
    And the entity node of the stored event for the "person" "Ada Lovelace" is byte-identical to the one stored before the run apart from its "@type"
