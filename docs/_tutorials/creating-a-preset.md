---
title: Creating a Preset
parent: Tutorials
layout: default
nav_order: 2
---

# Creating a Preset

Presets are bundles of related resource types that you can install with a single command. WeOS ships with built-in presets like `website`, `ecommerce`, and `tasks`. In this tutorial you'll create your own preset — a "restaurant" preset with Menu and MenuItem types — and install it.

## Prerequisites

- WeOS built and running (see [Running WeOS]({% link _tutorials/running-weos.md %}))
- Basic familiarity with Go

## What You'll Build

A `restaurant` preset containing:
- **Menu** — represents a restaurant menu (e.g., "Lunch Menu", "Drinks")
- **MenuItem** — an individual dish or drink on a menu

Each type will have a JSON-LD context (for semantic meaning) and a JSON Schema (for validation and projection table columns).

## Step 1: Create the Preset Package

Create a new directory for your preset:

```bash
mkdir -p application/presets/restaurant
```

Create `application/presets/restaurant/preset.go`:

```go
package restaurant

import (
    "encoding/json"

    "github.com/wepala/weos/application"
)

func Register(registry *application.PresetRegistry) {
    registry.Register(application.Preset{
        Name:        "restaurant",
        Description: "Restaurant menus and menu items",
        AutoInstall: false,
        Types: []application.PresetType{
            menuType(),
            menuItemType(),
        },
    })
}

func menuType() application.PresetType {
    return application.PresetType{
        Name:        "Menu",
        Slug:        "menu",
        Description: "A restaurant menu grouping (e.g., Lunch, Dinner, Drinks)",
        Context: json.RawMessage(`{
            "@vocab": "https://schema.org/",
            "@type": "Menu"
        }`),
        Schema: json.RawMessage(`{
            "type": "object",
            "properties": {
                "name":        {"type": "string", "description": "Menu name"},
                "description": {"type": "string", "description": "Menu description"},
                "availability": {"type": "string", "description": "When this menu is available"}
            },
            "required": ["name"]
        }`),
    }
}

func menuItemType() application.PresetType {
    return application.PresetType{
        Name:        "Menu Item",
        Slug:        "menu-item",
        Description: "An individual dish or drink",
        Context: json.RawMessage(`{
            "@vocab": "https://schema.org/",
            "@type": "MenuItem"
        }`),
        Schema: json.RawMessage(`{
            "type": "object",
            "properties": {
                "name":        {"type": "string", "description": "Dish name"},
                "description": {"type": "string", "description": "Dish description"},
                "price":       {"type": "number", "description": "Price in local currency"},
                "category":    {"type": "string", "description": "Category (appetizer, main, dessert, drink)"},
                "image":       {"type": "string", "format": "uri", "description": "Photo URL"},
                "menu":        {
                    "type": "string",
                    "x-resource-type": "menu",
                    "x-display-property": "name",
                    "description": "Which menu this item belongs to"
                }
            },
            "required": ["name", "price"]
        }`),
    }
}
```

### Key Points

- **`@vocab`** sets the default vocabulary to Schema.org, so property names like `name` and `description` automatically map to `schema:name`, `schema:description`.
- **`@type`** declares the RDF type. LLMs and search engines understand this semantic metadata.
- **`x-resource-type`** on the `menu` property tells WeOS this field is a foreign key referencing the `menu` resource type. WeOS will store the menu's ID and create a display column (`menu_display`) with the menu's name.
- **`required`** lists fields that must be present when creating a resource.

## Step 2: Register the Preset

Open `application/presets/register.go` and add your preset to the `RegisterAll` function:

```go
import "github.com/wepala/weos/application/presets/restaurant"

func RegisterAll(registry *application.PresetRegistry) {
    core.Register(registry)
    auth.Register(registry)
    ecommerce.Register(registry)
    tasks.Register(registry)
    website.Register(registry)
    events.Register(registry)
    knowledge.Register(registry)
    restaurant.Register(registry) // Add this line
}
```

## Step 3: Rebuild and Install

Rebuild the binary to include your new preset:

```bash
make build
```

Install it:

```bash
./bin/weos resource-type preset install restaurant
```

You should see output showing that the `menu` and `menu-item` types were created.

## Step 4: Verify

List resource types to confirm they exist:

```bash
./bin/weos resource-type list
```

Create a menu and a menu item:

```bash
# Create a lunch menu
./bin/weos resource create --type menu \
  --data '{"name": "Lunch Menu", "description": "Available 11am-3pm", "availability": "weekdays"}'

# List menus to get the ID
./bin/weos resource list --type menu

# Create a menu item (replace MENU_ID with the actual ID from above)
./bin/weos resource create --type menu-item \
  --data '{"name": "Grilled Salmon", "price": 24.99, "category": "main", "menu": "MENU_ID"}'
```

## What Happens Behind the Scenes

When you install a preset, WeOS:

1. **Creates ResourceType entities** for each type in the preset, recording `ResourceType.Created` events
2. **Generates projection tables** via the ProjectionManager:
   - `menu` slug becomes the `menus` table
   - `menu-item` slug becomes the `menu_items` table
3. **Adds typed columns** extracted from the JSON Schema — e.g., `name TEXT`, `price REAL`, `category TEXT`
4. **Registers foreign key relationships** — the `menu` column on `menu_items` stores the referenced menu's ID, and `menu_display` stores its name

See [Projections]({% link _explanation/projections.md %}) for a deep dive on how this works.

## Customizing Screens in the Admin UI

The admin UI automatically generates list and detail screens for each resource type. The JSON Schema properties determine:

- Which columns appear in the list view
- Which form fields appear in the create/edit view
- Field types (text inputs, number inputs, dropdowns for `x-resource-type` references)

To customize which types appear in the sidebar and how they're organized, see [Customizing the UI]({% link _tutorials/customizing-the-ui.md %}).

## What You've Learned

- How presets bundle related resource types
- How to define types with JSON-LD context and JSON Schema
- How `x-resource-type` creates foreign key relationships between types
- How projection tables are auto-generated from schemas

## What's Next

- [Preset Catalog]({% link _reference/preset-catalog.md %}) — see all built-in presets and their types
- [RDF and the Ontology]({% link _explanation/rdf-and-ontology.md %}) — understand why JSON-LD matters
- [Projections]({% link _explanation/projections.md %}) — how schemas become SQL tables
