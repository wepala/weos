@issue-522
Feature: A control keyword never claims a predicate a property owns
  As an operator whose type declares its parent, its abstractness and its aliases in the same @context
  I want WeOS to read those entries as control data and never as term definitions
  So that no property loses its edges to a keyword that happens to expand onto the same IRI

  # WHY THIS EXISTS. `jsonld.ParseContext` skips every key beginning with "@"
  # and nothing else. `jsonld.ControlKeywords` — the five entries WeOS reads as
  # control data — are not `@`-keywords, so a control key whose value is a
  # STRING is expanded exactly as if it were a term definition:
  #
  #     {"@vocab":"https://schema.org/", "rdfs:subClassOf":"gadget"}
  #       ParseContext      → {"rdfs:subClassOf": "https://schema.org/gadget"}
  #       BuildReverseMap   → {"https://schema.org/gadget": "rdfs:subClassOf"}
  #
  # `rdfs:subClassOf` is the only one of the five that reaches the map today:
  # `weos:abstract` and `weos:valueObject` are booleans, `weos:adoptedTerms` is
  # an array, and `weos:termAliases` is an object with no `@id`, so all four
  # fall off ParseContext's type switch already. Its value is a TYPE SLUG (or,
  # written by someone who knows RDF, `schema:Thing` or an absolute IRI) —
  # never a predicate. The epic's audit counts 52 contexts in the field
  # declaring it; on THIS branch no built-in preset context does (checked with
  # grep over application/presets), so the contexts that carry it are the ones
  # operators and private presets create, which is precisely the population no
  # unit test over the registry will ever see.
  #
  # `application/triple_extraction.go isTermDefinition` already filters
  # ControlKeywords BY NAME for a resource document. ParseContext does not.
  # That inconsistency is the whole story.
  #
  # WHAT IT COSTS TODAY. Nothing in the built-in registry, which is why this is
  # a LATENT fault: no built-in property resolves to the same IRI as a control
  # entry, and no built-in context declares one. The day one does — a type named
  # after its parent, a preset that repoints a term, an operator editing a
  # stored context, a private preset with a class hierarchy — the reverse map
  # is a map[string]string and the two entries collide. The winner is Go map
  # iteration order, so the SAME database answers differently on different
  # boots, and the loser is the property: its legacy edges stop resolving, and
  # the boot reports a healthy reference as one whose writes are dropped.
  #
  # ---------------------------------------------------------------------------
  # CONTRACT — read this before implementing.
  #
  # 1. WHAT IS PINNED BY UNIT TESTS, NOT BY THIS FILE. Most of this story is
  #    pkg/jsonld behaviour with no observable surface of its own, and a
  #    map-order defect cannot be proven by a single end-to-end run. The
  #    implementer MUST write these, and they are where the acceptance criteria
  #    are actually nailed down:
  #
  #    pkg/jsonld/context_test.go
  #      - TestParseContext_SkipsEveryControlKeyword
  #          A context declaring all five ControlKeywords beside ordinary terms
  #          returns a map holding the ordinary terms and NO key in
  #          ControlKeywords — and no value equal to what a control entry would
  #          have expanded to. Table-driven over the value shapes each keyword
  #          is legitimately written in: a bare slug, a compact IRI, an absolute
  #          IRI, and (for subClassOf) an object carrying "@id".  [AC 1, AC 2]
  #      - TestParseContext_SkipsByNameNotByColon
  #          A key that merely CONTAINS a colon and is not a control keyword —
  #          e.g. "skos:related" mapped to an IRI — keeps its mapping. The fix
  #          is exact membership in ControlKeywords; a "skip any key with a
  #          colon" shortcut would silently drop real terms and must fail here.
  #          A PREFIX definition ("rdfs": "http://www.w3.org/2000/01/rdf-schema#")
  #          must survive too — only the compound key is control data.  [AC 1]
  #      - TestBuildReverseMap_ControlKeywordNeverClaimsAPredicate
  #          A context where rdfs:subClassOf expands onto the same IRI as
  #          property "maker" maps that IRI to "maker". Assert it over many
  #          rebuilds (>= 100) in one test, because before the fix it passes
  #          about half the time. No entry of the returned map may name a
  #          control keyword.  [AC 2, AC 3 — this is the deterministic-collision
  #          test the story asks for]
  #      - TestControlKeywordReadersDoNotUseTheTermMap
  #          On the same context, SubClassOf, IsAbstract, IsValueObject,
  #          TermAliases and AdoptedTerms all still return their declared
  #          values, and the existing TestSubClassOf / TestIsValueObject /
  #          TestIsAbstract / TestBuildReverseMap_TermAliases /
  #          TestBuildReverseMap_AliasNeverShadowsALiveTerm keep passing
  #          UNCHANGED. They read the raw context; nothing about them may need
  #          editing to make this story green. An edit to any of them is the
  #          signal that the fix went too far.  [AC 4]
  #
  #    pkg/jsonld/edges_test.go (new file)
  #      - TestEdgeProperty_ControlKeywordNeverClaimsALegacyKey
  #          EdgeProperty(<the shared IRI>, ctx) returns the property name, over
  #          many runs. This is the read path scenario 1 exercises end to end.
  #
  #    application/preset_context_reconcile_test.go
  #      - TestReferencePropertiesWithoutContextEntry_IgnoresAControlKeyword
  #          referencePropertiesWithoutContextEntry returns nothing for a type
  #          whose reference property shares its IRI with rdfs:subClassOf. This
  #          is the false "writes are dropped" report scenario 2 observes.
  #
  # 2. THE EPIC-WIDE GUARDS BELONG IN THE REGISTRY SWEEP, NOT HERE. The epic's
  #    DoD asks for two registry-wide assertions. Both are pure functions of
  #    presets.NewDefaultRegistry() — no database, no boot — so they belong
  #    beside the existing TestPresets_* sweeps in
  #    application/resource_type_presets_test.go, where they run in
  #    `make test-unit` on every change rather than only in the e2e job:
  #      - TestPresets_ReferencePropertiesReverseMapToTheirOwnName
  #          For every type of every built-in preset: BuildReverseMap of its
  #          declared context maps each reference property's predicate IRI back
  #          to that property's own name. (tests/e2e/features/
  #          house_vocabulary_domain.feature already asserts this for INSTALLED
  #          types; this one covers the DECLARED presets, which is where a bad
  #          edit lands first, and it fails without a database.)
  #      - TestPresets_NoPredicateIRIKeepsACompactPrefix
  #          For every type: no predicate IRI any property resolves to, and no
  #          IRI any context term expands to, contains a ":" after the scheme
  #          separator — the shape a compact IRI leaves behind when its prefix
  #          was never declared (e.g. https://schema.org/foaf:knows). Name the
  #          type, the term and the IRI in the failure.
  #      - TestPresets_NoControlKeywordClaimsAPredicateIRI
  #          For every type: ParseContext's map holds no key in
  #          jsonld.ControlKeywords. This is AC 1 and AC 2 asserted across the
  #          whole registry, and it is the guard that keeps a future preset from
  #          reintroducing the fault.
  #
  # 3. THE SYNTHETIC WORLD. These scenarios run in the "catalog" step world
  #    (tests/e2e/preset_context_reconcile_test.go, initPresetContextScenario),
  #    which every context feature shares. Add a
  #    registerControlKeywordSteps(sc) group beside the existing ones and a
  #    TestControlKeywordTerms suite via runContextFeature.
  #
  #    FOUR new steps are needed; everything else already exists.
  #      a. `the stored "widget" context declares these control entries:`
  #         A | entry | value | table whose VALUE cell is parsed as JSON and
  #         written verbatim into the stored context, so a bool, an array and an
  #         object are all expressible. Delegates to the same writer as the
  #         existing `the operator maps … in the stored "widget" context`.
  #      b. `no predicate of "widget" is claimed by a control keyword`
  #         ParseContext of the stored context holds no key in
  #         jsonld.ControlKeywords, and no value of BuildReverseMap names one.
  #         This assertion is DETERMINISTIC — it fails on every run before the
  #         fix — which is what makes these scenarios guards rather than coin
  #         flips. Every scenario that builds a collision asserts it.
  #      c. `the twin still reads the control entries of "widget" as:`
  #         A | reader | value | table over the five readers: "subclass of"
  #         (SubClassOf), "abstract" (IsAbstract), "value object"
  #         (IsValueObject), "adopted terms" (AdoptedTerms, comma separated),
  #         "alias of <property>" (TermAliases for that property).
  #      d. `the boot reconcile does not name "([^"]*)" as a property whose
  #         writes are dropped`
  #         The plain-negative sibling of the adoption world's existing
  #         "no longer names …" step, for a property that was never named.
  #
  # 4. WHY THE LEGACY RECORD. Since #515 a new write keys its edges by PROPERTY
  #    NAME, so it needs no reverse map at all and cannot lose the collision.
  #    The population at risk is every record written before #515 — keyed by the
  #    predicate IRI, resolved through BuildReverseMap on every read — and #523
  #    normalizes those only when the operator runs the migration. So the outline
  #    below plants the pre-#515 shape with the world's existing step and reads it
  #    back through all three readers (projection, API, canonical record). The
  #    new write beside it is the positive control: it must keep working too.
  #
  # 5. WHAT IS NOT ASSERTED HERE, AND WHY.
  #      - Dual projection into a parent table. Covered by
  #        infrastructure/database/gorm/{resource_repository,projection_manager}
  #        _test.go and application/event_handlers_test.go, which already build
  #        contexts carrying rdfs:subClassOf and weos:abstract. The scenario
  #        "A type's control entries keep their meaning" pins that the
  #        DECLARATION still reads back after this change; the table routing
  #        behind it is unchanged and already covered.
  #      - A control keyword leaking into a resource document's own @context.
  #        isTermDefinition already filters it, asserted by
  #        application/edge_key_normalization_test.go and
  #        infrastructure/graph/oxigraph/stored_shape_test.go.
  #      - The #523 normalizer's ambiguity report. It already skips a claimant
  #        whose NAME is an IRI key (edge_key_normalization.go), so a control
  #        keyword never enters its candidate set. Nothing to add.
  #      - The #515 boot refusal. It groups REFERENCE PROPERTIES; a control
  #        keyword is not one, so a collision with it neither triggers nor
  #        suppresses that refusal.
  #
  # 6. A NOTE ON THE PARENT SLUG IN THESE SCENARIOS. The parents declared below
  #    ("gadget", an absolute IRI) name no installed type. That is deliberate
  #    and safe: dual projection skips an ancestor with no projection table
  #    (resource_repository.go:143), so the collision is all that is under test.

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

  # The two spellings a parent is written in that expand ONTO A PROPERTY'S OWN
  # PREDICATE. Both go through @vocab: a bare slug is prefixed with it, and a
  # `schema:` prefix the context never declares falls back to it
  # (jsonld.ExpandIRI). The absolute spelling — the one the ControlKeywords doc
  # comment says an RDF-literate operator reaches for — is exercised in the last
  # scenario instead: the pre-#515 writer copied an absolute-IRI value into the
  # resource's OWN embedded @context, and this outline is about the reverse map
  # rather than about that.
  Scenario Outline: A record written under a predicate a control keyword also names still reads back
    Given the operator maps "maker" to "<iri>" in the stored "widget" context
    And the stored "widget" context declares these control entries:
      | entry           | value      |
      | rdfs:subClassOf | <declared> |
    And a "widget" named "Bolt cutter" stored in the old expanded edges form with "maker" referring to the vendors "Acme"
    When I create a "widget" named "Hex key" with "maker" referring to the "vendor" "Acme"
    Then no predicate of "widget" is claimed by a control keyword
    And reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Acme"
    And the API read of the "widget" "Bolt cutter" returns "maker" as the "vendor" "Acme"
    And the JSON-LD representation of the "widget" "Bolt cutter" still carries a "maker" edge to the "vendor" "Acme"
    And reading the "widget" "Hex key" back through the projection returns "maker" as the "vendor" "Acme"

    Examples: the parent spellings that collide with "maker"
      | iri                       | declared        |
      | https://schema.org/maker  | "maker"         |
      | https://schema.org/madeBy | "schema:madeBy" |

  # The boot's completeness check asks whether reverse[predicateIRI] is the
  # property's own name (preset_context_reconcile.go
  # referencePropertiesWithoutContextEntry). A keyword holding that IRI makes it
  # answer no, so the boot warns that a perfectly healthy reference is dropping
  # its writes — and, per the existing #513 contract, stops reporting the type
  # as updated. Nothing is held here and no term is repointed, so the ONLY
  # reason the boot could complain about "gadget" is the collision.
  #
  # "supplier" is the positive control: it is what makes this boot reconcile the
  # type at all, so the negative assertion above cannot rot into a permanent
  # pass if the report line is renamed.
  Scenario: The boot's completeness check never names a property a control keyword shadows
    Given the "catalog" preset adds a "gadget" reference property to "widget" targeting "vendor"
    And the twin restarts against the same database
    And the stored "widget" context declares these control entries:
      | entry           | value    |
      | rdfs:subClassOf | "gadget" |
    And the "catalog" preset adds a "supplier" reference property to "widget" targeting "vendor"
    When the twin restarts against the same database
    Then no predicate of "widget" is claimed by a control keyword
    And the boot reconcile does not name "gadget" as a property whose writes are dropped
    And the boot reconcile reports "widget" as updated
    And the stored "widget" context has an entry for "gadget"

  # The collision that needs no operator at all, and the one that will actually
  # happen: a type whose parent slug is also the name of one of its properties.
  # "A widget is a kind of gadget, and it belongs to a gadget" is ordinary
  # modelling, and both resolve to @vocab + "gadget".
  Scenario: A property named after the type's parent keeps its own predicate
    Given the "catalog" preset adds a "gadget" reference property to "widget" targeting "vendor"
    And the twin restarts against the same database
    And the stored "widget" context declares these control entries:
      | entry           | value    |
      | rdfs:subClassOf | "gadget" |
    When I create a "widget" named "Bolt cutter" with these references:
      | property | vendor |
      | maker    | Acme   |
      | gadget   | Acme   |
    Then no predicate of "widget" is claimed by a control keyword
    And reading the "widget" "Bolt cutter" back through the projection returns "gadget" as the "vendor" "Acme"
    And reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Acme"
    And the API read of the "widget" "Bolt cutter" returns "gadget" as the "vendor" "Acme"
    And the twin still reads the control entries of "widget" as:
      | reader      | value  |
      | subclass of | gadget |

  # The four readers that must keep working (AC 4). They read the RAW stored
  # context, so skipping these keys in ParseContext must not touch them — and a
  # fix that instead STRIPPED the entries from the stored context would pass
  # every other scenario in this file and fail here. The restart is the point:
  # the boot rewrites the stored context, and the entries must survive it. At
  # least one boolean is declared TRUE on purpose — a stripped context reads as
  # false, so a pair of false rows would pass while asserting nothing. No
  # resource is created here: "weos:valueObject" changes how a type is treated,
  # and every other scenario already writes and reads one.
  Scenario: A type's control entries keep their meaning while none of them claims a predicate
    Given the stored "widget" context declares these control entries:
      | entry             | value                                            |
      | rdfs:subClassOf   | "gadget"                                         |
      | weos:abstract     | false                                            |
      | weos:valueObject  | true                                             |
      | weos:adoptedTerms | ["maker"]                                        |
      | weos:termAliases  | {"maker":["https://example.org/catalog#madeBy"]} |
    When the twin restarts against the same database
    Then the twin still reads the control entries of "widget" as:
      | reader         | value                              |
      | subclass of    | gadget                             |
      | abstract       | false                              |
      | value object   | true                               |
      | adopted terms  | maker                              |
      | alias of maker | https://example.org/catalog#madeBy |
    And no predicate of "widget" is claimed by a control keyword

  # The alias path #513 added is the one place where the collision DELETES data
  # that had already been rescued. BuildReverseMap adds a historical IRI only
  # when nothing else claims it ("an alias NEVER shadows a live term"), so a
  # control keyword sitting on that IRI blocks the alias outright and the edge
  # the operator adopted the term to recover goes dark again.
  #
  # Before the fix this is not a coin flip, it fails on every run: the keyword
  # is the ONLY claimant of that IRI once "maker" has moved to the catalog
  # namespace, so the alias is always skipped.
  #
  # The shape is the adopted-PREFIX case from context_term_adoption.feature,
  # chosen because its historical IRI — https://schema.org/cat:madeBy, a
  # compact IRI expanded through @vocab before the prefix existed — is NOT
  # @vocab + the property name. That matters: EdgeProperty falls back to
  # stripping @vocab, and for any other alias that fallback would rescue the
  # read and hide the defect. It also happens to be exactly the malformed IRI
  # shape the epic's second registry guard hunts for.
  #
  # The record must be in the pre-#515 expanded shape for the alias to be
  # consulted at all — a compact record carries its own property name — so the
  # scenario plants it, and deliberately does NOT reproject: a reproject would
  # replay the creation through today's writer and rewrite the shape under test.
  # The API read and the canonical record are the load-bearing assertions; the
  # projection column was filled at plant time and is there as a control.
  Scenario: A historical IRI is not lost to a control keyword on the same IRI
    Given the operator maps "maker" to "cat:madeBy" in the stored "widget" context
    And a "widget" named "Bolt cutter" stored in the old expanded edges form with "maker" referring to the vendors "Acme"
    And the "catalog" preset declares "cat" as "https://example.org/catalog#" in the "widget" context
    And the twin restarts against the same database
    And the operator adopts the held "cat" context term for "widget"
    And the stored "widget" context records "https://schema.org/cat:madeBy" as a historical IRI for "maker"
    When the stored "widget" context declares these control entries:
      | entry           | value                             |
      | rdfs:subClassOf | "https://schema.org/cat:madeBy"   |
    Then no predicate of "widget" is claimed by a control keyword
    And the stored "widget" context records "https://schema.org/cat:madeBy" as a historical IRI for "maker"
    And the API read of the "widget" "Bolt cutter" returns "maker" as the "vendor" "Acme"
    And the JSON-LD representation of the "widget" "Bolt cutter" still carries a "maker" edge to the "vendor" "Acme"
    And reading the "widget" "Bolt cutter" back through the projection returns "maker" as the "vendor" "Acme"
