// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package mealplanning

import (
	"encoding/json"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/pkg/jsonld"
)

// Register adds the meal-planning preset to the registry.
func Register(registry *application.PresetRegistry) {
	registry.MustAdd(application.PresetDefinition{
		Name:        "meal-planning",
		Description: "Recipe management, meal planning, pantry tracking, and shopping lists",
		Types: []application.PresetResourceType{
			recipeType(),
			howToStepType(),
			ingredientType(),
			recipeIngredientType(),
			nutritionInformationType(),
			cookbookType(),
			mealPlanType(),
			scheduledMealType(),
			mealOccurrenceType(),
			pantryType(),
			foodItemType(),
			shoppingListType(),
			shoppingListItemType(),
			restrictedDietType(),
			tasteProfileType(),
			mealLogType(),
			restaurantType(),
			plannedMealType(),
			groceryAmendmentType(),
			groceryListItemType(),
			stapleType(),
			purchaseType(),
			purchaseLineType(),
			itemKindType(),
		},
		Sidebar: &application.PresetSidebarConfig{
			HiddenSlugs: []string{
				"how-to-step", "recipe-ingredient", "nutrition-information",
				"meal-occurrence", "food-item", "shopping-list-item", "restricted-diet",
				"grocery-list-item", "grocery-amendment", "purchase-line",
			},
			MenuGroups: map[string]string{
				"scheduled-meal":     "meal-plan",
				"shopping-list-item": "shopping-list",
				"food-item":          "pantry",
			},
		},
		Behaviors: map[string]application.BehaviorFactory{
			"pantry": func(svc application.BehaviorServices) entities.ResourceBehavior {
				return newEnforceSingleDefaultBehavior(svc)
			},
			"scheduled-meal": func(svc application.BehaviorServices) entities.ResourceBehavior {
				return newScheduledMealBehavior(svc)
			},
			"meal-occurrence": func(svc application.BehaviorServices) entities.ResourceBehavior {
				return newDepletePantryOnCookBehavior(svc)
			},
		},
		BehaviorMeta: map[string]entities.BehaviorMeta{
			"pantry": {
				Slug:        "pantry",
				DisplayName: "Enforce Single Default Pantry",
				Description: "When a pantry is marked default, unsets isDefault on all others.",
				Default:     true,
				Manageable:  true,
			},
			"scheduled-meal": {
				Slug:        "scheduled-meal",
				DisplayName: "Generate Meal Occurrences",
				Description: "Expands recurring schedules into MealOccurrence resources and cascades on update/delete.",
				Default:     true,
				Manageable:  true,
			},
			"meal-occurrence": {
				Slug:        "meal-occurrence",
				DisplayName: "Deplete Pantry on Cook",
				Description: "Decrements pantry FoodItem quantities when an occurrence is marked cooked.",
				Default:     true,
				Manageable:  true,
			},
		},
	})
}

// -- contexts ----------------------------------------------------------------

// mpTypeContext returns a JSON-LD context for a meal-planning type whose
// @type is the custom mp:<typeName>. extraTerms is a JSON object fragment of
// additional term mappings (without surrounding braces) for types that have
// reference properties needing explicit predicate IRIs.
func mpTypeContext(typeName, extraTerms string) json.RawMessage {
	return mpContext("mp:"+typeName, extraTerms)
}

// schemaTypeContext returns a JSON-LD context whose @type is a schema.org type
// (e.g. "MealPlan", "Schedule", "Collection"). The mp: namespace and shared
// custom-term mappings are still declared so types can use mp: predicates.
func schemaTypeContext(schemaType, extraTerms string) json.RawMessage {
	return mpContext(schemaType, extraTerms)
}

// mpContext is the shared builder for both helpers.
func mpContext(typeIRI, extraTerms string) json.RawMessage {
	terms := `"@vocab":"https://schema.org/",` +
		`"mp":"` + jsonld.MealPlanningVocab + `",` +
		`"mealType":"mp:mealType",` +
		`"servings":"mp:servings",` +
		`"@type":"` + typeIRI + `"`
	if extraTerms != "" {
		terms += "," + extraTerms
	}
	return json.RawMessage("{" + terms + "}")
}

// -- type constructors -------------------------------------------------------

