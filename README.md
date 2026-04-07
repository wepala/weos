# WeOS

WeOS is an open source Go application for building a **digital twin** of yourself or your business — a knowledge graph of the information from the apps and devices you use, exposed to any LLM so it can answer with your real context. WeOS never calls an LLM directly; it exposes an MCP server that any MCP-compatible LLM (Claude, GPT, Gemini, Ollama) connects to.

## What it does

1. **Stores your data as a knowledge graph** — resources are stored as JSON-LD documents typed with Schema.org, FOAF, vCard and other ontologies, with relationships between resources modeled as RDF triples, so people, events, products, places, messages and the relationships between them are first-class.
2. **Runs an MCP server** — any MCP-compatible LLM connects and queries your graph for grounded, context-rich responses.
3. **Optionally renders sites and APIs** — the same graph can drive a static-first HTML site or a REST API when you want to publish or integrate.

## Under the hood

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
