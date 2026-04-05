---
title: "ADR: Transaction ID and Projection Consolidation"
parent: Architecture Decision Records
layout: default
nav_order: 2
---

# ADR: Transaction ID on Events and Consolidated Projection Writes

**Status:** Accepted — Implemented (fully consolidated: TripleService removed, all mutations through UoW + Resource.Published)  
**Date:** 2026-04-04  
**Follows from:** [Event Handler Data Availability](event-handler-data-availability.md) (Accepted — Option 5)

## Problem

The current event handler architecture writes to projection tables incrementally — each handler writes its own piece:

```
Resource.Created handler  → INSERT projection row (sequence_no = 0)
Triple.Created handler    → UPDATE projection FK column
Triple.Created graph sync → UPDATE resources.data + sequence_no = 1
Resource.Published handler→ UPDATE sequence_no = 2
```

This causes three problems:

### 1. Multiple writes per resource creation

A resource with 2 relationships triggers at minimum 5 projection writes (1 insert + 4 updates). Each is a separate SQL statement, adding latency and DB load.

### 2. Fragile sequence number tracking

The projection's `sequence_no` is bumped piecemeal by whichever handler runs last. The `Resource.Created` handler stores `env.SequenceNo` (the event's own sequence, e.g. 0), then the graph sync handler patches it to 1, etc. If a new event type is added to the sequence (as happened with `Resource.Published`), the projection's `sequence_no` falls behind the event store's actual version, causing optimistic concurrency conflicts on the next update.

### 3. No way to correlate events from the same commit

When handlers process individual events, they have no way to know which other events were committed in the same UnitOfWork. The `Resource.Published` signal helps (it fires last), but the handler still can't enumerate sibling events without querying by aggregate ID and guessing which events are "recent."

## Proposal

Two changes that work together:

### Change 1: Add `TransactionID` to events via Metadata

Use the existing `Metadata` field on `EventEnvelope` to stamp a transaction ID on all events committed in the same UnitOfWork. The `Metadata` field already exists, is persisted to both GORM (`events.metadata` JSONB column) and BigQuery, and is currently unused.

**In pericarp `SimpleUnitOfWork.Commit()`**, before persisting:

```go
// Generate a transaction ID for this commit batch
txID := ksuid.New().String()
for i := range allEvents {
    if allEvents[i].Metadata == nil {
        allEvents[i].Metadata = make(map[string]any)
    }
    allEvents[i].Metadata["transaction_id"] = txID
}
```

This requires no schema changes — `Metadata` is already a JSONB column. Handlers can read it:

```go
txID, _ := env.Metadata["transaction_id"].(string)
```

### Change 2: Consolidate projection writes into `Resource.Published` handler

Instead of writing to the projection table incrementally from each handler, defer the full projection write to the `Resource.Published` handler. Since it fires last, the canonical `resources` table and `triples` table are already populated by prior handlers — the `Resource.Published` handler reads the final state and does a single projection write.

**Current flow (incremental):**
```
Resource.Created  → INSERT into resources table + INSERT into projection table
Triple.Created    → INSERT into triples table
Triple.Created    → UPDATE projection FK + display columns
Triple.Created    → UPDATE resources.data (graph sync) + bump sequence_no
Resource.Published→ UPDATE projection sequence_no
```

**Proposed flow (consolidated):**
```
Resource.Created  → INSERT into resources table only (no projection write)
Triple.Created    → INSERT into triples table
Triple.Created    → UPDATE resources.data (graph sync)
Resource.Published→ READ final state from resources + triples
                  → Single INSERT/UPDATE to projection table with all columns,
                    FK values, display values, and correct sequence_no
```

This reduces N+1 projection writes to exactly 1.

## Detailed Design

### EventEnvelope Metadata — Transaction ID

**File:** pericarp `pkg/eventsourcing/application/unitofwork.go`

In `Commit()`, after collecting `allEvents` and before the `eventStore.Append()` loop:

```go
txID := ksuid.New().String()
for i := range allEvents {
    if allEvents[i].Metadata == nil {
        allEvents[i].Metadata = make(map[string]any)
    }
    allEvents[i].Metadata["transaction_id"] = txID
}
```

**Why Metadata instead of a dedicated field?**

- Zero schema migration — `Metadata` JSONB column already exists in the `events` table
- Zero breaking changes — existing code that doesn't read `transaction_id` is unaffected
- The pericarp library stays generic — transaction correlation is opt-in, not structural
- If a first-class `TransactionID` field is wanted later, the Metadata approach serves as a low-risk proof-of-concept

