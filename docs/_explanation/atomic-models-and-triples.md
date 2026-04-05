---
title: Atomic Models and Triples
parent: Explanation
layout: default
nav_order: 2
---

# Atomic Models and Triples

In WeOS, every resource is an **atomic unit** — it contains its own data and can exist independently. Relationships between resources are modeled as **RDF triples** (subject-predicate-object statements) that are stored alongside the resource's events. This approach keeps resources self-contained while enabling rich, typed connections.

## Why Atomic?

Traditional relational models embed foreign keys directly in entities. A `task` row might have a `project_id` column, creating a hard dependency between the two tables. This works well for CRUD applications, but it causes problems for event-sourced systems:

1. **Event ordering** — if a Task depends on its Project existing first, creation events must be ordered carefully
2. **Replay complexity** — replaying events requires resolving dependencies in the right sequence
3. **Schema coupling** — adding a relationship means altering the entity's schema

WeOS solves this by keeping entities atomic. A Task resource contains only its own properties (name, status, priority). The relationship to a Project is a separate fact — a triple — that's recorded alongside the task's events.

## RDF Triples

A triple is a statement with three parts:

| Part | Role | Example |
|------|------|---------|
| **Subject** | The resource making the statement | `urn:task:abc123` |
| **Predicate** | The relationship type | `https://schema.org/isPartOf` |
| **Object** | The target resource | `urn:project:xyz789` |

This triple says: "Task abc123 is part of Project xyz789."

## How Triples Are Created

When you create a resource with a property that references another resource type (marked with `x-resource-type` in the JSON Schema), WeOS records both the resource creation and the relationship as domain events:

1. `Resource.Created` — the task entity is created with its own data
2. `Triple.Created` — the relationship to the project is recorded
3. `Resource.Published` — a signal that all events for this resource are complete

These events are recorded atomically within the same Unit of Work, so they either all succeed or all fail.

### Triple Events

```
Triple.Created {
  Subject:   "urn:task:abc123",
  Predicate: "https://schema.org/isPartOf",
  Object:    "urn:project:xyz789"
}
```

When a relationship changes, a `Triple.Deleted` event removes the old triple and a new `Triple.Created` records the updated relationship.

## The @graph Format

Triples are reflected in the resource's JSON-LD data using the `@graph` format:

```json
{
  "@graph": [
    {
      "@id": "urn:task:abc123",
      "@type": "Action",
      "name": "Design landing page",
      "status": "open"
    },
    {
      "project": "urn:project:xyz789"
    }
  ]
}
```

- **Node 0** (entity node): the resource's own properties
- **Node 1** (edges node): references to other resources

This separation is important:
- The entity node is self-contained — it doesn't know or care about relationships
- The edges node captures relationships without polluting the entity's data
- Event handlers can process entity data and edge data independently

## Comparison to Foreign Keys

| Aspect | Foreign Keys | Triples |
|--------|-------------|---------|
| Storage | Column in entity table | Separate events on the entity |
| Schema coupling | Entity schema includes FK | Entity schema is independent |
| Directionality | Usually one direction | Can be bidirectional |
| Event ordering | Entity must exist before FK | Entity and triple events are atomic |
| Query optimization | SQL JOIN | Projection table with FK + display columns |

## Projection Table Integration

For query performance, the ProjectionManager bridges the gap between atomic triples and efficient SQL queries. When a resource type has an `x-resource-type` property, the projection table includes:

- A **foreign key column** storing the referenced resource's ID (e.g., `project TEXT`)
- A **display column** storing a human-readable value from the referenced resource (e.g., `project_display TEXT`)

The `x-display-property` extension specifies which property of the referenced type to use for the display column. For example:

```json
{
  "project": {
    "type": "string",
    "x-resource-type": "project",
    "x-display-property": "name"
  }
}
```

This creates columns `project` (stores the project URN) and `project_display` (stores the project's name). When the project's name changes, the display value propagates automatically to all referencing resources.

## The Resource.Published Signal

Because entity creation involves multiple events (Resource.Created + Triple.Created), event handlers that need the complete picture wait for the `Resource.Published` signal. This event fires after all creation events are committed, indicating that the resource's data and relationships are fully available.

This design is documented in the [Event Handler Data Availability ADR]({% link decisions/event-handler-data-availability.md %}).

## Further Reading

- [RDF and the Ontology]({% link _explanation/rdf-and-ontology.md %}) — the vocabulary system underlying triples
- [Event Store]({% link _explanation/event-store.md %}) — how triple events are stored and replayed
- [Projections]({% link _explanation/projections.md %}) — how triples become queryable columns
