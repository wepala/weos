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
- **Knowledge graph** — site structure stored as a graph (pages, sections, content blocks, links, metadata)
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
- KSUID for entity ID generation
- Gorilla Sessions for HTTP session management
- Ontologies: Schema.org, FOAF, vCard, W3C ORG, Activity Streams 2.0, GoodRelations, PROV-O, SKOS

## Essential Commands

### Building
```bash
make build              # Build all applications (API + CLI)
make build-api          # Build API server only
make build-cli          # Build CLI only
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
make run                # Run the API server (go run ./cmd/api)
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

### Dependency Injection with Uber Fx

The entire application is wired using Uber Fx (`application/module.go`):

**Dependency Graph:**
```
Config
  ↓
Logger, Database, EventStore, EventDispatcher, SessionStore
  ↓
Repositories
  ↓
Services
  ↓
Event Handlers + Lifecycle Hooks
```

**Module Pattern:** The `Module(cfg config.Config)` function accepts a Config and returns an `fx.Option` that provides all dependencies. Applications (CLI, API) create their own Config and pass it to the Module.

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
    e.BaseEntity = ddd.NewBaseEntity(identity.New("my-entity"))
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

### Dual Entry Points

- **API Server** (`cmd/api/main.go`): Echo HTTP server with Fx DI, SPA middleware, graceful shutdown. Routes are registered under `/api` prefix; all other paths fall through to SPA middleware.
- **CLI** (`cmd/cli/main.go`): Cobra commands sharing the same Fx DI container via `internal/cli/di.go`. Commands use `StartContainer()` to get services.

### Configuration

**Loading Order** (each step overrides the previous):
1. `config.Default()` — sensible defaults (SQLite `weos.db`, port 8080)
2. `godotenv.Load()` — loads `.env` file into process environment (called in entry points)
3. `cfg.LoadFromEnvironment()` — reads environment variables into Config struct
4. CLI flags — `--database-dsn`, `--verbose` (CLI only)

**Environment Variables:**
| Variable | Purpose | Default |
|----------|---------|---------|
| `DATABASE_DSN` | Database connection string | `weos.db` |
| `LOG_LEVEL` | Logging level (debug/info/warn/error) | `info` |
| `SERVER_PORT` / `PORT` | HTTP server port | `8080` |
| `SERVER_HOST` | HTTP server bind address | `0.0.0.0` |
| `IDENTITY_BASE_PATH` | Base URL for entity IDs | `https://example.com/weos` |
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
| DI Module | `application/module.go` |
| DI Providers | `application/providers.go` |
| API Server | `cmd/api/main.go` |
| CLI Entry | `cmd/cli/main.go` |
| CLI DI | `internal/cli/di.go` |
| Config | `internal/config/config.go` |
| Health Handler | `api/handlers/health.go` |
| SPA Middleware | `api/middleware/static.go` |
| DB Provider | `infrastructure/database/gorm/provider.go` |
| Logger | `infrastructure/logging/zap_logger.go` |
| Event Dispatcher | `infrastructure/events/dispatcher_provider.go` |
| Identity | `pkg/identity/identity.go` |
| Pagination | `domain/repositories/pagination.go` |
| Logger Interface | `domain/entities/logger.go` |
| Frontend Embed | `web/embed.go` |

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
3. Register routes under `/api` group in `cmd/api/main.go`

### Adding a New CLI Command
1. Create command file in `internal/cli/`
2. Register with `rootCmd` in `init()`
3. Use `StartContainer()` to access services

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