**Why not a dedicated `TransactionID` field on EventEnvelope?**

A dedicated field is cleaner long-term but requires:
- Changing the `EventEnvelope` struct (pericarp)
- Database migration to add a `transaction_id` column to the `events` table
- Updating the GORM model, BigQuery schema, and all serialization paths
- Updating the `events.go` CLI tooling that reads events

The Metadata approach gets the same behavior with zero migration. If it proves valuable, promoting to a first-class field is a follow-up.

### Consolidated Projection Writes

**Changes to `Resource.Created` handler** (`application/event_handlers.go`):

Remove the `saveToProjection` call. The handler still saves to the canonical `resources` table (needed by subsequent handlers like graph sync), but skips the projection table:

```go
domain.Subscribe(d, "Resource.Created",
    func(ctx context.Context, env domain.EventEnvelope[entities.ResourceCreated]) error {
        p := env.Payload
        entity := &entities.Resource{}
        if err := entity.Restore(
            env.AggregateID, p.TypeSlug, "active",
            json.RawMessage(p.Data), p.CreatedBy, p.AccountID,
            p.Timestamp, env.SequenceNo,
        ); err != nil {
            return err
        }
        // Save to canonical resources table only — projection deferred to Resource.Published
        return repo.SaveCanonical(ctx, entity)
    },
)
```

**Changes to `Triple.Created` projection sync handler** (`application/triple_handler.go`):

The `syncTripleToProjection` handler (lines 74-81) currently updates FK and display columns on the projection table. Two options:

