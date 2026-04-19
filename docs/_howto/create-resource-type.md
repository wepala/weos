---
title: Create a Resource Type
parent: How-to Guides
layout: default
nav_order: 3
---

# Create a Resource Type

## Install from a Preset

The quickest way — presets bundle related types:

```bash
# List available presets
weos resource-type preset list

# Install one
weos resource-type preset install website
```

Available presets: `core`, `ecommerce`, `tasks`, `website`, `events`, `knowledge`, `meal-planning`. See [Preset Catalog]({% link _reference/preset-catalog.md %}).

To update existing types when re-installing:

```bash
weos resource-type preset install website --update
```

## Create via CLI

```bash
weos resource-type create \
  --name "Menu" \
  --slug "menu" \
  --description "A restaurant menu" \
  --context '{"@vocab": "https://schema.org/", "@type": "Menu"}' \
  --schema '{
    "type": "object",
    "properties": {
      "name": {"type": "string"},
      "description": {"type": "string"}
    },
    "required": ["name"]
  }'
```

## Create via API

```bash
curl -X POST http://localhost:8080/api/resource-types \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Menu",
    "slug": "menu",
    "context": {"@vocab": "https://schema.org/", "@type": "Menu"},
    "description": "A restaurant menu"
  }'
```

## What Happens

1. A `ResourceType.Created` event is recorded
2. If a JSON Schema is provided, the ProjectionManager creates a dedicated SQL table:
   - Table name: slug with hyphens→underscores, pluralized (`menu` → `menus`)
   - Typed columns from schema properties (`name TEXT`, `description TEXT`)
   - Standard columns: `id`, `type_slug`, `status`, `created_by`, `account_id`, `sequence_no`, `created_at`, `updated_at`
3. The type appears in the admin UI sidebar

## Verify

```bash
weos resource-type list
```

## Create a Custom Preset

For reusable type bundles, create a preset package. See [Creating a Preset]({% link _tutorials/creating-a-preset.md %}).
