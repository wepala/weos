---
title: Architecture
parent: Explanation
layout: default
nav_order: 6
---

# Architecture

WeOS follows **Clean Architecture** principles with four layers. Dependencies point inward — outer layers depend on inner layers, never the reverse. This keeps the domain logic free from infrastructure concerns and makes the system testable and adaptable.

## Layers

```
API → Application → Domain ← Infrastructure
```

### Domain Layer (`domain/`)

The innermost layer. Contains business entities, value objects, repository interfaces, and domain events. Has **no dependencies** on external frameworks.

Key contents:
- `domain/entities/` — ResourceType, Resource, and their event types
- `domain/repositories/` — Repository interfaces (implemented by infrastructure)
- `domain/` — Shared types like EventEnvelope, BasicTripleEvent

Entities embed `*ddd.BaseEntity` from the Pericarp library for event sourcing support.

### Application Layer (`application/`)

Orchestrates use cases by coordinating domain entities, repositories, and the Unit of Work. Contains services, DI configuration, and event handler subscriptions.

Key contents:
- `application/module.go` — Uber Fx dependency injection module
- `application/providers.go` — Provider functions for services
- `application/resource_type_service.go`, `resource_service.go` — Service implementations
- `application/presets/` — Built-in resource type presets

Services own the UnitOfWork lifecycle: they create a UoW, track entities, and commit or rollback.

### Infrastructure Layer (`infrastructure/`)

Implements the interfaces defined by the domain layer. Provides concrete database access, event storage, logging, and external integrations.

Key contents:
- `infrastructure/database/gorm/` — GORM-based repositories, ProjectionManager, EventStore
- `infrastructure/events/` — EventDispatcher provider
- `infrastructure/logging/` — Zap structured logging

The GORM provider auto-detects the database driver from the DSN format (SQLite for file paths, PostgreSQL for connection URIs).

### API Layer (`api/`)

The outermost layer. HTTP handlers, middleware, and route registration.

Key contents:
- `api/handlers/` — Request handlers for each resource type
- `api/middleware/` — Auth, authorization, impersonation, SPA static serving

## Dependency Injection

WeOS uses **Uber Fx** for dependency injection. The entire application is wired in `application/module.go`:

```
Config
  ↓
Logger, Database, EventStore, EventDispatcher, SessionStore
  ↓
Repositories + ProjectionManager
  ↓
Services
  ↓
Lifecycle Hooks (projection table migration)
```

The `Module(cfg config.Config, registry *PresetRegistry)` function accepts a Config and returns an `fx.Option` that provides all dependencies. Both the CLI commands and the API server use this same module.

### Provider Pattern

Providers use `fx.In`/`fx.Out` struct injection:

```go
type ResourceServiceParams struct {
    fx.In
    EventStore  domain.EventStore
    Dispatcher  domain.EventDispatcher
    Repo        repositories.ResourceRepository
    Logger      entities.Logger
}

type ResourceServiceResult struct {
    fx.Out
    Service *ResourceService
}
```

This pattern makes dependencies explicit and testable.

## Request Lifecycle

A typical API request flows through these layers:

1. **HTTP request** arrives at the Echo router
2. **Middleware** chain processes it (auth, authorization, impersonation)
3. **Handler** extracts request data and calls the appropriate service method
4. **Service** creates a UnitOfWork, performs domain operations
5. **Domain entity** records events (e.g., Resource.Created, Triple.Created, Resource.Published)
6. **UnitOfWork.Commit()** persists events to the EventStore and dispatches them
7. **Event handlers** update projections and trigger side effects
8. **Handler** returns the response

The MCP server follows the same flow, but step 1 is an MCP tool call instead of an HTTP request.

## Unified Binary

The `cmd/weos/main.go` entry point provides a single binary with multiple modes via Cobra commands:

- `weos serve` — starts the HTTP server
- `weos mcp` — starts the MCP server
- `weos resource-type ...` — CLI management of resource types
- `weos resource ...` — CLI management of resources
- `weos person ...` / `weos organization ...` — CLI for persons and organizations
- `weos seed` — seeds development data

Each command creates its own Config and passes it to the shared `application.Module()`.

## Further Reading

- [Architecture Overview]({% link architecture/README.md %}) — concise layer and pattern summary
- [Event Store]({% link _explanation/event-store.md %}) — how events flow through the system
- [Projections]({% link _explanation/projections.md %}) — how event handlers update read models
- [Contributing]({% link contributing.md %}) — development workflow and code standards
