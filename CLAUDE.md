# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**WeOS**  is an open source website system where your AI is the webmaster. Users describe what they want in natural language and the site updates accordingly. WeOS never calls any LLM directly — it exposes an MCP server that any MCP-compatible LLM (Claude, GPT, Gemini, Ollama) connects to for driving edits. Output is static-first HTML.

See `.claude/local-context.md` for full product vision, user personas, and business context.

### What the Go Binary Does
1. **Generates and serves the static site** — templates + content → HTML
2. **Runs the MCP server** — LLM-driven edits via MCP protocol
3. **Serves as the API backend** — escape hatch for dynamic needs

### Key Design Decisions
- **LLM-agnostic** — MCP server interface, no direct LLM calls
- **Static-first** — static HTML output, server-side rendering only when opted in
- **Ontology-backed entities** — content objects (products, events, services) are RDF-typed using Schema.org, FOAF, etc. — gives free SEO structured data and grounded LLM reasoning
- **Resource types with dynamic projection tables** — when a resource type is created, a dedicated projection table is generated from its JSON Schema. All content (websites, pages, products, etc.) is modeled as resource types + resources.
- **Template system** — bring-your-own HTML templates annotated with `data-weos-*` attributes (e.g., `data-weos-entity="Product"`, `data-weos-slot="hero.headline"`)
- **Dual license** — AGPL 3.0 (open source) + commercial license via WeOS Cloud

### Technology Stack
- Go 1.24
- Event Sourcing / CQRS (pericarp library: BaseEntity, EventDispatcher, UnitOfWork, EventStore)
- Uber Fx for dependency injection
- GORM for database (auto-detects PostgreSQL/SQLite from DSN)
- Echo v4 for HTTP server with SPA middleware
- Cobra for CLI
- Zap for structured logging
- KSUID for entity ID generation; URN format: ResourceType `urn:type:<slug>`, Resource `urn:<typeSlug>:<ksuid>`
- Gorilla Sessions for HTTP session management
- Ontologies: Schema.org, FOAF, vCard, W3C ORG, Activity Streams 2.0, GoodRelations, PROV-O, SKOS

## Essential Commands

### Building
```bash
make build              # Build the weos binary
```

### Testing
```bash
make test               # Run all tests with race detection and coverage
make test-unit          # Run unit tests only (tests/unit/)
make test-integration   # Run integration tests only (tests/integration/)
make test-e2e           # Run E2E tests (tests/e2e/)
go test -v -run TestName ./path/to/package  # Run a single test
```

### Development
```bash
make run                # Run the API server (go run ./cmd/weos serve)
make fmt                # Format code (go fmt + goimports)
make lint               # Run golangci-lint
make vet                # Run go vet
make deps               # Download and tidy dependencies
make mocks              # Generate mocks (requires moqup)
make clean              # Remove build artifacts
```

**Tool prerequisites:** `goimports` (for `make fmt`) and `moqup` (for `make mocks`) must be installed separately.

## Linting Constraints

golangci-lint is configured with strict rules (`.golangci.yml`):
- **Line length:** 120 characters max
- **Function length:** 100 lines / 50 statements max
- **Cyclomatic complexity:** 15 max
- **Duplicate threshold:** 100 tokens
- These limits are relaxed in `_test.go` files (errcheck, dupl, funlen, gocognit excluded)

## Architecture Overview

### Unified Binary

There is a single binary (`cmd/weos/main.go`) that serves as both the CLI and API server. Cobra subcommands provide different modes:
- `weos serve` — starts the Echo HTTP server (API + SPA middleware)
- `weos mcp` — starts the MCP server (stdio transport)
- `weos resource-type ...` — manage resource types
- `weos resource ...` — manage resources
- `weos person ...` / `weos organization ...` — manage persons and organizations

Routes are registered under `/api` prefix; all other paths fall through to SPA middleware.

### Dependency Injection with Uber Fx

The entire application is wired using Uber Fx (`application/module.go`):

**Dependency Graph:**
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

**Module Pattern:** The `Module(cfg config.Config)` function accepts a Config and returns an `fx.Option` that provides all dependencies. Both the CLI and API server create their own Config and pass it to the Module.

**Fx Provider Pattern:** Providers use `fx.In`/`fx.Out` struct injection. See `application/providers.go` for the template. For named dependencies, use `fx.ResultTags` in `module.go` and name tags in provider structs.

### Event Sourcing (Pericarp)

Domain entities embed `*ddd.BaseEntity`:
```go
type MyEntity struct {
    *ddd.BaseEntity
    name string
}
```

**Event Recording:**
```go
func (e *MyEntity) With(name string) (*MyEntity, error) {
    e.BaseEntity = ddd.NewBaseEntity(identity.NewResourceType(slug))
    e.RecordEvent(MyEntityCreated{Name: name}, "MyEntity.Created")
    return e, nil
}
```

**Unit of Work Pattern:**
```go
uow := application.NewSimpleUnitOfWork(eventStore, dispatcher)
uow.Track(entity)
uow.Commit(ctx)
```

### Resource Types and Dynamic Projection Tables

Content is modeled using two core entities:
- **ResourceType** — defines a type (e.g. "product", "blog-post") with JSON-LD context and optional JSON Schema
- **Resource** — an instance of a ResourceType, storing JSON-LD data

When a ResourceType is created, the `ProjectionManager` creates a dedicated SQL table for it:
- Table name derived from slug: `blog-post` → `blog_posts` (hyphens→underscores, pluralized)
- Standard columns: `id`, `type_slug`, `data`, `status`, `sequence_no`, `created_at`, `updated_at`, `deleted_at`
- Additional typed columns extracted from JSON Schema properties (camelCase→snake_case, JSON types→SQL types)
- The `data` column always stores the full JSON-LD blob; typed columns exist for query optimization

