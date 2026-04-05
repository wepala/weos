---
title: Configure the Database
parent: How-to Guides
layout: default
nav_order: 2
---

# Configure the Database

WeOS auto-detects the database driver from the DSN format. No driver flag is needed.

## SQLite (Default)

Zero configuration. WeOS creates a `weos.db` file in the current directory:

```bash
weos serve
```

Or specify a custom path:

```bash
DATABASE_DSN=/path/to/my.db weos serve
```

SQLite with query parameters:

```bash
DATABASE_DSN="file:weos.db?cache=shared&_foreign_keys=1" weos serve
```

## PostgreSQL

Set `DATABASE_DSN` to a PostgreSQL connection string:

```bash
# Key-value format
DATABASE_DSN="host=localhost user=postgres password=secret dbname=weos sslmode=disable" weos serve

# URI format
DATABASE_DSN="postgres://postgres:secret@localhost:5432/weos?sslmode=disable" weos serve
```

WeOS auto-detects PostgreSQL from the `host=` prefix or `postgres://` scheme.

## Using a .env File

Create a `.env` file in the project root:

```bash
DATABASE_DSN=postgres://postgres:secret@localhost:5432/weos?sslmode=disable
```

This is loaded automatically on startup. The `.env` file is in `.gitignore` by default.

## CLI Flag Override

The `--database-dsn` flag overrides both the default and the environment variable:

```bash
weos serve --database-dsn "postgres://localhost:5432/weos"
```

## Auto-Migration

On startup, WeOS runs GORM AutoMigrate for core tables (event store, resource types, settings). Projection tables are created dynamically when resource types are registered.