- **Option A:** Remove it entirely — `Resource.Published` handler will write FK + display values
- **Option B:** Keep it for standalone `TripleService.Link()` calls (post-creation triples that don't fire `Resource.Published`), but have it no-op when the projection row doesn't exist yet

Option B is safer since `TripleService.Link()` is a valid entry point that doesn't go through the resource service and won't fire `Resource.Published`.

**New `Resource.Published` handler** (`application/event_handlers.go`):

```go
domain.Subscribe(d, "Resource.Published",
    func(ctx context.Context, env domain.EventEnvelope[entities.ResourcePublished]) error {
        resource, err := repo.FindByID(ctx, env.AggregateID)
        if err != nil {
            return fmt.Errorf("projection read failed: %w", err)
        }
        // Restore with the correct final sequence number
        if err := resource.Restore(
            env.AggregateID, resource.TypeSlug(), resource.Status(),
            resource.Data(), resource.CreatedBy(), resource.AccountID(),
            resource.CreatedAt(), env.SequenceNo,
        ); err != nil {
            return err
        }

        triples, err := tripleRepo.FindBySubject(ctx, env.AggregateID)
        if err != nil {
            return fmt.Errorf("triple lookup failed: %w", err)
        }

        // Single projection write: all columns, FKs, display values, correct sequence_no
        return repo.SaveOrUpdateProjection(ctx, resource, triples)
    },
)
```

**New repository method: `SaveOrUpdateProjection`**

This method builds the full projection row in one shot — base columns, flat data columns, FK columns from triples, display values from referenced resources:

```go
func (r *ResourceRepository) SaveOrUpdateProjection(
    ctx context.Context, entity *entities.Resource, triples []Triple,
) error {
    if !r.projMgr.HasProjectionTable(entity.TypeSlug()) {
        return nil
    }
    tableName := r.projMgr.TableName(entity.TypeSlug())
    row := map[string]any{
        "id":          entity.GetID(),
        "type_slug":   entity.TypeSlug(),
        "status":      entity.Status(),
        "created_by":  entity.CreatedBy(),
        "account_id":  entity.AccountID(),
        "sequence_no": entity.GetSequenceNo(),
        "created_at":  entity.CreatedAt(),
        "updated_at":  time.Now(),
    }
    ldCtx := r.projMgr.Context(entity.TypeSlug())
    ExtractFlatColumns(entity.Data(), ldCtx, row)

    // Add FK + display columns from triples
    for _, t := range triples {
        colName, displayCol := r.projMgr.ColumnForPredicate(entity.TypeSlug(), t.Predicate)
        if colName != "" {
            row[colName] = t.Object
            row[displayCol] = resolveDisplayValue(ctx, t.Object, ...)
        }
    }

    // Upsert: INSERT or UPDATE
    return r.db.WithContext(ctx).Table(tableName).
        Clauses(clause.OnConflict{
            Columns:   []clause.Column{{Name: "id"}},
            DoUpdates: clause.AssignmentColumns(keys(row)),
        }).Create(row).Error
}
```

**Changes to `SaveCanonical`** — a new method (or refactor of `Save`) that writes only to the `resources` table:

```go
func (r *ResourceRepository) SaveCanonical(ctx context.Context, entity *entities.Resource) error {
    model := models.FromResource(entity)
    return r.db.WithContext(ctx).Create(model).Error
}
```

### Graph sync handler — no change needed

The graph sync handler (`triple_handler.go:117-143`) updates `resources.data` via `UpdateData`. This still runs during `Triple.Created` dispatch and updates the canonical `resources` table. When `Resource.Published` fires next, `FindByID` reads the already-updated `resources.data` with the full `@graph`.

The graph sync handler's `sequence_no` update to the `resources` table becomes redundant (since `Resource.Published` will set the final value), but it's harmless and can be cleaned up later.

### Event sequence with proposed changes

```
UoW.Commit() begins
  ├─ EventStore.Append(all events with transaction_id in Metadata)
  │
  ├─ Dispatch("Resource.Created")
  │     └─ Save to canonical resources table only
  │
  ├─ Dispatch("Triple.Created" #1)
  │     ├─ Save to triples table
  │     ├─ Update resources.data (graph sync)
  │     └─ [projection FK sync skipped — row doesn't exist yet]
  │
  ├─ Dispatch("Triple.Created" #2)
  │     ├─ Save to triples table
  │     ├─ Update resources.data (graph sync)
  │     └─ [projection FK sync skipped]
  │
  ├─ Dispatch("Resource.Published")
  │     └─ Read resources + triples → single UPSERT to projection table
  │
  └─ UoW.Commit() returns
```

## What Changes Where

| Component | File | Change |
|-----------|------|--------|
| UoW transaction ID | pericarp `unitofwork.go` | Stamp `transaction_id` in Metadata before persist |
| Resource.Created handler | `application/event_handlers.go` | Save to canonical table only, skip projection |
| Resource.Published handler | `application/event_handlers.go` | Read final state, single projection upsert |
| Triple.Created projection sync | `application/triple_handler.go` | Guard against missing projection row (for creation flow); keep for standalone Link() |
| ResourceRepository | `infrastructure/database/gorm/resource_repository.go` | Add `SaveCanonical` and `SaveOrUpdateProjection` methods |
| ResourceRepository interface | `domain/repositories/resource_repository.go` | Add new methods to interface |

## Risks and Mitigations

**Risk:** `Resource.Published` handler fails — projection table never gets written.  
**Mitigation:** Dispatch errors are currently non-fatal (`_ = err` in UoW). This is a pre-existing issue for all handlers. The canonical `resources` table and `triples` table are already populated. A retry mechanism or startup reconciliation job could rebuild projections from canonical data.

**Risk:** `TripleService.Link()` (standalone, no `Resource.Published`) adds a triple but projection FK column isn't updated.  
**Mitigation:** Keep the existing `Triple.Created` projection sync handler. It already handles this case. The guard for missing projection rows only applies during creation flow (row doesn't exist yet until `Resource.Published` creates it).

**Risk:** Read-after-write within the same request — code that calls `Create()` and then immediately queries the projection table may not see the row if handlers run async in the future.  
**Mitigation:** Current dispatch is synchronous (UoW.Commit blocks until all handlers complete). This remains safe as long as dispatch stays synchronous. If dispatch becomes async later, this concern applies to all handlers, not just the consolidated one.

**Risk:** `FindByID` reads from canonical `resources` table, not projection. During the window between `Resource.Created` handler and `Resource.Published` handler, the canonical table has `sequence_no = 0`.  
**Mitigation:** Within a single UoW.Commit, events are dispatched sequentially. The only code that reads during this window is other handlers in the same commit — they use `FindByID` to read `resources.data` (for graph sync), not `sequence_no`. The stale sequence in the canonical table during this window is harmless.

## Alternatives Considered

### First-class `TransactionID` field on EventEnvelope

Cleaner API but requires struct change in pericarp, database migration, and updates to all serialization paths (GORM model, BigQuery, CLI events tooling). The Metadata approach is a zero-migration proof-of-concept. Promote to a dedicated field if the pattern proves valuable.

### Keep incremental projection writes, just fix sequence_no

This is what we currently have (the `Resource.Published` handler that bumps `sequence_no`). It works but leaves the N+1 write problem and the fragile sequence tracking. The consolidated approach solves both.

### Write projection in the service layer (not in handlers)

Move the projection write out of the event system entirely — the service calls `repo.SaveWithProjection()` after `UoW.Commit()` returns. Simpler, but breaks the event-driven architecture: projections would no longer be rebuildable from event replay.