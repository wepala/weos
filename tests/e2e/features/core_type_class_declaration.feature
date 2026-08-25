@issue-521
Feature: Person and Organization declare an RDF class
  As an operator whose graph is queried by class, and by an LLM through the kg_* tools
  I want the built-in Person and Organization types to declare their RDF class like every other type
  So that what a person resource IS in the graph is written down rather than inferred from a display name

  # WHY THIS EXISTS. `core`'s `person` and `organization` are the only two
  # built-in types whose `@context` declares no `"@type"`. Verified against the
  # real registry, not the story text: a sweep of every type in
  # `presets.NewDefaultRegistry()` reports exactly these two and nothing else.
  # Every other type declares its class — a bare term against a schema.org
  # `@vocab` (`"@type":"Product"`), or a compact one whose prefix the same
  # context defines (`mem:Fact`, `ag:AgentSkill`, `fo:Food`).
  #
  # ---------------------------------------------------------------------------
  # CONTRACT — what these scenarios pin, and the parts an implementer would
  # otherwise have to infer. Every claim below was checked against the code on
  # this branch, and the load-bearing ones were run.
  #
  # 1. THE CLASS. Akeem settled this on 2026-08-25: `foaf:Person` on person and
  #    `org:Organization` on organization, spelled COMPACT, because both
  #    prefixes are already declared on those two contexts and this is what they
  #    were declared for.
  #      person       "@type":"foaf:Person"  → http://xmlns.com/foaf/0.1/Person
  #      organization "@type":"org:Organization" → http://www.w3.org/ns/org#Organization
  #
  #    THE REASONING THE CODE COMMENT MUST CARRY:
  #      a. FOAF and W3C ORG are the standard, purpose-built classes for these
  #         two concepts. The type descriptions have named them since the preset
  #         was written ("A person (foaf:Person / schema:Person)").
  #      b. The prefixes are declared on these contexts for exactly this. Today
  #         `foaf:` and `org:` are declared and used by nothing; naming the class
  #         from them is what makes the declaration meaningful rather than
  #         vestigial.
  #      c. schema.org stays the `@vocab`, so every PREDICATE on these types
  #         (`givenName`, `familyName`, `email`, `name`, `url`, `description`)
  #         keeps resolving to schema.org exactly as it does today. Only the
  #         class moves. That is a deliberate mixed graph — the standard class
  #         for the subject, schema.org for the properties — not an oversight,
  #         and the comment should say so or someone will "fix" it later.
  #      d. The COMPACT form is correct here, unlike a bare `"schema:Person"`
  #         would be: `foaf` and `org` are real prefix definitions in the same
  #         context, and `buildStorableContext` keeps prefix definitions, so the
  #         prefix travels into every resource's embedded `@context` and the
  #         document expands on its own. (`jsonld.ExpandIRI` also has a private
  #         fallback that maps an UNDECLARED `schema:` prefix onto `@vocab`; a
  #         conformant JSON-LD parser has no such rule, and the graph store is a
  #         conformant parser. The scenario "The declared class resolves through
  #         the document's own context" is the guard, and under this choice it
  #         has real work to do: if the prefix ever stopped travelling into the
  #         embedded context, every person in the graph would lose its class.)
  #
  #    WHAT THIS COSTS, stated because the scenarios below are shaped by it.
  #    `BuildResourceGraph` (application/triple_extraction.go:737) stamps the
  #    entity `@type` as the context's `"@type"` IF IT SETS ONE, else the resource
  #    TYPE'S NAME — "Person" / "Organization" — which expands vocab-relative
  #    through `"@vocab":"https://schema.org/"`. So every person and organization
  #    written before this story carries `https://schema.org/Person` /
  #    `https://schema.org/Organization`, and `resourceTypeClassIRI`
  #    (application/oxigraph_handler.go:118) advertises the same IRIs. This story
  #    MOVES both. On an existing install that is a real migration (CONTRACT 5),
  #    and any stored query, saved SPARQL or LLM habit that matches
  #    `https://schema.org/Person` stops matching once it runs.
  #
  #    The class is an Examples column throughout, so the choice remains a table
  #    edit. CONTRACT 6 names the scenarios that encode the choice in their
  #    SHAPE rather than in a table cell.
  #
  # 2. ON AN EXISTING INSTALL THE DECLARATION IS HELD, WITH OR WITHOUT DATA.
  #    Run against `reconcileAdditiveContext` on this branch: a preset `"@type"`
  #    the stored context lacks starts as an Added term and is then taken back by
  #    `holdMovingTerms`, because `livePredicates` unconditionally adds `"@type"`
  #    and `resolveIn` reads an absent stored `"@type"` as `""`, which differs
  #    from the preset's expansion. Run with the chosen spelling, the result is
  #      Added=[]  Conflicts=[]  Changed=false
  #      Moves=[{Term:@type Property:@type StoredIRI:"" PresetIRI:http://xmlns.com/foaf/0.1/Person}]
  #    Three consequences the scenarios pin:
  #      - The hold does NOT depend on any person or organization existing. The
  #        check keys off the stored context and schema, never a row count, so a
  #        brand-new empty install that merely PREDATES this build still holds.
  #      - `Changed=false` means the boot writes nothing for these types. The
  #        only signal is the warn line "context terms held at their stored
  #        definition: adopting them would repoint a predicate that already has
  #        data" carrying `heldContextTerms`, which is the same bucket the #513
  #        and #520 steps already read.
  #      - Nothing splits WHILE THE HOLD LASTS. The stored context still has no
  #        `"@type"`, so a write made after the upgrade takes the type name
  #        exactly as one made before it did, and both land on
  #        `https://schema.org/Person`. The split appears at ADOPTION, not at
  #        upgrade, and it is what the migration exists to close (CONTRACT 5).
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
  # 5. THE MIGRATION IS REAL, AND THE STORY'S LINE ABOUT IT IS WRONG AS WRITTEN.
  #    "Existing resources gain the class on reprojection" cannot be right:
  #    `worker reproject` REPLAYS each Resource.Created/Updated payload verbatim,
  #    and the class is derived from the document's OWN embedded `@context` and
  #    entity `@type`, stamped at write time. #520 settled the mechanism that
  #    does move a stored class — `weos worker normalize-edge-keys --restamp` —
  #    and this story is its first real user: `restampDocument`
  #    (application/edge_key_normalization.go:558) compares the entity `@type` as
  #    the CLASS it expands to, finds `https://schema.org/Person` where the
  #    type's current context now says `http://xmlns.com/foaf/0.1/Person`, and
  #    rewrites the entity `@type` to `"foaf:Person"`. It rewrites nothing else
  #    here: `buildStorableContext` strips a top-level `"@type"` (a keyword
  #    redefinition the parser refuses), so the document's embedded `@context` is
  #    unchanged — the prefix it needs was already there.
  #
  #    THE PROCEDURE, in this order:
  #      weos resource-type adopt-term core person --term @type
  #      weos resource-type adopt-term core organization --term @type
  #      weos worker normalize-edge-keys --restamp --type person --type organization --write
  #      weos worker reproject
  #      weos worker checkpoint reset oxigraph --truncate
  #    Order matters twice over:
  #      - `--restamp` works from the type's STORED context, so a re-stamp run
  #        BEFORE adoption finds nothing to move and exits reporting success.
  #        The scenario "A re-stamp before the class is adopted has nothing to
  #        move" is that trap, pinned.
  #      - The truncate is NOT optional under this choice.
  #        `projectResourceTypeOntology` clears the class subject it is ABOUT to
  #        write (`RemoveSubject(resourceTypeClassIRI(...))` computed from the
  #        NEW context, oxigraph_handler.go:100), so the OLD subject —
  #        `https://schema.org/Person` with its `rdfs:Class` and `rdfs:label`
  #        triples — is never cleared and lingers in the graph advertising a
  #        class no resource carries any more. Rebuilding the graph is what
  #        removes it.
  #
  # 6. THE SCENARIOS THAT ENCODE THE CHOICE IN THEIR SHAPE. Everything else is a
  #    table edit if the class is ever revisited.
  #      - "A reprojection alone leaves the old class; a re-stamp moves it"
  #        exists BECAUSE the class moves. Against a class equal to what
  #        resources already carry it would invert into a no-op assertion.
  #      - "Adopting the class splits it until the migration runs" likewise: it
  #        asserts a split that only a moving class produces.
  #      - "Nothing splits while the class is held" hardcodes
  #        `https://schema.org/Person` deliberately — that is the
  #        PRE-declaration class, and it stays the answer for as long as the
  #        stored context has no `"@type"`, whatever the preset now declares.
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
  #    `the re-stamp reports N events to re-stamp for "<slug>"`,
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
  # 2. Who tells the consumers? Moving the class means a saved SPARQL query, an
  #    MCP client's habit, or a downstream integration that filters on
  #    `https://schema.org/Person` silently returns nothing after the migration.
  #    Nothing in the codebase can assert that. It is a release-note obligation,
  #    recorded here so the story is not called done without one.
  #
  # 3. Should the audit sweep in "No built-in type is left without a class" be
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
      | slug         | declared          | class                                  |
      | person       | foaf:Person       | http://xmlns.com/foaf/0.1/Person       |
      | organization | org:Organization  | http://www.w3.org/ns/org#Organization  |

  # CONTRACT 1d. The compact class is only as good as the prefix that expands
  # it, and the prefix has to survive `buildStorableContext` into every
  # resource's own embedded @context — the graph store parses the document, not
  # the resource type. If it ever stopped travelling, every person in the graph
  # would lose its class while the type still looked correctly declared.
  Scenario Outline: The declared class resolves through the document's own context
    Given a clean WeOS database
    When a "<slug>" resource is created
    Then the embedded "@context" of that resource defines the "<prefix>" prefix as "<namespace>"
    And the "@type" of that resource resolves to "<class>" through its own embedded context alone
    And that resolution does not depend on the built-in "schema" prefix fallback

    Examples:
      | slug         | prefix | namespace                     | class                                 |
      | person       | foaf   | http://xmlns.com/foaf/0.1/    | http://xmlns.com/foaf/0.1/Person      |
      | organization | org    | http://www.w3.org/ns/org#     | http://www.w3.org/ns/org#Organization |

  # CONTRACT 1c. Only the class moves. Every predicate on these types stays on
  # schema.org, and the person behaviour still rewrites `name` on every write —
  # the entity node is where the class and the properties live side by side.
  Scenario: Declaring the class leaves the properties and the computed name on schema.org
    Given a clean WeOS database
    When I create a "person" named "Ada Lovelace" with these properties:
      | givenName  | Ada      |
      | familyName | Lovelace |
    Then that resource carries the RDF type "http://xmlns.com/foaf/0.1/Person"
    And the stored document states "https://schema.org/givenName" from the "person" "Ada Lovelace" with the value "Ada"
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

  Scenario Outline: The upgrade holds the class rather than moving it under the operator
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

  # CONTRACT 6. The IRI here is hardcoded because it is the PRE-declaration
  # class: while the stored context has no "@type", a write takes the type name
  # and lands on schema.org whatever the preset declares. The upgrade alone
  # therefore splits nothing — the split arrives with adoption, in the scenario
  # after this one.
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
    Then "@type" is reported as held and offered at "http://xmlns.com/foaf/0.1/Person"
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
    And running that command declares "foaf:Person" as the "@type" of the stored "person" context

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
      | slug         | declared          | class                                 |
      | person       | foaf:Person       | http://xmlns.com/foaf/0.1/Person      |
      | organization | org:Organization  | http://www.w3.org/ns/org#Organization |

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
  # THE MIGRATION. CONTRACT 5 — the class MOVES, a reprojection alone never
  # moves a stored one, and `--restamp` reads the type's STORED context, so the
  # steps only work in one order.
  # ===========================================================================

  # Why the migration exists, in one scenario: adoption changes what NEW writes
  # carry and nothing about what is already stored. An operator who adopts and
  # stops here has two classes for one type and no error anywhere.
  Scenario: Adopting the class splits it until the migration runs
    Given a WeOS database provisioned by the build before Person and Organization declared a class
    And a "person" named "Ada Lovelace" exists
    And the twin restarts on the build that declares the class
    When the operator adopts the held "@type" context term for "core" "person"
    And a "person" named "Grace Hopper" exists
    Then the "person" "Grace Hopper" carries the RDF type "http://xmlns.com/foaf/0.1/Person" in the stored document
    And the "person" "Ada Lovelace" carries the RDF type "https://schema.org/Person" in the stored document
    And the "person" type advertises the RDF class "http://xmlns.com/foaf/0.1/Person"

  # The sequence the runbook line has to state, both halves in one scenario so
  # neither can be read without the other. This is #520's mechanism doing the
  # work #520 predicted it would be needed for.
  Scenario: A reprojection alone leaves the old class; a re-stamp moves it
    Given a WeOS database provisioned by the build before Person and Organization declared a class
    And a "person" named "Ada Lovelace" exists
    And a "person" named "Alan Turing" exists
    And the twin restarts on the build that declares the class
    And the operator adopts the held "@type" context term for "core" "person"
    When the operator reprojects the event feed
    Then the "person" "Ada Lovelace" carries the RDF type "https://schema.org/Person" in the stored document
    When the operator re-stamps the stored documents as a dry run
    Then the re-stamp reports 2 events to re-stamp for "person"
    And the stored events are byte-identical to the ones stored before the run
    When the operator re-stamps the stored documents and writes
    And the operator reprojects the event feed
    Then the "person" "Ada Lovelace" carries the RDF type "http://xmlns.com/foaf/0.1/Person" in the stored document
    And the "person" "Alan Turing" carries the RDF type "http://xmlns.com/foaf/0.1/Person" in the stored document
    And every "person" resource carries the same RDF type in the stored document

  # The ordering trap. `--restamp` works from the STORED context, so run before
  # the adoption it finds nothing to move and exits reporting success — leaving
  # an operator who ran the migration first believing it is done.
  Scenario: A re-stamp before the class is adopted has nothing to move
    Given a WeOS database provisioned by the build before Person and Organization declared a class
    And a "person" named "Ada Lovelace" exists
    And the twin restarts on the build that declares the class
    When the operator re-stamps the stored documents as a dry run
    Then the re-stamp reports nothing to re-stamp for "person"
    And the stored "person" context declares no "@type"
    And the "person" "Ada Lovelace" carries the RDF type "https://schema.org/Person" in the stored document

  # The end state of the documented procedure, per type: a resource written
  # before the declaration and one written after it carry the same class.
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
    And no "<slug>" resource carries an RDF type under "https://schema.org/" in the stored document

    Examples:
      | slug         | class                                 |
      | person       | http://xmlns.com/foaf/0.1/Person      |
      | organization | http://www.w3.org/ns/org#Organization |

  # A second run of the migration is a no-op. This is what makes the procedure
  # safe to leave in a deployment script, and it is the assertion that catches a
  # re-stamp comparing the @type as TEXT rather than as the class it expands to
  # — `"foaf:Person"` and `http://xmlns.com/foaf/0.1/Person` are the same class
  # spelled two ways, and a textual comparison would rewrite every event forever.
  Scenario: A second migration re-stamps nothing
    Given a WeOS database provisioned by the build before Person and Organization declared a class
    And a "person" named "Ada Lovelace" exists
    And the twin restarts on the build that declares the class
    And the operator adopts the held "@type" context term for "core" "person"
    And the operator re-stamps the stored documents and writes
    When the operator re-stamps the stored documents as a dry run
    Then the re-stamp reports nothing to re-stamp
    When the operator re-stamps the stored documents and writes
    Then the re-stamp re-stamped 0 events
    And the stored events are byte-identical to the ones stored before the second run

  # A fresh install has nothing to migrate, and the migration must be able to
  # say so rather than rewriting every document to the same class it already
  # carries.
  Scenario: A migration on an install that always declared the class re-stamps nothing
    Given a clean WeOS database
    And a "person" named "Ada Lovelace" exists
    And an "organization" named "Wepala" exists
    When the operator re-stamps the stored documents as a dry run
    Then the re-stamp reports nothing to re-stamp
    And the "person" "Ada Lovelace" carries the RDF type "http://xmlns.com/foaf/0.1/Person" in the stored document

  # The guarantee that makes rewriting stored events acceptable: only the class
  # moves. The computed name is the one that would notice — it lives in the
  # entity node beside the `@type` the re-stamp is allowed to touch — and every
  # predicate stays on schema.org (CONTRACT 1c).
  Scenario: The migration moves the class and changes nothing else
    Given a WeOS database provisioned by the build before Person and Organization declared a class
    And I create a "person" named "Ada Lovelace" with these properties:
      | givenName  | Ada      |
      | familyName | Lovelace |
    And the twin restarts on the build that declares the class
    And the operator adopts the held "@type" context term for "core" "person"
    When the operator re-stamps the stored documents and writes
    Then the entity node of the stored event for the "person" "Ada Lovelace" is byte-identical to the one stored before the run apart from its "@type"
    When the operator reprojects the event feed
    Then reading the "person" "Ada Lovelace" back through the projection returns "name" as "Ada Lovelace"
    And reading the "person" "Ada Lovelace" back through the projection returns "givenName" as "Ada"
    And the API read of the "person" "Ada Lovelace" returns "familyName" as "Lovelace"
    And the stored document states "https://schema.org/givenName" from the "person" "Ada Lovelace" with the value "Ada"
