---
title: CLI Commands
parent: Reference
layout: default
nav_order: 1
---

# CLI Commands

The `weos` binary provides all functionality through Cobra subcommands.

## Global Flags

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--database-dsn` | | string | `""` | Database connection string (overrides `DATABASE_DSN` env var) |
| `--verbose` | `-v` | bool | `false` | Enable debug logging |
| `--version` | | | | Print version and exit |

## `weos serve`

Start the HTTP API server.

```bash
weos serve
```

**Behavior:**
- Starts an Echo HTTP server with API routes under `/api`
- Serves embedded frontend SPA for all non-API routes
- Auto-migrates database tables on startup
- Installs auto-install presets (core, auth)
- Runs in development mode when OAuth is not configured

**Environment variables used:** `PORT` (overrides `SERVER_PORT`), `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `FRONTEND_URL`

**Default:** Binds to `0.0.0.0:8080`, uses SQLite `weos.db`

---

## `weos mcp`

Start the MCP (Model Context Protocol) server using stdio transport.

```bash
weos mcp [--services <name>...]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--services` | string slice | (all) | Tool groups to enable. Can be repeated. Valid values: `person`, `organization`, `resource-type`, `resource` |

**Environment variable:** `MCP_SERVICES` (comma-separated list)

**Examples:**
```bash
# All tools
weos mcp

# Only resource management
weos mcp --services resource --services resource-type

# Via environment variable
MCP_SERVICES=person,organization weos mcp
```

---

## `weos resource-type` (alias: `rt`)

Manage resource type definitions.

### `resource-type create`

```bash
weos resource-type create --name <name> --slug <slug> [--description <desc>] [--context <json>] [--schema <json>]
```

| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--name` | string | Yes | Display name |
| `--slug` | string | Yes | URL-safe identifier |
| `--description` | string | No | Description |
| `--context` | string | No | JSON-LD context (JSON string) |
| `--schema` | string | No | JSON Schema (JSON string) |

### `resource-type get <id>`

Get a resource type by its ID (URN).

### `resource-type list`

```bash
weos resource-type list [--limit <n>] [--cursor <token>]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--limit` | int | 20 | Items per page |
| `--cursor` | string | | Pagination cursor |

### `resource-type delete <id>`

Delete (archive) a resource type by ID.

### `resource-type preset install <name>`

Install a preset by name. See [Preset Catalog]({% link _reference/preset-catalog.md %}) for available presets.

```bash
weos resource-type preset install tasks
weos resource-type preset install tasks --update  # update existing types
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--update` | bool | `false` | Update existing resource types with preset definitions instead of skipping them |

### `resource-type preset list`

List all available presets.

---

## `weos resource` (alias: `res`)

Manage resources of any type.

### `resource create`

```bash
weos resource create --type <slug> --data '<json>'
```

| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--type` | string | Yes | Resource type slug |
| `--data` | string | Yes | Resource data (JSON string) |

### `resource get <id>`

Get a resource by ID (URN).

### `resource list`

```bash
weos resource list --type <slug> [--limit <n>] [--cursor <token>]
```

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--type` | string | Yes | | Resource type slug |
| `--limit` | int | No | 20 | Items per page |
| `--cursor` | string | No | | Pagination cursor |

### `resource delete <id>`

Delete (archive) a resource by ID.

---

## `weos person`

Manage persons (FOAF/Schema.org Person entities).

### `person create`

```bash
weos person create --given-name <name> --family-name <name> [--email <email>]
```

| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--given-name` | string | Yes | First name |
| `--family-name` | string | Yes | Last name |
| `--email` | string | No | Email address |

### `person get <id>`

### `person list`

```bash
weos person list [--limit <n>] [--cursor <token>]
```

### `person delete <id>`

---

## `weos organization` (alias: `org`)

Manage organizations (W3C ORG/Schema.org Organization entities).

### `organization create`

```bash
weos organization create --name <name> --slug <slug>
```

| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--name` | string | Yes | Organization name |
| `--slug` | string | Yes | URL-safe identifier |

### `organization get <id>`

### `organization list`

```bash
weos organization list [--limit <n>] [--cursor <token>]
```

### `organization delete <id>`

---

## `weos seed`

Seed the database with development data.

```bash
weos seed
```

**Behavior:**
- Creates sample users (admin and member)
- Installs the `tasks` preset
- Creates sample projects and tasks
- Writes a seed manifest to `.dev-seed.json`
- Idempotent — safe to run multiple times

---

## Make Targets

For convenience, the Makefile provides shortcuts:

| Target | Command | Description |
|--------|---------|-------------|
| `make build` | `go build -o bin/weos ./cmd/weos` | Build binary |
| `make run` | `go run ./cmd/weos serve` | Run server |
| `make test` | `go test -v -race -coverprofile=coverage.out ./...` | All tests |
| `make test-unit` | `go test -v -short ./tests/unit/...` | Unit tests |
| `make test-integration` | `go test -v ./tests/integration/...` | Integration tests |
| `make test-e2e` | `go test -v ./tests/e2e/...` | E2E tests |
| `make lint` | `golangci-lint run ./...` | Lint |
| `make fmt` | `go fmt` + `goimports` | Format code |
| `make deps` | `go mod download && go mod tidy` | Install dependencies |
| `make dev-seed` | Build + seed | Seed dev data |
| `make dev-serve` | Build + serve (no OAuth) | Dev server |
| `make dev-setup` | Seed + serve | Full dev setup |
| `make dev-build-frontend` | Nuxt generate + copy | Build admin UI |
| `make clean` | Remove `bin/`, coverage files | Clean build artifacts |
