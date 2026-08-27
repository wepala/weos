@issue-520
Feature: WeOS-minted vocabulary resolves on the domain WeOS owns
  As an operator whose graph is queried across presets and may one day be federated
  I want every house predicate and class to resolve under https://weos.io/vocab/…
  So that no WeOS-minted IRI points at a domain WeOS does not control

  # WHY THIS EXISTS. Three built-in presets mint their own vocabulary because no
  # published ontology covers the concepts: meal-planning (`mp:`), memory
  # (`mem:`) and agents (`ag:`). All three sit on `weos.org`, which is not the
  # WeOS domain. This story moves them to `weos.io`. Nothing is retargeted to a
  # different ontology — `fo:Food`, PROV-O and schema.org stay exactly where
  # they are.
  #
  # ---------------------------------------------------------------------------
  # CONTRACT — what these scenarios pin, and the parts an implementer would
  # otherwise have to infer.
  #
  # 1. THE NAMESPACE IS SPELLED TWO WAYS, AND BOTH MOVE. `meal-planning` always
  #    writes the compact form: the context declares `"mp":"https://weos.org/…"`
  #    and every term and `@type` is `"mp:something"`. `memory` and `agents` do
  #    NOT: they declare the prefix AND then spell the namespace out in full —
  #    `"confidence":"https://weos.org/vocab/memory#confidence"`,
  #    `"instructions":"https://weos.org/vocab/agents#instructions"`. Editing
  #    only the three prefix lines therefore moves meal-planning and leaves ten
  #    memory/agents terms behind, pointing at weos.org, while every prefix
  #    assertion passes. The sweep scenarios below are what catch that, and they
  #    sweep the WHOLE registry rather than the three named presets, because
  #    nothing stops a fourth preset from having copied the string.
  #
  # 2. WHICH TERMS CARRY EDGES, AND WHICH DO NOT. The read paths consult the
  #    `@context` only for REFERENCE properties: a literal lives in the entity
  #    node and is copied verbatim. So the "keeps every existing edge readable"
  #    criterion has real teeth for meal-planning only — 8 of its 12 house terms
  #    key edges (`mp:recipe`, `mp:mealPlan`, `mp:occurrenceOf`,
  #    `mp:isInstanceOf`, `mp:pantry`, `mp:targetsPantry`, `mp:ingredient`,
  #    `mp:hasItem`) and 4 are literals (`mealType`, `servings`, `defaultUnit`).
  #    Every `mem:` and `ag:` term is a literal; `fact`'s only edge is
  #    `wasRevisionOf`, which is PROV-O and does not move. Memory and agents
  #    therefore change only their RDF class and their literal predicates in the
  #    graph — no edge of theirs can break, and saying so here stops a reviewer
  #    reading their thin coverage as a gap.
  #
  # 3. THE COUNTS IN THE STORY ARE SOURCE-LEVEL, SO THEY ARE NOT ASSERTED HERE
  #    AS TOTALS. "6 `@type` IRIs and 12 term mappings" counts occurrences
  #    authored in `application/presets/mealplanning/preset.go`. It is not a
  #    count of anything observable on an installed instance: `mealType` and
  #    `servings` are authored once in the shared context builder and appear on
  #    nine installed type contexts, and `mp:recipe` is authored twice. A
  #    Gherkin assertion on "12" would either restate the source file or be
  #    wrong. What IS observable — and stronger — is the pair of sweeps that open the
  #    fresh-install section: no installed type resolves anything under weos.org,
  #    and every house IRI resolves under weos.io. The literal criterion "no
  #    file under application/presets/ references weos.org" is a source fact and
  #    belongs in a unit guard beside
  #    `application/presets/preset_context_completeness_test.go`; note that
  #    `application/presets/memory/preset_test.go:92` asserts the weos.org Fact
  #    IRI today and is itself under `application/presets/`, so it is in scope
  #    for that criterion.
  #
  # 4. AN EXISTING INSTALL: THE BOOT HOLDS, AND NOTHING ON THIS BRANCH CAN
  #    ADOPT. A prefix present in both the stored and the preset context with a
  #    different value is a CONFLICT in `reconcileAdditiveContext` — held at the
  #    stored definition and reported on the "context terms diverge" warn line.
  #    That half of the criterion is fully testable now — every scenario
  #    under "AN EXISTING INSTALL — the hold".
  #    Adoption was not, while `HeldContextTerms` built its result from
  #    `rec.Moves` only: `weos resource-type held-terms` reported nothing for a
  #    Conflict and `adopt-term` refused it as "not held". Story #518 reaches
  #    the Conflict, and the scenarios under "AN EXISTING INSTALL — the
  #    adoption" are tagged @issue-518 and go green with it.
  #
  # 5. WHY #523 DOES NOT REMOVE THE NEED FOR #518 HERE. Normalizing the event
  #    store keys edges by PROPERTY NAME, which is what makes a rename cost no
  #    alias. It does not change WHERE the new IRI has to land: the resource
  #    type's STORED `@context`, which the boot is holding at weos.org. So a
  #    normalized instance still needs the stored context changed, and on this
  #    branch there is no supported command that changes it —
  #    `preset install --update` is forbidden by the epic's anti-patterns
  #    (it overwrites the stored context wholesale and records nothing), and
  #    `adopt-term` cannot see a Conflict. Normalization changes what adoption
  #    COSTS, not whether it is available. "On a normalized instance the adoption needs no
  #    alias at all" asserts exactly that payoff — after normalizing, the recorded alias can be deleted and every
  #    edge still reads — which is the epic's "normalize before you alias"
  #    claim, proved on the real presets rather than the synthetic ones.
  #
  # 6. A REPROJECTION DOES NOT MOVE A PREDICATE OR A CLASS; A RE-STAMP DOES.
  #    The projection column and the API read resolve through the TYPE's
  #    current `@context`, so they follow a new IRI as soon as the stored
  #    context changes. The knowledge graph does not: a resource's canonical
  #    record carries its OWN embedded `@context`, stamped by
  #    `BuildResourceGraph` at write time, and `worker reproject` REPLAYS that
  #    payload rather than re-deriving it. So a resource written before the move
  #    keeps `https://weos.org/vocab/meal-planning#FoodItem` as its class no
  #    matter how many times it is reprojected.
  #
  #    This story therefore adds `weos worker normalize-edge-keys --restamp`.
  #    It walks every `Resource.Created` and `Resource.Updated` event and
  #    rewrites two things, even when the edges are ALREADY keyed by property
  #    name (which is what the plain normalization stops at):
  #      - the document's embedded `@context`, to `buildStorableContext` of the
  #        type's CURRENT stored context — what a fresh write embeds today;
  #      - the entity node's `@type`, to what `BuildResourceGraph` would write
  #        now: the context's `"@type"` if it sets one, else the type name.
  #    Dry run by default, `--write` applies, idempotent, and it reports per
  #    resource type how many events it re-stamped. The full procedure is
  #    `--restamp --write`, then `worker reproject`, then
  #    `worker checkpoint reset oxigraph --truncate`.
  #
  #    Note WHICH half does the work for THIS story: the entity `@type` string
  #    is `"mp:FoodItem"` before and after, so the class moves because the
  #    embedded PREFIX moves. The `@type` half of the re-stamp is what story
  #    #521 needs, where a type gains an `@type` it never had.
  #
  # 6a. THE KNOWN LIMIT: THE `triples` TABLE IS NOT RE-STAMPED. A re-stamp does
  #    not touch `Triple.Created` / `Triple.Deleted` events, and those carry the
  #    predicate IRI resolved at write time (`resource_service.go:300`). A
  #    reprojection replays them verbatim. So after a re-stamp the KNOWLEDGE
  #    GRAPH carries the class and predicates the current stored context names,
  #    while the `triples` read-model table still holds the write-time predicate
  #    for every old edge. The two surfaces disagree, deliberately and
  #    permanently, and a scenario below asserts it so nobody reads the silence
  #    as "everything moved". This is why the scenarios distinguish "the
  #    knowledge graph" — asserted on the STORED DOCUMENT, the JSON-LD the
  #    graph store ingests verbatim, because the embedded store only exists
  #    under the oxigraph_embedded build tag and the main gate has none
  #    from "the triple store" (the `triples` table `edge_key_normalization.feature`
  #    asserts against). They are not interchangeable here.
  #
  # 7. PROVISIONING A PRE-MOVE DATABASE. The existing-install scenarios need a
  #    database written by the build BEFORE this story. The shim is to install
  #    the real presets from a registry whose type contexts have had
  #    `weos.io` rewritten back to `weos.org` — a string substitution over
  #    `PresetResourceType.Context`, not a second copy of the presets, so it
  #    cannot drift. "The twin restarts on the build that moved the vocabulary"
  #    then means: restart the same database against the unmodified
  #    `presets.NewDefaultRegistry()`.
  #
  # 8. REPORT MARKERS. These reuse the vocabulary the #510/#513 suites already
  #    established, so the assertions can be lifted rather than reinvented:
  #    a Conflict is the warn line carrying `heldContextTerms`, and
  #    "the boot reconcile reports the "X" context term as held for "Y"" reads
  #    that bucket specifically. `meal-planning`, `memory` and `agents` are NOT
  #    AutoInstall presets, so a fresh install is an explicit
  #    `weos resource-type preset install <name>`; the boot reconcile, however,
  #    runs for every REGISTERED preset whether or not it was installed, which
  #    is why the hold is reported without anyone re-running install.
  #
  # 8a. SIMULATING AN ADOPTED CONTEXT WITHOUT #518. The re-stamp scenarios need
  #    a stored context that already names weos.io. Adoption cannot produce one
  #    on this branch (CONTRACT 4), so they use the same lever the #510/#513
  #    suites use on the synthetic preset — the operator writes the term into
  #    the stored type context directly ("the operator maps "mp" to "…" in the
  #    stored "food-item" context"), here parameterised by real type slug. That
  #    is a test lever, not a supported operator procedure: what a re-stamp does
  #    is independent of HOW the stored context came to say weos.io, so pinning
  #    it this way keeps every re-stamp scenario runnable today and unchanged
  #    when #518 lands.
  #
  # ---------------------------------------------------------------------------
  # SETTLED — recorded here because the scenarios below depend on the answer.
  #
  # 1. "The `@type` changes need a reprojection" is not true of a reprojection
  #    alone, which replays payloads rather than re-deriving them. Akeem settled
  #    this on 2026-08-25: #520 adds `normalize-edge-keys --restamp`, specified
  #    in CONTRACT 6, and the runbook line becomes "re-stamp, reproject, rebuild
  #    the graph". Story #521 rests on the same mechanism and inherits it.
  #    "A reprojection alone leaves the old class; a re-stamp moves it" is the
  #    scenario that pins both halves of that sequence.
  #
  # ---------------------------------------------------------------------------
  # OPEN QUESTION — this still needs an answer before the story is called done.
  #
  # 1. When #518 teaches `AdoptContextTerms` to reach a Conflict, what does it
  #    record an alias against for a PREFIX? The existing Move path records the
  #    old IRI against each PROPERTY the prefix moves, and deliberately never
  #    against `@type`. A changed prefix moves `@type` too — `"mp:FoodItem"` is
  #    textually identical on both sides, so `@type` is never itself held, and
  #    the class moves silently as a side effect of adopting `mp`. That is the
  #    one path by which a sweep can move a class without the operator asking,
  #    which is what `selectTermsToAdopt` excludes `@type` to prevent.
  #    "Adopting a prefix that also moves the class says so" pins the
  #    behaviour this contract expects; if #518 decides a class-moving prefix
  #    must be named explicitly rather than swept, that is the scenario to
  #    amend.
  # ---------------------------------------------------------------------------

  # ===========================================================================
  # A FRESH INSTALL — fully testable on this branch.
  # ===========================================================================

  Scenario: No built-in preset mints a term on a domain WeOS does not own
    Given a clean WeOS database
    When the operator installs every built-in preset
    Then no installed resource type resolves any term, prefix or "@type" under "https://weos.org/"
    And no installed resource type resolves any term, prefix or "@type" under "http://weos.org/"

  Scenario Outline: The house prefix of each minting preset resolves on weos.io
    Given a clean WeOS database
    When the operator installs the "<preset>" preset
    Then every installed type of "<preset>" that declares "<prefix>" resolves it to "<namespace>"
    And every house IRI the installed types of "<preset>" resolve is under "<namespace>"

    Examples:
      | preset        | prefix | namespace                            |
      | meal-planning | mp     | https://weos.io/vocab/meal-planning# |
      | memory        | mem    | https://weos.io/vocab/memory#        |
      | agents        | ag     | https://weos.io/vocab/agents#        |
      | core          | core   | https://weos.io/vocab/core#          |
      | notifications | notif  | https://weos.io/vocab/notifications# |
      | tasks         | task   | https://weos.io/vocab/tasks#         |
      | website       | web    | https://weos.io/vocab/website#       |

  # `knowledge` is deliberately absent from the table above and adding a row for
  # it is a bug. #537 repaired its two properties with PUBLISHED Dublin Core
  # terms, so that preset mints no house vocabulary and has no prefix to
  # resolve. The second assertion — that every house IRI a preset resolves sits
  # under THAT preset's namespace — is also why `web-page.slug` and
  # `organization.slug` take two IRIs for one concept rather than sharing
  # `core:slug`.
  #
  # The classes. Six for meal-planning, two for memory, one for agents — the
  # `@type` IRIs the story counts, named individually so a missed one fails on
  # its own line rather than inside a total.
  Scenario Outline: A resource created on a fresh install carries its class on weos.io
    Given a clean WeOS database
    And the operator installs the "<preset>" preset
    When a "<slug>" resource is created
    Then that resource carries the RDF type "<class>"
    And no resource carries an RDF type under "https://weos.org/"

    Examples:
      | preset        | slug               | class                                                |
      | meal-planning | recipe-ingredient  | https://weos.io/vocab/meal-planning#RecipeIngredient |
      | meal-planning | meal-occurrence    | https://weos.io/vocab/meal-planning#MealOccurrence   |
      | meal-planning | pantry             | https://weos.io/vocab/meal-planning#Pantry           |
      | meal-planning | food-item          | https://weos.io/vocab/meal-planning#FoodItem         |
      | meal-planning | shopping-list      | https://weos.io/vocab/meal-planning#ShoppingList     |
      | meal-planning | shopping-list-item | https://weos.io/vocab/meal-planning#ShoppingListItem |
      | memory        | fact               | https://weos.io/vocab/memory#Fact                    |
      | memory        | playbook           | https://weos.io/vocab/memory#Playbook                |
      | agents        | agent-skill        | https://weos.io/vocab/agents#AgentSkill              |

  # The eight meal-planning terms that key EDGES. These are the ones whose move
  # a reader can observe, and the ones an existing install has to keep readable.
  Scenario Outline: A reference on a house predicate lands under weos.io and reads back
    Given a clean WeOS database
    And the operator installs the "meal-planning" preset
    And a "<target slug>" named "<target>" exists
    When I create a "<slug>" named "<name>" with "<property>" referring to the "<target slug>" "<target>"
    Then the triple store holds "<predicate>" from the "<slug>" "<name>" to the "<target slug>" "<target>"
    And the triple store holds no edge under "https://weos.org/" from the "<slug>" "<name>"
    And reading the "<slug>" "<name>" back through the projection returns "<property>" as the "<target slug>" "<target>"
    And the API read of the "<slug>" "<name>" returns "<property>" as the "<target slug>" "<target>"

    Examples:
      | slug               | name        | property      | target slug   | target   | predicate                                        |
      | food-item          | Garlic head | ingredient    | ingredient    | Garlic   | https://weos.io/vocab/meal-planning#isInstanceOf |
      | food-item          | Garlic head | pantry        | pantry        | Kitchen  | https://weos.io/vocab/meal-planning#pantry       |
      | meal-occurrence    | Tuesday     | scheduledMeal | scheduled-meal| Taco     | https://weos.io/vocab/meal-planning#occurrenceOf |
      | scheduled-meal     | Taco        | recipe        | recipe        | Tacos    | https://weos.io/vocab/meal-planning#recipe       |
      | scheduled-meal     | Taco        | mealPlan      | meal-plan     | Week 1   | https://weos.io/vocab/meal-planning#mealPlan     |
      | recipe-ingredient  | Two cloves  | recipe        | recipe        | Tacos    | https://weos.io/vocab/meal-planning#recipe       |
      | shopping-list      | Saturday    | pantry        | pantry        | Kitchen  | https://weos.io/vocab/meal-planning#targetsPantry|
      | shopping-list-item | Two limes   | ingredient    | ingredient    | Lime     | https://weos.io/vocab/meal-planning#ingredient   |
      | shopping-list-item | Two limes   | shoppingList  | shopping-list | Saturday | https://weos.io/vocab/meal-planning#hasItem      |

  # The literal side. A literal never breaks a read, so this is not about
  # readability — it is the only way memory's and agents' terms are observable
  # at all, and they are the terms a prefix-only edit would leave on weos.org
  # (CONTRACT 1).
  Scenario Outline: A literal on a house predicate is stated under weos.io
    Given a clean WeOS database
    And the operator installs the "<preset>" preset
    When I create a "<slug>" named "<name>" with "<property>" set to "<value>"
    Then the triple store holds "<predicate>" from the "<slug>" "<name>" with the value "<value>"
    And the triple store holds no statement under "https://weos.org/" about the "<slug>" "<name>"

    Examples:
      | preset        | slug         | name        | property     | value          | predicate                                          |
      | meal-planning | scheduled-meal| Taco       | mealType     | dinner         | https://weos.io/vocab/meal-planning#mealType       |
      | meal-planning | ingredient   | Garlic      | defaultUnit  | clove          | https://weos.io/vocab/meal-planning#defaultUnit    |
      | memory        | playbook     | Reorder     | trigger      | pantry is low  | https://weos.io/vocab/memory#triggerCondition      |
      | memory        | fact         | Allergy     | confidence   | 0.9            | https://weos.io/vocab/memory#confidence            |
      | agents        | agent-skill  | Shopper     | instructions | Build the list | https://weos.io/vocab/agents#instructions          |

  # Insurance against a botched find-and-replace. Two properties collapsing onto
  # one predicate is exactly what a careless substitution produces, and the
  # reverse map is a map[string]string: the loser reads back as nothing while
  # every IRI assertion above still passes. This is a narrow instance of the
  # epic-wide guard #522 owns; it is here because THIS story is the edit that
  # could cause it.
  Scenario: Every reference property still reverse-maps to its own name after the move
    Given a clean WeOS database
    When the operator installs every built-in preset
    Then every reference property of every installed type reverse-maps to its own name
    And no two properties of one installed type resolve to the same predicate IRI

  # ===========================================================================
  # AN EXISTING INSTALL — the hold. Fully testable on this branch.
  # ===========================================================================

  Scenario Outline: The upgrade holds the house prefix at its stored definition and reports it
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "<preset>" preset
    When the twin restarts on the build that moved the vocabulary
    Then the boot reconcile reports the "<prefix>" context term as held for "<slug>"
    And the stored "<slug>" context still maps "<prefix>" to "<old namespace>"

    Examples:
      | preset        | prefix | slug        | old namespace                         |
      | meal-planning | mp     | food-item   | https://weos.org/vocab/meal-planning# |
      | meal-planning | mp     | recipe      | https://weos.org/vocab/meal-planning# |
      | memory        | mem    | fact        | https://weos.org/vocab/memory#        |
      | memory        | mem    | playbook    | https://weos.org/vocab/memory#        |
      | agents        | ag     | agent-skill | https://weos.org/vocab/agents#        |

  # The prefix is not the only Conflict for memory and agents: their terms name
  # the namespace in full, so each one diverges on its own (CONTRACT 1). If an
  # implementer moves only the prefix lines, the scenario above passes and this
  # one fails — which is the whole point of having both.
  Scenario Outline: A term that spells the namespace out is held on its own account
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "<preset>" preset
    When the twin restarts on the build that moved the vocabulary
    Then the boot reconcile reports the "<term>" context term as held for "<slug>"

    Examples:
      | preset | term         | slug        |
      | memory | confidence   | fact        |
      | memory | trigger      | playbook    |
      | memory | steps        | playbook    |
      | memory | successCount | playbook    |
      | memory | failureCount | playbook    |
      | agents | instructions | agent-skill |
      | agents | tools        | agent-skill |
      | agents | mode         | agent-skill |
      | agents | widgets      | agent-skill |
      | agents | model        | agent-skill |

  Scenario: Every installed meal-planning type that declares the prefix reports it held
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "meal-planning" preset
    When the twin restarts on the build that moved the vocabulary
    Then the boot reconcile reports the "mp" context term as held for every installed meal-planning type whose stored context declares it
    And no installed meal-planning type has "mp" resolving to "https://weos.io/vocab/meal-planning#"

  # The criterion's other half: holding is worth nothing if the upgrade breaks
  # reads on the way past. It does not, and the reason is that the stored
  # context is untouched — but that has to be asserted through a read, not
  # through the stored shape, because a stored context can look right while
  # SimplifyJSONLD and extractEdgeColumns both drop the edge.
  Scenario: Every existing edge still reads back across the upgrade
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "meal-planning" preset
    And an "ingredient" named "Garlic" exists
    And a "pantry" named "Kitchen" exists
    And I create a "food-item" named "Garlic head" with these references:
      | property   | target slug | target  |
      | ingredient | ingredient  | Garlic  |
      | pantry     | pantry      | Kitchen |
    When the twin restarts on the build that moved the vocabulary
    Then reading the "food-item" "Garlic head" back through the projection returns "ingredient" as the "ingredient" "Garlic"
    And reading the "food-item" "Garlic head" back through the projection returns "pantry" as the "pantry" "Kitchen"
    And the API read of the "food-item" "Garlic head" returns "ingredient" as the "ingredient" "Garlic"
    And the JSON-LD representation of the "food-item" "Garlic head" still carries an "ingredient" edge to the "ingredient" "Garlic"

  # A write made while the prefix is held must go where the reads look, which is
  # the stored definition. If the writer resolved through the PRESET context
  # while the reader resolved through the stored one, new edges would be
  # unreadable for exactly as long as the hold lasts — and the hold lasts until
  # #518 ships.
  Scenario: A write made while the prefix is held is readable
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "meal-planning" preset
    And an "ingredient" named "Garlic" exists
    And a "pantry" named "Kitchen" exists
    And the twin restarts on the build that moved the vocabulary
    When I create a "food-item" named "Garlic head" with these references:
      | property   | target slug | target  |
      | ingredient | ingredient  | Garlic  |
      | pantry     | pantry      | Kitchen |
    Then reading the "food-item" "Garlic head" back through the projection returns "ingredient" as the "ingredient" "Garlic"
    And the triple store holds "https://weos.org/vocab/meal-planning#isInstanceOf" from the "food-item" "Garlic head" to the "ingredient" "Garlic"

  # The hold is a standing condition, not a one-time event. Until #518 ships
  # there is no command that resolves it, so an operator sees this line on every
  # start — and a report that only fires on the first boot after an upgrade
  # would be invisible to anyone who restarts before reading the log.
  Scenario: The hold is reported on every boot, not only the first
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "meal-planning" preset
    And the twin restarts on the build that moved the vocabulary
    And the boot reconcile reports the "mp" context term as held for "food-item"
    When the twin restarts on the build that moved the vocabulary again
    Then the boot reconcile reports the "mp" context term as held for "food-item"

  # Per-entry refusal. A held prefix must not take the additive merge beside it
  # down: a property the same build adds still needs its column and its term, or
  # this story silently reintroduces the drop #510 closed for every type that
  # declares a house prefix — which is most of meal-planning.
  Scenario: A held prefix does not block an additive change on the same type
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "meal-planning" preset
    And the build that moved the vocabulary also adds a "brand" string property to "food-item"
    When the twin restarts on the build that moved the vocabulary
    Then the boot reconcile reports the "mp" context term as held for "food-item"
    And the "food-item" projection table has a "brand" column
    And I create a "food-item" named "Garlic head" with "brand" set to "Acme"
    And reading the "food-item" "Garlic head" back through the projection returns "brand" as "Acme"

  # Nothing about this story may reach the stored context by itself. The epic
  # forbids `preset install --update` for exactly this change, and the boot's
  # additive merge is what enforces it — so the assertion is that an ordinary
  # upgrade, including a re-run of the ordinary install, leaves weos.org in
  # place rather than overwriting it.
  Scenario: An ordinary preset install does not overwrite the held prefix
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "meal-planning" preset
    And the twin restarts on the build that moved the vocabulary
    When the operator installs the "meal-planning" preset
    Then the stored "food-item" context still maps "mp" to "https://weos.org/vocab/meal-planning#"

  # ===========================================================================
  # THE RE-STAMP — `weos worker normalize-edge-keys --restamp`. Runnable on this
  # branch: the stored context is moved with the test lever of CONTRACT 8a, not
  # by adopting, so none of these waits on #518.
  # ===========================================================================

  # The sequence the runbook line has to state, both halves in one scenario so
  # neither can be read without the other. A reprojection replays the payload
  # and the old class survives it; the re-stamp is what rewrites the embedded
  # context the class resolves through.
  Scenario: A reprojection alone leaves the old class; a re-stamp moves it
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "meal-planning" preset
    And a "food-item" named "Garlic head" exists
    And the twin restarts on the build that moved the vocabulary
    And the operator maps "mp" to "https://weos.io/vocab/meal-planning#" in the stored "food-item" context
    And a "food-item" named "Lime wedge" exists
    When the operator reprojects the event feed
    Then the "food-item" "Lime wedge" carries the RDF type "https://weos.io/vocab/meal-planning#FoodItem" in the stored document
    And the "food-item" "Garlic head" carries the RDF type "https://weos.org/vocab/meal-planning#FoodItem" in the stored document
    When the operator re-stamps the stored documents and writes
    And the operator reprojects the event feed
    Then the "food-item" "Garlic head" carries the RDF type "https://weos.io/vocab/meal-planning#FoodItem" in the stored document
    And every "food-item" resource carries the same RDF type in the stored document
    # Scoped to food-item (amended 2026-08-25, approved): the pantry and ingredient a
    # food-item REQUIRES are fixtures whose types the lever never re-mapped.
    And no "food-item" resource carries an RDF type under "https://weos.org/" in the stored document

  # A re-stamp rewrites stored events. The guarantee that makes that acceptable
  # is that nothing a reader sees moves — the projection and the API were
  # already resolving through the type's current context, so they were correct
  # before the re-stamp and must be identical after it.
  Scenario: A re-stamp moves the class in the graph and changes nothing an API reader sees
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "meal-planning" preset
    And an "ingredient" named "Garlic" exists
    And a "pantry" named "Kitchen" exists
    And I create a "food-item" named "Garlic head" with these references:
      | property   | target slug | target  |
      | ingredient | ingredient  | Garlic  |
      | pantry     | pantry      | Kitchen |
    And the twin restarts on the build that moved the vocabulary
    And the operator maps "mp" to "https://weos.io/vocab/meal-planning#" in the stored "food-item" context
    And the read of every resource with a reference property is captured
    When the operator re-stamps the stored documents and writes
    And the operator reprojects the event feed
    Then the read of every resource with a reference property matches what was captured
    And the API read of the "food-item" "Garlic head" returns "ingredient" as the "ingredient" "Garlic"
    And reading the "food-item" "Garlic head" back through the projection returns "pantry" as the "pantry" "Kitchen"

  # The edge half. The stored key is already the property name, so the predicate
  # the graph sees comes entirely from the document's embedded context — which
  # is exactly what the re-stamp rewrites.
  Scenario: A re-stamped edge takes its predicate from the context the type has now
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "meal-planning" preset
    And an "ingredient" named "Garlic" exists
    And a "pantry" named "Kitchen" exists
    And I create a "food-item" named "Garlic head" with "ingredient" referring to the "ingredient" "Garlic"
    And the twin restarts on the build that moved the vocabulary
    And the operator maps "mp" to "https://weos.io/vocab/meal-planning#" in the stored "food-item" context
    When the operator re-stamps the stored documents and writes
    Then the stored event for the "food-item" "Garlic head" maps "mp" to "https://weos.io/vocab/meal-planning#" in its own context
    When the operator reprojects the event feed
    Then the stored document states "https://weos.io/vocab/meal-planning#isInstanceOf" from the "food-item" "Garlic head" to the "ingredient" "Garlic"
    And the stored document states no edge under "https://weos.org/" from the "food-item" "Garlic head"

  # Memory and agents spell the namespace out in full, so their LITERAL
  # predicates move with the re-stamp too. This is the only way those terms are
  # observable at all (CONTRACT 2), and it is the second place a prefix-only
  # edit would be caught.
  Scenario Outline: A re-stamped literal predicate follows the current context
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "<preset>" preset
    And I create a "<slug>" named "<name>" with "<property>" set to "<value>"
    And the twin restarts on the build that moved the vocabulary
    And the operator maps "<prefix>" to "<namespace>" in the stored "<slug>" context
    And the operator maps "<property>" to "<new predicate>" in the stored "<slug>" context
    When the operator re-stamps the stored documents and writes
    And the operator reprojects the event feed
    Then the stored document states "<new predicate>" from the "<slug>" "<name>" with the value "<value>"
    And the stored document states no statement under "https://weos.org/" about the "<slug>" "<name>"

    Examples:
      | preset | slug        | name    | property     | value          | new predicate                                 | prefix | namespace                     |
      | memory | playbook    | Reorder | trigger      | pantry is low  | https://weos.io/vocab/memory#triggerCondition | mem    | https://weos.io/vocab/memory# |
      | memory | fact        | Allergy | confidence   | 0.9            | https://weos.io/vocab/memory#confidence       | mem    | https://weos.io/vocab/memory# |
      | agents | agent-skill | Shopper | instructions | Build the list | https://weos.io/vocab/agents#instructions     | ag     | https://weos.io/vocab/agents# |

  # Same default as the normalization it extends: an operator inspects before
  # rewriting stored events, and the report is per resource type so a large
  # instance can be reasoned about before the write.
  Scenario: The re-stamp reports per resource type and writes nothing until told to
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "meal-planning" preset
    And a "food-item" named "Garlic head" exists
    And a "food-item" named "Lime wedge" exists
    And a "pantry" named "Kitchen" exists
    And the twin restarts on the build that moved the vocabulary
    And the operator maps "mp" to "https://weos.io/vocab/meal-planning#" in the stored "food-item" context
    When the operator re-stamps the stored documents as a dry run
    Then the re-stamp reports 2 events to re-stamp for "food-item"
    And the re-stamp reports nothing to re-stamp for "pantry"
    And the re-stamp reports itself as a dry run
    And the stored events are byte-identical to the ones stored before the run
    When the operator re-stamps the stored documents and writes
    Then the re-stamp re-stamped 2 events for "food-item"
    And the stored event for the "food-item" "Garlic head" maps "mp" to "https://weos.io/vocab/meal-planning#" in its own context

  Scenario: A second re-stamp re-stamps nothing
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "meal-planning" preset
    And a "food-item" named "Garlic head" exists
    And the twin restarts on the build that moved the vocabulary
    And the operator maps "mp" to "https://weos.io/vocab/meal-planning#" in the stored "food-item" context
    And the operator re-stamps the stored documents and writes
    When the operator re-stamps the stored documents as a dry run
    Then the re-stamp reports nothing to re-stamp
    When the operator re-stamps the stored documents and writes
    Then the re-stamp re-stamped 0 events
    And the stored events are byte-identical to the ones stored before the second run

  # The no-op case, and the one that decides whether a re-stamp is safe to leave
  # in a deployment script. Nothing about the stored context changed, so a
  # re-stamp must be able to say so rather than rewriting every event in the
  # database to the same bytes.
  Scenario: A re-stamp on an instance whose context never changed re-stamps nothing
    Given a clean WeOS database
    And the operator installs the "meal-planning" preset
    And an "ingredient" named "Garlic" exists
    And a "food-item" named "Garlic head" with "ingredient" referring to the "ingredient" "Garlic" exists
    When the operator re-stamps the stored documents as a dry run
    Then the re-stamp reports nothing to re-stamp
    When the operator re-stamps the stored documents and writes
    Then the re-stamp re-stamped 0 events
    And the stored events are byte-identical to the ones stored before the run
    And reading the "food-item" "Garlic head" back through the projection returns "ingredient" as the "ingredient" "Garlic"

  # The blast radius. A re-stamp rewrites exactly two things (CONTRACT 6): the
  # document's `@context` and the entity node's `@type`. Every literal beside
  # them, and every edge key, is left where it was — which is what keeps the
  # rewrite reviewable and the rollback "restore the backup".
  Scenario: The re-stamp touches only the document context and the entity @type
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "meal-planning" preset
    And an "ingredient" named "Garlic" exists
    And a "pantry" named "Kitchen" exists
    And I create a "food-item" named "Garlic head" with "ingredient" referring to the "ingredient" "Garlic" and "unit" set to "clove"
    And the twin restarts on the build that moved the vocabulary
    And the operator maps "mp" to "https://weos.io/vocab/meal-planning#" in the stored "food-item" context
    When the operator re-stamps the stored documents and writes
    Then the entity node of the stored event for the "food-item" "Garlic head" is byte-identical to the one stored before the run apart from its "@type"
    And the stored event for the "food-item" "Garlic head" keys its "ingredient" edge by the property name
    And the event feed holds the same events in the same order as before the run
    And every event keeps its aggregate id, sequence number and event type
    # Triple.* move with the document (amended 2026-08-25, approved): left on the old IRI,
    # a reprojection folds the old predicate back in as a second edge.
    And no event of a type other than "Resource.Created", "Resource.Updated", "Triple.Created" or "Triple.Deleted" was re-stamped
    When the operator reprojects the event feed
    Then reading the "food-item" "Garlic head" back through the projection returns "unit" as "clove"
    And reading the "food-item" "Garlic head" back through the projection returns "ingredient" as the "ingredient" "Garlic"

  # CONTRACT 6a, asserted rather than left to be discovered. The `triples` table
  # is fed by `Triple.Created` payloads, which a re-stamp does not rewrite and a
  # reprojection replays verbatim. An operator reading only the knowledge graph
  # would conclude the whole instance moved; it did not.
  # Amended 2026-08-25 (approved): --restamp moves the aggregate's Triple.* events too, so
  # the triples table now receives the current predicate on reproject. The remaining limit
  # is that the triple projection is upsert-only — the row under the OLD predicate lingers
  # until the projection is truncated and rebuilt.
  Scenario: The triples table follows the re-stamped predicate on reproject
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "meal-planning" preset
    And an "ingredient" named "Garlic" exists
    And I create a "food-item" named "Garlic head" with "ingredient" referring to the "ingredient" "Garlic"
    And the twin restarts on the build that moved the vocabulary
    And the operator maps "mp" to "https://weos.io/vocab/meal-planning#" in the stored "food-item" context
    When the operator re-stamps the stored documents and writes
    And the operator reprojects the event feed
    Then the stored document states "https://weos.io/vocab/meal-planning#isInstanceOf" from the "food-item" "Garlic head" to the "ingredient" "Garlic"
    And the triple store holds "https://weos.io/vocab/meal-planning#isInstanceOf" from the "food-item" "Garlic head" to the "ingredient" "Garlic"
    And reading the "food-item" "Garlic head" back through the projection returns "ingredient" as the "ingredient" "Garlic"

  # ===========================================================================
  # AN EXISTING INSTALL — the adoption. Delivered by #518, which teaches
  # `HeldContextTerms` to report `rec.Conflicts` beside `rec.Moves` and
  # `AdoptContextTerms` to take them (CONTRACT 4). Promoted out of @wip with
  # that story; before it lands these fail, which is the point.
  # ===========================================================================

  @issue-518
  Scenario: The held prefix is reported by held-terms so the operator can see the decision
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "meal-planning" preset
    And the twin restarts on the build that moved the vocabulary
    When the operator lists the held terms for "meal-planning" "food-item"
    Then "mp" is reported as held, stored at "https://weos.org/vocab/meal-planning#" and offered at "https://weos.io/vocab/meal-planning#"

  @issue-518
  Scenario: Adopting the held prefix moves the stored context and records what the edges use
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "meal-planning" preset
    And an "ingredient" named "Garlic" exists
    And a "pantry" named "Kitchen" exists
    And I create a "food-item" named "Garlic head" with "ingredient" referring to the "ingredient" "Garlic"
    And the twin restarts on the build that moved the vocabulary
    When the operator adopts the held "mp" context term for "meal-planning" "food-item"
    Then the stored "food-item" context maps "mp" to "https://weos.io/vocab/meal-planning#"
    And the stored "food-item" context records "https://weos.org/vocab/meal-planning#isInstanceOf" as a historical IRI for "ingredient"
    And the JSON-LD representation of the "food-item" "Garlic head" still carries an "ingredient" edge to the "ingredient" "Garlic"
    When the operator reprojects the event feed
    Then reading the "food-item" "Garlic head" back through the projection returns "ingredient" as the "ingredient" "Garlic"

  @issue-518
  Scenario: A write made after adoption lands on weos.io beside an edge that predates it
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "meal-planning" preset
    And an "ingredient" named "Garlic" exists
    And a "pantry" named "Kitchen" exists
    And I create a "food-item" named "Garlic head" with "ingredient" referring to the "ingredient" "Garlic"
    And the twin restarts on the build that moved the vocabulary
    And the operator adopts the held "mp" context term for "meal-planning" "food-item"
    When I create a "food-item" named "Lime wedge" with "ingredient" referring to the "ingredient" "Garlic"
    Then the triple store holds "https://weos.io/vocab/meal-planning#isInstanceOf" from the "food-item" "Lime wedge" to the "ingredient" "Garlic"
    And reading the "food-item" "Lime wedge" back through the projection returns "ingredient" as the "ingredient" "Garlic"
    And reading the "food-item" "Garlic head" back through the projection returns "ingredient" as the "ingredient" "Garlic"

  # Amended 2026-08-25 while writing #518's contract. The original expected one
  # sweep to settle the type. It cannot, and must not: `playbook` declares
  # `"@type":"mem:Playbook"`, so the `mem` prefix moves the RDF class as well as
  # the literal predicates, and #521's rule — a sweep never moves the class —
  # applies to a Conflict prefix exactly as it applies to a Move. The sweep
  # therefore takes the absolute terms (`trigger` and its siblings) and hands
  # `mem` back with the command that takes it. Settling the boot is two steps,
  # and the second one is a decision the operator makes knowingly.
  @issue-518
  Scenario: The boot settles once the sweep and the class-moving prefix are both adopted
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "memory" preset
    And the twin restarts on the build that moved the vocabulary
    When the operator adopts every held context term for "memory" "playbook"
    Then the operator is told the class was not adopted and how to adopt it
    And the stored "playbook" context maps "trigger" to "https://weos.io/vocab/memory#triggerCondition"
    And the stored "playbook" context maps "mem" to "https://weos.org/vocab/memory#"
    When the operator adopts the held "mem" context term for "memory" "playbook"
    And the twin restarts on the build that moved the vocabulary again
    Then the boot reconcile does not report the "mem" context term as held for "playbook"
    And the boot reconcile does not report the "trigger" context term as held for "playbook"
    And the boot reconcile records no failure for "playbook"
    And the stored "playbook" context maps "mem" to "https://weos.io/vocab/memory#"

  # CONTRACT 5, proved on the real presets: on a NORMALIZED instance the alias
  # adoption records has no work to do, because the stored edge is keyed by the
  # property name and resolves through whatever the context says today. Deleting
  # every historical IRI and reprojecting must change nothing. If it does, the
  # epic's "normalize before you alias" claim is false for the presets that
  # actually ship.
  @issue-518
  Scenario: On a normalized instance the adoption needs no alias at all
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "meal-planning" preset
    And an "ingredient" named "Garlic" exists
    And a "food-item" named "Garlic head" written by the pre-#515 binary with "ingredient" referring to the "ingredient" "Garlic"
    And the operator normalizes the stored edge keys and writes
    And the twin restarts on the build that moved the vocabulary
    When the operator adopts the held "mp" context term for "meal-planning" "food-item"
    And the operator removes every historical IRI from the stored "food-item" context
    And the operator reprojects the event feed
    Then reading the "food-item" "Garlic head" back through the projection returns "ingredient" as the "ingredient" "Garlic"
    And the API read of the "food-item" "Garlic head" returns "ingredient" as the "ingredient" "Garlic"
    And the JSON-LD representation of the "food-item" "Garlic head" still carries an "ingredient" edge to the "ingredient" "Garlic"

  # THE OPEN QUESTION, SETTLED BY #518. `@type` is `"mp:FoodItem"` on both
  # sides, so it never diverges and is never held — the class moves as a
  # consequence of adopting the `mp` prefix, on the one path
  # `selectTermsToAdopt`'s `@type` exclusion could not see. The rule #518
  # adopts is #521's, extended to Conflicts: a SWEEP never takes a term the
  # class expands through (see the sweep scenario above), and NAMING it takes
  # it and reports the class move with the migration that applies it. This
  # scenario asserts the second half — reported at adoption time, not
  # discovered later in the graph.
  @issue-518
  Scenario: Adopting a prefix that also moves the class says so
    Given a WeOS database provisioned by the build before the vocabulary moved
    And the operator installs the "meal-planning" preset
    And a "food-item" named "Garlic head" exists
    And the twin restarts on the build that moved the vocabulary
    When the operator adopts the held "mp" context term for "meal-planning" "food-item"
    Then the adoption reports the "food-item" RDF class moving from "https://weos.org/vocab/meal-planning#FoodItem" to "https://weos.io/vocab/meal-planning#FoodItem"
    And the adoption names the migration that applies it to existing resources
