---
title: "ADR: Event Handler Data Availability"
parent: Architecture Decision Records
layout: default
nav_order: 1
---

# ADR: Handling Data Availability in Event Handlers for Atomic Resources

**Status:** Accepted — Option 5 (Resource.Published signal)  
**Date:** 2026-04-04  
**Context:** WeOS event sourcing with atomic resource modeling and triple-based relationships

## Problem

Resources in WeOS are atomic. When a resource is created, the system records a `Resource.Created` event followed by zero or more `Triple.Created` events (one per relationship). All events are committed atomically via UnitOfWork, but dispatched sequentially — `Resource.Created` is dispatched before `Triple.Created`.

If a developer writes a handler that reacts to `Resource.Created` for a specific resource type (e.g., "when a Product is created, do X"), that handler may not have access to relationship data yet because the triple events haven't been dispatched.

### Current Dispatch Mechanics (from pericarp)

Understanding the actual dispatch behavior is critical for evaluating options:

1. **UnitOfWork.Commit()** persists all events to the EventStore first, then dispatches them **one at a time** in recording order (`Resource.Created` before `Triple.Created`).
2. **EventDispatcher.Dispatch()** runs all handlers for a given event type **in parallel** (via `errgroup`). There is no guaranteed ordering between handlers subscribed to the same event type.
3. **Dispatch errors are non-fatal** in the UnitOfWork — errors from `dispatcher.Dispatch()` are silently discarded (`_ = err`). Events are already persisted regardless.
4. Handler registration order in `subscribeEventHandlers()` controls which handlers exist for each event type, but does NOT control execution order within a single dispatch (parallel execution).

### Event Sequence for Resource Creation

```
UoW.Commit() begins
  ├─ EventStore.Append(all events)          ← atomic persistence
  │
  ├─ Dispatch("Resource.Created")           ← all Resource.Created handlers run in parallel
  │     ├─ projection handler (saves to DB)
  │     └─ [your custom handler runs HERE — triples NOT yet dispatched]
  │
  ├─ Dispatch("Triple.Created" #1)          ← all Triple.Created handlers run in parallel
  │     ├─ triple repo save
  │     ├─ projection FK sync
  │     └─ graph sync
  │
  ├─ Dispatch("Triple.Created" #2)          ← next triple
  │     └─ ...
  │
  └─ UoW.Commit() returns
```

## Options

---

### Option 1: Listen to the Specific Triple Event

Instead of subscribing to `Resource.Created`, subscribe to the `Triple.Created` event that carries the relationship data you actually need.

**Example:**
```go
domain.Subscribe(d, "Triple.Created",
    func(ctx context.Context, env domain.EventEnvelope[entities.TripleCreated]) error {
        p := env.Payload
        // Filter: only act on products linked to a category
        if extractTypeSlugFromResourceID(p.Subject) != "product" {
            return nil
        }
        if p.Predicate != "schema:category" {
            return nil
        }
        // At this point, p.Subject is the product ID, p.Object is the category ID
        return doSomethingWithProductCategory(ctx, p.Subject, p.Object)
    },
)
```

**Pros:**
- Zero infrastructure changes — uses the existing subscription mechanism as-is
- Simple to reason about: "when this relationship is established, react"
- Works for both creation and update flows (triples are reconciled on update too)
- Handler is naturally idempotent per-triple
- Follows the existing pattern used by triple_handler.go (projection sync, graph sync)

**Cons:**
- Only works when you need data from a single relationship — if you need the resource's own properties AND a relationship, you must read the resource from the repo inside the handler
- Filtering by type slug requires parsing the URN (`extractTypeSlugFromResourceID`), which is a string convention, not a typed guarantee
- If the handler needs data from multiple triples (e.g., product needs both a category AND a supplier), it fires once per triple — you get partial views each time
- Handlers run in parallel with existing triple handlers (projection sync, graph sync), so the projection table FK column may not be populated yet when your handler runs
- Does not distinguish between "initial creation" and "relationship updated later" — if the distinction matters, you'd need to check resource age or sequence number

---

### Option 2: Saga / Process Manager

A stateful coordinator that listens to multiple event types and accumulates state. It triggers the downstream action only when all required data is present.

