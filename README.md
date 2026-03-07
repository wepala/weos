# WeOS

A Go microservices template following Clean Architecture principles with event sourcing support.

## Features

- **Clean Architecture** with domain-driven design
- **Event Sourcing** via [pericarp](https://github.com/akeemphilbert/pericarp) library
- **Dependency Injection** with Uber Fx
- **Dual Entry Points** - API server (Echo) + CLI (Cobra)
- **Auto-detecting Database** - SQLite for development, PostgreSQL for production
- **Frontend Embedding** - Single-binary deployment with SPA support
- **Structured Logging** - Zap with interface abstraction
- **KSUID Identity** - Time-sortable, URL-safe entity IDs
- **Auth Ready** - Pericarp auth integration with OAuth/session support

## Project Structure

```
weos/
├── application/         # DI module and service providers
├── cmd/
│   ├── api/            # API server entry point
│   └── cli/            # CLI entry point
├── domain/             # Domain entities and business logic
│   ├── entities/       # Domain entities (embed ddd.BaseEntity)
│   ├── repositories/   # Repository interfaces
│   └── services/       # Domain services
├── infrastructure/     # External concerns
│   ├── database/       # GORM database provider
│   ├── events/         # Event dispatcher provider
│   ├── external/       # External service clients
│   ├── logging/        # Zap logging implementation
│   └── models/         # GORM models
├── api/                # API layer
│   ├── handlers/       # HTTP handlers
│   ├── middleware/      # HTTP middleware (SPA, auth)
│   └── validators/     # Request validators
├── pkg/                # Public packages
│   ├── errors/         # Error definitions
│   ├── identity/       # KSUID-based entity ID generation
│   ├── utils/          # Utility functions
│   └── validators/     # Validation utilities
├── internal/           # Private application code
│   ├── auth/           # Authentication logic
│   ├── cli/            # Cobra CLI setup and DI
│   ├── config/         # Configuration management
│   ├── logging/        # Logging utilities
│   └── observability/  # OpenTelemetry setup
├── web/                # Embedded frontend assets
├── tests/              # Test files
│   ├── unit/           # Unit tests
│   ├── integration/    # Integration tests
│   ├── e2e/            # E2E tests (Godog/Gherkin)
│   └── newman/         # API canary tests
├── config/             # Configuration files
├── migrations/         # Database migrations
├── scripts/            # Utility scripts
└── docs/               # Documentation
```

## Getting Started

1. Clone and rename the module:
```bash
# Update go.mod module name
# Find and replace "weos" with your module name across all Go files
```

2. Install dependencies:
```bash
make deps
```

3. Run the API server:
```bash
make run
```

4. Build all binaries:
```bash
make build
```

## Key Patterns

### Dependency Injection (Uber Fx)

All dependencies are wired in `application/module.go`. Add providers and invoke hooks there.

### Event Sourcing (Pericarp)

Domain entities embed `*ddd.BaseEntity` and record events via `RecordEvent()`. Services use `SimpleUnitOfWork` for atomic event persistence and dispatch.

### Configuration

Three-layer precedence: Defaults -> Environment Variables -> CLI Flags.

### Database

Auto-detects SQLite vs PostgreSQL from DSN format. Use SQLite locally, PostgreSQL in production.

## Development

See `CONTRIBUTING.md` for contribution guidelines.