func recipeType() application.PresetResourceType {
	return application.PresetResourceType{
		Name:        "Recipe",
		Slug:        "recipe",
		Description: "A food recipe with ingredients, steps, and nutritional information",
		Context: json.RawMessage(`{
	"@vocab":"https://schema.org/","@type":"Recipe",
	"mp":"` + jsonld.MealPlanningVocab + `",
	"recipeInstructions":"https://schema.org/recipeInstructions",
	"recipeIngredient":"https://schema.org/recipeIngredient",
	"nutrition":"https://schema.org/nutrition",
	"suitableForDiet":"https://schema.org/suitableForDiet"
}`),
		Schema: json.RawMessage(`{
	"type":"object",
	"properties":{
		"name":{"type":"string"},
		"description":{"type":"string"},
		"recipeYield":{
			"type":"object",
			"description":"Structured yield for scaling/shopping-list math",
			"properties":{
				"@type":{"type":"string","enum":["QuantitativeValue"]},
				"value":{"type":"number"},
				"unitText":{"type":"string"}
			},
			"required":["value","unitText"]
		},
		"prepTime":{"type":"string","description":"ISO 8601 duration, e.g. PT15M"},
		"cookTime":{"type":"string","description":"ISO 8601 duration"},
		"totalTime":{"type":"string","description":"ISO 8601 duration"},
		"recipeCuisine":{"type":"string"},
		"recipeCategory":{"type":"string"},
		"keywords":{"type":"array","items":{"type":"string"}},
		"suitableForDiet":{"type":"array",
			"x-resource-type":"restricted-diet",
			"x-display-property":"name",
			"items":{"type":"string"}},
		"recipeInstructions":{"type":"array",
			"x-resource-type":"how-to-step",
			"x-display-property":"text",
			"items":{"type":"string"}},
		"recipeIngredient":{"type":"array",
			"x-resource-type":"recipe-ingredient",
			"x-display-property":"unit",
			"items":{"type":"string"}},
		"nutrition":{"type":"string",
			"x-resource-type":"nutrition-information",
			"x-display-property":"servingSize"},
		"image":{"type":"string","format":"uri"}
	},
	"required":["name"]
}`),
	}
}

func howToStepType() application.PresetResourceType {
	return application.PresetResourceType{
		Name:        "How-To Step",
		Slug:        "how-to-step",
		Description: "A single step in a recipe's instructions",
		Context: json.RawMessage(`{
	"@vocab":"https://schema.org/","@type":"HowToStep",
	"mp":"` + jsonld.MealPlanningVocab + `",
	"recipe":"https://schema.org/isPartOf"
}`),
		Schema: json.RawMessage(`{
	"type":"object",
	"properties":{
		"position":{"type":"integer"},
		"text":{"type":"string"},
		"image":{"type":"string","format":"uri"},
		"recipe":{"type":"string","x-resource-type":"recipe","x-display-property":"name"}
	},
	"required":["position","text","recipe"]
}`),
	}
}

func ingredientType() application.PresetResourceType {
	return application.PresetResourceType{
		Name:        "Ingredient",
		Slug:        "ingredient",
		Description: "A type-level food ingredient (e.g. garlic, chicken breast)",
		Context: json.RawMessage(`{
	"@vocab":"https://schema.org/",
	"@type":"fo:Food",
	"fo":"http://purl.org/foodontology#",
	"skos":"http://www.w3.org/2004/02/skos/core#",
	"suitableForDiet":"https://schema.org/suitableForDiet",
	"mp":"` + jsonld.MealPlanningVocab + `",
	"alternateNames":"skos:altLabel",
	"shoppingCategory":"mp:shoppingCategory",
	"season":"mp:season",
	"defaultUnit":"mp:defaultUnit"
}`),
		Schema: json.RawMessage(`{
	"type":"object",
	"properties":{
		"name":{"type":"string"},
		"description":{"type":"string"},
		"alternateNames":{"type":"array","items":{"type":"string"}},
		"shoppingCategory":{"type":"string","enum":[
			"produce","meat","seafood","dairy","bakery","pantry",
			"frozen","beverages","condiments","spices","other"]},
		"season":{"type":"array","items":{"type":"string","enum":["spring","summer","autumn","winter"]}},
		"suitableForDiet":{"type":"array",
			"x-resource-type":"restricted-diet",
			"x-display-property":"name",
			"items":{"type":"string"}},
		"defaultUnit":{"type":"string"},
		"image":{"type":"string","format":"uri"}
	},
	"required":["name"]
}`),
		Fixtures: ingredientFixtures(),
	}
}

