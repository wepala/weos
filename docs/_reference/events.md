---
title: Events
parent: Reference
layout: default
nav_order: 8
---

# Events

WeOS uses event sourcing. All state changes are captured as immutable domain events. This page catalogs every event type, its payload, and when it fires.

## ResourceType Events

Pattern: `ResourceType.%`

### ResourceType.Created

Fired when a new resource type is registered (via preset install, CLI, or API).

| Field | Type | Description |
|-------|------|-------------|
| `Name` | string | Display name |
| `Slug` | string | URL-safe identifier |
| `Description` | string | Type description |
| `Context` | json.RawMessage | JSON-LD context |
| `Schema` | json.RawMessage | JSON Schema |
| `Timestamp` | time.Time | When the event occurred |

### ResourceType.Updated

Fired when a resource type's definition is modified.

| Field | Type | Description |
|-------|------|-------------|
| `Name` | string | Updated name |
| `Slug` | string | Updated slug |
| `Description` | string | Updated description |
| `Context` | json.RawMessage | Updated JSON-LD context |
| `Schema` | json.RawMessage | Updated JSON Schema |
| `Status` | string | Updated status |
| `Timestamp` | time.Time | When the event occurred |

### ResourceType.Deleted

Fired when a resource type is archived.

| Field | Type | Description |
|-------|------|-------------|
| `Timestamp` | time.Time | When the event occurred |

---

## Resource Events

Pattern: `Resource.%`

### Resource.Created

Fired when a new resource is created.

| Field | Type | Description |
|-------|------|-------------|
| `TypeSlug` | string | Resource type slug |
| `Data` | json.RawMessage | Full JSON-LD data |
| `CreatedBy` | string | Creator's agent ID |
| `AccountID` | string | Owning account ID |
| `Timestamp` | time.Time | When the event occurred |

### Resource.Updated

Fired when a resource's data is modified.

| Field | Type | Description |
|-------|------|-------------|
| `Data` | json.RawMessage | Updated JSON-LD data |
| `Timestamp` | time.Time | When the event occurred |

### Resource.Deleted

Fired when a resource is archived (soft-deleted).

| Field | Type | Description |
|-------|------|-------------|
| `Timestamp` | time.Time | When the event occurred |

### Resource.Published

A **signal event** fired after all creation events for a resource have been committed. This tells event handlers that the resource's data and relationships are fully available.

| Field | Type | Description |
|-------|------|-------------|
| `TypeSlug` | string | Resource type slug |
| `Timestamp` | time.Time | When the event occurred |

This event is the primary trigger for projection writes. See [ADR: Event Handler Data Availability]({% link decisions/event-handler-data-availability.md %}).

---

## Triple Events

Pattern: `Triple.%`

Triple events are recorded on the resource entity (same event stream). They model RDF relationships between resources.

### Triple.Created

Fired when a relationship between two resources is established.

| Field | Type | Description |
|-------|------|-------------|
| `Subject` | string | Source resource URN |
| `Predicate` | string | Relationship IRI (e.g., `https://schema.org/isPartOf`) |
| `Object` | string | Target resource URN |
| `Timestamp` | time.Time | When the event occurred |

### Triple.Deleted

Fired when a relationship between two resources is removed.

| Field | Type | Description |
|-------|------|-------------|
| `Subject` | string | Source resource URN |
| `Predicate` | string | Relationship IRI |
| `Object` | string | Target resource URN |
| `Timestamp` | time.Time | When the event occurred |

---

## Event Lifecycle

A typical resource creation produces this event sequence:

```
1. Resource.Created    — entity data stored
2. Triple.Created      — relationship(s) recorded (if any x-resource-type properties)
3. Resource.Published  — signal that all events are committed
```

Event handlers should:
- Subscribe to `Resource.Published` for projection writes (not `Resource.Created`)
- Be **idempotent** — the same event may be delivered more than once
- Never modify events after they're stored

## Subscribing to Events

Event handlers subscribe via pattern matching in `application/module.go`:

```go
domain.Subscribe[any](dispatcher, "Resource.%", myHandler)    // all resource events
domain.Subscribe[any](dispatcher, "ResourceType.%", myHandler) // all type events
```

The `%` wildcard matches any suffix.
