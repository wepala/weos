---
title: Preset Catalog
parent: Reference
layout: default
nav_order: 5
---

# Preset Catalog

Presets bundle related resource types. Install a preset with:

```bash
weos resource-type preset install <name>
```

## core

**Auto-install:** Yes (installed on first run)

| Type | Slug | @type | Properties |
|------|------|-------|------------|
| Person | `person` | foaf:Person / schema:Person | `givenName`\*, `familyName`\*, `name` (computed), `email`, `avatarURL` |
| Organization | `organization` | org:Organization / schema:Organization | `name`\*, `slug`\*, `description`, `url`, `logoURL` |

The Person type auto-computes `name` from `givenName` + `familyName`.

---

## ecommerce

**Auto-install:** No

| Type | Slug | @type | Properties |
|------|------|-------|------------|
| Product | `product` | schema:Product | `name`\*, `description`, `sku`, `brand`, `image` (format: uri) |
| Offer | `offer` | schema:Offer | `name`\*, `price`\* (number), `priceCurrency`, `availability` |
| Review | `review` | schema:Review | `name`\*, `reviewBody`, `reviewRating` (integer), `author` |
| Service | `service` | schema:Service | `name`\*, `description`, `provider`, `serviceType` |

---

## tasks

**Auto-install:** No

| Type | Slug | @type | Properties |
|------|------|-------|------------|
| Project | `project` | schema:Project | `name`\*, `description`, `status` |
| Task | `task` | schema:Action | `name`\*, `description`, `status`, `priority`, `dueDate` (format: date), `project` (ref→project, display: name) |

The Task type's `project` property references the Project type, creating a foreign key relationship.

---

## website

**Auto-install:** No

| Type | Slug | @type | Properties |
|------|------|-------|------------|
| Web Site | `web-site` | schema:WebSite | `name`\*, `url` (format: uri), `description`, `inLanguage` |
| Web Page | `web-page` | schema:WebPage | `name`\*, `slug`, `description`, `template` |
| Web Page Element | `web-page-element` | schema:WebPageElement | `name`\*, `cssSelector`, `content` |
| Web Page Template | `web-page-template` | schema:WebPage (variant: template) | `name`\*, `templateBody`, `slots` (array of strings) |
| Theme | `theme` | schema:CreativeWork | `name`\*, `version`, `thumbnailUrl` (format: uri) |
| Article | `article` | schema:Article | `headline`\*, `articleBody`, `author`, `datePublished` (format: date-time) |
| Blog Post | `blog-post` | schema:BlogPosting | `headline`\*, `articleBody`, `author`, `datePublished` (format: date-time) |
| FAQ | `faq` | schema:FAQPage | `name`\*, `mainEntity` (array of {name, acceptedAnswer}) |
| Breadcrumb List | `breadcrumb-list` | schema:BreadcrumbList | `name`\*, `itemListElement` (array of {name, item (uri), position (int)}) |

---

## events

**Auto-install:** No

| Type | Slug | @type | Properties |
|------|------|-------|------------|
| Event | `event` | schema:Event | `name`\*, `description`, `startDate` (format: date-time), `endDate` (format: date-time), `location` |
| Place | `place` | schema:Place | `name`\*, `address`, `geo` (object: latitude, longitude) |
| Venue | `venue` | schema:EventVenue | `name`\*, `address`, `maximumAttendeeCapacity` (integer) |

---

## knowledge

**Auto-install:** No

| Type | Slug | @type | Properties |
|------|------|-------|------------|
| Concept | `concept` | skos:Concept | `prefLabel`\*, `altLabel` (array), `definition` |
| Concept Scheme | `concept-scheme` | skos:ConceptScheme | `title`\*, `description` |
| Collection | `collection` | skos:Collection | `prefLabel`\*, `member` (array) |

---

## meal-planning

**Auto-install:** No

