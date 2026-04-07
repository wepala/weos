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

	"weos/application"
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
		},
		Sidebar: &application.PresetSidebarConfig{
			HiddenSlugs: []string{
				"how-to-step", "recipe-ingredient", "nutrition-information",
				"meal-occurrence", "food-item", "shopping-list-item", "restricted-diet",
			},
			MenuGroups: map[string]string{
				"scheduled-meal":     "meal-plan",
				"shopping-list-item": "shopping-list",
				"food-item":          "pantry",
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
		`"mp":"https://weos.org/vocab/meal-planning#",` +
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
	"mp":"https://weos.org/vocab/meal-planning#",
	"fo":"http://purl.org/foodontology#",
	"recipeInstructions":"https://schema.org/recipeInstructions",
	"recipeIngredient":"fo:hasIngredient",
	"nutrition":"https://schema.org/nutrition",
	"suitableForDiet":"https://schema.org/suitableForDiet"
}`),
		Schema: json.RawMessage(`{
	"type":"object",
	"properties":{
		"name":{"type":"string"},
		"description":{"type":"string"},
		"recipeYield":{"type":"string"},
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
	"mp":"https://weos.org/vocab/meal-planning#",
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
	"mp":"https://weos.org/vocab/meal-planning#",
	"alternateNames":"skos:altLabel",
	"shoppingCategory":"fo:ShoppingCategory",
	"season":"fo:at_its_best",
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
			`"recipe":"mp:recipe","ingredient":"fo:ingredient",`+
				`"fo":"http://purl.org/foodontology#"`),
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
		Context:     schemaTypeContext("MealPlan", ""),
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
			`"recipe":"mp:recipe","mealPlan":"mp:mealPlan"`),
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
		Description: "A concrete single-date instance of a scheduled meal",
		Context: mpTypeContext("MealOccurrence",
			`"scheduledMeal":"mp:occurrenceOf"`),
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
	"required":["date","mealType","status","scheduledMeal"]
}`),
	}
}

func pantryType() application.PresetResourceType {
	return application.PresetResourceType{
		Name:        "Pantry",
		Slug:        "pantry",
		Description: "A named storage context for food items (e.g. Home, Beach House)",
		Context:     mpTypeContext("Pantry", ""),
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
			`"ingredient":"mp:isInstanceOf","pantry":"mp:pantry"`),
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
				`"pantry":"mp:targetsPantry"`),
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
			`"ingredient":"mp:ingredient","shoppingList":"mp:hasItem"`),
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