func recipeIngredientType() application.PresetResourceType {
	return application.PresetResourceType{
		Name:        "Recipe Ingredient",
		Slug:        "recipe-ingredient",
		Description: "A reified relation linking a recipe to an ingredient with quantity and preparation",
		Context: mpTypeContext("RecipeIngredient",
			`"recipe":"mp:recipe","ingredient":"mp:ingredient",`+
				`"quantity":"mp:quantity","unit":"mp:unit",`+
				`"optional":"mp:optional","preparation":"mp:preparation"`),
		Schema: json.RawMessage(`{
	"type":"object",
	"properties":{
		"quantity":{"type":"number"},
		"unit":{"type":"string"},
		"preparation":{"type":"string"},
		"optional":{"type":"boolean"},
		"recipe":{"type":"string","x-resource-type":"recipe","x-display-property":"name"},
		"ingredient":{"type":"string","x-resource-type":"ingredient","x-display-property":"name"}
	},
	"required":["quantity","unit","recipe","ingredient"]
}`),
	}
}

func nutritionInformationType() application.PresetResourceType {
	return application.PresetResourceType{
		Name:        "Nutrition Information",
		Slug:        "nutrition-information",
		Description: "Nutritional data for a recipe or ingredient",
		Context: json.RawMessage(`{
	"@vocab":"https://schema.org/","@type":"NutritionInformation",
	"recipe":"https://schema.org/isPartOf",
	"ingredient":"https://schema.org/about"
}`),
		Schema: json.RawMessage(`{
	"type":"object",
	"properties":{
		"servingSize":{"type":"string"},
		"calories":{"type":"string"},
		"proteinContent":{"type":"string"},
		"carbohydrateContent":{"type":"string"},
		"fatContent":{"type":"string"},
		"saturatedFatContent":{"type":"string"},
		"fiberContent":{"type":"string"},
		"sugarContent":{"type":"string"},
		"sodiumContent":{"type":"string"},
		"recipe":{"type":"string","x-resource-type":"recipe","x-display-property":"name"},
		"ingredient":{"type":"string","x-resource-type":"ingredient","x-display-property":"name"}
	},
	"required":["servingSize"],
	"anyOf":[
		{"required":["recipe"]},
		{"required":["ingredient"]}
	]
}`),
	}
}

func cookbookType() application.PresetResourceType {
	return application.PresetResourceType{
		Name:        "Cookbook",
		Slug:        "cookbook",
		Description: "A named collection of recipes",
		Context: schemaTypeContext("Collection",
			`"recipes":"https://schema.org/hasPart"`),
		Schema: json.RawMessage(`{
	"type":"object",
	"properties":{
		"name":{"type":"string"},
		"description":{"type":"string"},
		"image":{"type":"string","format":"uri"},
		"keywords":{"type":"array","items":{"type":"string"}},
		"recipes":{"type":"array",
			"x-resource-type":"recipe","x-display-property":"name",
			"items":{"type":"string"}}
	},
	"required":["name"]
}`),
	}
}

func mealPlanType() application.PresetResourceType {
	return application.PresetResourceType{
		Name:        "Meal Plan",
		Slug:        "meal-plan",
		Description: "A weekly or custom-period meal plan",
		Context: schemaTypeContext("MealPlan",
			`"suitableForDiet":"https://schema.org/suitableForDiet"`),
		Schema: json.RawMessage(`{
	"type":"object",
	"properties":{
		"name":{"type":"string"},
		"description":{"type":"string"},
		"startDate":{"type":"string","format":"date"},
		"endDate":{"type":"string","format":"date"},
		"suitableForDiet":{"type":"array",
			"x-resource-type":"restricted-diet",
			"x-display-property":"name",
			"items":{"type":"string"}}
	},
	"required":["name","startDate","endDate"]
}`),
	}
}

func scheduledMealType() application.PresetResourceType {
	return application.PresetResourceType{
		Name:        "Scheduled Meal",
		Slug:        "scheduled-meal",
		Description: "A scheduled (possibly recurring) meal within a meal plan",
		Context: schemaTypeContext("Schedule",
			`"recipe":"mp:recipe","mealPlan":"mp:mealPlan",`+
				`"notes":"https://schema.org/description"`),
		Schema: json.RawMessage(`{
	"type":"object",
	"properties":{
		"startDate":{"type":"string","format":"date"},
		"endDate":{"type":"string","format":"date"},
		"startTime":{"type":"string"},
		"endTime":{"type":"string"},
		"duration":{"type":"string","description":"ISO 8601 duration"},
		"repeatFrequency":{"type":"string","description":"ISO 8601 duration, e.g. P1W for weekly"},
		"repeatCount":{"type":"integer"},
		"byDay":{"type":"array","items":{"type":"string","enum":[
			"Monday","Tuesday","Wednesday","Thursday",
			"Friday","Saturday","Sunday"]}},
		"byMonth":{"type":"array","items":{"type":"integer","minimum":1,"maximum":12}},
		"byMonthDay":{"type":"array","items":{"type":"integer","minimum":1,"maximum":31}},
		"exceptDate":{"type":"array","items":{"type":"string","format":"date"}},
		"scheduleTimezone":{"type":"string"},
		"mealType":{"type":"string","enum":["breakfast","lunch","dinner","snack"]},
		"servings":{"type":"number"},
		"notes":{"type":"string"},
		"recipe":{"type":"string","x-resource-type":"recipe","x-display-property":"name"},
		"mealPlan":{"type":"string","x-resource-type":"meal-plan","x-display-property":"name"}
	},
	"required":["startDate","mealType","recipe","mealPlan"]
}`),
	}
}

