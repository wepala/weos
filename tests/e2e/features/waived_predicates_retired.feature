@issue-537
Feature: Every preset property states a predicate its vocabulary defines, with no waiver left standing
  As an operator whose graph is read by an LLM and reused by a second twin
  I want the last twenty-three waived predicates repaired and the waiver list emptied
  So that the registry-wide guard #535 shipped is a gate rather than a ledger of known lies

  # WHY THIS EXISTS. #535 shipped a registry-wide guard in
  # `application/presets/published_vocabulary.go`. It reports any preset property
  # whose EFFECTIVE predicate lands in a published vocabulary that does not
  # define it (a MINT), or that borrows a term the vocabulary published about
  # another subject (a SUBJECT MISUSE). It ships as a RATCHET: the violation set
  # must EQUAL `vocabularyWaivers` exactly, so no new offender can be authored
  # anywhere, and repairing one fails until its waiver line is deleted.
  #
  # #535 repaired meal-planning and left 23 waivers across five presets. Each
  # reads "#537: <preset>" and none has an owner. While they stand, the guard is
  # a gate for new code and a ledger for old code, and the five presets keep
  # asserting that schema.org defines `dedupeKey`, that SKOS defines `title`,
  # and that a task's workflow state is the status of a medical study.
  #
  # This story deletes all 23 lines and leaves `vocabularyWaivers` empty.
  #
  # ---------------------------------------------------------------------------
  # CONTRACT — what these scenarios pin, and the parts an implementer would
  # otherwise have to infer. Every IRI below was RESOLVED, on 2026-08-26,
  # against the published document rather than assumed:
  # `schemaorg-current-https.jsonld` (schema.org's own file, 1521 rdf:Property
  # entries), the DCMI Terms RDF at dublincore.org, and the SKOS Core list
  # already curated in `published_vocabulary.go`. The property inventory was
  # swept out of `presets.NewDefaultRegistry()` on this branch by running
  # `presets.PublishedVocabularyViolations` and
  # `presets.ResolvedPredicateFor` over every type — not read off the issue.
  #
  # 0. DO NOT ASSUME A 404, AND DO NOT ASSUME A HOUSE TERM. Those are the two
  #    ways this story goes wrong, and they pull in opposite directions.
  #
  #    #535 learned the first the hard way: `preparation`, `status` and `title`
  #    all RESOLVE, and all three were still wrong. A name-only sweep reports
  #    them clean.
  #
  #    The second is the risk unique to THIS story, because 23 waivers in a row
  #    invite a sweep that moves all 23 into house namespaces. Seven of them
  #    must not go house — the published vocabulary already has the right term
  #    under a different spelling, and taking it is the whole point of the
  #    ontology strategy. Moving those seven house breaks NOTHING any test would
  #    notice, because every read resolves through the type's own context either
  #    way, while quietly draining the structured-data and grounded-reasoning
  #    payoff away. That is why every one of the seven is named on its own line
  #    below and asserted by IRI.
  #
  # 1. THE THREE CASES, AND THE ONE THE ISSUE'S TAXONOMY DOES NOT HAVE A BOX
  #    FOR. The issue states three:
  #
  #      case 1  the vocabulary defines nothing            -> mint a house term
  #      case 2  the vocabulary defines it, right sense    -> allow-list the row
  #      case 3  the vocabulary defines it for something   -> house term, AND
  #              else                                         a deny-list note
  #
  #    A FOURTH SHAPE OCCURS HERE AND MUST NOT BE FORCED INTO CASE 1 OR 3: the
  #    vocabulary does not define THIS NAME, or defines it for something else,
  #    but publishes the right term under a DIFFERENT SPELLING. The repair is a
  #    published term, not a house one. #535 met this and called it CONTRACT 1c
  #    (`meal-occurrence.date` -> `schema:startDate`). It applies to SEVEN of the
  #    23 here, and to one of them — `notification.title` — on top of a case-3
  #    fault, which is the combination no reading of the issue predicts:
  #    `schema:title` is published for JobPosting (so the deny-list row stands),
  #    AND `schema:name` is the right published term for a notification's title
  #    (so the repair is a rename, not a mint).
  #
  #    Call the shape RENAME-BY-TERM throughout: the JSON property name is
  #    unchanged — renaming a property is a breaking API and projection change
  #    and is out of scope — and only the `@context` entry moves. This repo
  #    already does it in seven places (`food-item.notes` -> `schema:description`,
  #    `note.content` -> `schema:text`, `task.project` -> `schema:isPartOf`,
  #    `nutrition-information.ingredient` -> `schema:about`, and #535's three),
  #    so the divergence between a property name and its predicate's local name
  #    is established house practice, not a novelty this story introduces.
  #
  # 2. THE FULL DECISION TABLE — all 23, each with the IRI it states today, the
  #    case, and the IRI it must state after. Nothing here is a count; a missed
  #    row fails by name.
  #
  #    RENAME-BY-TERM (7). The published vocabulary already has it:
  #      person.avatarURL          schema:avatarURL     -> schema:image
  #      organization.logoURL      schema:logoURL       -> schema:logo
  #      notification.title        schema:title (misuse)-> schema:name
  #      notification.body         schema:body          -> schema:text
  #      web-page-element.content  schema:content       -> schema:text
  #      concept-scheme.title      skos:title           -> dcterms:title
  #      concept-scheme.description skos:description    -> dcterms:description
  #
  #    HOUSE MINT, case 1 (14). The vocabulary answers nothing:
  #      organization.slug              -> core:slug
  #      notification.kind              -> notif:kind
  #      notification.actionUrl         -> notif:actionUrl
  #      notification.actionLabel       -> notif:actionLabel
  #      notification.taskRef           -> notif:taskRef
  #      notification.occurredAt        -> notif:occurredAt
  #      notification.read              -> notif:read
  #      notification.dedupeKey         -> notif:dedupeKey
  #      task.dueDate                   -> task:dueDate
  #      task.priority                  -> task:priority
  #      web-page.slug                  -> web:slug
  #      web-page.template              -> web:template
  #      web-page-template.slots        -> web:slots
  #      web-page-template.templateBody -> web:templateBody
  #
  #    HOUSE MINT, case 3 (2). The name resolves, published for something else:
  #      task.status                    -> task:status
  #      project.status                 -> task:status
  #
  #    7 + 14 + 2 = 23, which is `len(vocabularyWaivers)` on this branch.
  #
  # 2a. THE REASONING BEHIND EACH JUDGEMENT CALL, because "resolve it yourself"
  #    is not a decision and the next reader will re-litigate whatever is left
  #    unwritten. Only the rows a reasonable implementer would get wrong are
  #    argued; the seven notification mints and the four website mints have no
  #    published candidate at all and need no defence.
  #
  #    person.avatarURL -> schema:image. RESOLVED: `schema:image` is "An image
  #    of the item", domain Thing, range ImageObject or URL. A Person is a
  #    Thing; an avatar IS an image of the person; the stored value is a URL,
  #    which is one of the published ranges. WeOS's person type has exactly one
  #    image property and no narrower sense of "avatar" to preserve, so there is
  #    nothing a house term would say that `schema:image` does not. Case 2/1c.
  #
  #    organization.logoURL -> schema:logo. RESOLVED: `schema:logo` is "An
  #    associated logo", and Organization is IN its domainIncludes — this is the
  #    rare row where even the advisory domain agrees. Range ImageObject or URL.
  #    Case 2/1c.
  #
  #    NOTE, AND IT IS A CORRECTION TO THE ISSUE'S FRAMING: `image` is already in
  #    the schema.org allow-list and already resolved by `product`, `recipe`,
  #    `cookbook`, `ingredient` and `how-to-step`, so adopting it adds no row.
  #    `logo` IS NOT IN THE ALLOW-LIST. Verified by running the guard over the
  #    proposed contexts: `organization: "logoURL" states a term the vocabulary
  #    does not define (https://schema.org/logo)`. So this repair REQUIRES a new
  #    allow-list row, and adding that row IS the verification step. An
  #    implementer who assumes `logo` is already there will ship a red build and
  #    conclude the rename was wrong.
  #
  #    notification.title -> schema:name. `schema:title` is "The title of the
  #    job", published for JobPosting, and is already on the deny-list. The
  #    question is what replaces it. RESOLVED: `schema:name` is "The name of the
  #    item", domain Thing, range Text. A notification's title is its short
  #    human-readable label, which is what `name` means, and the notification
  #    type declares NO `name` property, so nothing collides. Every other WeOS
  #    type labels itself with `schema:name`; a house `notif:title` would make
  #    notifications the one type an LLM cannot label without a special case.
  #    Case 3 fault, RENAME-BY-TERM repair — the shape of CONTRACT 1.
  #
  #    notification.body -> schema:text. RESOLVED: `schema:text` is "The textual
  #    content of this CreativeWork", domain CreativeWork. The notification type
  #    declares `@type: Message`, and schema.org's Message IS a CreativeWork, so
  #    this is not a domain stretch — it is the published term for exactly this,
  #    on the class the type already claims. `memory/note.content` already
  #    resolves to `schema:text` in this repo for the same reason. Case 1c.
  #
  #    web-page-element.content -> schema:text. Same term, same reasoning:
  #    WebPageElement is a CreativeWork and the property holds the block's
  #    textual content. This is the sharpest anti-over-correction row in
  #    `website` and it sits on the same type as `cssSelector`, which must not
  #    move either.
  #
  #    concept-scheme.title / .description -> dcterms. RESOLVED: SKOS Core
  #    publishes NEITHER; both are Dublin Core terms. `dcterms:title` is "A name
  #    given to the resource" and `dcterms:description` is "An account of the
  #    resource", both rdf:Property, both range rdfs:Literal, and dct: is the
  #    convention the SKOS Primer itself uses to title a ConceptScheme. Two
  #    in-vocabulary alternatives were resolved and REJECTED: `skos:prefLabel`
  #    is the lexical label whose SKOS integrity conditions are about labelling
  #    Concepts, and `skos:definition` is "a complete explanation of the intended
  #    meaning of a CONCEPT" — using it for a scheme's blurb would be a fresh
  #    subject misuse committed while repairing one, which is the `schema:season`
  #    trap #535 named. Case 2/1c.
  #
  #    task.status and project.status -> task:status. `schema:status` is the
  #    status of a MedicalCondition / MedicalProcedure / MedicalStudy and is
  #    already on the deny-list. RESOLVED AND REJECTED: `schema:actionStatus`
  #    looks tempting because `task` declares `@type: Action` — but its range is
  #    ActionStatusType, a closed enumeration of four members, and WeOS stores a
  #    free-form string ("todo", "blocked", "done"). Asserting
  #    `schema:actionStatus "blocked"` claims a member of that enumeration which
  #    does not exist, so it trades one misuse for another. `project` declares
  #    `@type: Project`, which is not an Action at all, so the term could not
  #    cover both types even if the range fitted. House term, ONE IRI declared on
  #    BOTH types, mirroring `mp:status` on `meal-occurrence` and
  #    `shopping-list` — one concept, one predicate. Case 3.
  #
  #    task.dueDate -> task:dueDate. RESOLVED: schema.org defines no `dueDate`.
  #    `schema:endTime` (Action) is when the action ENDED or is expected to end,
  #    not a deadline; `schema:scheduledTime` is published for PlanAction only
  #    and means "the time the object is scheduled to", which is a plan, not a
  #    due date. Neither says "deadline". Case 1.
  #
  #    task.priority -> task:priority. schema.org defines no `priority` and no
  #    near neighbour. Case 1.
  #
  #    notification.occurredAt -> notif:occurredAt. RESOLVED AND REJECTED:
  #    `schema:dateSent` (Message, "the date/time at which the message was
  #    sent") is the obvious grab and is WRONG. `NotificationInput.OccurredAt`
  #    is supplied by the PRODUCING service from a domain event — it is when the
  #    underlying signal happened, and a producer may backdate it — while the
  #    notification row is created at Notify time. Two different instants.
  #    Case 1.
  #
  #    notification.read -> notif:read. RESOLVED AND REJECTED: `schema:dateRead`
  #    exists on Message, but it is a Date/DateTime and WeOS stores a BOOLEAN.
  #    Wrong range and wrong shape. Case 1.
  #
  #    notification.taskRef -> notif:taskRef. RESOLVED AND REJECTED:
  #    `schema:about` ("The subject matter of an object", range Thing) is the
  #    closest published term and is already allow-listed. It is rejected because
  #    `taskRef` is deliberately an OPAQUE STRING — the preset's own comment says
  #    it "stays a plain string so the store never couples to any particular
  #    consumer's types" — and it carries no `x-resource-type`, so it is never a
  #    node in the graph. Asserting a Thing-ranged predicate over a bare literal
  #    is a range claim the data does not support. Recorded as OPEN QUESTION 2.
  #
  #    notification.actionUrl / .actionLabel -> house. RESOLVED: schema.org
  #    models a call to action as `schema:potentialAction` pointing at an Action
  #    with its own `target` and `name`. That is a different data shape, not a
  #    different predicate, and re-modelling the notification type is out of
  #    scope. `schema:url` is the URL OF THE ITEM, not of an action it offers.
  #    Case 1 for both.
  #
  #    notification.kind / .dedupeKey -> house. `kind` is a routing key
  #    ("import.completed"), not a human category; `schema:category` is published
  #    for Offer/Product/Service and friends and means a browsable category.
  #    `dedupeKey` is an idempotency key for the SIGNAL, not an identifier of the
  #    notification, which already has a KSUID URN. Case 1 for both.
  #
  #    web-page.template -> web:template. RESOLVED AND REJECTED:
  #    `schema:isBasedOn` ("A resource from which this work is derived",
  #    CreativeWork) reads plausibly, but the stored value is a template NAME
  #    the renderer looks up, the property carries no `x-resource-type`, and
  #    `isBasedOn` asserts derivation from a work. Case 1. This is the
  #    lowest-confidence mint in the table; see OPEN QUESTION 3.
  #
  #    web-page-template.templateBody -> web:templateBody. RESOLVED AND
  #    REJECTED: `schema:text` — the same term adopted twice above — because a
  #    template body is HTML markup carrying `data-weos-*` slot annotations, not
  #    the work's textual content. Both `web-page-element.content` and this
  #    property mapping to `schema:text` would mean a consumer reading
  #    `schema:text` off a WebPage-typed thing gets raw template machinery. It
  #    sits beside `slots`, which is unambiguously house. Case 1, and see OPEN
  #    QUESTION 3.
  #
  #    web-page.slug and organization.slug -> house, in their OWN namespaces.
  #    RESOLVED AND REJECTED: `schema:identifier` is "any kind of identifier for
  #    any kind of Thing" and would claim the slug is THE identifier, competing
  #    with the `urn:<typeSlug>:<ksuid>` identity every resource already has. A
  #    slug is a routing segment. Both mint. WHY TWO IRIs FOR ONE CONCEPT is
  #    OPEN QUESTION 1.
  #
  # 3. THE NAMES THAT MUST NOT MOVE. Swept out of the five presets rather than
  #    guessed, and asserted below by IRI because nothing else in the suite would
  #    notice one of them drifting into a house namespace.
  #
  #    The one the issue calls out: `web-page-element.cssSelector`. schema.org
  #    really does define `cssSelector`, and its domainIncludes is literally
  #    `WebPageElement` — our type. It sits on the same type as `content`, which
  #    DOES move. One moves, one stays, on one type.
  #
  #    The rest, per preset, all currently resolving correctly and all of which a
  #    careless sweep would take on its way past:
  #      core           person.givenName, .familyName, .name, .email;
  #                     organization.name, .description, .url; and both RDF
  #                     classes, foaf:Person and org:Organization
  #      knowledge      concept.prefLabel, .altLabel, .definition;
  #                     collection.prefLabel, .member — all SKOS, and the
  #                     evidence that the concept-scheme pair is a real fault
  #                     rather than SKOS being the wrong @vocab
  #      notifications  notification.recipient (schema:recipient IS published
  #                     for Message) and the Message class itself
  #      tasks          project.name, .description; task.name, .description;
  #                     and task.project, which already carries an EXPLICIT
  #                     `schema:isPartOf` term and is the only reference property
  #                     in any of the five presets
  #      website        web-site.name, .url, .description, .inLanguage;
  #                     web-page.name, .description; web-page-element.name;
  #                     web-page-template.name; theme.name, .version,
  #                     .thumbnailUrl; article and blog-post .headline,
  #                     .articleBody, .author, .datePublished; faq.name,
  #                     .mainEntity; breadcrumb-list.name, .itemListElement
  #
  # 4. THE HOUSE VOCABULARIES, NAMED, WITH THEIR EXACT IRIs. `pkg/jsonld/vocab.go`
  #    today defines only `HouseVocabBase`, `MealPlanningVocab`, `MemoryVocab`
  #    and `AgentsVocab`. Four constants are added, built from `HouseVocabBase`
  #    and never from a literal — #520 was the story that had to move the domain,
  #    and a literal here reintroduces the problem it just finished solving:
  #
  #      CoreVocab          = HouseVocabBase + "core#"
  #      NotificationsVocab = HouseVocabBase + "notifications#"
  #      TasksVocab         = HouseVocabBase + "tasks#"
  #      WebsiteVocab       = HouseVocabBase + "website#"
  #
  #    resolving to https://weos.io/vocab/core# , /notifications# , /tasks# and
  #    /website# respectively. The naming follows the existing constants: the
  #    namespace segment is the PRESET NAME.
  #
  #    THERE IS NO `KnowledgeVocab`, AND ADDING ONE IS A BUG. Both knowledge
  #    repairs are published Dublin Core terms, so that preset mints nothing.
  #    An implementer working through the five presets in order will reflexively
  #    add a fifth constant and then have nothing to point at it.
  #
  #    The context prefixes are `core`, `notif`, `task`, `web` and `dct`. None
  #    collides with a property name or an existing prefix on the types that
  #    declare them (checked: `person` declares `foaf`, `organization` declares
  #    `org`, the other three declare none).
  #
  # 5. THE GUARD'S OWN LISTS CHANGE, AND TWO OF THE CHANGES ARE THE REPAIR
  #    RATHER THAN A SIDE EFFECT.
  #
  #    a. `policedVocabularies["https://schema.org/"]` GAINS EXACTLY ONE NAME:
  #       `logo`. Every other published term this story adopts — `image`,
  #       `name`, `text` — is already listed and already used.
  #
  #    b. `http://purl.org/dc/terms/` BECOMES A NEWLY POLICED NAMESPACE, with
  #       its own allow-list holding `title` and `description`. THIS IS NOT
  #       OPTIONAL BOOKKEEPING. The guard "does not police a namespace absent
  #       from policedVocabularies", so without this row the concept-scheme pair
  #       simply stops being looked at and the guard reports the knowledge preset
  #       clean by never asking. Verified by running the guard over the proposed
  #       contexts: with dcterms unpoliced, the two properties produce NO
  #       violation and NO report — a silent pass, indistinguishable from a
  #       repair. Add the namespace, or the repair is cosmetic.
  #
  #       Use `http://purl.org/dc/terms/` and NOT the legacy
  #       `http://purl.org/dc/elements/1.1/`. Both publish `title` and
  #       `description`; dcterms is the maintained one and dct:title is a
  #       subPropertyOf the elements form. The elements namespace is deliberately
  #       NOT policed, because nothing uses it — which means a slip into it would
  #       be unpoliced, so the scenario below pins the terms IRI exactly rather
  #       than pinning "somewhere in Dublin Core".
  #
  #    c. `termsPublishedForAnotherSubject` GAINS NOTHING. All three subject
  #       misuses here — `notification.title`, `task.status`, `project.status` —
  #       already have their names on that list (`title` and `status`), put there
  #       by #535.
  #
  #       AND NEITHER ROW MAY BE DELETED when the waivers go. After this story
  #       no preset resolves `schema:title` or `schema:status` at all, and the
  #       list will therefore look dead. It is not: it is what makes the NEXT
  #       type that adds an untermed `status` fail on the day it is authored,
  #       which is the entire reason the deny-list exists rather than an
  #       allow-list. `preparation` has been sitting there unused since #535 for
  #       exactly this reason, and no test prunes it —
  #       `UnusedAllowListEntries` sweeps `policedVocabularies` only.
  #
  #    d. THE ANTI-RUBBER-STAMP RULE STILL APPLIES TO WHAT IS ADDED.
  #       `TestPresets_EveryAllowListedTermIsStillUsed` fails on any listed name
  #       no type resolves, so the three new rows (`logo`, `dcterms:title`,
  #       `dcterms:description`) are only allowed to exist because the repair
  #       makes them used. Adding a speculative row is a red build, by design.
  #
  # 6. WHERE EACH ASSERTION BELONGS, following the precedent #522 set and #535
  #    kept. The registry sweep is a pure function of the registry — no database,
  #    no boot — so it lives beside the existing sweeps in
  #    `application/presets/published_vocabulary_test.go`, where it runs in
  #    `make test-unit` on every change rather than only in the e2e job. The
  #    scenarios in THIS file pin what is observable on a RUNNING instance.
  #
  #    The unit-level work, stated so it is not left to inference:
  #
  #      EXISTING, and they must pass WITH NO WAIVERS AT ALL:
  #        TestPresets_NoPropertyClaimsAnUndefinedPublishedTerm
  #        TestPresets_NoPropertyClaimsAPublishedTermForAnotherSubject
  #        TestPresets_NoWaiverOutlivesItsViolation
  #          This last one is what makes `vocabularyWaivers = map[string]string{}`
  #          load-bearing rather than decorative: it fails on any line naming
  #          nothing. Do not delete it along with the waivers it policed.
  #        TestPresets_EveryAllowListedTermIsStillUsed  (see 5d)
  #
  #      NEW, and each one exists because something above would otherwise be
  #      unverified:
  #        TestPresets_TheRepairedWaivedPredicates
  #          The sibling of #535's TestPresets_TheRepairedMealPlanningPredicates,
  #          in exactly its shape: a want-map of slug -> property -> IRI covering
  #          all 23 repairs AND the must-not-move list of CONTRACT 3, checking
  #          that the property still EXISTS before checking where it points.
  #          That existence check is not optional — `ResolvedPredicateFor`
  #          resolves any name at all through `@vocab`, so without it every pin
  #          would pass identically against a type whose schema had been emptied.
  #        TestPresets_TheGuardPolicesDublinCore
  #          A probe type minting an undefined name in the dcterms namespace,
  #          reported as FaultUndefinedTerm. Without it, "dcterms is policed" is
  #          a claim about a map literal rather than about behaviour, and the
  #          silent pass of 5b would come back the first time somebody edits the
  #          namespace string.
  #
  # 7. DECLARE EACH TERM ON THE TYPES THAT ACTUALLY HAVE THE PROPERTY. None of
  #    these five presets has a shared context builder like meal-planning's
  #    `mpContext`, so the exact trap #535 named cannot be sprung here — but the
  #    equivalent one can: adding the house prefix and its terms to EVERY type of
  #    a preset. `article`, `blog-post`, `theme`, `faq`, `breadcrumb-list` and
  #    `web-site` have no slug, no template, no slots and no templateBody, and
  #    `concept` and `collection` have no dct: term. A context that declares them
  #    anyway is inert today and lies about the type, which is how a future
  #    collision gets built. A scenario below asserts the negative.
  #
  # 8. THE UPGRADE. THIS IS NOT #535'S UPGRADE, AND THE DIFFERENCE IS THE WHOLE
  #    OPERATIONAL STORY. #535 had two populations: 17 untermed properties that
  #    merged silently, and four carrying a WRONG explicit `fo:` term whose
  #    definition CHANGED, which the boot HELD and which needed three
  #    `adopt-term` commands.
  #
  #    #537 HAS NO SECOND POPULATION. Every one of the 23 rides `@vocab` today
  #    with no term of its own, and none of them is a reference property, so
  #    none is in `livePredicates` and nothing holds them. Established by driving
  #    `reconcileAdditiveContext` (`application/preset_context_reconcile.go`)
  #    with the real before/after context pairs rather than by reading its doc
  #    comment — the same method #535 used, and the results:
  #
  #      person             Added=[avatarURL]                        Conflicts=[] Changed=true
  #      organization       Added=[logoURL slug]                     Conflicts=[] Changed=true
  #      concept-scheme     Added=[dct description title]            Conflicts=[] Changed=true
  #      notification       Added=[actionLabel actionUrl body dedupeKey
  #                                kind notif occurredAt read
  #                                taskRef title]                    Conflicts=[] Changed=true
  #      task               Added=[dueDate priority status tasks]    Conflicts=[] Changed=true
  #      web-page           Added=[slug template web]                Conflicts=[] Changed=true
  #      web-page-element   Added=[content]                          Conflicts=[] Changed=true
  #      web-page-template  Added=[slots templateBody web]           Conflicts=[] Changed=true
  #
  #    So: NO held terms, NO `adopt-term`, NO `held-terms` listing, nothing for
  #    an operator to approve. The runbook in 8b has no adoption step, and a
  #    reviewer who expects one by analogy with #535 should read this paragraph
  #    rather than adding it.
  #
  #    THE ONE WAY A #537 TERM IS HELD is an operator who had already mapped one
  #    of these names by hand in a stored context. Then the definitions diverge
  #    and the merge holds at the stored one: `Conflicts=[title]`, `Added=[]`,
  #    `Changed=false` — measured, not assumed. That is the reconcile working,
  #    it is the only population-B path in this story, and it has a scenario
  #    below. The adoption MECHANISM is already covered end to end by
  #    `context_term_adoption.feature`; the scenario here pins only that #537's
  #    own terms take that path, and stops there.
  #
  # 8a. SILENT IS NOT THE SAME AS COMPLETE, AND HERE IT MATTERS MORE THAN IT DID
  #    FOR #535. Reads are correct the moment the boot merges the terms — the
  #    projection and the API resolve through the type's own context — but the
  #    GRAPH carries the predicate resolved AT WRITE TIME, stamped into the
  #    resource's own embedded `@context` by `BuildResourceGraph`, and
  #    `worker reproject` replays that payload rather than re-deriving it. An old
  #    notification's embedded context has no `body` term, so `body` still rides
  #    `@vocab` to `https://schema.org/body` on every replay.
  #
  #    WHY IT IS BIGGER HERE: `core` and `notifications` are both
  #    `AutoInstall: true`. Every WeOS instance in existence has person,
  #    organization and notification rows, so this migration is universal rather
  #    than opt-in the way meal-planning's was. `tasks`, `website` and
  #    `knowledge` are opt-in and only affect instances that installed them.
  #
  #    The lingering-row half is unchanged from #520 CONTRACT 6 and #535
  #    CONTRACT 8a: the triple projection is UPSERT-ONLY, so a re-stamp plus a
  #    reproject writes the new predicate and the row under the old one survives
  #    beside it until the store is truncated and rebuilt. That is why 8b ends
  #    where it does.
  #
  # 8a0. THE NAMING TRAP IN THE STEP VOCABULARY, restated because it is
  #    pre-existing and two scenarios in an earlier draft of #535's file
  #    contradicted themselves by not knowing it. For a LITERAL,
  #    `the triple store holds "X" … with the value "V"` and
  #    `the stored document states "X" … with the value "V"` bind to the SAME
  #    function (`documentStatesLiteral`), because literals never reach the
  #    triples table and the stored document is the honest surface. Every literal
  #    assertion in this file is therefore a claim about the stored document.
  #    Only the EDGE steps reach the `triples` table, and this story repairs no
  #    reference property, so no scenario here uses one.
  #
  # 8b. THE RUNBOOK, IN ORDER. Every command exists on this branch; nothing here
  #    is new tooling, and there is no adoption step (see 8):
  #
  #      weos worker normalize-edge-keys --restamp --write
  #      weos worker reproject
  #      weos worker checkpoint reset oxigraph --truncate
  #
  #    Whether #537 ships this in the PR body or defers it is OPEN QUESTION 4.
  #    The live twin runs on GCP against real data, so whatever the runbook says
  #    is what somebody actually executes.
  #
  # 9. WHAT IS ALREADY COVERED ELSEWHERE AND IS DELIBERATELY NOT RE-AUTHORED
  #    HERE. Reconciled by intent against the existing suite before writing:
  #
  #    - THE GUARD BITING. `meal_planning_house_terms.feature` already pins both
  #      halves ("The guard names a house property that rides @vocab into a
  #      published vocabulary" and "…borrowing a term published for another
  #      subject"). This story changes the guard's LISTS, not its mechanism, so
  #      only the dcterms half is new, and it belongs at the unit level (6).
  #      One bite scenario is kept below for a different reason: to prove the
  #      guard still bites once the waiver map is EMPTY, which is a state that
  #      has never existed before.
  #    - THE ADOPTION MECHANISM. `context_term_adoption.feature` covers add vs
  #      rename, `--all`, aliases, double adoption, and edges written under a
  #      retired IRI, over 24 scenarios. `conflict_adoption_test.go` and
  #      `preset_context_guards.feature` cover the hold itself. Nothing here
  #      re-states any of it; the one hold scenario below asserts only that a
  #      hand-mapped #537 term takes that path.
  #    - THE RE-STAMP AND REPROJECT MECHANISM. `house_vocabulary_domain.feature`
  #      pins it end to end ("A re-stamped edge takes its predicate from the
  #      context the type has now", "A re-stamped literal predicate follows the
  #      current context", the dry-run and second-run cases). One scenario below
  #      exercises the sequence on #537 data because the DATA is what is being
  #      claimed, not the mechanism; do not grow it into a restatement.
  #    - REGISTRY-WIDE COLLISION AND REVERSE-MAPPING.
  #      `house_vocabulary_domain.feature`'s "Every reference property still
  #      reverse-maps to its own name after the move" already sweeps every
  #      installed type and asserts "no two properties of one installed type
  #      resolve to the same predicate IRI". It needs no amendment and is not
  #      re-authored — but it becomes MORE load-bearing here: `task:status` is
  #      now declared on two types, and `schema:text` on two types in two
  #      presets, so a careless edit that also moved `templateBody` onto
  #      `schema:text` would collide there. It is asserted once below as a
  #      control on the repaired install.
  #    - PERSON AND ORGANIZATION'S RDF CLASSES.
  #      `core_type_class_declaration.feature` owns them. Checked: it pins
  #      `givenName`, the computed `name` and `familyName`, and does NOT pin
  #      `avatarURL`, so nothing there goes red. This story must not touch
  #      `@type` on either type, which CONTRACT 3 asserts.
  #    - THE `variant` CONTROL ENTRY on `web-page-template`.
  #      `control_keyword_terms.feature` owns control keywords. `variant` has no
  #      matching schema property so the vocabulary guard never sees it, and the
  #      reconcile above shows adding `web`, `slots` and `templateBody` beside it
  #      merges cleanly. Not re-authored.
  #    - THE INBOX ITSELF. `notification_inbox.feature` owns the behaviour. The
  #      repair changes predicates only, and the read scenarios below are the
  #      control proving it.
  #
  # 9a. ONE EXISTING FEATURE NEEDS AMENDING RATHER THAN DUPLICATING.
  #    `house_vocabulary_domain.feature`'s "The house prefix of each minting
  #    preset resolves on weos.io" is a Scenario Outline whose Examples name the
  #    three minting presets that exist today. Four presets start minting in this
  #    story, so FOUR ROWS ARE ADDED THERE:
  #
  #      | core          | core  | https://weos.io/vocab/core#          |
  #      | notifications | notif | https://weos.io/vocab/notifications# |
  #      | tasks         | task  | https://weos.io/vocab/tasks#         |
  #      | website       | web   | https://weos.io/vocab/website#       |
  #
  #    `knowledge` gets NO row: it mints nothing (CONTRACT 4). Adding the rows
  #    there rather than copying the scenario here is what keeps that outline's
  #    second assertion — "every house IRI the installed types of <preset>
  #    resolve is under <namespace>" — true of the new presets too. It is also
  #    the reason CONTRACT 4 gives each preset its OWN namespace and OPEN
  #    QUESTION 1 exists.
  #
  # ---------------------------------------------------------------------------
  # THE SHIM FOR AN EXISTING INSTALL. The upgrade scenarios need a database
  # written by the build BEFORE this story. Use #520's CONTRACT 7 pattern, the
  # one #535 and #521 both followed: a registry whose five preset contexts are
  # REVERTED to their pre-#537 shape by a TRANSFORM over
  # `PresetResourceType.Context` — not a second copy of the presets, so it cannot
  # drift. "The twin restarts on the build that retires the waived predicates"
  # then means: restart the same database against the unmodified
  # `presets.NewDefaultRegistry()`.
  #
  # HERE THE REVERT IS ONE OPERATION, NOT TWO. Unlike #535, where four properties
  # had a WRONG term that had to be restored, every one of the 23 had NO term, so
  # reverting is purely a STRIP: delete the terms and the four house prefixes and
  # the `dct` prefix this story adds. There is no `fo:`-shaped case, which is the
  # same fact as CONTRACT 8's "no second population" seen from the other side.
  #
  # ---------------------------------------------------------------------------
  # NEW STEPS THIS FILE NEEDS. Named so the implementer does not discover them
  # one failing scenario at a time.
  #
  #   - `a WeOS database provisioned by the build before the waived predicates
  #      were retired` and `the twin restarts on the build that retires the
  #      waived predicates` — the #537 pair of the shim steps every one of these
  #      stories defines for itself.
  #   - `no property of any installed type resolves to a term its vocabulary does
  #      not define` and its other-subject sibling. #535's equivalents are
  #      hardcoded to meal-planning (`w.noUndefinedPublishedTerm` sweeps
  #      `installedMealPlanningTypes`). GENERALISE THOSE by taking the preset
  #      name as a capture group rather than adding a second implementation; the
  #      registry-wide form is then the same function over every installed type.
  #   - `the vocabulary waiver list is empty` — reads `presets.VocabularyWaivers()`.
  #   - `the vocabulary guard names no other property of any installed type` —
  #      the registry-wide form of the meal-planning-scoped step.
  #   - `the "<slug>" type declares no term under "<namespace>"` — for CONTRACT 7.
  #   - `I create a "<slug>" named "<name>" with these properties:` already
  #      exists, but in `core_type_class_declaration_test.go`'s world rather than
  #      the shared `vocabWorld`. LIFT IT into `vocabWorld` rather than
  #      re-implementing it. This file needs it because `notification` requires
  #      four fields (recipient, title, occurredAt, read) and no single-property
  #      create step can satisfy them.
  #
  # ---------------------------------------------------------------------------
  # OPEN QUESTIONS — these need Akeem before the story is called done.
  #
  # 1. TWO SLUG PREDICATES, OR ONE? `organization.slug` and `web-page.slug` are
  #    the same concept — the URL-safe key the resource is addressed by — and
  #    this contract mints them SEPARATELY as `core:slug` and `web:slug`, so a
  #    query for "the thing whose slug is X" needs a UNION.
  #    The one-predicate alternative is to declare `core:slug` on `web-page` too,
  #    exactly as #535 declared `mp:ingredient` on two types for one relation.
  #    It is rejected here for two reasons and both are reversible: it makes the
  #    OPTIONAL `website` preset depend on `core`'s namespace to state its own
  #    predicate, and it breaks the SHAPE of `house_vocabulary_domain.feature`'s
  #    per-preset outline (9a), whose second assertion is that every house IRI a
  #    preset resolves is under THAT preset's namespace — so taking the shared
  #    term costs a step change there, not just an Examples row.
  #    If Akeem prefers one predicate, one Examples row in this file changes,
  #    that outline's website row needs its assertion relaxed, and `adopt-term`
  #    plus `weos:termAliases` is the mechanism for unifying them later at
  #    bounded cost either way.
  #
  # 2. `notification.taskRef` — house term, or `schema:about`? Contract 2a mints
  #    it house because the value is an opaque string with no `x-resource-type`
  #    and `schema:about` is Thing-ranged. The counter-argument is that
  #    `nutrition-information.ingredient` already resolves to `schema:about` in
  #    this repo, and a URN string IS what the notification is about. If Akeem
  #    prefers the published term it is one Examples row moving from the mint
  #    outline to the rename outline, and `about` is already allow-listed so no
  #    other change follows.
  #
  # 3. `web-page.template` and `web-page-template.templateBody` are the two
  #    lowest-confidence mints. `schema:isBasedOn` and `schema:text` respectively
  #    are defensible readings and both are already allow-listed. The contract
  #    rejects both on the grounds that the stored values are renderer machinery
  #    rather than the things those terms describe. Each is one Examples row if
  #    Akeem rules otherwise. Getting these wrong is cheap in exactly one
  #    direction: an over-correction here mints a house term for something
  #    published, which is invisible; the reverse asserts a published term over
  #    machinery, which is a fresh subject misuse.
  #
  # 4. DOES #537 SHIP THE MIGRATION RUNBOOK, OR DEFER IT? Same question #535
  #    left open, with more weight: `core` and `notifications` are AutoInstall,
  #    so unlike meal-planning this migration applies to EVERY instance, and the
  #    graph on the live twin answers under two predicates until somebody runs
  #    8b. Nothing is broken for an API or projection reader either way.
  # ---------------------------------------------------------------------------

  # ===========================================================================
  # A FRESH INSTALL — the twenty-three repairs.
  # ===========================================================================

  # CONTRACT 2, case 1. Fourteen properties across five types, each claiming a
  # name the vocabulary it lands in does not define. Named one per row rather
  # than counted, so a missed one fails on its own line instead of inside a
  # total. Grouped by preset because each preset mints into its OWN house
  # namespace (CONTRACT 4), and a repair that pointed all fourteen at one
  # namespace would pass a laxer assertion.
  Scenario Outline: A property its vocabulary does not define resolves to its own preset's house vocabulary
    Given a clean WeOS database
    When the operator installs the "<preset>" preset
    Then the "<slug>" type resolves the property "<property>" to "<predicate>"
    And the "<slug>" type resolves nothing to "https://schema.org/<property>"

    Examples: core
      | preset | slug         | property | predicate                       |
      | core   | organization | slug     | https://weos.io/vocab/core#slug |

    Examples: notifications
      | preset        | slug         | property    | predicate                                       |
      | notifications | notification | kind        | https://weos.io/vocab/notifications#kind        |
      | notifications | notification | actionUrl   | https://weos.io/vocab/notifications#actionUrl   |
      | notifications | notification | actionLabel | https://weos.io/vocab/notifications#actionLabel |
      | notifications | notification | taskRef     | https://weos.io/vocab/notifications#taskRef     |
      | notifications | notification | occurredAt  | https://weos.io/vocab/notifications#occurredAt  |
      | notifications | notification | read        | https://weos.io/vocab/notifications#read        |
      | notifications | notification | dedupeKey   | https://weos.io/vocab/notifications#dedupeKey   |

    Examples: tasks
      | preset | slug | property | predicate                            |
      | tasks  | task | dueDate  | https://weos.io/vocab/tasks#dueDate  |
      | tasks  | task | priority | https://weos.io/vocab/tasks#priority |

    Examples: website
      | preset  | slug              | property     | predicate                                  |
      | website | web-page          | slug         | https://weos.io/vocab/website#slug         |
      | website | web-page          | template     | https://weos.io/vocab/website#template     |
      | website | web-page-template | slots        | https://weos.io/vocab/website#slots        |
      | website | web-page-template | templateBody | https://weos.io/vocab/website#templateBody |

  # CONTRACT 2, case 3. These two RESOLVE, which is what makes them the
  # dangerous half: no lookup ever exposes them and every read passes either
  # way. `schema:status` is the status of a MedicalCondition, MedicalProcedure
  # or MedicalStudy. A task is not a clinical trial. This is the same misuse
  # #535 removed from `meal-occurrence` and `shopping-list`, and it is repaired
  # the same way: ONE house predicate on BOTH types, so a query for "what state
  # is this in" asks one question.
  Scenario Outline: A property the vocabulary publishes for another subject stops borrowing it
    Given a clean WeOS database
    When the operator installs the "tasks" preset
    Then the "<slug>" type resolves the property "status" to "https://weos.io/vocab/tasks#status"
    And the "<slug>" type resolves nothing to "https://schema.org/status"

    Examples: the medical term both task types were riding
      | slug    |
      | task    |
      | project |

  # CONTRACT 1 and 2 — RENAME-BY-TERM, and the scenario that fails if the fix
  # over-corrects. Every row here is a published term in the published sense, so
  # a house mint would satisfy the outline above and still be the wrong answer.
  # `notification.title` is the row no reading of the issue predicts: the fault
  # is a subject misuse (case 3, which the issue says to repair with a house
  # term) and the repair is a different PUBLISHED term, because `schema:name` is
  # what a Message's short label is called.
  Scenario Outline: A property whose vocabulary already publishes the right term takes the published spelling
    Given a clean WeOS database
    When the operator installs the "<preset>" preset
    Then the "<slug>" type resolves the property "<property>" to "<predicate>"
    And the "<slug>" type resolves nothing to "https://weos.io/vocab/<preset>#<property>"

    Examples: names schema.org really does define, in the sense these presets use
      | preset        | slug             | property  | predicate                     |
      | core          | person           | avatarURL | https://schema.org/image      |
      | core          | organization     | logoURL   | https://schema.org/logo       |
      | notifications | notification     | title     | https://schema.org/name       |
      | notifications | notification     | body      | https://schema.org/text       |
      | website       | web-page-element | content   | https://schema.org/text       |

  # CONTRACT 2a and 5b, and the pair that proves the guard polices NAMESPACES
  # rather than hard-coding schema.org. SKOS Core publishes neither `title` nor
  # `description`; both are Dublin Core. The repair is a PUBLISHED term, and the
  # namespace it lands in must become policed in the same change — otherwise the
  # guard stops looking at these two properties and reports the knowledge preset
  # clean by never asking, which is a silent pass rather than a repair.
  # The IRI is pinned exactly because the legacy `dc/elements/1.1/` namespace
  # publishes both names too and is deliberately NOT policed.
  Scenario: A concept scheme titles itself in the vocabulary that publishes the term
    Given a clean WeOS database
    When the operator installs the "knowledge" preset
    Then the "concept-scheme" type resolves the property "title" to "http://purl.org/dc/terms/title"
    And the "concept-scheme" type resolves the property "description" to "http://purl.org/dc/terms/description"
    And the "concept-scheme" type resolves nothing to "http://www.w3.org/2004/02/skos/core#title"
    And the "concept-scheme" type resolves nothing to "http://www.w3.org/2004/02/skos/core#description"
    And the "concept-scheme" type resolves nothing to "http://purl.org/dc/elements/1.1/title"

  # CONTRACT 3 — the most important scenario in this file, and the only one that
  # fails if the fix over-corrects. `web-page-element.cssSelector` is the
  # sharpest row: schema.org's domainIncludes for it is literally
  # `WebPageElement`, and it sits on the same type as `content`, which does
  # move. One moves, one stays, on one type. The rest are the names a careless
  # sweep would take on its way past — a whole Article, a whole WebSite, both
  # SKOS types, and the two that appear on nearly every type.
  Scenario Outline: A genuine published name keeps resolving where it already does
    Given a clean WeOS database
    When the operator installs the "<preset>" preset
    Then the "<slug>" type resolves the property "<property>" to "<predicate>"

    Examples: the row this scenario exists for
      | preset  | slug             | property    | predicate                      |
      | website | web-page-element | cssSelector | https://schema.org/cssSelector |

    Examples: core
      | preset | slug         | property    | predicate                      |
      | core   | person       | givenName   | https://schema.org/givenName   |
      | core   | person       | familyName  | https://schema.org/familyName  |
      | core   | person       | name        | https://schema.org/name        |
      | core   | person       | email       | https://schema.org/email       |
      | core   | organization | name        | https://schema.org/name        |
      | core   | organization | description | https://schema.org/description |
      | core   | organization | url         | https://schema.org/url         |

    Examples: knowledge — the evidence that the concept-scheme pair is a real fault
      | preset    | slug       | property   | predicate                                   |
      | knowledge | concept    | prefLabel  | http://www.w3.org/2004/02/skos/core#prefLabel  |
      | knowledge | concept    | altLabel   | http://www.w3.org/2004/02/skos/core#altLabel   |
      | knowledge | concept    | definition | http://www.w3.org/2004/02/skos/core#definition |
      | knowledge | collection | prefLabel  | http://www.w3.org/2004/02/skos/core#prefLabel  |
      | knowledge | collection | member     | http://www.w3.org/2004/02/skos/core#member     |

    Examples: notifications — schema.org publishes recipient for Message
      | preset        | slug         | property  | predicate                     |
      | notifications | notification | recipient | https://schema.org/recipient  |

    Examples: tasks — including the only reference property in these five presets
      | preset | slug    | property    | predicate                      |
      | tasks  | project | name        | https://schema.org/name        |
      | tasks  | project | description | https://schema.org/description |
      | tasks  | task    | name        | https://schema.org/name        |
      | tasks  | task    | description | https://schema.org/description |
      | tasks  | task    | project     | https://schema.org/isPartOf    |

    Examples: website
      | preset  | slug              | property        | predicate                        |
      | website | web-site          | name            | https://schema.org/name          |
      | website | web-site          | url             | https://schema.org/url           |
      | website | web-site          | description     | https://schema.org/description   |
      | website | web-site          | inLanguage      | https://schema.org/inLanguage    |
      | website | web-page          | name            | https://schema.org/name          |
      | website | web-page          | description     | https://schema.org/description   |
      | website | web-page-element  | name            | https://schema.org/name          |
      | website | web-page-template | name            | https://schema.org/name          |
      | website | theme             | name            | https://schema.org/name          |
      | website | theme             | version         | https://schema.org/version       |
      | website | theme             | thumbnailUrl    | https://schema.org/thumbnailUrl  |
      | website | article           | headline        | https://schema.org/headline      |
      | website | article           | articleBody     | https://schema.org/articleBody   |
      | website | article           | author          | https://schema.org/author        |
      | website | article           | datePublished   | https://schema.org/datePublished |
      | website | blog-post         | headline        | https://schema.org/headline      |
      | website | blog-post         | articleBody     | https://schema.org/articleBody   |
      | website | blog-post         | author          | https://schema.org/author        |
      | website | blog-post         | datePublished   | https://schema.org/datePublished |
      | website | faq               | mainEntity      | https://schema.org/mainEntity    |
      | website | breadcrumb-list   | itemListElement | https://schema.org/itemListElement |

  # CONTRACT 3, the class half. `core_type_class_declaration.feature` owns these
  # two classes; this asserts only that #537 leaves them alone, because a repair
  # that rewrote the core contexts wholesale would take the `@type` with it and
  # nothing in THIS file would notice.
  Scenario Outline: The repair leaves each type's RDF class exactly where it was
    Given a clean WeOS database
    And the operator installs the "<preset>" preset
    When a "<slug>" resource is created
    Then that resource carries the RDF type "<class>"

    Examples:
      | preset        | slug              | class                                             |
      | core          | person            | http://xmlns.com/foaf/0.1/Person                  |
      | core          | organization      | http://www.w3.org/ns/org#Organization             |
      | knowledge     | concept-scheme    | http://www.w3.org/2004/02/skos/core#ConceptScheme |
      | notifications | notification      | https://schema.org/Message                        |
      | tasks         | task              | https://schema.org/Action                         |
      | tasks         | project           | https://schema.org/Project                        |
      | website       | web-page-element  | https://schema.org/WebPageElement                 |

  # CONTRACT 7. None of these five presets has a shared context builder, so the
  # trap #535 named cannot be sprung — but its equivalent can: declaring the
  # house prefix and its terms on every type of the preset. These types have no
  # slug, no template, no slots, no templateBody and no dct: term, and a context
  # that declares them anyway is inert today, lies about the type, and is how a
  # future collision gets built.
  Scenario Outline: A type without the property declares neither the term nor the house prefix
    Given a clean WeOS database
    And the operator installs the "<preset>" preset
    Then the "<slug>" type declares no term under "<namespace>"
    And the stored "<slug>" context declares no "<prefix>"

    Examples: website types with no template machinery
      | preset  | slug            | namespace                      | prefix |
      | website | web-site        | https://weos.io/vocab/website# | web    |
      | website | article         | https://weos.io/vocab/website# | web    |
      | website | blog-post       | https://weos.io/vocab/website# | web    |
      | website | theme           | https://weos.io/vocab/website# | web    |
      | website | faq             | https://weos.io/vocab/website# | web    |
      | website | breadcrumb-list | https://weos.io/vocab/website# | web    |

    Examples: knowledge types with no Dublin Core term
      | preset    | slug       | namespace                | prefix |
      | knowledge | concept    | http://purl.org/dc/terms/ | dct   |
      | knowledge | collection | http://purl.org/dc/terms/ | dct   |

  # CONTRACT 4. Each preset mints under its OWN namespace, and `knowledge` mints
  # nothing at all — both repairs there are published Dublin Core terms, so a
  # `KnowledgeVocab` constant would have nothing to point at. Asserted because an
  # implementer working through five presets in order will add a fifth one.
  # The per-preset prefix sweep itself belongs in
  # `house_vocabulary_domain.feature` as four new Examples rows (CONTRACT 9a),
  # not here.
  Scenario: The knowledge preset mints no house vocabulary at all
    Given a clean WeOS database
    When the operator installs the "knowledge" preset
    Then the "concept" type declares no term under "https://weos.io/vocab/"
    And the "concept-scheme" type declares no term under "https://weos.io/vocab/"
    And the "collection" type declares no term under "https://weos.io/vocab/"

  # A term is only worth anything if a written value actually lands on it. The
  # projection and the API resolve through the type's own context, so they read
  # the same before and after and prove nothing on their own; the stored document
  # is where the predicate is observable, which is why the statement assertions
  # carry this scenario and the reads are the control beside them. `task` is used
  # for the write path throughout this file because `name` is its only required
  # field.
  Scenario: A value written to a repaired property is stated on its new predicate
    Given a clean WeOS database
    And the operator installs the "tasks" preset
    When I create a "task" named "Ship the repair" with these properties:
      | status   | in-progress |
      | priority | high        |
    Then the triple store holds "https://weos.io/vocab/tasks#status" from the "task" "Ship the repair" with the value "in-progress"
    And the triple store holds no statement under "https://schema.org/status" about the "task" "Ship the repair"
    # THE PROJECTION READ IS ON `priority`, NOT `status`. `status` is a RESERVED
    # projection column (`standardColumnNames`, projection_manager.go:46), so
    # `extractNodeColumns` SKIPS a schema property of that name and the column
    # carries the resource lifecycle status instead — a `task` can never surface
    # its own `status` through a flat read. That is a pre-existing defect, tracked
    # as #539, not something this repair caused: it fails identically on the build
    # before #537. `priority` is a #537 house mint on the same type, so the line
    # still reads back exactly what this story changed. Every `status` assertion on
    # the triple store and the stored document stays — those are what prove the
    # shared `task:status` predicate.
    And reading the "task" "Ship the repair" back through the projection returns "priority" as "high"
    And the API read of the "task" "Ship the repair" returns "status" as "in-progress"

  # The notification half, on the preset that is AutoInstall and therefore
  # present on every instance in existence. It also exercises the two rename
  # rows on one type at once: `title` lands on the published `schema:name` while
  # `body` lands on the published `schema:text`, and neither may quietly become
  # a house term. Needs the `with these properties:` step lifted into the shared
  # world, because a notification requires four fields.
  Scenario: A notification states its title and body on the published terms and its house fields on ours
    Given a clean WeOS database
    When I create a "notification" named "Import finished" with these properties:
      | recipient   | urn:agent:akeem          |
      | title       | Import finished          |
      | body        | 412 rows were imported   |
      | kind        | import.completed         |
      | occurredAt  | 2026-08-26T10:00:00.000000000Z |
      | read        | false                    |
    Then the triple store holds "https://schema.org/name" from the "notification" "Import finished" with the value "Import finished"
    And the triple store holds "https://schema.org/text" from the "notification" "Import finished" with the value "412 rows were imported"
    And the triple store holds "https://weos.io/vocab/notifications#kind" from the "notification" "Import finished" with the value "import.completed"
    And the triple store holds no statement under "https://schema.org/title" about the "notification" "Import finished"
    And the triple store holds no statement under "https://schema.org/body" about the "notification" "Import finished"
    And the API read of the "notification" "Import finished" returns "title" as "Import finished"
    And the API read of the "notification" "Import finished" returns "kind" as "import.completed"

  # ===========================================================================
  # THE GUARD — the issue's own definition of done.
  # ===========================================================================

  # The criterion, stated registry-wide rather than per preset: nothing violates
  # and nothing is waived. The second assertion is the one that makes the first
  # mean anything — a violation set equal to a NON-empty waiver map is exactly
  # the state this story exists to leave behind.
  Scenario: No installed type claims an undefined term, and no waiver remains
    Given a clean WeOS database
    When the operator installs every built-in preset
    Then no property of any installed type resolves to a term its vocabulary does not define
    And no property of any installed type resolves to a term a published vocabulary defines for another subject
    And the vocabulary waiver list is empty

  # The guard must still BITE with the waiver map EMPTY, which is a state that
  # has never existed before: every previous green run of this guard was green
  # against a non-empty ratchet. A sweep that passes by never looking at
  # anything is indistinguishable from a green one otherwise, and an empty map
  # is exactly the shape that invites an implementer to "simplify" the equality
  # check into nothing. Asserted on the shape that caused #535 — a house
  # property added with no term, riding `@vocab` into somebody else's namespace.
  Scenario: The guard still names a new offender once the waiver list is empty
    Given a clean WeOS database
    And the "notifications" preset adds an untermed "snoozedUntil" string property to "notification"
    When the operator installs every built-in preset
    Then the vocabulary guard names "notification" "snoozedUntil" resolving to "https://schema.org/snoozedUntil"
    And the vocabulary guard names no other property of any installed type

  # CONTRACT 9's control. `task:status` is now declared on two types and
  # `schema:text` on two types in two presets, so the registry-wide collision
  # sweep `house_vocabulary_domain.feature` already owns becomes more
  # load-bearing here. Asserted once, on the repaired install, rather than
  # re-authored there.
  Scenario: The repair introduces no collision and breaks no reference
    Given a clean WeOS database
    When the operator installs every built-in preset
    Then no two properties of one installed type resolve to the same predicate IRI
    And every reference property of every installed type reverse-maps to its own name

  # ===========================================================================
  # AN EXISTING INSTALL — CONTRACT 8. Every one of the 23 had no term at all,
  # so the whole upgrade is a silent merge. These scenarios are what "silent"
  # has to mean, and the last one is what it must NOT be read as meaning.
  # ===========================================================================

  # The negative assertion is the load-bearing one: nothing may be HELD, because
  # nothing diverges. Measured against `reconcileAdditiveContext`, not assumed
  # (CONTRACT 8). One row per preset, because a repair that got the merge right
  # on four presets and wrong on the fifth would otherwise pass.
  Scenario Outline: A term that never existed before reaches an existing install with no operator action
    Given a WeOS database provisioned by the build before the waived predicates were retired
    And the operator installs the "<preset>" preset
    When the twin restarts on the build that retires the waived predicates
    Then the stored "<slug>" context maps "<property>" to "<predicate>"
    And the boot reconcile does not report the "<property>" context term as held for "<slug>"
    And the boot reconcile records no failure for "<slug>"

    Examples:
      | preset        | slug              | property     | predicate                                  |
      | core          | person            | avatarURL    | https://schema.org/image                   |
      | core          | organization      | slug         | https://weos.io/vocab/core#slug            |
      | knowledge     | concept-scheme    | title        | http://purl.org/dc/terms/title             |
      | notifications | notification      | body         | https://schema.org/text                    |
      | notifications | notification      | dedupeKey    | https://weos.io/vocab/notifications#dedupeKey |
      | tasks         | task              | status       | https://weos.io/vocab/tasks#status         |
      | website       | web-page-element  | content      | https://schema.org/text                    |
      | website       | web-page-template | templateBody | https://weos.io/vocab/website#templateBody |

  # The read half of "silent": nothing an API or projection consumer sees may
  # change. This is the control that makes the graph scenario below meaningful —
  # without it, "the graph still says schema.org" reads like a broken upgrade
  # rather than a deferred migration.
  Scenario: A value written before the terms existed still reads back after the upgrade
    Given a WeOS database provisioned by the build before the waived predicates were retired
    And the operator installs the "tasks" preset
    And I create a "task" named "Ship the repair" with these properties:
      | status   | in-progress |
      | priority | high        |
    When the twin restarts on the build that retires the waived predicates
    # On `priority` rather than `status` for the reserved-column reason above:
    # `status` is a standard projection column, so a flat read can never return
    # the type's own value (#539). `priority` is a #537 mint on the same type.
    Then reading the "task" "Ship the repair" back through the projection returns "priority" as "high"
    And the API read of the "task" "Ship the repair" returns "status" as "in-progress"

  # CONTRACT 8a, and the scenario that stops anyone reading "the upgrade is
  # silent" as "the upgrade is complete". The predicate of an old literal is
  # stamped into the resource's own embedded context at write time and a
  # reprojection replays it, so the stored document disagrees with the type's
  # current context until the re-stamp runs. An operator reading only the graph
  # would conclude the upgrade did nothing.
  #
  # DELIBERATELY NOT ASSERTED HERE, and not because it does not happen: the
  # lingering row under the old predicate after a re-stamp and a reproject. The
  # triple projection is upsert-only and only
  # `weos worker checkpoint reset oxigraph --truncate` clears it, which is why
  # CONTRACT 8b's runbook ends there. It cannot be asserted in this world for the
  # two reasons #535 found by running it — `config.Default()` sets no Oxigraph
  # store, so there is no row to observe, and the literal steps in this world all
  # bind to `documentStatesLiteral` (CONTRACT 8a0), so asserting it would assert
  # the opposite of the line above it about one surface. Follow that precedent
  # rather than promoting these lines into steps.
  Scenario: An old value keeps its write-time predicate until a re-stamp
    Given a WeOS database provisioned by the build before the waived predicates were retired
    And the operator installs the "tasks" preset
    And I create a "task" named "Ship the repair" with "status" set to "in-progress"
    And the twin restarts on the build that retires the waived predicates
    Then the stored document states "https://schema.org/status" from the "task" "Ship the repair" with the value "in-progress"
    When the operator re-stamps the stored documents and writes
    And the operator reprojects the event feed
    Then the stored document states "https://weos.io/vocab/tasks#status" from the "task" "Ship the repair" with the value "in-progress"
    And the stored document states no statement under "https://schema.org/status" about the "task" "Ship the repair"

  Scenario: A value written after the upgrade lands on the new predicate with no migration
    Given a WeOS database provisioned by the build before the waived predicates were retired
    And the operator installs the "tasks" preset
    And the twin restarts on the build that retires the waived predicates
    When I create a "task" named "Close the ticket" with these properties:
      | status   | done   |
      | priority | normal |
    Then the triple store holds "https://weos.io/vocab/tasks#status" from the "task" "Close the ticket" with the value "done"
    And the triple store holds no statement under "https://schema.org/status" about the "task" "Close the ticket"
    # On `priority` rather than `status` for the reserved-column reason above:
    # `status` is a standard projection column, so a flat read can never return
    # the type's own value (#539). `priority` is a #537 mint on the same type.
    And reading the "task" "Close the ticket" back through the projection returns "priority" as "normal"

  # CONTRACT 8's exception, and the ONLY path to a held term in this story: an
  # operator who had already mapped one of these names by hand. Then the
  # definitions diverge, the merge holds at the stored one, and the boot reports
  # it — `Conflicts=[status]`, `Added=[]`, `Changed=false`, measured against
  # `reconcileAdditiveContext`. Holding is the system working: overwriting would
  # repoint a predicate the operator's own data is already keyed by.
  #
  # The adoption MECHANISM is not restated here. `context_term_adoption.feature`
  # covers add-versus-rename, `--all`, aliases, double adoption and edges written
  # under a retired IRI across 24 scenarios, and `preset_context_guards.feature`
  # covers the hold itself. This asserts only that a #537 term takes that path
  # and that the operator's own IRI survives the boot untouched.
  Scenario: A term an operator had already mapped by hand is held rather than overwritten
    Given a WeOS database provisioned by the build before the waived predicates were retired
    And the operator installs the "tasks" preset
    And the operator maps "status" to "https://example.org/workflow#state" in the stored "task" context
    And I create a "task" named "Ship the repair" with these properties:
      | status   | in-progress |
      | priority | high        |
    When the twin restarts on the build that retires the waived predicates
    Then the boot reconcile reports the "status" context term as held for "task"
    And the stored "task" context still maps "status" to "https://example.org/workflow#state"
    And the boot reconcile records no failure for "task"
    # On `priority` rather than `status` for the reserved-column reason above:
    # `status` is a standard projection column, so a flat read can never return
    # the type's own value (#539). `priority` is a #537 mint on the same type.
    And reading the "task" "Ship the repair" back through the projection returns "priority" as "high"
