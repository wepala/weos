---
title: Behaviors
parent: Explanation
layout: default
nav_order: 7
---

# Behaviors

Behaviors are WeOS's mechanism for attaching domain logic to resource types. They implement the **Type Object pattern** — a design pattern where instances of a "type" class define the behavior of their associated objects at runtime, rather than through compile-time subclassing.

In practice, this means a resource type like "person" can have custom logic (e.g. computing a full name from given and family names) without requiring a dedicated Go struct for every content type. Behaviors are registered by slug and resolved automatically when resources are created, updated, or deleted.

## Why Behaviors?

WeOS models all content as generic **Resources** backed by **ResourceTypes**. This gives enormous flexibility — users create new content types at runtime via presets or the MCP server — but raises a question: where does type-specific business logic live?

Traditional approaches would either:
- Hardcode logic into a monolithic service (violating open/closed principle)
- Create Go subtypes for every resource type (impossible when types are created at runtime)

The Type Object pattern solves this by externalizing type-specific logic into a registry of behavior objects keyed by resource type slug. The service looks up the correct behavior at runtime and delegates to it.

**Reference:** Woolf, B. "The Type Object Pattern." *Pattern Languages of Program Design 3*, Addison-Wesley, 1998.

## The ResourceBehavior Interface

Every behavior implements the `ResourceBehavior` interface defined in `domain/entities/resource_behavior.go`:

```go
type ResourceBehavior interface {
    BeforeCreate(ctx context.Context, data json.RawMessage, rt *ResourceType) (json.RawMessage, error)
    BeforeCreateCommit(ctx context.Context, resource *Resource) error
    AfterCreate(ctx context.Context, resource *Resource) error

    BeforeUpdate(ctx context.Context, existing *Resource, data json.RawMessage, rt *ResourceType) (json.RawMessage, error)
    BeforeUpdateCommit(ctx context.Context, resource *Resource) error
    AfterUpdate(ctx context.Context, resource *Resource) error

    BeforeDelete(ctx context.Context, resource *Resource) error
    AfterDelete(ctx context.Context, resource *Resource) error
}
```

### Hook Categories

The hooks fall into three categories with different execution semantics:

| Category | Hooks | Returns | On Error |
|----------|-------|---------|----------|
| **Data transform** | `BeforeCreate`, `BeforeUpdate` | Modified `json.RawMessage` | Short-circuits — operation is rejected |
| **Gate** | `BeforeCreateCommit`, `BeforeUpdateCommit`, `BeforeDelete` | `error` only | Short-circuits — operation is rejected |
| **After** | `AfterCreate`, `AfterUpdate`, `AfterDelete` | `error` only | Logged but does not fail the operation (commit already succeeded) |

### Lifecycle Integration

Here is how behaviors fit into the `ResourceService.Create` flow:

1. Load the ResourceType by slug
2. **Resolve behavior** via the registry (see below)
3. **`BeforeCreate`** — transform or validate the raw JSON data
4. JSON Schema validation (runs on the behavior's output)
5. Construct the Resource entity
6. **`BeforeCreateCommit`** — last chance to reject before persistence
7. UnitOfWork commit (events persisted, projections updated)
8. **`AfterCreate`** — post-commit side effects

Update and Delete follow the same pattern with their respective hooks.

## DefaultBehavior and Composition

`DefaultBehavior` is a no-op struct that passes data through unchanged and returns `nil` for all gate/after hooks. Concrete behaviors **embed** `DefaultBehavior` and override only the hooks they need:

```go
type myBehavior struct {
    entities.DefaultBehavior  // all hooks default to no-op
}

func (b *myBehavior) BeforeCreate(ctx context.Context, data json.RawMessage, rt *entities.ResourceType) (json.RawMessage, error) {
    // custom logic here
    return data, nil
}
```

## Behavior Inheritance via CompositeBehavior

Resource types can declare an `rdfs:subClassOf` relationship in their JSON-LD context. When the service resolves a behavior, it walks the inheritance chain and collects all registered behaviors from child to parent. If multiple behaviors are found, they are wrapped in a `CompositeBehavior` that chains them:

- **Data transform hooks** pipeline outputs: child runs first, its output feeds into the parent
- **Gate hooks** short-circuit on the first error
- **After hooks** fire all behaviors, capturing only the first error

For example, if "invoice" extends "commitment" which extends "action", and both "invoice" and "action" have registered behaviors, the composite chains them as `[invoice, action]`.

Circular references are detected and safely terminated.

## The Behavior Registry

Behaviors are collected from presets into a `ResourceBehaviorRegistry` (a `map[string]entities.ResourceBehavior`). Each preset can declare behaviors in its `PresetDefinition.Behaviors` map:

```go
registry.MustAdd(application.PresetDefinition{
    Name: "core",
    Behaviors: map[string]entities.ResourceBehavior{
        "person":       &personBehavior{},
        "organization": &organizationBehavior{},
    },
    // ...
})
```

The registry is provided to `ResourceService` via dependency injection. At runtime, `behaviorFor()` looks up the slug in the registry, walks the type hierarchy, and returns the appropriate behavior (or `DefaultBehavior` if none is registered).

## Further Reading

- [How to Create a Behavior]({% link _howto/create-behavior.md %}) — step-by-step guide
- [Creating a Preset]({% link _tutorials/creating-a-preset.md %}) — how presets bundle types and behaviors
- [Architecture]({% link _explanation/architecture.md %}) — where behaviors fit in the Clean Architecture layers
- [The Type Object Pattern](https://web.archive.org/web/20190401094235/http://www.cs.ox.ac.uk/jeremy.gibbons/dpa/typeobject.pdf) (Gibbons et al., University of Oxford) — the foundational academic paper