| Type | Slug | @type | Properties |
|------|------|-------|------------|
| Recipe | `recipe` | schema:Recipe | `name`\*, `description`, `recipeYield` (object), `prepTime`, `cookTime`, `totalTime`, `recipeCuisine`, `recipeCategory`, `keywords` (array), `suitableForDiet` (ref→restricted-diet), `recipeInstructions` (ref→how-to-step), `recipeIngredient` (ref→recipe-ingredient), `nutrition` (ref→nutrition-information), `image` (format: uri) |
| How-To Step | `how-to-step` | schema:HowToStep | `position`\* (integer), `text`\*, `image` (format: uri), `recipe`\* (ref→recipe) |
| Ingredient | `ingredient` | fo:Food | `name`\*, `description`, `alternateNames` (array), `shoppingCategory` (enum), `season` (array), `suitableForDiet` (ref→restricted-diet), `defaultUnit`, `image` (format: uri) |
| Recipe Ingredient | `recipe-ingredient` | mp:RecipeIngredient | `quantity`\* (number), `unit`\*, `preparation`, `optional` (boolean), `recipe`\* (ref→recipe), `ingredient`\* (ref→ingredient) |
| Nutrition Information | `nutrition-information` | schema:NutritionInformation | `servingSize`\*, `calories`, `proteinContent`, `carbohydrateContent`, `fatContent`, `saturatedFatContent`, `fiberContent`, `sugarContent`, `sodiumContent`, `recipe` (ref→recipe), `ingredient` (ref→ingredient) |
| Cookbook | `cookbook` | schema:Collection | `name`\*, `description`, `image` (format: uri), `keywords` (array), `recipes` (ref→recipe) |
| Meal Plan | `meal-plan` | schema:MealPlan | `name`\*, `description`, `startDate`\* (format: date), `endDate`\* (format: date), `suitableForDiet` (ref→restricted-diet) |
| Scheduled Meal | `scheduled-meal` | schema:Schedule | `startDate`\*, `endDate`, `startTime`, `endTime`, `duration`, `repeatFrequency`, `repeatCount` (integer), `byDay` (array), `mealType`\* (enum), `servings` (number), `notes`, `recipe`\* (ref→recipe), `mealPlan`\* (ref→meal-plan) |
| Meal Occurrence | `meal-occurrence` | mp:MealOccurrence | `date`\* (format: date), `mealType`\* (enum), `servings` (number), `status`\* (enum: planned, cooked, skipped), `cookedAt` (format: date-time), `notes`, `scheduledMeal`\* (ref→scheduled-meal) |
| Pantry | `pantry` | mp:Pantry | `name`\*, `description`, `location`, `isDefault` (boolean) |
| Food Item | `food-item` | mp:FoodItem | `quantity`\* (number), `unit`\*, `storage` (enum), `purchaseDate` (format: date), `expirationDate` (format: date), `notes`, `ingredient`\* (ref→ingredient), `pantry`\* (ref→pantry) |
| Shopping List | `shopping-list` | mp:ShoppingList | `name`\*, `createdAt` (format: date-time), `status` (enum: draft, active, completed), `mealPlan` (ref→meal-plan), `pantry` (ref→pantry) |
| Shopping List Item | `shopping-list-item` | mp:ShoppingListItem | `quantity`\* (number), `unit`\*, `checked` (boolean), `notes`, `ingredient`\* (ref→ingredient), `shoppingList`\* (ref→shopping-list) |
| Restricted Diet | `restricted-diet` | schema:RestrictedDiet | `name`\*, `description`, `identifier` |

Behaviors: `pantry` (enforce single default), `scheduled-meal` (generate meal occurrences), `meal-occurrence` (deplete pantry on cook). Hidden from sidebar by default: how-to-step, recipe-ingredient, nutrition-information, meal-occurrence, food-item, shopping-list-item, restricted-diet.

---

\* = required property

## JSON Schema Extensions

| Extension | Purpose | Example |
|-----------|---------|---------|
| `x-resource-type` | Foreign key to another resource type | `"x-resource-type": "project"` |
| `x-display-property` | Which property of the referenced type to show | `"x-display-property": "name"` |
