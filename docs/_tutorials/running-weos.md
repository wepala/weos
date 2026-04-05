---
title: Running WeOS
parent: Tutorials
layout: default
nav_order: 1
---

# Running WeOS

In this tutorial you'll build WeOS from source, start the server, and verify everything works. By the end you'll have a running WeOS instance serving a default site on `localhost:8080`.

## Prerequisites

- **Go 1.25+** installed ([go.dev/dl](https://go.dev/dl/))
- **Git** installed
- A terminal

## Step 1: Clone the Repository

```bash
git clone https://github.com/wepala/weos.git
cd weos
```

## Step 2: Download Dependencies

```bash
make deps
```

This runs `go mod download` and `go mod tidy` to fetch all Go modules.

## Step 3: Build the Binary

```bash
make build
```

This compiles the `weos` binary into `bin/weos`. You can verify it built correctly:

```bash
./bin/weos --version
```

You should see output like: `weos version 0.1.0`

## Step 4: Start the Server

```bash
./bin/weos serve
```

By default, WeOS:
- Binds to `0.0.0.0:8080`
- Uses SQLite with a local `weos.db` file (created automatically)
- Serves the embedded frontend SPA
- Runs in development mode (no OAuth required)

You should see log output confirming the server is running.

## Step 5: Verify It Works

### Check the health endpoint

```bash
curl http://localhost:8080/api/health
```

Expected response:

```json
{"status": "ok"}
```

### Open the admin UI

Navigate to [http://localhost:8080](http://localhost:8080) in your browser. You should see the WeOS admin interface.

### Install a preset and create content

While the server is running, open a second terminal and try:

```bash
# Install the "tasks" preset (creates Project and Task resource types)
./bin/weos resource-type preset install tasks

# List installed resource types
./bin/weos resource-type list

# Create a project
./bin/weos resource create --type project --data '{"name": "My First Project", "description": "Testing WeOS"}'

# List projects
./bin/weos resource list --type project
```

## Step 6: Seed Sample Data (Optional)

For a richer starting experience, seed the database with sample users, presets, and content:

```bash
make dev-seed
```

This installs presets (including tasks), creates sample users, and populates projects and tasks.

## What You've Learned

- How to build WeOS from source with `make build`
- How to start the server with `weos serve`
- How to verify the server is running via the health endpoint
- How to install presets and create resources via the CLI
- How to seed development data

## What's Next

- [Creating a Preset]({% link _tutorials/creating-a-preset.md %}) — define your own resource types
- [Connecting MCP to an LLM]({% link _tutorials/connecting-mcp-to-llm.md %}) — let an AI manage your site
- [Configure the Database]({% link _howto/configure-database.md %}) — switch from SQLite to PostgreSQL

## Common Options

| Flag | Purpose | Default |
|------|---------|---------|
| `--database-dsn` | Database connection string | (uses config default: `weos.db`) |
| `--verbose` | Enable debug logging | `false` |

You can also set these via environment variables. See [Environment Variables]({% link _reference/environment-variables.md %}) for the full list.