The `ResourceRepository` routes CRUD operations to projection tables when available, falling back to the generic `resources` table for pre-existing data.

### Configuration

**Loading Order** (each step overrides the previous):
1. `config.Default()` — sensible defaults (SQLite `weos.db`, port 8080)
2. `godotenv.Load()` — loads `.env` file into process environment (called in entry point)
3. `cfg.LoadFromEnvironment()` — reads environment variables into Config struct
4. CLI flags — `--database-dsn`, `--verbose`

**Environment Variables:**
| Variable | Purpose | Default |
|----------|---------|---------|
| `DATABASE_DSN` | Database connection string | `weos.db` |
| `LOG_LEVEL` | Logging level (debug/info/warn/error) | `info` |
| `SERVER_PORT` / `PORT` | HTTP server port | `8080` |
| `SERVER_HOST` | HTTP server bind address | `0.0.0.0` |
| `SESSION_SECRET` | Session cookie secret | `change-me-in-production` |

### Database

Auto-detects driver from DSN format:
- SQLite: file paths or `file:` URIs (default: `weos.db`)
- PostgreSQL: `host=...` or `postgres://...` URIs

Add GORM models for AutoMigrate in `infrastructure/database/gorm/provider.go`.

### Logging

The `entities.Logger` interface uses variadic key-value pairs:
```go
logger.Info(ctx, "user created", "userID", user.ID, "email", email)
```
`debug` log level uses Zap's development config; all others use production config.

## Key Code Locations

| Component | Path |
|-----------|------|
| Entry Point | `cmd/weos/main.go` |
| DI Module | `application/module.go` |
| DI Providers | `application/providers.go` |
| CLI Root | `internal/cli/root.go` |
| CLI DI | `internal/cli/di.go` |
| CLI Serve | `internal/cli/serve.go` |
| Config | `internal/config/config.go` |
| Health Handler | `api/handlers/health.go` |
| SPA Middleware | `api/middleware/static.go` |
| DB Provider | `infrastructure/database/gorm/provider.go` |
| ProjectionManager | `infrastructure/database/gorm/projection_manager.go` |
| Logger | `infrastructure/logging/zap_logger.go` |
| Event Dispatcher | `infrastructure/events/dispatcher_provider.go` |
| Identity | `pkg/identity/identity.go` |
| Pagination | `domain/repositories/pagination.go` |
| Logger Interface | `domain/entities/logger.go` |
| Frontend Embed | `web/embed.go` |
| MCP Server | `internal/mcp/server.go` |

## Common Patterns When Making Changes

### Adding a New Domain Entity
1. Create entity in `domain/entities/` embedding `*ddd.BaseEntity`
2. Define event structs in `domain/entities/*_events.go`
3. Implement `With()` constructor that records creation event
4. Implement `ApplyEvent()` for event sourcing reconstruction

### Adding a New Repository
1. Define interface in `domain/repositories/`
2. Implement in `infrastructure/database/gorm/`
3. Add provider function in the gorm package
4. Register in `application/module.go`

### Adding a New Service
1. Create service in `application/`
2. Define interface and implementation
3. Add provider in `application/providers.go`
4. Register in `application/module.go`
5. Use `SimpleUnitOfWork` for event persistence

### Adding a New API Handler
1. Create handler in `api/handlers/`
2. Inject service via constructor
3. Register routes under `/api` group in `internal/cli/serve.go`
4. Use the response envelope helpers for all JSON responses (see below)

### API Response Envelope

All API responses (except `/health` and static files) use a standard envelope:

**Success:** `respond(c, status, data)` → `{"data": <data>, "messages": [...]}`
**Paginated:** `respondPaginated(c, status, data, cursor, hasMore)` → `{"data": [...], "cursor": "...", "has_more": bool}`
**Error:** `respondError(c, status, msg)` → `{"error": "msg"}`
**Raw JSON:** `respondRaw(c, status, rawBytes)` — for pre-serialized JSON (e.g., JSON-LD)

The `messages` key is omitted when empty. `respondError` does not add the error itself to `messages`; that array only contains messages accumulated via context helpers. Services can surface non-fatal messages via context helpers:
```go
entities.AddMessage(ctx, entities.Message{Type: "warning", Text: "schema missing"})
```
Messages are accumulated per-request by the `Messages()` middleware and automatically included in responses.

### Adding a New CLI Command
1. Create command file in `internal/cli/`
2. Register with `rootCmd` in `init()`
3. Use `StartContainer()` to access services

### Adding a New Resource Type
1. Define a preset in `application/resource_type_presets.go` (or create via API/MCP)
2. Include JSON-LD context and optional JSON Schema
3. On creation, a projection table is auto-generated by `ProjectionManager`
4. Resources of that type are stored in the dedicated projection table

### Adding Event Handlers
1. Create handler in `application/`
2. Subscribe in module.go via `fx.Invoke` using `domain.Subscribe[any]()`

## Architectural Constraints

1. **Never persist entities directly** — always use UnitOfWork
2. **Events are immutable** — never modify events after creation
3. **Event handlers must be idempotent** — support event replay
4. **Config passed to Module** — each entry point creates Config and passes to Module
5. **Services own UnitOfWork lifecycle** — create, track, commit/rollback
6. **Dependencies point inward** — API → Application → Domain ← Infrastructure

## CI

GitHub Actions runs on pushes/PRs to `main` and `develop` branches: build, test (with race detection + coverage uploaded to Codecov), and lint.