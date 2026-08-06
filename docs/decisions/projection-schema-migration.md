---
title: "ADR: Projection Schema Migration"
parent: Architecture Decision Records
layout: default
nav_order: 7
---

# ADR: Migrating Projection Tables When a Resource Type Gains a Property

**Status:** Accepted — Implemented (additive scope)
**Date:** 2026-08-06
**Issue:** [#379 — Spike: migrate projection tables when a resource type gains a column](https://github.com/wepala/weos/issues/379)

## Problem

When a resource type's JSON Schema gained a property in a new build, databases provisioned by an earlier build kept the old projection table. Writes to the new field were then **silently dropped**: `dropMissingColumns` deletes any key with no matching column before the INSERT, so the write succeeded, no error surfaced, and every read, filter, or sort on the new field behaved as if it were empty.

This surfaced in finexity, where a resource type gained bill fields. Databases created before the change lacked the columns, so bill detection persisted candidates whose new fields vanished, the SPA's bill list (filtering on those fields) showed empty results, and confirming a bill returned 422 because the read-back saw an empty value. The data survived in the event store the whole time — only the projection lost it.

The spike asked where migration should live, whether GORM's `AutoMigrate` could do it, how existing rows get backfilled, what to do about non-additive changes, and how to detect drift.

### What already worked

Most of the machinery was already in place and correct:

- `addMissingColumns` (`infrastructure/database/gorm/projection_manager.go`) issues idempotent, `HasColumn`-guarded `ALTER TABLE ... ADD COLUMN` statements.
- `EnsureTable` calls it for both the base system columns and the schema-derived ones.
- `EnsureExistingTables` runs `EnsureTable` over every live resource type at **every** startup, via `fx.Invoke(ensureProjectionTables)`.
- `ResourceType.Updated` → `ensureProjection` → `EnsureTable` applies the same reconciliation the moment a type changes at runtime.

### The actual gap

All of that derives its column set from the schema **stored in the database**, and nothing refreshed that stored schema from the preset's code definition. `ensureBuiltInResourceTypes` called `InstallPreset(ctx, preset.Name, false)`; with `update=false`, an already-installed type is *Skipped*. So the chain was:

1. A preset's schema gains a property in code.
2. Startup skips the existing type — `resource_types.schema` stays stale.
3. `EnsureExistingTables` derives the **old** column set from that stale schema.
4. `addMissingColumns` finds nothing to add.
5. `dropMissingColumns` silently deletes the field on every write.

The fix belongs at step 2, not in the ALTER machinery.

## Design Goals

1. An additive schema change in code reaches an already-provisioned database on the next restart.
2. Operator customisation of a built-in resource type is never silently overwritten.
3. A restart with an unchanged preset is a no-op — no `ResourceTypeUpdated` event per type per boot.
4. Non-additive changes are detected and reported, never applied.
5. No change to the ALTER machinery, the write hot path, or the explicit `preset install --update` semantics.

## Decision

### 1. Where migration runs

Additive-column migration stays exactly where it was — `ensureProjectionTables` at startup. What was added is a narrower **schema reconciliation** step ahead of it.

`ResourceTypeService.ReconcilePresetSchemas` merges a preset's code-defined schema into the stored resource types, and `ensureBuiltInResourceTypes` calls it after `InstallPreset`. Because it runs before `fx.Invoke(ensureProjectionTables)` — and because the `ResourceType.Updated` it emits is handled synchronously — the refreshed schema is in place by the time any column set is derived.

`InstallPreset` was deliberately **not** changed. Neither of its modes is safe unattended: `update=false` skips existing types (the bug), and `update=true` overwrites `Name`, `Description`, `Context` *and* `Schema` from code on every boot, which would discard operator customisation. Startup gets a third, narrower behaviour instead; the explicit `resource-type preset install --update` path keeps its full-overwrite semantics, because there an operator asked for it.

### 2. The merge rules

`reconcileAdditiveSchema` compares the preset's schema against the stored one:

| Case | Action |
|---|---|
| Property in the preset, absent from stored | **Merged in** — this is what yields the missing column |
| Property in stored, absent from the preset | **Preserved** — never dropped |
| Property in both, same definition | No-op |
| Property in both, **different** definition | **Refused** — the whole type is left untouched and logged |
| Other top-level keywords (`required`, `type`, …) | Follow the preset where it declares them |

Preserving stored-only properties is what protects operator customisation. WeOS has no provenance field on `ResourceType`, so an operator's addition through the API is indistinguishable from drift — dropping it would trade one silent data loss for another. Preserving it also stops a single customisation from permanently blocking later preset additions.

The merge is idempotent: re-running it against its own output reports no change, which is what keeps repeated restarts quiet.

**`required` is authoritative.** A property the preset newly marks required tightens the stored schema. This is a deliberate choice, and it has a consequence worth stating plainly: existing rows that lack that property will start failing validation on their next write.

### 3. GORM `AutoMigrate` — rejected

`AutoMigrate` reflects over Go structs. Projection tables have none: they are generated from each resource type's JSON Schema at runtime, with column names and SQL types derived by `schemaToColumns`. There is nothing for `AutoMigrate` to reflect over, so using it would mean synthesising throwaway structs per type on every boot.

Rejecting it also avoids its broader behaviour. `AutoMigrate` will attempt type alters and constraint changes, which diverge across SQLite and Postgres — precisely the destructive class this work defers. The hand-rolled `ALTER TABLE ... ADD COLUMN` is additive by construction and behaves identically on both stores.

One nuance the hand-rolled path already encodes (see `baseColumnDefs`): `ADD COLUMN` cannot introduce `NOT NULL` without a default on a populated table, so added columns are deliberately nullable.

### 4. Backfilling existing rows

Adding a column leaves it `NULL` on every pre-existing row. Two paths exist, and they cover different cases:

- `backfillFromCanonical` fills rows missing **entirely** from the projection, via a `NOT EXISTS` anti-join. It never touches rows that already have a projection row, so it does *not* populate a newly added column.
- `weos worker reproject` replays the event feed through the synchronous projection handlers, rewriting each row from its canonical JSON-LD. **This is the backfill path for a newly added column.**

Reproject stays **operator-invoked**. A full replay is expensive and must not fire on boot; the runbook case (server stopped, column added, operator reprojects) is the right shape.

Making that true required a fix. Reproject previously replayed the feed in one strict position order, which meant each type's *original* schema was applied, every resource projected against it, and only then the later `ResourceType.Updated` that added the column — so the column was never backfilled, and re-running didn't help because the ordering was identical every pass. Reproject now replays in **two passes, resource types first**, so every type reaches its final shape before a single resource projects. Pass 1 always starts at the beginning of the feed (re-applying type events is idempotent); pass 2 is the resumable one and alone honours `--after-position`.

### 5. Non-additive changes — detected, deferred

Renames, type changes, and drops remain out of scope. They are now **detected** rather than ignored: a property whose definition differs between the preset and the stored schema refuses the whole type, logs a warning naming the conflicting properties, and reports them in `ReconcilePresetResult.Refused`. Nothing is rewritten, so an operator decides.

### 6. Drift detection — the remaining gap

Three sites still degrade silently when a schema property has no matching column. They are **not** fixed here and are tracked separately:

| Site | Behaviour | Severity |
|---|---|---|
| `resource_repository.go` — `dropMissingColumns` | Silently deletes the key on write | Lossy, silent |
| `resource_repository.go` — filter loop in `findAllFromProjectionWithFilters` | A filter on a missing column is `continue`d, **widening** the result set | **Most severe** |
| `resource_repository.go` — sort-column validation | Silently falls back to `id` | Cosmetic but confusing |

The filter case deserves emphasis: dropping a filter does not narrow results, it *widens* them — the query returns rows the caller explicitly asked to exclude. That is a correctness and potential exposure problem in its own right, independent of migration, and it should fail loudly rather than degrade.

## Consequences

### Good

- An additive schema change in code reaches every provisioned database on the next restart, with no operator action.
- Operator customisation of a built-in type survives restarts by construction.
- Non-additive divergence is surfaced with the offending property names instead of being applied or ignored.
- `worker reproject` genuinely backfills added columns now, making it a usable repair tool rather than a no-op for this case.
- No change to the write hot path, `EnsureTable`, `addMissingColumns`, or `preset install --update`.

### Neutral

- Reconciliation re-marshals the stored schema, so its top-level key order becomes alphabetical. Semantically equivalent; `jsonEquivalent` compares decoded values, so this does not cause spurious updates.
- Reproject now scans the feed twice. The event store has no server-side type filter, so this trades a second read for correctness in an operator-invoked one-shot.

### Risks

- **`required` tightening breaks existing rows.** A property the preset newly marks required will fail validation for rows that lack it, on their next write. Accepted deliberately; operators changing `required` on a populated type should plan a data pass.
- **Refusal is all-or-nothing per type.** One conflicting property blocks that type's additive changes too, until an operator resolves it. Chosen over partial application, which would leave a schema in a state neither the preset nor the operator authored.
- **Drift is still silent at read time.** Until the follow-up tickets land, a filter on a column that does not exist still widens results rather than erroring.

## Alternatives Considered

1. **Flip `ensureBuiltInResourceTypes` to `InstallPreset(..., true)`.** The smallest possible diff, and rejected: it makes preset code authoritative for `Name`, `Description`, `Context` and `Schema` on every boot, silently discarding operator customisation. Trading a silent-data-loss bug for a different silent-data-loss bug is not a fix.
2. **A config flag selecting overwrite / merge / off.** Rejected for v1 as surface area without a decision: the merge behaviour is what a correct default looks like, and the explicit `--update` path already exists for operators who want overwrite.
3. **GORM `AutoMigrate`.** Rejected — see above.
4. **Backfill automatically after adding a column.** Rejected: a full replay on boot is expensive and unpredictable. Reproject stays an explicit operator action.
5. **Add a provenance field to `ResourceType` (preset-owned vs operator-edited).** A cleaner long-term answer to "who owns this property", and out of scope here — the additive merge achieves the same protection without a schema migration on the entity itself.

## Further Reading

- [Projections]({% link _explanation/projections.md %}) — how projection tables are derived from resource type schemas.
- [Cross-Preset Link Definitions]({% link decisions/cross-preset-link-definitions.md %}) — the other mechanism that adds columns to a projection table via `addMissingColumns`.