**Example:**
```go
type ProductReadySaga struct {
    mu              sync.Mutex
    pending         map[string]*productState // keyed by resource ID
}

type productState struct {
    created       bool
    categorySet   bool
    resourceEvent domain.EventEnvelope[entities.ResourceCreated]
    categoryID    string
}

func (s *ProductReadySaga) HandleCreated(ctx context.Context, env domain.EventEnvelope[entities.ResourceCreated]) error {
    if env.Payload.TypeSlug != "product" {
        return nil
    }
    s.mu.Lock()
    defer s.mu.Unlock()
    state := s.getOrCreate(env.AggregateID)
    state.created = true
    state.resourceEvent = env
    return s.tryComplete(ctx, env.AggregateID, state)
}

func (s *ProductReadySaga) HandleTriple(ctx context.Context, env domain.EventEnvelope[entities.TripleCreated]) error {
    if extractTypeSlugFromResourceID(env.Payload.Subject) != "product" {
        return nil
    }
    if env.Payload.Predicate != "schema:category" {
        return nil
    }
    s.mu.Lock()
    defer s.mu.Unlock()
    state := s.getOrCreate(env.Payload.Subject)
    state.categorySet = true
    state.categoryID = env.Payload.Object
    return s.tryComplete(ctx, env.Payload.Subject, state)
}

func (s *ProductReadySaga) tryComplete(ctx context.Context, id string, state *productState) error {
    if state.created && state.categorySet {
        delete(s.pending, id)
        return onProductReady(ctx, state)
    }
    return nil
}
```

**Pros:**
- Handles complex multi-event preconditions (e.g., resource + 2 specific triples all required)
- Explicitly models the "readiness" criteria — self-documenting
- Works even if event ordering changes in the future (e.g., if dispatch becomes async)
- Can handle cross-aggregate coordination (events from different entities)

**Cons:**
- Significant complexity for what is often a simple need — the saga struct, state tracking, completion logic, and cleanup are substantial boilerplate
- In-memory state is lost on process restart. Since dispatch errors are swallowed, a saga that partially filled before a crash will never complete. Durable saga state (persisted to DB) adds even more complexity.
- Thread safety requires careful locking since handlers run in parallel
- Must handle cleanup of stale/incomplete sagas (e.g., resource created but the expected triple never arrives because schema changed)
- Currently no saga infrastructure in the codebase — would need to build from scratch
- Harder to test than a simple event handler
- Overkill for the common case where events always arrive together in the same UoW commit

---

### Option 3: UnitOfWork "Batch Committed" Event

After `UoW.Commit()` dispatches all individual events, fire one additional synthetic event that carries the full list of events that were just committed. Handlers subscribe to this batch event when they need the complete picture.

**Example — changes to pericarp UnitOfWork:**
```go
// After dispatching individual events in Commit():
if dispatcher != nil && len(allEvents) > 0 {
    for _, event := range allEvents {
        _ = dispatcher.Dispatch(ctx, event)
    }
    // Fire batch event
    batchEnvelope := domain.EventEnvelope[any]{
        AggregateID: aggregateID,
        EventType:   "UnitOfWork.Committed",
        Payload:     BatchCommitted{Events: allEvents},
    }
    _ = dispatcher.Dispatch(ctx, batchEnvelope)
}
```

**Example — handler:**
```go
domain.Subscribe(d, "UnitOfWork.Committed",
    func(ctx context.Context, env domain.EventEnvelope[BatchCommitted]) error {
        var resourceCreated *entities.ResourceCreated
        var triples []entities.TripleCreated
        for _, e := range env.Payload.Events {
            switch p := e.Payload.(type) {
            case *entities.ResourceCreated:
                if p.TypeSlug == "product" {
                    resourceCreated = p
                }
            case *entities.TripleCreated:
                triples = append(triples, *p)
            }
        }
        if resourceCreated != nil {
            return onProductFullyCreated(ctx, resourceCreated, triples)
        }
        return nil
    },
)
```