func mealOccurrenceType() application.PresetResourceType {
	return application.PresetResourceType{
		Name:        "Meal Occurrence",
		Slug:        "meal-occurrence",
		Description: "A concrete single-date instance of a meal: one expanded from a schedule, or one eaten ad hoc",
		Context: mpTypeContext("MealOccurrence",
			`"scheduledMeal":"mp:occurrenceOf",`+
				`"notes":"https://schema.org/description",`+
				`"date":"https://schema.org/startDate",`+
				`"cookedAt":"mp:cookedAt","status":"mp:status"`),
		Schema: json.RawMessage(`{
	"type":"object",
	"properties":{
		"date":{"type":"string","format":"date"},
		"mealType":{"type":"string","enum":["breakfast","lunch","dinner","snack"]},
		"servings":{"type":"number"},
		"status":{"type":"string","enum":["planned","cooked","skipped"]},
		"cookedAt":{"type":"string","format":"date-time"},
		"notes":{"type":"string"},
		"scheduledMeal":{"type":"string","x-resource-type":"scheduled-meal","x-display-property":"mealType"}
	},
	"required":["date","mealType","status"]
}`),
	}
}

func pantryType() application.PresetResourceType {
	return application.PresetResourceType{
		Name:        "Pantry",
		Slug:        "pantry",
		Description: "A named storage context for food items (e.g. Home, Beach House)",
		Context:     mpTypeContext("Pantry", `"isDefault":"mp:isDefault"`),
		Schema: json.RawMessage(`{
	"type":"object",
	"properties":{
		"name":{"type":"string"},
		"description":{"type":"string"},
		"location":{"type":"string"},
		"isDefault":{"type":"boolean"}
	},
	"required":["name"]
}`),
	}
}

func foodItemType() application.PresetResourceType {
	return application.PresetResourceType{
		Name:        "Food Item",
		Slug:        "food-item",
		Description: "A physical food item in a pantry (instance of an ingredient)",
		Context: mpTypeContext("FoodItem",
			`"ingredient":"mp:isInstanceOf","pantry":"mp:pantry",`+
				`"notes":"https://schema.org/description",`+
				`"quantity":"mp:quantity","unit":"mp:unit",`+
				`"storage":"mp:storage","expirationDate":"mp:expirationDate"`),
		Schema: json.RawMessage(`{
	"type":"object",
	"properties":{
		"quantity":{"type":"number"},
		"unit":{"type":"string"},
		"storage":{"type":"string","enum":["pantry","fridge","freezer","other"]},
		"purchaseDate":{"type":"string","format":"date"},
		"expirationDate":{"type":"string","format":"date"},
		"notes":{"type":"string"},
		"ingredient":{"type":"string","x-resource-type":"ingredient","x-display-property":"name"},
		"pantry":{"type":"string","x-resource-type":"pantry","x-display-property":"name"}
	},
	"required":["quantity","unit","ingredient","pantry"]
}`),
	}
}

func shoppingListType() application.PresetResourceType {
	return application.PresetResourceType{
		Name:        "Shopping List",
		Slug:        "shopping-list",
		Description: "A grocery shopping list, optionally derived from a meal plan",
		Context: mpTypeContext("ShoppingList",
			`"mealPlan":"http://www.w3.org/ns/prov#wasDerivedFrom",`+
				`"pantry":"mp:targetsPantry",`+
				`"createdAt":"mp:createdAt","status":"mp:status"`),
		Schema: json.RawMessage(`{
	"type":"object",
	"properties":{
		"name":{"type":"string"},
		"createdAt":{"type":"string","format":"date-time"},
		"status":{"type":"string","enum":["draft","active","completed"]},
		"mealPlan":{"type":"string","x-resource-type":"meal-plan","x-display-property":"name"},
		"pantry":{"type":"string","x-resource-type":"pantry","x-display-property":"name"}
	},
	"required":["name"]
}`),
	}
}

