@issue-535
Feature: A meal-planning house property states a predicate its vocabulary defines
  As an operator whose meal-planning graph is read by an LLM and reused by a second twin
  I want every house property to carry a term WeOS mints rather than one schema.org never published
  So that a consumer resolving a predicate against schema.org gets an answer instead of a 404

  # WHY THIS EXISTS. Every `meal-planning` type sets `"@vocab":"https://schema.org/"`.
  # A property with no term of its own rides that default, so `quantity` on a
  # shopping list item is written as `https://schema.org/quantity` — an assertion
  # that schema.org defines a term it does not. The IRI travels: anything reusing
  # the type inherits the claim, and `mini-me-weos` already reuses
  # `ShoppingListItem` and `MealOccurrence` so both graphs answer one query.
  #
  # `@vocab` pointing at a real vocabulary is the RIGHT default and stays. The
  # gap is only the house properties that need an explicit term beside it, the
  # way `mp:recipe` and `mp:ingredient` already have one.
  #
  # ---------------------------------------------------------------------------
  # CONTRACT — what these scenarios pin, and the parts an implementer would
  # otherwise have to infer. Every IRI named below was resolved against
  # schema.org on 2026-08-26, not assumed; the property inventory was swept out
  # of `presets.NewDefaultRegistry()` on this branch, not read off the issue.
  #
  # 1. THREE DIFFERENT FAULTS WEAR THE SAME COSTUME, AND THEY NEED DIFFERENT
  #    FIXES. Every one of them is "an untermed property riding @vocab", but
  #    what is wrong differs, and so does the repair:
  #
  #    a. A MINT — schema.org returns 404 for the name. There is no published
  #       term to reach for, so the fix is a house term under
  #       `https://weos.io/vocab/meal-planning#`. Verified 404: `quantity`,
  #       `unit`, `optional`, `storage`, `expirationDate`, `isDefault`,
  #       `createdAt`, `checked`, `cookedAt`, `date`.
  #
  #    b. A DOMAIN MISUSE — schema.org DOES define the name, for something else
  #       entirely. `https://schema.org/status` is "the status of the study",
  #       on MedicalCondition / MedicalProcedure / MedicalStudy.
  #       `https://schema.org/preparation` is "typical preparation that a
  #       patient must undergo before having the procedure performed", on
  #       MedicalProcedure. A meal occurrence is not a clinical trial and
  #       "finely chopped" is not a pre-operative instruction. The IRI
  #       resolves, so no 404 ever reveals it — which makes this the half a
  #       spot-check misses.
  #
  #       `preparation` IS a domain misuse and is repaired here. Recording why,
  #       because a name-only sweep of the official vocabulary reports it as a
  #       legitimate term and a reviewer will meet that reading: the sweep is
  #       right that schema.org defines the name, and that is exactly the
  #       property of class 1b. `schema:preparation` means "typical preparation
  #       that a patient must undergo before having the procedure performed"
  #       (MedicalProcedure) — the same medical family as `status`, reached by
  #       the same route, costing the same thing. Treating it as a real term
  #       because it resolves is the mistake the deny-list exists to stop. If
  #       Akeem rules otherwise it is one Examples row.
  #
  #    c. A NAME THE PUBLISHED VOCABULARY ALREADY HAS UNDER A DIFFERENT SPELLING
  #       — `meal-occurrence.date` mints `https://schema.org/date` (404) when
  #       `https://schema.org/startDate` is the published term for exactly this,
  #       and a downstream consumer already reads it. Here the fix is a
  #       schema.org term, NOT a house one. Minting `mp:date` would pass every
  #       "no bad schema.org IRI" assertion in this file and still be wrong,
  #       which is why `date` gets a scenario of its own rather than a row in
  #       the table above it.
  #
  # 2. THE OVER-CORRECTION IS THE REAL RISK, AND IT IS INVISIBLE. Dragging a
  #    genuine schema.org name into the house vocabulary breaks nothing a test
  #    would notice — the projection and the API resolve through the type's own
  #    context either way, so every read still passes — while quietly destroying
  #    the SEO structured data and the grounded-reasoning payoff the whole
  #    ontology strategy exists for. Nothing else in the suite catches it. The
  #    sharpest case is `food-item`: `quantity`, `unit`, `storage` and
  #    `expirationDate` are mints, and `purchaseDate` sitting beside them is a
  #    real schema.org property ("the date the item, e.g. vehicle, was purchased
  #    by the current owner", Product / Vehicle) whose SENSE is exactly right
  #    for a food item. Four move and one must not, on one type.
  #
  #    Note what that case settles about the RULE: it is "does the published
  #    vocabulary define this term, in this sense", NOT "is this type listed in
  #    the term's domainIncludes". schema.org's domainIncludes is advisory, and
  #    a strict reading of it would move `purchaseDate`, half of `scheduled-meal`
  #    and most of `recipe` into the house namespace. A domain misuse is a
  #    judgement about MEANING (1b), which is why it cannot be mechanised and is
  #    carried by an explicit list — see 4.
  #
  # 3. NONE OF THESE PROPERTIES IS A REFERENCE, SO THIS CONTRADICTS NO EXISTING
  #    GUARD. Every property this story repairs is a LITERAL. The read paths
  #    consult the `@context` only for reference properties, so
  #    `presets.ContextGuardViolations`, `house_vocabulary_domain.feature` and
  #    the #513/#522 suites — all of which reason about edges — neither cover
  #    this class nor conflict with it. Two consequences worth stating:
  #      - no edge can break here, so "keeps existing data readable" is a weaker
  #        claim than it was for #520, and the upgrade scenarios below assert it
  #        on the LITERAL read paths rather than on edges;
  #      - the collision risk the fix introduces (two properties of one type
  #        collapsing onto one house IRI) is ALREADY swept registry-wide by
  #        `house_vocabulary_domain.feature`'s "Every reference property still
  #        reverse-maps to its own name after the move", whose second assertion
  #        is "no two properties of one installed type resolve to the same
  #        predicate IRI". That scenario runs over every built-in preset and
  #        needs no amendment. Do not restate it here.
  #
  # 4. HOW A TEST DECIDES "THE PUBLISHED VOCABULARY DOES NOT DEFINE THIS",
  #    OFFLINE. The gate must not touch the network: schema.org going down, or
  #    getting slow, must never redden CI, and a guard that only fires when the
  #    machine has DNS is not a guard. So the sweep carries its own answer:
  #
  #      - A set of PUBLISHED NAMESPACES it polices — `https://schema.org/`,
  #        SKOS, FOAF, W3C ORG, PROV-O, food ontology, vCard, Activity Streams,
  #        GoodRelations. An IRI landing outside all of them is not this guard's
  #        business.
  #      - `https://weos.io/vocab/…` is EXEMPT. WeOS publishes it, so WeOS
  #        defines whatever it names. #520's guard already polices its shape.
  #      - Per policed namespace, a checked-in ALLOW-LIST of the local names the
  #        presets legitimately borrow. One entry means one human resolved one
  #        IRI. An effective predicate landing in a policed namespace whose local
  #        name is absent from that namespace's list is a violation.
  #      - A separate DENY-LIST for 1b: names that DO resolve but whose published
  #        meaning is wrong for the WeOS use. `schema:status` and
  #        `schema:preparation` are its founding members. An allow-list alone can
  #        never catch these, because the term exists.
  #
  #    THE LIMITS, SAID PLAINLY, because a guard whose limits are undocumented
  #    gets trusted past them:
  #      - it is exactly as good as the human who curated the list;
  #      - a genuinely new schema.org property fails until someone adds it. That
  #        is the point, not a defect: adding the row IS the verification step;
  #      - it cannot find a domain misuse nobody noticed. `preparation` sat
  #        undetected until this sweep and would have again;
  #      - it never notices schema.org deprecating or moving a term.
  #
  # 5. THE SWEEP IS REGISTRY-WIDE, AND MEAL-PLANNING IS NOT THE ONLY OFFENDER.
  #    Measured, not estimated: the official schema.org vocabulary
  #    (`schemaorg-current-https.jsonld`, 1521 rdf:Property entries) was
  #    cross-referenced against every untermed property of every type in
  #    `presets.NewDefaultRegistry()`. 164 properties ride `@vocab`; the large
  #    majority are fine. Beyond meal-planning's 14, the same rule reports 18:
  #
  #      core           person.avatarURL, organization.logoURL,
  #                     organization.slug
  #      notifications  notification.actionLabel, .actionUrl, .body,
  #                     .dedupeKey, .kind, .occurredAt, .read, .taskRef
  #      tasks          task.dueDate, task.priority
  #      website        web-page.slug, web-page.template,
  #                     web-page-element.content, web-page-template.slots,
  #                     web-page-template.templateBody
  #
  #    Plus, in a DIFFERENT published vocabulary, `knowledge.concept-scheme`'s
  #    `title` and `description`: they ride a SKOS `@vocab` and mint
  #    `skos:title` / `skos:description`, which SKOS core defines neither of
  #    (both are Dublin Core terms). That pair is why the guard must police
  #    namespaces generally rather than hard-code schema.org — a schema.org-only
  #    check reports the whole `knowledge` preset as clean.
  #
  #    And `tasks.project.status` / `tasks.task.status` are the SAME medical
  #    misuse meal-planning is being repaired for. They are class 1b, not
  #    mints, so they belong on the deny-list waivers rather than the mint
  #    waivers — and a guard that waives only mints would let them pass while
  #    claiming to police the class.
  #
  #    THIS STORY REPAIRS MEAL-PLANNING ONLY. So the guard ships with an
  #    explicit WAIVER list naming every offender above, and the registry sweep
  #    asserts the violation set EQUALS the waivers — not "contains no
  #    meal-planning entry". Exact equality is what makes the list only ever
  #    shrink: a new offender anywhere fails on the day it is authored, which is
  #    the criterion the issue asks for, and repairing one elsewhere fails until
  #    its waiver line is deleted. No meal-planning waiver may survive this
  #    story.
  #
  # 6. WHERE EACH ASSERTION BELONGS. The registry sweep of 4 and 5 is a pure
  #    function of the registry — no database, no boot — so per the precedent
  #    #522 set it lives beside the existing sweeps in
  #    `application/presets/`, where it runs in `make test-unit` on every change
  #    rather than only in the e2e job. The implementer MUST write these, and
  #    they are where the epic-wide criterion is actually nailed down:
  #
  #    application/presets/published_vocabulary_test.go (new)
  #      - TestPresets_NoPropertyClaimsAnUndefinedPublishedTerm
  #          Sweeps every type of every built-in preset. Violations must EQUAL
  #          the waiver list (5). Name the preset, type, property and effective
  #          IRI on every failing line — a bare count is unactionable.
  #      - TestPresets_NoPropertyClaimsAPublishedTermForAnotherSubject
  #          The deny-list half. Same equality rule, same waivers.
  #      - TestPresets_EveryAllowListedTermIsStillUsed
  #          An allow-list nobody prunes becomes a rubber stamp. Fails on an
  #          entry no preset resolves any more.
  #
  #    Extend `presets.ContextGuardViolations` rather than adding a parallel
  #    entry point, so the overlay build's private registry inherits this sweep
  #    the way it inherits the #517 rules. That is what the exported function is
  #    for.
  #
  #    The new term constants build from `jsonld.MealPlanningVocab`, never from
  #    a literal namespace string. `pkg/jsonld/vocab.go` already centralises the
  #    house namespaces on `HouseVocabBase` precisely so a future move is one
  #    edit; #520 was the story that had to move them, and a literal here
  #    reintroduces the problem it just finished solving.
  #
  #    The scenarios below pin what is observable on a RUNNING instance: what an
  #    installed type resolves, what a written value states in the graph, and
  #    what an upgrade does to a database that predates the terms.
  #
  # 7. THE SHARED CONTEXT BUILDER IS A TRAP. `mpContext` is one function feeding
  #    every meal-planning type, and `mealType` / `servings` are declared there
  #    for all of them. Declaring `quantity` and `unit` there too would put them
  #    on `recipe`, `cookbook` and `restricted-diet`, which have no such
  #    property. Inert today, but it makes the context lie about the type, and
  #    it is how a future collision gets built. Declare each term on the types
  #    that have the property — the `extraTerms` argument already exists for it.
  #
  # 8. AN EXISTING INSTALL NEEDS NO OPERATOR ACTION, AND THAT IS WORTH PINNING.
  #    A term the preset declares and the stored context LACKS is merged in
  #    additively (`reconcileAdditiveContext`); nothing is held, because nothing
  #    diverges. So unlike #520 this upgrade is silent — no `held-terms`, no
  #    `adopt-term`, nothing to approve. What it does NOT do is move the
  #    predicate on data already written, and the silence is what makes that
  #    dangerous.
  #
  #    BE PRECISE ABOUT WHICH PART OF AN OLD RECORD IS STALE, because "#515
  #    keys edges by property name, so stored documents need no rewriting" is
  #    true and still leaves this broken. The stored KEY is indeed the property
  #    name. The PREDICATE that key resolves to comes from the document's own
  #    embedded `@context`, stamped by `BuildResourceGraph` at write time, and
  #    `worker reproject` replays that payload rather than re-deriving it. An
  #    old record's embedded context has no `unit` term, so `unit` still rides
  #    `@vocab` to `https://schema.org/unit` on every replay. That is exactly
  #    what `worker normalize-edge-keys --restamp` exists to rewrite (#520
  #    CONTRACT 6), and it applies to a literal for the same reason it applied
  #    to a class.
  #
  # 8a. THE GRAPH STORE NEEDS REBUILDING, NOT JUST REPROJECTING. Triples are
  #    materialised into a persisted store with the predicate resolved at write
  #    time, and the triple projection is UPSERT-ONLY. So a re-stamp plus a
  #    reproject writes the house predicate, but the row under
  #    `https://schema.org/unit` lingers beside it: a SPARQL query by the house
  #    predicate now answers, and a query by the schema.org one still answers
  #    too. Only `weos worker checkpoint reset oxigraph --truncate` clears the
  #    old row. The full sequence is the one #520 already established —
  #    re-stamp, reproject, truncate and rebuild — and this story inherits it
  #    rather than inventing a second procedure. The scenario "An old value
  #    keeps its write-time predicate in the graph until a re-stamp" asserts
  #    both halves, the lingering row included, so nobody ships a runbook that
  #    stops after the reproject.
  #
  #    THIS IS NOT A LOCAL CONCERN. The live twin runs on GCP against real
  #    meal-planning data, so whatever the runbook says is what somebody
  #    actually executes. Whether #535 ships that runbook or defers it is
  #    OPEN QUESTION 3.
  #
  # 9. THE DOWNSTREAM CONSUMER IS SAFE BY CONSTRUCTION, AND IS NOT TESTED HERE.
  #    `mini-me-weos` reuses `ShoppingListItem` and `MealOccurrence` and pins
  #    core's context by READING it at test time
  #    (`TestGroceryListItemSpeaksCoresShoppingListLanguage`) rather than by
  #    literal. It follows automatically. Nothing in this repo can assert that,
  #    and a scenario claiming to would be asserting against a stub.
  #
  # ---------------------------------------------------------------------------
  # THE SHIM FOR AN EXISTING INSTALL. The upgrade scenarios need a database
  # written by the build BEFORE this story. Use #520's CONTRACT 7 pattern: a
  # registry whose meal-planning type contexts have the new terms STRIPPED —
  # a transform over `PresetResourceType.Context`, not a second copy of the
  # presets, so it cannot drift. "The twin restarts on the build that terms the
  # house properties" then means: restart the same database against the
  # unmodified `presets.NewDefaultRegistry()`.
  #
  # ---------------------------------------------------------------------------
  # OPEN QUESTIONS — these need Akeem before the story is called done.
  #
  # 1. `expirationDate` and `createdAt` are pinned below as house mints, per the
  #    issue's "give each house property an explicit mp: term". But schema.org
  #    publishes near neighbours: `schema:expires` ("date the content expires
  #    and is no longer useful or available", Certification / CreativeWork) and
  #    `schema:dateCreated` ("the date on which the CreativeWork was created",
  #    CreativeWork / DataFeedItem). Both are CreativeWork-shaped, so a food
  #    item's expiry and a shopping list's creation are a stretch of SENSE, not
  #    an obvious fit — which is the test 1b sets. Taking the published term
  #    would be the same move `date` makes. If Akeem prefers them, two Examples
  #    rows move from the mint outline to the "already has a published spelling"
  #    scenario and nothing else in this file changes.
  #
  # 2. THE SCOPE OF THE GUARD. #535 names meal-planning, but the guard it asks
  #    for lights up 18 further violations in four presets this issue does not
  #    own (CONTRACT 5). Three shapes are available, and the contract above is
  #    written for the third:
  #      a. Repair all 32 now. Honest, and turns a meal-planning ticket into a
  #         registry-wide rewrite touching `core`, `notifications`, `tasks`,
  #         `website` and `knowledge` — five presets, five different sets of
  #         downstream consumers, one review.
  #      b. Scope the guard to meal-planning. Ships green and green stays
  #         meaningless: the class walks straight back in through the next
  #         preset somebody writes, which is the exact failure #535 was filed
  #         about.
  #      c. RATCHET (what is pinned above). Repair meal-planning's 14, add the
  #         registry-wide guard, and carry the other 18 as an explicit
  #         exemption list the guard reads. No NEW mint can be introduced
  #         anywhere from the day this merges, and the existing ones are
  #         visible rather than forgotten. This is how the repo already works
  #         (`ContextGuardViolations` plus tracked exemptions), and the exact-
  #         equality rule means the list can only shrink.
  #    The SDET recommendation is (c). It needs one thing to not rot: each
  #    exemption line wants a follow-up issue, or the list is a to-do list with
  #    no owner. `notifications` (8) and `website` (5) are the two large ones
  #    and are the natural first two tickets.
  #
  # 3. DOES #535 SHIP THE MIGRATION RUNBOOK, OR DEFER IT? CONTRACT 8a: reads
  #    are correct the moment the boot merges the terms, so nothing is broken
  #    for an API or projection consumer and the upgrade needs no action. The
  #    graph is the surface that goes stale, and the live twin on GCP is the
  #    instance that has one. Deferring is defensible — the sequence already
  #    exists from #520 and nothing new is needed to run it — but somebody has
  #    to be told to run it, and "the twin answers a SPARQL query two ways for
  #    a while" is the cost of not being told.
  # ---------------------------------------------------------------------------

  # ===========================================================================
  # A FRESH INSTALL — the terms themselves.
  # ===========================================================================

  # CONTRACT 1a. Thirteen properties across six types, each of which mints an
  # IRI schema.org answers 404 for. Named one per row rather than counted, so a
  # missed one fails on its own line instead of inside a total.
  @wip
  Scenario Outline: A property schema.org does not define resolves to the house vocabulary
    Given a clean WeOS database
    When the operator installs the "meal-planning" preset
    Then the "<slug>" type resolves the property "<property>" to "<predicate>"

    Examples: the names schema.org returns 404 for
      | slug               | property       | predicate                                          |
      | recipe-ingredient  | quantity       | https://weos.io/vocab/meal-planning#quantity       |
      | recipe-ingredient  | unit           | https://weos.io/vocab/meal-planning#unit           |
      | recipe-ingredient  | optional       | https://weos.io/vocab/meal-planning#optional       |
      | meal-occurrence    | cookedAt       | https://weos.io/vocab/meal-planning#cookedAt       |
      | pantry             | isDefault      | https://weos.io/vocab/meal-planning#isDefault      |
      | food-item          | quantity       | https://weos.io/vocab/meal-planning#quantity       |
      | food-item          | unit           | https://weos.io/vocab/meal-planning#unit           |
      | food-item          | storage        | https://weos.io/vocab/meal-planning#storage        |
      | food-item          | expirationDate | https://weos.io/vocab/meal-planning#expirationDate |
      | shopping-list      | createdAt      | https://weos.io/vocab/meal-planning#createdAt      |
      | shopping-list-item | quantity       | https://weos.io/vocab/meal-planning#quantity       |
      | shopping-list-item | unit           | https://weos.io/vocab/meal-planning#unit           |
      | shopping-list-item | checked        | https://weos.io/vocab/meal-planning#checked        |

  # CONTRACT 1b. These three resolve, which is what makes them the dangerous
  # half: no 404 ever exposes them, and every read passes either way. They are
  # separated from the outline above because the FAULT is different — schema.org
  # is being made to say something about food that it says about medicine.
  @wip
  Scenario Outline: A property schema.org defines for another subject stops borrowing it
    Given a clean WeOS database
    When the operator installs the "meal-planning" preset
    Then the "<slug>" type resolves the property "<property>" to "<predicate>"
    And the "<slug>" type resolves nothing to "https://schema.org/<property>"

    Examples: the medical terms a meal-planning type was riding
      | slug              | property    | predicate                                       |
      | meal-occurrence   | status      | https://weos.io/vocab/meal-planning#status      |
      | shopping-list     | status      | https://weos.io/vocab/meal-planning#status      |
      | recipe-ingredient | preparation | https://weos.io/vocab/meal-planning#preparation |

  # CONTRACT 1c, and the case that proves the fix is "term it correctly" rather
  # than "move it to the house namespace". `https://schema.org/date` is a 404;
  # `schema:startDate` is the published term for the same thing and is what the
  # downstream twin already reads. A house `mp:date` would satisfy the outline
  # above and still be the wrong answer, so this is asserted on its own.
  @wip
  Scenario: A meal occurrence dates itself with the published start date, not a minted one
    Given a clean WeOS database
    And the operator installs the "meal-planning" preset
    When I create a "meal-occurrence" named "Taco Tuesday" with "date" set to "2026-09-01"
    Then the "meal-occurrence" type resolves the property "date" to "https://schema.org/startDate"
    And the triple store holds "https://schema.org/startDate" from the "meal-occurrence" "Taco Tuesday" with the value "2026-09-01"
    And the triple store holds no statement under "https://schema.org/date" about the "meal-occurrence" "Taco Tuesday"

  # CONTRACT 2 — the most important scenario in this file, and the only one that
  # fails if the fix over-corrects. `food-item.purchaseDate` is the sharpest row:
  # it sits on the same type as four mints and must not move with them. The rest
  # are the names a careless sweep would take on its way past — a whole
  # `Schedule`, a whole `NutritionInformation`, and the four names that appear
  # on nearly every type.
  @wip
  Scenario Outline: A genuine schema.org name keeps resolving to schema.org
    Given a clean WeOS database
    When the operator installs the "meal-planning" preset
    Then the "<slug>" type resolves the property "<property>" to "<predicate>"

    Examples: names schema.org really does define, in the sense meal-planning uses
      | slug                  | property         | predicate                            |
      | food-item             | purchaseDate     | https://schema.org/purchaseDate      |
      | meal-plan             | startDate        | https://schema.org/startDate         |
      | meal-plan             | endDate          | https://schema.org/endDate           |
      | scheduled-meal        | startTime        | https://schema.org/startTime         |
      | scheduled-meal        | repeatFrequency  | https://schema.org/repeatFrequency   |
      | scheduled-meal        | scheduleTimezone | https://schema.org/scheduleTimezone  |
      | nutrition-information | servingSize      | https://schema.org/servingSize       |
      | recipe                | recipeYield      | https://schema.org/recipeYield       |
      | how-to-step           | position         | https://schema.org/position          |
      | restricted-diet       | identifier       | https://schema.org/identifier        |
      | pantry                | name             | https://schema.org/name              |
      | pantry                | description      | https://schema.org/description       |

  # A term is only worth anything if a written value actually lands on it. The
  # projection and the API resolve through the type's own context, so they read
  # the same before and after and prove nothing on their own; the graph is where
  # the predicate is observable, which is why the statement assertions carry
  # this scenario and the read is the control beside them.
  @wip
  Scenario: A value written to a house property is stated on the house predicate
    Given a clean WeOS database
    And the operator installs the "meal-planning" preset
    And an "ingredient" named "Garlic" exists
    And a "pantry" named "Kitchen" exists
    When I create a "food-item" named "Garlic head" with "unit" set to "clove"
    Then the triple store holds "https://weos.io/vocab/meal-planning#unit" from the "food-item" "Garlic head" with the value "clove"
    And the triple store holds no statement under "https://schema.org/unit" about the "food-item" "Garlic head"
    And reading the "food-item" "Garlic head" back through the projection returns "unit" as "clove"
    And the API read of the "food-item" "Garlic head" returns "unit" as "clove"

  # ===========================================================================
  # THE GUARD — CONTRACT 4. The registry-wide sweep lives in `make test-unit`
  # (CONTRACT 6); these three pin its BEHAVIOUR on a running instance, because a
  # guard that passes by never looking at anything is the failure mode a green
  # sweep cannot distinguish itself from.
  # ===========================================================================

  @wip
  Scenario: No meal-planning property claims a term its vocabulary does not define
    Given a clean WeOS database
    When the operator installs the "meal-planning" preset
    Then no property of an installed meal-planning type resolves to a term its vocabulary does not define
    And no property of an installed meal-planning type resolves to a term a published vocabulary defines for another subject

  # The guard must BITE, and it must bite on the shape that caused this issue:
  # a house property added with no term, riding `@vocab` into somebody else's
  # namespace. This is the check the issue asks for — "the one that would have
  # caught this class at the point it was introduced" — so it is asserted by
  # introducing exactly that and watching the guard name it.
  @wip
  Scenario: The guard names a house property that rides @vocab into a published vocabulary
    Given a clean WeOS database
    And the "meal-planning" preset adds an untermed "spiciness" string property to "food-item"
    When the operator installs the "meal-planning" preset
    Then the vocabulary guard names "food-item" "spiciness" resolving to "https://schema.org/spiciness"
    And the vocabulary guard names no other property of an installed meal-planning type

  # The deny-list half bites too, on a name that resolves. Without this the
  # guard degrades to a 404 checker and `status` walks back in on the next type
  # somebody adds.
  @wip
  Scenario: The guard names a house property borrowing a term published for another subject
    Given a clean WeOS database
    And the "meal-planning" preset adds an untermed "status" string property to "pantry"
    When the operator installs the "meal-planning" preset
    Then the vocabulary guard names "pantry" "status" as a term "https://schema.org/" defines for another subject

  # ===========================================================================
  # AN EXISTING INSTALL — CONTRACT 8. The upgrade is silent, and the scenarios
  # below are what "silent" has to mean: no hold, no operator command, no read
  # that changes, and one documented thing that does not move on its own.
  # ===========================================================================

  @wip
  Scenario: The new terms reach an existing install without the operator doing anything
    Given a WeOS database provisioned by the build before the house properties were termed
    And the operator installs the "meal-planning" preset
    When the twin restarts on the build that terms the house properties
    Then the stored "food-item" context maps "unit" to "https://weos.io/vocab/meal-planning#unit"
    And the boot reconcile does not report the "unit" context term as held for "food-item"
    And the boot reconcile records no failure for "food-item"

  @wip
  Scenario: A value written before the terms existed still reads back after the upgrade
    Given a WeOS database provisioned by the build before the house properties were termed
    And the operator installs the "meal-planning" preset
    And an "ingredient" named "Garlic" exists
    And a "pantry" named "Kitchen" exists
    And I create a "food-item" named "Garlic head" with "unit" set to "clove"
    When the twin restarts on the build that terms the house properties
    Then reading the "food-item" "Garlic head" back through the projection returns "unit" as "clove"
    And the API read of the "food-item" "Garlic head" returns "unit" as "clove"

  # CONTRACT 8 and 8a, asserted rather than left to be discovered, and in one
  # scenario because neither half is safe to read without the other. The
  # predicate of an old literal is stamped into the resource's own embedded
  # context at write time and a reprojection replays it, so the graph disagrees
  # with the type's current context until the re-stamp runs — an operator
  # reading only the graph would conclude the upgrade did nothing. And the
  # re-stamp is not the end of it: the triple projection is upsert-only, so the
  # schema.org row survives beside the house one until the store is truncated
  # and rebuilt. A runbook that stops after the reproject leaves the twin
  # answering the same question under two predicates, which is why the
  # lingering row is asserted here rather than described in a comment.
  @wip
  Scenario: An old value keeps its write-time predicate in the graph until a re-stamp
    Given a WeOS database provisioned by the build before the house properties were termed
    And the operator installs the "meal-planning" preset
    And an "ingredient" named "Garlic" exists
    And a "pantry" named "Kitchen" exists
    And I create a "food-item" named "Garlic head" with "unit" set to "clove"
    And the twin restarts on the build that terms the house properties
    Then the stored document states "https://schema.org/unit" from the "food-item" "Garlic head" with the value "clove"
    When the operator re-stamps the stored documents and writes
    And the operator reprojects the event feed
    Then the stored document states "https://weos.io/vocab/meal-planning#unit" from the "food-item" "Garlic head" with the value "clove"
    And the stored document states no statement under "https://schema.org/unit" about the "food-item" "Garlic head"
    And the triple store holds "https://weos.io/vocab/meal-planning#unit" from the "food-item" "Garlic head" with the value "clove"
    But the triple store still holds "https://schema.org/unit" from the "food-item" "Garlic head" with the value "clove"
    When the operator truncates the graph store and rebuilds it
    Then the triple store holds no statement under "https://schema.org/unit" about the "food-item" "Garlic head"

  @wip
  Scenario: A value written after the upgrade lands on the house predicate
    Given a WeOS database provisioned by the build before the house properties were termed
    And the operator installs the "meal-planning" preset
    And an "ingredient" named "Garlic" exists
    And a "pantry" named "Kitchen" exists
    And the twin restarts on the build that terms the house properties
    When I create a "food-item" named "Lime wedge" with "unit" set to "each"
    Then the triple store holds "https://weos.io/vocab/meal-planning#unit" from the "food-item" "Lime wedge" with the value "each"
    And reading the "food-item" "Lime wedge" back through the projection returns "unit" as "each"