**Pros:**
- Handler receives ALL events from the transaction in one shot — full picture guaranteed
- Aligns naturally with UoW boundaries, which are the unit of consistency in the codebase
- Relatively simple to implement (small change to pericarp's Commit method)
- No in-memory state to manage or clean up
- Works for creation, update, and delete flows — any UoW commit triggers it

**Cons:**
- Requires a change to pericarp (the external library). Since you own pericarp, this is feasible but couples a WeOS-specific need into the general-purpose library.
- The `BatchCommitted` event is a "meta-event" — it breaks the pattern that events map to domain concepts. Handlers must type-switch on heterogeneous payloads, which is less clean than typed subscriptions.
- The batch event fires AFTER all individual handlers have already run. If individual handlers mutate state (like projection sync), the batch handler sees the pre-mutation event data but post-mutation DB state — potential confusion.
- Naming the aggregate ID is ambiguous when a UoW tracks multiple aggregates (though current usage is always single-aggregate)
- Every UoW.Commit triggers this event, even when no handler cares. Minor overhead, but it means every handler for this event type must filter for relevance.
- Does not help if the need is to react to events across different UoW commits (e.g., resource created in one request, triple added in a later request)

---

### Option 4: React from the Projection (Read Model)

Instead of reacting to raw events, trigger the handler after the projection is fully updated. The handler reads the current state from the projection table, which already includes the resource data and FK columns populated by the triple handlers.

**Example — using AfterCreate behavior hook:**
```go
type ProductBehavior struct {
    entities.DefaultBehavior
    resourceRepo repositories.ResourceRepository
    tripleRepo   repositories.TripleRepository
}

func (b *ProductBehavior) AfterCreate(ctx context.Context, entity *entities.Resource) error {
    // By this point, UoW.Commit() has completed and all event handlers
    // (including triple projection sync) have run.
    // Read the fully-projected state.
    resource, _ := b.resourceRepo.FindByID(ctx, entity.GetID())
    triples, _ := b.tripleRepo.FindBySubject(ctx, entity.GetID())
    return onProductReady(ctx, resource, triples)
}
```

**Example — using a dedicated post-commit dispatcher (new infrastructure):**
```go
// In resource_service.go, after UoW.Commit returns:
uow.Commit(ctx)
s.postCommitDispatcher.Dispatch(ctx, PostCommitEvent{
    Entity: entity,
    Type:   cmd.TypeSlug,
})

// Handler reads projection for complete state
func handleProductPostCommit(ctx context.Context, evt PostCommitEvent) error {
    if evt.Type != "product" { return nil }
    resource, _ := resourceRepo.FindByID(ctx, evt.Entity.GetID())
    triples, _ := tripleRepo.FindBySubject(ctx, evt.Entity.GetID())
    return onProductReady(ctx, resource, triples)
}
```

**Pros:**
- Handler always sees the complete, consistent state — no timing issues
- Can leverage the existing `ResourceBehavior.AfterCreate` / `AfterUpdate` hooks without new infrastructure
- Simple to understand: "after everything is done, read the result and act"
- No changes needed to pericarp or the event dispatcher
- Natural fit for type-specific logic (the behavior registry already dispatches by resource type slug)

**Cons:**
- Adds extra database reads (FindByID + FindBySubject) that duplicate work the handlers already did
- The `AfterCreate` hook runs after `UoW.Commit()`, but dispatch errors are swallowed — so if a triple handler failed silently, the projection may be incomplete and the post-commit handler would act on bad data without knowing it
- Couples the reaction to the service layer (behavior hooks) rather than the event system — loses the decoupling benefit of event-driven architecture
- AfterCreate errors are currently logged but don't fail the operation (`resource_service.go:183`). If the post-commit action is important (e.g., sending a notification), silent failure is risky.
- Only works for operations that go through the service layer. Direct event replay or other event consumers don't trigger behaviors.
- Behavior hooks are registered per resource type slug, which is good for type-specific logic but doesn't generalize to cross-type reactions

---

### Option 5: Generic "Resource.Published" Composite Event

Since all resources use the generic `Resource` entity, the composite event doesn't need to be type-specific. Instead, the service records a `Resource.Published` event as the **last event** on every resource creation, carrying the fully assembled data plus all relationships. This is a generic lifecycle event: "this resource and all its relationships have been recorded and are ready for consumption."

The `Resource.Published` event is a **pure signal** — it carries only the resource ID and type slug, not a copy of the data. It tells handlers "this resource is fully assembled — go get what you need." Handlers choose their own data access strategy: read from the projection (for current state) or hydrate from the event store (for event replay scenarios).

**Example — new event type:**
```go
type ResourcePublished struct {
    TypeSlug  string    `json:"typeSlug"`
    Timestamp time.Time `json:"timestamp"`
}

func (e ResourcePublished) EventType() string { return "Resource.Published" }
```

**Example — in resource_service.go Create(), after all atomic events:**
```go
entity.RecordEvent(ResourceCreated{...}, "Resource.Created")
for _, ref := range refs {
    entity.RecordEvent(TripleCreated{...}, "Triple.Created")
}

// Signal: this resource is fully assembled
published := entities.ResourcePublished{
    TypeSlug:  cmd.TypeSlug,
    Timestamp: time.Now(),
}
entity.RecordEvent(published, published.EventType())
```

**Example — handler reads from projection:**
```go
domain.Subscribe(d, "Resource.Published",
    func(ctx context.Context, env domain.EventEnvelope[entities.ResourcePublished]) error {
        if env.Payload.TypeSlug != "product" {
            return nil
        }
        // Read the final state — projection handlers have already run
        resource, err := resourceRepo.FindByID(ctx, env.AggregateID)
        if err != nil {
            return err
        }
        triples, err := tripleRepo.FindBySubject(ctx, env.AggregateID)
        if err != nil {
            return err
        }
        return onProductReady(ctx, resource, triples)
    },
)
```

**Example — handler hydrates from event store (for replay):**
```go
domain.Subscribe(d, "Resource.Published",
    func(ctx context.Context, env domain.EventEnvelope[entities.ResourcePublished]) error {
        if env.Payload.TypeSlug != "product" {
            return nil
        }
        // Hydrate from event history — works during replay
        events, err := eventStore.Load(ctx, env.AggregateID)
        if err != nil {
            return err
        }
        return onProductReadyFromEvents(ctx, env.AggregateID, events)
    },
)
```

**Pros:**
- **Pure signal, no data duplication** — the event is tiny (just type slug + timestamp). No risk of the event snapshot drifting from the actual state.
- Generic — fires for every resource, preserves the uniform resource model, no type-specific branching in the service layer
- **Handler chooses its own data strategy** — read projection for current state, hydrate from event store for replay. Each handler gets exactly the data it needs, in the form it needs.
- Persisted in the event store — survives restarts, supports replay
- Dispatched last, after all `Triple.Created` events — by the time `Resource.Published` handlers run, all atomic projection handlers have already run (projection table, triples table, graph sync are populated)
- Fits naturally into the existing `domain.Subscribe` model
- Works for updates too: record `Resource.Updated` + triple reconciliation + `Resource.Published`
- Minimal event store overhead — the payload is just two fields

**Cons:**
- Handlers must read from the DB or event store — adds read latency and DB load per handler invocation. Unlike a fat event, the handler can't operate from the payload alone.
- Since handlers for `Resource.Published` run **in parallel** (pericarp dispatcher uses errgroup), and since projection handlers for the preceding events also run in parallel, there's a subtle race: the `Resource.Published` handler might query the projection before a slow triple projection handler has finished writing. The current dispatch model (events dispatched sequentially, handlers per event in parallel) mitigates this — `Resource.Published` is dispatched after all `Triple.Created` dispatches return — but it depends on all prior handlers completing before the next `Dispatch()` call, which is the current UoW behavior.
- Only signals creation/update-time assembly. Triples added later via `TripleService.Link()` don't fire `Resource.Published` — a handler that needs to react to post-creation relationship changes won't be triggered.
- Introduces the "published" lifecycle concept. Needs clear documentation that this is a technical assembly-complete signal, not a user-facing publish/draft workflow.
- Every resource write fires this event, even when no handler is subscribed — minor overhead but non-zero

---

## Comparison Matrix

| Criteria | Option 1: Listen to Triple | Option 2: Saga | Option 3: Batch Event | Option 4: Read Projection | Option 5: Composite Event |
|---|---|---|---|---|---|
| **Implementation effort** | None | High | Medium (pericarp change) | Low (use existing hooks) | Low (new event type only) |
| **Infrastructure changes** | None | New saga framework | Modify pericarp UoW | None or small dispatcher | None |
| **Single-relationship reactions** | Excellent | Overkill | Works | Works | Works |
| **Multi-relationship reactions** | Poor (partial view) | Excellent | Good | Good (reads from DB) | Good (handler reads what it needs) |
| **Cross-aggregate reactions** | No | Yes | No (single UoW) | No | No |
| **Survives restart** | Yes (events persisted) | No (in-memory) | Yes (events persisted) | N/A | Yes (events persisted) |
| **Supports event replay** | Yes | Needs durable state | Yes | No | Yes (handler can hydrate from store) |
| **Maintains generic resource service** | Yes | Yes | Yes | Mostly (behaviors) | Yes (generic event) |
| **Handler complexity** | Simple + filter | Complex | Medium (type-switch) | Simple | Simple (filter by TypeSlug + read) |
| **Existing pattern in codebase** | Yes (triple_handler.go) | No | No | Partial (AfterCreate) | No |
| **Data duplication in event store** | None | None | None | None | None (signal only) |
| **Extra DB reads in handler** | Sometimes | No | No | Always | Always (handler's choice) |

## Recommendation

**Option 1 (Listen to Triple)** remains the simplest choice for single-relationship reactions. Zero changes, follows the existing `triple_handler.go` pattern.

**Option 5 (Resource.Published signal)** is the strongest general-purpose solution. It's a pure signal — no data duplication, no pericarp changes, preserves the generic resource model, and lets each handler decide how to get the state it needs (projection read vs event hydration). The tradeoff is one extra lightweight event per write and a DB read per handler invocation.

**Option 3 (Batch Event)** solves a similar problem but requires modifying pericarp and forces handlers to type-switch on heterogeneous payloads. Option 5 is preferable unless you specifically need the batch of raw events without any DB reads.

**Option 4 (Read Projection)** is appropriate for side-effect-only handlers (notifications, external calls) where event replay doesn't matter. The existing `AfterCreate` behavior hook makes this zero-infrastructure. However, Option 5 subsumes this — a `Resource.Published` handler that reads the projection is functionally identical, but also works during event replay.

**Reserve Option 2 (Saga)** for future cross-aggregate coordination needs that span multiple requests.