func shoppingListItemType() application.PresetResourceType {
	return application.PresetResourceType{
		Name:        "Shopping List Item",
		Slug:        "shopping-list-item",
		Description: "A line item on a shopping list",
		Context: mpTypeContext("ShoppingListItem",
			`"ingredient":"mp:ingredient","shoppingList":"mp:hasItem",`+
				`"notes":"https://schema.org/description",`+
				`"quantity":"mp:quantity","unit":"mp:unit",`+
				`"checked":"mp:checked"`),
		Schema: json.RawMessage(`{
	"type":"object",
	"properties":{
		"quantity":{"type":"number"},
		"unit":{"type":"string"},
		"checked":{"type":"boolean"},
		"notes":{"type":"string"},
		"ingredient":{"type":"string","x-resource-type":"ingredient","x-display-property":"name"},
		"shoppingList":{"type":"string","x-resource-type":"shopping-list","x-display-property":"name"}
	},
	"required":["quantity","unit","ingredient","shoppingList"]
}`),
	}
}

func restrictedDietType() application.PresetResourceType {
	return application.PresetResourceType{
		Name:        "Restricted Diet",
		Slug:        "restricted-diet",
		Description: "A dietary restriction (e.g. gluten-free, vegan)",
		Context:     json.RawMessage(`{"@vocab":"https://schema.org/","@type":"RestrictedDiet"}`),
		Schema: json.RawMessage(`{
	"type":"object",
	"properties":{
		"name":{"type":"string"},
		"description":{"type":"string"},
		"identifier":{"type":"string","description":"Schema.org RestrictedDiet identifier"}
	},
	"required":["name"]
}`),
		Fixtures: restrictedDietFixtures(),
	}
}

// -- food types ----------------------------------------------------------------
//
// A stored resource type is keyed by slug alone, and live twins already store
// these ten under mini-me's definitions. Every context, schema, name and
// description below must stay byte-identical to that stored copy, or the first
// boot on this build records a type change for every twin. food_types_test.go
// compares them with a golden copy of mini-me's output; change one only
// together with that golden copy and the product that stores it.

const (
	schemaNS       = "https://schema.org/"
	mealPlanningNS = jsonld.MealPlanningVocab
	ingestNS       = jsonld.IngestVocab
)

func tasteProfileType() application.PresetResourceType {
	return application.NewPresetType("TasteProfile", "taste-profile",
		"A person's food preferences: cuisines and ingredients liked, disliked, dietary rules, never-eat rules, and per-context budget caps",
		`{"@vocab":"`+schemaNS+`","@type":"`+mealPlanningNS+`TasteProfile",`+
			`"name":"`+schemaNS+`name",`+
			`"likes":"`+mealPlanningNS+`likes",`+
			`"dislikes":"`+mealPlanningNS+`dislikes",`+
			`"diet":"`+mealPlanningNS+`diet",`+
			`"neverEat":"`+mealPlanningNS+`neverEat",`+
			`"budgetCaps":"`+mealPlanningNS+`budgetCaps",`+
			`"notes":"`+schemaNS+`description"}`,
		`{"type":"object","properties":{`+
			`"name":{"type":"string"},`+
			`"likes":{"type":"array","items":{"type":"string"}},`+
			`"dislikes":{"type":"array","items":{"type":"string"}},`+
			`"diet":{"type":"array","items":{"type":"string"}},`+
			`"neverEat":{"type":"array","items":{"type":"string"}},`+
			`"budgetCaps":{"type":"array","items":{"type":"object","properties":{`+
			`"context":{"type":"string"},`+
			`"amount":{"type":"number","minimum":0},`+
			`"currency":{"type":"string"}`+
			`},"required":["context","amount"]}},`+
			`"notes":{"type":"string"}`+
			`},"required":["name"]}`,
	)
}

