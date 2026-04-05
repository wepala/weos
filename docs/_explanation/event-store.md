---
title: Event Store
parent: Explanation
layout: default
nav_order: 3
---

# Event Store

WeOS uses **event sourcing** as its core persistence strategy. Instead of storing the current state of an entity (like a traditional ORM), WeOS stores the sequence of events that led to that state. The current state is derived by replaying events in order.

## Why Event Sourcing?

Event sourcing provides several properties that are valuable for a CMS where an AI manages content:

1. **Complete audit trail** — every change is recorded as an immutable event. You can see exactly what happened, when, and in what order.
2. **Time travel** — you can reconstruct the state of any entity at any point in time by replaying events up to that moment.
3. **Undo capability** — because events are immutable, you can reason about reversing changes.
4. **Decoupled projections** — the write model (events) is separate from the read model (projections). You can build multiple views from the same events.
5. **AI safety** — when an LLM makes changes via MCP, every change is traceable. If something goes wrong, you can see exactly what happened and rebuild from events.

## The Pericarp Library

WeOS uses the [Pericarp](https://github.com/wepala/pericarp) library for event sourcing primitives:

### BaseEntity

Every domain entity embeds `*ddd.BaseEntity`, which provides event recording and replay:

```go
type Resource struct {
    *ddd.BaseEntity
    typeSlug  string
    data      json.RawMessage
    status    string
    createdBy string
    accountID string
    createdAt time.Time
}
```

### Recording Events

Entity methods record events to capture state changes. Events are not persisted immediately — they're queued on the entity:

```go
func (r *Resource) With(id, typeSlug string, data json.RawMessage, createdBy, accountID string) (*Resource, error) {
    r.BaseEntity = ddd.NewBaseEntity(id)
    r.RecordEvent(ResourceCreated{
        TypeSlug:  typeSlug,
        Data:      data,
        CreatedBy: createdBy,
        AccountID: accountID,
        Timestamp: time.Now(),
    }, "Resource.Created")
    return r, nil
}
```

### Applying Events

The `ApplyEvent` method reconstructs entity state from events during replay:

```go
func (r *Resource) ApplyEvent(ctx context.Context, envelope domain.EventEnvelope[any]) error {
    switch envelope.Type {
    case "Resource.Created":
        // set fields from event payload
    case "Resource.Updated":
        // update fields
    case "Resource.Deleted":
        // mark as archived
    case "Triple.Created":
        // update @graph edges
    }
    return nil
}
```

## Unit of Work

The **Unit of Work** pattern coordinates event persistence and dispatch. Services never persist entities directly — they track entities in a UnitOfWork and commit them atomically:

```go
// In a service method:
uow := application.NewSimpleUnitOfWork(eventStore, dispatcher)
uow.Track(resource)
err := uow.Commit(ctx)
```

When `Commit` is called:
1. All queued events from tracked entities are collected
2. Events are appended to the EventStore in a single transaction
3. Events are dispatched to registered handlers via the EventDispatcher

If any step fails, the entire operation rolls back.

## Event Types

WeOS defines these domain events:

### ResourceType Events
| Event | Trigger | Payload |
|-------|---------|---------|
| `ResourceType.Created` | New type registered | Name, Slug, Description, Context, Schema |
| `ResourceType.Updated` | Type modified | Name, Slug, Description, Context, Schema, Status |
| `ResourceType.Deleted` | Type archived | Timestamp |

### Resource Events
| Event | Trigger | Payload |
|-------|---------|---------|
| `Resource.Created` | New resource | TypeSlug, Data, CreatedBy, AccountID |
| `Resource.Updated` | Resource modified | Data |
| `Resource.Deleted` | Resource archived | Timestamp |
| `Resource.Published` | All creation events committed | TypeSlug |

### Triple Events
| Event | Trigger | Payload |
|-------|---------|---------|
| `Triple.Created` | Relationship established | Subject, Predicate, Object |
| `Triple.Deleted` | Relationship removed | Subject, Predicate, Object |

## Event Dispatch

The EventDispatcher delivers events to registered handlers. Handlers subscribe to event patterns:

```go
domain.Subscribe[any](dispatcher, "Resource.%", handler)
```

The `%` wildcard matches any suffix, so `Resource.%` catches `Resource.Created`, `Resource.Updated`, `Resource.Deleted`, and `Resource.Published`.

Handlers must be **idempotent** — they may receive the same event more than once (during replay or retry). Design handlers so that processing an event twice produces the same result as processing it once.

## Event Store Implementations

WeOS supports two event store backends:

1. **GORM EventStore** — the default, stores events in the same database as projections (SQLite or PostgreSQL)
2. **BigQuery Dual-Write EventStore** — optionally writes events to both the primary database and Google BigQuery for analytics

The dual-write store is enabled when `BIGQUERY_PROJECT_ID` is configured.

## Key Constraints

1. **Events are immutable** — never modify an event after it's stored
2. **Handlers must be idempotent** — support event replay
3. **Never persist entities directly** — always use UnitOfWork
4. **Services own UnitOfWork lifecycle** — create, track, commit/rollback

## Further Reading

- [Projections]({% link _explanation/projections.md %}) — how events become queryable tables
- [Atomic Models and Triples]({% link _explanation/atomic-models-and-triples.md %}) — how triple events model relationships
- [ADR: Transaction ID and Projection Consolidation]({% link decisions/transaction-id-and-projection-consolidation.md %}) — how events are grouped into transactions
- [Events Reference]({% link _reference/events.md %}) — complete event type catalog
