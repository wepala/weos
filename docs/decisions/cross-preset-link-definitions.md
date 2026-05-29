---
title: "ADR: Cross-Preset Link Definitions"
parent: Architecture Decision Records
layout: default
nav_order: 6
---

# ADR: Declaring Cross-Type Relationships Outside Resource Type Schemas

**Status:** Accepted — Implemented
**Date:** 2026-04-19
**Issue:** [#328 — Find another way to define foreign key relationships in resource type schema](https://github.com/wepala/weos/issues/328)

## Problem

Cross-type relationships in WeOS were declared by embedding an `x-resource-type` extension on a property in the source type's JSON Schema:

```json
{
  "properties": {
    "guardianId": {
      "type": "string",
      "x-resource-type": "guardian",
      "x-display-property": "name"
    }
  }
}
```

This works well when both types live in the same preset, but it creates a one-way coupling across presets: for a `finance` preset's `Invoice` to reference an `education` preset's `Guardian`, `Invoice`'s schema must name `Guardian` explicitly. The parent preset ends up with "knowledge of other presets" even though the two are conceptually independent. A third integration package that wants to connect two existing presets cannot express a link between them without modifying one of their schemas.

The issue also asked that cross-preset links **only form when both sides are installed** — installing `finance` alone should not leave a dangling `guardian` column on the Invoice projection table.

## Design Goals

1. Resource type schemas stay atomic — only their own properties.
2. A link between two types can be declared by a *third* package without either endpoint preset knowing about it.
3. Links activate only when both source and target resource types are installed.
4. The existing `x-resource-type` mechanism keeps working unchanged (backward compatibility).
5. Downstream systems (triple extraction, projection FK columns, display propagation, UI rendering) treat schema-declared and link-declared references identically.

## Decision

Introduce a **link registry** populated from two ergonomic entry points, reconciled against installed types by a dedicated activator.

### Data model

```go
// PresetLinkDefinition lives alongside PresetResourceType, not inside a schema.
type PresetLinkDefinition struct {
    Name            string
    SourceType      string // slug of source resource type
    TargetType      string // slug of target resource type
    PropertyName    string // attribute on source (drives FK column name)
    PredicateIRI    string // optional; resolved from @vocab if empty
    DisplayProperty string // defaults to "name"
}

// Links inside a preset — natural packaging for a "finance-education" integration preset.
type PresetDefinition struct {
    // ... existing fields ...
    Links []PresetLinkDefinition
}

// Or at package init — for integration packages that don't ship a full preset.
func RegisterLink(def PresetLinkDefinition) error
```

Both feed one `LinkRegistry`, deduped on `(SourceType, PropertyName)` — the database uniqueness key for a FK column on a source type.

### Activation

A `LinkActivator.Reconcile(ctx)` pass runs:
- After every `InstallPreset` call (including the per-preset ones inside `ensureBuiltInResourceTypes`).
- Once more at startup, after the auto-install loop finishes — this catches links whose endpoints arrived from two different presets and would otherwise only see each other after the next manual install.

Reconcile loads installed type slugs from the repository, asks the registry `ActiveFor(installed)` for links whose source **and** target are both present, and calls `ProjectionManager.RegisterLink` for each. `RegisterLink` adds the FK column + display column via `ALTER TABLE` (idempotent via `addMissingColumns`) and records the same forward/reverse reference entries that schema-declared `x-resource-type` produces.

### Triple extraction

`ExtractReferencePropertiesWithLinks(schema, ldContext, externalLinks)` merges schema-derived refs with link-derived refs into one `[]ReferencePropertyDef`. The write path (`BuildResourceGraph`, `ExtractReferenceTriples`, projection FK/display population) consumes this list unchanged, so link-declared references produce the same triples and the same `@graph` edges node as schema-declared ones.

When both mechanisms declare the same `PropertyName`, **schema wins** — the conflicting link is silently dropped from the merged list and from the `registerReverseReferences` replay. Schemas are closer to the type definition, so a conflicting external link is almost always a mistake; callers that need to surface the conflict can inspect the registry and compare against the schema before merging.

## Consequences

### Good

- Cross-preset coupling is gone. A `finance-education` integration preset (or any package) can connect Invoice and Guardian without either preset depending on the other.
- Activation is conditional and idempotent — installing only one side leaves the schema clean; installing the other side later adds the columns on the next reconcile.
- The existing `x-resource-type` mechanism continues to work, so no presets in the repo need to migrate.
- Downstream consumers (UI forms, triple projections, display propagation) see one uniform list of references regardless of where each reference was declared.

### Neutral

- Link definitions are declarative Go code, not persisted domain entities. "Dormant vs active" is a runtime view computed from `(link, installed slugs)` rather than a stored state — simpler, and sync bugs between registry and DB are impossible by construction. The trade-off: we never record *when* a link was activated, but the triples it produces carry that history already.
- Always materializes FK + display columns when a link activates. Chosen over a "triples-only" opt-out because list query performance parity with `x-resource-type` was prioritized; the issue's "atomic schema" goal is already satisfied by keeping the *source schema* untouched.

### Risks

- **Late activation and historical rows.** Rows written before a link activates won't have `<prop>_display` populated. v1 ships with this limitation; a backfill job is a follow-up, mirroring how resource renames propagate today via `UpdateColumnByFK`.
- **Type deletion while linked.** Not addressed in v1. Follow-up should block deletion of a resource type when any active link references it.
- **Predicate collision with schemas.** Dedup on `PropertyName` is schema-first: when a schema property and a cross-preset link definition collide, the schema definition wins and the link definition is silently dropped.

## Alternatives Considered

1. **Persist `LinkDefinition` as a domain entity with events.** Rejected as sync-prone: the preset code is already the source of truth for link definitions, and persisting them creates two authorities for the same fact.
2. **Triples-only links with no projection columns.** Rejected for v1: list queries that already rely on FK columns would need JOIN rewrites across the codebase, and there is no opt-in complexity budget for a mechanism that users should adopt by default.
3. **Replace `x-resource-type` entirely and migrate all presets.** Rejected as unnecessary churn — intra-preset references are unambiguous and the schema is already the right place to declare them.

## Further Reading

- [Atomic Models and Triples]({% link _explanation/atomic-models-and-triples.md %}) — the triple-based relationship model the link mechanism plugs into.
- [Creating a Preset]({% link _tutorials/creating-a-preset.md %}) — how to use `Links` and `RegisterLink` from a preset.