// mealLogType is a twin of meal-occurrence: both carry mp:MealOccurrence, but
// meal-log keeps its own slug and table for the order edge and rating that
// meal-occurrence has no notion of. Merging the pair is P1 and needs a data
// migration; do not fold either type into the other before then.
//
// status is not required because logs written before it existed have none, and
// the boot reconcile tightens `required` on existing rows at once. orderId is
// not named `order`, a SQL reserved word that breaks the projection table. Its
// target type `order` is defined only by the private commerce preset, so a
// core-only install has nothing for it to point at. scheduledMeal targets
// planned-meal, the slug the twin writes, because x-resource-type drives the
// display column.
func mealLogType() application.PresetResourceType {
	return application.NewPresetType("MealLog", "meal-log",
		"One meal Akeem ate: when, what kind, and the recipe cooked or order placed",
		`{"@vocab":"`+schemaNS+`","@type":"`+mealPlanningNS+`MealOccurrence",`+
			`"date":"`+schemaNS+`startDate",`+
			`"mealType":"`+mealPlanningNS+`mealType",`+
			`"servings":"`+mealPlanningNS+`servings",`+
			`"orderId":"`+schemaNS+`isBasedOn",`+
			`"recipe":"`+mealPlanningNS+`recipe",`+
			`"scheduledMeal":"`+mealPlanningNS+`occurrenceOf",`+
			`"status":"`+mealPlanningNS+`status",`+
			`"notes":"`+schemaNS+`description",`+
			`"rating":"`+mealPlanningNS+`rating"}`,
		`{"type":"object","properties":{`+
			`"date":{"type":"string","format":"date"},`+
			`"mealType":{"type":"string","enum":["breakfast","lunch","dinner","snack"]},`+
			`"status":{"type":"string","enum":["planned","cooked","skipped"]},`+
			`"recipe":{"type":"string","x-resource-type":"recipe","x-display-property":"name"},`+
			`"orderId":{"type":"string","x-resource-type":"order","x-display-property":"orderNumber"},`+
			`"scheduledMeal":{"type":"string","x-resource-type":"planned-meal","x-display-property":"mealType"},`+
			`"servings":{"type":"integer","minimum":1},`+
			`"notes":{"type":"string"},`+
			`"rating":{"type":"integer","minimum":1,"maximum":5}`+
			`},"required":["date","mealType"]}`,
	)
}

// restaurantType subclasses `agent`, which only the private finance preset
// defines, so a core-only install gets no parent edge. The parent stays a plain
// string because it sits inside the byte-identical context.
func restaurantType() application.PresetResourceType {
	return application.NewPresetType("Restaurant", "restaurant",
		"A restaurant Akeem orders from, standing as the providing agent on orders and invoices",
		`{"@vocab":"`+schemaNS+`","@type":"Restaurant","rdfs:subClassOf":"agent"}`,
		`{"type":"object","properties":{`+
			`"name":{"type":"string"},`+
			`"servesCuisine":{"type":"string"},`+
			`"address":{"type":"string"},`+
			`"telephone":{"type":"string"},`+
			`"url":{"type":"string"}`+
			`},"required":["name"]}`,
	)
}

// plannedMealType is a twin of scheduled-meal: both carry schema:Schedule, but
// planned-meal keeps its own slug and table. Merging the pair is P1 and needs a
// data migration; do not fold either type into the other before then.
func plannedMealType() application.PresetResourceType {
	return application.NewPresetType("PlannedMeal", "planned-meal",
		"A recipe planned for an upcoming meal, with an optional headcount that scales its grocery needs",
		`{"@vocab":"`+schemaNS+`","@type":"`+schemaNS+`Schedule",`+
			`"recipe":"`+mealPlanningNS+`recipe",`+
			`"people":"`+mealPlanningNS+`people",`+
			`"scaleFactor":"`+mealPlanningNS+`scaleFactor",`+
			`"plannedFor":"`+mealPlanningNS+`plannedFor"}`,
		`{"type":"object","properties":{`+
			`"recipe":{"type":"string","x-resource-type":"recipe","x-display-property":"name"},`+
			`"people":{"type":"integer","minimum":1},`+
			`"scaleFactor":{"type":"number","minimum":0},`+
			`"plannedFor":{"type":"string","format":"date"}`+
			`},"required":["recipe"]}`,
	)
}

// groceryAmendmentType maps kind to mp:amendmentKind rather than mp:kind, so it
// never shares a predicate with item-kind's unrelated kind.
func groceryAmendmentType() application.PresetResourceType {
	return application.NewPresetType("GroceryAmendment", "grocery-amendment",
		"A manual change to the grocery list: an add of an ingredient, or a check-off of one already on it",
		`{"@vocab":"`+schemaNS+`","@type":"`+mealPlanningNS+`ShoppingListAmendment",`+
			`"ingredient":"`+mealPlanningNS+`ingredient",`+
			`"kind":"`+mealPlanningNS+`amendmentKind"}`,
		`{"type":"object","properties":{`+
			`"kind":{"type":"string","enum":["add","check-off"]},`+
			`"ingredient":{"type":"string","x-resource-type":"ingredient","x-display-property":"name"}`+
			`},"required":["kind","ingredient"]}`,
	)
}

