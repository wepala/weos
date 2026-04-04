# Architecture Documentation

## Clean Architecture

This project follows Clean Architecture principles with clear separation of concerns.

### Layers

1. **Domain Layer** (`domain/`)
   - Contains business entities and core business logic
   - No dependencies on external frameworks
   - Defines repository interfaces
   - Entities embed `*ddd.BaseEntity` from pericarp for event sourcing

2. **Application Layer** (`application/`)
   - Uber Fx dependency injection module
   - Service provider functions
   - Event handler subscriptions
   - Use case orchestration

3. **Infrastructure Layer** (`infrastructure/`)
   - Database implementations (GORM with auto-detect SQLite/PostgreSQL)
   - Event dispatcher provider (pericarp)
   - Structured logging (Zap)
   - External service clients
   - Implements domain interfaces

4. **API Layer** (`api/`)
   - HTTP handlers (Echo v4)
   - SPA middleware with embedded frontend assets
   - Auth middleware
   - Request validation

### Dependency Flow

```
API → Application → Domain ← Infrastructure
```

All dependencies point inward toward the domain layer.

## Design Patterns

- **Repository Pattern**: Abstract data access with interfaces in domain
- **Dependency Injection**: Uber Fx with Module pattern
- **Event Sourcing**: Domain events via pericarp BaseEntity
- **Unit of Work**: Atomic event persistence and dispatch
- **CQRS**: Command/event separation via pericarp dispatchers
- **Service Layer**: Business logic in domain services
- **Middleware Pattern**: Cross-cutting concerns (auth, logging, tracing, SPA)

## Event Sourcing Flow

```
1. Entity.RecordEvent(payload, eventType)
2. Service tracks entity in UnitOfWork
3. UnitOfWork.Commit(ctx)
   → EventStore.Append(events)
   → EventDispatcher.Dispatch(events)
4. Event handlers update projections / trigger side-effects
```

## Observability

- OpenTelemetry for distributed tracing
- Structured logging with context propagation via Zap
- Metrics collection via Prometheus
- Request ID correlation across services
