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
- Installs auto-install presets (core)
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

## `weos account audit-members`

List the memberships of an account that nothing on record explains. The command only reads: it
changes nothing and never removes a row. It reads the store directly and needs no running server.

```bash
weos account audit-members --database-dsn <dsn> [--account <id>]
```

| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--account` | string | No | Account to audit. Default: the instance's first account, the one with the lowest id |

**Why to run it after upgrading.** Earlier versions saved a role change made on the Users page into
the instance's first account, whatever account the owner or admin who made it acted in. Those
memberships are still there, and a person holding `owner` or `admin` through one of them can manage
the first account's people.

**What it lists.** A membership is listed when:

- the account's history does not record the person joining it, and no invite in the account names
  them, by the person or by an address on one of their credentials — `no sign-up or invite in this
  account`; or
- the membership's role is not the role their joining recorded — `role differs from the recorded
  <role>`.

A role an owner or admin of the account changed on the Users page since is listed as well, because
that change is not recorded either. Check each row with the account's owners before you act on it.

**Output.** One line per membership: `AGENT ID`, `ROLE`, `WRITTEN AT` and `WHY LISTED`. `WRITTEN AT` is
the membership's `created_at`, which every role save rewrites, so it is when the row was last
written. The command prints no email address or name. A warning on standard error says when the
account has no recorded history, because every membership no invite explains is then listed.

**Removing a row.** Back up the database, then run this against the same database, with the account id
and the agent id the command printed:

```sql
DELETE FROM account_members WHERE account_id = '<account id>' AND agent_id = '<agent id>';
```

To put back the role the person's joining recorded instead:

```sql
UPDATE account_members SET role_id = '<recorded role>' WHERE account_id = '<account id>' AND agent_id = '<agent id>';
```

The person keeps every other account they belong to. A browser session they hold in the account is
refused on its next request, and the features they resolved in it expire within
`FEATURE_CACHE_MAX_AGE_SECONDS`. No restart is needed.

---

## `weos maintenance assign-owner`

Give an owning account to the resources one agent wrote with no account. A resource takes its owner
from the session that wrote it, so a session whose active account was empty wrote resources with no
owner, and account-scoped reads never find them.

```bash
weos maintenance assign-owner --database-dsn <dsn> --account <id> --created-by <agent id> \
  [--created-from <time>] [--created-to <time>] [--confirm-account <id>] [--write]
```

**Preview by default.** Without `--write` the command names the account, lists every selected
resource and the agent's accounts, and writes nothing. Only `--write` assigns. An assignment cannot be
undone, so a forgotten flag costs a preview, never a write.

| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--account` | string | Yes | The account to give the resources as their owner |
| `--created-by` | string | Yes | The agent whose ownerless resources are assigned |
| `--created-from` | string | No | Only resources created at or after this time: RFC 3339, or a date (`2026-08-01`, 00:00 UTC) |
| `--created-to` | string | No | Only resources created at or before this time: RFC 3339, or a date, which takes that whole day (UTC) |
| `--confirm-account` | string | With `--write`, when the agent is in more than one account | The same id as `--account`, repeated. A run with `--write` refuses without it when the agent is a member of more than one account |
| `--write` | bool | No | Apply the assignment. Without it (the default) the command is a preview: it names the account, lists every selected resource (created_at, type, id) and the counts per type, and changes nothing |

**What it changes.** Every resource, of every type, with an empty owner and the given creator (inside
the window, when one is given) gets one `Resource.AccountAssigned` event, written through the same
unit of work as any other write. The projection tables, the knowledge graph and the search index all
take the owner from that event, and a rebuild from events (`weos worker reproject`) keeps it. A
resource that has an owner is never touched, and an owner is never moved between accounts. **An
assignment cannot be undone**: only a database restore reverses a run. The decision is recorded in
[An Ownerless Resource Is Given Its Owner Through the Event Stream]({% link decisions/resource-account-assigned.md %}).

A run with `--write` also sets `updated_at` on every repaired row to the time of the run, so lists sorted by
last update show those rows first.

**Refusals.** The command refuses to run when the account does not exist, when the agent is not a
member of it, when no database is named, or — with `--write` — when the agent is a member of more
than one account and `--confirm-account` does not repeat `--account`.

**Where the ids come from.** No `weos` command lists accounts or agents, so read them from the same
database:

```sql
-- --created-by: the agents that wrote ownerless resources
SELECT DISTINCT created_by FROM resources WHERE account_id = '' OR account_id IS NULL;
-- or one person's agent id, from an email address
SELECT agent_id FROM credentials WHERE email = '<address>';

-- --account: the accounts that agent is a member of
SELECT a.id, a.name, m.role_id FROM account_members m
  JOIN accounts a ON a.id = m.account_id
  WHERE m.agent_id = '<agent id>';
```

A `--created-by` that wrote no ownerless resource prints "nothing to assign" and exits 0. Check the id
with the first query.

**Procedure.**

1. Upgrade the instance to this release. The preview (no flag) may run while the instance serves: it
   opens the database read-only and starts nothing (no migration, no preset install), so it writes
   nothing.
2. Preview first: run with no flag. Read the account name, the list of resources and the agent's
   accounts.