// groceryListItemType is a twin of shopping-list-item: both carry
// mp:ShoppingListItem, and the properties they share use shopping-list-item's
// predicates so the two answer one query. grocery-list-item keeps its own slug
// and table. Merging the pair is P1 and needs a data migration; do not fold
// either type into the other before then.
func groceryListItemType() application.PresetResourceType {
	return application.NewPresetType("GroceryListItem", "grocery-list-item",
		"One materialized line on the grocery list: an ingredient to get, an optional combined quantity, and the per-recipe component needs behind it",
		`{"@vocab":"`+schemaNS+`","@type":"`+mealPlanningNS+`ShoppingListItem",`+
			`"ingredient":"`+mealPlanningNS+`ingredient",`+
			`"quantity":"`+mealPlanningNS+`quantity",`+
			`"unit":"`+mealPlanningNS+`unit",`+
			`"manuallyAdded":"`+mealPlanningNS+`manuallyAdded",`+
			`"stapleLow":"`+mealPlanningNS+`stapleLow",`+
			`"components":"`+mealPlanningNS+`components"}`,
		`{"type":"object","properties":{`+
			`"ingredient":{"type":"string","x-resource-type":"ingredient","x-display-property":"name"},`+
			`"quantity":{"type":"number"},`+
			`"unit":{"type":"string"},`+
			`"manuallyAdded":{"type":"boolean"},`+
			`"stapleLow":{"type":"boolean"},`+
			`"components":{"type":"array","items":{"type":"object","properties":{`+
			`"quantity":{"type":"number"},`+
			`"unit":{"type":"string"},`+
			`"recipe":{"type":"string"}`+
			`}}}`+
			`},"required":["ingredient","manuallyAdded"]}`,
	)
}

func stapleType() application.PresetResourceType {
	return application.NewPresetType("Staple", "staple",
		"A staple ingredient Akeem always keeps stocked; running low is judged against the current pantry declaration",
		`{"@vocab":"`+schemaNS+`","@type":"`+mealPlanningNS+`Staple",`+
			`"ingredient":"`+mealPlanningNS+`ingredient"}`,
		`{"type":"object","properties":{`+
			`"ingredient":{"type":"string","x-resource-type":"ingredient","x-display-property":"name"}`+
			`},"required":["ingredient"]}`,
	)
}

// purchaseType declares the parent the commerce preset declares for
// schema:Order. The ontology projector clears a class subject before writing
// it, so a different parent here would change the class hierarchy between
// boots. `agreement` is defined only by the private presets, so a core-only
// install gets no parent edge.
func purchaseType() application.PresetResourceType {
	return application.NewPresetType("Purchase", "purchase",
		"One confirmed grocery purchase: the store, when it happened (and how that time is known), the printed total, and the dedup identity of its receipt",
		`{"@vocab":"`+schemaNS+`","@type":"`+schemaNS+`Order",`+
			`"rdfs:subClassOf":"agreement",`+
			`"seller":"`+schemaNS+`seller",`+
			`"store":"`+mealPlanningNS+`store",`+
			`"purchasedAt":"`+mealPlanningNS+`purchasedAt",`+
			`"timeSource":"`+mealPlanningNS+`timeSource",`+
			`"total":"`+mealPlanningNS+`total",`+
			`"contentHash":"`+ingestNS+`contentHash"}`,
		`{"type":"object","properties":{`+
			`"store":{"type":"string"},`+
			`"seller":{"type":"string","x-resource-type":"organization","x-display-property":"name"},`+
			`"purchasedAt":{"type":"string"},`+
			`"timeSource":{"type":"string","enum":["receipt","share"]},`+
			`"total":{"type":"string"},`+
			`"contentHash":{"type":"string"}`+
			`},"required":["store","purchasedAt","timeSource"]}`,
	)
}

func purchaseLineType() application.PresetResourceType {
	return application.NewPresetType("PurchaseLine", "purchase-line",
		"One line item of a confirmed purchase: the item as printed, its prices, and the vocabulary node it names when known",
		`{"@vocab":"`+schemaNS+`","@type":"`+schemaNS+`OrderItem",`+
			`"name":"`+schemaNS+`name",`+
			`"purchase":"`+schemaNS+`isPartOf",`+
			`"ingredient":"`+mealPlanningNS+`ingredient",`+
			`"quantity":"`+mealPlanningNS+`quantity",`+
			`"unitPrice":"`+mealPlanningNS+`unitPrice",`+
			`"lineTotal":"`+mealPlanningNS+`lineTotal"}`,
		`{"type":"object","properties":{`+
			`"name":{"type":"string"},`+
			`"purchase":{"type":"string","x-resource-type":"purchase","x-display-property":"store"},`+
			`"quantity":{"type":"number"},`+
			`"unitPrice":{"type":"string"},`+
			`"lineTotal":{"type":"string"},`+
			`"ingredient":{"type":"string","x-resource-type":"ingredient","x-display-property":"name"}`+
			`},"required":["name","purchase"]}`,
	)
}

