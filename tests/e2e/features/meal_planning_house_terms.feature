@issue-535
Feature: A meal-planning house property states a predicate its vocabulary defines
  As an operator whose meal-planning graph is read by an LLM and reused by a second twin
  I want every house property to state a predicate the vocabulary it names actually defines
  So that a consumer resolving one of our predicates gets an answer instead of nothing

  # WHY THIS EXISTS. Every `meal-planning` type sets `"@vocab":"https://schema.org/"`.
  # A property with no term of its own rides that default, so `quantity` on a
  # shopping list item is written as `https://schema.org/quantity` — an assertion
  # that schema.org defines a term it does not. The IRI travels: anything reusing
  # the type inherits the claim, and `mini-me-weos` already reuses
  # `ShoppingListItem` and `MealOccurrence` so both graphs answer one query.
  #
  # `@vocab` pointing at a real vocabulary is the RIGHT default and stays. What
  # is missing is a correct term on each house property — for most of them
  # because they have no term at all, and for four of them because the term
  # they DO have points into the food ontology at a name it never defined
  # (CONTRACT 0). Both end the same way: a predicate that names a published
  # vocabulary the vocabulary itself will not confirm.
  #
  # ---------------------------------------------------------------------------
  # CONTRACT — what these scenarios pin, and the parts an implementer would
  # otherwise have to infer. Every IRI named below was resolved against
  # schema.org on 2026-08-26, not assumed; the property inventory was swept out
  # of `presets.NewDefaultRegistry()` on this branch, not read off the issue.
  #
  # 0. THE FAULT IS NOT "UNTERMED". Say this first, because it is the mistake
  #    two independent sweeps of this preset both made. The original framing —
  #    the issue's, and the first two audits' — was "a property with no term
  #    rides `@vocab` and mints a schema.org IRI". That describes the majority
  #    of the fault and hides the rest. FOUR meal-planning properties carry an
  #    EXPLICIT term and are wrong anyway, because the term points into the
  #    food ontology at names that ontology does not define:
  #
  #      recipe.recipeIngredient        fo:hasIngredient
  #      recipe-ingredient.ingredient   fo:ingredient
  #      ingredient.shoppingCategory    fo:ShoppingCategory
  #      ingredient.season              fo:at_its_best
  #
  #    Resolved on 2026-08-26: `http://purl.org/foodontology` (via purl.org,
  #    now redirecting to the ITMO University file that serves the namespace)
  #    defines 14 terms and none of those four — classes `Food`, `Ingredient`,
  #    `FoodAdditive`; properties `containsIngredient`, `containsGMO`,
  #    `ingredientsListAsText`, and the `energyPer100g` / `fatPer100g` /
  #    `proteinsPer100g` / `carbohydratesPer100g` families with their
  #    `AsDouble` variants. It uses `containsIngredient`, never
  #    `hasIngredient`. `fo:Food` IS defined, so `ingredient`'s `@type` is
  #    correct and stays.
  #
  #    THE RULE, THEREFORE: the guard reasons about a property's EFFECTIVE
  #    predicate IRI, whichever way the property arrives at it — an explicit
  #    term, a compact term through a declared prefix, or `@vocab`. An explicit
  #    term is not evidence of correctness; it only moves WHICH published
  #    vocabulary is being misquoted. Anything narrower than "effective IRI"
  #    reproduces the blind spot that hid these four.
  #
  #    THE FULL REPAIR SET IS 21 PROPERTIES: 17 reached through `@vocab` (14
  #    mints and 3 domain misuses) and the 4 explicit `fo:` terms above. Every
  #    one is named on its own line in the scenarios below rather than counted,
  #    so a missed one fails by name.
  #
  # 1. THREE DIFFERENT FAULTS WEAR THE SAME COSTUME, AND THEY NEED DIFFERENT
  #    FIXES. What is wrong differs, and so does the repair:
  #
  #    a. A MINT — the vocabulary the IRI lands in does not define the name.
  #       There is no published term to reach for, so the fix is a house term
  #       under `https://weos.io/vocab/meal-planning#`. Verified absent from
  #       schema.org: `quantity`, `unit`, `optional`, `storage`,
  #       `expirationDate`, `isDefault`, `createdAt`, `checked`, `cookedAt`,
  #       `date`. Verified absent from the food ontology: `ingredient`,
  #       `ShoppingCategory`, `at_its_best`. `ShoppingCategory` is doubly
  #       wrong — a Capitalised, class-shaped name used as a predicate — and
  #       is not defined under either reading.
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
  #    c. A NAME THE PUBLISHED VOCABULARY ALREADY HAS UNDER A DIFFERENT
  #       SPELLING. Two instances, and the fix is a PUBLISHED term, not a house
  #       one:
  #         `meal-occurrence.date` mints `https://schema.org/date` (404) when
  #         `https://schema.org/startDate` is the published term for exactly
  #         this, and a downstream consumer already reads it.
  #         `recipe.recipeIngredient` points at `fo:hasIngredient`, which the
  #         food ontology does not define, when `https://schema.org/recipeIngredient`
  #         is schema.org's own term for a recipe's ingredients — on the very
  #         type (`Recipe`) this is declared on.
  #       Minting `mp:date` or `mp:recipeIngredient` would satisfy every "no
  #       bad published IRI" assertion in this file and still be the wrong
  #       answer, which is why both get scenarios of their own rather than rows
  #       in the mint table.
  #
  #       `recipe-ingredient.ingredient` deliberately does NOT join them. There
  #       is no published term for "the ingredient this reified relation points
  #       at", and `mp:ingredient` is already what `shopping-list-item` uses for
  #       the same relation — so it is a 1a mint, and terming it that way makes
  #       one relation speak one predicate across both types.
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
  # 3. MOSTLY LITERALS, BUT TWO REFERENCES — SO THIS STORY REPOINTS EDGES.
  #    An earlier draft of this contract asserted that every repaired property
  #    was a literal and built two conclusions on it. That was true of the
  #    `@vocab` population and false once the four food-ontology terms of 0
  #    joined it: `recipe.recipeIngredient` and `recipe-ingredient.ingredient`
  #    both carry `x-resource-type`, so both are REFERENCES. The corrected
  #    position, and what each half changes:
  #
  #      - THE EXISTING GUARDS NOW GENUINELY APPLY TO THESE TWO.
  #        `presets.ContextGuardViolations`'s `referenceGuard` requires every
  #        reference property to reverse-map to its own name. Both keep an
  #        explicit term after the repair (`schema:recipeIngredient` and
  #        `mp:ingredient`), so both still reverse-map — but that is a claim to
  #        ASSERT, not to assume, because a repair that dropped a term instead
  #        of repointing it would leave the property riding `@vocab` and still
  #        pass every IRI assertion in this file. A scenario below asserts it.
  #
  #      - THE UPGRADE IS NO LONGER LITERAL-ONLY. An edge already written under
  #        `http://purl.org/foodontology#ingredient` is keyed in the stored
  #        document by PROPERTY NAME (#515), so the projection and the API keep
  #        reading it across the upgrade exactly as a literal does. What goes
  #        stale is the same thing that goes stale for a literal: the graph
  #        triple carries the predicate resolved at write time, and the
  #        document's embedded `@context` is what a reprojection replays. So
  #        CONTRACT 8 and 8a apply unchanged, to edges as well as literals —
  #        the re-stamp scenario below covers the mechanism for both, and the
  #        edge case is asserted beside the literal one rather than assumed to
  #        follow from it.
  #
  #      - THE COLLISION RISK IS STILL NOT OURS TO RESTATE. Two properties of
  #        one type collapsing onto one predicate is ALREADY swept
  #        registry-wide by `house_vocabulary_domain.feature`'s "Every
  #        reference property still reverse-maps to its own name after the
  #        move", whose second assertion is "no two properties of one installed
  #        type resolve to the same predicate IRI". It runs over every built-in
  #        preset and needs no amendment. This story makes it MORE load-bearing,
  #        not less: `mp:ingredient` is now declared on two types, so a careless
  #        edit that also moved `food-item.ingredient` (today `mp:isInstanceOf`)
  #        onto it would collide there.
  #
  #      - ONE READ-BACK IS DELIBERATELY NOT ASSERTED. `recipe.recipeIngredient`
  #        is an ARRAY reference, and array references are dropped on read today
  #        (`SimplifyJSONLD` / `extractEdgeColumns` unwrap a single map only,
  #        tracked separately as #513). Asserting a read-back on it would fail
  #        for a reason that has nothing to do with this story. So
  #        `recipeIngredient` is pinned at the TERM level only, and the edge
  #        read-back is asserted on `recipe-ingredient.ingredient`, which is a
  #        scalar reference and exercises the same path honestly.
  #
  # 4. HOW A TEST DECIDES "THE PUBLISHED VOCABULARY DOES NOT DEFINE THIS",
  #    OFFLINE. The gate must not touch the network: schema.org going down, or
  #    getting slow, must never redden CI, and a guard that only fires when the
  #    machine has DNS is not a guard. So the sweep carries its own answer:
  #
  #      - It reasons about a property's EFFECTIVE predicate IRI — what
  #        `jsonld.ResolvePredicateIRI` returns — regardless of whether the
  #        property got there through an explicit term, a compact term via a
  #        declared prefix, or `@vocab`. See 0: "untermed" is not the fault, and
  #        a guard scoped to untermed properties misses four of meal-planning's
  #        eighteen.
  #      - A set of PUBLISHED NAMESPACES it polices — `https://schema.org/`,
  #        SKOS, FOAF, W3C ORG, PROV-O, the food ontology, vCard, Activity
  #        Streams, GoodRelations. An IRI landing outside all of them is not
  #        this guard's business.
  #      - `https://weos.io/vocab/…` is EXEMPT. WeOS publishes it, so WeOS
  #        defines whatever it names. #520's guard already polices its shape.
  #      - Per policed namespace, a checked-in ALLOW-LIST of the local names the
  #        presets legitimately borrow, TAKEN FROM THE PUBLISHED DOCUMENT rather
  #        than from memory. One entry means one human resolved one IRI. An
  #        effective predicate landing in a policed namespace whose local name is
  #        absent from that namespace's list is a violation.
  #
  #        For the food ontology that is the whole vocabulary, because it is
  #        small enough to state: `Food`, `Ingredient`, `FoodAdditive`,
  #        `containsIngredient`, `containsGMO`, `ingredientsListAsText`,
  #        `energyPer100g`, `fatPer100g`, `proteinsPer100g`,
  #        `carbohydratesPer100g` and the four `…AsDouble` variants. After this
  #        story the presets borrow exactly one of them — `fo:Food`, as
  #        `ingredient`'s `@type`.
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
  #        undetected through two sweeps and would have again;
  #      - it never notices a published vocabulary deprecating or moving a term.
  #        That limit is not hypothetical for the food ontology: the document
  #        serving `http://purl.org/foodontology#` is version 0.0.9, dated
  #        2015, authored at ITMO University, and reached today only through two
  #        purl.org redirects. A namespace this thinly maintained is a reason to
  #        borrow from it sparingly — which, after this story, the presets do.
  #
  # 5. THE SWEEP IS REGISTRY-WIDE, AND MEAL-PLANNING IS NOT THE ONLY OFFENDER.
  #    Measured, not estimated: the official schema.org vocabulary
  #    (`schemaorg-current-https.jsonld`, 1521 rdf:Property entries) was
  #    cross-referenced against every untermed property of every type in
  #    `presets.NewDefaultRegistry()`. 164 properties ride `@vocab`; the large
  #    majority are fine. Beyond meal-planning's 21, the same rule reports 22:
  #
  #      core           person.avatarURL, organization.logoURL,
  #                     organization.slug                                  (3)
  #      knowledge      concept-scheme.title, concept-scheme.description   (2)
  #      notifications  notification.actionLabel, .actionUrl, .body,
  #                     .dedupeKey, .kind, .occurredAt, .read, .taskRef    (8)
  #      tasks          task.dueDate, task.priority (mints), plus
  #                     project.status and task.status (deny-list)         (4)
  #      website        web-page.slug, web-page.template,
  #                     web-page-element.content, web-page-template.slots,
  #                     web-page-template.templateBody                     (5)
  #                                                                    total 22
  #
  #    `notification.title` is NOT among them — schema.org defines `title`.
  #    An earlier draft of this contract listed it; it was removed on the
  #    measurement, and the count is computed from the rows above rather than
  #    carried forward from prose.
  #
  #    Two of those rows are the ones that prove the guard's shape. The
  #    `knowledge` pair rides a SKOS `@vocab` and mints `skos:title` /
  #    `skos:description`, which SKOS core defines neither of (both are Dublin
  #    Core) — a schema.org-only check reports the whole `knowledge` preset as
  #    clean. And `tasks`' two `status` properties are the SAME medical misuse
  #    meal-planning is being repaired for: class 1b, not mints, so they belong
  #    on the deny-list waivers, and a guard that waives only mints would let
  #    them pass while claiming to police the class.
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
  # 8. THE TWO POPULATIONS UPGRADE DIFFERENTLY, AND ONLY ONE OF THEM IS SILENT.
  #    An earlier draft claimed the whole upgrade needed no operator action.
  #    That is true of 17 of the 21 repairs and FALSE of the four `fo:` terms,
  #    which was established by driving `reconcileAdditiveContext`
  #    (`application/preset_context_reconcile.go:115`) with real before/after
  #    context pairs rather than by reading its doc comment. Both halves matter
  #    on Akeem's live instance, so both are stated:
  #
  #    POPULATION A — the 17 that had NO term and rode `@vocab`. MERGED
  #    SILENTLY. A term the preset declares and the stored context lacks is
  #    added; nothing diverges, so nothing is held. `Added: [quantity unit]`,
  #    `Conflicts: []`, `Changed: true`. No `held-terms`, no `adopt-term`,
  #    nothing to approve.
  #
  #      WHY `holdMovingTerms` DOES NOT CATCH THEM EITHER, which is what makes
  #      the silence correct rather than lucky: `livePredicates` collects the
  #      stored context's TERM NAMES, the schema's REFERENCE properties, and
  #      `@type`. Population A's properties are untermed LITERALS, so they are
  #      in none of the three and nothing holds them. Had any of the 17 been a
  #      reference, it WOULD have been held — so this claim is true of exactly
  #      this set and must not be generalised to "adding a term is always
  #      silent".
  #
  #    POPULATION B — the four `fo:` terms, whose definition CHANGES. HELD, not
  #    merged. A term present in both contexts with a different definition is a
  #    conflict, held at the stored definition, because overwriting it would
  #    repoint a predicate existing edges are already keyed by. For the two
  #    pure-B types the boot does nothing at all: `ingredient` reports
  #    `Conflicts: [season shoppingCategory]`, `Added: []`, `Changed: false`,
  #    and `recipe` the same for `recipeIngredient`. The stored contexts keep
  #    `fo:at_its_best`, `fo:ShoppingCategory` and `fo:hasIngredient`, and the
  #    boot reports them held on EVERY start until an operator adopts them.
  #
  #      THE HOLD IS THE SYSTEM WORKING. It is what #513 built and #518
  #      hardened, and nothing in this story should "fix" it. Without the hold,
  #      an upgrade would silently repoint `recipe-ingredient.ingredient` and
  #      orphan every edge already written under `fo:ingredient`. The cost of
  #      the hold is that four of the twenty-one repairs DO NOT TAKE EFFECT on
  #      an existing instance until someone runs `adopt-term` — which is a
  #      documentation obligation, not a defect.
  #
  #    ONE TYPE CAN BE BOTH. `recipe-ingredient` gains `quantity`, `unit`,
  #    `optional` and `preparation` (population A) while `ingredient` is held
  #    (population B), so the boot merges and holds on the same type in the
  #    same pass. That per-entry refusal is #520's "A held prefix does not block
  #    an additive change on the same type", now exercised on the real preset,
  #    and it has a scenario below.
  #
  #    A VESTIGIAL PREFIX IS EXPECTED, NOT A BUG — BUT THE ADOPTION LEAVES IT,
  #    NOT THE UPGRADE. Getting the order right matters, because the wrong
  #    version sends a reader hunting for a boot-time change that never happens.
  #    The repair removes the `fo` prefix from `recipe`'s PRESET context, and
  #    the merge rule preserves a term the STORED context has and the preset
  #    does not. But at boot `recipe` is a pure population-B type —
  #    `Added: []`, `Conflicts: [recipeIngredient]`, `Changed: false` — so
  #    `recipeIngredient` is still HELD at `fo:hasIngredient` and the `fo`
  #    prefix is still doing its job. The stored context is untouched entirely.
  #
  #    `fo` becomes vestigial only AFTER `adopt-term` moves `recipeIngredient`
  #    to `schema:recipeIngredient`. From then on the prefix is declared, resolves
  #    nothing, and is preserved on every later boot because the preset no
  #    longer declares it. That is the reconcile correctly refusing to delete
  #    what might be an operator's own term, and it is harmless — do not add a
  #    cleanup pass for it.
  #
  #    WHAT NEITHER POPULATION DOES is move the predicate on data already
  #    written, and for population A the silence is what makes that dangerous.
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
  # 8a0. A NAMING TRAP IN THE STEP VOCABULARY, RECORDED SO NOBODY OVER-READS
  #    THESE SCENARIOS. For a LITERAL, `the triple store holds "X" … with the
  #    value "V"` and `the stored document states "X" … with the value "V"` are
  #    the SAME assertion — both bind to `documentStatesLiteral`, because
  #    literals never reach the triples table and the stored document is the
  #    honest surface. So every literal assertion in this file, whichever
  #    phrasing it uses, is a claim about the stored document and NOT about a
  #    running graph store. Only the EDGE steps (`… to the "<slug>" "<name>"`)
  #    reach the `triples` table. This is pre-existing house vocabulary and not
  #    this story's to rename; it is written down because two scenarios in an
  #    earlier draft of this file contradicted themselves by not knowing it.
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
  # 8b. THE RUNBOOK, IN ORDER, AND WHY ADOPTION COMES FIRST. Every command
  #    exists on this branch; nothing here is new tooling.
  #
  #      weos resource-type held-terms meal-planning <slug>
  #      weos resource-type adopt-term meal-planning recipe --all
  #      weos resource-type adopt-term meal-planning recipe-ingredient --all
  #      weos resource-type adopt-term meal-planning ingredient --all
  #      weos worker normalize-edge-keys --restamp --write
  #      weos worker reproject
  #      weos worker checkpoint reset oxigraph --truncate
  #
  #    ADOPTION IS FIRST BECAUSE A RE-STAMP CANNOT SUBSTITUTE FOR IT, and an
  #    operator who runs the migration half alone will conclude it did not
  #    work. A re-stamp rewrites a stored document's embedded `@context` to
  #    match the TYPE's current stored context. While a term is held, the
  #    type's stored context still says `fo:ingredient` — so the re-stamp
  #    faithfully re-stamps the old IRI, the reproject replays it, and nothing
  #    moves. Adoption is what changes the type's stored context, which is what
  #    `--all` is REQUIRED, not decoration: `adopt-term` refuses with "name at
  #    least one --term, or pass --all to adopt every held term" when neither is
  #    given (internal/cli/resource_type_adopt.go). A runbook that omits it does
  #    not half-work, it errors on the first line.
  #
  #    every later step reads. The three `adopt-term` lines are the three types
  #    carrying population B; the other eleven meal-planning types need none,
  #    because population A merged at boot.
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
  # registry whose meal-planning type contexts are REVERTED to their pre-#535
  # shape — a transform over `PresetResourceType.Context`, not a second copy of
  # the presets, so it cannot drift. "The twin restarts on the build that terms
  # the house properties" then means: restart the same database against the
  # unmodified `presets.NewDefaultRegistry()`.
  #
  # "Reverted" is two operations, not one, and an implementer who reads it as
  # only the first will find the edge scenarios untestable. The `@vocab`
  # population had NO term before, so those are STRIPPED. The four `fo:`
  # properties of CONTRACT 0 had a WRONG term before, so those are RESTORED to
  # `fo:hasIngredient`, `fo:ingredient`, `fo:ShoppingCategory` and
  # `fo:at_its_best`, with the `fo` prefix put back on the `recipe` context it
  # is removed from. Only the second kind produces a stored edge under a
  # published IRI, which is what the edge upgrade scenario needs to exist at
  # all.
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
  #      c. RATCHET (what is pinned above). Repair meal-planning's 21, add the
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
  #
  # 4. DO THE FOUR `fo:` REPAIRS SHIP IN #535, OR MOVE TO THEIR OWN CHANGE?
  #    They are held on Akeem's live instance (CONTRACT 8), so shipping them
  #    here means this PR no longer has one clean story: 17 properties repair
  #    themselves at boot and 4 wait on three `adopt-term` commands. Splitting
  #    them out would keep #535's "no operator action" claim true of everything
  #    it contains.
  #
  #    The SDET recommendation is to SHIP AS IS, with CONTRACT 8b's runbook in
  #    the PR body. Three reasons. First, the four are the WORST of the
  #    twenty-one: an untermed property at least claims a term in a vocabulary
  #    the type already cites, while `fo:hasIngredient` cites a specific small
  #    ontology at a name it never had — deferring leaves the most clearly false
  #    statements in the graph the longest. Second, splitting does not avoid the
  #    adoption; it only moves it to a later date, and the later change carries
  #    the identical three commands with less context around them. Third, the
  #    guard ships in THIS story: with the four unrepaired, they are either
  #    guard violations on day one or they need meal-planning waivers, and
  #    CONTRACT 5 forbids a meal-planning waiver surviving this story. Deferring
  #    therefore forces a change to the guard's own rules, which is a worse
  #    outcome than three documented commands.
  #
  #    It is still Akeem's instance and his call. If he defers, the four
  #    `fo:` rows leave the mint outline and the 1c scenario, the population-B
  #    scenarios and CONTRACT 0 move with them, and CONTRACT 5 needs an
  #    explicit temporary meal-planning waiver with the follow-up issue named.
  # ---------------------------------------------------------------------------

  # ===========================================================================
  # A FRESH INSTALL — the terms themselves.
  # ===========================================================================

  # CONTRACT 1a. Sixteen properties across seven types, each of which claims a
  # name the vocabulary it lands in does not define. Named one per row rather
  # than counted, so a missed one fails on its own line instead of inside a
  # total. The two Examples blocks are the two vocabularies being misquoted —
  # kept apart because the first population rides `@vocab` and the second
  # arrives through an EXPLICIT `fo:` term, and a guard that only looks at the
  # first misses the second entirely (CONTRACT 0).
  Scenario Outline: A property its vocabulary does not define resolves to the house vocabulary
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

    Examples: the names the food ontology does not define, reached by an explicit fo: term
      | slug              | property         | predicate                                            |
      | recipe-ingredient | ingredient       | https://weos.io/vocab/meal-planning#ingredient       |
      | ingredient        | shoppingCategory | https://weos.io/vocab/meal-planning#shoppingCategory |
      | ingredient        | season           | https://weos.io/vocab/meal-planning#season           |

  # The food ontology keeps the one term it really does define. A sweep that
  # repaired the four `fo:` properties by deleting the prefix outright would
  # pass every row above and silently strip `ingredient`'s RDF class, which is
  # the type's whole identity in the graph — so the class is asserted here
  # rather than left to the reader's good faith. `season` is the reason the
  # house term is right and a schema.org borrow is not: `schema:season` is
  # about broadcast seasons, so taking it would be a fresh 1b misuse committed
  # while repairing one.
  Scenario: The ingredient type keeps the food ontology class the ontology does define
    Given a clean WeOS database
    And the operator installs the "meal-planning" preset
    When a "ingredient" resource is created
    Then that resource carries the RDF type "http://purl.org/foodontology#Food"
    And the "ingredient" type resolves nothing to "http://purl.org/foodontology#at_its_best"
    And the "ingredient" type resolves nothing to "http://purl.org/foodontology#ShoppingCategory"

  # CONTRACT 1b. These three resolve, which is what makes them the dangerous
  # half: no 404 ever exposes them, and every read passes either way. They are
  # separated from the outline above because the FAULT is different — schema.org
  # is being made to say something about food that it says about medicine.
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
  Scenario: A meal occurrence dates itself with the published start date, not a minted one
    Given a clean WeOS database
    And the operator installs the "meal-planning" preset
    When I create a "meal-occurrence" named "Taco Tuesday" with "date" set to "2026-09-01"
    Then the "meal-occurrence" type resolves the property "date" to "https://schema.org/startDate"
    And the triple store holds "https://schema.org/startDate" from the "meal-occurrence" "Taco Tuesday" with the value "2026-09-01"
    And the triple store holds no statement under "https://schema.org/date" about the "meal-occurrence" "Taco Tuesday"

  # CONTRACT 1c, second instance, and the one an "everything house-ward" sweep
  # gets wrong. `recipe` declares its RDF class as schema.org's `Recipe`, and
  # schema.org publishes `recipeIngredient` for exactly this on exactly that
  # class — so the term the preset was missing was never a house term. Pinned
  # at the TERM level only: `recipeIngredient` is an array reference, and array
  # references are dropped on read today (#513), so a read-back assertion here
  # would fail for a reason that has nothing to do with this story
  # (CONTRACT 3). The edge read-back lives in the scenario below, on a scalar.
  Scenario: A recipe names its ingredients with schema.org's own term
    Given a clean WeOS database
    When the operator installs the "meal-planning" preset
    Then the "recipe" type resolves the property "recipeIngredient" to "https://schema.org/recipeIngredient"
    And the "recipe" type resolves nothing to "http://purl.org/foodontology#hasIngredient"

  # CONTRACT 3's first half, asserted rather than assumed. Two of the repaired
  # properties are REFERENCES, so this story repoints edges and the existing
  # reference guard now genuinely applies to them. The failure this catches is
  # a repair that DROPS a term instead of repointing it: the property would
  # fall back to `@vocab`, every IRI assertion in this file would still pass,
  # and the edge would stop reverse-mapping to its own name. `mp:ingredient` is
  # also now declared on two types for one relation, which is the point of
  # choosing it — so the read is asserted on both.
  Scenario: A reference moved to the house vocabulary still keys and reads its edge
    Given a clean WeOS database
    And the operator installs the "meal-planning" preset
    And an "ingredient" named "Garlic" exists
    And a "recipe" named "Tacos" exists
    And a "shopping-list" named "Saturday" exists
    When I create a "recipe-ingredient" named "Two cloves" with "ingredient" referring to the "ingredient" "Garlic"
    And I create a "shopping-list-item" named "Two limes" with "ingredient" referring to the "ingredient" "Garlic"
    Then the triple store holds "https://weos.io/vocab/meal-planning#ingredient" from the "recipe-ingredient" "Two cloves" to the "ingredient" "Garlic"
    And reading the "recipe-ingredient" "Two cloves" back through the projection returns "ingredient" as the "ingredient" "Garlic"
    And the API read of the "shopping-list-item" "Two limes" returns "ingredient" as the "ingredient" "Garlic"
    And every reference property of every installed type reverse-maps to its own name

  # CONTRACT 2 — the most important scenario in this file, and the only one that
  # fails if the fix over-corrects. `food-item.purchaseDate` is the sharpest row:
  # it sits on the same type as four mints and must not move with them. The rest
  # are the names a careless sweep would take on its way past — a whole
  # `Schedule`, a whole `NutritionInformation`, and the four names that appear
  # on nearly every type.
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
  Scenario: The guard names a house property that rides @vocab into a published vocabulary
    Given a clean WeOS database
    And the "meal-planning" preset adds an untermed "spiciness" string property to "food-item"
    When the operator installs the "meal-planning" preset
    Then the vocabulary guard names "food-item" "spiciness" resolving to "https://schema.org/spiciness"
    And the vocabulary guard names no other property of an installed meal-planning type

  # The deny-list half bites too, on a name that resolves. Without this the
  # guard degrades to a 404 checker and `status` walks back in on the next type
  # somebody adds.
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

  # POPULATION A only — a property that had no term at all. The negative
  # assertion is the load-bearing one: `unit` must NOT be held, because nothing
  # about it diverges. Do not read this scenario as a claim about the whole
  # change; the four `fo:` terms behave the opposite way and are pinned below.
  Scenario: A term that never existed before reaches an existing install with no operator action
    Given a WeOS database provisioned by the build before the house properties were termed
    And the operator installs the "meal-planning" preset
    When the twin restarts on the build that terms the house properties
    Then the stored "food-item" context maps "unit" to "https://weos.io/vocab/meal-planning#unit"
    And the boot reconcile does not report the "unit" context term as held for "food-item"
    And the boot reconcile records no failure for "food-item"

  Scenario: A value written before the terms existed still reads back after the upgrade
    Given a WeOS database provisioned by the build before the house properties were termed
    And the operator installs the "meal-planning" preset
    And an "ingredient" named "Garlic" exists
    And a "pantry" named "Kitchen" exists
    And I create a "food-item" named "Garlic head" with "unit" set to "clove"
    When the twin restarts on the build that terms the house properties
    Then reading the "food-item" "Garlic head" back through the projection returns "unit" as "clove"
    And the API read of the "food-item" "Garlic head" returns "unit" as "clove"

  # CONTRACT 3's second half, and population B's whole point. The edge sibling
  # of the scenario above, asserted beside it rather than assumed to follow
  # from it, because the reason a literal survives and the reason an edge
  # survives are not the same reason. Note WHAT it is reading through: the
  # `ingredient` term is HELD at `fo:ingredient` across this boot (CONTRACT 8),
  # so this asserts that holding keeps the write working — which is the entire
  # justification for holding rather than overwriting. It is also the scenario
  # that fails if anyone "fixes" the repair by rewriting stored edge keys,
  # which nothing in this story should touch.
  Scenario: An edge written under the old ontology term still reads back after the upgrade
    Given a WeOS database provisioned by the build before the house properties were termed
    And the operator installs the "meal-planning" preset
    And an "ingredient" named "Garlic" exists
    And a "recipe" named "Tacos" exists
    And I create a "recipe-ingredient" named "Two cloves" with "ingredient" referring to the "ingredient" "Garlic"
    When the twin restarts on the build that terms the house properties
    Then reading the "recipe-ingredient" "Two cloves" back through the projection returns "ingredient" as the "ingredient" "Garlic"
    And the API read of the "recipe-ingredient" "Two cloves" returns "ingredient" as the "ingredient" "Garlic"
    And the JSON-LD representation of the "recipe-ingredient" "Two cloves" still carries an "ingredient" edge to the "ingredient" "Garlic"

  # CONTRACT 8, population B. `ingredient` is a PURE population-B type — its
  # only repairs are the two `fo:` terms — so the boot does nothing to it at
  # all: `Changed: false`, both terms held at the food-ontology IRIs they were
  # already stored at. This is the scenario that stops anyone reading "the
  # upgrade is silent" as "the upgrade is complete". That the hold repeats on
  # every start is not restated here; `house_vocabulary_domain.feature`'s "The
  # hold is reported on every boot, not only the first" already pins that
  # property on this same preset.
  Scenario Outline: A term moving out of the food ontology is held at its stored definition
    Given a WeOS database provisioned by the build before the house properties were termed
    And the operator installs the "meal-planning" preset
    When the twin restarts on the build that terms the house properties
    Then the boot reconcile reports the "<term>" context term as held for "<slug>"
    And the stored "<slug>" context still maps "<term>" to "<stored>"

    Examples:
      | slug              | term             | stored                                        |
      | ingredient        | season           | http://purl.org/foodontology#at_its_best      |
      | ingredient        | shoppingCategory | http://purl.org/foodontology#ShoppingCategory |
      | recipe            | recipeIngredient | http://purl.org/foodontology#hasIngredient    |
      | recipe-ingredient | ingredient       | http://purl.org/foodontology#ingredient       |

  # CONTRACT 8's "one type can be both". `recipe-ingredient` carries four
  # population-A properties and one population-B term, so the boot must merge
  # and hold in the same pass on the same type. A per-entry refusal that
  # degraded to per-type would either strand `quantity` and `unit` — silently
  # reintroducing the drop #510 closed — or overwrite `ingredient` and orphan
  # its edges. This is #520's "A held prefix does not block an additive change
  # on the same type", exercised on the real preset rather than a synthetic one.
  Scenario: A held ontology term does not block the new terms merging on the same type
    Given a WeOS database provisioned by the build before the house properties were termed
    And the operator installs the "meal-planning" preset
    When the twin restarts on the build that terms the house properties
    Then the boot reconcile reports the "ingredient" context term as held for "recipe-ingredient"
    And the stored "recipe-ingredient" context maps "quantity" to "https://weos.io/vocab/meal-planning#quantity"
    And the stored "recipe-ingredient" context maps "unit" to "https://weos.io/vocab/meal-planning#unit"
    And the stored "recipe-ingredient" context still maps "ingredient" to "http://purl.org/foodontology#ingredient"

  # The other end of the hold: what the operator does about it, and what it
  # costs. Adoption moves the stored term and records the old IRI as a
  # historical one, so the edges written under `fo:ingredient` stay readable
  # while new writes land on the house predicate. The alias is asserted because
  # it is the mechanism that makes adoption safe — without it this scenario is
  # indistinguishable from an overwrite that happened to be reprojected in time.
  # The `held-terms` listing and the class-move reporting are not restated here;
  # `house_vocabulary_domain.feature` pins both on this preset already.
  Scenario: Adopting the held term moves new writes without orphaning the old edges
    Given a WeOS database provisioned by the build before the house properties were termed
    And the operator installs the "meal-planning" preset
    And an "ingredient" named "Garlic" exists
    And a "recipe" named "Tacos" exists
    And I create a "recipe-ingredient" named "Two cloves" with "ingredient" referring to the "ingredient" "Garlic"
    And the twin restarts on the build that terms the house properties
    When the operator adopts the held "ingredient" context term for "meal-planning" "recipe-ingredient"
    Then the stored "recipe-ingredient" context maps "ingredient" to "https://weos.io/vocab/meal-planning#ingredient"
    And the stored "recipe-ingredient" context records "http://purl.org/foodontology#ingredient" as a historical IRI for "ingredient"
    And the JSON-LD representation of the "recipe-ingredient" "Two cloves" still carries an "ingredient" edge to the "ingredient" "Garlic"
    When I create a "recipe-ingredient" named "Four cloves" with "ingredient" referring to the "ingredient" "Garlic"
    Then the triple store holds "https://weos.io/vocab/meal-planning#ingredient" from the "recipe-ingredient" "Four cloves" to the "ingredient" "Garlic"
    And reading the "recipe-ingredient" "Two cloves" back through the projection returns "ingredient" as the "ingredient" "Garlic"

  # The boot has to go quiet once the operator has done the work, or the report
  # is noise that trains people to ignore it. Asserted on `ingredient` because
  # it is pure population B: after adopting both terms there is nothing left for
  # the boot to hold on that type, so a lingering report can only be a bug.
  Scenario: The boot stops reporting the type once its ontology terms are adopted
    Given a WeOS database provisioned by the build before the house properties were termed
    And the operator installs the "meal-planning" preset
    And the twin restarts on the build that terms the house properties
    When the operator adopts every held context term for "meal-planning" "ingredient"
    And the twin restarts on the build that terms the house properties again
    Then the boot reconcile does not report the "season" context term as held for "ingredient"
    And the boot reconcile does not report the "shoppingCategory" context term as held for "ingredient"
    And the boot reconcile records no failure for "ingredient"
    And the "ingredient" type resolves the property "season" to "https://weos.io/vocab/meal-planning#season"

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
  #
  # This is asserted on a LITERAL, and it covers the edge case too, because the
  # mechanism is identical — a re-stamp rewrites the document's embedded
  # `@context`, and every predicate in that document, literal or edge, resolves
  # through it. The edge half is not duplicated here because
  # `house_vocabulary_domain.feature`'s "A re-stamped edge takes its predicate
  # from the context the type has now" already pins exactly that end to end, on
  # this same preset. Extend that scenario if the mechanism ever changes; do
  # not grow this one to 20 steps restating it.
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
    # WHAT IS DELIBERATELY NOT ASSERTED HERE, and it is not because it does not
    # happen. The lingering Oxigraph row is real: the triple projection is
    # upsert-only, so after a re-stamp and a reproject the row under
    # `https://schema.org/unit` survives beside the house one, and only
    # `weos worker checkpoint reset oxigraph --truncate` clears it. That is why
    # CONTRACT 8b's runbook ends there.
    #
    # It cannot be asserted in THIS gate, for two reasons found by running it.
    # First, `config.Default()` sets no `OXIGRAPH_URL` and no `Oxigraph.Path`,
    # so the knowledge-graph store is the nop store and
    # `ProvideOxigraphGroup` returns no group — there is no store to truncate
    # and no row to observe. Second, and decisively, this world has no third
    # surface to observe it ON: `the triple store holds … with the value …` and
    # `the stored document states … with the value …` are bound to the SAME
    # function (`documentStatesLiteral`), because literals never reach the
    # triples table and the stored document is the honest surface. Asserting the
    # lingering row in this world would assert the exact opposite of the line
    # above it, about one surface.
    #
    # `house_vocabulary_domain.feature` CONTRACT 6a met this same wall and
    # settled it the same way — the truncate-and-rebuild stays in prose and the
    # gate asserts only what it can see. This story did not change that
    # mechanism and should not grow a second gate to restate it. Follow that
    # precedent if you are tempted to promote these lines into steps.

  Scenario: A value written after the upgrade lands on the house predicate
    Given a WeOS database provisioned by the build before the house properties were termed
    And the operator installs the "meal-planning" preset
    And an "ingredient" named "Garlic" exists
    And a "pantry" named "Kitchen" exists
    And the twin restarts on the build that terms the house properties
    When I create a "food-item" named "Lime wedge" with "unit" set to "each"
    Then the triple store holds "https://weos.io/vocab/meal-planning#unit" from the "food-item" "Lime wedge" with the value "each"
    And reading the "food-item" "Lime wedge" back through the projection returns "unit" as "each"
