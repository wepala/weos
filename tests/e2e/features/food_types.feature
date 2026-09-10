@wm-kb6sg.1
Feature: The food types live in core's meal-planning preset
  As a WeOS service that registers only core's presets
  I want mini-me's ten food types to install from core's meal-planning preset
  So that a second product reads the same food definitions mini-me stores today

  # CONTRACT for story wm-kb6sg.1. The design is the "Story wm-kb6sg.1" section of
  # docs/decisions/wehungry-food-types-uploads-and-account-erasure.md.
  #
  # 1. THE GOLDEN COPY AND ITS FREEZE (finding wm-5kgbk). The byte check reads one
  #    file, application/presets/mealplanning/testdata/mini_me_food_types.golden.json.
  #    It records its source as repo wepala/mini-me-weos, commit fbf90ba6, path
  #    cmd/mini-me/food_preset.go, blob 99f92d2b7170087af64223387460f79e783a7e30.
  #    It holds the ten types with name, slug, description, and the context and
  #    schema as JSON STRINGS, so no decoder can re-order a byte. The freeze is that
  #    commit: a change to mini-me's food_preset.go after it is not in core until the
  #    golden is re-taken at the mini-me bump. The upgrade scenarios below build the
  #    old install FROM that file, so a stale or edited golden fails here, not only in
  #    a unit test. The shim step refuses a golden that does not name fbf90ba6 or does
  #    not hold exactly ten types.
  #
  # 2. WHAT IS NOT IN THIS FILE, AND WHY. Registry bytes, hidden slugs and the P1
  #    comment on the three twin pairs have no e2e harness. The report on bead
  #    wm-mol-jlor names the unit tests that own them. The existing scenarios "No
  #    meal-planning property claims a term its vocabulary does not define" and "Every
  #    reference property still reverse-maps to its own name after the move" already
  #    sweep every meal-planning type, so they cover the ten moved types unchanged.

  @wip
  Scenario Outline: A moved food type installs from meal-planning with the class mini-me gives it
    Given a clean WeOS database
    When the operator installs the "meal-planning" preset
    Then the "<slug>" type advertises the RDF class "<class>"

    Examples:
      | slug              | class                                                     |
      | taste-profile     | https://weos.io/vocab/meal-planning#TasteProfile          |
      | meal-log          | https://weos.io/vocab/meal-planning#MealOccurrence        |
      | restaurant        | https://schema.org/Restaurant                             |
      | planned-meal      | https://schema.org/Schedule                               |
      | grocery-amendment | https://weos.io/vocab/meal-planning#ShoppingListAmendment |
      | grocery-list-item | https://weos.io/vocab/meal-planning#ShoppingListItem      |
      | staple            | https://weos.io/vocab/meal-planning#Staple                |
      | purchase          | https://schema.org/Order                                  |
      | purchase-line     | https://schema.org/OrderItem                              |
      | item-kind         | https://schema.org/DefinedTerm                            |

  @wip
  Scenario: The meal-planning preset offers all twenty-four food types and no other preset offers them
    When the operator lists the built-in presets
    Then the "meal-planning" preset offers exactly these types:
      | recipe                |
      | how-to-step           |
      | ingredient            |
      | recipe-ingredient     |
      | nutrition-information |
      | cookbook              |
      | meal-plan             |
      | scheduled-meal        |
      | meal-occurrence       |
      | pantry                |
      | food-item             |
      | shopping-list         |
      | shopping-list-item    |
      | restricted-diet       |
      | taste-profile         |
      | meal-log              |
      | restaurant            |
      | planned-meal          |
      | grocery-amendment     |
      | grocery-list-item     |
      | staple                |
      | purchase              |
      | purchase-line         |
      | item-kind             |
    And no other built-in preset offers any of those types

  @wip
  Scenario: A restaurant is recorded on a core-only install that has no agent type
    Given a clean WeOS database
    And the operator installs the "meal-planning" preset
    When I create a "restaurant" named "Roti Palace" with "servesCuisine" set to "Trinidadian"
    Then the API read of the "restaurant" "Roti Palace" returns "servesCuisine" as "Trinidadian"
    And the "restaurant" "Roti Palace" carries the RDF type "https://schema.org/Restaurant" in the stored document
    And the triple store holds "https://schema.org/servesCuisine" from the "restaurant" "Roti Palace" with the value "Trinidadian"

  @wip
  Scenario Outline: A twin food type keeps its own slug and table while it shares a core class
    Given a clean WeOS database
    When the operator installs the "meal-planning" preset
    Then the "<twin>" type advertises the RDF class "<class>"
    And the "<core type>" type advertises the RDF class "<class>"
    And the "<twin>" projection table has a "<twin column>" column

    Examples:
      | twin              | core type          | class                                              | twin column |
      | meal-log          | meal-occurrence    | https://weos.io/vocab/meal-planning#MealOccurrence   | rating      |
      | planned-meal      | scheduled-meal     | https://schema.org/Schedule                          | people      |
      | grocery-list-item | shopping-list-item | https://weos.io/vocab/meal-planning#ShoppingListItem | ingredient  |

  @wip
  Scenario Outline: A moved type keeps the published schema.org property mini-me gives it
    Given a clean WeOS database
    When the operator installs the "meal-planning" preset
    Then the "<slug>" type resolves the property "<property>" to "<predicate>"
    And the vocabulary waiver list is empty

    Examples:
      | slug       | property      | predicate                        |
      | meal-log   | orderId       | https://schema.org/isBasedOn     |
      | purchase   | seller        | https://schema.org/seller        |
      | restaurant | servesCuisine | https://schema.org/servesCuisine |
      | restaurant | telephone     | https://schema.org/telephone     |

  @wip
  Scenario: A purchase keeps its receipt hash on the ingest vocabulary
    Given a clean WeOS database
    When the operator installs the "meal-planning" preset
    Then the "purchase" type resolves the property "contentHash" to "https://weos.io/vocab/ingest#contentHash"
    And the "purchase" type resolves nothing to "https://weos.io/vocab/meal-planning#contentHash"

  @wip
  Scenario Outline: Upgrading a twin whose food types came from mini-me records no type change
    Given a WeOS database provisioned by the build whose "food" preset carries mini-me's food definitions at commit "fbf90ba6"
    And the operator installs the "meal-planning" preset
    And the operator installs the "food" preset
    When the twin restarts on the build that moves the food definitions into meal-planning
    Then no resource type update is recorded for "<slug>"
    And the boot reconcile does not report "<slug>" as updated
    And the boot reconcile records no failure for "<slug>"

    Examples:
      | slug              |
      | taste-profile     |
      | meal-log          |
      | restaurant        |
      | planned-meal      |
      | grocery-amendment |
      | grocery-list-item |
      | staple            |
      | purchase          |
      | purchase-line     |
      | item-kind         |

  @wip
  Scenario: A restaurant written before the upgrade reads back unchanged after it
    Given a WeOS database provisioned by the build whose "food" preset carries mini-me's food definitions at commit "fbf90ba6"
    And the operator installs the "meal-planning" preset
    And the operator installs the "food" preset
    And I create a "restaurant" named "Roti Palace" with "servesCuisine" set to "Trinidadian"
    When the twin restarts on the build that moves the food definitions into meal-planning
    Then the API read of the "restaurant" "Roti Palace" returns "servesCuisine" as "Trinidadian"
    And the triple store holds "https://schema.org/servesCuisine" from the "restaurant" "Roti Palace" with the value "Trinidadian"