func itemKindType() application.PresetResourceType {
	return application.NewPresetType("ItemKind", "item-kind",
		"One item's declared perishability: perishable or non-perishable, at the purchase-line name grain",
		`{"@vocab":"`+schemaNS+`","@type":"`+schemaNS+`DefinedTerm",`+
			`"name":"`+schemaNS+`name",`+
			`"kind":"`+mealPlanningNS+`perishability"}`,
		`{"type":"object","properties":{`+
			`"name":{"type":"string"},`+
			`"kind":{"type":"string","enum":["perishable","non-perishable"]}`+
			`},"required":["name","kind"]}`,
	)
}

// -- fixtures ----------------------------------------------------------------

func restrictedDietFixtures() []json.RawMessage {
	return []json.RawMessage{
		json.RawMessage(`{"name":"Diabetic Diet","identifier":"https://schema.org/DiabeticDiet"}`),
		json.RawMessage(`{"name":"Gluten-Free Diet","identifier":"https://schema.org/GlutenFreeDiet"}`),
		json.RawMessage(`{"name":"Halal Diet","identifier":"https://schema.org/HalalDiet"}`),
		json.RawMessage(`{"name":"Hindu Diet","identifier":"https://schema.org/HinduDiet"}`),
		json.RawMessage(`{"name":"Kosher Diet","identifier":"https://schema.org/KosherDiet"}`),
		json.RawMessage(`{"name":"Low Calorie Diet","identifier":"https://schema.org/LowCalorieDiet"}`),
		json.RawMessage(`{"name":"Low Fat Diet","identifier":"https://schema.org/LowFatDiet"}`),
		json.RawMessage(`{"name":"Low Lactose Diet","identifier":"https://schema.org/LowLactoseDiet"}`),
		json.RawMessage(`{"name":"Low Salt Diet","identifier":"https://schema.org/LowSaltDiet"}`),
		json.RawMessage(`{"name":"Vegan Diet","identifier":"https://schema.org/VeganDiet"}`),
		json.RawMessage(`{"name":"Vegetarian Diet","identifier":"https://schema.org/VegetarianDiet"}`),
	}
}

func ingredientFixtures() []json.RawMessage {
	return []json.RawMessage{
		json.RawMessage(`{"name":"Salt","shoppingCategory":"spices","defaultUnit":"tsp"}`),
		json.RawMessage(`{"name":"Black Pepper","shoppingCategory":"spices","defaultUnit":"tsp"}`),
		json.RawMessage(`{"name":"Olive Oil","shoppingCategory":"condiments","defaultUnit":"tbsp"}`),
		json.RawMessage(`{"name":"Butter","shoppingCategory":"dairy","defaultUnit":"tbsp"}`),
		json.RawMessage(`{"name":"Garlic","shoppingCategory":"produce","defaultUnit":"clove"}`),
		json.RawMessage(`{"name":"Onion","shoppingCategory":"produce","defaultUnit":"each"}`),
		json.RawMessage(`{"name":"Tomato","shoppingCategory":"produce","defaultUnit":"each"}`),
		json.RawMessage(`{"name":"Chicken Breast","shoppingCategory":"meat","defaultUnit":"g"}`),
		json.RawMessage(`{"name":"Ground Beef","shoppingCategory":"meat","defaultUnit":"g"}`),
		json.RawMessage(`{"name":"Egg","shoppingCategory":"dairy","defaultUnit":"each"}`),
		json.RawMessage(`{"name":"Milk","shoppingCategory":"dairy","defaultUnit":"ml"}`),
		json.RawMessage(`{"name":"All-Purpose Flour","shoppingCategory":"pantry","defaultUnit":"g"}`),
		json.RawMessage(`{"name":"Sugar","shoppingCategory":"pantry","defaultUnit":"g"}`),
		json.RawMessage(`{"name":"Rice","shoppingCategory":"pantry","defaultUnit":"g"}`),
		json.RawMessage(`{"name":"Pasta","shoppingCategory":"pantry","defaultUnit":"g"}`),
		json.RawMessage(`{"name":"Carrot","shoppingCategory":"produce","defaultUnit":"each"}`),
		json.RawMessage(`{"name":"Potato","shoppingCategory":"produce","defaultUnit":"each"}`),
		json.RawMessage(`{"name":"Lemon","shoppingCategory":"produce","defaultUnit":"each"}`),
		json.RawMessage(`{"name":"Cheese","shoppingCategory":"dairy","defaultUnit":"g"}`),
		json.RawMessage(`{"name":"Soy Sauce","shoppingCategory":"condiments","defaultUnit":"tbsp"}`),
	}
}
