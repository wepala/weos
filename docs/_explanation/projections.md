---
title: Projections
parent: Explanation
layout: default
nav_order: 4
---

# Projections

In an event-sourced system, the **event store** is the source of truth, but querying events directly for every read request would be impractical. **Projections** solve this by maintaining denormalized read models — SQL tables optimized for queries — that are updated in response to domain events.

## How Projections Work in WeOS

When a resource type is created, the **ProjectionManager** automatically generates a dedicated SQL table for it. This table is the projection — a queryable view of the current state derived from events.

### Table Generation

The ProjectionManager reads the resource type's JSON Schema and creates a table with:

1. **Standard columns** present on every projection table:

   | Column | Type | Purpose |
   |--------|------|---------|
   | `id` | TEXT (PK) | Resource URN (e.g., `urn:task:abc123`) |
   | `type_slug` | TEXT | Resource type slug |
   | `status` | TEXT | Resource status (active, archived) |
   | `created_by` | TEXT | Creator's agent ID |
   | `account_id` | TEXT | Owning account ID |
   | `sequence_no` | INTEGER | Event sequence number |
   | `created_at` | DATETIME | Creation timestamp |
   | `updated_at` | DATETIME | Last update timestamp |

2. **Typed columns** extracted from the JSON Schema properties:

   | JSON Type | SQL Type |
   |-----------|----------|
   | `string` | TEXT |
   | `number` | REAL |
   | `integer` | INTEGER |
   | `boolean` | BOOLEAN |

3. **Display columns** for `x-resource-type` references — a `_display` suffix column that stores a human-readable value from the referenced resource.

### Naming Conventions

- **Table name**: slug with hyphens replaced by underscores, then pluralized. `blog-post` becomes `blog_posts`. `menu-item` becomes `menu_items`.
- **Column name**: camelCase properties converted to snake_case. `givenName` becomes `given_name`. `datePublished` becomes `date_published`.
- **Display column**: FK column name + `_display`. `project` gets a companion `project_display`.

### Example

Given a `task` resource type with this schema:

```json
{
  "properties": {
    "name": {"type": "string"},
    "status": {"type": "string"},
    "priority": {"type": "string"},
    "dueDate": {"type": "string", "format": "date"},
    "project": {
      "type": "string",
      "x-resource-type": "project",
      "x-display-property": "name"
    }
  }
}
```

The ProjectionManager creates a `tasks` table with columns:

```
id TEXT PRIMARY KEY
type_slug TEXT NOT NULL
status TEXT NOT NULL DEFAULT 'active'
created_by TEXT
account_id TEXT
sequence_no INTEGER
created_at DATETIME
updated_at DATETIME
name TEXT
priority TEXT
due_date TEXT
project TEXT
project_display TEXT
```

Note that the full JSON-LD data is stored in the generic `resources` table (in its `data` column), not in the projection table. Projection tables contain only typed columns extracted from the schema, optimized for SQL queries.

## Event-Driven Updates

Projections are updated by event handlers that subscribe to domain events:

1. **Resource.Published** — upserts the resource into its projection table. This is the primary event that triggers projection writes. It fires after all creation events (Resource.Created + Triple.Created) are committed.
2. **Resource.Updated** — updates the projection row with new data.
3. **Resource.Deleted** — marks the row as archived (soft delete).

The consolidated write approach (triggered by Resource.Published rather than individual events) is documented in the [Transaction ID and Projection Consolidation ADR]({% link decisions/transaction-id-and-projection-consolidation.md %}).

## Display Value Propagation

When a resource with a `x-display-property` is updated (e.g., a project's name changes), the ProjectionManager propagates the change to all referencing tables' display columns. For example:

1. Project "Alpha" is renamed to "Alpha v2"
2. The ProjectionManager finds all types with an `x-resource-type: "project"` property
3. It updates every `project_display` column where `project` matches the renamed project's ID

This keeps display values consistent without requiring event replay.

## Ancestor Tables (Type Inheritance)

WeOS supports type inheritance via `rdfs:subClassOf`. When a resource type declares a parent type, resources are written to **both** the type's own projection table and the parent's table. The `AncestorSlugs()` method returns the inheritance chain.

## Generic Resources Table

Resource types that don't have a JSON Schema (or were created before schema support) use the generic `resources` table. The `ResourceRepository` routes queries to projection tables when available, falling back to this generic table for legacy data.

## Lazy Table Creation

Projection tables are created lazily. `HasProjectionTable()` checks the cache first, and if the table isn't cached, it loads the resource type from the database and calls `EnsureTable()` to create the table on the fly. This means the system is self-healing — even if a table is missing, it will be recreated from the resource type's schema.

On startup, `EnsureExistingTables()` pre-loads all active resource types and ensures their projection tables exist, handling schema evolution by adding any missing columns.

## Further Reading

- [Event Store]({% link _explanation/event-store.md %}) — the events that drive projection updates
- [Creating a Preset]({% link _tutorials/creating-a-preset.md %}) — see projection tables in action
- [ADR: Transaction ID and Projection Consolidation]({% link decisions/transaction-id-and-projection-consolidation.md %}) — design rationale for consolidated writes