3. Stop the instance, and back up the database. A run with `--write` opens the store directly, and a
   serving instance competes with it for the database (on SQLite, for the write lock).
4. Add `--write` and run again (with `--confirm-account` when the preview said the agent is in more
   than one account). The command prints the account before it writes.
5. Start the instance.

A run that stops part-way has assigned what it reports; running it again with `--write` assigns the
rest. A resource whose record is behind its event stream is skipped and listed, and the command exits
non-zero: with the instance stopped, run `weos worker reproject` against the same database, then run
this command again.

A resource whose record has the `--account` owner but that has no projection row at all in one of its
type tables (its own type's or an ancestor's) is missing from that type's listing just as surely as
one whose row has no owner. Neither an assignment nor a fill writes a row that does not exist, so the
preview and the run list such a resource with the ones left for a reproject, naming the table, and a
run with `--write` exits non-zero. `weos worker reproject` on the stopped instance writes the row.

A resource is counted as assigned only when its record and each of its projection rows (its own
type's table and each ancestor's) read back with the owner. One with a row the write did not reach at
all — a row that does not exist, or one another account owns, which the upsert keeps — is listed with
the ones left for a reproject, naming the table and the account that holds the row. The record and the rows are written one
after the other, so a failed write can leave a projection row ownerless under a record that has the
owner. The run then stops with an error that names the resource. Fix the cause and run the command
again: it selects that resource, fills the row from the record, and records no second event, because
the assignment is already in the event store. The preview marks such a resource "record has the
owner; projection row to fill". It is selected only when the record's owner is the `--account` of the
run. A resource whose record is owned by another account is left alone, but it is not dropped silently:
the preview and the result count such resources per owning account and print each account id, so the
operator can run again with that `--account` (or, when the agent is not a member of it, run
`weos worker reproject` on the stopped instance).

The preview and the result count these rows to fill apart from the resources to assign. An assignment
stores one `Resource.AccountAssigned` event and a fill stores no event, so only the assigned count
matches the events a run adds to the store. A fill rewrites the whole projection row from the record
and sets its `updated_at` to the time of the run, as `weos worker reproject` does, so it can change
columns other than the owner where the row had drifted from the record. When the preview lists many
rows to fill (it suggests this from 100), stop the instance and run `weos worker reproject` instead: it
repairs every row in one pass. Then preview again.

A fill writes no event and does not write the record, so no version check guards it the way the
unit of work guards an assignment. Instead each row is written only while it still has no owner and its
sequence number is not past the record's, checked in the write statement itself. A row that changed
after the run read the record (a write by a serving instance) is not written over: the resource is
listed with the ones left for a reproject, and the command exits non-zero.

**If an older binary served during or after a run with `--write`.** A binary built before this release does not
know `Resource.AccountAssigned`. Its background workers skip the event and move their checkpoints
past it, so the knowledge graph and the search index never get the owner, and nothing reports it. With
this release, run these two commands, then start the instance; the workers replay the history:

```bash
weos worker checkpoint reset oxigraph
weos worker checkpoint reset lexical-index
```

---

## Make Targets

For convenience, the Makefile provides shortcuts:

| Target | Command | Description |
|--------|---------|-------------|
| `make build` | `go build -o bin/weos ./cmd/weos` | Build binary |
| `make run` | `go run ./cmd/weos serve` | Run server |
| `make test` | `go test -v -race -coverprofile=coverage.out ./...` | All tests |
| `make test-unit` | `go test -v -race -short ./tests/unit/...` | Unit tests |
| `make test-integration` | `go test -v -race ./tests/integration/...` | Integration tests |
| `make test-e2e` | `go test -v ./tests/e2e/...` | E2E tests |
| `make test-graph-embedded` | `CGO_LDFLAGS="$(CGO_LDFLAGS_EMBEDDED)" go test -race -tags oxigraph_embedded ./infrastructure/graph/...` then `CGO_LDFLAGS="$(CGO_LDFLAGS_EMBEDDED)" go test -race -tags oxigraph_embedded ./application/ -run 'EmbeddedLabelSearch'` then `CGO_LDFLAGS="$(CGO_LDFLAGS_EMBEDDED)" go test -race -tags oxigraph_embedded ./tests/e2e/ -run 'KnowledgeGraph'` then `CGO_LDFLAGS="$(CGO_LDFLAGS_EMBEDDED)" go test -race -tags oxigraph_embedded ./tests/e2e/ -run 'TestAccountDeletion'` | Embedded oxigraph backend tests, including the embedded label search and the account deletion suite (fetches the vendored static lib first) |
| `make lint` | `golangci-lint run ./...` | Lint |
| `make fmt` | `go fmt` + `goimports` | Format code |
| `make deps` | `go mod download && go mod tidy` | Install dependencies |
| `make dev-seed` | Build + seed | Seed dev data |
| `make dev-serve` | Build + serve (no OAuth) | Dev server |
| `make dev-setup` | Seed + serve | Full dev setup |
| `make dev-build-frontend` | Nuxt generate + copy | Build admin UI |
| `make clean` | Remove `bin/`, coverage files | Clean build artifacts |
